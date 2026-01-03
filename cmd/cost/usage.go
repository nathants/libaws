package libaws

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cost-usage"] = costUsage
	lib.Args["cost-usage"] = costUsageArgs{}
}

type costUsageArgs struct {
	DaysAgo int    `arg:"-d,--days-ago" default:"30" help:"number of days to look back"`
	Service string `arg:"-s,--service" help:"filter by service (e.g., 'EC2 - Other')"`
}

func (costUsageArgs) Description() string {
	return "\nshow cost breakdown by usage type across all accounts\n"
}

type costUsageKey struct {
	Service   string
	UsageType string
}

type costUsageEntry struct {
	Service   string
	UsageType string
	Cost      float64
}

func costUsage() {
	var args costUsageArgs
	arg.MustParse(&args)
	ctx := context.Background()

	start := time.Now().UTC().Add(time.Duration(args.DaysAgo) * -1 * 24 * time.Hour).Truncate(24 * time.Hour)
	end := time.Now().UTC().Truncate(24 * time.Hour)

	costs := map[costUsageKey]float64{}

	var filter *cetypes.Expression
	if args.Service != "" {
		filter = &cetypes.Expression{
			Dimensions: &cetypes.DimensionValues{
				Key:    cetypes.DimensionService,
				Values: []string{args.Service},
			},
		}
	}

	var pageToken *string
	for {
		out, err := lib.CostExplorerClient().GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			NextPageToken: pageToken,
			Filter:        filter,
			Granularity:   cetypes.GranularityDaily,
			GroupBy: []cetypes.GroupDefinition{
				{
					Type: cetypes.GroupDefinitionTypeDimension,
					Key:  aws.String(string(cetypes.DimensionService)),
				},
				{
					Type: cetypes.GroupDefinitionTypeDimension,
					Key:  aws.String(string(cetypes.DimensionUsageType)),
				},
			},
			Metrics: []string{
				string(cetypes.MetricUnblendedCost),
			},
			TimePeriod: &cetypes.DateInterval{
				Start: aws.String(start.Format("2006-01-02")),
				End:   aws.String(end.Format("2006-01-02")),
			},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}

		for _, result := range out.ResultsByTime {
			for _, group := range result.Groups {
				if len(group.Keys) != 2 {
					panic(lib.PformatAlways(group))
				}
				service := lib.NormalizeServiceName(group.Keys[0])
				usageType := group.Keys[1]
				cost := 0.0
				if amount, ok := group.Metrics["UnblendedCost"]; ok && amount.Amount != nil {
					_, err := fmt.Sscanf(*amount.Amount, "%f", &cost)
					if err != nil {
						panic(err)
					}
				}
				key := costUsageKey{service, usageType}
				costs[key] += cost
			}
		}

		if out.NextPageToken == nil {
			break
		}
		pageToken = out.NextPageToken
	}

	var entries []costUsageEntry
	for key, cost := range costs {
		if cost > 0.01 {
			entries = append(entries, costUsageEntry{
				Service:   key.Service,
				UsageType: key.UsageType,
				Cost:      cost,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Cost > entries[j].Cost
	})

	for _, e := range entries {
		var vals []string
		vals = append(vals, fmt.Sprintf("service=%s", e.Service))
		vals = append(vals, fmt.Sprintf("usage-type=%s", e.UsageType))
		vals = append(vals, fmt.Sprintf("cost=%.6f", e.Cost))
		fmt.Println(strings.Join(vals, " "))
	}
}
