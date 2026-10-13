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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

const (
	lambdaTriggerAlarm              = "alarm"
	lambdaAlarmInvocationPrincipal  = "lambda.alarms.cloudwatch.amazonaws.com"
	lambdaAlarmAttrName             = "name"
	lambdaAlarmAttrInvocations      = "lambda-invocations"
	lambdaAlarmAttrErrors           = "lambda-errors"
	lambdaAlarmAttrIteratorAge      = "lambda-iterator-age"
	lambdaAlarmAttrAtLeast          = "at-least"
	lambdaAlarmAttrRetained         = "lambda-retained-failures"
	lambdaAlarmAttrDeliveryFailures = "lambda-destination-delivery-failures"
	lambdaAlarmAttrTable            = "dynamodb-table"
	lambdaAlarmRateUnit             = "/minute"

	lambdaAlarmNamespace              = "AWS/Lambda"
	lambdaAlarmMetricInvocations      = "Invocations"
	lambdaAlarmMetricErrors           = "Errors"
	lambdaAlarmMetricIteratorAge      = "IteratorAge"
	lambdaAlarmMetricRetained         = "OnFailureDestinationDeliveredEventCount"
	lambdaAlarmMetricDeliveryFailures = "DestinationDeliveryFailures"
	lambdaAlarmMappingDimension       = "EventSourceMappingUUID"
	lambdaAlarmDimension              = "FunctionName"
	lambdaAlarmPeriod                 = int32(60)
	lambdaAlarmEvaluationPeriods      = int32(1)
	lambdaAlarmDatapointsToAlarm      = int32(1)
	lambdaAlarmMissingData            = "notBreaching"
)

var lambdaAlarmNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
var lambdaAlarmRatePattern = regexp.MustCompile(`^[1-9][0-9]*$`)
var lambdaAlarmTablePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,255}$`)

type lambdaAlarmConfig struct {
	name                string
	monitoredLambdaName string
	threshold           int64
	metric              string
	tableName           string
	mappingUUID         string
}

func (config *lambdaAlarmConfig) monitorAttribute() string {
	switch config.metric {
	case lambdaAlarmMetricIteratorAge:
		return lambdaAlarmAttrIteratorAge
	case lambdaAlarmMetricErrors:
		return lambdaAlarmAttrErrors
	case lambdaAlarmMetricRetained:
		return lambdaAlarmAttrRetained
	case lambdaAlarmMetricDeliveryFailures:
		return lambdaAlarmAttrDeliveryFailures
	default:
		return lambdaAlarmAttrInvocations
	}
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
		case lambdaAlarmAttrName, lambdaAlarmAttrInvocations, lambdaAlarmAttrErrors, lambdaAlarmAttrIteratorAge, lambdaAlarmAttrRetained, lambdaAlarmAttrDeliveryFailures, lambdaAlarmAttrTable, lambdaAlarmAttrAtLeast:
			values[key] = value
		default:
			return nil, fmt.Errorf("unknown Lambda alarm attribute %q", key)
		}
	}
	for _, key := range []string{lambdaAlarmAttrName, lambdaAlarmAttrAtLeast} {
		if values[key] == "" {
			return nil, fmt.Errorf("lambda alarm is missing %q", key)
		}
	}
	name := values[lambdaAlarmAttrName]
	if !lambdaAlarmNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid Lambda alarm name %q", name)
	}
	config := &lambdaAlarmConfig{name: name, tableName: values[lambdaAlarmAttrTable]}
	for attr, metric := range map[string]string{
		lambdaAlarmAttrInvocations:      lambdaAlarmMetricInvocations,
		lambdaAlarmAttrErrors:           lambdaAlarmMetricErrors,
		lambdaAlarmAttrIteratorAge:      lambdaAlarmMetricIteratorAge,
		lambdaAlarmAttrRetained:         lambdaAlarmMetricRetained,
		lambdaAlarmAttrDeliveryFailures: lambdaAlarmMetricDeliveryFailures,
	} {
		if value := values[attr]; value != "" {
			if config.metric != "" {
				return nil, errors.New("lambda alarm requires exactly one monitored metric")
			}
			if err := validateLambdaName(value); err != nil {
				return nil, fmt.Errorf("invalid Lambda alarm %s: %w", attr, err)
			}
			config.monitoredLambdaName, config.metric = value, metric
		}
	}
	if config.metric == "" {
		return nil, errors.New("lambda alarm requires a monitored metric")
	}
	if config.metric == lambdaAlarmMetricRetained {
		if !lambdaAlarmTablePattern.MatchString(config.tableName) {
			return nil, errors.New("retained-failure alarm requires a DynamoDB table name")
		}
	} else if config.tableName != "" {
		return nil, errors.New("DynamoDB table scope is only supported for retained-failure alarms")
	}
	rate := values[lambdaAlarmAttrAtLeast]
	if config.metric == lambdaAlarmMetricIteratorAge {
		duration, err := time.ParseDuration(rate)
		if err != nil || duration <= 0 || duration%time.Millisecond != 0 {
			return nil, fmt.Errorf("lambda alarm %s must be a positive duration in whole milliseconds: %q", lambdaAlarmAttrAtLeast, rate)
		}
		config.threshold = duration.Milliseconds()
		return config, nil
	}
	count, hasUnit := strings.CutSuffix(rate, lambdaAlarmRateUnit)
	if !hasUnit || !lambdaAlarmRatePattern.MatchString(count) {
		return nil, fmt.Errorf("lambda alarm %s must be a positive integer followed by %q: %q", lambdaAlarmAttrAtLeast, lambdaAlarmRateUnit, rate)
	}
	parsed, err := strconv.ParseInt(count, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("lambda alarm %s is too large: %q", lambdaAlarmAttrAtLeast, rate)
	}
	config.threshold = parsed
	return config, nil
}

func (config *lambdaAlarmConfig) thresholdAttribute() string {
	if config.metric == lambdaAlarmMetricIteratorAge {
		return (time.Duration(config.threshold) * time.Millisecond).String()
	}
	return strconv.FormatInt(config.threshold, 10) + lambdaAlarmRateUnit
}

func (config *lambdaAlarmConfig) metricAlarm(targetARN string) *cwtypes.MetricAlarm {
	dimensions := []cwtypes.Dimension{{Name: aws.String(lambdaAlarmDimension), Value: aws.String(config.monitoredLambdaName)}}
	if config.metric == lambdaAlarmMetricRetained {
		dimensions = []cwtypes.Dimension{{Name: aws.String(lambdaAlarmMappingDimension), Value: aws.String(config.mappingUUID)}}
	}
	statistic := cwtypes.StatisticSum
	if config.metric == lambdaAlarmMetricIteratorAge {
		statistic = cwtypes.StatisticMaximum
	}
	return &cwtypes.MetricAlarm{
		ActionsEnabled:     aws.Bool(true),
		AlarmActions:       []string{targetARN},
		AlarmDescription:   aws.String(""),
		AlarmName:          aws.String(config.name),
		ComparisonOperator: cwtypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
		DatapointsToAlarm:  aws.Int32(lambdaAlarmDatapointsToAlarm),
		Dimensions:         dimensions,
		EvaluationPeriods:  aws.Int32(lambdaAlarmEvaluationPeriods),
		MetricName:         aws.String(config.metric),
		Namespace:          aws.String(lambdaAlarmNamespace),
		Period:             aws.Int32(lambdaAlarmPeriod),
		Statistic:          statistic,
		Threshold:          aws.Float64(float64(config.threshold)),
		TreatMissingData:   aws.String(lambdaAlarmMissingData),
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
		nil,
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
	declarations map[string]*InfraLambda,
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
	for _, config := range configs {
		if config.metric == lambdaAlarmMetricRetained {
			mapping, err := lambdaAlarmResolveMapping(ctx, config, functionARN.Partition, region, account, preview, declarations)
			if err != nil {
				return nil, err
			}
			if mapping != nil {
				config.mappingUUID = aws.ToString(mapping.UUID)
			}
		}
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
		if config.metric == lambdaAlarmMetricRetained && config.mappingUUID == "" {
			Logger.Println(PreviewString(true)+"will resolve CloudWatch alarm mapping:", config.name, config.monitoredLambdaName, config.tableName)
		} else if create || !lambdaAlarmMatches(&alarms[0], desired) {
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

func lambdaAlarmActionFunctionName(alarm *cwtypes.MetricAlarm) (string, bool) {
	if alarm == nil || alarm.AlarmArn == nil || len(alarm.AlarmActions) != 1 {
		return "", false
	}
	name, exactFunctionARN := lambdaUnqualifiedFunctionName(alarm.AlarmActions[0])
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
	threshold := aws.ToFloat64(alarm.Threshold)
	maxThreshold := int64(1<<31 - 1)
	if aws.ToString(alarm.MetricName) == lambdaAlarmMetricIteratorAge {
		maxThreshold = int64((1<<63 - 1) / time.Millisecond)
	}
	if threshold < 1 || threshold > float64(maxThreshold) || math.Trunc(threshold) != threshold {
		return nil, false
	}
	config := &lambdaAlarmConfig{
		name:      aws.ToString(alarm.AlarmName),
		metric:    aws.ToString(alarm.MetricName),
		threshold: int64(threshold),
	}
	dimension := alarm.Dimensions[0]
	value := aws.ToString(dimension.Value)
	switch config.metric {
	case lambdaAlarmMetricRetained:
		if aws.ToString(dimension.Name) != lambdaAlarmMappingDimension || value == "" {
			return nil, false
		}
		config.mappingUUID = value
	case lambdaAlarmMetricInvocations, lambdaAlarmMetricErrors, lambdaAlarmMetricIteratorAge, lambdaAlarmMetricDeliveryFailures:
		if aws.ToString(dimension.Name) != lambdaAlarmDimension || validateLambdaName(value) != nil {
			return nil, false
		}
		config.monitoredLambdaName = value
	default:
		return nil, false
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

func lambdaAlarmTriggers(ctx context.Context, setName string) ([]*InfraTrigger, error) {
	alarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), nil)
	if err != nil {
		return nil, err
	}
	var triggers []*InfraTrigger
	for _, alarm := range alarms {
		if setName != "" {
			owner, err := cloudwatchAlarmInfraSet(ctx, CloudwatchClient(), aws.ToString(alarm.AlarmArn))
			if err != nil {
				var absent *cwtypes.ResourceNotFoundException
				var missing *cwtypes.ResourceNotFound
				if errors.As(err, &absent) || errors.As(err, &missing) {
					continue
				}
				return nil, err
			}
			if owner != setName {
				continue
			}
		}
		config, representable := lambdaAlarmConfigFromMetricAlarm(&alarm)
		if !representable {
			continue
		}
		if config.metric == lambdaAlarmMetricRetained {
			mapping, err := LambdaClient().GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{UUID: aws.String(config.mappingUUID)})
			if err != nil {
				return nil, err
			}
			if mapping == nil || aws.ToString(mapping.UUID) != config.mappingUUID {
				return nil, errors.New("alarm mapping read returned an incomplete or different identity")
			}
			alarmARN, err := awsarn.Parse(aws.ToString(alarm.AlarmArn))
			if err != nil {
				return nil, err
			}
			function, table, err := lambdaAlarmMappingIdentity(aws.ToString(mapping.FunctionArn), aws.ToString(mapping.EventSourceArn), alarmARN.Partition, alarmARN.Region, alarmARN.AccountID)
			if err != nil {
				return nil, err
			}
			config.monitoredLambdaName, config.tableName = function, table
		}
		lambdaName, _ := lambdaAlarmActionFunctionName(&alarm)
		attrs := []string{lambdaAlarmAttrName + "=" + config.name, config.monitorAttribute() + "=" + config.monitoredLambdaName}
		if config.tableName != "" {
			attrs = append(attrs, lambdaAlarmAttrTable+"="+config.tableName)
		}
		attrs = append(attrs, lambdaAlarmAttrAtLeast+"="+config.thresholdAttribute())
		triggers = append(triggers, &InfraTrigger{lambdaName: lambdaName, Type: lambdaTriggerAlarm, Attr: attrs})
	}
	return triggers, nil
}

func lambdaAlarmMappingIdentity(functionARN, sourceARN, partition, region, account string) (string, string, error) {
	function, valid := lambdaUnqualifiedFunctionName(functionARN)
	fn, fnErr := awsarn.Parse(functionARN)
	source, sourceErr := awsarn.Parse(sourceARN)
	table, tableErr := lambdaDynamoDBStreamTableName(sourceARN)
	if !valid || fnErr != nil || sourceErr != nil || tableErr != nil || len(strings.Split(source.Resource, "/")) != 4 ||
		fn.Partition != partition || source.Partition != partition || fn.Region != region || source.Region != region ||
		fn.AccountID != account || source.AccountID != account {
		return "", "", fmt.Errorf("invalid alarm mapping identity: function=%q source=%q", functionARN, sourceARN)
	}
	return function, table, nil
}

func lambdaAlarmResolveMapping(ctx context.Context, config *lambdaAlarmConfig, partition, region, account string, preview bool, declarations map[string]*InfraLambda) (*lambdatypes.EventSourceMappingConfiguration, error) {
	stream, err := DynamoDBStreamArn(ctx, config.tableName)
	if err != nil {
		if preview && lambdaDynamoDBResourceNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	mappings, err := lambdaListEventSourceMappings(ctx, config.monitoredLambdaName)
	if err != nil {
		if preview && lambdaEventSourceMappingNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var found *lambdatypes.EventSourceMappingConfiguration
	for _, mapping := range mappings {
		if aws.ToString(mapping.EventSourceArn) != stream {
			continue
		}
		function, table, err := lambdaAlarmMappingIdentity(aws.ToString(mapping.FunctionArn), aws.ToString(mapping.EventSourceArn), partition, region, account)
		if err != nil {
			return nil, err
		}
		if function != config.monitoredLambdaName || table != config.tableName || aws.ToString(mapping.UUID) == "" {
			return nil, errors.New("alarm mapping identity does not match declaration")
		}
		if found != nil {
			return nil, fmt.Errorf("ambiguous alarm mapping for %s %s", function, table)
		}
		found = &mapping
	}
	if found == nil {
		if preview {
			return nil, nil
		}
		return nil, fmt.Errorf("no DynamoDB mapping for alarm %s: %s %s", config.name, config.monitoredLambdaName, config.tableName)
	}
	// A full preview may be enabling metrics and retention before this alarm.
	if preview && declarations[config.monitoredLambdaName] != nil {
		if err := lambdaAlarmValidateDeclaredMapping(config, declarations); err != nil {
			return nil, err
		}
		return found, nil
	}
	if !slices.Contains(lambdaMappingMetrics(found.MetricsConfig), lambdatypes.EventSourceMappingMetricEventCount) {
		return nil, fmt.Errorf("alarm %s requires metrics=event_count on %s %s", config.name, config.monitoredLambdaName, config.tableName)
	}
	if dynamoDBFailureDestination(found.DestinationConfig) == "" {
		return nil, fmt.Errorf("alarm %s requires an on-failure destination on %s %s", config.name, config.monitoredLambdaName, config.tableName)
	}
	return found, nil
}

// Validate cross-function declarations without resolving provider identities.
func validateLambdaAlarms(declarations map[string]*InfraLambda) error {
	names := map[string]bool{}
	for _, function := range declarations {
		for _, trigger := range function.Trigger {
			if trigger.Type != lambdaTriggerAlarm {
				continue
			}
			config, err := parseLambdaAlarmTrigger(trigger)
			if err != nil {
				return err
			}
			if names[config.name] {
				return fmt.Errorf("duplicate Lambda alarm name %q", config.name)
			}
			names[config.name] = true
			if err := lambdaAlarmValidateDeclaredMapping(config, declarations); err != nil {
				return err
			}
		}
	}
	return nil
}

func lambdaAlarmValidateDeclaredMapping(config *lambdaAlarmConfig, declarations map[string]*InfraLambda) error {
	function := declarations[config.monitoredLambdaName]
	if config.metric != lambdaAlarmMetricRetained || function == nil {
		return nil
	}
	mappings, err := lambdaDynamoDBParseMappings(function)
	if err != nil {
		return err
	}
	for _, mapping := range mappings {
		if mapping.tableName == config.tableName && slices.Contains(lambdaMappingMetrics(mapping.create.MetricsConfig), lambdatypes.EventSourceMappingMetricEventCount) {
			if dynamoDBFailureDestination(mapping.create.DestinationConfig) == "" {
				return fmt.Errorf("alarm %s requires an on-failure destination on %s %s", config.name, config.monitoredLambdaName, config.tableName)
			}
			return nil
		}
	}
	return fmt.Errorf("alarm %s requires a DynamoDB trigger with metrics=event_count on %s %s", config.name, config.monitoredLambdaName, config.tableName)
}
