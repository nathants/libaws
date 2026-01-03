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
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cost-summary"] = costSummary
	lib.Args["cost-summary"] = costSummaryArgs{}
}

type costSummaryArgs struct {
	DaysAgo int `arg:"-d,--days-ago" default:"30" help:"number of days to look back"`
}

func (costSummaryArgs) Description() string {
	return "\nshow cost summary across all organization accounts, grouped by account and service\n"
}

type costSummaryEntry struct {
	AccountID   string
	AccountName string
	Service     string
	Cost        float64
}

func costSummary() {
	var args costSummaryArgs
	arg.MustParse(&args)
	ctx := context.Background()

	var token *string
	var accounts []orgtypes.Account
	for {
		out, err := lib.OrganizationsClient().ListAccounts(ctx, &organizations.ListAccountsInput{
			NextToken: token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		accounts = append(accounts, out.Accounts...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}

	accountNames := map[string]string{}
	for _, acc := range accounts {
		accountNames[*acc.Id] = *acc.Name
	}

	start := time.Now().UTC().Add(time.Duration(args.DaysAgo) * -1 * 24 * time.Hour).Truncate(24 * time.Hour)
	end := time.Now().UTC().Truncate(24 * time.Hour)

	accountServiceCosts := map[string]map[string]float64{}
	var pageToken *string
	for {
		out, err := lib.CostExplorerClient().GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			NextPageToken: pageToken,
			Granularity:   cetypes.GranularityDaily,
			GroupBy: []cetypes.GroupDefinition{
				{
					Type: cetypes.GroupDefinitionTypeDimension,
					Key:  aws.String(string(cetypes.DimensionLinkedAccount)),
				},
				{
					Type: cetypes.GroupDefinitionTypeDimension,
					Key:  aws.String(string(cetypes.DimensionService)),
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
				accountID := group.Keys[0]
				service := lib.NormalizeServiceName(group.Keys[1])
				cost := 0.0
				if amount, ok := group.Metrics["UnblendedCost"]; ok && amount.Amount != nil {
					_, err := fmt.Sscanf(*amount.Amount, "%f", &cost)
					if err != nil {
						panic(err)
					}
				}
				if accountServiceCosts[accountID] == nil {
					accountServiceCosts[accountID] = map[string]float64{}
				}
				accountServiceCosts[accountID][service] += cost
			}
		}

		if out.NextPageToken == nil {
			break
		}
		pageToken = out.NextPageToken
	}

	var entries []costSummaryEntry
	for accountID, services := range accountServiceCosts {
		name := accountNames[accountID]
		if name == "" {
			name = accountID
		}
		for service, cost := range services {
			if cost > 0.01 {
				entries = append(entries, costSummaryEntry{
					AccountID:   accountID,
					AccountName: name,
					Service:     service,
					Cost:        cost,
				})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Cost > entries[j].Cost
	})

	for _, e := range entries {
		var vals []string
		vals = append(vals, fmt.Sprintf("account-id=%s", e.AccountID))
		vals = append(vals, fmt.Sprintf("account-name=%s", e.AccountName))
		vals = append(vals, fmt.Sprintf("service=%s", e.Service))
		vals = append(vals, fmt.Sprintf("cost=%.6f", e.Cost))
		fmt.Println(strings.Join(vals, " "))
	}
}
