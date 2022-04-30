package cliaws

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/costexplorer"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["cost-explorer"] = costExplorer
	lib.Args["cost-explorer"] = costExplorerArgs{}
}

type costExplorerArgs struct {
	AccountNum   string `arg:"required,positional"`
	DaysAgoStart int    `arg:"-d,--days-ago-start" default:"7"`
	Region       string `arg:"-r,--region"`
}

func (costExplorerArgs) Description() string {
	return "\ncost explorer\n"
}

func costExplorer() {
	var args costExplorerArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var token *string
	var results []*costexplorer.ResultByTime
	filters := []*costexplorer.Expression{
		{
			Dimensions: &costexplorer.DimensionValues{
				Key:    aws.String(costexplorer.DimensionLinkedAccount),
				Values: []*string{aws.String(args.AccountNum)},
			},
		},
	}
	if args.Region != "" {
		filters = append(filters, &costexplorer.Expression{
			Dimensions: &costexplorer.DimensionValues{
				Key:    aws.String(costexplorer.DimensionRegion),
				Values: []*string{aws.String(args.Region)},
			},
		})
	}
	var filter *costexplorer.Expression
	if len(filters) == 1 {
		filter = filters[0]
	} else {
		filter = &costexplorer.Expression{And: filters}
	}
	for {
		out, err := lib.CostExplorerClient().GetCostAndUsageWithContext(ctx, &costexplorer.GetCostAndUsageInput{
			Filter:      filter,
			Granularity: aws.String(costexplorer.GranularityHourly),
			GroupBy: []*costexplorer.GroupDefinition{
				{Type: aws.String(costexplorer.GroupDefinitionTypeDimension), Key: aws.String(costexplorer.DimensionService)},
			},
			Metrics: []*string{
				aws.String(costexplorer.MetricUnblendedCost),
			},
			NextPageToken: token,
			TimePeriod: &costexplorer.DateInterval{
				End:   aws.String(time.Now().UTC().Format(time.RFC3339)),
				Start: aws.String(time.Now().UTC().Add(time.Duration(args.DaysAgoStart) * -1 * 24 * time.Hour).Format(time.RFC3339)),
			},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		// dimensions = append(dimensions, out.DimensionValueAttributes...)
		// groups = append(groups, out.GroupDefinitions...)
		results = append(results, out.ResultsByTime...)
		if out.NextPageToken == nil {
			break
		}
		token = out.NextPageToken
	}
	// fmt.Println(lib.Pformat(dimensions))
	// fmt.Println(lib.Pformat(groups))
	// fmt.Println(lib.Pformat(results))
	dashes := regexp.MustCompile(`\-+`)
	for _, result := range results {
		var vals []string
		for _, group := range result.Groups {
			if len(group.Keys) != 1 {
				panic(lib.PformatAlways(group))
			}
			key := *group.Keys[0]
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
