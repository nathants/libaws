package lib

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type stubCloudwatchAlarmClient struct {
	describe func(*cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error)
	listTags func(*cloudwatch.ListTagsForResourceInput) (*cloudwatch.ListTagsForResourceOutput, error)
	put      func(*cloudwatch.PutMetricAlarmInput) (*cloudwatch.PutMetricAlarmOutput, error)
	tag      func(*cloudwatch.TagResourceInput) (*cloudwatch.TagResourceOutput, error)
	delete   func(*cloudwatch.DeleteAlarmsInput) (*cloudwatch.DeleteAlarmsOutput, error)
}

func (client *stubCloudwatchAlarmClient) DescribeAlarms(_ context.Context, input *cloudwatch.DescribeAlarmsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error) {
	return client.describe(input)
}

func (client *stubCloudwatchAlarmClient) ListTagsForResource(_ context.Context, input *cloudwatch.ListTagsForResourceInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.ListTagsForResourceOutput, error) {
	return client.listTags(input)
}

func (client *stubCloudwatchAlarmClient) PutMetricAlarm(_ context.Context, input *cloudwatch.PutMetricAlarmInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricAlarmOutput, error) {
	return client.put(input)
}

func (client *stubCloudwatchAlarmClient) TagResource(_ context.Context, input *cloudwatch.TagResourceInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.TagResourceOutput, error) {
	return client.tag(input)
}

func (client *stubCloudwatchAlarmClient) DeleteAlarms(_ context.Context, input *cloudwatch.DeleteAlarmsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.DeleteAlarmsOutput, error) {
	return client.delete(input)
}

func validLambdaAlarmTrigger() *InfraTrigger {
	return &InfraTrigger{
		Type: lambdaTriggerAlarm,
		Attr: []string{
			"name=better-beta-invocations-runaway",
			"lambda-invocations=better-beta",
			"at-least=300/minute",
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
		aws.ToString(input.AlarmDescription) != "" ||
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

func TestLambdaAlarmTriggerRejectsMalformedConfiguration(t *testing.T) {
	for name, attrs := range map[string][]string{
		"empty":               nil,
		"missing attributes":  {"name=alarm"},
		"duplicate name":      {"name=alarm", "name=other", "lambda-invocations=function", "at-least=1/minute"},
		"invalid alarm name":  {"name=bad name", "lambda-invocations=function", "at-least=1/minute"},
		"invalid Lambda name": {"name=alarm", "lambda-invocations=bad.name", "at-least=1/minute"},
		"zero rate":           {"name=alarm", "lambda-invocations=function", "at-least=0/minute"},
		"leading-zero rate":   {"name=alarm", "lambda-invocations=function", "at-least=01/minute"},
		"fractional rate":     {"name=alarm", "lambda-invocations=function", "at-least=1.5/minute"},
		"wrong rate unit":     {"name=alarm", "lambda-invocations=function", "at-least=1/second"},
		"rate too large":      {"name=alarm", "lambda-invocations=function", "at-least=2147483648/minute"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseLambdaAlarmTrigger(&InfraTrigger{Type: lambdaTriggerAlarm, Attr: attrs}); err == nil {
				t.Fatalf("malformed alarm trigger was accepted: %#v", attrs)
			}
		})
	}
}

func TestLambdaAlarmTriggerRejectsGenericCloudWatchAttributes(t *testing.T) {
	for _, attr := range []string{
		"description=description",
		"namespace=AWS/Lambda",
		"metric=Invocations",
		"dimension=FunctionName=function",
		"statistic=Sum",
		"period=60",
		"threshold=1",
		"comparison=GreaterThanOrEqualToThreshold",
		"evaluation-periods=1",
		"datapoints-to-alarm=1",
		"missing=notBreaching",
	} {
		trigger := validLambdaAlarmTrigger()
		trigger.Attr = append(trigger.Attr, attr)
		if _, err := parseLambdaAlarmTrigger(trigger); err == nil {
			t.Fatalf("generic CloudWatch attribute %q was accepted", attr)
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
	*actual.Threshold = *desired.Threshold
	actual.EvaluationWindow = &cwtypes.EvaluationWindowMemberWallClockWindow{}
	if lambdaAlarmMatches(actual, desired) {
		t.Fatal("unsupported evaluation window was accepted")
	}
}

func TestLambdaAlarmTriggerRepresentationRequiresExactOpinionatedSubset(t *testing.T) {
	config, err := parseLambdaAlarmTrigger(validLambdaAlarmTrigger())
	if err != nil {
		t.Fatal(err)
	}
	const target = "arn:aws:lambda:us-west-2:337909772623:function:better-game"
	exact := func() *cwtypes.MetricAlarm {
		alarm := config.metricAlarm(target)
		alarm.AlarmArn = aws.String("arn:aws:cloudwatch:us-west-2:337909772623:alarm:better-beta-invocations-runaway")
		return alarm
	}
	if !lambdaAlarmTriggerRepresentable(exact()) {
		t.Fatal("exact managed alarm was not representable")
	}

	for _, test := range []struct {
		name   string
		mutate func(*cwtypes.MetricAlarm)
	}{
		{"description", func(alarm *cwtypes.MetricAlarm) { alarm.AlarmDescription = aws.String("custom") }},
		{"namespace", func(alarm *cwtypes.MetricAlarm) { alarm.Namespace = aws.String("Custom") }},
		{"metric", func(alarm *cwtypes.MetricAlarm) { alarm.MetricName = aws.String("Errors") }},
		{"dimension", func(alarm *cwtypes.MetricAlarm) { alarm.Dimensions[0].Name = aws.String("Resource") }},
		{"extra dimension", func(alarm *cwtypes.MetricAlarm) {
			alarm.Dimensions = append(alarm.Dimensions, cwtypes.Dimension{Name: aws.String("Extra"), Value: aws.String("value")})
		}},
		{"statistic", func(alarm *cwtypes.MetricAlarm) { alarm.Statistic = cwtypes.StatisticAverage }},
		{"period", func(alarm *cwtypes.MetricAlarm) { alarm.Period = aws.Int32(300) }},
		{"fractional threshold", func(alarm *cwtypes.MetricAlarm) { alarm.Threshold = aws.Float64(300.5) }},
		{"comparison", func(alarm *cwtypes.MetricAlarm) {
			alarm.ComparisonOperator = cwtypes.ComparisonOperatorGreaterThanThreshold
		}},
		{"evaluation periods", func(alarm *cwtypes.MetricAlarm) { alarm.EvaluationPeriods = aws.Int32(2) }},
		{"datapoints", func(alarm *cwtypes.MetricAlarm) { alarm.DatapointsToAlarm = aws.Int32(2) }},
		{"missing data", func(alarm *cwtypes.MetricAlarm) { alarm.TreatMissingData = aws.String("missing") }},
		{"unit", func(alarm *cwtypes.MetricAlarm) { alarm.Unit = cwtypes.StandardUnitCount }},
		{"OK action", func(alarm *cwtypes.MetricAlarm) { alarm.OKActions = []string{target} }},
		{"low-sample policy", func(alarm *cwtypes.MetricAlarm) { alarm.EvaluateLowSampleCountPercentile = aws.String("ignore") }},
		{"evaluation window", func(alarm *cwtypes.MetricAlarm) {
			alarm.EvaluationWindow = &cwtypes.EvaluationWindowMemberWallClockWindow{}
		}},
		{"cross-account action", func(alarm *cwtypes.MetricAlarm) {
			alarm.AlarmActions[0] = "arn:aws:lambda:us-west-2:000000000000:function:better-game"
		}},
		{"cross-region action", func(alarm *cwtypes.MetricAlarm) {
			alarm.AlarmArn = aws.String("arn:aws:cloudwatch:us-east-1:337909772623:alarm:better-beta-invocations-runaway")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			alarm := exact()
			test.mutate(alarm)
			if lambdaAlarmTriggerRepresentable(alarm) {
				t.Fatalf("alarm outside the opinionated subset was representable: %#v", alarm)
			}
		})
	}
}

func TestLambdaAlarmFunctionNameRequiresUnqualifiedFunctionARN(t *testing.T) {
	for arn, want := range map[string]string{
		"arn:aws:lambda:us-west-2:337909772623:function:better-game":           "better-game",
		"arn:aws-us-gov:lambda:us-gov-west-1:337909772623:function:government": "government",
	} {
		got, ok := lambdaUnqualifiedFunctionName(arn)
		if !ok || got != want {
			t.Fatalf("lambda alarm action %q = %q, %v; want %q, true", arn, got, ok, want)
		}
	}
	for _, arn := range []string{
		"arn:aws:lambda:us-west-2:337909772623:function:better-game:beta",
		"arn:aws:lambda:us-west-2:337909772623:function:better-game:42",
		"arn:aws:sns:us-west-2:337909772623:topic",
		"not-an-arn",
	} {
		if name, ok := lambdaUnqualifiedFunctionName(arn); ok {
			t.Fatalf("qualified or invalid Lambda action %q was accepted as %q", arn, name)
		}
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
          - lambda-invocations=better-beta
          - at-least=300/minute
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
          - lambda-invocations=better-beta
          - at-least=300/minute
  second:
    entrypoint: backend.go
    trigger:
      - type: alarm
        attr:
          - name=shared-alarm
          - lambda-invocations=better-beta
          - at-least=300/minute
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

func TestLambdaPermissionReconciliationRepairsMissingSourceAccount(t *testing.T) {
	const (
		sid       = "permission"
		function  = "arn:aws:lambda:us-west-2:337909772623:function:better-game"
		principal = "events.amazonaws.com"
		sourceARN = "arn:aws:events:us-west-2:337909772623:rule/expected"
		account   = "337909772623"
	)
	policy, err := json.Marshal(IamPolicyDocument{
		Version: "2012-10-17",
		Statement: []IamStatementEntry{{
			Sid:       sid,
			Effect:    "Allow",
			Principal: map[string]any{"Service": principal},
			Action:    "lambda:InvokeFunction",
			Resource:  function,
			Condition: map[string]any{
				"ArnLike": map[string]any{"AWS:SourceArn": sourceARN},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	needsUpdate, removeExisting, err := lambdaPermissionReconciliation(
		string(policy), sid, function, principal, sourceARN, account,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !needsUpdate || !removeExisting {
		t.Fatalf("missing source-account reconciliation = needs update %v, remove existing %v; want true, true", needsUpdate, removeExisting)
	}
}

func TestLambdaPermissionReconciliationRequiresSourceAccount(t *testing.T) {
	if _, _, err := lambdaPermissionReconciliation(
		`{"Version":"2012-10-17"}`, "permission", "function", "events.amazonaws.com", "source", "",
	); err == nil {
		t.Fatal("permission reconciliation accepted an empty source account")
	}
}

func TestLambdaPermissionReconciliationCreatesMissingSIDInExistingPolicy(t *testing.T) {
	const (
		sid       = "alarm_permission"
		function  = "arn:aws:lambda:us-west-2:337909772623:function:better-game"
		principal = "lambda.alarms.cloudwatch.amazonaws.com"
		alarm     = "arn:aws:cloudwatch:us-west-2:337909772623:alarm:better-beta-invocations-runaway"
		account   = "337909772623"
	)
	policy, err := json.Marshal(IamPolicyDocument{
		Version: "2012-10-17",
		Statement: []IamStatementEntry{{
			Sid:      "unrelated_permission",
			Effect:   "Allow",
			Action:   "lambda:InvokeFunction",
			Resource: function,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	needsUpdate, removeExisting, err := lambdaPermissionReconciliation(string(policy), sid, function, principal, alarm, account)
	if err != nil {
		t.Fatal(err)
	}
	if !needsUpdate || removeExisting {
		t.Fatalf("missing SID reconciliation = needs update %v, remove existing %v; want true, false", needsUpdate, removeExisting)
	}
}

func TestLambdaEnsureTriggerAlarmRejectsQualifiedOrWrongFunctionARN(t *testing.T) {
	client := &stubCloudwatchAlarmClient{}
	client.describe = func(*cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error) {
		t.Fatal("described alarms for an invalid Lambda identity")
		return nil, nil
	}
	resolveAccount := func(context.Context) (string, error) { return "337909772623", nil }
	ensurePermission := func(context.Context, string, string, string, string, bool) (string, error) {
		t.Fatal("mutated permissions for an invalid Lambda identity")
		return "", nil
	}
	for _, functionARN := range []string{
		"arn:aws:lambda:us-west-2:337909772623:function:better-game:beta",
		"arn:aws:lambda:us-west-2:337909772623:function:other",
	} {
		infraLambda := &InfraLambda{
			Name:         "better-game",
			Arn:          functionARN,
			Trigger:      []*InfraTrigger{validLambdaAlarmTrigger()},
			infraSetName: "better-game",
		}
		if _, err := lambdaEnsureTriggerAlarm(
			context.Background(), client, resolveAccount, "us-west-2", ensurePermission, infraLambda, false,
		); err == nil {
			t.Fatalf("invalid Lambda ARN %q was accepted", functionARN)
		}
	}
}

func TestLambdaEnsureTriggerAlarmReconcilesUnsupportedAndStaleAlarms(t *testing.T) {
	const (
		account      = "337909772623"
		region       = "us-west-2"
		functionName = "better-game"
		infraSetName = "better-game"
	)
	functionARN := "arn:aws:lambda:" + region + ":" + account + ":function:" + functionName
	config, err := parseLambdaAlarmTrigger(validLambdaAlarmTrigger())
	if err != nil {
		t.Fatal(err)
	}
	desiredARN := cloudwatchAlarmARN("aws", region, account, config.name)
	staleName := "stale-alarm"
	staleARN := cloudwatchAlarmARN("aws", region, account, staleName)
	unrelatedName := "unrelated-alarm"
	unrelatedARN := cloudwatchAlarmARN("aws", region, account, unrelatedName)

	desired := config.metricAlarm(functionARN)
	desired.AlarmArn = aws.String(desiredARN)
	desired.EvaluationWindow = &cwtypes.EvaluationWindowMemberWallClockWindow{}
	stale := config.metricAlarm(functionARN)
	stale.AlarmName = aws.String(staleName)
	stale.AlarmArn = aws.String(staleARN)
	unrelated := config.metricAlarm(functionARN)
	unrelated.AlarmName = aws.String(unrelatedName)
	unrelated.AlarmArn = aws.String(unrelatedARN)
	alarms := map[string]cwtypes.MetricAlarm{
		config.name:   *desired,
		staleName:     *stale,
		unrelatedName: *unrelated,
	}
	tags := map[string]string{
		desiredARN:   infraSetName,
		staleARN:     infraSetName,
		unrelatedARN: "someone-else",
	}
	var putCount, deleteCount int
	client := &stubCloudwatchAlarmClient{}
	client.describe = func(input *cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error) {
		var result []cwtypes.MetricAlarm
		if len(input.AlarmNames) != 0 {
			for _, name := range input.AlarmNames {
				if alarm, found := alarms[name]; found {
					result = append(result, alarm)
				}
			}
		} else {
			for _, alarm := range alarms {
				result = append(result, alarm)
			}
		}
		return &cloudwatch.DescribeAlarmsOutput{MetricAlarms: result}, nil
	}
	client.listTags = func(input *cloudwatch.ListTagsForResourceInput) (*cloudwatch.ListTagsForResourceOutput, error) {
		value := tags[aws.ToString(input.ResourceARN)]
		var result []cwtypes.Tag
		if value != "" {
			result = append(result, cwtypes.Tag{Key: aws.String(infraSetTagName), Value: aws.String(value)})
		}
		return &cloudwatch.ListTagsForResourceOutput{Tags: result}, nil
	}
	client.put = func(input *cloudwatch.PutMetricAlarmInput) (*cloudwatch.PutMetricAlarmOutput, error) {
		putCount++
		if aws.ToString(input.AlarmName) != config.name || len(input.Tags) != 0 {
			t.Fatalf("unexpected alarm update: %#v", input)
		}
		updated := config.metricAlarm(functionARN)
		updated.AlarmArn = aws.String(desiredARN)
		alarms[config.name] = *updated
		return &cloudwatch.PutMetricAlarmOutput{}, nil
	}
	client.tag = func(input *cloudwatch.TagResourceInput) (*cloudwatch.TagResourceOutput, error) {
		if len(input.Tags) != 1 {
			t.Fatalf("unexpected alarm tag update: %#v", input)
		}
		tags[aws.ToString(input.ResourceARN)] = aws.ToString(input.Tags[0].Value)
		return &cloudwatch.TagResourceOutput{}, nil
	}
	client.delete = func(input *cloudwatch.DeleteAlarmsInput) (*cloudwatch.DeleteAlarmsOutput, error) {
		deleteCount++
		for _, name := range input.AlarmNames {
			delete(alarms, name)
		}
		return &cloudwatch.DeleteAlarmsOutput{}, nil
	}
	permissionCount := 0
	ensurePermission := func(_ context.Context, name, principal, sourceARN, sourceAccount string, preview bool) (string, error) {
		permissionCount++
		if name != functionName || principal != lambdaAlarmInvocationPrincipal || sourceARN != desiredARN || sourceAccount != account || preview {
			t.Fatalf("unexpected alarm permission: %q %q %q %q preview=%v", name, principal, sourceARN, sourceAccount, preview)
		}
		return "alarm-permission", nil
	}
	infraLambda := &InfraLambda{
		Name:         functionName,
		Arn:          functionARN,
		Trigger:      []*InfraTrigger{validLambdaAlarmTrigger()},
		infraSetName: infraSetName,
	}
	resolveAccount := func(context.Context) (string, error) { return account, nil }

	sids, err := lambdaEnsureTriggerAlarm(context.Background(), client, resolveAccount, region, ensurePermission, infraLambda, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sids, []string{"alarm-permission"}) || putCount != 1 || deleteCount != 1 || permissionCount != 1 {
		t.Fatalf("first reconciliation = sids %#v, puts %d, deletes %d, permissions %d", sids, putCount, deleteCount, permissionCount)
	}
	if _, found := alarms[staleName]; found {
		t.Fatal("owned stale alarm survived reconciliation")
	}
	if _, found := alarms[unrelatedName]; !found {
		t.Fatal("unrelated alarm was deleted")
	}

	putCount, deleteCount, permissionCount = 0, 0, 0
	if _, err := lambdaEnsureTriggerAlarm(context.Background(), client, resolveAccount, region, ensurePermission, infraLambda, false); err != nil {
		t.Fatal(err)
	}
	if putCount != 0 || deleteCount != 0 || permissionCount != 1 {
		t.Fatalf("converged reconciliation = puts %d, deletes %d, permissions %d", putCount, deleteCount, permissionCount)
	}
}

func TestLambdaCleanupAlarmTriggersAfterFunctionDeletionPreservesUnownedAlarms(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:337909772623:function:better-game"
	makeAlarm := func(name, target string, actions int) cwtypes.MetricAlarm {
		alarmActions := []string{target}
		if actions > 1 {
			alarmActions = append(alarmActions, functionARN)
		}
		return cwtypes.MetricAlarm{
			AlarmName:    aws.String(name),
			AlarmArn:     aws.String(cloudwatchAlarmARN("aws", "us-west-2", "337909772623", name)),
			AlarmActions: alarmActions,
		}
	}
	alarms := []cwtypes.MetricAlarm{
		makeAlarm("owned", functionARN, 1),
		makeAlarm("wrong-tag", functionARN, 1),
		makeAlarm("extra-action", functionARN, 2),
		makeAlarm("wrong-target", functionARN+":alias", 1),
	}
	tags := map[string]string{
		aws.ToString(alarms[0].AlarmArn): "set",
		aws.ToString(alarms[1].AlarmArn): "other",
		aws.ToString(alarms[2].AlarmArn): "set",
		aws.ToString(alarms[3].AlarmArn): "set",
	}
	var deleted []string
	client := &stubCloudwatchAlarmClient{
		describe: func(*cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error) {
			return &cloudwatch.DescribeAlarmsOutput{MetricAlarms: alarms}, nil
		},
		listTags: func(input *cloudwatch.ListTagsForResourceInput) (*cloudwatch.ListTagsForResourceOutput, error) {
			value := tags[aws.ToString(input.ResourceARN)]
			return &cloudwatch.ListTagsForResourceOutput{Tags: []cwtypes.Tag{{
				Key: aws.String(infraSetTagName), Value: aws.String(value),
			}}}, nil
		},
		delete: func(input *cloudwatch.DeleteAlarmsInput) (*cloudwatch.DeleteAlarmsOutput, error) {
			deleted = append(deleted, input.AlarmNames...)
			return &cloudwatch.DeleteAlarmsOutput{}, nil
		},
	}
	identity := lambdaIdentity{name: "better-game", arn: functionARN, infraSetName: "set", exists: false}
	if err := lambdaCleanupAlarmTriggers(context.Background(), client, identity, nil, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(deleted, []string{"owned"}) {
		t.Fatalf("deleted alarms = %v, want [owned]", deleted)
	}
}

func TestLambdaAlarmIntegration(t *testing.T) {
	if os.Getenv("LIBAWS_INTEGRATION") != "1" {
		t.Skip("set LIBAWS_INTEGRATION=1 to run AWS integration tests")
	}
	functionName := os.Getenv("LIBAWS_LAMBDA_ALARM_TEST_FUNCTION")
	alarmName := os.Getenv("LIBAWS_LAMBDA_ALARM_TEST_ALARM")
	mode := os.Getenv("LIBAWS_LAMBDA_ALARM_TEST_MODE")
	if !strings.HasPrefix(functionName, "test-lambda-") || len(functionName) == len("test-lambda-") {
		t.Fatalf("LIBAWS_LAMBDA_ALARM_TEST_FUNCTION must name a unique test-lambda-* function, got %q", functionName)
	}
	if !strings.HasPrefix(alarmName, "test-alarm-") || len(alarmName) == len("test-alarm-") {
		t.Fatalf("LIBAWS_LAMBDA_ALARM_TEST_ALARM must name a unique test-alarm-* alarm, got %q", alarmName)
	}
	if !slices.Contains([]string{"drift", "verify-drift", "verify-converged", "delete-function", "force-cleanup"}, mode) {
		t.Fatalf("unknown LIBAWS_LAMBDA_ALARM_TEST_MODE %q", mode)
	}
	expectedAccount := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expectedAccount == "" {
		t.Fatal("LIBAWS_TEST_ACCOUNT must identify the authorized scratch account")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	actualAccount, err := StsAccount(ctx)
	if err != nil {
		t.Fatalf("verify AWS account: %v", err)
	}
	if actualAccount != expectedAccount {
		t.Fatalf("refusing Lambda alarm integration test in AWS account %q; expected scratch account %q", actualAccount, expectedAccount)
	}
	if mode == "force-cleanup" {
		if _, err := CloudwatchClient().DeleteAlarms(ctx, &cloudwatch.DeleteAlarmsInput{
			AlarmNames: []string{alarmName},
		}); err != nil {
			t.Fatalf("force-delete integration alarm: %v", err)
		}
		return
	}
	if mode == "delete-function" {
		if err := LambdaDeleteFunction(ctx, functionName, false); err != nil {
			t.Fatalf("manually delete integration Lambda: %v", err)
		}
		if functionARN, err := LambdaArn(ctx, functionName); err == nil || functionARN != "" {
			t.Fatalf("manually deleted Lambda still resolves as %q with error %v", functionARN, err)
		}
		return
	}
	functionARN, err := LambdaArn(ctx, functionName)
	if err != nil {
		t.Fatalf("resolve test Lambda ARN: %v", err)
	}
	if functionARN == "" {
		t.Fatalf("test Lambda %q does not exist", functionName)
	}
	remoteAccount := "000000000000"
	if remoteAccount == actualAccount {
		remoteAccount = "111111111111"
	}
	remoteFunctionARN := strings.Replace(functionARN, actualAccount, remoteAccount, 1)

	client := CloudwatchClient()
	getAlarm := func() *cwtypes.MetricAlarm {
		t.Helper()
		alarms, getErr := cloudwatchDescribeMetricAlarms(ctx, client, []string{alarmName})
		if getErr != nil {
			t.Fatalf("describe CloudWatch alarm %q: %v", alarmName, getErr)
		}
		if len(alarms) != 1 {
			t.Fatalf("describe CloudWatch alarm %q returned %d alarms", alarmName, len(alarms))
		}
		return &alarms[0]
	}
	putInput := func(alarm *cwtypes.MetricAlarm) *cloudwatch.PutMetricAlarmInput {
		return &cloudwatch.PutMetricAlarmInput{
			ActionsEnabled:     alarm.ActionsEnabled,
			AlarmActions:       slices.Clone(alarm.AlarmActions),
			AlarmDescription:   alarm.AlarmDescription,
			AlarmName:          alarm.AlarmName,
			ComparisonOperator: alarm.ComparisonOperator,
			DatapointsToAlarm:  alarm.DatapointsToAlarm,
			Dimensions:         slices.Clone(alarm.Dimensions),
			EvaluationPeriods:  alarm.EvaluationPeriods,
			MetricName:         alarm.MetricName,
			Namespace:          alarm.Namespace,
			OKActions:          slices.Clone(alarm.OKActions),
			Period:             alarm.Period,
			Statistic:          alarm.Statistic,
			Threshold:          alarm.Threshold,
			TreatMissingData:   alarm.TreatMissingData,
		}
	}
	assertDrift := func() {
		t.Helper()
		alarm := getAlarm()
		if !reflect.DeepEqual(alarm.AlarmActions, []string{remoteFunctionARN}) ||
			!reflect.DeepEqual(alarm.OKActions, []string{functionARN}) ||
			aws.ToString(alarm.AlarmDescription) != "custom description" ||
			aws.ToInt32(alarm.Period) != 300 ||
			!lambdaAlarmHasUnsupportedConfiguration(alarm) || lambdaAlarmTriggerRepresentable(alarm) {
			t.Fatalf("alarm drift was lost or representable: %#v", alarm)
		}
	}

	switch mode {
	case "drift":
		alarm := getAlarm()
		if !lambdaAlarmTriggerRepresentable(alarm) {
			t.Fatalf("initial alarm is not representable: %#v", alarm)
		}
		input := putInput(alarm)
		input.AlarmActions = []string{remoteFunctionARN}
		input.OKActions = []string{functionARN}
		input.AlarmDescription = aws.String("custom description")
		input.Period = aws.Int32(300)
		if _, err := client.PutMetricAlarm(ctx, input); err != nil {
			t.Fatalf("drift alarm outside the opinionated subset: %v", err)
		}
		assertDrift()
	case "verify-drift":
		assertDrift()
	case "verify-converged":
		alarm := getAlarm()
		config, representable := lambdaAlarmConfigFromMetricAlarm(alarm)
		if !representable || !reflect.DeepEqual(alarm.AlarmActions, []string{functionARN}) ||
			config.name != alarmName || config.invocationLambdaName != functionName || config.invocationsPerMinute != 1 {
			t.Fatalf("alarm did not converge to the declared subset: %#v, config %#v", alarm, config)
		}
	default:
		t.Fatalf("unknown integration mode %q", mode)
	}
}
