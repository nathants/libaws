package cliaws

import (
	"context"
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["cloudwatch-get-metric"] = cloudwatchGetMetric
	lib.Args["cloudwatch-get-metric"] = cloudwatchGetMetricArgs{}
}

type cloudwatchGetMetricArgs struct {
	Namespace string `arg:"positional,required"`
	Metric    string `arg:"positional,required"`
	Dimension string `arg:"positional,required"`
	FromHours int    `arg:"-f,--from-hours" default:"72" help:"get data no older than this"`
	ToHours   int    `arg:"-t,--to-hours" default:"0" help:"get data no younger than this"`
	Period    int    `arg:"-p,--period" default:"60" help:"granularity of data in seconds"`
	Stat      string `arg:"-s,--stat" default:"Average" help:"how to summarize data"`
}

func (cloudwatchGetMetricArgs) Description() string {
	return "\nget cloudwatch metric\n"
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

	out, err := lib.CloudwatchGetMetricData(ctx, args.Period, args.Stat, fromTime, toTime, args.Namespace, args.Metric, args.Dimension)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, o := range out {
		if len(o.Timestamps) != len(o.Values) {
			panic(fmt.Sprint(len(o.Timestamps), "!=", len(o.Values)))
		}
		for i, t := range o.Timestamps {
			v := o.Values[i]
			fmt.Println(t.Format(time.RFC3339), *v)
		}
	}
}
