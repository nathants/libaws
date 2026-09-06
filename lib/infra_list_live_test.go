package lib

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
)

// Hooks only control request timing or page size; every response comes from AWS.
func infraListLiveHook(name string, handler func(context.Context, middleware.InitializeInput, middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error)) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc(name, handler), middleware.Before)
	}
}

func infraListLiveWait(ctx context.Context, ready func() (bool, error)) error {
	for {
		ok, err := ready()
		if ok || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// examples/misc/infrasets owns all fixtures and invokes this test before teardown.
func TestInfraListSetIntegration(t *testing.T) {
	uid := os.Getenv("LIBAWS_INFRALIST_TEST_UID")
	if uid == "" {
		t.Skip("run through examples/misc/infrasets to provide owned fixtures")
	}
	requireLiveAWSAccount(t)
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("inventory integration requires a unique 12-digit hexadecimal fixture ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	first, second := "test-owned-"+uid, "test-owned-"+uid+"-other"
	function := "test-worker-" + uid
	functionOut, err := LambdaClient().GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(function)})
	if err != nil {
		t.Fatal(err)
	}
	functionTags, err := LambdaClient().ListTags(ctx, &lambda.ListTagsInput{Resource: functionOut.FunctionArn})
	if err != nil || functionTags.Tags[infraSetTagName] != first {
		t.Fatalf("function is not owned by the inventory fixture: %v", err)
	}

	t.Run("bucket lifecycle", func(t *testing.T) {
		// Keep destructive lifecycle mutations separate from the example's other
		// fixtures, so stale restoration reads cannot affect unrelated subtests.
		bucket, setName := "test-inventory-lifecycle-"+uid, first+"-lifecycle"
		client := S3Client()
		_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
		var absent *s3types.NotFound
		if !errors.As(err, &absent) {
			t.Fatalf("lifecycle fixture must not exist: %v", err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cleanupCancel()
			if _, err := client.DeleteBucket(cleanupCtx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)}); err != nil && !isS3NoSuchBucket(err) {
				t.Errorf("remove empty lifecycle bucket: %v", err)
				return
			}
			if err := infraListLiveWait(cleanupCtx, func() (bool, error) {
				_, err := client.GetBucketLocation(cleanupCtx, &s3.GetBucketLocationInput{Bucket: aws.String(bucket)})
				if isS3NoSuchBucket(err) {
					return true, nil
				}
				return false, err
			}); err != nil {
				t.Errorf("verify lifecycle bucket deletion: %v", err)
			}
		})
		input, err := S3EnsureInput(setName, bucket, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := S3Ensure(ctx, input, false); err != nil {
			t.Fatal(err)
		}
		regional, err := S3ClientBucketRegion(ctx, bucket)
		if err != nil {
			t.Fatal(err)
		}
		var observed bool
		var observedRules []s3types.LifecycleRule
		options := regional.Options()
		options.APIOptions = append(slices.Clone(options.APIOptions), infraListLiveHook("observe inventoried lifecycle", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
			out, metadata, err := next.HandleInitialize(ctx, in)
			if input, ok := in.Parameters.(*s3.GetBucketLifecycleConfigurationInput); ok && aws.ToString(input.Bucket) == bucket {
				observed = true
				if result, ok := out.Result.(*s3.GetBucketLifecycleConfigurationOutput); ok {
					observedRules = result.Rules
				}
			}
			return out, metadata, err
		}))
		s3ClientLock.Lock()
		s3ClientsRegional[options.Region] = s3.New(options)
		s3ClientLock.Unlock()
		defer func() {
			s3ClientLock.Lock()
			s3ClientsRegional[options.Region] = regional
			s3ClientLock.Unlock()
		}()
		// Assert against the exact real AWS response consumed by inventory. A
		// new response followed by an old one is legal during S3 propagation;
		// only fixture readiness is retried, never a failed behavior assertion.
		awaitInventory := func(matches func([]s3types.LifecycleRule) bool) {
			t.Helper()
			err := infraListLiveWait(ctx, func() (bool, error) {
				observed, observedRules = false, nil
				inventory, inventoryErr := InfraListSet(ctx, setName, false)
				if !observed {
					return false, inventoryErr // membership has not propagated yet
				}
				unsupported := len(observedRules) == 1 && slices.Contains([]string{"date", "multipart"}, aws.ToString(observedRules[0].ID))
				if unsupported {
					if inventoryErr == nil || !strings.Contains(inventoryErr.Error(), "unsupported lifecycle configuration") || !strings.Contains(inventoryErr.Error(), bucket) {
						t.Fatalf("inventory accepted its observed unsupported lifecycle: rules=%s err=%v", Pformat(observedRules), inventoryErr)
					}
				} else if inventoryErr != nil {
					return false, inventoryErr
				} else {
					set := inventory.InfraSet[setName]
					if set == nil || set.S3[bucket] == nil {
						t.Fatal("inventory omitted the selected lifecycle fixture")
					}
					if len(observedRules) == 1 && observedRules[0].Expiration != nil && aws.ToInt32(observedRules[0].Expiration.Days) == 1 && !slices.Contains(set.S3[bucket].Attr, "ttldays=1") {
						t.Fatal("inventory did not represent the observed day-based expiration")
					}
				}
				return matches(observedRules), nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		date := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(24 * time.Hour)
		for _, rule := range []s3types.LifecycleRule{
			{ID: aws.String("date"), Status: s3types.ExpirationStatusEnabled, Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String("")}, Expiration: &s3types.LifecycleExpiration{Date: &date}},
			{ID: aws.String("multipart"), Status: s3types.ExpirationStatusEnabled, Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String("")}, AbortIncompleteMultipartUpload: &s3types.AbortIncompleteMultipartUpload{DaysAfterInitiation: aws.Int32(7)}},
		} {
			if _, err := client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{Bucket: aws.String(bucket), LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{Rules: []s3types.LifecycleRule{rule}}}); err != nil {
				t.Fatal(err)
			}
			awaitInventory(func(rules []s3types.LifecycleRule) bool {
				return len(rules) == 1 && aws.ToString(rules[0].ID) == aws.ToString(rule.ID)
			})
		}
		// Ensure must converge a real lifecycle without an Expiration field.
		input, err = S3EnsureInput(setName, bucket, []string{"ttldays=1"})
		if err != nil {
			t.Fatal(err)
		}
		if err := S3Ensure(ctx, input, false); err != nil {
			t.Fatal(err)
		}
		awaitInventory(func(rules []s3types.LifecycleRule) bool {
			return len(rules) == 1 && rules[0].Expiration != nil && aws.ToInt32(rules[0].Expiration.Days) == 1
		})
	})

	t.Run("API integration identity", func(t *testing.T) {
		client := ApiClient()
		api, err := Api(ctx, function)
		if err != nil || api.Tags[infraSetTagName] != first {
			t.Fatalf("API is not owned by the inventory fixture: %v", err)
		}
		integrations, err := lambdaAPIIntegrations(ctx, client, api.ApiId)
		if err != nil || len(integrations) != 1 || !lambdaAPIIntegrationMatches(&integrations[0], aws.ToString(functionOut.FunctionArn)) {
			t.Fatalf("unexpected fixture integration: %v", err)
		}
		original := integrations[0]
		update := func(ctx context.Context, kind apitypes.IntegrationType, uri *string) error {
			if _, err := client.UpdateIntegration(ctx, &apigatewayv2.UpdateIntegrationInput{ApiId: api.ApiId, IntegrationId: original.IntegrationId, IntegrationType: kind, IntegrationUri: uri}); err != nil {
				return err
			}
			return infraListLiveWait(ctx, func() (bool, error) {
				out, err := client.GetIntegration(ctx, &apigatewayv2.GetIntegrationInput{ApiId: api.ApiId, IntegrationId: original.IntegrationId})
				return err == nil && out.IntegrationType == kind && aws.ToString(out.IntegrationUri) == aws.ToString(uri), err
			})
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			if err := update(cleanupCtx, original.IntegrationType, original.IntegrationUri); err != nil {
				t.Errorf("restore API integration: %v", err)
			}
		})
		for _, target := range []struct {
			kind apitypes.IntegrationType
			uri  string
		}{
			{apitypes.IntegrationTypeAwsProxy, strings.TrimSuffix(aws.ToString(functionOut.FunctionArn), function) + "test-other-worker-" + uid},
			{apitypes.IntegrationTypeHttpProxy, "https://example.com"},
		} {
			if err := update(ctx, target.kind, aws.String(target.uri)); err != nil {
				t.Fatal(err)
			}
			inventory, err := InfraListSet(ctx, first, false)
			if err != nil {
				t.Fatal(err)
			}
			set := inventory.InfraSet[first]
			if set == nil || set.Api[function] == nil || set.Lambda[function] == nil {
				t.Fatal("unmatched API must remain visible as a standalone resource")
			}
			for _, trigger := range set.Lambda[function].Trigger {
				if trigger.Type == lambdaTriggerApi {
					t.Fatalf("fabricated a Lambda trigger for live integration %s", target.uri)
				}
			}
		}
	})

	t.Run("profile tag pagination", func(t *testing.T) {
		name := "test-profile-" + uid
		client := IamClient()
		tags, err := iamListInstanceProfileTags(ctx, name)
		if err != nil || !(infraListScope{setName: second}).iamTagsMatch(tags) {
			t.Fatalf("profile is not owned by the inventory fixture: %v", err)
		}
		if slices.ContainsFunc(tags, func(tag iamtypes.Tag) bool { return aws.ToString(tag.Key) == "a-inventory-test" }) {
			t.Fatal("unexpected fixture tag")
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			if _, err := client.UntagInstanceProfile(cleanupCtx, &iam.UntagInstanceProfileInput{InstanceProfileName: aws.String(name), TagKeys: []string{"a-inventory-test"}}); err != nil {
				t.Errorf("restore profile tags: %v", err)
			}
		})
		if _, err := client.TagInstanceProfile(ctx, &iam.TagInstanceProfileInput{InstanceProfileName: aws.String(name), Tags: []iamtypes.Tag{{Key: aws.String("a-inventory-test"), Value: aws.String("page-one")}}}); err != nil {
			t.Fatal(err)
		}
		if err := infraListLiveWait(ctx, func() (bool, error) {
			tags, err := iamListInstanceProfileTags(ctx, name)
			return slices.ContainsFunc(tags, func(tag iamtypes.Tag) bool { return aws.ToString(tag.Key) == "a-inventory-test" }), err
		}); err != nil {
			t.Fatal(err)
		}
		var reads atomic.Int32
		options := client.Options()
		options.APIOptions = append(slices.Clone(options.APIOptions), infraListLiveHook("fixture profile pages", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
			if input, ok := in.Parameters.(*iam.ListInstanceProfileTagsInput); ok && aws.ToString(input.InstanceProfileName) == name {
				input.MaxItems = aws.Int32(1)
				reads.Add(1)
			}
			return next.HandleInitialize(ctx, in)
		}))
		iamClient = iam.New(options)
		defer func() { iamClient = client }()
		profiles, err := (infraListScope{setName: second}).listInstanceProfile(ctx)
		if err != nil || profiles[name] == nil || reads.Load() < 2 {
			t.Fatalf("live paginated ownership: profile=%v reads=%d err=%v", profiles[name], reads.Load(), err)
		}
	})

	t.Run("queue deletion identity failure", func(t *testing.T) {
		client, account := STSClient(), stsAccount
		options := client.Options()
		options.Region = "us-east-1" // STS rejects credentials scoped to the wrong signing region.
		options.BaseEndpoint = aws.String("https://sts.us-west-2.amazonaws.com")
		stsClient, stsAccount = sts.New(options), nil
		defer func() { stsClient, stsAccount = client, account }()
		// Preview can never delete a resource, even if the fixture is misconfigured.
		if err := SQSDeleteQueue(ctx, "test-queue-"+uid, true); err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
			t.Fatalf("AWS identity rejection must remain a queue deletion error: %v", err)
		}
	})

	t.Run("selected queue deletion", func(t *testing.T) {
		client := SQSClient()
		name := "test-inventory-race-" + uid
		if _, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)}); !infraListSQSAbsent(err) {
			t.Fatalf("race fixture queue must not exist: %v", err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			out, err := client.GetQueueUrl(cleanupCtx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
			if infraListSQSAbsent(err) {
				return
			}
			if err == nil {
				_, err = client.DeleteQueue(cleanupCtx, &sqs.DeleteQueueInput{QueueUrl: out.QueueUrl})
			}
			if err != nil {
				t.Errorf("remove race queue: %v", err)
			}
		})
		created, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name), Tags: map[string]string{infraSetTagName: second}})
		if err != nil {
			t.Fatal(err)
		}
		if err := infraListLiveWait(ctx, func() (bool, error) {
			urls, err := SQSListQueueUrls(ctx)
			return slices.Contains(urls, aws.ToString(created.QueueUrl)), err
		}); err != nil {
			t.Fatal(err)
		}
		var deleted atomic.Bool
		options := client.Options()
		options.APIOptions = append(slices.Clone(options.APIOptions), infraListLiveHook("delete selected fixture queue", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (out middleware.InitializeOutput, metadata middleware.Metadata, err error) {
			out, metadata, err = next.HandleInitialize(ctx, in)
			input, ok := in.Parameters.(*sqs.ListQueueTagsInput)
			if err != nil || !ok || aws.ToString(input.QueueUrl) != aws.ToString(created.QueueUrl) {
				return
			}
			if _, err = client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: created.QueueUrl}); err != nil {
				return
			}
			err = infraListLiveWait(ctx, func() (bool, error) {
				_, probeErr := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: created.QueueUrl})
				if infraListSQSAbsent(probeErr) {
					return true, nil
				}
				return false, probeErr
			})
			deleted.Store(err == nil)
			return
		}))
		sqsClient = sqs.New(options)
		defer func() { sqsClient = client }()
		if _, err := (infraListScope{setName: second}).listSQS(ctx); !deleted.Load() || !infraListSQSAbsent(err) {
			t.Fatalf("selected AWS queue disappearance must remain an error: deleted=%v err=%v", deleted.Load(), err)
		}
	})

	t.Run("event membership snapshot", func(t *testing.T) {
		client := EventsClient()
		name := "test-inventory-event-" + uid
		_, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{Name: aws.String(name)})
		var absent *eventtypes.ResourceNotFoundException
		if !errors.As(err, &absent) {
			t.Fatalf("race fixture rule must not exist: %v", err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			out, err := client.RemoveTargets(cleanupCtx, &eventbridge.RemoveTargetsInput{Rule: aws.String(name), Ids: []string{"fixture"}})
			if err != nil || (out != nil && out.FailedEntryCount != 0) {
				t.Errorf("remove race rule target: %v, %v", out, err)
			}
			if _, err := client.DeleteRule(cleanupCtx, &eventbridge.DeleteRuleInput{Name: aws.String(name)}); err != nil {
				t.Errorf("remove race rule: %v", err)
			}
		})
		created, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String(name), ScheduleExpression: aws.String("rate(1 hour)"), State: eventtypes.RuleStateDisabled, Tags: []eventtypes.Tag{{Key: aws.String(infraSetTagName), Value: aws.String(first)}}})
		if err != nil {
			t.Fatal(err)
		}
		put, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{Rule: aws.String(name), Targets: []eventtypes.Target{{Id: aws.String("fixture"), Arn: functionOut.FunctionArn}}})
		if err != nil || put.FailedEntryCount != 0 {
			t.Fatalf("create fixture target: %v, %v", put, err)
		}
		if err := infraListLiveWait(ctx, func() (bool, error) {
			rules, err := client.ListRules(ctx, &eventbridge.ListRulesInput{NamePrefix: aws.String(name)})
			if err != nil || !slices.ContainsFunc(rules.Rules, func(rule eventtypes.Rule) bool { return aws.ToString(rule.Name) == name }) {
				return false, err
			}
			targets, err := EventsListRuleTargets(ctx, name, nil)
			return slices.ContainsFunc(targets, func(target eventtypes.Target) bool {
				return aws.ToString(target.Arn) == aws.ToString(functionOut.FunctionArn)
			}), err
		}); err != nil {
			t.Fatal(err)
		}
		var reads atomic.Int32
		options := client.Options()
		options.APIOptions = append(slices.Clone(options.APIOptions), infraListLiveHook("retag selected fixture rule", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (out middleware.InitializeOutput, metadata middleware.Metadata, err error) {
			out, metadata, err = next.HandleInitialize(ctx, in)
			input, ok := in.Parameters.(*eventbridge.ListTagsForResourceInput)
			if err != nil || !ok || aws.ToString(input.ResourceARN) != aws.ToString(created.RuleArn) || reads.Add(1) != 1 {
				return
			}
			if _, err = client.TagResource(ctx, &eventbridge.TagResourceInput{ResourceARN: created.RuleArn, Tags: []eventtypes.Tag{{Key: aws.String(infraSetTagName), Value: aws.String(second)}}}); err != nil {
				return
			}
			err = infraListLiveWait(ctx, func() (bool, error) {
				probe, probeErr := client.ListTagsForResource(ctx, &eventbridge.ListTagsForResourceInput{ResourceARN: created.RuleArn})
				return probeErr == nil && lambdaSDKTagsOwned(probe.Tags, second), probeErr
			})
			return
		}))
		eventsClient = eventbridge.New(options)
		defer func() { eventsClient = client }()
		rules, err := (infraListScope{setName: first}).listEvent(ctx, make(chan *InfraTrigger, 10))
		if err != nil || rules[name] == nil || rules[name].infraSetName != first || reads.Load() != 1 {
			t.Fatalf("live membership snapshot changed: rule=%v reads=%d err=%v", rules[name], reads.Load(), err)
		}
		if rules[name].Target != aws.ToString(functionOut.FunctionArn) {
			t.Fatalf("unexpected rule target: %s", rules[name].Target)
		}
	})
}
