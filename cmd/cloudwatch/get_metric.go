package libaws

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
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
	if err := writeCloudwatchMetricData(os.Stdout, metrics, args.Dimension, out); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}

type cloudwatchMetricValue struct {
	value   float64
	present bool
}

type cloudwatchMetricTime struct {
	key   int64
	label string
}

func writeCloudwatchMetricData(w io.Writer, metrics []string, dimension string, out []cwtypes.MetricDataResult) error {
	if len(metrics) == 0 {
		return fmt.Errorf("no metrics requested")
	}
	metricIndexes := make(map[string]int, len(metrics))
	for i, metric := range metrics {
		if metric == "" {
			return fmt.Errorf("metric %d is empty", i)
		}
		if _, exists := metricIndexes[metric]; exists {
			return fmt.Errorf("metric %q was requested more than once", metric)
		}
		metricIndexes[metric] = i
	}

	seenResults := make([]bool, len(metrics))
	values := map[int64][]cloudwatchMetricValue{}
	var times []cloudwatchMetricTime
	for _, result := range out {
		if result.Label == nil {
			return fmt.Errorf("CloudWatch returned a metric result without a label")
		}
		metricIndex, ok := metricIndexes[*result.Label]
		if !ok {
			return fmt.Errorf("CloudWatch returned unexpected metric %q", *result.Label)
		}
		seenResults[metricIndex] = true
		if len(result.Timestamps) != len(result.Values) {
			return fmt.Errorf(
				"CloudWatch metric %q returned %d timestamps and %d values",
				*result.Label,
				len(result.Timestamps),
				len(result.Values),
			)
		}
		for i, timestamp := range result.Timestamps {
			key := timestamp.UnixNano()
			row, exists := values[key]
			if !exists {
				row = make([]cloudwatchMetricValue, len(metrics))
				values[key] = row
			}
			if row[metricIndex].present {
				return fmt.Errorf(
					"CloudWatch metric %q returned duplicate timestamp %s",
					*result.Label,
					timestamp.Format(time.RFC3339),
				)
			}
			row[metricIndex] = cloudwatchMetricValue{value: result.Values[i], present: true}
			if metricIndex == 0 {
				times = append(times, cloudwatchMetricTime{
					key:   key,
					label: timestamp.Format(time.RFC3339),
				})
			}
		}
	}
	for i, seen := range seenResults {
		if !seen {
			return fmt.Errorf("CloudWatch omitted result for metric %q", metrics[i])
		}
	}

	var output strings.Builder
	dimensionLabel := strings.ReplaceAll(dimension, "=", "-")
	for _, timestamp := range times {
		row := values[timestamp.key]
		complete := true
		for _, value := range row {
			if !value.present {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		fmt.Fprint(&output, "timestamp="+timestamp.label, " ")
		for i, metric := range metrics {
			if len(metrics) == 1 {
				metric = ""
			} else {
				metric = "::" + metric
			}
			fmt.Fprint(&output, dimensionLabel+metric+"="+fmt.Sprint(row[i].value), " ")
		}
		fmt.Fprint(&output, "\n")
	}
	_, err := io.WriteString(w, output.String())
	return err
}
