package lib

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const (
	lambdaTriggerAlarm               = "alarm"
	lambdaAlarmInvocationPrincipal   = "lambda.alarms.cloudwatch.amazonaws.com"
	lambdaAlarmAttrName              = "name"
	lambdaAlarmAttrDescription       = "description"
	lambdaAlarmAttrNamespace         = "namespace"
	lambdaAlarmAttrMetric            = "metric"
	lambdaAlarmAttrDimension         = "dimension"
	lambdaAlarmAttrStatistic         = "statistic"
	lambdaAlarmAttrPeriod            = "period"
	lambdaAlarmAttrThreshold         = "threshold"
	lambdaAlarmAttrComparison        = "comparison"
	lambdaAlarmAttrEvaluationPeriods = "evaluation-periods"
	lambdaAlarmAttrDatapointsToAlarm = "datapoints-to-alarm"
	lambdaAlarmAttrTreatMissingData  = "missing"
)

var lambdaAlarmNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)

type lambdaAlarmConfig struct {
	name              string
	description       string
	namespace         string
	metric            string
	dimensions        []cwtypes.Dimension
	statistic         cwtypes.Statistic
	period            int32
	threshold         float64
	comparison        cwtypes.ComparisonOperator
	evaluationPeriods int32
	datapointsToAlarm int32
	treatMissingData  string
}

func parsePositiveInt32(key, value string) (int32, error) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("lambda alarm %s must be a positive integer: %q", key, value)
	}
	return int32(parsed), nil
}

func parseLambdaAlarmTrigger(trigger *InfraTrigger) (*lambdaAlarmConfig, error) {
	if trigger == nil || trigger.Type != lambdaTriggerAlarm {
		return nil, errors.New("invalid Lambda alarm trigger")
	}
	values := map[string]string{}
	var dimensions []cwtypes.Dimension
	for _, attr := range trigger.Attr {
		key, value, err := SplitOnce(attr, "=")
		if err != nil || key == "" || value == "" {
			return nil, fmt.Errorf("invalid Lambda alarm attribute %q", attr)
		}
		if key == lambdaAlarmAttrDimension {
			name, dimensionValue, err := SplitOnce(value, "=")
			if err != nil || name == "" || dimensionValue == "" {
				return nil, fmt.Errorf("invalid Lambda alarm dimension %q", value)
			}
			for _, dimension := range dimensions {
				if aws.ToString(dimension.Name) == name {
					return nil, fmt.Errorf("duplicate Lambda alarm dimension %q", name)
				}
			}
			dimensions = append(dimensions, cwtypes.Dimension{Name: aws.String(name), Value: aws.String(dimensionValue)})
			continue
		}
		if _, found := values[key]; found {
			return nil, fmt.Errorf("duplicate Lambda alarm attribute %q", key)
		}
		values[key] = value
	}
	allowed := []string{
		lambdaAlarmAttrName,
		lambdaAlarmAttrDescription,
		lambdaAlarmAttrNamespace,
		lambdaAlarmAttrMetric,
		lambdaAlarmAttrStatistic,
		lambdaAlarmAttrPeriod,
		lambdaAlarmAttrThreshold,
		lambdaAlarmAttrComparison,
		lambdaAlarmAttrEvaluationPeriods,
		lambdaAlarmAttrDatapointsToAlarm,
		lambdaAlarmAttrTreatMissingData,
	}
	for key := range values {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("unknown Lambda alarm attribute %q", key)
		}
	}
	for _, key := range []string{
		lambdaAlarmAttrName,
		lambdaAlarmAttrNamespace,
		lambdaAlarmAttrMetric,
		lambdaAlarmAttrStatistic,
		lambdaAlarmAttrPeriod,
		lambdaAlarmAttrThreshold,
		lambdaAlarmAttrComparison,
		lambdaAlarmAttrEvaluationPeriods,
		lambdaAlarmAttrDatapointsToAlarm,
		lambdaAlarmAttrTreatMissingData,
	} {
		if values[key] == "" {
			return nil, fmt.Errorf("lambda alarm is missing %q", key)
		}
	}
	if !lambdaAlarmNamePattern.MatchString(values[lambdaAlarmAttrName]) {
		return nil, fmt.Errorf("invalid Lambda alarm name %q", values[lambdaAlarmAttrName])
	}
	if len(dimensions) == 0 {
		return nil, errors.New("lambda alarm requires at least one dimension")
	}
	statistic := cwtypes.Statistic(values[lambdaAlarmAttrStatistic])
	if !slices.Contains(statistic.Values(), statistic) {
		return nil, fmt.Errorf("invalid Lambda alarm statistic %q", statistic)
	}
	comparison := cwtypes.ComparisonOperator(values[lambdaAlarmAttrComparison])
	if !slices.Contains(comparison.Values(), comparison) {
		return nil, fmt.Errorf("invalid Lambda alarm comparison %q", comparison)
	}
	period, err := parsePositiveInt32(lambdaAlarmAttrPeriod, values[lambdaAlarmAttrPeriod])
	if err != nil {
		return nil, err
	}
	if period < 60 || period%60 != 0 {
		return nil, fmt.Errorf("lambda alarm period must be a whole number of minutes: %d", period)
	}
	threshold, err := strconv.ParseFloat(values[lambdaAlarmAttrThreshold], 64)
	if err != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return nil, fmt.Errorf("invalid Lambda alarm threshold %q", values[lambdaAlarmAttrThreshold])
	}
	evaluationPeriods, err := parsePositiveInt32(lambdaAlarmAttrEvaluationPeriods, values[lambdaAlarmAttrEvaluationPeriods])
	if err != nil {
		return nil, err
	}
	datapointsToAlarm, err := parsePositiveInt32(lambdaAlarmAttrDatapointsToAlarm, values[lambdaAlarmAttrDatapointsToAlarm])
	if err != nil {
		return nil, err
	}
	if datapointsToAlarm > evaluationPeriods {
		return nil, errors.New("lambda alarm datapoints-to-alarm exceeds evaluation-periods")
	}
	missing := values[lambdaAlarmAttrTreatMissingData]
	if !slices.Contains([]string{"breaching", "notBreaching", "ignore", "missing"}, missing) {
		return nil, fmt.Errorf("invalid Lambda alarm missing-data policy %q", missing)
	}
	sort.Slice(dimensions, func(i, j int) bool { return aws.ToString(dimensions[i].Name) < aws.ToString(dimensions[j].Name) })
	return &lambdaAlarmConfig{
		name:              values[lambdaAlarmAttrName],
		description:       values[lambdaAlarmAttrDescription],
		namespace:         values[lambdaAlarmAttrNamespace],
		metric:            values[lambdaAlarmAttrMetric],
		dimensions:        dimensions,
		statistic:         statistic,
		period:            period,
		threshold:         threshold,
		comparison:        comparison,
		evaluationPeriods: evaluationPeriods,
		datapointsToAlarm: datapointsToAlarm,
		treatMissingData:  missing,
	}, nil
}

func (config *lambdaAlarmConfig) metricAlarm(targetARN string) *cwtypes.MetricAlarm {
	return &cwtypes.MetricAlarm{
		ActionsEnabled:     aws.Bool(true),
		AlarmActions:       []string{targetARN},
		AlarmDescription:   aws.String(config.description),
		AlarmName:          aws.String(config.name),
		ComparisonOperator: config.comparison,
		DatapointsToAlarm:  aws.Int32(config.datapointsToAlarm),
		Dimensions:         slices.Clone(config.dimensions),
		EvaluationPeriods:  aws.Int32(config.evaluationPeriods),
		MetricName:         aws.String(config.metric),
		Namespace:          aws.String(config.namespace),
		Period:             aws.Int32(config.period),
		Statistic:          config.statistic,
		Threshold:          aws.Float64(config.threshold),
		TreatMissingData:   aws.String(config.treatMissingData),
	}
}

func (config *lambdaAlarmConfig) putInput(targetARN, infraSetName string, includeTags bool) *cloudwatch.PutMetricAlarmInput {
	alarm := config.metricAlarm(targetARN)
	input := &cloudwatch.PutMetricAlarmInput{
		ActionsEnabled:     alarm.ActionsEnabled,
		AlarmActions:       alarm.AlarmActions,
		AlarmDescription:   alarm.AlarmDescription,
		AlarmName:          alarm.AlarmName,
		ComparisonOperator: alarm.ComparisonOperator,
		DatapointsToAlarm:  alarm.DatapointsToAlarm,
		Dimensions:         alarm.Dimensions,
		EvaluationPeriods:  alarm.EvaluationPeriods,
		MetricName:         alarm.MetricName,
		Namespace:          alarm.Namespace,
		Period:             alarm.Period,
		Statistic:          alarm.Statistic,
		Threshold:          alarm.Threshold,
		TreatMissingData:   alarm.TreatMissingData,
	}
	if includeTags {
		input.Tags = []cwtypes.Tag{{Key: aws.String(infraSetTagName), Value: aws.String(infraSetName)}}
	}
	return input
}

func normalizedAlarmDimensions(dimensions []cwtypes.Dimension) []cwtypes.Dimension {
	result := slices.Clone(dimensions)
	sort.Slice(result, func(i, j int) bool {
		return aws.ToString(result[i].Name) < aws.ToString(result[j].Name)
	})
	return result
}

func lambdaAlarmMatches(actual, expected *cwtypes.MetricAlarm) bool {
	if actual == nil || expected == nil {
		return false
	}
	return aws.ToBool(actual.ActionsEnabled) == aws.ToBool(expected.ActionsEnabled) &&
		reflect.DeepEqual(actual.AlarmActions, expected.AlarmActions) &&
		aws.ToString(actual.AlarmDescription) == aws.ToString(expected.AlarmDescription) &&
		aws.ToString(actual.AlarmName) == aws.ToString(expected.AlarmName) &&
		actual.ComparisonOperator == expected.ComparisonOperator &&
		aws.ToInt32(actual.DatapointsToAlarm) == aws.ToInt32(expected.DatapointsToAlarm) &&
		reflect.DeepEqual(normalizedAlarmDimensions(actual.Dimensions), normalizedAlarmDimensions(expected.Dimensions)) &&
		aws.ToInt32(actual.EvaluationPeriods) == aws.ToInt32(expected.EvaluationPeriods) &&
		aws.ToString(actual.MetricName) == aws.ToString(expected.MetricName) &&
		aws.ToString(actual.Namespace) == aws.ToString(expected.Namespace) &&
		aws.ToInt32(actual.Period) == aws.ToInt32(expected.Period) &&
		actual.Statistic == expected.Statistic &&
		aws.ToFloat64(actual.Threshold) == aws.ToFloat64(expected.Threshold) &&
		aws.ToString(actual.TreatMissingData) == aws.ToString(expected.TreatMissingData) &&
		len(actual.OKActions) == 0 && len(actual.InsufficientDataActions) == 0 &&
		actual.EvaluateLowSampleCountPercentile == nil && actual.ExtendedStatistic == nil &&
		len(actual.Metrics) == 0 && actual.ThresholdMetricId == nil && actual.Unit == ""
}

func cloudwatchAlarmARN(region, account, name string) string {
	return fmt.Sprintf("arn:aws:cloudwatch:%s:%s:alarm:%s", region, account, name)
}

func cloudwatchDescribeMetricAlarms(ctx context.Context, client *cloudwatch.Client, names []string) ([]cwtypes.MetricAlarm, error) {
	var token *string
	var alarms []cwtypes.MetricAlarm
	for {
		out, err := client.DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{AlarmNames: names, NextToken: token})
		if err != nil {
			return nil, err
		}
		alarms = append(alarms, out.MetricAlarms...)
		if out.NextToken == nil {
			return alarms, nil
		}
		token = out.NextToken
	}
}

func cloudwatchAlarmInfraSet(ctx context.Context, client *cloudwatch.Client, alarmARN string) (string, error) {
	out, err := client.ListTagsForResource(ctx, &cloudwatch.ListTagsForResourceInput{ResourceARN: aws.String(alarmARN)})
	if err != nil {
		return "", err
	}
	for _, tag := range out.Tags {
		if aws.ToString(tag.Key) == infraSetTagName {
			return aws.ToString(tag.Value), nil
		}
	}
	return "", nil
}

func LambdaEnsureTriggerAlarm(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	var configs []*lambdaAlarmConfig
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type != lambdaTriggerAlarm {
			continue
		}
		config, err := parseLambdaAlarmTrigger(trigger)
		if err != nil {
			return nil, err
		}
		for _, existing := range configs {
			if existing.name == config.name {
				return nil, fmt.Errorf("duplicate Lambda alarm name %q", config.name)
			}
		}
		configs = append(configs, config)
	}
	client := CloudwatchClient()
	account, err := StsAccount(ctx)
	if err != nil {
		return nil, err
	}
	region := Region()
	permissionSIDs := make([]string, 0, len(configs))
	configuredNames := make(map[string]bool, len(configs))
	for _, config := range configs {
		configuredNames[config.name] = true
		alarmARN := cloudwatchAlarmARN(region, account, config.name)
		sid, err := lambdaEnsurePermissionWithSourceAccount(
			ctx,
			infraLambda.Name,
			lambdaAlarmInvocationPrincipal,
			alarmARN,
			account,
			preview,
		)
		if err != nil {
			return nil, err
		}
		permissionSIDs = append(permissionSIDs, sid)
		alarms, err := cloudwatchDescribeMetricAlarms(ctx, client, []string{config.name})
		if err != nil {
			return nil, err
		}
		if len(alarms) > 1 {
			return nil, fmt.Errorf("CloudWatch returned duplicate alarm name %q", config.name)
		}
		create := len(alarms) == 0
		desired := config.metricAlarm(infraLambda.Arn)
		if create || !lambdaAlarmMatches(&alarms[0], desired) {
			if !preview {
				if _, err := client.PutMetricAlarm(ctx, config.putInput(infraLambda.Arn, infraLambda.infraSetName, create)); err != nil {
					return nil, err
				}
			}
			verb := "updated"
			if create {
				verb = "created"
			}
			Logger.Println(PreviewString(preview)+verb+" CloudWatch metric alarm:", config.name)
		}
		if !create {
			infraSetName, err := cloudwatchAlarmInfraSet(ctx, client, alarmARN)
			if err != nil {
				return nil, err
			}
			if infraSetName != infraLambda.infraSetName {
				if !preview {
					_, err := client.TagResource(ctx, &cloudwatch.TagResourceInput{
						ResourceARN: aws.String(alarmARN),
						Tags:        []cwtypes.Tag{{Key: aws.String(infraSetTagName), Value: aws.String(infraLambda.infraSetName)}},
					})
					if err != nil {
						return nil, err
					}
				}
				Logger.Println(PreviewString(preview)+"updated CloudWatch metric alarm infrastructure tag:", config.name)
			}
		}
	}
	alarms, err := cloudwatchDescribeMetricAlarms(ctx, client, nil)
	if err != nil {
		return nil, err
	}
	for _, alarm := range alarms {
		if configuredNames[aws.ToString(alarm.AlarmName)] || !slices.Contains(alarm.AlarmActions, infraLambda.Arn) || alarm.AlarmArn == nil {
			continue
		}
		infraSetName, err := cloudwatchAlarmInfraSet(ctx, client, aws.ToString(alarm.AlarmArn))
		if err != nil {
			return nil, err
		}
		if infraSetName != infraLambda.infraSetName {
			continue
		}
		if !preview {
			if _, err := client.DeleteAlarms(ctx, &cloudwatch.DeleteAlarmsInput{AlarmNames: []string{aws.ToString(alarm.AlarmName)}}); err != nil {
				return nil, err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted CloudWatch metric alarm:", aws.ToString(alarm.AlarmName))
	}
	return permissionSIDs, nil
}

func lambdaAlarmTriggerRepresentable(alarm *cwtypes.MetricAlarm) bool {
	return alarm != nil &&
		len(alarm.AlarmActions) == 1 && strings.HasPrefix(alarm.AlarmActions[0], "arn:aws:lambda:") &&
		alarm.MetricName != nil && alarm.Namespace != nil && alarm.Period != nil && alarm.Threshold != nil &&
		alarm.EvaluationPeriods != nil && alarm.DatapointsToAlarm != nil && alarm.TreatMissingData != nil &&
		alarm.AlarmName != nil && alarm.ActionsEnabled != nil && *alarm.ActionsEnabled && len(alarm.Dimensions) > 0 &&
		len(alarm.OKActions) == 0 && len(alarm.InsufficientDataActions) == 0 && len(alarm.Metrics) == 0 &&
		alarm.EvaluateLowSampleCountPercentile == nil && alarm.ExtendedStatistic == nil &&
		alarm.ThresholdMetricId == nil && alarm.Unit == ""
}

func lambdaAlarmTriggers(ctx context.Context) ([]*InfraTrigger, error) {
	alarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), nil)
	if err != nil {
		return nil, err
	}
	var triggers []*InfraTrigger
	for _, alarm := range alarms {
		if !lambdaAlarmTriggerRepresentable(&alarm) {
			continue
		}
		attrs := []string{
			lambdaAlarmAttrName + "=" + aws.ToString(alarm.AlarmName),
			lambdaAlarmAttrNamespace + "=" + aws.ToString(alarm.Namespace),
			lambdaAlarmAttrMetric + "=" + aws.ToString(alarm.MetricName),
		}
		if aws.ToString(alarm.AlarmDescription) != "" {
			attrs = append(attrs, lambdaAlarmAttrDescription+"="+aws.ToString(alarm.AlarmDescription))
		}
		for _, dimension := range normalizedAlarmDimensions(alarm.Dimensions) {
			attrs = append(attrs, lambdaAlarmAttrDimension+"="+aws.ToString(dimension.Name)+"="+aws.ToString(dimension.Value))
		}
		attrs = append(attrs,
			lambdaAlarmAttrStatistic+"="+string(alarm.Statistic),
			lambdaAlarmAttrPeriod+"="+strconv.FormatInt(int64(aws.ToInt32(alarm.Period)), 10),
			lambdaAlarmAttrThreshold+"="+strconv.FormatFloat(aws.ToFloat64(alarm.Threshold), 'g', -1, 64),
			lambdaAlarmAttrComparison+"="+string(alarm.ComparisonOperator),
			lambdaAlarmAttrEvaluationPeriods+"="+strconv.FormatInt(int64(aws.ToInt32(alarm.EvaluationPeriods)), 10),
			lambdaAlarmAttrDatapointsToAlarm+"="+strconv.FormatInt(int64(aws.ToInt32(alarm.DatapointsToAlarm)), 10),
			lambdaAlarmAttrTreatMissingData+"="+aws.ToString(alarm.TreatMissingData),
		)
		trigger := &InfraTrigger{
			lambdaName: LambdaArnToLambdaName(alarm.AlarmActions[0]),
			Type:       lambdaTriggerAlarm,
			Attr:       attrs,
		}
		if _, err := parseLambdaAlarmTrigger(trigger); err == nil {
			triggers = append(triggers, trigger)
		}
	}
	return triggers, nil
}
