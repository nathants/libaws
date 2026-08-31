package lib

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

var cloudwatchClient *cloudwatch.Client
var cloudwatchClientLock sync.Mutex

func CloudwatchClientExplicit(accessKeyID, accessKeySecret, region string) *cloudwatch.Client {
	return cloudwatch.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func CloudwatchClient() *cloudwatch.Client {
	cloudwatchClientLock.Lock()
	defer cloudwatchClientLock.Unlock()
	if cloudwatchClient == nil {
		cloudwatchClient = cloudwatch.NewFromConfig(*Session())
	}
	return cloudwatchClient
}

type CloudwatchAlarm struct {
	alarmArn              *string
	stateReason           *string
	stateReasonData       *string
	stateUpdatedTimestamp *time.Time

	ActionsEnabled                     *bool                      `json:",omitempty"`
	AlarmActions                       []string                   `json:",omitempty"`
	AlarmConfigurationUpdatedTimestamp *time.Time                 `json:",omitempty"`
	AlarmDescription                   *string                    `json:",omitempty"`
	AlarmName                          *string                    `json:",omitempty"`
	ComparisonOperator                 cwtypes.ComparisonOperator `json:",omitempty"`
	DatapointsToAlarm                  *int32                     `json:",omitempty"`
	Dimensions                         []cwtypes.Dimension        `json:",omitempty"`
	EvaluateLowSampleCountPercentile   *string                    `json:",omitempty"`
	EvaluationPeriods                  *int32                     `json:",omitempty"`
	ExtendedStatistic                  *string                    `json:",omitempty"`
	InsufficientDataActions            []string                   `json:",omitempty"`
	MetricName                         *string                    `json:",omitempty"`
	Metrics                            []cwtypes.MetricDataQuery  `json:",omitempty"`
	Namespace                          *string                    `json:",omitempty"`
	OKActions                          []string                   `json:",omitempty"`
	Period                             *int32                     `json:",omitempty"`
	StateValue                         cwtypes.StateValue         `json:",omitempty"`
	Statistic                          cwtypes.Statistic          `json:",omitempty"`
	Threshold                          *float64                   `json:",omitempty"`
	ThresholdMetricId                  *string                    `json:",omitempty"`
	TreatMissingData                   *string                    `json:",omitempty"`
	Unit                               cwtypes.StandardUnit       `json:",omitempty"`
}

func (a *CloudwatchAlarm) FromAlarm(alarm *cwtypes.MetricAlarm) {
	a.alarmArn = alarm.AlarmArn
	a.stateReason = alarm.StateReason
	a.stateReasonData = alarm.StateReasonData
	a.stateUpdatedTimestamp = alarm.StateUpdatedTimestamp
	a.ActionsEnabled = alarm.ActionsEnabled
	a.AlarmActions = alarm.AlarmActions
	a.AlarmConfigurationUpdatedTimestamp = alarm.AlarmConfigurationUpdatedTimestamp
	a.AlarmDescription = alarm.AlarmDescription
	a.AlarmName = alarm.AlarmName
	a.ComparisonOperator = alarm.ComparisonOperator
	a.DatapointsToAlarm = alarm.DatapointsToAlarm
	a.Dimensions = alarm.Dimensions
	a.EvaluateLowSampleCountPercentile = alarm.EvaluateLowSampleCountPercentile
	a.EvaluationPeriods = alarm.EvaluationPeriods
	a.ExtendedStatistic = alarm.ExtendedStatistic
	a.InsufficientDataActions = alarm.InsufficientDataActions
	a.MetricName = alarm.MetricName
	a.Metrics = alarm.Metrics
	a.Namespace = alarm.Namespace
	a.OKActions = alarm.OKActions
	a.Period = alarm.Period
	a.StateValue = alarm.StateValue
	a.Statistic = alarm.Statistic
	a.Threshold = alarm.Threshold
	a.ThresholdMetricId = alarm.ThresholdMetricId
	a.TreatMissingData = alarm.TreatMissingData
	a.Unit = alarm.Unit
}

func CloudwatchListAlarms(ctx context.Context) ([]*CloudwatchAlarm, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchListAlarms"}
		d.Start()
		defer d.End()
	}
	var token *string
	var result []*CloudwatchAlarm
	for {
		out, err := CloudwatchClient().DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, alarm := range out.MetricAlarms {
			a := &CloudwatchAlarm{}
			a.FromAlarm(&alarm)
			result = append(result, a)
		}
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return result, nil
}

func CloudwatchListMetrics(ctx context.Context, namespace, metric *string) ([]cwtypes.Metric, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchListMetrics"}
		d.Start()
		defer d.End()
	}
	var token *string
	var metrics []cwtypes.Metric
	for {
		out, err := CloudwatchClient().ListMetrics(ctx, &cloudwatch.ListMetricsInput{
			NextToken:  token,
			Namespace:  namespace,
			MetricName: metric,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		metrics = append(metrics, out.Metrics...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return metrics, nil
}

type cloudwatchMetricDataClient interface {
	GetMetricData(
		context.Context,
		*cloudwatch.GetMetricDataInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.GetMetricDataOutput, error)
}

func CloudwatchGetMetricData(ctx context.Context, period int, stat string, fromTime, toTime *time.Time, namespace string, metrics []string, dimension string) ([]cwtypes.MetricDataResult, error) {
	input, metricIndexes, err := cloudwatchMetricDataInput(period, stat, fromTime, toTime, namespace, metrics, dimension)
	if err != nil {
		return nil, err
	}
	return cloudwatchGetMetricDataPages(ctx, CloudwatchClient(), input, metricIndexes, metrics)
}

func cloudwatchGetMetricData(ctx context.Context, client cloudwatchMetricDataClient, period int, stat string, fromTime, toTime *time.Time, namespace string, metrics []string, dimension string) ([]cwtypes.MetricDataResult, error) {
	input, metricIndexes, err := cloudwatchMetricDataInput(period, stat, fromTime, toTime, namespace, metrics, dimension)
	if err != nil {
		return nil, err
	}
	return cloudwatchGetMetricDataPages(ctx, client, input, metricIndexes, metrics)
}

func cloudwatchMetricDataInput(
	period int,
	stat string,
	fromTime, toTime *time.Time,
	namespace string,
	metrics []string,
	dimension string,
) (*cloudwatch.GetMetricDataInput, map[string]int, error) {
	if period <= 0 || int64(period) > int64(1<<31-1) {
		return nil, nil, fmt.Errorf("CloudWatch metric period must be between 1 and %d, got %d", int64(1<<31-1), period)
	}
	if stat == "" {
		return nil, nil, fmt.Errorf("CloudWatch metric statistic is empty")
	}
	if fromTime == nil || toTime == nil || !fromTime.Before(*toTime) {
		return nil, nil, fmt.Errorf("CloudWatch metric time range must have a start before its end")
	}
	if namespace == "" {
		return nil, nil, fmt.Errorf("CloudWatch metric namespace is empty")
	}
	if len(metrics) == 0 {
		return nil, nil, fmt.Errorf("no CloudWatch metrics requested")
	}
	if len(metrics) > 500 {
		return nil, nil, fmt.Errorf("requested %d CloudWatch metrics; maximum is 500", len(metrics))
	}
	dimensionName, dimensionValue, found := strings.Cut(dimension, "=")
	if !found || dimensionName == "" || dimensionValue == "" {
		return nil, nil, fmt.Errorf("CloudWatch dimension must be NAME=VALUE, got %q", dimension)
	}

	input := &cloudwatch.GetMetricDataInput{
		EndTime:           toTime,
		StartTime:         fromTime,
		MetricDataQueries: make([]cwtypes.MetricDataQuery, 0, len(metrics)),
	}
	metricIndexes := make(map[string]int, len(metrics))
	metricNames := make(map[string]struct{}, len(metrics))
	for index, metric := range metrics {
		if metric == "" {
			return nil, nil, fmt.Errorf("CloudWatch metric %d is empty", index)
		}
		if _, exists := metricNames[metric]; exists {
			return nil, nil, fmt.Errorf("CloudWatch metric %q was requested more than once", metric)
		}
		metricNames[metric] = struct{}{}
		queryID := fmt.Sprintf("m%d", index)
		metricIndexes[queryID] = index
		input.MetricDataQueries = append(input.MetricDataQueries, cwtypes.MetricDataQuery{
			Id: aws.String(queryID),
			MetricStat: &cwtypes.MetricStat{
				Period: aws.Int32(int32(period)),
				Stat:   aws.String(stat),
				Metric: &cwtypes.Metric{
					Namespace:  aws.String(namespace),
					MetricName: aws.String(metric),
					Dimensions: []cwtypes.Dimension{{
						Name:  aws.String(dimensionName),
						Value: aws.String(dimensionValue),
					}},
				},
			},
		})
	}
	return input, metricIndexes, nil
}

func cloudwatchGetMetricDataPages(
	ctx context.Context,
	client cloudwatchMetricDataClient,
	input *cloudwatch.GetMetricDataInput,
	metricIndexes map[string]int,
	metrics []string,
) ([]cwtypes.MetricDataResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchGetMetricData"}
		d.Start()
		defer d.End()
	}
	paginator := cloudwatch.NewGetMetricDataPaginator(client, input, func(options *cloudwatch.GetMetricDataPaginatorOptions) {
		options.StopOnDuplicateToken = true
	})
	completed := make([]bool, len(metrics))
	var results []cwtypes.MetricDataResult
	var previousToken string
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return nil, fmt.Errorf("get CloudWatch metric data: %w", err)
		}
		if len(out.Messages) != 0 {
			return nil, fmt.Errorf("CloudWatch GetMetricData message: %s", cloudwatchMetricMessages(out.Messages))
		}
		nextToken := aws.ToString(out.NextToken)
		if nextToken != "" && nextToken == previousToken {
			return nil, fmt.Errorf("CloudWatch GetMetricData returned duplicate pagination token %q", nextToken)
		}
		hasMore := nextToken != ""
		previousToken = nextToken
		for _, result := range out.MetricDataResults {
			if result.Id == nil || *result.Id == "" {
				return nil, fmt.Errorf("CloudWatch returned a metric result without an ID")
			}
			metricIndex, exists := metricIndexes[*result.Id]
			if !exists {
				return nil, fmt.Errorf("CloudWatch returned unexpected metric result ID %q", *result.Id)
			}
			if len(result.Messages) != 0 {
				return nil, fmt.Errorf(
					"CloudWatch metric %q message: %s",
					metrics[metricIndex],
					cloudwatchMetricMessages(result.Messages),
				)
			}
			switch result.StatusCode {
			case cwtypes.StatusCodeComplete:
				completed[metricIndex] = true
			case cwtypes.StatusCodePartialData:
				completed[metricIndex] = false
				if !hasMore {
					return nil, fmt.Errorf("CloudWatch metric %q ended with status PartialData", metrics[metricIndex])
				}
			default:
				return nil, fmt.Errorf(
					"CloudWatch metric %q returned status %q",
					metrics[metricIndex],
					result.StatusCode,
				)
			}
			results = append(results, result)
		}
	}
	for index, complete := range completed {
		if !complete {
			return nil, fmt.Errorf("CloudWatch metric %q did not return complete data", metrics[index])
		}
	}
	return results, nil
}

func cloudwatchMetricMessages(messages []cwtypes.MessageData) string {
	formatted := make([]string, 0, len(messages))
	for _, message := range messages {
		code := aws.ToString(message.Code)
		value := aws.ToString(message.Value)
		switch {
		case code == "":
			formatted = append(formatted, value)
		case value == "":
			formatted = append(formatted, code)
		default:
			formatted = append(formatted, code+": "+value)
		}
	}
	return strings.Join(formatted, "; ")
}
