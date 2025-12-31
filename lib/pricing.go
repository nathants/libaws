package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

var pricingClient *pricing.Client
var pricingClientLock sync.Mutex

func PricingClientExplicit(accessKeyID, accessKeySecret string) *pricing.Client {
	cfg := SessionExplicit(accessKeyID, accessKeySecret, "us-east-1")
	return pricing.NewFromConfig(*cfg)
}

func PricingClient() *pricing.Client {
	pricingClientLock.Lock()
	defer pricingClientLock.Unlock()
	if pricingClient == nil {
		cfg, err := SessionRegion("us-east-1")
		if err != nil {
			panic(err)
		}
		pricingClient = pricing.NewFromConfig(*cfg)
	}
	return pricingClient
}

type ec2PriceProduct struct {
	Product ec2PriceProductProduct `json:"product"`
	Terms   ec2PriceProductTerms   `json:"terms"`
}

type ec2PriceProductProduct struct {
	Attributes ec2PriceProductAttributes `json:"attributes"`
}

type ec2PriceProductAttributes struct {
	InstanceType string `json:"instanceType"`
}

type ec2PriceProductTerms struct {
	OnDemand map[string]ec2PriceProductOnDemandTerm `json:"OnDemand"`
}

type ec2PriceProductOnDemandTerm struct {
	PriceDimensions map[string]ec2PriceProductPriceDimension `json:"priceDimensions"`
}

type ec2PriceProductPriceDimension struct {
	Unit         string            `json:"unit"`
	PricePerUnit map[string]string `json:"pricePerUnit"`
}

func EC2OnDemandPrices(ctx context.Context, region string, instanceType string) (map[string]float64, error) {
	if region == "" {
		region = Region()
	}
	if region == "" {
		return nil, fmt.Errorf("missing aws region")
	}

	client := PricingClient()
	filters := []pricingtypes.Filter{
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("productFamily"),
			Value: aws.String("Compute Instance"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("operatingSystem"),
			Value: aws.String("Linux"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("preInstalledSw"),
			Value: aws.String("NA"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("tenancy"),
			Value: aws.String("Shared"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("regionCode"),
			Value: aws.String(region),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("capacitystatus"),
			Value: aws.String("Used"),
		},
	}
	if instanceType != "" {
		filters = append(filters, pricingtypes.Filter{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("instanceType"),
			Value: aws.String(instanceType),
		})
	}

	input := &pricing.GetProductsInput{
		ServiceCode:   aws.String("AmazonEC2"),
		Filters:       filters,
		FormatVersion: aws.String("aws_v1"),
		MaxResults:    aws.Int32(100),
	}

	prices := map[string]float64{}

	for {
		var out *pricing.GetProductsOutput
		err := Retry(ctx, func() error {
			var err error
			out, err = client.GetProducts(ctx, input)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}

		for _, raw := range out.PriceList {
			var product ec2PriceProduct
			if err := json.Unmarshal([]byte(raw), &product); err != nil {
				continue
			}
			name := product.Product.Attributes.InstanceType
			if name == "" {
				continue
			}
			usd := ""
			for _, term := range product.Terms.OnDemand {
				for _, dim := range term.PriceDimensions {
					if dim.Unit == "Hrs" {
						usd = dim.PricePerUnit["USD"]
						break
					}
				}
				if usd != "" {
					break
				}
			}
			if usd == "" {
				return nil, fmt.Errorf("missing hourly USD price for %s", name)
			}
			price, err := strconv.ParseFloat(usd, 64)
			if err != nil {
				return nil, fmt.Errorf("parse hourly USD price for %s: %w", name, err)
			}
			if price == 0 {
				return nil, fmt.Errorf("zero hourly USD price for %s", name)
			}
			prices[name] = price
		}

		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		input.NextToken = out.NextToken
	}

	if len(prices) == 0 {
		return nil, fmt.Errorf("no prices for region %s instance-type %s", region, instanceType)
	}

	return prices, nil
}

func SortedInstanceTypes(prices map[string]float64) []string {
	var types []string
	for t := range prices {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
