package lib

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

func validLambdaAlarmTrigger() *InfraTrigger {
	return &InfraTrigger{
		Type: lambdaTriggerAlarm,
		Attr: []string{
			"name=better-beta-invocations-runaway",
			"description=Better Beta Lambda invocation runaway guard",
			"namespace=AWS/Lambda",
			"metric=Invocations",
			"dimension=FunctionName=better-beta",
			"statistic=Sum",
			"period=60",
			"threshold=300",
			"comparison=GreaterThanOrEqualToThreshold",
			"evaluation-periods=1",
			"datapoints-to-alarm=1",
			"missing=notBreaching",
		},
	}
}

func TestLambdaAlarmTriggerBuildsExactMetricAlarm(t *testing.T) {
	config, err := parseLambdaAlarmTrigger(validLambdaAlarmTrigger())
	if err != nil {
		t.Fatalf("parse Lambda alarm trigger: %v", err)
	}
	input := config.putInput("arn:aws:lambda:us-west-2:337909772623:function:better-game", "better-game", true)
	if aws.ToString(input.AlarmName) != "better-beta-invocations-runaway" ||
		aws.ToString(input.AlarmDescription) != "Better Beta Lambda invocation runaway guard" ||
		aws.ToString(input.Namespace) != "AWS/Lambda" ||
		aws.ToString(input.MetricName) != "Invocations" ||
		input.Statistic != cwtypes.StatisticSum ||
		aws.ToInt32(input.Period) != 60 ||
		aws.ToFloat64(input.Threshold) != 300 ||
		input.ComparisonOperator != cwtypes.ComparisonOperatorGreaterThanOrEqualToThreshold ||
		aws.ToInt32(input.EvaluationPeriods) != 1 ||
		aws.ToInt32(input.DatapointsToAlarm) != 1 ||
		aws.ToString(input.TreatMissingData) != "notBreaching" ||
		!aws.ToBool(input.ActionsEnabled) ||
		!reflect.DeepEqual(input.AlarmActions, []string{"arn:aws:lambda:us-west-2:337909772623:function:better-game"}) ||
		!reflect.DeepEqual(input.Dimensions, []cwtypes.Dimension{{Name: aws.String("FunctionName"), Value: aws.String("better-beta")}}) {
		t.Fatalf("unexpected metric alarm input: %#v", input)
	}
	if len(input.OKActions) != 0 || len(input.InsufficientDataActions) != 0 || len(input.Tags) != 1 ||
		aws.ToString(input.Tags[0].Key) != infraSetTagName || aws.ToString(input.Tags[0].Value) != "better-game" {
		t.Fatalf("unexpected alarm actions or tags: %#v", input)
	}
}

func TestLambdaAlarmTriggerRejectsMalformedOrAmbiguousConfiguration(t *testing.T) {
	for _, attrs := range [][]string{
		nil,
		{"name=alarm"},
		append(validLambdaAlarmTrigger().Attr, "threshold=301"),
		append(validLambdaAlarmTrigger().Attr[:11], "missing=breach"),
		append(validLambdaAlarmTrigger().Attr[:9], "evaluation-periods=0", "datapoints-to-alarm=1", "missing=notBreaching"),
		append(validLambdaAlarmTrigger().Attr[:10], "datapoints-to-alarm=2", "missing=notBreaching"),
		append(validLambdaAlarmTrigger().Attr[:4], "dimension==better-beta"),
	} {
		if _, err := parseLambdaAlarmTrigger(&InfraTrigger{Type: lambdaTriggerAlarm, Attr: attrs}); err == nil {
			t.Fatalf("malformed alarm trigger was accepted: %#v", attrs)
		}
	}
}

func TestLambdaAlarmComparisonDetectsThresholdDrift(t *testing.T) {
	config, err := parseLambdaAlarmTrigger(validLambdaAlarmTrigger())
	if err != nil {
		t.Fatal(err)
	}
	target := "arn:aws:lambda:us-west-2:337909772623:function:better-game"
	desired := config.metricAlarm(target)
	actual := config.metricAlarm(target)
	if !lambdaAlarmMatches(actual, desired) {
		t.Fatal("exact alarm did not match")
	}
	*actual.Threshold = 301
	if lambdaAlarmMatches(actual, desired) {
		t.Fatal("threshold drift was accepted")
	}
}

func TestLambdaAlarmTriggerRepresentationRejectsUnmanagedConfiguration(t *testing.T) {
	config, err := parseLambdaAlarmTrigger(validLambdaAlarmTrigger())
	if err != nil {
		t.Fatal(err)
	}
	target := "arn:aws:lambda:us-west-2:337909772623:function:better-game"
	alarm := config.metricAlarm(target)
	if !lambdaAlarmTriggerRepresentable(alarm) {
		t.Fatal("exact managed alarm was not representable")
	}

	alarm.Unit = cwtypes.StandardUnitCount
	if lambdaAlarmTriggerRepresentable(alarm) {
		t.Fatal("alarm unit would be silently omitted from infrastructure listing")
	}
	alarm.Unit = ""
	alarm.EvaluateLowSampleCountPercentile = aws.String("ignore")
	if lambdaAlarmTriggerRepresentable(alarm) {
		t.Fatal("low-sample policy would be silently omitted from infrastructure listing")
	}
}

func TestInfraParseAcceptsLambdaAlarmTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.yaml")
	data := `name: better-game
lambda:
  better-game:
    entrypoint: backend.go
    trigger:
      - type: alarm
        attr:
          - name=better-beta-invocations-runaway
          - description=Better Beta Lambda invocation runaway guard
          - namespace=AWS/Lambda
          - metric=Invocations
          - dimension=FunctionName=better-beta
          - statistic=Sum
          - period=60
          - threshold=300
          - comparison=GreaterThanOrEqualToThreshold
          - evaluation-periods=1
          - datapoints-to-alarm=1
          - missing=notBreaching
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	infra, err := InfraParse(path)
	if err != nil {
		t.Fatalf("parse infrastructure alarm: %v", err)
	}
	if len(infra.Lambda["better-game"].Trigger) != 1 || infra.Lambda["better-game"].Trigger[0].Type != lambdaTriggerAlarm {
		t.Fatalf("alarm trigger missing after infrastructure parse: %#v", infra)
	}
}

func TestInfraParseRejectsDuplicateLambdaAlarmNamesAcrossFunctions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.yaml")
	data := `name: better-game
lambda:
  first:
    entrypoint: backend.go
    trigger:
      - type: alarm
        attr:
          - name=shared-alarm
          - namespace=AWS/Lambda
          - metric=Invocations
          - dimension=FunctionName=better-beta
          - statistic=Sum
          - period=60
          - threshold=300
          - comparison=GreaterThanOrEqualToThreshold
          - evaluation-periods=1
          - datapoints-to-alarm=1
          - missing=notBreaching
  second:
    entrypoint: backend.go
    trigger:
      - type: alarm
        attr:
          - name=shared-alarm
          - namespace=AWS/Lambda
          - metric=Invocations
          - dimension=FunctionName=better-beta
          - statistic=Sum
          - period=60
          - threshold=300
          - comparison=GreaterThanOrEqualToThreshold
          - evaluation-periods=1
          - datapoints-to-alarm=1
          - missing=notBreaching
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InfraParse(path); err == nil {
		t.Fatal("duplicate account-global CloudWatch alarm name was accepted")
	}
}

func TestSourceAccountPermissionRequiresExactAlarmBoundary(t *testing.T) {
	const (
		sid       = "alarm_permission"
		function  = "arn:aws:lambda:us-west-2:337909772623:function:better-game"
		principal = "lambda.alarms.cloudwatch.amazonaws.com"
		alarm     = "arn:aws:cloudwatch:us-west-2:337909772623:alarm:better-beta-invocations-runaway"
		account   = "337909772623"
	)
	statement := IamStatementEntry{
		Sid:       sid,
		Effect:    "Allow",
		Principal: map[string]any{"Service": principal},
		Action:    "lambda:InvokeFunction",
		Resource:  function,
		Condition: map[string]any{
			"ArnLike":      map[string]any{"AWS:SourceArn": alarm},
			"StringEquals": map[string]any{"AWS:SourceAccount": account},
		},
	}
	matches, err := lambdaSourceAccountPermissionMatches(statement, sid, function, principal, alarm, account)
	if err != nil || !matches {
		t.Fatalf("exact alarm permission did not match: %v, %v", matches, err)
	}
	matches, err = lambdaSourceAccountPermissionMatches(statement, sid, function, principal, alarm, "000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if matches {
		t.Fatal("wrong source account permission matched")
	}
}
