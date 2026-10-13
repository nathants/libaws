package lib

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestDynamoDBRetentionMonitoringIntegration(t *testing.T) {
	uid := os.Getenv("LIBAWS_DYNAMODB_RETENTION_UID")
	if uid == "" {
		t.Skip("run through examples/misc/dynamodb-retention")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("requires a unique 12-hex uid")
	}
	requireLiveAWSAccount(t)
	// Sequential mapping transitions and real metric publication each have AWS
	// control-plane latency. Keep the full run bounded without borrowing the
	// independent cleanup budget reserved by the example's outer timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	infraPath := os.Getenv("LIBAWS_DYNAMODB_RETENTION_INFRA")
	infra, err := InfraParse(infraPath)
	if err != nil {
		t.Fatal(err)
	}
	name, notifier := "test-retention-"+uid, "test-notify-"+uid
	retained, failed, decoy := "test-retained-"+uid, "test-delivery-"+uid, "test-decoy-"+uid
	invocationErrors, age := "test-errors-"+uid, "test-age-"+uid
	alarmNames := []string{retained, failed, invocationErrors, age, decoy}
	if infra.Name != name || len(infra.Lambda) != 2 || infra.Lambda[name] == nil || infra.Lambda[notifier] == nil {
		t.Fatal("unexpected fixture declaration")
	}
	// Independent finalizers run even if the infrastructure owner fails halfway.
	t.Cleanup(func() {
		for _, cleanup := range []func(context.Context) error{
			func(ctx context.Context) error { return LambdaDelete(ctx, notifier, false) },
			func(ctx context.Context) error { return LambdaDelete(ctx, name, false) },
			func(ctx context.Context) error {
				_, err := CloudwatchClient().DeleteAlarms(ctx, &cloudwatch.DeleteAlarmsInput{AlarmNames: alarmNames})
				return err
			},
			func(ctx context.Context) error { return DynamoDBDeleteTable(ctx, name, false, false) },
			func(ctx context.Context) error {
				err := S3DeleteBucket(ctx, name, false)
				if isS3NoSuchBucket(err) {
					return nil
				}
				return err
			},
		} {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			if err := cleanup(ctx); err != nil {
				t.Error("cleanup:", err)
			}
			cancel()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err := InfraListSet(ctx, name, false)
		if err != nil || len(out.InfraSet) != 0 {
			t.Errorf("fixture remains after cleanup: %v %v", out, err)
		}
		for _, function := range []string{name, notifier} {
			_, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(function)})
			if _, absent := errors.AsType[*lambdatypes.ResourceNotFoundException](err); !absent {
				t.Errorf("function remains after cleanup: %s %v", function, err)
			}
		}
		_, err = DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
		if _, absent := errors.AsType[*ddbtypes.ResourceNotFoundException](err); !absent {
			t.Errorf("table remains after cleanup: %v", err)
		}
		if err := s3.NewBucketNotExistsWaiter(S3Client()).Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)}, time.Minute); err != nil {
			t.Error("bucket remains after cleanup:", err)
		}
		alarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), alarmNames)
		if err != nil || len(alarms) != 0 {
			t.Errorf("alarms remain after cleanup: %v %v", alarms, err)
		}
	})
	listed, err := InfraListSet(ctx, name, false)
	if err != nil || len(listed.InfraSet) != 0 {
		t.Fatalf("fixture not initially absent: %v %v", listed, err)
	}
	ensureCLI := func(preview bool) {
		t.Helper()
		args := []string{"infra-ensure", infraPath}
		if preview {
			args = append(args, "--preview")
		}
		started := time.Now()
		t.Logf("starting ensure preview=%t", preview)
		out, err := exec.CommandContext(ctx, "libaws", args...).CombinedOutput()
		t.Logf("ensure preview=%t took %s:\n%s", preview, time.Since(started), out)
		if err != nil {
			t.Fatalf("ensure: %v", err)
		}
	}
	ensureCLI(true)
	listed, err = InfraListSet(ctx, name, false)
	if err != nil || len(listed.InfraSet) != 0 {
		t.Fatalf("new-resource preview created infrastructure: %v %v", listed, err)
	}
	ensureCLI(false) // Both monitored and notification functions start absent.
	for function, decl := range infra.Lambda {
		decl.Name = function
		decl.Arn, err = LambdaArn(ctx, function)
		if err != nil {
			t.Fatal(err)
		}
	}
	processor, notification := infra.Lambda[name], infra.Lambda[notifier]
	getMapping := func() lambdatypes.EventSourceMappingConfiguration {
		t.Helper()
		mappings, err := lambdaListEventSourceMappings(ctx, name)
		if err != nil || len(mappings) != 1 {
			t.Fatalf("exact mapping: %v %v", mappings, err)
		}
		return mappings[0]
	}
	assertInventory := func() {
		t.Helper()
		out, err := InfraListSet(ctx, name, false)
		if err != nil {
			t.Fatal(err)
		}
		set := out.InfraSet[name]
		if set == nil || set.Lambda[name] == nil || set.Lambda[notifier] == nil {
			t.Fatal("inventory lost functions")
		}
		for function, desired := range map[string]*InfraLambda{name: processor, notifier: notification} {
			got := set.Lambda[function].Trigger
			if len(got) != len(desired.Trigger) {
				t.Fatalf("inventory triggers: %#v", got)
			}
			for _, trigger := range desired.Trigger {
				want := slices.Clone(trigger.Attr)
				slices.Sort(want)
				found := false
				for _, actual := range got {
					attrs := slices.Clone(actual.Attr)
					slices.Sort(attrs)
					// Inventory also materializes DynamoDB's omitted fixed defaults.
					if actual.Type == trigger.Type && (slices.Equal(attrs, want) || trigger.Type == lambdaTriggerDynamoDB && slices.Contains(attrs, "metrics=event_count") && slices.Contains(attrs, "on-failure=arn:aws:s3:::"+name)) {
						found = true
					}
				}
				if !found {
					t.Fatalf("inventory lost declaration: %v in %v", trigger, got)
				}
			}
		}
	}
	assertInventory()
	initial := getMapping()
	if len(lambdaMappingMetrics(initial.MetricsConfig)) != 1 {
		t.Fatal("creation did not enable EventCount")
	}
	initialAlarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), []string{retained})
	if err != nil || len(initialAlarms) != 1 || initial.LastModified == nil || initialAlarms[0].AlarmConfigurationUpdatedTimestamp == nil {
		t.Fatalf("missing monitoring timestamps: %v", err)
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	if _, err := LambdaEnsureTriggerAlarm(ctx, notification, false); err != nil {
		t.Fatal(err)
	}
	unchangedAlarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), []string{retained})
	if err != nil || len(unchangedAlarms) != 1 || !aws.ToTime(getMapping().LastModified).Equal(*initial.LastModified) || !aws.ToTime(unchangedAlarms[0].AlarmConfigurationUpdatedTimestamp).Equal(*initialAlarms[0].AlarmConfigurationUpdatedTimestamp) {
		t.Fatalf("re-ensure changed converged monitoring: %v", err)
	}
	// Removal is explicit at the mapping owner; then the full desired preview
	// must accept enabling EventCount before reconciling the retained alarm.
	originalAttrs := slices.Clone(processor.Trigger[0].Attr)
	processor.Trigger[0].Attr = slices.DeleteFunc(slices.Clone(originalAttrs), func(attr string) bool { return attr == "metrics=event_count" })
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, true); err != nil {
		t.Fatal(err)
	}
	if len(lambdaMappingMetrics(getMapping().MetricsConfig)) != 1 {
		t.Fatal("preview removed metrics")
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	if len(lambdaMappingMetrics(getMapping().MetricsConfig)) != 0 {
		t.Fatal("metrics omission did not disable EventCount")
	}
	processor.Trigger[0].Attr = originalAttrs
	ensureCLI(true)
	if len(lambdaMappingMetrics(getMapping().MetricsConfig)) != 0 {
		t.Fatal("preview enabled metrics")
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	// A retained-event alarm needs a destination, not just enabled metrics.
	processor.Trigger[0].Attr = slices.DeleteFunc(slices.Clone(originalAttrs), func(attr string) bool { return strings.HasPrefix(attr, "on-failure=") })
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	beforePolicy, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{FunctionName: aws.String(notifier)})
	if err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{true, false} {
		if err := InfraEnsure(ctx, infra, "", preview, false); err == nil || !strings.Contains(err.Error(), "requires an on-failure destination") {
			t.Fatalf("declaration without retention passed preflight: %v", err)
		}
		if _, err := LambdaEnsureTriggerAlarm(ctx, notification, preview); err == nil || !strings.Contains(err.Error(), "requires an on-failure destination") {
			t.Fatalf("live mapping without retention accepted: %v", err)
		}
	}
	ensureCLI(true) // The unchanged declaration plans to restore the destination.
	if dynamoDBFailureDestination(getMapping().DestinationConfig) != "" {
		t.Fatal("preview restored the failure destination")
	}
	afterPolicy, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{FunctionName: aws.String(notifier)})
	if err != nil || aws.ToString(afterPolicy.RevisionId) != aws.ToString(beforePolicy.RevisionId) {
		t.Fatalf("rejected alarm or preview changed invocation permissions: %v", err)
	}
	unchangedAlarms, err = cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), []string{retained})
	if err != nil || len(unchangedAlarms) != 1 || !aws.ToTime(unchangedAlarms[0].AlarmConfigurationUpdatedTimestamp).Equal(*initialAlarms[0].AlarmConfigurationUpdatedTimestamp) {
		t.Fatalf("rejected alarm or preview changed alarm configuration: %v", err)
	}
	processor.Trigger[0].Attr = originalAttrs
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	// Replace the ephemeral mapping without changing its declarative identity.
	if _, err := LambdaClient().DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{UUID: initial.UUID}); err != nil {
		t.Fatal(err)
	}
	if err := lambdaWaitEventSourceMappingDeleted(ctx, LambdaClient(), aws.ToString(initial.UUID)); err != nil {
		t.Fatal(err)
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	replacement := getMapping()
	if aws.ToString(replacement.UUID) == aws.ToString(initial.UUID) {
		t.Fatal("mapping was not replaced")
	}
	if _, err := LambdaEnsureTriggerAlarm(ctx, notification, true); err != nil {
		t.Fatal(err)
	}
	alarms, err := cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), []string{retained})
	if err != nil || len(alarms) != 1 || aws.ToString(alarms[0].Dimensions[0].Value) != aws.ToString(initial.UUID) {
		t.Fatalf("preview mutated alarm: %v %v", alarms, err)
	}
	if _, err := LambdaEnsureTriggerAlarm(ctx, notification, false); err != nil {
		t.Fatal(err)
	}
	assertInventory()
	decoyConfig := &lambdaAlarmConfig{name: decoy, monitoredLambdaName: name, metric: lambdaAlarmMetricDeliveryFailures, threshold: 1000}
	if _, err := CloudwatchClient().PutMetricAlarm(ctx, decoyConfig.putInput(notification.Arn, name+"-other", true)); err != nil {
		t.Fatal(err)
	}
	if _, err := LambdaEnsureTriggerAlarm(ctx, notification, false); err != nil {
		t.Fatal(err)
	}
	alarms, err = cloudwatchDescribeMetricAlarms(ctx, CloudwatchClient(), []string{decoy})
	if err != nil || len(alarms) != 1 {
		t.Fatalf("unrelated alarm was lost: %v %v", alarms, err)
	}
	put := func(marker string) {
		t.Helper()
		_, err := DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(name), Item: map[string]ddbtypes.AttributeValue{"id": &ddbtypes.AttributeValueMemberS{Value: marker}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	observe := func(alarmName string, start time.Time) {
		t.Helper()
		for _, trigger := range notification.Trigger {
			config, err := parseLambdaAlarmTrigger(trigger)
			if err != nil {
				t.Fatal(err)
			}
			if config.name == alarmName {
				config.mappingUUID = aws.ToString(replacement.UUID)
				waitRetentionObservation(t, ctx, notifier, config.metricAlarm(notification.Arn), start)
				return
			}
		}
		t.Fatalf("missing alarm declaration: %s", alarmName)
	}
	// Accumulate a backlog without errors or EventCount metrics.
	processor.Trigger[0].Attr = slices.DeleteFunc(slices.Clone(originalAttrs), func(attr string) bool { return attr == "metrics=event_count" })
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	if _, err := LambdaClient().UpdateEventSourceMapping(ctx, &lambda.UpdateEventSourceMappingInput{UUID: replacement.UUID, Enabled: aws.Bool(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := lambdaWaitEventSourceMappingSettled(ctx, LambdaClient(), aws.ToString(replacement.UUID)); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Minute)
	put("backlog-" + uid)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(30 * time.Second):
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	observe(age, start)
	logs, err := LogsRecent(ctx, "/aws/lambda/"+name, 100)
	if err != nil || !strings.Contains(strings.Join(logs, "\n"), "processed backlog-"+uid) {
		t.Fatalf("backlogged record was not processed successfully: %v %v", logs, err)
	}
	processor.Trigger[0].Attr = originalAttrs
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	// Metrics activation can lag behind the mapping's Enabled state.
	waitRetentionMetrics(t, ctx, aws.ToString(replacement.UUID))
	start = time.Now().UTC().Add(-time.Minute)
	put("retained-" + uid)
	observe(retained, start)
	observe(invocationErrors, start)
	objects, err := S3Client().ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(name)})
	if err != nil || len(objects.Contents) == 0 {
		t.Fatalf("no retained payload: %v %v", objects, err)
	}
	foundPayload := false
	for _, object := range objects.Contents {
		out, err := S3Client().GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(name), Key: object.Key})
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(out.Body)
		closeErr := out.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		if strings.Contains(string(data), "retained-"+uid) && strings.Contains(string(data), "RetryAttemptsExhausted") {
			foundPayload = true
		}
	}
	if !foundPayload {
		t.Fatal("metric was not accompanied by the exhausted event's S3 payload")
	}
	// Deny only this role's S3 writes, after AWS has validated the destination.
	// Leave EventCount disabled to prove delivery failures do not require opt-in.
	processor.Trigger[0].Attr = slices.DeleteFunc(slices.Clone(originalAttrs), func(attr string) bool { return attr == "metrics=event_count" })
	if err := LambdaEnsureTriggerDynamoDB(ctx, processor, false); err != nil {
		t.Fatal(err)
	}
	roleARN, err := IamRoleArn(ctx, "lambda", name)
	if err != nil {
		t.Fatal(err)
	}
	policy := Json(map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{
		"Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::" + name + "/*",
		"Condition": map[string]any{"ArnEquals": map[string]string{"aws:PrincipalArn": roleARN}},
	}}})
	if _, err := S3Client().PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(name), Policy: aws.String(policy)}); err != nil {
		t.Fatal(err)
	}
	start = time.Now().UTC().Add(-time.Minute)
	put("denied-" + uid)
	observe(failed, start)
	if _, err := S3Client().DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(name)}); err != nil {
		t.Fatal(err)
	}
}

func waitRetentionObservation(t *testing.T, ctx context.Context, notifier string, expected *cwtypes.MetricAlarm, start time.Time) {
	t.Helper()
	alarm, metric := aws.ToString(expected.AlarmName), aws.ToString(expected.MetricName)
	deadline := time.Now().Add(6 * time.Minute)
	metricSeen, alarmSeen, notificationSeen := false, false, false
	for {
		data, err := CloudwatchClient().GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
			Namespace: aws.String("AWS/Lambda"), MetricName: aws.String(metric),
			Dimensions: expected.Dimensions,
			StartTime:  aws.Time(start), EndTime: aws.Time(time.Now().UTC()), Period: aws.Int32(60), Statistics: []cwtypes.Statistic{expected.Statistic},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, point := range data.Datapoints {
			value := aws.ToFloat64(point.Sum)
			if expected.Statistic == cwtypes.StatisticMaximum {
				value = aws.ToFloat64(point.Maximum)
			}
			if value >= aws.ToFloat64(expected.Threshold) {
				metricSeen = true
			}
		}
		history, err := CloudwatchClient().DescribeAlarmHistory(ctx, &cloudwatch.DescribeAlarmHistoryInput{AlarmName: aws.String(alarm), StartDate: aws.Time(start), HistoryItemType: cwtypes.HistoryItemTypeStateUpdate})
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range history.AlarmHistoryItems {
			var state struct {
				NewState struct {
					StateValue string `json:"stateValue"`
				} `json:"newState"`
			}
			if json.Unmarshal([]byte(aws.ToString(event.HistoryData)), &state) != nil {
				t.Fatal("invalid alarm history")
			}
			if state.NewState.StateValue == "ALARM" {
				alarmSeen = true
			}
		}
		logs, err := LogsRecent(ctx, "/aws/lambda/"+notifier, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range logs {
			if strings.Contains(line, alarm) && strings.Contains(line, "ALARM") {
				notificationSeen = true
			}
		}
		if metricSeen && alarmSeen && notificationSeen {
			t.Logf("observed %s {%s=%s}: %s >= %g, ALARM transition, CloudWatch → %s delivery", metric, aws.ToString(expected.Dimensions[0].Name), aws.ToString(expected.Dimensions[0].Value), expected.Statistic, aws.ToFloat64(expected.Threshold), notifier)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s for %s: metric=%t alarm=%t notification=%t after bounded wait", metric, alarm, metricSeen, alarmSeen, notificationSeen)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}

func waitRetentionMetrics(t *testing.T, ctx context.Context, mappingUUID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	since := time.Now().UTC()
	for {
		data, err := CloudwatchClient().GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
			Namespace: aws.String("AWS/Lambda"), MetricName: aws.String("PolledEventCount"),
			Dimensions: []cwtypes.Dimension{{Name: aws.String("EventSourceMappingUUID"), Value: aws.String(mappingUUID)}},
			StartTime:  aws.Time(since.Add(-time.Minute)), EndTime: aws.Time(time.Now().UTC()), Period: aws.Int32(60), Statistics: []cwtypes.Statistic{cwtypes.StatisticSum},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, point := range data.Datapoints {
			// Empty polls emit zero; require a new datapoint, not a positive count.
			if aws.ToTime(point.Timestamp).After(since) {
				t.Log("EventCount metrics active on", mappingUUID)
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("EventCount metrics did not become visible:", ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}
