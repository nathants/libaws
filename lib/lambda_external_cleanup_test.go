package lib

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

func TestLambdaFunctionARNPreservesPartition(t *testing.T) {
	got := lambdaFunctionARN("aws-us-gov", "us-gov-west-1", "123456789012", "function")
	want := "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function"
	if got != want {
		t.Fatalf("Lambda ARN = %q, want %q", got, want)
	}
}

func TestLambdaAddPermissionInputPreservesFunctionARNPartition(t *testing.T) {
	functionARN := "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function"
	input := lambdaAddPermissionInput(
		functionARN,
		"sid",
		"lambda.alarms.cloudwatch.amazonaws.com",
		"arn:aws-us-gov:cloudwatch:us-gov-west-1:123456789012:alarm:test",
		"123456789012",
	)
	if aws.ToString(input.FunctionName) != functionARN {
		t.Fatalf("permission function ARN = %q, want %q", aws.ToString(input.FunctionName), functionARN)
	}
	if aws.ToString(input.SourceAccount) != "123456789012" {
		t.Fatalf("permission source account = %q", aws.ToString(input.SourceAccount))
	}
}

func TestLambdaS3SourceARNPreservesPartition(t *testing.T) {
	got, err := lambdaS3SourceARN(
		"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function",
		"bucket",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "arn:aws-us-gov:s3:::bucket" {
		t.Fatalf("S3 source ARN = %q", got)
	}
}

func TestLambdaEventRuleARNPreservesPartition(t *testing.T) {
	got, err := lambdaEventRuleARN(
		"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function",
		"rule",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "arn:aws-us-gov:events:us-gov-west-1:123456789012:rule/rule"
	if got != want {
		t.Fatalf("EventBridge rule ARN = %q, want %q", got, want)
	}
}

func TestLambdaEventRuleNamesAreValidAndBounded(t *testing.T) {
	if got, want := lambdaScheduleName("function", "rate(1 minute)"), "function___cmF0ZSgxIG1pbnV0ZSk"; got != want {
		t.Fatalf("existing valid schedule name = %q, want %q", got, want)
	}
	cronName := lambdaScheduleName("function", "cron(0 12 * * ? *)")
	if strings.ContainsAny(cronName, "+/") || len(cronName) > 64 {
		t.Fatalf("cron rule name is invalid: %q", cronName)
	}
	longName := lambdaEventRuleName(strings.Repeat("a", 64), "trigger_ecr")
	if len(longName) != 64 || strings.ContainsAny(longName, "+/") {
		t.Fatalf("long EventBridge rule name is invalid: %q", longName)
	}
	if longName != lambdaEventRuleName(strings.Repeat("a", 64), "trigger_ecr") {
		t.Fatal("long EventBridge rule name is not stable")
	}
}

func TestLambdaResolveIdentitySynthesizesOnlyForTypedNotFound(t *testing.T) {
	callerCalls := 0
	identity, err := lambdaResolveIdentityWith(
		context.Background(),
		"function",
		"set",
		"us-gov-west-1",
		func(context.Context, string) (string, error) {
			return "", &lambdatypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
		func(context.Context) (string, error) {
			callerCalls++
			return "arn:aws-us-gov:sts::123456789012:assumed-role/test/session", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if identity.exists || identity.name != "function" || identity.infraSetName != "set" ||
		identity.arn != "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function" || callerCalls != 1 {
		t.Fatalf("synthesized identity = %#v, caller calls = %d", identity, callerCalls)
	}

	providerErr := errors.New("access denied")
	callerCalls = 0
	_, err = lambdaResolveIdentityWith(
		context.Background(), "function", "set", "us-gov-west-1",
		func(context.Context, string) (string, error) { return "", providerErr },
		func(context.Context) (string, error) {
			callerCalls++
			return "", nil
		},
	)
	if !errors.Is(err, providerErr) || callerCalls != 0 {
		t.Fatalf("provider error = %v, caller calls = %d; want access denied and 0", err, callerCalls)
	}
}

func TestLambdaAPIPermissionIdentityPreservesPartition(t *testing.T) {
	name, sourceARN, err := lambdaAPIPermissionIdentity(
		"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function",
		"api-id",
	)
	if err != nil {
		t.Fatal(err)
	}
	if name != "function" || sourceARN != "arn:aws-us-gov:execute-api:us-gov-west-1:123456789012:api-id/*/*" {
		t.Fatalf("API permission identity = %q %q", name, sourceARN)
	}
}

func TestLambdaAPIIntegrationRequiresExactTarget(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	integration := &apitypes.Integration{IntegrationUri: aws.String(functionARN)}
	if !lambdaAPIIntegrationMatches(integration, functionARN) {
		t.Fatal("exact API integration target did not match")
	}
	integration.IntegrationUri = aws.String(functionARN + ":alias")
	if lambdaAPIIntegrationMatches(integration, functionARN) {
		t.Fatal("qualified API integration target matched")
	}
	if lambdaAPIIntegrationMatches(nil, functionARN) {
		t.Fatal("nil API integration matched")
	}
}

func TestLambdaApiOwnershipRequiresExactProtocolTargetAndTag(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	api := &apitypes.Api{
		ApiId:        aws.String("api"),
		ProtocolType: apitypes.ProtocolTypeHttp,
		Tags:         map[string]string{infraSetTagName: "set"},
	}
	integrations := []apitypes.Integration{{IntegrationUri: aws.String(functionARN)}}
	if !lambdaApiOwned(api, integrations, apitypes.ProtocolTypeHttp, functionARN, "set") {
		t.Fatal("exact API integration was not owned")
	}
	untagged := *api
	untagged.Tags = nil
	if lambdaApiOwned(&untagged, integrations, apitypes.ProtocolTypeHttp, functionARN, "set") {
		t.Fatal("untagged API was owned")
	}
	if lambdaApiOwned(api, integrations, apitypes.ProtocolTypeWebsocket, functionARN, "set") {
		t.Fatal("wrong API protocol was owned")
	}
	integrations[0].IntegrationUri = aws.String(functionARN + ":alias")
	if lambdaApiOwned(api, integrations, apitypes.ProtocolTypeHttp, functionARN, "set") {
		t.Fatal("API integration with qualified target was owned")
	}
	integrations = append(integrations, apitypes.Integration{IntegrationUri: aws.String(functionARN)})
	if lambdaApiOwned(api, integrations, apitypes.ProtocolTypeHttp, functionARN, "set") {
		t.Fatal("API with multiple integrations was owned")
	}
}

func TestLambdaSesRuleOwnershipRequiresExactManagedShape(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	rule := &sestypes.ReceiptRule{
		Name:        aws.String("example.com"),
		Enabled:     true,
		TlsPolicy:   sestypes.TlsPolicyRequire,
		ScanEnabled: false,
		Recipients:  []string{"example.com"},
		Actions: []sestypes.ReceiptAction{
			{S3Action: &sestypes.S3Action{BucketName: aws.String("mail")}},
			{LambdaAction: &sestypes.LambdaAction{
				FunctionArn:    aws.String(functionARN),
				InvocationType: sestypes.InvocationTypeEvent,
			}},
		},
	}
	if !lambdaSesRuleOwned("example.com", rule, functionARN) {
		t.Fatal("exact managed SES rule was not owned")
	}
	wrongTarget := *rule
	wrongTarget.Actions = append([]sestypes.ReceiptAction(nil), rule.Actions...)
	wrongTarget.Actions[1].LambdaAction = &sestypes.LambdaAction{
		FunctionArn:    aws.String(functionARN + ":alias"),
		InvocationType: sestypes.InvocationTypeEvent,
	}
	if lambdaSesRuleOwned("example.com", &wrongTarget, functionARN) {
		t.Fatal("SES rule with a qualified target was owned")
	}
	extraAction := *rule
	extraAction.Actions = append(append([]sestypes.ReceiptAction(nil), rule.Actions...), sestypes.ReceiptAction{})
	if lambdaSesRuleOwned("example.com", &extraAction, functionARN) {
		t.Fatal("SES rule with an unmodeled action was owned")
	}
	lambdaTopic := *rule
	lambdaTopic.Actions = append([]sestypes.ReceiptAction(nil), rule.Actions...)
	lambdaTopic.Actions[1].LambdaAction = &sestypes.LambdaAction{
		FunctionArn:    aws.String(functionARN),
		InvocationType: sestypes.InvocationTypeEvent,
		TopicArn:       aws.String("arn:aws:sns:us-west-2:123456789012:topic"),
	}
	if lambdaSesRuleOwned("example.com", &lambdaTopic, functionARN) {
		t.Fatal("SES rule with an unmodeled Lambda topic was owned")
	}
}

func TestLambdaScheduleOwnershipRequiresDeterministicRule(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	infraLambda := &InfraLambda{Name: "function", Arn: functionARN}
	rule := eventbridgetypes.Rule{
		Name:               aws.String(lambdaScheduleName("function", "rate(1 minute)")),
		ScheduleExpression: aws.String("rate(1 minute)"),
	}
	target := eventbridgetypes.Target{Arn: aws.String(functionARN), Id: aws.String("1")}
	if !lambdaScheduleTargetMatches(infraLambda, rule, target) {
		t.Fatal("deterministic schedule rule and exact target did not match")
	}
	rule.Name = aws.String("unrelated-rule")
	if lambdaScheduleTargetMatches(infraLambda, rule, target) {
		t.Fatal("unrelated schedule rule with the same target matched")
	}
}

type fakeLambdaAPICleanupClient struct {
	apis             []apitypes.Api
	integrations     map[string][]apitypes.Integration
	apiPages         map[string]*apigatewayv2.GetApisOutput
	integrationPages map[string]map[string]*apigatewayv2.GetIntegrationsOutput
	deleted          []string
	tagged           []*apigatewayv2.TagResourceInput
}

func (client *fakeLambdaAPICleanupClient) GetApis(
	_ context.Context,
	input *apigatewayv2.GetApisInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.GetApisOutput, error) {
	if client.apiPages != nil {
		return client.apiPages[aws.ToString(input.NextToken)], nil
	}
	return &apigatewayv2.GetApisOutput{Items: client.apis}, nil
}

func (client *fakeLambdaAPICleanupClient) GetIntegrations(
	_ context.Context,
	input *apigatewayv2.GetIntegrationsInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.GetIntegrationsOutput, error) {
	if client.integrationPages != nil {
		pages := client.integrationPages[aws.ToString(input.ApiId)]
		return pages[aws.ToString(input.NextToken)], nil
	}
	return &apigatewayv2.GetIntegrationsOutput{Items: client.integrations[aws.ToString(input.ApiId)]}, nil
}

func (client *fakeLambdaAPICleanupClient) DeleteApi(
	_ context.Context,
	input *apigatewayv2.DeleteApiInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.DeleteApiOutput, error) {
	client.deleted = append(client.deleted, aws.ToString(input.ApiId))
	return &apigatewayv2.DeleteApiOutput{}, nil
}

func (client *fakeLambdaAPICleanupClient) TagResource(
	_ context.Context,
	input *apigatewayv2.TagResourceInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.TagResourceOutput, error) {
	client.tagged = append(client.tagged, input)
	return &apigatewayv2.TagResourceOutput{}, nil
}

func TestLambdaAPIDiscoveryPaginatesAPIsAndIntegrations(t *testing.T) {
	client := &fakeLambdaAPICleanupClient{
		apiPages: map[string]*apigatewayv2.GetApisOutput{
			"": {
				Items:     []apitypes.Api{{ApiId: aws.String("first")}},
				NextToken: aws.String("next"),
			},
			"next": {Items: []apitypes.Api{{ApiId: aws.String("second")}}},
		},
		integrationPages: map[string]map[string]*apigatewayv2.GetIntegrationsOutput{
			"first": {
				"": {
					Items:     []apitypes.Integration{{IntegrationId: aws.String("one")}},
					NextToken: aws.String("next"),
				},
				"next": {Items: []apitypes.Integration{{IntegrationId: aws.String("two")}}},
			},
		},
	}
	apis, err := lambdaAPIList(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(apis) != 2 || aws.ToString(apis[1].ApiId) != "second" {
		t.Fatalf("paginated APIs = %#v", apis)
	}
	integrations, err := lambdaAPIIntegrations(context.Background(), client, aws.String("first"))
	if err != nil {
		t.Fatal(err)
	}
	if len(integrations) != 2 || aws.ToString(integrations[1].IntegrationId) != "two" {
		t.Fatalf("paginated integrations = %#v", integrations)
	}
}

func TestLambdaCleanupAPITriggersAfterFunctionDeletionPreservesUnownedAPIs(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	api := func(id, name string, protocol apitypes.ProtocolType, infraSet string) apitypes.Api {
		tags := map[string]string{}
		if infraSet != "" {
			tags[infraSetTagName] = infraSet
		}
		return apitypes.Api{ApiId: aws.String(id), Name: aws.String(name), ProtocolType: protocol, Tags: tags}
	}
	client := &fakeLambdaAPICleanupClient{
		apis: []apitypes.Api{
			api("owned-http", "function", apitypes.ProtocolTypeHttp, "set"),
			api("owned-websocket", "function"+LambdaWebsocketSuffix, apitypes.ProtocolTypeWebsocket, "set"),
			api("untagged", "function", apitypes.ProtocolTypeHttp, ""),
			api("wrong-set", "function", apitypes.ProtocolTypeHttp, "other"),
			api("wrong-target", "function", apitypes.ProtocolTypeHttp, "set"),
			api("unrelated-name", "other", apitypes.ProtocolTypeHttp, "set"),
		},
		integrations: map[string][]apitypes.Integration{},
	}
	for _, id := range []string{"owned-http", "owned-websocket", "untagged", "wrong-set", "unrelated-name"} {
		client.integrations[id] = []apitypes.Integration{{IntegrationUri: aws.String(functionARN)}}
	}
	client.integrations["wrong-target"] = []apitypes.Integration{{IntegrationUri: aws.String(functionARN + ":alias")}}
	var cleanedDomains []string
	cleanDomains := func(_ context.Context, name string, _ *apitypes.Api, infraSetName string, _ bool) error {
		if infraSetName != "set" {
			t.Fatalf("domain cleanup infrastructure set = %q, want set", infraSetName)
		}
		cleanedDomains = append(cleanedDomains, name)
		return nil
	}
	identity := lambdaIdentity{name: "function", arn: functionARN, infraSetName: "set", exists: false}
	if err := lambdaCleanupAPITriggers(context.Background(), client, identity, false, cleanDomains); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.deleted, []string{"owned-http", "owned-websocket"}) {
		t.Fatalf("deleted APIs = %v, want owned HTTP and WebSocket APIs", client.deleted)
	}
	if !slices.Equal(cleanedDomains, []string{"function", "function" + LambdaWebsocketSuffix}) {
		t.Fatalf("domain cleanup = %v, want owned HTTP and WebSocket APIs", cleanedDomains)
	}
}

func TestLambdaCleanupAPITriggersWithoutKnownInfraSetAcceptsAnyLibawsTag(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	client := &fakeLambdaAPICleanupClient{
		apis: []apitypes.Api{{
			ApiId: aws.String("owned"), Name: aws.String("function"),
			ProtocolType: apitypes.ProtocolTypeHttp,
			Tags:         map[string]string{infraSetTagName: "discovered-set"},
		}},
		integrations: map[string][]apitypes.Integration{
			"owned": {{IntegrationUri: aws.String(functionARN)}},
		},
	}
	identity := lambdaIdentity{name: "function", arn: functionARN, exists: false}
	if err := lambdaCleanupAPITriggers(context.Background(), client, identity, false, func(context.Context, string, *apitypes.Api, string, bool) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.deleted, []string{"owned"}) {
		t.Fatalf("deleted APIs = %v, want [owned]", client.deleted)
	}
}

func TestLambdaEnsureAPITagUsesPartitionCorrectResourceARN(t *testing.T) {
	client := &fakeLambdaAPICleanupClient{}
	api := &apitypes.Api{ApiId: aws.String("abc123"), Name: aws.String("function")}
	functionARN := "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:function"
	if err := lambdaEnsureAPITag(context.Background(), client, api, functionARN, "set", false); err != nil {
		t.Fatal(err)
	}
	if len(client.tagged) != 1 || aws.ToString(client.tagged[0].ResourceArn) != "arn:aws-us-gov:apigateway:us-gov-west-1::/apis/abc123" {
		t.Fatalf("API tag requests = %#v", client.tagged)
	}
}

type fakeLambdaEventCleanupClient struct {
	rules         []eventbridgetypes.Rule
	targets       map[string][]eventbridgetypes.Target
	tags          map[string][]eventbridgetypes.Tag
	removed       []string
	deleted       []string
	tagged        []*eventbridge.TagResourceInput
	putInputs     []*eventbridge.PutTargetsInput
	removeFailure bool
	putFailure    bool
	deleteErr     error
}

func (client *fakeLambdaEventCleanupClient) PutTargets(
	_ context.Context,
	input *eventbridge.PutTargetsInput,
	_ ...func(*eventbridge.Options),
) (*eventbridge.PutTargetsOutput, error) {
	client.putInputs = append(client.putInputs, input)
	if client.putFailure {
		return &eventbridge.PutTargetsOutput{
			FailedEntryCount: 1,
			FailedEntries: []eventbridgetypes.PutTargetsResultEntry{{
				ErrorCode: aws.String("ConcurrentModificationException"),
			}},
		}, nil
	}
	return &eventbridge.PutTargetsOutput{}, nil
}

func (client *fakeLambdaEventCleanupClient) ListRules(
	context.Context,
	*eventbridge.ListRulesInput,
	...func(*eventbridge.Options),
) (*eventbridge.ListRulesOutput, error) {
	return &eventbridge.ListRulesOutput{Rules: client.rules}, nil
}

func (client *fakeLambdaEventCleanupClient) ListTargetsByRule(
	_ context.Context,
	input *eventbridge.ListTargetsByRuleInput,
	_ ...func(*eventbridge.Options),
) (*eventbridge.ListTargetsByRuleOutput, error) {
	return &eventbridge.ListTargetsByRuleOutput{Targets: client.targets[aws.ToString(input.Rule)]}, nil
}

func (client *fakeLambdaEventCleanupClient) ListTagsForResource(
	_ context.Context,
	input *eventbridge.ListTagsForResourceInput,
	_ ...func(*eventbridge.Options),
) (*eventbridge.ListTagsForResourceOutput, error) {
	return &eventbridge.ListTagsForResourceOutput{Tags: client.tags[aws.ToString(input.ResourceARN)]}, nil
}

func (client *fakeLambdaEventCleanupClient) TagResource(
	_ context.Context,
	input *eventbridge.TagResourceInput,
	_ ...func(*eventbridge.Options),
) (*eventbridge.TagResourceOutput, error) {
	client.tagged = append(client.tagged, input)
	return &eventbridge.TagResourceOutput{}, nil
}

func (client *fakeLambdaEventCleanupClient) RemoveTargets(
	_ context.Context,
	input *eventbridge.RemoveTargetsInput,
	_ ...func(*eventbridge.Options),
) (*eventbridge.RemoveTargetsOutput, error) {
	client.removed = append(client.removed, aws.ToString(input.Rule))
	if client.removeFailure {
		return &eventbridge.RemoveTargetsOutput{
			FailedEntryCount: 1,
			FailedEntries: []eventbridgetypes.RemoveTargetsResultEntry{{
				ErrorCode: aws.String("ConcurrentModificationException"),
			}},
		}, nil
	}
	return &eventbridge.RemoveTargetsOutput{}, nil
}

func (client *fakeLambdaEventCleanupClient) DeleteRule(
	_ context.Context,
	input *eventbridge.DeleteRuleInput,
	_ ...func(*eventbridge.Options),
) (*eventbridge.DeleteRuleOutput, error) {
	client.deleted = append(client.deleted, aws.ToString(input.Name))
	if client.deleteErr != nil {
		return nil, client.deleteErr
	}
	return &eventbridge.DeleteRuleOutput{}, nil
}

func eventRuleForTest(name, arnValue, schedule, pattern string) eventbridgetypes.Rule {
	return eventbridgetypes.Rule{
		Name:               aws.String(name),
		Arn:                aws.String(arnValue),
		ScheduleExpression: optionalString(schedule),
		EventPattern:       optionalString(pattern),
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}

func TestLambdaCleanupScheduleTriggersAfterFunctionDeletionPreservesUnownedRules(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	schedule := "rate(1 minute)"
	ownedName := lambdaScheduleName("function", schedule)
	wrongName := "unrelated"
	ownedARN := "arn:aws:events:us-west-2:123456789012:rule/" + ownedName
	wrongTagARN := ownedARN + "-wrong-tag"
	wrongNameARN := "arn:aws:events:us-west-2:123456789012:rule/" + wrongName
	extraTargetARN := ownedARN + "-extra-target"
	client := &fakeLambdaEventCleanupClient{
		rules: []eventbridgetypes.Rule{
			eventRuleForTest(ownedName, ownedARN, schedule, ""),
			eventRuleForTest(ownedName, wrongTagARN, schedule, ""),
			eventRuleForTest(wrongName, wrongNameARN, schedule, ""),
			eventRuleForTest(ownedName, extraTargetARN, schedule, ""),
		},
		targets: map[string][]eventbridgetypes.Target{
			ownedName: {{Id: aws.String("1"), Arn: aws.String(functionARN)}},
			wrongName: {{Id: aws.String("1"), Arn: aws.String(functionARN)}},
		},
		tags: map[string][]eventbridgetypes.Tag{
			ownedARN:       {{Key: aws.String(infraSetTagName), Value: aws.String("set")}},
			wrongTagARN:    {{Key: aws.String(infraSetTagName), Value: aws.String("other")}},
			wrongNameARN:   {{Key: aws.String(infraSetTagName), Value: aws.String("set")}},
			extraTargetARN: {{Key: aws.String(infraSetTagName), Value: aws.String("set")}},
		},
	}
	// Duplicate EventBridge names cannot exist in AWS. Give the extra-target rule a
	// distinct deterministic schedule/name while preserving the same Lambda target.
	extraSchedule := "rate(2 minutes)"
	client.rules[3].Name = aws.String(lambdaScheduleName("function", extraSchedule))
	client.rules[3].ScheduleExpression = aws.String(extraSchedule)
	client.targets[aws.ToString(client.rules[3].Name)] = []eventbridgetypes.Target{
		{Id: aws.String("1"), Arn: aws.String(functionARN)},
		{Id: aws.String("2"), Arn: aws.String(functionARN)},
	}
	identity := lambdaIdentity{name: "function", arn: functionARN, infraSetName: "set", exists: false}
	if err := lambdaCleanupScheduleTriggers(context.Background(), client, identity, nil, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.deleted, []string{ownedName}) {
		t.Fatalf("deleted schedule rules = %v, want [%s]", client.deleted, ownedName)
	}
}

func TestLambdaCleanupECRTriggerAfterFunctionDeletionPreservesUnownedRules(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	ruleName := "function" + lambdaEventRuleNameSeparator + "trigger_ecr"
	ownedARN := "arn:aws:events:us-west-2:123456789012:rule/" + ruleName
	client := &fakeLambdaEventCleanupClient{
		rules: []eventbridgetypes.Rule{
			eventRuleForTest(ruleName, ownedARN, "", lambdaEcrEventPattern),
			eventRuleForTest(ruleName, ownedARN+"-wrong-tag", "", lambdaEcrEventPattern),
		},
		targets: map[string][]eventbridgetypes.Target{
			ruleName: {{Id: aws.String("1"), Arn: aws.String(functionARN)}},
		},
		tags: map[string][]eventbridgetypes.Tag{
			ownedARN:                {{Key: aws.String(infraSetTagName), Value: aws.String("set")}},
			ownedARN + "-wrong-tag": {{Key: aws.String(infraSetTagName), Value: aws.String("other")}},
		},
	}
	identity := lambdaIdentity{name: "function", arn: functionARN, infraSetName: "set", exists: false}
	if err := lambdaCleanupECRTrigger(context.Background(), client, identity, false, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.deleted, []string{ruleName}) {
		t.Fatalf("deleted ECR rules = %v, want [%s]", client.deleted, ruleName)
	}
}

func TestLambdaEventTargetCreationFailsOnEntryFailure(t *testing.T) {
	client := &fakeLambdaEventCleanupClient{putFailure: true}
	if err := lambdaPutEventTarget(context.Background(), client, "rule", "arn:aws:lambda:region:account:function:function"); err == nil {
		t.Fatal("PutTargets entry failure was ignored")
	}
}

func TestLambdaEventCleanupFailsOnRemoveTargetEntryFailure(t *testing.T) {
	client := &fakeLambdaEventCleanupClient{removeFailure: true}
	rule := &eventbridgetypes.Rule{Name: aws.String("rule")}
	target := eventbridgetypes.Target{Id: aws.String("1")}
	if err := lambdaDeleteEventRule(context.Background(), client, rule, target, false); err == nil {
		t.Fatal("RemoveTargets entry failure was ignored")
	}
	if len(client.deleted) != 0 {
		t.Fatal("rule was deleted after target removal failed")
	}
}

func TestLambdaEventCleanupRestoresTargetWhenRuleDeletionFails(t *testing.T) {
	failure := errors.New("delete failed")
	client := &fakeLambdaEventCleanupClient{deleteErr: failure}
	rule := &eventbridgetypes.Rule{Name: aws.String("rule")}
	target := eventbridgetypes.Target{
		Id:  aws.String("1"),
		Arn: aws.String("arn:aws:lambda:region:account:function:function"),
	}
	if err := lambdaDeleteEventRule(context.Background(), client, rule, target, false); !errors.Is(err, failure) {
		t.Fatalf("delete error = %v, want %v", err, failure)
	}
	if len(client.putInputs) != 1 || aws.ToString(client.putInputs[0].Rule) != "rule" ||
		!reflect.DeepEqual(client.putInputs[0].Targets, []eventbridgetypes.Target{target}) {
		t.Fatalf("restored targets = %#v, want exact original target", client.putInputs)
	}
}

func TestLambdaManualDeleteIntegration(t *testing.T) {
	if os.Getenv("LIBAWS_INTEGRATION") != "1" {
		t.Skip("set LIBAWS_INTEGRATION=1 to run AWS integration tests")
	}
	name := os.Getenv("LIBAWS_LAMBDA_DELETE_TEST_FUNCTION")
	if !strings.HasPrefix(name, "test-lambda-") || len(name) == len("test-lambda-") {
		t.Fatalf("LIBAWS_LAMBDA_DELETE_TEST_FUNCTION must name a unique test-lambda-* function, got %q", name)
	}
	expectedAccount := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expectedAccount == "" {
		t.Fatal("LIBAWS_TEST_ACCOUNT must identify the authorized scratch account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	actualAccount, err := StsAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if actualAccount != expectedAccount {
		t.Fatalf("refusing manual Lambda deletion in account %q; expected %q", actualAccount, expectedAccount)
	}
	if err := LambdaDeleteFunction(ctx, name, false); err != nil {
		t.Fatal(err)
	}
	if functionARN, err := LambdaArn(ctx, name); err == nil || functionARN != "" {
		t.Fatalf("manually deleted Lambda still resolves as %q with error %v", functionARN, err)
	}
}
