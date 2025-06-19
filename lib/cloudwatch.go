package lib

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/gofrs/uuid"
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

func CloudwatchEnsureAlarm(ctx context.Context, name string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchEnsureAlarm"}
		d.Start()
		defer d.End()
	}
	out, err := CloudwatchClient().DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{
		AlarmNames: []string{
			name,
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_ = out
	_, _ = CloudwatchClient().PutMetricAlarm(ctx, &cloudwatch.PutMetricAlarmInput{})
	return nil
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

func CloudwatchGetMetricData(ctx context.Context, period int, stat string, fromTime, toTime *time.Time, namespace string, metrics []string, dimension string) ([]cwtypes.MetricDataResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CloudwatchGetMetricData"}
		d.Start()
		defer d.End()
	}
	var token *string
	var result []cwtypes.MetricDataResult
	for {
		input := &cloudwatch.GetMetricDataInput{
			EndTime:           toTime,
			StartTime:         fromTime,
			NextToken:         token,
			MetricDataQueries: []cwtypes.MetricDataQuery{},
		}
		for _, metric := range metrics {
			input.MetricDataQueries = append(input.MetricDataQueries, cwtypes.MetricDataQuery{
				Id: aws.String("a" + strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")),
				MetricStat: &cwtypes.MetricStat{
					Period: aws.Int32(int32(period)),
					Stat:   aws.String(stat),
					Metric: &cwtypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(metric),
						Dimensions: []cwtypes.Dimension{{
							Name:  aws.String(strings.Split(dimension, "=")[0]),
							Value: aws.String(strings.Split(dimension, "=")[1]),
						}},
					},
				},
			})
		}
		out, err := CloudwatchClient().GetMetricData(ctx, input)
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
