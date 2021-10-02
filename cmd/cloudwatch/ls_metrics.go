package cliaws

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["cloudwatch-ls-metrics"] = cloudwatchLsMetrics
	lib.Args["cloudwatch-ls-metrics"] = cloudwatchLsMetricsArgs{}
}

type cloudwatchLsMetricsArgs struct {
	Namespace string `arg:"positional,required"`
}

func (cloudwatchLsMetricsArgs) Description() string {
	return "\nlist cloudwatch metrics\n"
}

func cloudwatchLsMetrics() {
	var args cloudwatchLsMetricsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	metrics, err := lib.CloudwatchListMetrics(ctx, aws.String(args.Namespace), nil)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	metricsMap := make(map[string]int)
	for _, m := range metrics {
		metricsMap[*m.MetricName] += 0
		for range m.Dimensions {
			metricsMap[*m.MetricName] += 1
		}
	}
	var metricsNames []string
	for m := range metricsMap {
		metricsNames = append(metricsNames, m)
	}
	sort.Strings(metricsNames)
	fmt.Fprintln(os.Stderr, "name", "dimensions")
	for _, m := range metricsNames {
		fmt.Println(m, metricsMap[m])
	}

}
