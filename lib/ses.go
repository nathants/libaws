package lib

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	sesv2 "github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

var sesClient *sesv2.Client
var sesClientLock sync.Mutex

func SesClientExplicit(accessKeyID, accessKeySecret, region string) *sesv2.Client {
	return sesv2.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func SesClient() *sesv2.Client {
	sesClientLock.Lock()
	defer sesClientLock.Unlock()
	if sesClient == nil {
		sesClient = sesv2.NewFromConfig(*Session())
	}
	return sesClient
}

type sesReceiptRuleCleanupClient interface {
	ListReceiptRuleSets(context.Context, *sesv2.ListReceiptRuleSetsInput, ...func(*sesv2.Options)) (*sesv2.ListReceiptRuleSetsOutput, error)
	DescribeActiveReceiptRuleSet(context.Context, *sesv2.DescribeActiveReceiptRuleSetInput, ...func(*sesv2.Options)) (*sesv2.DescribeActiveReceiptRuleSetOutput, error)
	DescribeReceiptRuleSet(context.Context, *sesv2.DescribeReceiptRuleSetInput, ...func(*sesv2.Options)) (*sesv2.DescribeReceiptRuleSetOutput, error)
	SetActiveReceiptRuleSet(context.Context, *sesv2.SetActiveReceiptRuleSetInput, ...func(*sesv2.Options)) (*sesv2.SetActiveReceiptRuleSetOutput, error)
	CreateReceiptRule(context.Context, *sesv2.CreateReceiptRuleInput, ...func(*sesv2.Options)) (*sesv2.CreateReceiptRuleOutput, error)
	DeleteReceiptRule(context.Context, *sesv2.DeleteReceiptRuleInput, ...func(*sesv2.Options)) (*sesv2.DeleteReceiptRuleOutput, error)
	DeleteReceiptRuleSet(context.Context, *sesv2.DeleteReceiptRuleSetInput, ...func(*sesv2.Options)) (*sesv2.DeleteReceiptRuleSetOutput, error)
}

type sesReceiptRuleEnsureClient interface {
	sesReceiptRuleCleanupClient
	CreateReceiptRuleSet(context.Context, *sesv2.CreateReceiptRuleSetInput, ...func(*sesv2.Options)) (*sesv2.CreateReceiptRuleSetOutput, error)
	UpdateReceiptRule(context.Context, *sesv2.UpdateReceiptRuleInput, ...func(*sesv2.Options)) (*sesv2.UpdateReceiptRuleOutput, error)
}

func sesListReceiptRulesets(ctx context.Context, client sesReceiptRuleCleanupClient) ([]sestypes.ReceiptRuleSetMetadata, error) {
	var token *string
	var result []sestypes.ReceiptRuleSetMetadata
	for {
		out, err := client.ListReceiptRuleSets(ctx, &sesv2.ListReceiptRuleSetsInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("SES ListReceiptRuleSets returned nil output")
		}
		result = append(result, out.RuleSets...)
		if out.NextToken == nil {
			return result, nil
		}
		token = out.NextToken
	}
}

func SesListReceiptRulesets(ctx context.Context) ([]sestypes.ReceiptRuleSetMetadata, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesListReceiptRulesets"}
		d.Start()
		defer d.End()
	}
	result, err := sesListReceiptRulesets(ctx, SesClient())
	if err != nil {
		Logger.Println("error:", err)
	}
	return result, err
}

func sesRuleSetDoesNotExist(err error) bool {
	var notFound *sestypes.RuleSetDoesNotExistException
	return errors.As(err, &notFound)
}

func sesRuleDoesNotExist(err error) bool {
	var notFound *sestypes.RuleDoesNotExistException
	return errors.As(err, &notFound)
}

func sesRestoreReceiptRuleset(
	ctx context.Context,
	client sesReceiptRuleCleanupClient,
	domain string,
	rule *sestypes.ReceiptRule,
	restoreActive bool,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	var rollbackErrors []error
	ruleRestored := true
	if rule != nil {
		_, err := client.CreateReceiptRule(ctx, &sesv2.CreateReceiptRuleInput{
			RuleSetName: aws.String(domain),
			Rule:        rule,
		})
		if err != nil {
			ruleRestored = false
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore SES receipt rule %q: %w", domain, err))
		}
	}
	if restoreActive && ruleRestored {
		if _, err := client.SetActiveReceiptRuleSet(ctx, &sesv2.SetActiveReceiptRuleSetInput{
			RuleSetName: aws.String(domain),
		}); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore active SES receipt rule set %q: %w", domain, err))
		}
	}
	if len(rollbackErrors) == 0 {
		return cause
	}
	return errors.Join(append([]error{cause}, rollbackErrors...)...)
}

func sesRmReceiptRuleset(ctx context.Context, client sesReceiptRuleCleanupClient, domain string, preview bool) error {
	ruleSets, err := sesListReceiptRulesets(ctx, client)
	if err != nil {
		return err
	}
	found := false
	for _, ruleSet := range ruleSets {
		if aws.ToString(ruleSet.Name) == domain {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	described, err := client.DescribeReceiptRuleSet(ctx, &sesv2.DescribeReceiptRuleSetInput{
		RuleSetName: aws.String(domain),
	})
	if err != nil {
		if sesRuleSetDoesNotExist(err) {
			return nil
		}
		return err
	}
	if described == nil {
		return errors.New("SES DescribeReceiptRuleSet returned nil output")
	}
	if len(described.Rules) > 1 {
		return fmt.Errorf("refusing to delete SES receipt rule set %q with %d rules", domain, len(described.Rules))
	}
	var originalRule *sestypes.ReceiptRule
	if len(described.Rules) == 1 {
		if aws.ToString(described.Rules[0].Name) != domain {
			return fmt.Errorf("refusing to delete SES receipt rule set %q with unexpected rule %q", domain, aws.ToString(described.Rules[0].Name))
		}
		originalRule = &described.Rules[0]
	}
	active, err := client.DescribeActiveReceiptRuleSet(ctx, &sesv2.DescribeActiveReceiptRuleSetInput{})
	if err != nil && !sesRuleSetDoesNotExist(err) {
		return err
	}
	wasActive := active != nil && active.Metadata != nil && aws.ToString(active.Metadata.Name) == domain
	if !preview {
		if wasActive {
			if _, err := client.SetActiveReceiptRuleSet(ctx, &sesv2.SetActiveReceiptRuleSetInput{}); err != nil && !sesRuleSetDoesNotExist(err) {
				return err
			}
		}
		if originalRule != nil {
			_, err := client.DeleteReceiptRule(ctx, &sesv2.DeleteReceiptRuleInput{
				RuleName:    aws.String(domain),
				RuleSetName: aws.String(domain),
			})
			if err != nil && !sesRuleDoesNotExist(err) && !sesRuleSetDoesNotExist(err) {
				return sesRestoreReceiptRuleset(ctx, client, domain, nil, wasActive, err)
			}
		}
		if _, err := client.DeleteReceiptRuleSet(ctx, &sesv2.DeleteReceiptRuleSetInput{
			RuleSetName: aws.String(domain),
		}); err != nil && !sesRuleSetDoesNotExist(err) {
			return sesRestoreReceiptRuleset(ctx, client, domain, originalRule, wasActive, err)
		}
	}
	Logger.Println(PreviewString(preview)+"deleted SES receipt rule:", domain)
	return nil
}

func SesRmReceiptRuleset(ctx context.Context, domain string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesRmReceiptRuleset"}
		d.Start()
		defer d.End()
	}
	err := sesRmReceiptRuleset(ctx, SesClient(), domain, preview)
	if err != nil {
		Logger.Println("error:", err)
	}
	return err
}

func sesDomainValid(domain string) bool {
	if len(domain) > 64 {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
				(char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func sesReceiptRulePermissionIdentity(lambdaARN, domain, region, account string) (string, string, error) {
	parsed, err := awsarn.Parse(lambdaARN)
	if err != nil || parsed.Partition == "" || parsed.Service != "lambda" || parsed.Region != region ||
		parsed.AccountID != account || !sesDomainValid(domain) || !strings.HasPrefix(parsed.Resource, "function:") {
		return "", "", fmt.Errorf("invalid Lambda ARN %q or SES domain %q", lambdaARN, domain)
	}
	lambdaName := strings.TrimPrefix(parsed.Resource, "function:")
	if lambdaName == "" || strings.Contains(lambdaName, ":") {
		return "", "", fmt.Errorf("SES trigger requires an unqualified Lambda ARN, got %q", lambdaARN)
	}
	sourceARN := fmt.Sprintf(
		"arn:%s:ses:%s:%s:receipt-rule-set/%s:receipt-rule/%s",
		parsed.Partition,
		parsed.Region,
		parsed.AccountID,
		domain,
		domain,
	)
	return lambdaName, sourceARN, nil
}

func sesDesiredReceiptRule(domain, bucket, prefix, lambdaARN string) *sestypes.ReceiptRule {
	var prefixParam *string
	if prefix != "" {
		prefixParam = aws.String(prefix)
	}
	return &sestypes.ReceiptRule{
		Enabled:     true,
		Name:        aws.String(domain),
		Recipients:  []string{domain},
		TlsPolicy:   sestypes.TlsPolicyRequire,
		ScanEnabled: false,
		Actions: []sestypes.ReceiptAction{
			{
				S3Action: &sestypes.S3Action{
					BucketName:      aws.String(bucket),
					ObjectKeyPrefix: prefixParam,
				},
			},
			{
				LambdaAction: &sestypes.LambdaAction{
					FunctionArn:    aws.String(lambdaARN),
					InvocationType: sestypes.InvocationTypeEvent,
				},
			},
		},
	}
}

func sesActiveReceiptRuleset(ctx context.Context, client sesReceiptRuleCleanupClient) (string, error) {
	out, err := client.DescribeActiveReceiptRuleSet(ctx, &sesv2.DescribeActiveReceiptRuleSetInput{})
	if err != nil {
		if sesRuleSetDoesNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if out == nil {
		return "", errors.New("SES DescribeActiveReceiptRuleSet returned nil output")
	}
	if out.Metadata == nil {
		return "", nil
	}
	return aws.ToString(out.Metadata.Name), nil
}

func sesRollbackNewReceiptRuleset(
	ctx context.Context,
	client sesReceiptRuleEnsureClient,
	domain string,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	_, rollbackErr := client.DeleteReceiptRuleSet(ctx, &sesv2.DeleteReceiptRuleSetInput{RuleSetName: aws.String(domain)})
	if rollbackErr == nil || sesRuleSetDoesNotExist(rollbackErr) {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("roll back new SES receipt rule set %q: %w", domain, rollbackErr))
}

func sesEnsureReceiptRulesetState(
	ctx context.Context,
	client sesReceiptRuleEnsureClient,
	domain string,
	bucket string,
	prefix string,
	lambdaARN string,
	preview bool,
) error {
	if domain == "" || bucket == "" {
		return errors.New("SES receipt rule requires nonempty domain and bucket")
	}
	ruleSets, err := sesListReceiptRulesets(ctx, client)
	if err != nil {
		return err
	}
	found := false
	for _, ruleSet := range ruleSets {
		if aws.ToString(ruleSet.Name) == domain {
			found = true
			break
		}
	}
	createdSet := false
	if !found && !preview {
		_, err := client.CreateReceiptRuleSet(ctx, &sesv2.CreateReceiptRuleSetInput{RuleSetName: aws.String(domain)})
		if err != nil {
			var exists *sestypes.AlreadyExistsException
			if !errors.As(err, &exists) {
				return err
			}
			found = true
		} else {
			createdSet = true
		}
	}

	desiredRule := sesDesiredReceiptRule(domain, bucket, prefix, lambdaARN)
	var currentRule *sestypes.ReceiptRule
	if found {
		out, err := client.DescribeReceiptRuleSet(ctx, &sesv2.DescribeReceiptRuleSetInput{
			RuleSetName: aws.String(domain),
		})
		if err != nil {
			return err
		}
		if out == nil {
			return errors.New("SES DescribeReceiptRuleSet returned nil output")
		}
		switch len(out.Rules) {
		case 0:
		case 1:
			if aws.ToString(out.Rules[0].Name) != domain {
				return fmt.Errorf("refusing to adopt SES receipt rule set %q with unexpected rule %q", domain, aws.ToString(out.Rules[0].Name))
			}
			currentRule = &out.Rules[0]
		default:
			return fmt.Errorf("refusing to adopt SES receipt rule set %q with %d rules", domain, len(out.Rules))
		}
	}

	if currentRule == nil {
		if !preview {
			_, err := client.CreateReceiptRule(ctx, &sesv2.CreateReceiptRuleInput{
				RuleSetName: aws.String(domain),
				Rule:        desiredRule,
			})
			if err != nil {
				if createdSet {
					return sesRollbackNewReceiptRuleset(ctx, client, domain, err)
				}
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created SES receipt rule:", "domain="+domain, "bucket="+bucket, "prefix="+prefix)
	} else if !reflect.DeepEqual(currentRule, desiredRule) {
		if !preview {
			_, err := client.UpdateReceiptRule(ctx, &sesv2.UpdateReceiptRuleInput{
				Rule:        desiredRule,
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"updated SES receipt rule:", "domain="+domain, "bucket="+bucket, "prefix="+prefix)
	}

	active, err := sesActiveReceiptRuleset(ctx, client)
	if err != nil {
		return err
	}
	if active != domain {
		if !preview {
			_, err := client.SetActiveReceiptRuleSet(ctx, &sesv2.SetActiveReceiptRuleSetInput{RuleSetName: aws.String(domain)})
			if err != nil {
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"activated SES receipt rule set:", domain)
	}
	return nil
}

func SesEnsureReceiptRuleset(ctx context.Context, domain string, bucket string, prefix string, lambdaARN string, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesEnsureReceiptRuleset"}
		d.Start()
		defer d.End()
	}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	lambdaName, sourceARN, err := sesReceiptRulePermissionIdentity(lambdaARN, domain, Region(), account)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sid, err := lambdaEnsurePermission(ctx, lambdaName, "ses.amazonaws.com", sourceARN, preview)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if err := sesEnsureReceiptRulesetState(ctx, SesClient(), domain, bucket, prefix, lambdaARN, preview); err != nil {
		Logger.Println("error:", err)
		return sid, err
	}
	return sid, nil
}

var _ sesReceiptRuleEnsureClient = (*sesv2.Client)(nil)
