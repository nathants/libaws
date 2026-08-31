package lib

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type cloudwatchMetricValue struct {
	value   float64
	present bool
}

type cloudwatchMetricTime struct {
	key   int64
	label string
}

func CloudwatchWriteMetricData(w io.Writer, metrics []string, dimension string, out []cwtypes.MetricDataResult) error {
	if len(metrics) == 0 {
		return fmt.Errorf("no metrics requested")
	}
	metricIndexes := make(map[string]int, len(metrics))
	for i, metric := range metrics {
		if metric == "" {
			return fmt.Errorf("metric %d is empty", i)
		}
		for _, existingMetric := range metrics[:i] {
			if existingMetric == metric {
				return fmt.Errorf("metric %q was requested more than once", metric)
			}
		}
		metricIndexes[fmt.Sprintf("m%d", i)] = i
	}

	seenResults := make([]bool, len(metrics))
	values := map[int64][]cloudwatchMetricValue{}
	timesByKey := map[int64]cloudwatchMetricTime{}
	for _, result := range out {
		if result.Id == nil || *result.Id == "" {
			return fmt.Errorf("CloudWatch returned a metric result without an ID")
		}
		metricIndex, ok := metricIndexes[*result.Id]
		if !ok {
			return fmt.Errorf("CloudWatch returned unexpected metric result ID %q", *result.Id)
		}
		seenResults[metricIndex] = true
		if len(result.Timestamps) != len(result.Values) {
			return fmt.Errorf(
				"CloudWatch metric %q returned %d timestamps and %d values",
				metrics[metricIndex],
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
				timesByKey[key] = cloudwatchMetricTime{
					key:   key,
					label: timestamp.UTC().Format(time.RFC3339Nano),
				}
			}
			if row[metricIndex].present {
				return fmt.Errorf(
					"CloudWatch metric %q returned duplicate timestamp %s",
					metrics[metricIndex],
					timestamp.UTC().Format(time.RFC3339Nano),
				)
			}
			row[metricIndex] = cloudwatchMetricValue{value: result.Values[i], present: true}
		}
	}
	for i, seen := range seenResults {
		if !seen {
			return fmt.Errorf("CloudWatch omitted result for metric %q", metrics[i])
		}
	}

	times := make([]cloudwatchMetricTime, 0, len(timesByKey))
	for _, timestamp := range timesByKey {
		times = append(times, timestamp)
	}
	sort.Slice(times, func(i, j int) bool {
		return times[i].key > times[j].key
	})

	var output strings.Builder
	dimensionLabel := strings.ReplaceAll(dimension, "=", "-")
	for _, timestamp := range times {
		row := values[timestamp.key]
		fmt.Fprint(&output, "timestamp="+timestamp.label, " ")
		for i, metric := range metrics {
			if !row[i].present {
				continue
			}
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
