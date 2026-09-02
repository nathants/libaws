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
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const (
	lambdaTriggerAlarm             = "alarm"
	lambdaAlarmInvocationPrincipal = "lambda.alarms.cloudwatch.amazonaws.com"
	lambdaAlarmAttrName            = "name"
	lambdaAlarmAttrInvocations     = "lambda-invocations"
	lambdaAlarmAttrAtLeast         = "at-least"
	lambdaAlarmRateUnit            = "/minute"

	lambdaAlarmNamespace         = "AWS/Lambda"
	lambdaAlarmMetric            = "Invocations"
	lambdaAlarmDimension         = "FunctionName"
	lambdaAlarmPeriod            = int32(60)
	lambdaAlarmEvaluationPeriods = int32(1)
	lambdaAlarmDatapointsToAlarm = int32(1)
	lambdaAlarmMissingData       = "notBreaching"
)

var lambdaAlarmNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
var lambdaAlarmRatePattern = regexp.MustCompile(`^[1-9][0-9]*$`)

type lambdaAlarmConfig struct {
	name                 string
	invocationLambdaName string
	invocationsPerMinute int32
}

func parseLambdaAlarmTrigger(trigger *InfraTrigger) (*lambdaAlarmConfig, error) {
	if trigger == nil || trigger.Type != lambdaTriggerAlarm {
		return nil, errors.New("invalid Lambda alarm trigger")
	}
	values := map[string]string{}
	for _, attr := range trigger.Attr {
		key, value, err := SplitOnce(attr, "=")
		if err != nil || key == "" || value == "" {
			return nil, fmt.Errorf("invalid Lambda alarm attribute %q", attr)
		}
		if _, found := values[key]; found {
			return nil, fmt.Errorf("duplicate Lambda alarm attribute %q", key)
		}
		switch key {
		case lambdaAlarmAttrName, lambdaAlarmAttrInvocations, lambdaAlarmAttrAtLeast:
			values[key] = value
		default:
			return nil, fmt.Errorf("unknown Lambda alarm attribute %q", key)
		}
	}
	for _, key := range []string{lambdaAlarmAttrName, lambdaAlarmAttrInvocations, lambdaAlarmAttrAtLeast} {
		if values[key] == "" {
			return nil, fmt.Errorf("lambda alarm is missing %q", key)
		}
	}
	name := values[lambdaAlarmAttrName]
	if !lambdaAlarmNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid Lambda alarm name %q", name)
	}
	invocationLambdaName := values[lambdaAlarmAttrInvocations]
	if err := validateLambdaName(invocationLambdaName); err != nil {
		return nil, fmt.Errorf("invalid Lambda alarm %s %q: %w", lambdaAlarmAttrInvocations, invocationLambdaName, err)
	}
	rate := values[lambdaAlarmAttrAtLeast]
	count, hasUnit := strings.CutSuffix(rate, lambdaAlarmRateUnit)
	if !hasUnit || !lambdaAlarmRatePattern.MatchString(count) {
		return nil, fmt.Errorf("lambda alarm %s must be a positive integer followed by %q: %q", lambdaAlarmAttrAtLeast, lambdaAlarmRateUnit, rate)
	}
	parsed, err := strconv.ParseInt(count, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("lambda alarm %s is too large: %q", lambdaAlarmAttrAtLeast, rate)
	}
	return &lambdaAlarmConfig{
		name:                 name,
		invocationLambdaName: invocationLambdaName,
		invocationsPerMinute: int32(parsed),
	}, nil
}

func (config *lambdaAlarmConfig) metricAlarm(targetARN string) *cwtypes.MetricAlarm {
	return &cwtypes.MetricAlarm{
		ActionsEnabled:     aws.Bool(true),
		AlarmActions:       []string{targetARN},
		AlarmDescription:   aws.String(""),
		AlarmName:          aws.String(config.name),
		ComparisonOperator: cwtypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
		DatapointsToAlarm:  aws.Int32(lambdaAlarmDatapointsToAlarm),
		Dimensions: []cwtypes.Dimension{{
			Name:  aws.String(lambdaAlarmDimension),
			Value: aws.String(config.invocationLambdaName),
		}},
		EvaluationPeriods: aws.Int32(lambdaAlarmEvaluationPeriods),
		MetricName:        aws.String(lambdaAlarmMetric),
		Namespace:         aws.String(lambdaAlarmNamespace),
		Period:            aws.Int32(lambdaAlarmPeriod),
		Statistic:         cwtypes.StatisticSum,
		Threshold:         aws.Float64(float64(config.invocationsPerMinute)),
		TreatMissingData:  aws.String(lambdaAlarmMissingData),
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

func lambdaAlarmHasUnsupportedConfiguration(alarm *cwtypes.MetricAlarm) bool {
	return alarm == nil || len(alarm.OKActions) != 0 || len(alarm.InsufficientDataActions) != 0 ||
		alarm.EvaluateLowSampleCountPercentile != nil || alarm.EvaluationCriteria != nil ||
		alarm.EvaluationInterval != nil || alarm.EvaluationWindow != nil || alarm.ExtendedStatistic != nil ||
		len(alarm.Metrics) != 0 || alarm.ThresholdMetricId != nil || alarm.Unit != ""
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
		!lambdaAlarmHasUnsupportedConfiguration(actual)
}

func cloudwatchAlarmARN(partition, region, account, name string) string {
	return fmt.Sprintf("arn:%s:cloudwatch:%s:%s:alarm:%s", partition, region, account, name)
}

type cloudwatchAlarmClient interface {
	DescribeAlarms(context.Context, *cloudwatch.DescribeAlarmsInput, ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error)
	ListTagsForResource(context.Context, *cloudwatch.ListTagsForResourceInput, ...func(*cloudwatch.Options)) (*cloudwatch.ListTagsForResourceOutput, error)
	PutMetricAlarm(context.Context, *cloudwatch.PutMetricAlarmInput, ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricAlarmOutput, error)
	TagResource(context.Context, *cloudwatch.TagResourceInput, ...func(*cloudwatch.Options)) (*cloudwatch.TagResourceOutput, error)
	DeleteAlarms(context.Context, *cloudwatch.DeleteAlarmsInput, ...func(*cloudwatch.Options)) (*cloudwatch.DeleteAlarmsOutput, error)
}

func cloudwatchDescribeMetricAlarms(ctx context.Context, client cloudwatchAlarmClient, names []string) ([]cwtypes.MetricAlarm, error) {
	var token *string
	var alarms []cwtypes.MetricAlarm
	for {
		out, err := client.DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{AlarmNames: names, NextToken: token})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("CloudWatch DescribeAlarms returned nil output")
		}
		alarms = append(alarms, out.MetricAlarms...)
		if out.NextToken == nil {
			return alarms, nil
		}
		token = out.NextToken
	}
}

func cloudwatchAlarmInfraSet(ctx context.Context, client cloudwatchAlarmClient, alarmARN string) (string, error) {
	out, err := client.ListTagsForResource(ctx, &cloudwatch.ListTagsForResourceInput{ResourceARN: aws.String(alarmARN)})
	if err != nil {
		return "", err
	}
	if out == nil {
		return "", errors.New("CloudWatch ListTagsForResource returned nil output")
	}
	for _, tag := range out.Tags {
		if aws.ToString(tag.Key) == infraSetTagName {
			return aws.ToString(tag.Value), nil
		}
	}
	return "", nil
}

func lambdaCleanupAlarmTriggers(
	ctx context.Context,
	client cloudwatchAlarmClient,
	identity lambdaIdentity,
	desiredNames map[string]bool,
	preview bool,
) error {
	alarms, err := cloudwatchDescribeMetricAlarms(ctx, client, nil)
	if err != nil {
		return err
	}
	for _, alarm := range alarms {
		name := aws.ToString(alarm.AlarmName)
		if desiredNames[name] || alarm.AlarmArn == nil || len(alarm.AlarmActions) != 1 || alarm.AlarmActions[0] != identity.arn {
			continue
		}
		infraSetName, err := cloudwatchAlarmInfraSet(ctx, client, aws.ToString(alarm.AlarmArn))
		if err != nil {
			return err
		}
		if infraSetName == "" || (identity.infraSetName != "" && infraSetName != identity.infraSetName) {
			continue
		}
		if !preview {
			if _, err := client.DeleteAlarms(ctx, &cloudwatch.DeleteAlarmsInput{AlarmNames: []string{name}}); err != nil {
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted CloudWatch metric alarm:", name)
	}
	return nil
}

type lambdaAlarmAccountResolver func(context.Context) (string, error)
type lambdaAlarmPermissionEnsurer func(context.Context, string, string, string, string, bool) (string, error)

func LambdaEnsureTriggerAlarm(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	return lambdaEnsureTriggerAlarm(
		ctx,
		CloudwatchClient(),
		StsAccount,
		Region(),
		lambdaEnsurePermissionWithSourceAccount,
		infraLambda,
		preview,
	)
}

func lambdaEnsureTriggerAlarm(
	ctx context.Context,
	client cloudwatchAlarmClient,
	resolveAccount lambdaAlarmAccountResolver,
	region string,
	ensurePermission lambdaAlarmPermissionEnsurer,
	infraLambda *InfraLambda,
	preview bool,
) ([]string, error) {
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
	account, err := resolveAccount(ctx)
	if err != nil {
		return nil, err
	}
	functionARN, err := awsarn.Parse(infraLambda.Arn)
	if err != nil || functionARN.AccountID != account ||
		!lambdaFunctionARNMatches(infraLambda.Arn, region, infraLambda.Name) {
		return nil, fmt.Errorf("invalid Lambda identity ARN %q", infraLambda.Arn)
	}
	permissionSIDs := make([]string, 0, len(configs))
	configuredNames := make(map[string]bool, len(configs))
	for _, config := range configs {
		configuredNames[config.name] = true
		alarmARN := cloudwatchAlarmARN(functionARN.Partition, region, account, config.name)
		sid, err := ensurePermission(
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
	identity := lambdaIdentity{
		name:         infraLambda.Name,
		arn:          infraLambda.Arn,
		infraSetName: infraLambda.infraSetName,
		exists:       true,
	}
	if err := lambdaCleanupAlarmTriggers(ctx, client, identity, configuredNames, preview); err != nil {
		return nil, err
	}
	return permissionSIDs, nil
}

func lambdaAlarmFunctionName(actionARN string) (string, bool) {
	parsed, err := awsarn.Parse(actionARN)
	if err != nil || parsed.Service != "lambda" || parsed.Region == "" || parsed.AccountID == "" {
		return "", false
	}
	resource := strings.Split(parsed.Resource, ":")
	if len(resource) != 2 || resource[0] != "function" || resource[1] == "" {
		return "", false
	}
	return resource[1], true
}

func lambdaAlarmActionFunctionName(alarm *cwtypes.MetricAlarm) (string, bool) {
	if alarm == nil || alarm.AlarmArn == nil || len(alarm.AlarmActions) != 1 {
		return "", false
	}
	name, exactFunctionARN := lambdaAlarmFunctionName(alarm.AlarmActions[0])
	actionARN, actionErr := awsarn.Parse(alarm.AlarmActions[0])
	alarmARN, alarmErr := awsarn.Parse(aws.ToString(alarm.AlarmArn))
	if !exactFunctionARN || actionErr != nil || alarmErr != nil || alarmARN.Service != "cloudwatch" ||
		actionARN.Partition != alarmARN.Partition || actionARN.Region != alarmARN.Region || actionARN.AccountID != alarmARN.AccountID {
		return "", false
	}
	return name, true
}

func lambdaAlarmConfigFromMetricAlarm(alarm *cwtypes.MetricAlarm) (*lambdaAlarmConfig, bool) {
	if alarm == nil || len(alarm.Dimensions) != 1 {
		return nil, false
	}
	_, exactFunctionARN := lambdaAlarmActionFunctionName(alarm)
	if !exactFunctionARN {
		return nil, false
	}
	dimension := alarm.Dimensions[0]
	invocationLambdaName := aws.ToString(dimension.Value)
	threshold := aws.ToFloat64(alarm.Threshold)
	if aws.ToString(dimension.Name) != lambdaAlarmDimension || validateLambdaName(invocationLambdaName) != nil ||
		threshold < 1 || threshold > float64(int64(1<<31-1)) || math.Trunc(threshold) != threshold {
		return nil, false
	}
	config := &lambdaAlarmConfig{
		name:                 aws.ToString(alarm.AlarmName),
		invocationLambdaName: invocationLambdaName,
		invocationsPerMinute: int32(threshold),
	}
	if !lambdaAlarmNamePattern.MatchString(config.name) || !lambdaAlarmMatches(alarm, config.metricAlarm(alarm.AlarmActions[0])) {
		return nil, false
	}
	return config, true
}

func lambdaAlarmTriggerRepresentable(alarm *cwtypes.MetricAlarm) bool {
	_, representable := lambdaAlarmConfigFromMetricAlarm(alarm)
	return representable
}

func lambdaAlarmTriggers(ctx context.Context) ([]*InfraTrigger, error) {
	alarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), nil)
	if err != nil {
		return nil, err
	}
	var triggers []*InfraTrigger
	for _, alarm := range alarms {
		config, representable := lambdaAlarmConfigFromMetricAlarm(&alarm)
		if !representable {
			continue
		}
		lambdaName, _ := lambdaAlarmActionFunctionName(&alarm)
		triggers = append(triggers, &InfraTrigger{
			lambdaName: lambdaName,
			Type:       lambdaTriggerAlarm,
			Attr: []string{
				lambdaAlarmAttrName + "=" + config.name,
				lambdaAlarmAttrInvocations + "=" + config.invocationLambdaName,
				lambdaAlarmAttrAtLeast + "=" + strconv.FormatInt(int64(config.invocationsPerMinute), 10) + lambdaAlarmRateUnit,
			},
		})
	}
	return triggers, nil
}
