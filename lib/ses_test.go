package lib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sesv2 "github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type fakeSesReceiptRuleCleanupClient struct {
	ruleSets        []sestypes.ReceiptRuleSetMetadata
	rules           []sestypes.ReceiptRule
	active          string
	listErr         error
	describeErr     error
	activeErr       error
	createSetErr    error
	createRuleErr   error
	updateRuleErr   error
	deleteRuleErr   error
	setActiveErr    error
	deleteSetErr    error
	calls           []string
	setActiveInput  *sesv2.SetActiveReceiptRuleSetInput
	deletedRuleSets []string
	createdRule     *sestypes.ReceiptRule
	updatedRule     *sestypes.ReceiptRule
}

func (client *fakeSesReceiptRuleCleanupClient) ListReceiptRuleSets(
	context.Context,
	*sesv2.ListReceiptRuleSetsInput,
	...func(*sesv2.Options),
) (*sesv2.ListReceiptRuleSetsOutput, error) {
	client.calls = append(client.calls, "list")
	if client.listErr != nil {
		return nil, client.listErr
	}
	return &sesv2.ListReceiptRuleSetsOutput{RuleSets: client.ruleSets}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) CreateReceiptRuleSet(
	context.Context,
	*sesv2.CreateReceiptRuleSetInput,
	...func(*sesv2.Options),
) (*sesv2.CreateReceiptRuleSetOutput, error) {
	client.calls = append(client.calls, "create-set")
	if client.createSetErr != nil {
		return nil, client.createSetErr
	}
	return &sesv2.CreateReceiptRuleSetOutput{}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) CreateReceiptRule(
	_ context.Context,
	input *sesv2.CreateReceiptRuleInput,
	_ ...func(*sesv2.Options),
) (*sesv2.CreateReceiptRuleOutput, error) {
	client.calls = append(client.calls, "create-rule")
	client.createdRule = input.Rule
	if client.createRuleErr != nil {
		return nil, client.createRuleErr
	}
	return &sesv2.CreateReceiptRuleOutput{}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) UpdateReceiptRule(
	_ context.Context,
	input *sesv2.UpdateReceiptRuleInput,
	_ ...func(*sesv2.Options),
) (*sesv2.UpdateReceiptRuleOutput, error) {
	client.calls = append(client.calls, "update-rule")
	client.updatedRule = input.Rule
	if client.updateRuleErr != nil {
		return nil, client.updateRuleErr
	}
	return &sesv2.UpdateReceiptRuleOutput{}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) DescribeActiveReceiptRuleSet(
	context.Context,
	*sesv2.DescribeActiveReceiptRuleSetInput,
	...func(*sesv2.Options),
) (*sesv2.DescribeActiveReceiptRuleSetOutput, error) {
	client.calls = append(client.calls, "describe-active")
	if client.activeErr != nil {
		return nil, client.activeErr
	}
	out := &sesv2.DescribeActiveReceiptRuleSetOutput{}
	if client.active != "" {
		out.Metadata = &sestypes.ReceiptRuleSetMetadata{Name: aws.String(client.active)}
	}
	return out, nil
}

func (client *fakeSesReceiptRuleCleanupClient) DescribeReceiptRuleSet(
	context.Context,
	*sesv2.DescribeReceiptRuleSetInput,
	...func(*sesv2.Options),
) (*sesv2.DescribeReceiptRuleSetOutput, error) {
	client.calls = append(client.calls, "describe")
	if client.describeErr != nil {
		return nil, client.describeErr
	}
	return &sesv2.DescribeReceiptRuleSetOutput{Rules: client.rules}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) SetActiveReceiptRuleSet(
	_ context.Context,
	input *sesv2.SetActiveReceiptRuleSetInput,
	_ ...func(*sesv2.Options),
) (*sesv2.SetActiveReceiptRuleSetOutput, error) {
	call := "deactivate"
	if input.RuleSetName != nil {
		call = "activate"
	}
	client.calls = append(client.calls, call)
	client.setActiveInput = input
	if client.setActiveErr != nil {
		return nil, client.setActiveErr
	}
	return &sesv2.SetActiveReceiptRuleSetOutput{}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) DeleteReceiptRule(
	context.Context,
	*sesv2.DeleteReceiptRuleInput,
	...func(*sesv2.Options),
) (*sesv2.DeleteReceiptRuleOutput, error) {
	client.calls = append(client.calls, "delete-rule")
	if client.deleteRuleErr != nil {
		return nil, client.deleteRuleErr
	}
	return &sesv2.DeleteReceiptRuleOutput{}, nil
}

func (client *fakeSesReceiptRuleCleanupClient) DeleteReceiptRuleSet(
	_ context.Context,
	input *sesv2.DeleteReceiptRuleSetInput,
	_ ...func(*sesv2.Options),
) (*sesv2.DeleteReceiptRuleSetOutput, error) {
	client.calls = append(client.calls, "delete-set")
	client.deletedRuleSets = append(client.deletedRuleSets, aws.ToString(input.RuleSetName))
	if client.deleteSetErr != nil {
		return nil, client.deleteSetErr
	}
	return &sesv2.DeleteReceiptRuleSetOutput{}, nil
}

func fakeSesRuleSet(domain string) *fakeSesReceiptRuleCleanupClient {
	return &fakeSesReceiptRuleCleanupClient{
		ruleSets: []sestypes.ReceiptRuleSetMetadata{{Name: aws.String(domain)}},
		rules:    []sestypes.ReceiptRule{{Name: aws.String(domain)}},
	}
}

func TestInfraParseRejectsMultipleSESTriggers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.yaml")
	data := `name: mail
lambda:
  first:
    entrypoint: main.go
    trigger:
      - type: ses
        attr:
          - dns=first.example
          - bucket=first-bucket
  second:
    entrypoint: main.go
    trigger:
      - type: ses
        attr:
          - dns=second.example
          - bucket=second-bucket
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InfraParse(path); err == nil {
		t.Fatal("multiple SES triggers were accepted even though only one rule set can be active")
	}
}

func TestSesReceiptRulePermissionIdentityPreservesPartition(t *testing.T) {
	lambdaName, sourceARN, err := sesReceiptRulePermissionIdentity(
		"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function",
		"mail.example",
		"us-gov-west-1",
		"123456789012",
	)
	if err != nil {
		t.Fatal(err)
	}
	if lambdaName != "function" || sourceARN != "arn:aws-us-gov:ses:us-gov-west-1:123456789012:receipt-rule-set/mail.example:receipt-rule/mail.example" {
		t.Fatalf("permission identity = %q %q", lambdaName, sourceARN)
	}
	if _, _, err := sesReceiptRulePermissionIdentity(
		"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function:alias",
		"mail.example",
		"us-gov-west-1",
		"123456789012",
	); err == nil {
		t.Fatal("qualified Lambda ARN was accepted")
	}
	if _, _, err := sesReceiptRulePermissionIdentity(
		"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function",
		"not-a-domain",
		"us-gov-west-1",
		"123456789012",
	); err == nil {
		t.Fatal("invalid SES recipient domain was accepted")
	}
}

func TestSesEnsureReceiptRulesetCreatesRuleBeforeActivation(t *testing.T) {
	client := &fakeSesReceiptRuleCleanupClient{}
	if err := sesEnsureReceiptRulesetState(
		context.Background(),
		client,
		"mail.example",
		"mail-bucket",
		"emails/",
		"arn:aws:lambda:us-west-2:123456789012:function:function",
		false,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "create-set", "create-rule", "describe-active", "activate"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
	if !reflect.DeepEqual(
		client.createdRule,
		sesDesiredReceiptRule(
			"mail.example",
			"mail-bucket",
			"emails/",
			"arn:aws:lambda:us-west-2:123456789012:function:function",
		),
	) {
		t.Fatalf("created rule = %#v", client.createdRule)
	}
	if client.setActiveInput == nil || aws.ToString(client.setActiveInput.RuleSetName) != "mail.example" {
		t.Fatalf("active rule set input = %#v", client.setActiveInput)
	}
}

func TestSesEnsureReceiptRulesetRollsBackEmptyNewSetAfterRuleFailure(t *testing.T) {
	failure := errors.New("invalid rule")
	client := &fakeSesReceiptRuleCleanupClient{createRuleErr: failure}
	err := sesEnsureReceiptRulesetState(
		context.Background(),
		client,
		"mail.example",
		"mail-bucket",
		"emails/",
		"arn:aws:lambda:us-west-2:123456789012:function:function",
		false,
	)
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want %v", err, failure)
	}
	want := []string{"list", "create-set", "create-rule", "delete-set"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
	if !slices.Equal(client.deletedRuleSets, []string{"mail.example"}) {
		t.Fatalf("deleted rule sets = %v", client.deletedRuleSets)
	}
}

func TestSesEnsureReceiptRulesetRejectsUnmanagedRulesBeforeMutation(t *testing.T) {
	domain := "mail.example"
	desired := sesDesiredReceiptRule(
		domain,
		"mail-bucket",
		"emails/",
		"arn:aws:lambda:us-west-2:123456789012:function:function",
	)
	client := &fakeSesReceiptRuleCleanupClient{
		ruleSets: []sestypes.ReceiptRuleSetMetadata{{Name: aws.String(domain)}},
		rules: []sestypes.ReceiptRule{
			*desired,
			{Name: aws.String("unmanaged")},
		},
		active: "unrelated.example",
	}
	if err := sesEnsureReceiptRulesetState(
		context.Background(), client, domain, "mail-bucket", "emails/",
		"arn:aws:lambda:us-west-2:123456789012:function:function", false,
	); err == nil {
		t.Fatal("SES rule set with an unmanaged rule was accepted")
	}
	if !slices.Equal(client.calls, []string{"list", "describe"}) {
		t.Fatalf("calls = %v, want read-only rule-set inspection", client.calls)
	}
	if client.createdRule != nil || client.updatedRule != nil || client.setActiveInput != nil {
		t.Fatalf("unmanaged SES rule set was mutated: created=%#v updated=%#v active=%#v", client.createdRule, client.updatedRule, client.setActiveInput)
	}
}

func TestSesEnsureReceiptRulesetConvergesExistingRuleBeforeActivation(t *testing.T) {
	oldRule := sesDesiredReceiptRule(
		"mail.example",
		"old-bucket",
		"old/",
		"arn:aws:lambda:us-west-2:123456789012:function:function",
	)
	client := &fakeSesReceiptRuleCleanupClient{
		ruleSets: []sestypes.ReceiptRuleSetMetadata{{Name: aws.String("mail.example")}},
		rules:    []sestypes.ReceiptRule{*oldRule},
		active:   "unrelated.example",
	}
	if err := sesEnsureReceiptRulesetState(
		context.Background(),
		client,
		"mail.example",
		"mail-bucket",
		"emails/",
		"arn:aws:lambda:us-west-2:123456789012:function:function",
		false,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "describe", "update-rule", "describe-active", "activate"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
	if aws.ToString(client.updatedRule.Actions[0].S3Action.BucketName) != "mail-bucket" {
		t.Fatalf("updated rule = %#v", client.updatedRule)
	}
}

func TestSesRmReceiptRulesetIsIdempotentWhenAbsent(t *testing.T) {
	client := &fakeSesReceiptRuleCleanupClient{}
	if err := sesRmReceiptRuleset(context.Background(), client, "mail.example", false); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 || client.calls[0] != "list" {
		t.Fatalf("calls = %v, want [list]", client.calls)
	}
}

func TestSesRmReceiptRulesetClearsOnlyMatchingActiveSet(t *testing.T) {
	client := fakeSesRuleSet("mail.example")
	client.active = "mail.example"
	if err := sesRmReceiptRuleset(context.Background(), client, "mail.example", false); err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "describe", "describe-active", "deactivate", "delete-rule", "delete-set"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
	if client.setActiveInput == nil || client.setActiveInput.RuleSetName != nil {
		t.Fatalf("SetActiveReceiptRuleSet input = %#v, want nil RuleSetName", client.setActiveInput)
	}

	client = fakeSesRuleSet("mail.example")
	client.active = "unrelated.example"
	if err := sesRmReceiptRuleset(context.Background(), client, "mail.example", false); err != nil {
		t.Fatal(err)
	}
	want = []string{"list", "describe", "describe-active", "delete-rule", "delete-set"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls with unrelated active set = %v, want %v", client.calls, want)
	}
}

func TestSesRmReceiptRulesetPreservesUnexpectedRules(t *testing.T) {
	client := fakeSesRuleSet("mail.example")
	client.rules = append(client.rules, sestypes.ReceiptRule{Name: aws.String("unrelated")})
	if err := sesRmReceiptRuleset(context.Background(), client, "mail.example", false); err == nil {
		t.Fatal("unexpected rule set shape was deleted")
	}
	want := []string{"list", "describe"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
}

func TestSesRmReceiptRulesetPreviewDoesNotMutate(t *testing.T) {
	client := fakeSesRuleSet("mail.example")
	client.active = "mail.example"
	if err := sesRmReceiptRuleset(context.Background(), client, "mail.example", true); err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "describe", "describe-active"}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("preview calls = %v, want %v", client.calls, want)
	}
}

func TestLambdaCleanupSESTriggersAfterFunctionDeletionPreservesUnownedRuleSets(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	client := &fakeSesReceiptRuleCleanupClient{
		ruleSets: []sestypes.ReceiptRuleSetMetadata{
			{Name: aws.String("mail.example")},
			{Name: aws.String("unrelated.example")},
		},
		active: "mail.example",
		rules: []sestypes.ReceiptRule{{
			Name:        aws.String("mail.example"),
			Enabled:     true,
			TlsPolicy:   sestypes.TlsPolicyRequire,
			Recipients:  []string{"mail.example"},
			ScanEnabled: false,
			Actions: []sestypes.ReceiptAction{
				{S3Action: &sestypes.S3Action{BucketName: aws.String("mail-bucket")}},
				{LambdaAction: &sestypes.LambdaAction{
					FunctionArn:    aws.String(functionARN),
					InvocationType: sestypes.InvocationTypeEvent,
				}},
			},
		}},
	}
	identity := lambdaIdentity{name: "function", arn: functionARN, infraSetName: "set", exists: false}
	if err := lambdaCleanupSESTriggers(context.Background(), client, identity, nil, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.deletedRuleSets, []string{"mail.example"}) {
		t.Fatalf("deleted SES rule sets = %v, want [mail.example]", client.deletedRuleSets)
	}
}

func TestSesRmReceiptRulesetRollsBackAfterDeleteSetFailure(t *testing.T) {
	failure := errors.New("delete set failed")
	client := fakeSesRuleSet("mail.example")
	client.active = "mail.example"
	originalRule := client.rules[0]
	client.deleteSetErr = failure

	err := sesRmReceiptRuleset(context.Background(), client, "mail.example", false)
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want %v", err, failure)
	}
	want := []string{
		"list",
		"describe",
		"describe-active",
		"deactivate",
		"delete-rule",
		"delete-set",
		"create-rule",
		"activate",
	}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
	if !reflect.DeepEqual(client.createdRule, &originalRule) {
		t.Fatalf("restored rule = %#v, want %#v", client.createdRule, originalRule)
	}
	if client.setActiveInput == nil || aws.ToString(client.setActiveInput.RuleSetName) != "mail.example" {
		t.Fatalf("restored active rule set input = %#v", client.setActiveInput)
	}
}

func TestSesRmReceiptRulesetPropagatesMutationErrors(t *testing.T) {
	failure := errors.New("provider failure")
	tests := []struct {
		name string
		set  func(*fakeSesReceiptRuleCleanupClient)
	}{
		{"delete rule", func(client *fakeSesReceiptRuleCleanupClient) { client.deleteRuleErr = failure }},
		{"deactivate", func(client *fakeSesReceiptRuleCleanupClient) { client.setActiveErr = failure }},
		{"delete set", func(client *fakeSesReceiptRuleCleanupClient) { client.deleteSetErr = failure }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := fakeSesRuleSet("mail.example")
			client.active = "mail.example"
			test.set(client)
			if err := sesRmReceiptRuleset(context.Background(), client, "mail.example", false); !errors.Is(err, failure) {
				t.Fatalf("error = %v, want %v", err, failure)
			}
		})
	}
}
