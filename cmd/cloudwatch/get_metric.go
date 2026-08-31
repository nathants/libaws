package libaws

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cloudwatch-get-metric"] = cloudwatchGetMetric
	lib.Args["cloudwatch-get-metric"] = cloudwatchGetMetricArgs{}
}

type cloudwatchGetMetricArgs struct {
	Namespace string `arg:"positional,required"`
	Metric    string `arg:"positional,required" help:"comma separated list of metrics"`
	Dimension string `arg:"positional,required"`
	FromHours int    `arg:"-f,--from-hours" default:"72" help:"get data no older than this"`
	ToHours   int    `arg:"-t,--to-hours" default:"0" help:"get data no younger than this"`
	Period    int    `arg:"-p,--period" default:"60" help:"granularity of data in seconds"`
	Stat      string `arg:"-s,--stat" default:"Average" help:"how to summarize data"`
}

func (cloudwatchGetMetricArgs) Description() string {
	return "\nget cloudwatch metric; sparse timestamps omit metrics without a data point\n"
}

func cloudwatchGetMetric() {
	var args cloudwatchGetMetricArgs
	arg.MustParse(&args)
	ctx := context.Background()

	toTime := aws.Time(time.Now().UTC())
	if args.ToHours != 0 {
		offset := -1 * time.Hour * time.Duration(args.ToHours)
		toTime = aws.Time(time.Now().UTC().Add(offset))
	}
	offset := -1 * time.Hour * time.Duration(args.FromHours)
	fromTime := aws.Time(time.Now().UTC().Add(offset))

	metrics := strings.Split(args.Metric, ",")
	out, err := lib.CloudwatchGetMetricData(ctx, args.Period, args.Stat, fromTime, toTime, args.Namespace, metrics, args.Dimension)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if err := lib.CloudwatchWriteMetricData(os.Stdout, metrics, args.Dimension, out); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
