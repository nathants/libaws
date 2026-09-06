package lib

import (
	"context"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/gofrs/uuid"
)

type cancelAfterEventRemoval struct {
	*eventbridge.Client
	cancel context.CancelFunc
}

func (client *cancelAfterEventRemoval) RemoveTargets(ctx context.Context, input *eventbridge.RemoveTargetsInput, options ...func(*eventbridge.Options)) (*eventbridge.RemoveTargetsOutput, error) {
	out, err := client.Client.RemoveTargets(ctx, input, options...)
	if err == nil && out.FailedEntryCount == 0 && len(out.FailedEntries) == 0 {
		client.cancel()
	}
	return out, err
}

func TestEventBridgeRollbackIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := "test-rollback-" + uuid.Must(uuid.NewV4()).String()
	client := EventsClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		var absent *eventtypes.ResourceNotFoundException
		out, err := client.RemoveTargets(cleanupCtx, &eventbridge.RemoveTargetsInput{Rule: aws.String(name), Ids: []string{"1"}})
		if err != nil && !errors.As(err, &absent) {
			t.Errorf("remove target fixture: %v", err)
		} else if err == nil && (out.FailedEntryCount != 0 || len(out.FailedEntries) != 0) {
			t.Errorf("remove target fixture: %s", Json(out.FailedEntries))
		}
		if _, err := client.DeleteRule(cleanupCtx, &eventbridge.DeleteRuleInput{Name: aws.String(name)}); err != nil && !errors.As(err, &absent) {
			t.Errorf("delete rule fixture: %v", err)
		}
		if _, err := client.DescribeRule(cleanupCtx, &eventbridge.DescribeRuleInput{Name: aws.String(name)}); !errors.As(err, &absent) {
			t.Errorf("rule fixture remains or deletion could not be verified: %v", err)
		}
	})
	// Disabled and in the distant future; no target function or invocation permission.
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String(name), State: eventtypes.RuleStateDisabled, ScheduleExpression: aws.String("cron(0 0 1 1 ? 2099)")}); err != nil {
		t.Fatal(err)
	}
	target := eventtypes.Target{Id: aws.String("1"), Arn: aws.String(lambdaFunctionARN("aws", Region(), os.Getenv("LIBAWS_TEST_ACCOUNT"), name)), Input: aws.String(`{"fixture":true}`)}
	if err := lambdaPutEventTargets(ctx, client, name, []eventtypes.Target{target}); err != nil {
		t.Fatal(err)
	}
	deleteCtx, deleteCancel := context.WithCancel(ctx)
	defer deleteCancel()
	err := lambdaDeleteEventRule(deleteCtx, &cancelAfterEventRemoval{Client: client, cancel: deleteCancel}, &eventtypes.Rule{Name: aws.String(name)}, target, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled deletion, got %v", err)
	}
	targets, err := lambdaEventTargets(ctx, client, aws.String(name))
	if err != nil || !reflect.DeepEqual(targets, []eventtypes.Target{target}) {
		t.Fatalf("real target not restored exactly: targets=%s err=%v", Json(targets), err)
	}
}

type cancelAfterSESMutation struct {
	*ses.Client
	cancel context.CancelFunc
}

func (client *cancelAfterSESMutation) DeleteReceiptRule(ctx context.Context, input *ses.DeleteReceiptRuleInput, options ...func(*ses.Options)) (*ses.DeleteReceiptRuleOutput, error) {
	out, err := client.Client.DeleteReceiptRule(ctx, input, options...)
	if err == nil {
		client.cancel()
	}
	return out, err
}

func (client *cancelAfterSESMutation) CreateReceiptRuleSet(ctx context.Context, input *ses.CreateReceiptRuleSetInput, options ...func(*ses.Options)) (*ses.CreateReceiptRuleSetOutput, error) {
	out, err := client.Client.CreateReceiptRuleSet(ctx, input, options...)
	if err == nil {
		client.cancel()
	}
	return out, err
}

func TestSESEmptySetRollbackIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := "test-rollback-" + uuid.Must(uuid.NewV4()).String()
	client := SesClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if _, err := client.DeleteReceiptRuleSet(cleanupCtx, &ses.DeleteReceiptRuleSetInput{RuleSetName: aws.String(name)}); err != nil && !sesRuleSetDoesNotExist(err) {
			t.Errorf("delete empty SES fixture: %v", err)
		}
		if _, err := client.DescribeReceiptRuleSet(cleanupCtx, &ses.DescribeReceiptRuleSetInput{RuleSetName: aws.String(name)}); !sesRuleSetDoesNotExist(err) {
			t.Errorf("SES fixture remains or deletion could not be verified: %v", err)
		}
	})
	createCtx, createCancel := context.WithCancel(ctx)
	defer createCancel()
	// Cancellation occurs after the real empty set is created, before any rule or activation.
	err := sesEnsureReceiptRulesetState(createCtx, &cancelAfterSESMutation{Client: client, cancel: createCancel}, name, "unused-bucket", "", "unused-function", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled rule creation, got %v", err)
	}
	if _, err := client.DescribeReceiptRuleSet(ctx, &ses.DescribeReceiptRuleSetInput{RuleSetName: aws.String(name)}); !sesRuleSetDoesNotExist(err) {
		t.Fatalf("empty new SES set was not rolled back: %v", err)
	}
}

// Run only inside the exclusive SES example, which owns the active rule set.
func TestSESActiveRuleRollbackIntegration(t *testing.T) {
	domain := os.Getenv("LIBAWS_SES_ROLLBACK_TEST_DOMAIN")
	if domain == "" {
		t.Skip("run through examples/simple/go/ses")
	}
	if !regexp.MustCompile(`^test-ses-[0-9a-f]{12}\.`).MatchString(domain) || !strings.HasSuffix(domain, "."+os.Getenv("LIBAWS_TEST_DOMAIN")) {
		t.Fatal("requires unique SES example domain")
	}
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := SesClient()
	before, err := client.DescribeReceiptRuleSet(ctx, &ses.DescribeReceiptRuleSetInput{RuleSetName: aws.String(domain)})
	if err != nil || len(before.Rules) != 1 || aws.ToString(before.Rules[0].Name) != domain {
		t.Fatalf("expected one owned SES rule: out=%v err=%v", before, err)
	}
	active, err := sesActiveReceiptRuleset(ctx, client)
	if err != nil || active != domain {
		t.Fatalf("expected owned active SES fixture: %s err=%v", active, err)
	}
	deleteCtx, deleteCancel := context.WithCancel(ctx)
	defer deleteCancel()
	err = sesRmReceiptRuleset(deleteCtx, &cancelAfterSESMutation{Client: client, cancel: deleteCancel}, domain, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled set deletion, got %v", err)
	}
	after, err := client.DescribeReceiptRuleSet(ctx, &ses.DescribeReceiptRuleSetInput{RuleSetName: aws.String(domain)})
	if err != nil || !reflect.DeepEqual(before.Rules, after.Rules) {
		t.Fatalf("original real SES rule not restored: out=%v err=%v", after, err)
	}
	active, err = sesActiveReceiptRuleset(ctx, client)
	if err != nil || active != domain {
		t.Fatalf("active SES fixture not restored: %s err=%v", active, err)
	}
	// The example continues through its normal owned-resource teardown.
}
