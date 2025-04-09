package libaws

import (
	"context"
	"fmt"
	"sort"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cloudwatch-ls-dimensions"] = cloudwatchLsDimensions
	lib.Args["cloudwatch-ls-dimensions"] = cloudwatchLsDimensionsArgs{}
}

type cloudwatchLsDimensionsArgs struct {
	Namespace string `arg:"positional,required"`
	Metric    string `arg:"positional,required"`
}

func (cloudwatchLsDimensionsArgs) Description() string {
	return "\nlist cloudwatch dimensions\n"
}

func cloudwatchLsDimensions() {
	var args cloudwatchLsDimensionsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	metrics, err := lib.CloudwatchListMetrics(ctx, aws.String(args.Namespace), aws.String(args.Metric))
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	dimensionsMap := map[string]any{}
	for _, m := range metrics {
		for _, d := range m.Dimensions {
			dimensionsMap[*d.Name+"="+*d.Value] = nil
		}
	}
	var dimensions []string
	for n := range dimensionsMap {
		dimensions = append(dimensions, n)
	}
	sort.Strings(dimensions)
	for _, d := range dimensions {
		fmt.Println(d)
	}
}
