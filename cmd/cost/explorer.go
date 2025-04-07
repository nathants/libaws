package libaws

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cost-explorer"] = costExplorer
	lib.Args["cost-explorer"] = costExplorerArgs{}
}

type costExplorerArgs struct {
	AccountNum   string `arg:"-a,--account,required"`
	Region       string `arg:"-r,--region"`
	DaysAgoStart int    `arg:"-d,--days-ago-start" default:"7"`
	Daily        bool   `arg:"--daily" help:"use daily instead of hourly granularity"`
}

func (costExplorerArgs) Description() string {
	return "\ncost explorer, caching data locally by hour since the api is very expensive\n"
}

func costExplorer() {
	var args costExplorerArgs
	arg.MustParse(&args)
	ctx := context.Background()
	start := time.Now().UTC().Add(time.Duration(args.DaysAgoStart) * -1 * 24 * time.Hour).Truncate(time.Hour)
	end := time.Now().UTC()
	filter := &cetypes.Expression{
		Dimensions: &cetypes.DimensionValues{
			Key:    cetypes.DimensionLinkedAccount,
			Values: []string{args.AccountNum},
		},
	}
	if args.Region != "" {
		filter = &cetypes.Expression{
			And: []cetypes.Expression{
				{
					Dimensions: &cetypes.DimensionValues{
						Key:    cetypes.DimensionLinkedAccount,
						Values: []string{args.AccountNum},
					},
				},
				{
					Dimensions: &cetypes.DimensionValues{
						Key:    cetypes.DimensionRegion,
						Values: []string{args.Region},
					},
				},
			},
		}
	}
	var results []cetypes.ResultByTime
	var token *string
	granularity := cetypes.GranularityHourly
	formatDate := func(s string) string { return s }
	if args.Daily {
		granularity = cetypes.GranularityDaily
		formatDate = func(s string) string { return s[:10] } // yyyy-MM-dd
	}
	for {
		out, err := lib.CostExplorerClient().GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			NextPageToken: token,
			Filter:        filter,
			Granularity:   granularity,
			GroupBy: []cetypes.GroupDefinition{{
				Type: cetypes.GroupDefinitionTypeDimension,
				Key:  aws.String(string(cetypes.DimensionService)),
			}},
			Metrics: []string{
				string(cetypes.MetricUnblendedCost),
			},
			TimePeriod: &cetypes.DateInterval{
				Start: aws.String(formatDate(start.Format(time.RFC3339))),
				End:   aws.String(formatDate(end.Format(time.RFC3339))),
			},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		results = append(results, out.ResultsByTime...)
		if out.NextPageToken == nil {
			break
		}
		token = out.NextPageToken
	}
	dashes := regexp.MustCompile(`\-+`)
	for _, result := range results {
		var vals []string
		for _, group := range result.Groups {
			if len(group.Keys) != 1 {
				panic(lib.PformatAlways(group))
			}
			key := group.Keys[0]
			key = strings.ReplaceAll(key, "AWS ", "")
			key = strings.ReplaceAll(key, "Amazon Simple Storage Service", "S3")
			key = strings.ReplaceAll(key, "Amazon EC2 Container Registry (ECR)", "ECR")
			key = strings.ReplaceAll(key, "Amazon ", "")
			key = strings.ReplaceAll(key, "Simple Queue Service ", "SQS")
			key = strings.ReplaceAll(key, "API Gateway", "ApiGateway")
			key = strings.ReplaceAll(key, "AmazonCloudWatch", "Cloudwatch")
			key = strings.ReplaceAll(key, " ", "-")
			key = dashes.ReplaceAllString(key, "-")
			key = strings.ReplaceAll(key, "Elastic-Compute-Cloud-Compute", "EC2")
			vals = append(vals, fmt.Sprintf("%s=%s", key, *group.Metrics["UnblendedCost"].Amount))
		}
		fmt.Println("timestamp="+*result.TimePeriod.Start, strings.Join(vals, " "))
	}
}
