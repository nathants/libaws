package cliaws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
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

	metrics := strings.Split(args.Metric, ",")
	out, err := lib.CloudwatchGetMetricData(ctx, args.Period, args.Stat, fromTime, toTime, args.Namespace, metrics, args.Dimension)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	timesMap := make(map[string]interface{})
	var times []string
	vals := make(map[string][]float64)
	for i, o := range out {
		if metrics[i] != *o.Label {
			panic(fmt.Sprint(metrics[i], *o.Label))
		}
		if len(o.Timestamps) != len(o.Values) {
			panic(fmt.Sprint(len(o.Timestamps), "!=", len(o.Values)))
		}
		for j, t := range o.Timestamps {
			v := o.Values[j]
			t := t.Format(time.RFC3339)
			_, ok := timesMap[t]
			if !ok {
				times = append(times, t)
				timesMap[t] = nil
			}
			vals[t] = append(vals[t], *v)
		}
	}
	for _, t := range times {
		fmt.Print("timestamp="+t, " ")
		for i, m := range metrics {
			if len(metrics) == 1 {
				m = ""
			} else {
				m = "::" + m
			}
			fmt.Print(strings.ReplaceAll(args.Dimension, "=", "-")+m+"="+fmt.Sprint(vals[t][i]), " ")
		}
		fmt.Print("\n")
	}
}
