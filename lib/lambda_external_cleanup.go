package lib

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	sesv2 "github.com/aws/aws-sdk-go-v2/service/ses"
)

func lambdaInfraSetTagOwned(tags map[string]string, expected string) bool {
	actual := tags[infraSetTagName]
	return actual != "" && (expected == "" || actual == expected)
}

func lambdaSDKTagsOwned(tags []eventbridgetypes.Tag, expected string) bool {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == infraSetTagName {
			actual := aws.ToString(tag.Value)
			return actual != "" && (expected == "" || actual == expected)
		}
	}
	return false
}

type lambdaAPITagClient interface {
	TagResource(context.Context, *apigatewayv2.TagResourceInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.TagResourceOutput, error)
}

func lambdaAPIPermissionIdentity(functionARN, apiID string) (string, string, error) {
	parsed, err := awsarn.Parse(functionARN)
	if err != nil || parsed.Partition == "" || parsed.Service != "lambda" || parsed.Region == "" ||
		parsed.AccountID == "" || apiID == "" || !strings.HasPrefix(parsed.Resource, "function:") {
		return "", "", fmt.Errorf("invalid Lambda ARN %q or API ID %q", functionARN, apiID)
	}
	functionName := strings.TrimPrefix(parsed.Resource, "function:")
	if functionName == "" || strings.Contains(functionName, ":") {
		return "", "", fmt.Errorf("API trigger requires an unqualified Lambda ARN, got %q", functionARN)
	}
	sourceARN := fmt.Sprintf(
		"arn:%s:execute-api:%s:%s:%s/*/*",
		parsed.Partition,
		parsed.Region,
		parsed.AccountID,
		apiID,
	)
	return functionName, sourceARN, nil
}

func lambdaAPIARN(functionARN, apiID string) (string, error) {
	parsed, err := awsarn.Parse(functionARN)
	if err != nil || parsed.Partition == "" || parsed.Service != "lambda" || parsed.Region == "" || apiID == "" {
		return "", fmt.Errorf("invalid Lambda identity ARN %q or API ID %q", functionARN, apiID)
	}
	return fmt.Sprintf("arn:%s:apigateway:%s::/apis/%s", parsed.Partition, parsed.Region, apiID), nil
}

func lambdaEnsureAPITag(
	ctx context.Context,
	client lambdaAPITagClient,
	api *apitypes.Api,
	functionARN string,
	infraSetName string,
	preview bool,
) error {
	if api == nil || api.ApiId == nil {
		return errors.New("cannot tag API without an API ID")
	}
	if api.Tags[infraSetTagName] == infraSetName && infraSetName != "" {
		return nil
	}
	resourceARN, err := lambdaAPIARN(functionARN, aws.ToString(api.ApiId))
	if err != nil {
		return err
	}
	if !preview {
		if _, err := client.TagResource(ctx, &apigatewayv2.TagResourceInput{
			ResourceArn: aws.String(resourceARN),
			Tags:        map[string]string{infraSetTagName: infraSetName},
		}); err != nil {
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated API infrastructure tag:", aws.ToString(api.Name))
	return nil
}

type lambdaAPICleanupClient interface {
	GetApis(context.Context, *apigatewayv2.GetApisInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApisOutput, error)
	GetIntegrations(context.Context, *apigatewayv2.GetIntegrationsInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.GetIntegrationsOutput, error)
	DeleteApi(context.Context, *apigatewayv2.DeleteApiInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteApiOutput, error)
}

type lambdaAPIDomainCleaner func(context.Context, string, *apitypes.Api, string, bool) error

func lambdaAPIList(ctx context.Context, client lambdaAPICleanupClient) ([]apitypes.Api, error) {
	var result []apitypes.Api
	var token *string
	for {
		out, err := client.GetApis(ctx, &apigatewayv2.GetApisInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("API Gateway GetApis returned nil output")
		}
		result = append(result, out.Items...)
		if out.NextToken == nil {
			return result, nil
		}
		token = out.NextToken
	}
}

func lambdaAPIIntegrations(ctx context.Context, client lambdaAPICleanupClient, apiID *string) ([]apitypes.Integration, error) {
	var result []apitypes.Integration
	var token *string
	for {
		out, err := client.GetIntegrations(ctx, &apigatewayv2.GetIntegrationsInput{
			ApiId:      apiID,
			NextToken:  token,
			MaxResults: aws.String("500"),
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("API Gateway GetIntegrations returned nil output")
		}
		result = append(result, out.Items...)
		if out.NextToken == nil {
			return result, nil
		}
		token = out.NextToken
	}
}

func lambdaAPIIntegrationMatches(integration *apitypes.Integration, functionARN string) bool {
	return integration != nil && functionARN != "" && aws.ToString(integration.IntegrationUri) == functionARN
}

func lambdaApiOwned(
	api *apitypes.Api,
	integrations []apitypes.Integration,
	protocolType apitypes.ProtocolType,
	functionARN string,
	infraSetName string,
) bool {
	return api != nil && api.ApiId != nil && lambdaInfraSetTagOwned(api.Tags, infraSetName) &&
		api.ProtocolType == protocolType && len(integrations) == 1 &&
		lambdaAPIIntegrationMatches(&integrations[0], functionARN)
}

func lambdaCleanupAPIDomains(ctx context.Context, name string, api *apitypes.Api, infraSetName string, preview bool) error {
	domains, err := ApiListDomains(ctx)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		if err := lambdaTriggerApiDeleteDns(ctx, name, api, domain, infraSetName, preview); err != nil {
			return err
		}
	}
	return nil
}

func lambdaCleanupAPITriggers(
	ctx context.Context,
	client lambdaAPICleanupClient,
	identity lambdaIdentity,
	preview bool,
	cleanupDomains lambdaAPIDomainCleaner,
) error {
	apis, err := lambdaAPIList(ctx, client)
	if err != nil {
		return err
	}
	for _, expected := range []struct {
		name     string
		protocol apitypes.ProtocolType
	}{
		{identity.name, apitypes.ProtocolTypeHttp},
		{identity.name + LambdaWebsocketSuffix, apitypes.ProtocolTypeWebsocket},
	} {
		for index := range apis {
			api := &apis[index]
			if aws.ToString(api.Name) != expected.name {
				continue
			}
			integrations, err := lambdaAPIIntegrations(ctx, client, api.ApiId)
			if err != nil {
				var notFound *apitypes.NotFoundException
				if errors.As(err, &notFound) {
					continue
				}
				return err
			}
			if !lambdaApiOwned(api, integrations, expected.protocol, identity.arn, identity.infraSetName) {
				continue
			}
			if err := cleanupDomains(ctx, expected.name, api, identity.infraSetName, preview); err != nil {
				return err
			}
			if !preview {
				if _, err := client.DeleteApi(ctx, &apigatewayv2.DeleteApiInput{ApiId: api.ApiId}); err != nil {
					var notFound *apitypes.NotFoundException
					if !errors.As(err, &notFound) {
						return err
					}
				}
			}
			Logger.Println(PreviewString(preview)+"deleted api trigger for:", expected.name)
		}
	}
	return nil
}

func lambdaEventRuleARN(functionARN, ruleName string) (string, error) {
	parsed, err := awsarn.Parse(functionARN)
	if err != nil || parsed.Partition == "" || parsed.Service != "lambda" || parsed.Region == "" ||
		parsed.AccountID == "" || ruleName == "" || !strings.HasPrefix(parsed.Resource, "function:") ||
		strings.Contains(strings.TrimPrefix(parsed.Resource, "function:"), ":") {
		return "", fmt.Errorf("invalid Lambda ARN %q or EventBridge rule name %q", functionARN, ruleName)
	}
	return fmt.Sprintf("arn:%s:events:%s:%s:rule/%s", parsed.Partition, parsed.Region, parsed.AccountID, ruleName), nil
}

type lambdaEventTargetWriter interface {
	PutTargets(context.Context, *eventbridge.PutTargetsInput, ...func(*eventbridge.Options)) (*eventbridge.PutTargetsOutput, error)
}

func lambdaPutEventTargets(ctx context.Context, client lambdaEventTargetWriter, ruleName string, targets []eventbridgetypes.Target) error {
	out, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule:    aws.String(ruleName),
		Targets: targets,
	})
	if err != nil {
		return err
	}
	if out == nil {
		return errors.New("EventBridge PutTargets returned nil output")
	}
	if out.FailedEntryCount != 0 || len(out.FailedEntries) != 0 {
		return fmt.Errorf("EventBridge failed to put target for rule %q: %v", ruleName, out.FailedEntries)
	}
	return nil
}

func lambdaPutEventTarget(ctx context.Context, client lambdaEventTargetWriter, ruleName, functionARN string) error {
	return lambdaPutEventTargets(ctx, client, ruleName, []eventbridgetypes.Target{{
		Id:  aws.String("1"),
		Arn: aws.String(functionARN),
	}})
}

type lambdaEventCleanupClient interface {
	lambdaEventTargetWriter
	ListRules(context.Context, *eventbridge.ListRulesInput, ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error)
	ListTargetsByRule(context.Context, *eventbridge.ListTargetsByRuleInput, ...func(*eventbridge.Options)) (*eventbridge.ListTargetsByRuleOutput, error)
	ListTagsForResource(context.Context, *eventbridge.ListTagsForResourceInput, ...func(*eventbridge.Options)) (*eventbridge.ListTagsForResourceOutput, error)
	TagResource(context.Context, *eventbridge.TagResourceInput, ...func(*eventbridge.Options)) (*eventbridge.TagResourceOutput, error)
	RemoveTargets(context.Context, *eventbridge.RemoveTargetsInput, ...func(*eventbridge.Options)) (*eventbridge.RemoveTargetsOutput, error)
	DeleteRule(context.Context, *eventbridge.DeleteRuleInput, ...func(*eventbridge.Options)) (*eventbridge.DeleteRuleOutput, error)
}

func lambdaEventRules(ctx context.Context, client lambdaEventCleanupClient) ([]eventbridgetypes.Rule, error) {
	var result []eventbridgetypes.Rule
	var token *string
	for {
		out, err := client.ListRules(ctx, &eventbridge.ListRulesInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("EventBridge ListRules returned nil output")
		}
		result = append(result, out.Rules...)
		if out.NextToken == nil {
			return result, nil
		}
		token = out.NextToken
	}
}

func lambdaEventTargets(ctx context.Context, client lambdaEventCleanupClient, ruleName *string) ([]eventbridgetypes.Target, error) {
	var result []eventbridgetypes.Target
	var token *string
	for {
		out, err := client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{
			Rule:      ruleName,
			NextToken: token,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("EventBridge ListTargetsByRule returned nil output")
		}
		result = append(result, out.Targets...)
		if out.NextToken == nil {
			return result, nil
		}
		token = out.NextToken
	}
}

func lambdaEventRuleTagOwned(ctx context.Context, client lambdaEventCleanupClient, rule *eventbridgetypes.Rule, infraSetName string) (bool, error) {
	if rule == nil || rule.Arn == nil {
		return false, nil
	}
	out, err := client.ListTagsForResource(ctx, &eventbridge.ListTagsForResourceInput{ResourceARN: rule.Arn})
	if err != nil {
		var notFound *eventbridgetypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, err
	}
	if out == nil {
		return false, errors.New("EventBridge ListTagsForResource returned nil output")
	}
	return lambdaSDKTagsOwned(out.Tags, infraSetName), nil
}

func lambdaEnsureEventRuleTag(
	ctx context.Context,
	client lambdaEventCleanupClient,
	ruleARN string,
	infraSetName string,
	preview bool,
) error {
	out, err := client.ListTagsForResource(ctx, &eventbridge.ListTagsForResourceInput{ResourceARN: aws.String(ruleARN)})
	if err != nil {
		return err
	}
	if out == nil {
		return errors.New("EventBridge ListTagsForResource returned nil output")
	}
	if lambdaSDKTagsOwned(out.Tags, infraSetName) {
		return nil
	}
	if !preview {
		if _, err := client.TagResource(ctx, &eventbridge.TagResourceInput{
			ResourceARN: aws.String(ruleARN),
			Tags: []eventbridgetypes.Tag{{
				Key:   aws.String(infraSetTagName),
				Value: aws.String(infraSetName),
			}},
		}); err != nil {
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated EventBridge rule infrastructure tag:", ruleARN)
	return nil
}

func lambdaDeleteEventRule(
	ctx context.Context,
	client lambdaEventCleanupClient,
	rule *eventbridgetypes.Rule,
	target eventbridgetypes.Target,
	preview bool,
) error {
	if preview {
		return nil
	}
	out, err := client.RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{
		Rule: rule.Name,
		Ids:  []string{aws.ToString(target.Id)},
	})
	if err != nil {
		var notFound *eventbridgetypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	if out == nil {
		return errors.New("EventBridge RemoveTargets returned nil output")
	}
	if out.FailedEntryCount != 0 || len(out.FailedEntries) != 0 {
		return fmt.Errorf("EventBridge failed to remove targets from rule %q: %v", aws.ToString(rule.Name), out.FailedEntries)
	}
	if _, err := client.DeleteRule(ctx, &eventbridge.DeleteRuleInput{Name: rule.Name}); err != nil {
		var notFound *eventbridgetypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		restoreErr := lambdaPutEventTargets(ctx, client, aws.ToString(rule.Name), []eventbridgetypes.Target{target})
		if restoreErr != nil {
			return errors.Join(
				fmt.Errorf("delete EventBridge rule %q: %w", aws.ToString(rule.Name), err),
				fmt.Errorf("restore EventBridge target for rule %q: %w", aws.ToString(rule.Name), restoreErr),
			)
		}
		return err
	}
	return nil
}

func lambdaCleanupScheduleTriggers(
	ctx context.Context,
	client lambdaEventCleanupClient,
	identity lambdaIdentity,
	desiredSchedules []string,
	preview bool,
) error {
	rules, err := lambdaEventRules(ctx, client)
	if err != nil {
		return err
	}
	infraLambda := &InfraLambda{Name: identity.name, Arn: identity.arn}
	for index := range rules {
		rule := &rules[index]
		schedule := aws.ToString(rule.ScheduleExpression)
		if schedule == "" || slices.Contains(desiredSchedules, schedule) ||
			aws.ToString(rule.Name) != lambdaScheduleName(identity.name, schedule) {
			continue
		}
		targets, err := lambdaEventTargets(ctx, client, rule.Name)
		if err != nil {
			var notFound *eventbridgetypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				continue
			}
			return err
		}
		if len(targets) != 1 || !lambdaScheduleTargetMatches(infraLambda, *rule, targets[0]) {
			continue
		}
		owned, err := lambdaEventRuleTagOwned(ctx, client, rule, identity.infraSetName)
		if err != nil {
			return err
		}
		if !owned {
			continue
		}
		if err := lambdaDeleteEventRule(ctx, client, rule, targets[0], preview); err != nil {
			return err
		}
		Logger.Println(PreviewString(preview)+"deleted schedule trigger:", identity.name, aws.ToString(rule.ScheduleExpression))
	}
	return nil
}

func lambdaCleanupECRTrigger(
	ctx context.Context,
	client lambdaEventCleanupClient,
	identity lambdaIdentity,
	desired bool,
	preview bool,
) error {
	if desired {
		return nil
	}
	ruleName := lambdaEventRuleName(identity.name, "trigger_ecr")
	rules, err := lambdaEventRules(ctx, client)
	if err != nil {
		return err
	}
	for index := range rules {
		rule := &rules[index]
		if aws.ToString(rule.Name) != ruleName || aws.ToString(rule.EventPattern) != lambdaEcrEventPattern {
			continue
		}
		targets, err := lambdaEventTargets(ctx, client, rule.Name)
		if err != nil {
			var notFound *eventbridgetypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				continue
			}
			return err
		}
		if len(targets) != 1 || aws.ToString(targets[0].Id) != "1" || aws.ToString(targets[0].Arn) != identity.arn {
			continue
		}
		owned, err := lambdaEventRuleTagOwned(ctx, client, rule, identity.infraSetName)
		if err != nil {
			return err
		}
		if !owned {
			continue
		}
		if err := lambdaDeleteEventRule(ctx, client, rule, targets[0], preview); err != nil {
			return err
		}
		Logger.Println(PreviewString(preview)+"deleted ecr trigger:", identity.name)
	}
	return nil
}

func lambdaCleanupSESTriggers(
	ctx context.Context,
	client sesReceiptRuleCleanupClient,
	identity lambdaIdentity,
	desiredDomains []string,
	preview bool,
) error {
	ruleSets, err := sesListReceiptRulesets(ctx, client)
	if err != nil {
		return err
	}
	for _, ruleSet := range ruleSets {
		name := aws.ToString(ruleSet.Name)
		if name == "" || slices.Contains(desiredDomains, name) {
			continue
		}
		out, err := client.DescribeReceiptRuleSet(ctx, &sesv2.DescribeReceiptRuleSetInput{RuleSetName: ruleSet.Name})
		if err != nil {
			if sesRuleSetDoesNotExist(err) {
				continue
			}
			return err
		}
		if out == nil {
			return errors.New("SES DescribeReceiptRuleSet returned nil output")
		}
		if len(out.Rules) != 1 || !lambdaSesRuleOwned(name, &out.Rules[0], identity.arn) {
			continue
		}
		if err := sesRmReceiptRuleset(ctx, client, name, preview); err != nil {
			return err
		}
	}
	return nil
}

func lambdaCleanupExternalTriggers(ctx context.Context, identity lambdaIdentity, preview bool) error {
	if err := lambdaCleanupAPITriggers(ctx, ApiClient(), identity, preview, lambdaCleanupAPIDomains); err != nil {
		return err
	}
	if err := lambdaCleanupSESTriggers(ctx, SesClient(), identity, nil, preview); err != nil {
		return err
	}
	if err := lambdaCleanupScheduleTriggers(ctx, EventsClient(), identity, nil, preview); err != nil {
		return err
	}
	infraLambda := &InfraLambda{Name: identity.name, Arn: identity.arn}
	if err := lambdaRemoveStaleS3Triggers(ctx, S3Client(), lambdaAWSClientForBucket, infraLambda, nil, preview); err != nil {
		return err
	}
	if err := lambdaCleanupECRTrigger(ctx, EventsClient(), identity, false, preview); err != nil {
		return err
	}
	return lambdaCleanupAlarmTriggers(ctx, CloudwatchClient(), identity, nil, preview)
}

var _ lambdaAPICleanupClient = (*apigatewayv2.Client)(nil)
var _ lambdaEventCleanupClient = (*eventbridge.Client)(nil)
var _ lambdaS3AccountClient = (*s3.Client)(nil)
var _ sesReceiptRuleCleanupClient = (*sesv2.Client)(nil)
