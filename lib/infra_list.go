package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/aws/smithy-go"
)

// infraListScope selects membership before describing resource configuration.
// Its zero value preserves the account-wide inventory behaviour.
type infraListScope struct{ setName string }

// Each inventory worker must report completion even if resource decoding panics.
// Do not use logRecover here: it re-panics and terminates the caller's process.
func infraListRecover(errs chan<- error, resource string) {
	if recovered := recover(); recovered != nil {
		if err, ok := recovered.(error); ok {
			errs <- fmt.Errorf("list %s: %w", resource, err)
		} else {
			errs <- fmt.Errorf("list %s: %v", resource, recovered)
		}
	}
}

// InfraListSet lists the resources owned by one exact libaws.infraset tag value.
func InfraListSet(ctx context.Context, name string, showEnvVarValues bool) (*InfraListOutput, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("infrastructure set name is required")
	}
	return (infraListScope{setName: name}).list(ctx, "", showEnvVarValues)
}

func InfraList(ctx context.Context, filter string, showEnvVarValues bool) (*InfraListOutput, error) {
	return (infraListScope{}).list(ctx, filter, showEnvVarValues)
}

func InfraListEvent(ctx context.Context, triggersChan chan<- *InfraTrigger) (map[string]*InfraEvent, error) {
	return (infraListScope{}).listEvent(ctx, triggersChan)
}

func InfraListLambda(ctx context.Context, triggersChan <-chan *InfraTrigger, filter string) (map[string]*InfraLambda, error) {
	return (infraListScope{}).listLambda(ctx, triggersChan, filter)
}

func InfraListKeypair(ctx context.Context) (map[string]*InfraKeypair, error) {
	return (infraListScope{}).listKeypair(ctx)
}

func InfraListApi(ctx context.Context, triggersChan chan<- *InfraTrigger) (map[string]*InfraApi, error) {
	return (infraListScope{}).listApi(ctx, triggersChan)
}

func InfraListDynamoDB(ctx context.Context) (map[string]*InfraDynamoDB, error) {
	return (infraListScope{}).listDynamoDB(ctx)
}

func InfraListVpc(ctx context.Context) (map[string]*InfraVpc, error) {
	return (infraListScope{}).listVpc(ctx)
}

func InfraListEC2(ctx context.Context) (map[string]*InfraEC2, error) {
	return (infraListScope{}).listEC2(ctx, nil)
}

func InfraListUser(ctx context.Context) (map[string]*InfraUser, error) {
	return (infraListScope{}).listUser(ctx)
}

func InfraListRole(ctx context.Context) (map[string]*InfraRole, error) {
	return (infraListScope{}).listRole(ctx)
}

func InfraListInstanceProfile(ctx context.Context) (map[string]*InfraInstanceProfile, error) {
	return (infraListScope{}).listInstanceProfile(ctx)
}

func InfraListS3(ctx context.Context, triggersChan chan<- *InfraTrigger) (map[string]*InfraS3, error) {
	return (infraListScope{}).listS3(ctx, triggersChan)
}

func InfraListSQS(ctx context.Context) (map[string]*InfraSQS, error) {
	return (infraListScope{}).listSQS(ctx)
}

func (scope infraListScope) iamTagsMatch(tags []iamtypes.Tag) bool {
	if scope.setName == "" {
		return true
	}
	for _, tag := range tags {
		if aws.ToString(tag.Key) == infraSetTagName {
			return aws.ToString(tag.Value) == scope.setName
		}
	}
	return false
}

func iamListRoleTags(ctx context.Context, name string) ([]iamtypes.Tag, error) {
	var tags []iamtypes.Tag
	var marker *string
	for {
		out, err := IamClient().ListRoleTags(ctx, &iam.ListRoleTagsInput{RoleName: aws.String(name), Marker: marker})
		if err != nil {
			return nil, err
		}
		tags = append(tags, out.Tags...)
		if !out.IsTruncated {
			return tags, nil
		}
		if aws.ToString(out.Marker) == "" || aws.ToString(out.Marker) == aws.ToString(marker) {
			return nil, errors.New("IAM role tag pagination did not advance")
		}
		marker = out.Marker
	}
}

func iamListInstanceProfileTags(ctx context.Context, name string) ([]iamtypes.Tag, error) {
	var tags []iamtypes.Tag
	var marker *string
	for {
		out, err := IamClient().ListInstanceProfileTags(ctx, &iam.ListInstanceProfileTagsInput{InstanceProfileName: aws.String(name), Marker: marker})
		if err != nil {
			return nil, err
		}
		tags = append(tags, out.Tags...)
		if !out.IsTruncated {
			return tags, nil
		}
		if aws.ToString(out.Marker) == "" || aws.ToString(out.Marker) == aws.ToString(marker) {
			return nil, errors.New("IAM instance-profile tag pagination did not advance")
		}
		marker = out.Marker
	}
}

func infraListSQSAbsent(err error) bool {
	var apiError smithy.APIError
	return errors.As(err, &apiError) && slices.Contains([]string{"QueueDoesNotExist", "AWS.SimpleQueueService.NonExistentQueue"}, apiError.ErrorCode())
}

func infraListSecurityGroups(ctx context.Context, vpcIDs []string) ([]ec2types.SecurityGroup, error) {
	if len(vpcIDs) == 0 {
		return EC2ListSgs(ctx)
	}
	input := &ec2.DescribeSecurityGroupsInput{Filters: []ec2types.Filter{{Name: aws.String("vpc-id"), Values: vpcIDs}}}
	var groups []ec2types.SecurityGroup
	for {
		out, err := EC2Client().DescribeSecurityGroups(ctx, input)
		if err != nil {
			return nil, err
		}
		groups = append(groups, out.SecurityGroups...)
		if out.NextToken == nil {
			return groups, nil
		}
		input.NextToken = out.NextToken
	}
}

// SES receipt rules have no infraset tag. A function's exact invocation grant
// supplies the candidate identity; the caller verifies the actual rule target.
func (scope infraListScope) sesRules(ctx context.Context, functionARN string) ([]sestypes.ReceiptRuleSetMetadata, error) {
	if scope.setName == "" {
		return SesListReceiptRulesets(ctx)
	}
	out, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{FunctionName: aws.String(functionARN)})
	if err != nil {
		var absent *lambdatypes.ResourceNotFoundException
		if errors.As(err, &absent) {
			return nil, nil
		} // no resource-based policy
		return nil, err
	}
	var policy IamPolicyDocument
	if err := json.Unmarshal([]byte(aws.ToString(out.Policy)), &policy); err != nil {
		return nil, err
	}
	var names []string
	var rules []sestypes.ReceiptRuleSetMetadata
	for _, statement := range policy.Statement {
		principal, _ := statement.Principal.(map[string]any)
		if principal["Service"] != "ses.amazonaws.com" {
			continue
		}
		condition, _ := statement.Condition.(map[string]any)
		arnLike, _ := condition["ArnLike"].(map[string]any)
		source, _ := arnLike["AWS:SourceArn"].(string)
		parsed, err := awsarn.Parse(source)
		set, rule, ok := strings.Cut(strings.TrimPrefix(parsed.Resource, "receipt-rule-set/"), ":receipt-rule/")
		_, expected, identityErr := sesReceiptRulePermissionIdentity(functionARN, set, parsed.Region, parsed.AccountID)
		if err != nil || identityErr != nil || !ok || set != rule || source != expected || strings.ContainsAny(source, "*?") ||
			statement.Effect != "Allow" || statement.Action != "lambda:InvokeFunction" || statement.Resource != functionARN {
			return nil, fmt.Errorf("cannot scope SES permission for %s: exact receipt-rule source required", functionARN)
		}
		if !slices.Contains(names, set) {
			names = append(names, set)
			rules = append(rules, sestypes.ReceiptRuleSetMetadata{Name: aws.String(set)})
		}
	}
	return rules, nil
}
