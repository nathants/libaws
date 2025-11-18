package libaws

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-price-spot"] = ec2PriceSpot
	lib.Args["ec2-price-spot"] = ec2PriceSpotArgs{}
}

type ec2PriceSpotArgs struct {
	Type string `arg:"positional,required" help:"instance type, e.g. m6i.large"`
	Days int    `arg:"-d,--days" default:"7" help:"number of days of history to scan"`
}

type ec2ZonePrice struct {
	Zone  string
	Price float64
}

func (ec2PriceSpotArgs) Description() string {
	return "\nshow EC2 spot max price per availability zone\n"
}

func ec2PriceSpot() {
	var args ec2PriceSpotArgs
	arg.MustParse(&args)
	ctx := context.Background()

	region := lib.Region()
	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(time.Duration(-args.Days) * 24 * time.Hour)

	input := &ec2.DescribeSpotPriceHistoryInput{
		InstanceTypes: []ec2types.InstanceType{
			ec2types.InstanceType(args.Type),
		},
		ProductDescriptions: []string{
			"Linux/UNIX (Amazon VPC)",
		},
		StartTime:  aws.Time(start),
		EndTime:    aws.Time(end),
		MaxResults: aws.Int32(1000),
	}

	var history []ec2types.SpotPrice
	for {
		var out *ec2.DescribeSpotPriceHistoryOutput
		err := lib.Retry(ctx, func() error {
			var err error
			out, err = lib.EC2Client().DescribeSpotPriceHistory(ctx, input)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			lib.Logger.Println("error:", err)
			os.Exit(1)
		}
		history = append(history, out.SpotPriceHistory...)
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		input.NextToken = out.NextToken
	}

	pricesByZone := map[string]float64{}
	for _, sp := range history {
		zone := aws.ToString(sp.AvailabilityZone)
		if zone == "" || !strings.HasPrefix(zone, region) {
			continue
		}
		pstr := aws.ToString(sp.SpotPrice)
		if pstr == "" {
			continue
		}
		price, err := strconv.ParseFloat(pstr, 64)
		if err != nil {
			continue
		}
		prev, ok := pricesByZone[zone]
		if !ok || price > prev {
			pricesByZone[zone] = price
		}
	}

	if len(pricesByZone) == 0 {
		os.Exit(1)
	}

	var results []ec2ZonePrice
	for zone, price := range pricesByZone {
		results = append(results, ec2ZonePrice{
			Zone:  zone,
			Price: price,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Price < results[j].Price })

	func() {
		ctx := context.Background()
		prices, err := lib.EC2OnDemandPrices(ctx, region, args.Type)
		if err != nil {
			lib.Logger.Println("warn: failed to fetch on-demand price:", err)
			return
		}
		ondemand, ok := prices[args.Type]
		if !ok || ondemand <= 0 {
			return
		}
		spot := results[0].Price
		savings := int((ondemand - spot) / ondemand * 100)
		fmt.Fprintf(os.Stderr, "on demand: %g, spot offers %d%% savings\n", ondemand, savings)
	}()

	for _, r := range results {
		fmt.Printf("%s %g\n", r.Zone, r.Price)
	}
}
