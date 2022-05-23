package lib

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/gofrs/uuid"
)

var cloudwatchClient *cloudwatch.CloudWatch
var cloudwatchClientLock sync.RWMutex

func CloudwatchClientExplicit(accessKeyID, accessKeySecret, region string) *cloudwatch.CloudWatch {
	return cloudwatch.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func CloudwatchClient() *cloudwatch.CloudWatch {
	cloudwatchClientLock.Lock()
	defer cloudwatchClientLock.Unlock()
	if cloudwatchClient == nil {
		cloudwatchClient = cloudwatch.New(Session())
	}
	return cloudwatchClient
}

type CloudwatchAlarm struct {
	alarmArn              *string
	stateReason           *string
	stateReasonData       *string
	stateUpdatedTimestamp *time.Time

	ActionsEnabled                     *bool                         `json:",omitempty"`
	AlarmActions                       []*string                     `json:",omitempty"`
	AlarmConfigurationUpdatedTimestamp *time.Time                    `json:",omitempty"`
	AlarmName                          *string                       `json:",omitempty"`
	ComparisonOperator                 *string                       `json:",omitempty"`
	DatapointsToAlarm                  *int64                        `json:",omitempty"`
	Dimensions                         []*cloudwatch.Dimension       `json:",omitempty"`
	EvaluateLowSampleCountPercentile   *string                       `json:",omitempty"`
	EvaluationPeriods                  *int64                        `json:",omitempty"`
	ExtendedStatistic                  *string                       `json:",omitempty"`
	InsufficientDataActions            []*string                     `json:",omitempty"`
	MetricName                         *string                       `json:",omitempty"`
	Metrics                            []*cloudwatch.MetricDataQuery `json:",omitempty"`
	Namespace                          *string                       `json:",omitempty"`
	OKActions                          []*string                     `json:",omitempty"`
	Period                             *int64                        `json:",omitempty"`
	StateValue                         *string                       `json:",omitempty"`
	Statistic                          *string                       `json:",omitempty"`
	Threshold                          *float64                      `json:",omitempty"`
	ThresholdMetricId                  *string                       `json:",omitempty"`
	TreatMissingData                   *string                       `json:",omitempty"`
	Unit                               *string                       `json:",omitempty"`
}

func (a *CloudwatchAlarm) FromAlarm(alarm *cloudwatch.MetricAlarm) {
	a.alarmArn = alarm.AlarmArn
	a.stateReason = alarm.StateReason
	a.stateReasonData = alarm.StateReasonData
	a.stateUpdatedTimestamp = alarm.StateUpdatedTimestamp
	a.ActionsEnabled = alarm.ActionsEnabled
	a.AlarmActions = alarm.AlarmActions
	a.AlarmConfigurationUpdatedTimestamp = alarm.AlarmConfigurationUpdatedTimestamp
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
		defer d.Log()
	}
	var token *string
	var result []*CloudwatchAlarm
	for {
		out, err := CloudwatchClient().DescribeAlarmsWithContext(ctx, &cloudwatch.DescribeAlarmsInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, alarm := range out.MetricAlarms {
			a := &CloudwatchAlarm{}
			a.FromAlarm(alarm)
			result = append(result, a)
		}
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return result, nil
}

func CloudwatchEnsureAlarm(ctx context.Context, name string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchEnsureAlarm"}
		defer d.Log()
	}
	out, err := CloudwatchClient().DescribeAlarmsWithContext(ctx, &cloudwatch.DescribeAlarmsInput{
		AlarmNames: []*string{
			aws.String(name),
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_ = out
	_, _ = CloudwatchClient().PutMetricAlarmWithContext(ctx, &cloudwatch.PutMetricAlarmInput{})
	return nil
}

func CloudwatchListMetrics(ctx context.Context, namespace, metric *string) ([]*cloudwatch.Metric, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchListMetrics"}
		defer d.Log()
	}
	var token *string
	var metrics []*cloudwatch.Metric
	for {
		out, err := CloudwatchClient().ListMetricsWithContext(ctx, &cloudwatch.ListMetricsInput{
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

func CloudwatchGetMetricData(ctx context.Context, period int, stat string, fromTime, toTime *time.Time, namespace string, metrics []string, dimension string) ([]*cloudwatch.MetricDataResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchGetMetricData"}
		defer d.Log()
	}
	var token *string
	var result []*cloudwatch.MetricDataResult
	for {
		input := &cloudwatch.GetMetricDataInput{
			EndTime:           toTime,
			StartTime:         fromTime,
			NextToken:         token,
			MetricDataQueries: []*cloudwatch.MetricDataQuery{},
		}
		for _, metric := range metrics {
			input.MetricDataQueries = append(input.MetricDataQueries, &cloudwatch.MetricDataQuery{
				Id: aws.String("a" + strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")),
				MetricStat: &cloudwatch.MetricStat{
					Period: aws.Int64(int64(period)),
					Stat:   aws.String(stat),
					Metric: &cloudwatch.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(metric),
						Dimensions: []*cloudwatch.Dimension{{
							Name:  aws.String(strings.Split(dimension, "=")[0]),
							Value: aws.String(strings.Split(dimension, "=")[1]),
						}},
					},
				},
			})
		}
		out, err := CloudwatchClient().GetMetricDataWithContext(ctx, input)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		result = append(result, out.MetricDataResults...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return result, nil
}
