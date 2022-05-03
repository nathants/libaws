package cliaws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
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
	DaysAgoStart int    `arg:"-d,--days-ago-start" default:"14"`
}

func (costExplorerArgs) Description() string {
	return "\ncost explorer, caching data locally by hour since the api is very expensive\n"
}

func cachePath(accountNum string, hourStartTime time.Time) string {
	home := os.Getenv("HOME")
	if home == "" {
		panic("$HOME is empty string")
	}
	return fmt.Sprintf("%s/.cli-aws/cost-explorer-cache/%s/%s.json", home, accountNum, hourStartTime.Format(time.RFC3339))
}

func costExplorer() {
	var args costExplorerArgs
	arg.MustParse(&args)
	ctx := context.Background()
	start := time.Now().UTC().Add(time.Duration(args.DaysAgoStart) * -1 * 24 * time.Hour).Round(time.Hour)
	end := start
	for {
		start = end
		end = end.Add(1 * time.Hour)
		if end.After(time.Now().UTC()) {
			break
		}
		pth := cachePath(args.AccountNum, start)
		if lib.Exists(pth) {
			continue
		}
		out, err := lib.CostExplorerClient().GetCostAndUsageWithContext(ctx, &costexplorer.GetCostAndUsageInput{
			Filter: &costexplorer.Expression{
				Dimensions: &costexplorer.DimensionValues{
					Key:    aws.String(costexplorer.DimensionLinkedAccount),
					Values: []*string{aws.String(args.AccountNum)},
				},
			},
			Granularity: aws.String(costexplorer.GranularityHourly),
			GroupBy: []*costexplorer.GroupDefinition{{
				Type: aws.String(costexplorer.GroupDefinitionTypeDimension),
				Key:  aws.String(costexplorer.DimensionService),
			}},
			Metrics: []*string{
				aws.String(costexplorer.MetricUnblendedCost),
			},
			TimePeriod: &costexplorer.DateInterval{
				Start: aws.String(start.Format(time.RFC3339)),
				End:   aws.String(end.Format(time.RFC3339)),
			},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if out.NextPageToken != nil {
			panic("should not be paginating")
		}
		data, err := json.Marshal(out.ResultsByTime)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(path.Dir(pth), os.ModePerm)
		if err != nil {
			panic(err)
		}
		err = os.WriteFile(pth, data, os.ModePerm)
		if err != nil {
			panic(err)
		}
		lib.Logger.Println("cached data for:", pth)
	}
	dashes := regexp.MustCompile(`\-+`)
	start = time.Now().UTC().Add(time.Duration(args.DaysAgoStart) * -1 * 24 * time.Hour).Round(time.Hour)
	end = start
	for {
		start = end
		end = end.Add(1 * time.Hour)
		if end.After(time.Now().UTC()) {
			break
		}
		pth := cachePath(args.AccountNum, start)
		if !lib.Exists(pth) {
			panic("data should exist at this point")
		}
		results := []*costexplorer.ResultByTime{}
		data, err := os.ReadFile(pth)
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(data, &results)
		if err != nil {
			panic(err)
		}
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
}
