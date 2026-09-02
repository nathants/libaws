package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

const (
	lambdaAttrConcurrency = "concurrency"
	lambdaAttrMemory      = "memory"
	lambdaAttrTimeout     = "timeout"
	lambdaAttrLogsTTLDays = "logs-ttl-days"

	lambdaAttrMemoryDefault      = 128
	lambdaAttrTimeoutDefault     = 300
	lambdaAttrLogsTTLDaysDefault = 7

	lambdaTriggerSes           = "ses"
	lambdaTriggerSesAttrDns    = "dns"
	lambdaTriggerSesAttrBucket = "bucket"
	lambdaTriggerSesAttrPrefix = "prefix"

	lambdaTriggerSQS       = "sqs"
	lambdaTrigerS3         = "s3"
	lambdaTriggerDynamoDB  = "dynamodb"
	lambdaTriggerSchedule  = "schedule"
	lambdaTriggerEcr       = "ecr"
	lambdaTriggerApi       = "api"
	lambdaTriggerWebsocket = "websocket"
	lambdaTriggerUrl       = "url"

	lambdaTriggerApiAttrDns    = "dns"
	lambdaTriggerApiAttrDomain = "domain"

	lambdaDollarDefault     = "$default"
	lambdaDollarConnect     = "$connect"
	lambdaDollarDisconnect  = "$disconnect"
	lambdaAuthorizationType = "NONE"
	lambdaRouteSelection    = "${request.body.action}"
	lambdaIntegrationMethod = "POST"
	lambdaPayloadVersion    = "1.0"

	lambdaEnvVarApiID       = "API_ID"
	lambdaEnvVarWebsocketID = "WEBSOCKET_ID"

	lambdaEventRuleNameSeparator = "___"
	LambdaWebsocketSuffix        = lambdaEventRuleNameSeparator + "websocket"

	lambdaRuntimePython    = "python3.13"
	lambdaRuntimeGo        = "provided.al2023"
	lambdaRuntimeContainer = "container"

	lambdaUrlFuncSid   = "FunctionUrlInvoke"
	lambdaUrlInvokeSid = "FunctionUrlInvokeFunction"

	lambdaEnvironmentMaxBytes = 4 * 1024
)

var lambdaClient *lambda.Client
var lambdaClientLock sync.Mutex
var lambdaNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var lambdaEnvironmentNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]+$`)
var lambdaContainerImageDigestPattern = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)

func validateLambdaName(name string) error {
	if !lambdaNamePattern.MatchString(name) {
		return fmt.Errorf("lambda name must match '[A-Za-z0-9_-]{1,64}', got: %s", name)
	}
	return nil
}

func lambdaContainerImageDigest(imageURI string) (string, error) {
	digest := lambdaContainerImageDigestPattern.FindString(imageURI)
	if digest == "" {
		return "", fmt.Errorf("lambda container image URI must end with @sha256: followed by 64 lowercase hexadecimal characters, got: %s", imageURI)
	}
	return strings.TrimPrefix(digest, "@"), nil
}

func validateLambdaContainerImageURI(imageURI string) error {
	_, err := lambdaContainerImageDigest(imageURI)
	return err
}

func lambdaEnvironmentVariables(values []string) (map[string]string, error) {
	variables := make(map[string]string, len(values))
	totalBytes := 0
	for _, value := range values {
		name, content, err := SplitOnce(value, "=")
		if err != nil {
			return nil, err
		}
		if !lambdaEnvironmentNamePattern.MatchString(name) {
			return nil, fmt.Errorf("lambda environment variable names must match '[a-zA-Z][a-zA-Z0-9_]+', got: %s", name)
		}
		if _, duplicate := variables[name]; duplicate {
			return nil, fmt.Errorf("duplicate Lambda environment variable: %s", name)
		}
		variables[name] = content
		totalBytes += len(name) + len(content)
	}
	if totalBytes > lambdaEnvironmentMaxBytes {
		return nil, fmt.Errorf(
			"lambda environment size %d bytes exceeds AWS Lambda limit %d bytes",
			totalBytes,
			lambdaEnvironmentMaxBytes,
		)
	}
	return variables, nil
}

func LambdaClientExplicit(accessKeyID, accessKeySecret, region string) *lambda.Client {
	return lambda.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func LambdaClient() *lambda.Client {
	lambdaClientLock.Lock()
	defer lambdaClientLock.Unlock()
	if lambdaClient == nil {
		lambdaClient = lambda.NewFromConfig(*Session())
	}
	return lambdaClient
}

type lambdaConcurrencyClient interface {
	GetFunctionConcurrency(
		context.Context,
		*lambda.GetFunctionConcurrencyInput,
		...func(*lambda.Options),
	) (*lambda.GetFunctionConcurrencyOutput, error)
	PutFunctionConcurrency(
		context.Context,
		*lambda.PutFunctionConcurrencyInput,
		...func(*lambda.Options),
	) (*lambda.PutFunctionConcurrencyOutput, error)
	DeleteFunctionConcurrency(
		context.Context,
		*lambda.DeleteFunctionConcurrencyInput,
		...func(*lambda.Options),
	) (*lambda.DeleteFunctionConcurrencyOutput, error)
}

func parseLambdaConcurrency(value string) (*int32, error) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid lambda concurrency %q: %w", value, err)
	}
	if parsed < 0 {
		return nil, fmt.Errorf("lambda concurrency must be nonnegative, got %d", parsed)
	}
	concurrency := int32(parsed)
	return &concurrency, nil
}

func lambdaConcurrencyDescription(value *int32) string {
	if value == nil {
		return "unreserved"
	}
	if *value == 0 {
		return "reserved 0 (disabled)"
	}
	return fmt.Sprintf("reserved %d", *value)
}

func lambdaSetConcurrency(
	ctx context.Context,
	client lambdaConcurrencyClient,
	lambdaName string,
	desired *int32,
	preview bool,
) error {
	if desired != nil && *desired < 0 {
		return fmt.Errorf("lambda concurrency must be nonnegative, got %d", *desired)
	}
	out, err := client.GetFunctionConcurrency(ctx, &lambda.GetFunctionConcurrencyInput{
		FunctionName: aws.String(lambdaName),
	})
	if err != nil {
		var notFound *lambdatypes.ResourceNotFoundException
		if !preview || !errors.As(err, &notFound) {
			return fmt.Errorf("get Lambda concurrency for %s: %w", lambdaName, err)
		}
		out = &lambda.GetFunctionConcurrencyOutput{}
	}
	if out == nil {
		return fmt.Errorf("get Lambda concurrency for %s returned nil output", lambdaName)
	}
	current := out.ReservedConcurrentExecutions
	if (current == nil && desired == nil) ||
		(current != nil && desired != nil && *current == *desired) {
		return nil
	}
	if !preview {
		if desired == nil {
			_, err = client.DeleteFunctionConcurrency(ctx, &lambda.DeleteFunctionConcurrencyInput{
				FunctionName: aws.String(lambdaName),
			})
		} else {
			_, err = client.PutFunctionConcurrency(ctx, &lambda.PutFunctionConcurrencyInput{
				FunctionName:                 aws.String(lambdaName),
				ReservedConcurrentExecutions: desired,
			})
		}
		if err != nil {
			return fmt.Errorf("update Lambda concurrency for %s: %w", lambdaName, err)
		}
	}
	Logger.Printf(
		PreviewString(preview)+"updated concurrency: %s => %s\n",
		lambdaConcurrencyDescription(current),
		lambdaConcurrencyDescription(desired),
	)
	return nil
}

func LambdaSetConcurrency(ctx context.Context, lambdaName string, concurrency *int32, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaSetConcurrency"}
		d.Start()
		defer d.End()
	}
	return lambdaSetConcurrency(ctx, LambdaClient(), lambdaName, concurrency, preview)
}

type lambdaURLPermission struct {
	statement IamStatementEntry
	input     *lambda.AddPermissionInput
}

func lambdaURLPermissions(functionName, functionARN string) []lambdaURLPermission {
	return []lambdaURLPermission{
		{
			statement: IamStatementEntry{
				Sid:       lambdaUrlFuncSid,
				Effect:    "Allow",
				Action:    "lambda:InvokeFunctionUrl",
				Resource:  functionARN,
				Principal: "*",
				Condition: map[string]any{
					"StringEquals": map[string]any{
						"lambda:FunctionUrlAuthType": "NONE",
					},
				},
			},
			input: &lambda.AddPermissionInput{
				FunctionName:        aws.String(functionName),
				StatementId:         aws.String(lambdaUrlFuncSid),
				Action:              aws.String("lambda:InvokeFunctionUrl"),
				Principal:           aws.String("*"),
				FunctionUrlAuthType: lambdatypes.FunctionUrlAuthTypeNone,
			},
		},
		{
			statement: IamStatementEntry{
				Sid:       lambdaUrlInvokeSid,
				Effect:    "Allow",
				Action:    "lambda:InvokeFunction",
				Resource:  functionARN,
				Principal: "*",
				Condition: map[string]any{
					"Bool": map[string]any{
						"lambda:InvokedViaFunctionUrl": "true",
					},
				},
			},
			input: &lambda.AddPermissionInput{
				FunctionName:          aws.String(functionName),
				StatementId:           aws.String(lambdaUrlInvokeSid),
				Action:                aws.String("lambda:InvokeFunction"),
				Principal:             aws.String("*"),
				InvokedViaFunctionUrl: aws.Bool(true),
			},
		},
	}
}

func LambdaEnsureTriggerURL(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerURL"}
		d.Start()
		defer d.End()
	}
	var sids []string
	hasURL := false
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerUrl {
			hasURL = true
			break
		}
	}
	if hasURL {
		outCfg, err := LambdaClient().GetFunctionUrlConfig(ctx, &lambda.GetFunctionUrlConfigInput{
			FunctionName: aws.String(infraLambda.Name),
		})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if !errors.As(err, &notFound) {
				Logger.Println("error:", err)
				return nil, err
			}
			if !preview {
				_, err := LambdaClient().CreateFunctionUrlConfig(ctx, &lambda.CreateFunctionUrlConfigInput{
					FunctionName: aws.String(infraLambda.Name),
					AuthType:     lambdatypes.FunctionUrlAuthTypeNone,
					InvokeMode:   lambdatypes.InvokeModeResponseStream,
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			Logger.Println(PreviewString(preview)+"created function url:", infraLambda.Name)
		} else {
			if outCfg.AuthType != lambdatypes.FunctionUrlAuthTypeNone ||
				outCfg.InvokeMode != lambdatypes.InvokeModeResponseStream {
				if !preview {
					_, err = LambdaClient().UpdateFunctionUrlConfig(ctx, &lambda.UpdateFunctionUrlConfigInput{
						FunctionName: aws.String(infraLambda.Name),
						AuthType:     lambdatypes.FunctionUrlAuthTypeNone,
						InvokeMode:   lambdatypes.InvokeModeResponseStream,
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
				Logger.Println(PreviewString(preview)+"updated function url config:", infraLambda.Name)
			}
		}
		out, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{
			FunctionName: aws.String(infraLambda.Name),
		})
		if err != nil {
			var nfe *lambdatypes.ResourceNotFoundException
			if !errors.As(err, &nfe) {
				Logger.Println("error:", err)
				return nil, err
			}
			out = nil
		}
		permissions := lambdaURLPermissions(infraLambda.Name, infraLambda.Arn)
		needAdd := make(map[string]bool, len(permissions))
		for _, permission := range permissions {
			needAdd[permission.statement.Sid] = true
		}
		if out != nil && out.Policy != nil {
			var pol IamPolicyDocument
			if err := json.Unmarshal([]byte(*out.Policy), &pol); err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			for _, st := range pol.Statement {
				for _, permission := range permissions {
					if st.Sid != permission.statement.Sid {
						continue
					}
					expectedPolicy := IamPolicyDocument{
						Version:   "2012-10-17",
						Statement: []IamStatementEntry{permission.statement},
					}
					actualPolicy := IamPolicyDocument{
						Version:   "2012-10-17",
						Statement: []IamStatementEntry{st},
					}
					equal, err := iamPolicyEqual(Pformat(actualPolicy), Pformat(expectedPolicy))
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					if equal {
						needAdd[permission.statement.Sid] = false
						break
					}
					if !preview {
						_, err := LambdaClient().RemovePermission(ctx, &lambda.RemovePermissionInput{
							FunctionName: aws.String(infraLambda.Name),
							StatementId:  aws.String(permission.statement.Sid),
						})
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
					}
					Logger.Println(PreviewString(preview)+"removed misconfigured function url permission:", infraLambda.Name, permission.statement.Sid)
					break
				}
			}
		}
		for _, permission := range permissions {
			if needAdd[permission.statement.Sid] {
				if !preview {
					_, err := LambdaClient().AddPermission(ctx, permission.input)
					if err != nil {
						var conflict *lambdatypes.ResourceConflictException
						if !errors.As(err, &conflict) {
							Logger.Println("error:", err)
							return nil, err
						}
					}
				}
				Logger.Println(PreviewString(preview)+"added function url permission:", infraLambda.Name, permission.statement.Sid)
			}
			sids = append(sids, permission.statement.Sid)
		}
	} else {
		_, err := LambdaClient().GetFunctionUrlConfig(ctx, &lambda.GetFunctionUrlConfigInput{
			FunctionName: aws.String(infraLambda.Name),
		})
		if err == nil {
			if !preview {
				_, err := LambdaClient().DeleteFunctionUrlConfig(ctx, &lambda.DeleteFunctionUrlConfigInput{
					FunctionName: aws.String(infraLambda.Name),
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			Logger.Println(PreviewString(preview)+"deleted function url:", infraLambda.Name)
		} else {
			var notFound *lambdatypes.ResourceNotFoundException
			if !errors.As(err, &notFound) {
				Logger.Println("error:", err)
				return nil, err
			}
		}
	}
	return sids, nil
}

func LambdaArn(ctx context.Context, name string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaArn"}
		d.Start()
		defer d.End()
	}
	var expectedErr error
	var arn string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				expectedErr = err
				return nil
			}
			return err
		}
		arn = *out.Configuration.FunctionArn
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if expectedErr != nil {
		return "", expectedErr
	}
	return arn, nil
}

type lambdaIdentity struct {
	name         string
	arn          string
	infraSetName string
	exists       bool
}

func lambdaFunctionARN(partition, region, account, name string) string {
	return fmt.Sprintf("arn:%s:lambda:%s:%s:function:%s", partition, region, account, name)
}

func lambdaFunctionARNMatches(functionARN, region, name string) bool {
	parsed, err := arn.Parse(functionARN)
	if err != nil || parsed.Partition == "" || parsed.Service != "lambda" ||
		parsed.Region != region || parsed.AccountID == "" {
		return false
	}
	return parsed.Resource == "function:"+name
}

func lambdaIdentityFromCallerARN(name, infraSetName, region, callerARN string) (lambdaIdentity, error) {
	caller, err := arn.Parse(callerARN)
	if err != nil || caller.Partition == "" || caller.AccountID == "" ||
		(caller.Service != "sts" && caller.Service != "iam") || region == "" || !lambdaNamePattern.MatchString(name) {
		return lambdaIdentity{}, fmt.Errorf("invalid Lambda name %q, region %q, or STS caller ARN %q", name, region, callerARN)
	}
	return lambdaIdentity{
		name:         name,
		arn:          lambdaFunctionARN(caller.Partition, region, caller.AccountID, name),
		infraSetName: infraSetName,
	}, nil
}

type lambdaARNResolver func(context.Context, string) (string, error)
type lambdaCallerARNResolver func(context.Context) (string, error)

func lambdaResolveIdentityWith(
	ctx context.Context,
	name string,
	infraSetName string,
	region string,
	resolveFunctionARN lambdaARNResolver,
	resolveCallerARN lambdaCallerARNResolver,
) (lambdaIdentity, error) {
	functionARN, err := resolveFunctionARN(ctx, name)
	if err == nil {
		if !lambdaFunctionARNMatches(functionARN, region, name) {
			return lambdaIdentity{}, fmt.Errorf("invalid Lambda function ARN %q", functionARN)
		}
		return lambdaIdentity{name: name, arn: functionARN, infraSetName: infraSetName, exists: true}, nil
	}
	var notFound *lambdatypes.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return lambdaIdentity{}, err
	}
	callerARN, err := resolveCallerARN(ctx)
	if err != nil {
		return lambdaIdentity{}, err
	}
	return lambdaIdentityFromCallerARN(name, infraSetName, region, callerARN)
}

func lambdaResolveIdentity(ctx context.Context, name, infraSetName string) (lambdaIdentity, error) {
	return lambdaResolveIdentityWith(ctx, name, infraSetName, Region(), LambdaArn, StsArn)
}

const lambdaEcrEventPattern = `{
  "source": ["aws.ecr"],
  "detail-type": ["ECR Image Action"],
  "detail": {
    "result": ["SUCCESS"]
  }
}`

func lambdaSesS3ActionOwned(action *sestypes.ReceiptAction) bool {
	return action != nil && action.S3Action != nil && aws.ToString(action.S3Action.BucketName) != "" &&
		action.S3Action.IamRoleArn == nil && action.S3Action.KmsKeyArn == nil && action.S3Action.TopicArn == nil &&
		action.AddHeaderAction == nil && action.BounceAction == nil && action.ConnectAction == nil &&
		action.LambdaAction == nil && action.SNSAction == nil && action.StopAction == nil && action.WorkmailAction == nil
}

func lambdaSesLambdaActionOwned(action *sestypes.ReceiptAction, functionARN string) bool {
	return action != nil && action.LambdaAction != nil &&
		aws.ToString(action.LambdaAction.FunctionArn) == functionARN &&
		action.LambdaAction.InvocationType == sestypes.InvocationTypeEvent && action.LambdaAction.TopicArn == nil &&
		action.AddHeaderAction == nil && action.BounceAction == nil && action.ConnectAction == nil &&
		action.S3Action == nil && action.SNSAction == nil && action.StopAction == nil && action.WorkmailAction == nil
}

func lambdaSesRuleOwned(ruleSetName string, rule *sestypes.ReceiptRule, functionARN string) bool {
	return rule != nil && aws.ToString(rule.Name) == ruleSetName && rule.Enabled &&
		rule.TlsPolicy == sestypes.TlsPolicyRequire && !rule.ScanEnabled &&
		len(rule.Recipients) == 1 && rule.Recipients[0] == ruleSetName && len(rule.Actions) == 2 &&
		lambdaSesS3ActionOwned(&rule.Actions[0]) && lambdaSesLambdaActionOwned(&rule.Actions[1], functionARN)
}

func LambdaEnsureTriggerSes(ctx context.Context, infraLambda *InfraLambda, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerSes"}
		d.Start()
		defer d.End()
	}
	var triggers []*InfraTrigger
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerSes {
			triggers = append(triggers, trigger)
		}
	}
	if len(triggers) > 1 {
		return "", errors.New("a Lambda can declare only one SES trigger because AWS permits only one active receipt rule set per region")
	}
	sid := ""
	domains := []string{}
	for _, trigger := range triggers {
		domainName := ""
		bucket := ""
		prefix := ""
		for _, attr := range trigger.Attr {
			k, v, err := SplitOnce(attr, "=")
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			switch k {
			case lambdaTriggerSesAttrDns: // domain already enrolled in ses, ie example.com
				domainName = v
			case lambdaTriggerSesAttrBucket:
				bucket = v
			case lambdaTriggerSesAttrPrefix:
				prefix = v
			}
		}
		if domainName == "" {
			return "", fmt.Errorf("ses trigger needs dns=VALUE")
		}
		if bucket == "" {
			return "", fmt.Errorf("ses trigger needs bucket=VALUE")
		}
		domains = append(domains, domainName)
		var err error
		sid, err = SesEnsureReceiptRuleset(ctx, domainName, bucket, prefix, infraLambda.Arn, preview)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
	}
	identity := lambdaIdentity{
		name:         infraLambda.Name,
		arn:          infraLambda.Arn,
		infraSetName: infraLambda.infraSetName,
		exists:       true,
	}
	if err := lambdaCleanupSESTriggers(ctx, SesClient(), identity, domains, preview); err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return sid, nil
}

func LambdaEnsureTriggerEcr(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerEcr"}
		d.Start()
		defer d.End()
	}
	ruleName := lambdaEventRuleName(infraLambda.Name, "trigger_ecr")
	var permissionSids []string
	var triggers []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerEcr {
			triggers = append(triggers, trigger.Type)
			break
		}
	}
	if len(triggers) > 0 {
		ruleArn, err := lambdaEventRuleARN(infraLambda.Arn, ruleName)
		if err != nil {
			return nil, err
		}
		existingRule := false
		out, err := EventsClient().DescribeRule(ctx, &eventbridge.DescribeRuleInput{
			Name: aws.String(ruleName),
		})
		if err != nil {
			var rnfe *eventbridgetypes.ResourceNotFoundException
			if !errors.As(err, &rnfe) {
				return nil, err
			}
			if !preview {
				out, err := EventsClient().PutRule(ctx, &eventbridge.PutRuleInput{
					Name:         aws.String(ruleName),
					EventPattern: aws.String(lambdaEcrEventPattern),
					Tags: []eventbridgetypes.Tag{{
						Key:   aws.String(infraSetTagName),
						Value: aws.String(infraLambda.infraSetName),
					}},
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				if out == nil || aws.ToString(out.RuleArn) != ruleArn {
					return nil, fmt.Errorf("EventBridge PutRule returned ARN %q for %q, want %q", aws.ToString(out.RuleArn), ruleName, ruleArn)
				}
			}
			Logger.Println(PreviewString(preview)+"created ecr rule:", ruleName)
		} else {
			existingRule = true
			if aws.ToString(out.EventPattern) != lambdaEcrEventPattern {
				err := fmt.Errorf("ecr rule misconfigured: %s %s != %s", ruleName, lambdaEcrEventPattern, aws.ToString(out.EventPattern))
				Logger.Println("error:", err)
				return nil, err
			}
			if aws.ToString(out.Arn) != ruleArn {
				return nil, fmt.Errorf("EventBridge DescribeRule returned ARN %q for %q, want %q", aws.ToString(out.Arn), ruleName, ruleArn)
			}
		}
		var targets []eventbridgetypes.Target
		err = Retry(ctx, func() error {
			var err error
			targets, err = EventsListRuleTargets(ctx, ruleName, nil)
			var rnfe *eventbridgetypes.ResourceNotFoundException
			if errors.As(err, &rnfe) {
				return nil
			}
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch len(targets) {
		case 0:
			if !preview {
				if err := lambdaPutEventTarget(ctx, EventsClient(), ruleName, infraLambda.Arn); err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			Logger.Println(PreviewString(preview)+"created ecr rule target:", ruleName, infraLambda.Arn)
		case 1:
			if aws.ToString(targets[0].Id) != "1" || aws.ToString(targets[0].Arn) != infraLambda.Arn {
				err := fmt.Errorf("ecr rule is misconfigured with unknown target: %s %#v", infraLambda.Arn, targets[0])
				Logger.Println("error:", err)
				return nil, err
			}
		default:
			var targetArns []string
			for _, target := range targets {
				targetArns = append(targetArns, aws.ToString(target.Arn))
			}
			err := fmt.Errorf("ecr rule is misconfigured with unknown targets: %s %v", infraLambda.Arn, targetArns)
			Logger.Println("error:", err)
			return nil, err
		}
		if existingRule {
			if err := lambdaEnsureEventRuleTag(ctx, EventsClient(), ruleArn, infraLambda.infraSetName, preview); err != nil {
				return nil, err
			}
		}
		sid, err := lambdaEnsurePermission(ctx, infraLambda.Name, "events.amazonaws.com", ruleArn, preview)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		permissionSids = append(permissionSids, sid)
	}
	identity := lambdaIdentity{
		name:         infraLambda.Name,
		arn:          infraLambda.Arn,
		infraSetName: infraLambda.infraSetName,
		exists:       true,
	}
	if err := lambdaCleanupECRTrigger(ctx, EventsClient(), identity, len(triggers) > 0, preview); err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return permissionSids, nil
}

func lambdaS3SourceARN(functionARN, bucket string) (string, error) {
	parsed, err := arn.Parse(functionARN)
	if err != nil || parsed.Partition == "" || parsed.Service != "lambda" || bucket == "" {
		return "", fmt.Errorf("invalid Lambda ARN %q or S3 bucket %q", functionARN, bucket)
	}
	return fmt.Sprintf("arn:%s:s3:::%s", parsed.Partition, bucket), nil
}

func LambdaEnsureTriggerS3(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerS3"}
		d.Start()
		defer d.End()
	}
	events := []s3types.Event{
		"s3:ObjectCreated:*",
		"s3:ObjectRemoved:*",
	}
	var triggerBuckets []string
	var permissionSids []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type != lambdaTrigerS3 {
			continue
		}
		bucket := trigger.Attr[0]
		triggerBuckets = append(triggerBuckets, bucket)
		sourceARN, err := lambdaS3SourceARN(infraLambda.Arn, bucket)
		if err != nil {
			return nil, err
		}
		sid, err := lambdaEnsurePermission(
			ctx, infraLambda.Name, "s3.amazonaws.com", sourceARN, preview,
		)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		permissionSids = append(permissionSids, sid)
		if err := lambdaEnsureDesiredS3Trigger(
			ctx, lambdaAWSClientForBucket, infraLambda, bucket, events, preview,
		); err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	if err := lambdaRemoveStaleS3Triggers(
		ctx, S3Client(), lambdaAWSClientForBucket, infraLambda, triggerBuckets, preview,
	); err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return permissionSids, nil
}

func lambdaRemoveUnusedPermissions(ctx context.Context, name string, permissionSids []string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaRemoveUnusedPermissions"}
		d.Start()
		defer d.End()
	}
	out, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
		var notFound *lambdatypes.ResourceNotFoundException
		if !errors.As(err, &notFound) {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	policy := IamPolicyDocument{}
	err = json.Unmarshal([]byte(*out.Policy), &policy)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, statement := range policy.Statement {
		if !slices.Contains(permissionSids, statement.Sid) {
			if !preview {
				_, err := LambdaClient().RemovePermission(ctx, &lambda.RemovePermissionInput{
					FunctionName: aws.String(name),
					StatementId:  aws.String(statement.Sid),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"deleted unused lambda permissions:", name, statement.Sid)
		}
	}
	return nil
}

func lambdaPermissionSID(callerPrincipal, callerARN string) string {
	sid := strings.ReplaceAll(callerPrincipal, ".", "-") + "__" + Last(strings.Split(callerARN, ":"))
	sid = strings.ReplaceAll(sid, "$", "DOLLAR")
	sid = strings.ReplaceAll(sid, "*", "ALL")
	sid = strings.ReplaceAll(sid, ".", "DOT")
	sid = strings.ReplaceAll(sid, "-", "_")
	sid = strings.ReplaceAll(sid, "/", "__")
	return sid
}

func lambdaAddPermissionInput(functionARN, sid, callerPrincipal, callerARN, sourceAccount string) *lambda.AddPermissionInput {
	input := &lambda.AddPermissionInput{
		FunctionName: aws.String(functionARN),
		StatementId:  aws.String(sid),
		Action:       aws.String("lambda:InvokeFunction"),
		Principal:    aws.String(callerPrincipal),
		SourceArn:    aws.String(callerARN),
	}
	if sourceAccount != "" {
		input.SourceAccount = aws.String(sourceAccount)
	}
	return input
}

func lambdaAddPermission(ctx context.Context, sid, name, callerPrincipal, callerARN, sourceAccount string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaAddPermission"}
		d.Start()
		defer d.End()
	}
	functionARN, err := LambdaArn(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !lambdaFunctionARNMatches(functionARN, Region(), name) {
		return fmt.Errorf("invalid Lambda function ARN %q", functionARN)
	}
	_, err = LambdaClient().AddPermission(ctx, lambdaAddPermissionInput(
		functionARN, sid, callerPrincipal, callerARN, sourceAccount,
	))
	return err
}

func lambdaSourceAccountPermissionMatches(statement IamStatementEntry, sid, functionARN, callerPrincipal, callerARN, sourceAccount string) (bool, error) {
	condition := map[string]any{
		"ArnLike": map[string]any{"AWS:SourceArn": callerARN},
	}
	if sourceAccount != "" {
		condition["StringEquals"] = map[string]any{"AWS:SourceAccount": sourceAccount}
	}
	expected := IamStatementEntry{
		Sid:       sid,
		Effect:    "Allow",
		Principal: map[string]any{"Service": callerPrincipal},
		Action:    "lambda:InvokeFunction",
		Resource:  functionARN,
		Condition: condition,
	}
	actualPolicy := IamPolicyDocument{Version: "2012-10-17", Statement: []IamStatementEntry{statement}}
	expectedPolicy := IamPolicyDocument{Version: "2012-10-17", Statement: []IamStatementEntry{expected}}
	return iamPolicyEqual(Pformat(actualPolicy), Pformat(expectedPolicy))
}

func lambdaPermissionReconciliation(policyString, sid, functionARN, callerPrincipal, callerARN, sourceAccount string) (needsUpdate, removeExisting bool, err error) {
	if policyString == "" {
		return true, false, nil
	}
	policy := IamPolicyDocument{}
	if err := json.Unmarshal([]byte(policyString), &policy); err != nil {
		return false, false, err
	}
	for _, statement := range policy.Statement {
		if statement.Sid != sid {
			continue
		}
		matches, err := lambdaSourceAccountPermissionMatches(statement, sid, functionARN, callerPrincipal, callerARN, sourceAccount)
		if err != nil {
			return false, false, err
		}
		return !matches, !matches, nil
	}
	return true, false, nil
}

func lambdaEnsurePermissionWithSourceAccount(ctx context.Context, name, callerPrincipal, callerARN, sourceAccount string, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsurePermission"}
		d.Start()
		defer d.End()
	}
	sid := lambdaPermissionSID(callerPrincipal, callerARN)
	var policyString string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{FunctionName: aws.String(name)})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				return nil
			}
			return err
		}
		policyString = aws.ToString(out.Policy)
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	functionARN := ""
	if policyString != "" {
		functionARN, err = LambdaArn(ctx, name)
		if err != nil {
			return "", err
		}
		if !lambdaFunctionARNMatches(functionARN, Region(), name) {
			return "", fmt.Errorf("invalid Lambda function ARN %q", functionARN)
		}
	}
	needsUpdate, removeExisting, err := lambdaPermissionReconciliation(policyString, sid, functionARN, callerPrincipal, callerARN, sourceAccount)
	if err != nil {
		return "", err
	}
	if !needsUpdate {
		return sid, nil
	}
	if !preview {
		if removeExisting {
			if _, err := LambdaClient().RemovePermission(ctx, &lambda.RemovePermissionInput{
				FunctionName: aws.String(name),
				StatementId:  aws.String(sid),
			}); err != nil {
				return "", err
			}
		}
		if err := lambdaAddPermission(ctx, sid, name, callerPrincipal, callerARN, sourceAccount); err != nil {
			return "", err
		}
	}
	Logger.Println(PreviewString(preview)+"updated lambda permission:", name, callerPrincipal, callerARN)
	return sid, nil
}

func lambdaEnsurePermission(ctx context.Context, name, callerPrincipal, callerARN string, preview bool) (string, error) {
	return lambdaEnsurePermissionWithSourceAccount(ctx, name, callerPrincipal, callerARN, "", preview)
}

func LambdaArnToLambdaName(arn string) string {
	// "arn:aws:lambda:%s:%s:function:%s"
	name := Last(strings.Split(arn, ":"))
	return name
}

func lambdaEnsureTriggerApi(ctx context.Context, infraSetName, apiName, arnLambda string, protocolType apitypes.ProtocolType, preview bool) (*apitypes.Api, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApi"}
		d.Start()
		defer d.End()
	}
	var pType apitypes.ProtocolType
	if !slices.Contains(pType.Values(), protocolType) {
		err := fmt.Errorf("invalid protocol type: %s", protocolType)
		Logger.Println("error:", err)
		return nil, err
	}
	api, err := Api(ctx, apiName)
	if err != nil && err.Error() != ErrApiNotFound {
		Logger.Println("error:", err)
		return nil, err
	}
	if api == nil {
		if !preview {
			input := &apigatewayv2.CreateApiInput{
				Name:         aws.String(apiName),
				ProtocolType: apitypes.ProtocolType(protocolType),
				Tags: map[string]string{
					infraSetTagName: infraSetName,
				},
			}
			if protocolType == apitypes.ProtocolTypeWebsocket {
				input.RouteKey = aws.String(lambdaDollarDefault)
				input.Target = aws.String(arnLambda)
				input.RouteSelectionExpression = aws.String(lambdaRouteSelection)
			}
			_, err := ApiClient().CreateApi(ctx, input)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			api, err := Api(ctx, apiName)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			return api, nil
		}
		Logger.Println(PreviewString(preview)+"created api:", apiName)
		return nil, nil
	}
	if api.ProtocolType != apitypes.ProtocolType(protocolType) {
		err := fmt.Errorf("api protocol type misconfigured for %s %s: %v != %v", apiName, *api.ApiId, api.ProtocolType, protocolType)
		Logger.Println("error:", err)
		return nil, err
	}
	return api, nil
}

func lambdaEnsureTriggerApiIntegrationStageRoute(ctx context.Context, name, arnLambda string, protocolType apitypes.ProtocolType, api *apitypes.Api, timeoutMillis int32, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiIntegrationStageRoute"}
		d.Start()
		defer d.End()
	}
	if api == nil && preview {
		Logger.Println(PreviewString(preview)+"created api integration:", name)
		Logger.Println(PreviewString(preview)+"created api stage:", name)
		Logger.Println(PreviewString(preview)+"created api route:", name)
		return "", nil
	}
	lambdaName, sourceARN, err := lambdaAPIPermissionIdentity(arnLambda, aws.ToString(api.ApiId))
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	var integrationID string
	var integrations []apitypes.Integration
	if !(preview && api == nil) {
		integrations, err = lambdaAPIIntegrations(ctx, ApiClient(), api.ApiId)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
	}
	switch len(integrations) {
	case 0:
		if !preview {
			out, err := ApiClient().CreateIntegration(ctx, &apigatewayv2.CreateIntegrationInput{
				ApiId:                api.ApiId,
				IntegrationUri:       aws.String(arnLambda),
				ConnectionType:       apitypes.ConnectionTypeInternet,
				IntegrationType:      apitypes.IntegrationTypeAwsProxy,
				IntegrationMethod:    aws.String(lambdaIntegrationMethod),
				TimeoutInMillis:      aws.Int32(timeoutMillis),
				PayloadFormatVersion: aws.String(lambdaPayloadVersion),
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			if out == nil || out.IntegrationId == nil {
				return "", fmt.Errorf("API Gateway CreateIntegration returned no integration ID for %q", name)
			}
			integrationID = aws.ToString(out.IntegrationId)
		}
		Logger.Println(PreviewString(preview)+"created api integration:", name)
	case 1:
		integration := &integrations[0]
		if !lambdaAPIIntegrationMatches(integration, arnLambda) {
			return "", fmt.Errorf("api integration target misconfigured for %s %s: %q != %q", name, aws.ToString(api.ApiId), aws.ToString(integration.IntegrationUri), arnLambda)
		}
		integrationID = aws.ToString(integration.IntegrationId)
		if integrationID == "" {
			return "", fmt.Errorf("api integration for %s %s has no ID", name, aws.ToString(api.ApiId))
		}
		if integration.ConnectionType != apitypes.ConnectionTypeInternet {
			err := fmt.Errorf("api connection type misconfigured for %s %s: %s != %s", name, aws.ToString(api.ApiId), integration.ConnectionType, apitypes.ConnectionTypeInternet)
			Logger.Println("error:", err)
			return "", err
		}
		if integration.IntegrationType != apitypes.IntegrationTypeAwsProxy {
			err := fmt.Errorf("api integration type misconfigured for %s %s: %s != %s", name, aws.ToString(api.ApiId), integration.IntegrationType, apitypes.IntegrationTypeAwsProxy)
			Logger.Println("error:", err)
			return "", err
		}
		if aws.ToString(integration.IntegrationMethod) != lambdaIntegrationMethod {
			err := fmt.Errorf("api integration method misconfigured for %s %s: %s != %s", name, aws.ToString(api.ApiId), aws.ToString(integration.IntegrationMethod), lambdaIntegrationMethod)
			Logger.Println("error:", err)
			return "", err
		}
		if aws.ToInt32(integration.TimeoutInMillis) != timeoutMillis {
			err := fmt.Errorf("api timeout misconfigured for %s %s: %d != %d", name, aws.ToString(api.ApiId), aws.ToInt32(integration.TimeoutInMillis), timeoutMillis)
			Logger.Println("error:", err)
			return "", err
		}
		if aws.ToString(integration.PayloadFormatVersion) != lambdaPayloadVersion {
			err := fmt.Errorf("api payload format version misconfigured for %s %s: %s != %s", name, aws.ToString(api.ApiId), aws.ToString(integration.PayloadFormatVersion), lambdaPayloadVersion)
			Logger.Println("error:", err)
			return "", err
		}
	default:
		err := fmt.Errorf("api has more than one integration: %s %v", name, Pformat(integrations))
		Logger.Println("error:", err)
		return "", err
	}
	getStageOut, err := ApiClient().GetStage(ctx, &apigatewayv2.GetStageInput{
		ApiId:     api.ApiId,
		StageName: aws.String(lambdaDollarDefault),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return "", err
		}
		if !preview {
			_, err := ApiClient().CreateStage(ctx, &apigatewayv2.CreateStageInput{
				ApiId:      api.ApiId,
				AutoDeploy: aws.Bool(true),
				StageName:  aws.String(lambdaDollarDefault),
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
		Logger.Println(PreviewString(preview)+"created api stage:", name)
	} else {
		if *getStageOut.StageName != lambdaDollarDefault {
			err := fmt.Errorf("api stage name misconfigured for %s %s: %s != %s", name, *api.ApiId, *getStageOut.StageName, lambdaDollarDefault)
			Logger.Println("error:", err)
			return "", err
		}
		if !*getStageOut.AutoDeploy {
			err := fmt.Errorf("api stage auto deploy misconfigured for %s %s, should be enabled", name, *api.ApiId)
			Logger.Println("error:", err)
			return "", err
		}
	}
	getRoutesOut, err := ApiClient().GetRoutes(ctx, &apigatewayv2.GetRoutesInput{
		ApiId:      api.ApiId,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(getRoutesOut.Items) == 500 {
		err := fmt.Errorf("api has 500 or more routes: %s %s", name, *api.ApiId)
		Logger.Println("error:", err)
		return "", err
	}
	var routeKeys []string
	if protocolType == apitypes.ProtocolTypeHttp {
		routeKeys = []string{lambdaDollarDefault}
	} else if protocolType == apitypes.ProtocolTypeWebsocket {
		routeKeys = []string{lambdaDollarDefault, lambdaDollarConnect, lambdaDollarDisconnect}
	}
	for _, routeKey := range routeKeys {
		var routes []apitypes.Route
		for _, route := range getRoutesOut.Items {
			if *route.RouteKey == routeKey {
				routes = append(routes, route)
			}
		}
		switch len(routes) {
		case 0:
			if !preview {
				_, err := ApiClient().CreateRoute(ctx, &apigatewayv2.CreateRouteInput{
					ApiId:             api.ApiId,
					Target:            aws.String(fmt.Sprintf("integrations/%s", integrationID)),
					RouteKey:          aws.String(routeKey),
					AuthorizationType: apitypes.AuthorizationType(lambdaAuthorizationType),
					ApiKeyRequired:    aws.Bool(false),
				})
				if err != nil {
					Logger.Println("error:", err)
					return "", err
				}
			}
			Logger.Println(PreviewString(preview)+"created api route:", name, routeKey)
		case 1:
			route := routes[0]
			if aws.ToString(route.Target) != fmt.Sprintf("integrations/%s", integrationID) {
				err := fmt.Errorf("api route target misconfigured for %s %s: %s != %s", name, aws.ToString(api.ApiId), aws.ToString(route.Target), fmt.Sprintf("integrations/%s", integrationID))
				Logger.Println("error:", err)
				return "", err
			}
			if *route.RouteKey != routeKey {
				err := fmt.Errorf("api route key misconfigured for %s %s: %s != %s", name, *api.ApiId, *route.RouteKey, routeKey)
				Logger.Println("error:", err)
				return "", err
			}
			if route.AuthorizationType != apitypes.AuthorizationType(lambdaAuthorizationType) {
				err := fmt.Errorf("api route authorization type misconfigured for %s %s: %s != %s", name, *api.ApiId, route.AuthorizationType, lambdaAuthorizationType)
				Logger.Println("error:", err)
				return "", err
			}
			if *route.ApiKeyRequired {
				err := fmt.Errorf("api route apiKeyRequired misconfigured for %s %s, should be disabled", name, *api.ApiId)
				Logger.Println("error:", err)
				return "", err
			}
		default:
			err := fmt.Errorf("api has more than one route: %s %s %v", name, routeKey, Pformat(routes))
			Logger.Println("error:", err)
			return "", err
		}
	}
	sid, err := lambdaEnsurePermission(ctx, lambdaName, "apigateway.amazonaws.com", sourceARN, preview)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return sid, nil
}

func lambdaEnsureTriggerApiDomainName(ctx context.Context, name, domain string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiDomainName"}
		d.Start()
		defer d.End()
	}
	out, err := ApiClient().GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(domain),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return err
		}
		certs, err := AcmListCertificates(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		arnCert := ""
		for _, cert := range certs {
			if *cert.DomainName == domain {
				arnCert = *cert.CertificateArn
				break
			}
		}
		if arnCert == "" {
			_, parentDomain, err := SplitOnce(domain, ".")
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			certs, err := AcmListCertificates(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, cert := range certs {
				descOut, err := AcmClient().DescribeCertificate(ctx, &acm.DescribeCertificateInput{
					CertificateArn: cert.CertificateArn,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				wildcard := fmt.Sprintf("*.%s", parentDomain)
				if slices.Contains(descOut.Certificate.SubjectAlternativeNames, wildcard) {
					arnCert = *cert.CertificateArn
					break
				}
			}
		}
		if arnCert == "" {
			err := fmt.Errorf("no acm cert found for: %s", domain)
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			var unexpectedErr error
			err := Retry(ctx, func() error {
				_, err := ApiClient().CreateDomainName(ctx, &apigatewayv2.CreateDomainNameInput{
					DomainName: aws.String(domain),
					DomainNameConfigurations: []apitypes.DomainNameConfiguration{{
						ApiGatewayDomainName: aws.String(domain),
						CertificateArn:       aws.String(arnCert),
						EndpointType:         apitypes.EndpointTypeRegional,
						SecurityPolicy:       apitypes.SecurityPolicyTls12,
					}},
				})
				if err != nil {
					if strings.Contains(err.Error(), "TooManyRequestsException") {
						Logger.Println("create domain has low rate limits, sleeping then retrying")
						time.Sleep(15 * time.Second)
					} else {
						unexpectedErr = err
						return nil
					}
				}
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if unexpectedErr != nil {
				Logger.Println("error:", unexpectedErr)
				return unexpectedErr
			}
		}
		Logger.Println(PreviewString(preview)+"created api domain:", name, domain)
	} else {
		if len(out.DomainNameConfigurations) != 1 || out.DomainNameConfigurations[0].EndpointType != apitypes.EndpointTypeRegional {
			err := fmt.Errorf("api endpoint type misconfigured: %s", Pformat(out.DomainNameConfigurations))
			Logger.Println("error:", err)
			return err
		}
		if out.DomainNameConfigurations[0].SecurityPolicy == "" || out.DomainNameConfigurations[0].SecurityPolicy != apitypes.SecurityPolicyTls12 {
			err := fmt.Errorf("api security policy misconfigured: %s", out.DomainNameConfigurations[0].SecurityPolicy)
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDnsRecords(ctx context.Context, name, subDomain string, zone route53types.HostedZone, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiDnsRecords"}
		d.Start()
		defer d.End()
	}
	out, err := ApiClient().GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(subDomain),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println(PreviewString(preview)+"created api dns:", name, subDomain)
	} else {
		records, err := Route53ListRecords(ctx, *zone.Id)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		found := false
		needsUpdate := false
		for _, record := range records {
			if strings.TrimRight(*record.Name, ".") == subDomain && record.Type == route53types.RRTypeA {
				found = true
				if strings.TrimRight(*record.AliasTarget.DNSName, ".") != *out.DomainNameConfigurations[0].ApiGatewayDomainName {
					needsUpdate = true
					break
				}
			}
		}
		if !found || needsUpdate {
			if !preview {
				_, err := Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
					HostedZoneId: zone.Id,
					ChangeBatch: &route53types.ChangeBatch{
						Changes: []route53types.Change{{
							Action: route53types.ChangeActionUpsert,
							ResourceRecordSet: &route53types.ResourceRecordSet{
								Name: aws.String(subDomain),
								Type: route53types.RRTypeA,
								AliasTarget: &route53types.AliasTarget{
									DNSName:              out.DomainNameConfigurations[0].ApiGatewayDomainName,
									HostedZoneId:         out.DomainNameConfigurations[0].HostedZoneId,
									EvaluateTargetHealth: false,
								},
							},
						}},
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			if needsUpdate {
				Logger.Println(PreviewString(preview)+"updated api dns:", name, subDomain)
			} else {
				Logger.Println(PreviewString(preview)+"created api dns:", name, subDomain)
			}
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDns(ctx context.Context, name, domain string, api *apitypes.Api, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiDns"}
		d.Start()
		defer d.End()
	}
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	found := false
	for _, zone := range zones {
		if domain == strings.TrimRight(*zone.Name, ".") {
			found = true
			err := lambdaEnsureTriggerApiDomainName(ctx, name, domain, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiDnsRecords(ctx, name, domain, zone, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiMapping(ctx, name, domain, api, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			break
		}
	}
	if !found {
		_, parentDomain, err := SplitOnce(domain, ".")
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		subDomain := domain
		for _, zone := range zones {
			if parentDomain == strings.TrimRight(*zone.Name, ".") {
				found = true
				err := lambdaEnsureTriggerApiDomainName(ctx, name, subDomain, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				err = lambdaEnsureTriggerApiDnsRecords(ctx, name, subDomain, zone, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				err = lambdaEnsureTriggerApiMapping(ctx, name, subDomain, api, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				break
			}
		}
	}
	if !found {
		err := fmt.Errorf("no zone found matching domain or parent domain: %s", domain)
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaEnsureTriggerApiMapping(ctx context.Context, name, subDomain string, api *apitypes.Api, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiMapping"}
		d.Start()
		defer d.End()
	}
	if api == nil && preview {
		Logger.Println(PreviewString(preview)+"created api path mapping:", name, subDomain)
		return nil
	}
	mappings, err := ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
		DomainName: aws.String(subDomain),
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return err
		}
	}
	if mappings != nil && len(mappings.Items) == 500 {
		err := fmt.Errorf("too many path mappings for domain %s", subDomain)
		Logger.Println("error:", err)
		return err
	}
	switch {
	case mappings == nil || len(mappings.Items) == 0:
		if !preview {
			_, err := ApiClient().CreateApiMapping(ctx, &apigatewayv2.CreateApiMappingInput{
				DomainName: aws.String(subDomain),
				ApiId:      api.ApiId,
				Stage:      aws.String(lambdaDollarDefault),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api path mapping:", name, subDomain)
	case len(mappings.Items) == 1:
		mapping := mappings.Items[0]
		if *mapping.ApiId != *api.ApiId {
			err := fmt.Errorf("restapi id misconfigured: %s != %s", *mapping.ApiId, *api.ApiId)
			Logger.Println("error:", err)
			return err
		}
		if *mapping.Stage != lambdaDollarDefault {
			err := fmt.Errorf("stage misconfigured: %s != %s", *mapping.Stage, lambdaDollarDefault)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("found more than 1 path mapping: %s", Pformat(mappings.Items))
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func LambdaEnsureTriggerApi(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerApi"}
		d.Start()
		defer d.End()
	}
	var permissionSids []string
	hasApi := false
	hasWebsocket := false
	domainApi := ""
	domainWebsocket := ""
	arnLambda := infraLambda.Arn
	if arnLambda == "" {
		identity, err := lambdaResolveIdentity(ctx, infraLambda.Name, infraLambda.infraSetName)
		if err != nil {
			return nil, err
		}
		arnLambda = identity.arn
	}
	count := 0
	for _, trigger := range infraLambda.Trigger {
		var protocolType apitypes.ProtocolType
		var domainName string
		var apiName string
		var timeoutMillis int32
		if trigger.Type == lambdaTriggerApi || trigger.Type == lambdaTriggerWebsocket {
			time.Sleep(time.Duration(count) * 5 * time.Second)
			count++
			if trigger.Type == lambdaTriggerApi {
				apiName = infraLambda.Name
				if hasApi {
					err := fmt.Errorf("cannot have more than one api trigger")
					Logger.Println("error:", err)
					return nil, err
				}
				hasApi = true
				protocolType = apitypes.ProtocolTypeHttp
				timeoutMillis = 30000
			} else if trigger.Type == lambdaTriggerWebsocket {
				apiName = infraLambda.Name + LambdaWebsocketSuffix
				if hasWebsocket {
					err := fmt.Errorf("cannot have more than one websocket trigger")
					Logger.Println("error:", err)
					return nil, err
				}
				hasWebsocket = true
				protocolType = apitypes.ProtocolTypeWebsocket
				timeoutMillis = 29000
			}
			api, err := lambdaEnsureTriggerApi(ctx, infraLambda.infraSetName, apiName, arnLambda, protocolType, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			if protocolType == apitypes.ProtocolTypeHttp {
				if api != nil && api.ApiId != nil {
					err := os.Setenv(lambdaEnvVarApiID, *api.ApiId)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
			} else if protocolType == apitypes.ProtocolTypeWebsocket {
				if api != nil && api.ApiId != nil {
					err := os.Setenv(lambdaEnvVarWebsocketID, *api.ApiId)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
			}
			sid, err := lambdaEnsureTriggerApiIntegrationStageRoute(ctx, apiName, arnLambda, protocolType, api, timeoutMillis, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			if api != nil {
				if err := lambdaEnsureAPITag(ctx, ApiClient(), api, arnLambda, infraLambda.infraSetName, preview); err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			permissionSids = append(permissionSids, sid)
			for _, attr := range trigger.Attr {
				k, v, err := SplitOnce(attr, "=")
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				switch k {
				case lambdaTriggerApiAttrDns: // apigateway custom domain + route53
					domainName = v
					err := lambdaEnsureTriggerApiDns(ctx, apiName, domainName, api, preview)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				case lambdaTriggerApiAttrDomain: // apigateway custom domain
					domainName = v
					err := lambdaEnsureTriggerApiDomainName(ctx, apiName, domainName, preview)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					err = lambdaEnsureTriggerApiMapping(ctx, apiName, domainName, api, preview)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				default:
					err := fmt.Errorf("unknown attr: %s", attr)
					Logger.Println("error:", err)
					return nil, err
				}
				if trigger.Type == lambdaTriggerApi {
					domainApi = domainName
				} else if trigger.Type == lambdaTriggerWebsocket {
					domainWebsocket = domainName
				}
			}
		}
	}
	count = 0
	for _, kind := range []string{lambdaTriggerApi, lambdaTriggerWebsocket} {
		var apiEnsured bool
		var apiName string
		var apiDomain string
		var protocolType apitypes.ProtocolType
		if kind == lambdaTriggerApi {
			apiName = infraLambda.Name
			apiDomain = domainApi
			apiEnsured = hasApi
			protocolType = apitypes.ProtocolTypeHttp
		} else {
			apiName = infraLambda.Name + LambdaWebsocketSuffix
			apiDomain = domainWebsocket
			apiEnsured = hasWebsocket
			protocolType = apitypes.ProtocolTypeWebsocket
		}
		api, err := Api(ctx, apiName)
		if err != nil && err.Error() != ErrApiNotFound {
			Logger.Println("error:", err)
			return nil, err
		}
		if api != nil {
			if !apiEnsured {
				integrations, err := lambdaAPIIntegrations(ctx, ApiClient(), api.ApiId)
				if err != nil {
					return nil, err
				}
				if !lambdaApiOwned(api, integrations, protocolType, arnLambda, infraLambda.infraSetName) {
					continue
				}
			}
			time.Sleep(time.Duration(count) * 5 * time.Second) // very low api limits on apigateway delete domain, sleep here is we are adding more than 1
			count++
			domains, err := ApiListDomains(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			// delete any unused domains
			for _, domain := range domains {
				if *domain.DomainName == apiDomain {
					continue
				}
				err := lambdaTriggerApiDeleteDns(ctx, apiName, api, domain, true, preview)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			// if api trigger unused, delete rest api
			if !apiEnsured {
				err := lambdaTriggerApiDeleteApi(ctx, apiName, api, preview)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
		}
	}
	return permissionSids, nil
}

func lambdaTriggerApiDeleteApi(ctx context.Context, name string, api *apitypes.Api, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaTriggerApiDeleteApi"}
		d.Start()
		defer d.End()
	}
	if !preview {
		_, err := ApiClient().DeleteApi(ctx, &apigatewayv2.DeleteApiInput{
			ApiId: api.ApiId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted api trigger for:", name)
	return nil
}

func lambdaTriggerApiDeleteDns(ctx context.Context, name string, api *apitypes.Api, domain apitypes.DomainName, deleteDomain bool, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaTriggerApiDeleteDns"}
		d.Start()
		defer d.End()
	}
	mappings, err := ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
		DomainName: domain.DomainName,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if mappings.NextToken != nil {
		err := fmt.Errorf("too many api mappings for domain %s", aws.ToString(domain.DomainName))
		Logger.Println("error:", err)
		return err
	}
	var matchingMappings []apitypes.ApiMapping
	for _, mapping := range mappings.Items {
		if aws.ToString(mapping.ApiId) == aws.ToString(api.ApiId) {
			matchingMappings = append(matchingMappings, mapping)
		}
	}
	if len(matchingMappings) == 0 {
		return nil
	}
	if !deleteDomain || len(mappings.Items) != 1 {
		for _, mapping := range matchingMappings {
			if mapping.ApiMappingId == nil {
				return fmt.Errorf("API mapping for domain %q has no ID", aws.ToString(domain.DomainName))
			}
			if !preview {
				if _, err := ApiClient().DeleteApiMapping(ctx, &apigatewayv2.DeleteApiMappingInput{
					ApiMappingId: mapping.ApiMappingId,
					DomainName:   domain.DomainName,
				}); err != nil {
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"deleted api mapping:", name, aws.ToString(domain.DomainName))
		}
		return nil
	}
	for _, mapping := range matchingMappings {
		if *mapping.ApiId == *api.ApiId {
			zones, err := Route53ListZones(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, zone := range zones {
				records, err := Route53ListRecords(ctx, *zone.Id)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				for _, record := range records {
					targetMatch := record.AliasTarget != nil &&
						record.AliasTarget.DNSName != nil &&
						domain.DomainNameConfigurations != nil &&
						len(domain.DomainNameConfigurations) > 0 &&
						strings.TrimRight(*record.AliasTarget.DNSName, ".") == *domain.DomainNameConfigurations[0].ApiGatewayDomainName
					nameMatch := record.Name != nil &&
						strings.TrimRight(*record.Name, ".") == *domain.DomainName
					if targetMatch && nameMatch {
						if !preview {
							_, err := Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
								HostedZoneId: zone.Id,
								ChangeBatch: &route53types.ChangeBatch{Changes: []route53types.Change{{
									Action:            route53types.ChangeActionDelete,
									ResourceRecordSet: &record,
								}}},
							})
							if err != nil {
								Logger.Println("error:", err)
								return err
							}
						}
						Logger.Println(PreviewString(preview)+"deleted api dns records:", name, *domain.DomainName)
					}
				}
			}
			if !preview {
				var unexpectedErr error
				err := Retry(ctx, func() error {
					_, err := ApiClient().DeleteDomainName(ctx, &apigatewayv2.DeleteDomainNameInput{
						DomainName: domain.DomainName,
					})
					if err != nil {
						if strings.Contains(err.Error(), "TooManyRequestsException") {
							Logger.Println("delete domain has low rate limits, sleeping then retrying")
							time.Sleep(15 * time.Second)
						} else {
							unexpectedErr = err
							return nil
						}
					}
					return err
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if unexpectedErr != nil {
					Logger.Println("error:", unexpectedErr)
					return unexpectedErr
				}
			}
			Logger.Println(PreviewString(preview)+"deleted api domain:", name, *domain.DomainName)
		}
	}
	return nil
}

func lambdaEventRuleName(name, suffix string) string {
	candidate := name + lambdaEventRuleNameSeparator + suffix
	if len(candidate) <= 64 {
		return candidate
	}
	hash := sha256Hex([]byte(candidate))[:16]
	maxNameLen := 64 - len(lambdaEventRuleNameSeparator) - len(hash)
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	return name + lambdaEventRuleNameSeparator + hash
}

func lambdaScheduleName(name, schedule string) string {
	return lambdaEventRuleName(name, base64.RawURLEncoding.EncodeToString([]byte(schedule)))
}

func lambdaScheduleTargetMatches(infraLambda *InfraLambda, rule eventbridgetypes.Rule, target eventbridgetypes.Target) bool {
	return rule.Name != nil && rule.ScheduleExpression != nil &&
		*rule.Name == lambdaScheduleName(infraLambda.Name, *rule.ScheduleExpression) &&
		aws.ToString(target.Id) == "1" && aws.ToString(target.Arn) == infraLambda.Arn
}

func LambdaEnsureTriggerSchedule(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerSchedule"}
		d.Start()
		defer d.End()
	}
	var permissionSids []string
	var triggers []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerSchedule {
			triggers = append(triggers, trigger.Attr[0])
		}
	}
	if len(triggers) > 0 {
		for _, schedule := range triggers {
			scheduleName := lambdaScheduleName(infraLambda.Name, schedule)
			scheduleArn, err := lambdaEventRuleARN(infraLambda.Arn, scheduleName)
			if err != nil {
				return nil, err
			}
			existingRule := false
			out, err := EventsClient().DescribeRule(ctx, &eventbridge.DescribeRuleInput{
				Name: aws.String(scheduleName),
			})
			if err != nil {
				var rnfe *eventbridgetypes.ResourceNotFoundException
				if !errors.As(err, &rnfe) {
					return nil, err
				}
				if !preview {
					out, err := EventsClient().PutRule(ctx, &eventbridge.PutRuleInput{
						Name:               aws.String(scheduleName),
						ScheduleExpression: aws.String(schedule),
						Tags: []eventbridgetypes.Tag{{
							Key:   aws.String(infraSetTagName),
							Value: aws.String(infraLambda.infraSetName),
						}},
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					if out == nil || aws.ToString(out.RuleArn) != scheduleArn {
						return nil, fmt.Errorf("EventBridge PutRule returned ARN %q for %q, want %q", aws.ToString(out.RuleArn), scheduleName, scheduleArn)
					}
				}
				Logger.Println(PreviewString(preview)+"created cloudwatch rule:", scheduleName, schedule)
			} else {
				if aws.ToString(out.ScheduleExpression) != schedule {
					err := fmt.Errorf("cloudwatch rule misconfigured: %s %s != %s", scheduleName, schedule, aws.ToString(out.ScheduleExpression))
					Logger.Println("error:", err)
					return nil, err
				}
				existingRule = true
				if aws.ToString(out.Arn) != scheduleArn {
					return nil, fmt.Errorf("EventBridge DescribeRule returned ARN %q for %q, want %q", aws.ToString(out.Arn), scheduleName, scheduleArn)
				}
			}
			var targets []eventbridgetypes.Target
			err = Retry(ctx, func() error {
				var err error
				targets, err = EventsListRuleTargets(ctx, scheduleName, nil)
				var rnfe *eventbridgetypes.ResourceNotFoundException
				if errors.As(err, &rnfe) {
					return nil
				}
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			switch len(targets) {
			case 0:
				if !preview {
					if err := lambdaPutEventTarget(ctx, EventsClient(), scheduleName, infraLambda.Arn); err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
				Logger.Println(PreviewString(preview)+"created cloudwatch rule target:", scheduleName, infraLambda.Arn)
			case 1:
				if aws.ToString(targets[0].Id) != "1" || aws.ToString(targets[0].Arn) != infraLambda.Arn {
					err := fmt.Errorf("cloudwatch rule is misconfigured with unknown target: %s %#v", infraLambda.Arn, targets[0])
					Logger.Println("error:", err)
					return nil, err
				}
			default:
				var targetArns []string
				for _, target := range targets {
					targetArns = append(targetArns, aws.ToString(target.Arn))
				}
				err := fmt.Errorf("cloudwatch rule is misconfigured with unknown targets: %s %v", infraLambda.Arn, targetArns)
				Logger.Println("error:", err)
				return nil, err
			}
			if existingRule {
				if err := lambdaEnsureEventRuleTag(ctx, EventsClient(), scheduleArn, infraLambda.infraSetName, preview); err != nil {
					return nil, err
				}
			}
			sid, err := lambdaEnsurePermission(ctx, infraLambda.Name, "events.amazonaws.com", scheduleArn, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			permissionSids = append(permissionSids, sid)
		}
	}
	identity := lambdaIdentity{
		name:         infraLambda.Name,
		arn:          infraLambda.Arn,
		infraSetName: infraLambda.infraSetName,
		exists:       true,
	}
	if err := lambdaCleanupScheduleTriggers(ctx, EventsClient(), identity, triggers, preview); err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return permissionSids, nil
}

func lambdaDynamoDBTriggerAttrShortcut(s string) string {
	s2, ok := map[string]string{
		"batch":    "BatchSize",
		"parallel": "ParallelizationFactor",
		"retry":    "MaximumRetryAttempts",
		"start":    "StartingPosition",
		"window":   "MaximumBatchingWindowInSeconds",
	}[s]
	if ok {
		return s2
	}
	return s
}

type lambdaEventSourceMappingClient interface {
	CreateEventSourceMapping(context.Context, *lambda.CreateEventSourceMappingInput, ...func(*lambda.Options)) (*lambda.CreateEventSourceMappingOutput, error)
	DeleteEventSourceMapping(context.Context, *lambda.DeleteEventSourceMappingInput, ...func(*lambda.Options)) (*lambda.DeleteEventSourceMappingOutput, error)
	GetEventSourceMapping(context.Context, *lambda.GetEventSourceMappingInput, ...func(*lambda.Options)) (*lambda.GetEventSourceMappingOutput, error)
	ListEventSourceMappings(context.Context, *lambda.ListEventSourceMappingsInput, ...func(*lambda.Options)) (*lambda.ListEventSourceMappingsOutput, error)
	UpdateEventSourceMapping(context.Context, *lambda.UpdateEventSourceMappingInput, ...func(*lambda.Options)) (*lambda.UpdateEventSourceMappingOutput, error)
}

type lambdaDynamoDBStreamARNResolver func(context.Context, string) (string, error)

func LambdaEnsureTriggerDynamoDB(ctx context.Context, infraLambda *InfraLambda, preview bool) error {
	return lambdaEnsureTriggerDynamoDB(ctx, LambdaClient(), DynamoDBStreamArn, infraLambda, preview)
}

func lambdaEnsureTriggerDynamoDB(ctx context.Context, client lambdaEventSourceMappingClient, resolveStreamARN lambdaDynamoDBStreamARNResolver, infraLambda *InfraLambda, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerDynamoDB"}
		d.Start()
		defer d.End()
	}

	desired, err := lambdaDynamoDBDesiredMappings(ctx, resolveStreamARN, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	unresolved := false
	for _, desiredMapping := range desired {
		if desiredMapping.create.EventSourceArn == nil {
			Logger.Println(PreviewString(true)+"created event source mapping:", infraLambda.Name, infraLambda.Arn, desiredMapping.tableName, strings.Join(desiredMapping.triggerAttrs, " "))
			unresolved = true
		}
	}
	if unresolved {
		return nil
	}
	functionName := infraLambda.Name
	if infraLambda.Arn != "" {
		functionName = infraLambda.Arn
	}
	listed, err := lambdaListEventSourceMappingsWithClient(ctx, client, functionName)
	if err != nil && !(preview && lambdaEventSourceMappingNotFound(err)) {
		Logger.Println("error:", err)
		return err
	}
	mappings := make([]*lambdaDynamoDBCurrentMapping, 0, len(listed))
	for _, listedMapping := range listed {
		mapping, err := lambdaDynamoDBCurrentMappingFromConfiguration(listedMapping)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		mappings = append(mappings, mapping)
	}

	configuredStreamARNs := make([]string, 0, len(desired))
	for _, desiredMapping := range desired {
		configuredStreamARNs = append(configuredStreamARNs, aws.ToString(desiredMapping.create.EventSourceArn))
		var matches []*lambdaDynamoDBCurrentMapping
		for _, mapping := range mappings {
			if aws.ToString(mapping.eventSourceARN) == aws.ToString(desiredMapping.create.EventSourceArn) {
				matches = append(matches, mapping)
			}
		}
		if len(matches) > 1 {
			return fmt.Errorf("found more than 1 event source mapping for %s %s", infraLambda.Name, desiredMapping.tableName)
		}
		if len(matches) == 0 {
			if err := lambdaCreateDynamoDBMapping(ctx, client, infraLambda, desiredMapping, preview); err != nil {
				return err
			}
			continue
		}
		if err := lambdaConvergeDynamoDBMapping(ctx, client, infraLambda, desiredMapping, matches[0], preview); err != nil {
			return err
		}
	}

	for _, mapping := range mappings {
		streamARN := aws.ToString(mapping.eventSourceARN)
		if !lambdaDynamoDBStreamARN(streamARN) || slices.Contains(configuredStreamARNs, streamARN) {
			continue
		}
		tableName, err := lambdaDynamoDBStreamTableName(streamARN)
		if err != nil {
			return err
		}
		if preview {
			Logger.Println(PreviewString(true)+"deleted trigger:", infraLambda.Name, tableName)
			continue
		}
		settled, err := lambdaWaitEventSourceMappingSettled(ctx, client, aws.ToString(mapping.uuid))
		if err != nil {
			return err
		}
		if settled == nil {
			continue
		}
		_, err = client.DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{UUID: settled.uuid})
		if err != nil && !lambdaEventSourceMappingNotFound(err) {
			Logger.Println("error:", err)
			return err
		}
		if err := lambdaWaitEventSourceMappingDeleted(ctx, client, aws.ToString(settled.uuid)); err != nil {
			return err
		}
		Logger.Println("deleted trigger:", infraLambda.Name, tableName)
	}
	return nil
}

type lambdaDynamoDBDesiredMapping struct {
	tableName    string
	triggerAttrs []string
	create       *lambda.CreateEventSourceMappingInput
}

type lambdaDynamoDBCurrentMapping struct {
	uuid                           *string
	eventSourceARN                 *string
	state                          *string
	stateTransitionReason          *string
	batchSize                      *int32
	maximumBatchingWindowInSeconds *int32
	maximumRetryAttempts           *int32
	parallelizationFactor          *int32
	startingPosition               lambdatypes.EventSourcePosition
}

func lambdaDynamoDBDesiredMappings(ctx context.Context, resolveStreamARN lambdaDynamoDBStreamARNResolver, infraLambda *InfraLambda, preview bool) ([]*lambdaDynamoDBDesiredMapping, error) {
	var desired []*lambdaDynamoDBDesiredMapping
	seenTables := map[string]struct{}{}
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type != lambdaTriggerDynamoDB {
			continue
		}
		if len(trigger.Attr) == 0 || trigger.Attr[0] == "" {
			return nil, fmt.Errorf("lambda DynamoDB trigger requires a table name")
		}
		tableName := trigger.Attr[0]
		if _, duplicate := seenTables[tableName]; duplicate {
			return nil, fmt.Errorf("duplicate lambda DynamoDB trigger for table: %s", tableName)
		}
		seenTables[tableName] = struct{}{}
		triggerAttrs := trigger.Attr[1:]
		create := &lambda.CreateEventSourceMappingInput{
			FunctionName:                   aws.String(infraLambda.Name),
			Enabled:                        aws.Bool(true),
			BatchSize:                      aws.Int32(100),
			MaximumBatchingWindowInSeconds: aws.Int32(0),
			MaximumRetryAttempts:           aws.Int32(-1),
			ParallelizationFactor:          aws.Int32(1),
		}
		for _, line := range triggerAttrs {
			attr, value, err := SplitOnce(line, "=")
			if err != nil {
				return nil, err
			}
			attr = lambdaDynamoDBTriggerAttrShortcut(attr)
			switch attr {
			case "BatchSize":
				size, err := strconv.Atoi(value)
				if err != nil {
					return nil, err
				}
				create.BatchSize = aws.Int32(int32(size))
			case "MaximumBatchingWindowInSeconds":
				size, err := strconv.Atoi(value)
				if err != nil {
					return nil, err
				}
				create.MaximumBatchingWindowInSeconds = aws.Int32(int32(size))
			case "MaximumRetryAttempts":
				attempts, err := strconv.Atoi(value)
				if err != nil {
					return nil, err
				}
				create.MaximumRetryAttempts = aws.Int32(int32(attempts))
			case "ParallelizationFactor":
				factor, err := strconv.Atoi(value)
				if err != nil {
					return nil, err
				}
				create.ParallelizationFactor = aws.Int32(int32(factor))
			case "StartingPosition":
				create.StartingPosition = lambdatypes.EventSourcePosition(strings.ToUpper(value))
			default:
				return nil, fmt.Errorf("unknown lambda dynamodb trigger attribute: %s", line)
			}
		}
		if create.StartingPosition == "" {
			return nil, fmt.Errorf("lambda DynamoDB trigger for %s requires start=latest or start=trim_horizon", tableName)
		}
		if create.StartingPosition != lambdatypes.EventSourcePositionLatest && create.StartingPosition != lambdatypes.EventSourcePositionTrimHorizon {
			return nil, fmt.Errorf("lambda DynamoDB trigger for %s has invalid starting position: %s", tableName, create.StartingPosition)
		}
		streamARN, err := resolveStreamARN(ctx, tableName)
		if err != nil {
			if preview && lambdaDynamoDBResourceNotFound(err) {
				desired = append(desired, &lambdaDynamoDBDesiredMapping{tableName: tableName, triggerAttrs: triggerAttrs, create: create})
				continue
			}
			return nil, fmt.Errorf("resolve DynamoDB stream for %s: %w", tableName, err)
		}
		if streamARN == "" {
			return nil, fmt.Errorf("resolve DynamoDB stream for %s: empty stream ARN", tableName)
		}
		create.EventSourceArn = aws.String(streamARN)
		desired = append(desired, &lambdaDynamoDBDesiredMapping{tableName: tableName, triggerAttrs: triggerAttrs, create: create})
	}
	return desired, nil
}

func lambdaDynamoDBCurrentMappingFromConfiguration(mapping lambdatypes.EventSourceMappingConfiguration) (*lambdaDynamoDBCurrentMapping, error) {
	return lambdaDynamoDBCurrentMappingFromValues(
		mapping.UUID,
		mapping.EventSourceArn,
		mapping.State,
		mapping.StateTransitionReason,
		mapping.BatchSize,
		mapping.MaximumBatchingWindowInSeconds,
		mapping.MaximumRetryAttempts,
		mapping.ParallelizationFactor,
		mapping.StartingPosition,
	)
}

func lambdaDynamoDBCurrentMappingFromGet(mapping *lambda.GetEventSourceMappingOutput) (*lambdaDynamoDBCurrentMapping, error) {
	if mapping == nil {
		return nil, fmt.Errorf("lambda returned an empty event source mapping")
	}
	return lambdaDynamoDBCurrentMappingFromValues(
		mapping.UUID,
		mapping.EventSourceArn,
		mapping.State,
		mapping.StateTransitionReason,
		mapping.BatchSize,
		mapping.MaximumBatchingWindowInSeconds,
		mapping.MaximumRetryAttempts,
		mapping.ParallelizationFactor,
		mapping.StartingPosition,
	)
}

func lambdaDynamoDBCurrentMappingFromValues(uuid, eventSourceARN, state, stateTransitionReason *string, batchSize, maximumBatchingWindowInSeconds, maximumRetryAttempts, parallelizationFactor *int32, startingPosition lambdatypes.EventSourcePosition) (*lambdaDynamoDBCurrentMapping, error) {
	if uuid == nil || *uuid == "" {
		return nil, fmt.Errorf("lambda event source mapping lacks a UUID")
	}
	if eventSourceARN == nil || *eventSourceARN == "" {
		return nil, fmt.Errorf("lambda event source mapping %s lacks an event source ARN", *uuid)
	}
	if state == nil || *state == "" {
		return nil, fmt.Errorf("lambda event source mapping %s lacks a state", *uuid)
	}
	return &lambdaDynamoDBCurrentMapping{
		uuid:                           uuid,
		eventSourceARN:                 eventSourceARN,
		state:                          state,
		stateTransitionReason:          stateTransitionReason,
		batchSize:                      batchSize,
		maximumBatchingWindowInSeconds: maximumBatchingWindowInSeconds,
		maximumRetryAttempts:           maximumRetryAttempts,
		parallelizationFactor:          parallelizationFactor,
		startingPosition:               startingPosition,
	}, nil
}

func lambdaCreateDynamoDBMapping(ctx context.Context, client lambdaEventSourceMappingClient, infraLambda *InfraLambda, desired *lambdaDynamoDBDesiredMapping, preview bool) error {
	if preview {
		Logger.Println(PreviewString(true)+"created event source mapping:", infraLambda.Name, infraLambda.Arn, aws.ToString(desired.create.EventSourceArn), strings.Join(desired.triggerAttrs, " "))
		return nil
	}
	var output *lambda.CreateEventSourceMappingOutput
	err := Retry(ctx, func() error {
		var err error
		output, err = client.CreateEventSourceMapping(ctx, desired.create)
		return err
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if output == nil || output.UUID == nil || *output.UUID == "" {
		return fmt.Errorf("created Lambda event source mapping lacks a UUID")
	}
	if _, err := lambdaWaitEventSourceMappingEnabled(ctx, client, *output.UUID); err != nil {
		return err
	}
	Logger.Println("created event source mapping:", infraLambda.Name, infraLambda.Arn, aws.ToString(desired.create.EventSourceArn), strings.Join(desired.triggerAttrs, " "))
	return nil
}

func lambdaConvergeDynamoDBMapping(ctx context.Context, client lambdaEventSourceMappingClient, infraLambda *InfraLambda, desired *lambdaDynamoDBDesiredMapping, listed *lambdaDynamoDBCurrentMapping, preview bool) error {
	current := listed
	if !preview {
		var err error
		current, err = lambdaWaitEventSourceMappingSettled(ctx, client, aws.ToString(listed.uuid))
		if err != nil {
			return err
		}
		if current == nil {
			return lambdaCreateDynamoDBMapping(ctx, client, infraLambda, desired, false)
		}
	}
	update, needsUpdate, err := lambdaDynamoDBMappingUpdate(infraLambda, desired, current, preview)
	if err != nil {
		return err
	}
	if !needsUpdate {
		return nil
	}
	if !preview {
		if _, err := client.UpdateEventSourceMapping(ctx, update); err != nil {
			Logger.Println("error:", err)
			return err
		}
		if _, err := lambdaWaitEventSourceMappingEnabled(ctx, client, aws.ToString(current.uuid)); err != nil {
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated event source mapping for", infraLambda.Name, desired.tableName)
	return nil
}

func lambdaDynamoDBMappingUpdate(infraLambda *InfraLambda, desired *lambdaDynamoDBDesiredMapping, current *lambdaDynamoDBCurrentMapping, preview bool) (*lambda.UpdateEventSourceMappingInput, bool, error) {
	update := &lambda.UpdateEventSourceMappingInput{UUID: current.uuid, FunctionName: desired.create.FunctionName}
	needsUpdate := false
	if !int32PointersEqual(current.batchSize, desired.create.BatchSize) {
		Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d\n", infraLambda.Name, desired.tableName, aws.ToInt32(current.batchSize), aws.ToInt32(desired.create.BatchSize))
		update.BatchSize = desired.create.BatchSize
		needsUpdate = true
	}
	if !int32PointersEqual(current.maximumRetryAttempts, desired.create.MaximumRetryAttempts) {
		Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumRetryAttempts for %s %s: %d => %d\n", infraLambda.Name, desired.tableName, aws.ToInt32(current.maximumRetryAttempts), aws.ToInt32(desired.create.MaximumRetryAttempts))
		update.MaximumRetryAttempts = desired.create.MaximumRetryAttempts
		needsUpdate = true
	}
	if !int32PointersEqual(current.parallelizationFactor, desired.create.ParallelizationFactor) {
		Logger.Printf(PreviewString(preview)+"will update lambda event source mapping ParallelizationFactor for %s %s: %d => %d\n", infraLambda.Name, desired.tableName, aws.ToInt32(current.parallelizationFactor), aws.ToInt32(desired.create.ParallelizationFactor))
		update.ParallelizationFactor = desired.create.ParallelizationFactor
		needsUpdate = true
	}
	if !int32PointersEqual(current.maximumBatchingWindowInSeconds, desired.create.MaximumBatchingWindowInSeconds) {
		Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d\n", infraLambda.Name, desired.tableName, aws.ToInt32(current.maximumBatchingWindowInSeconds), aws.ToInt32(desired.create.MaximumBatchingWindowInSeconds))
		update.MaximumBatchingWindowInSeconds = desired.create.MaximumBatchingWindowInSeconds
		needsUpdate = true
	}
	if current.startingPosition != desired.create.StartingPosition {
		return nil, false, fmt.Errorf("cannot update StartingPosition for %s %s: %s => %s", infraLambda.Name, desired.tableName, current.startingPosition, desired.create.StartingPosition)
	}
	if aws.ToString(current.state) != "Enabled" {
		Logger.Printf(PreviewString(preview)+"will enable lambda event source mapping for %s %s: %s => Enabled\n", infraLambda.Name, desired.tableName, aws.ToString(current.state))
		update.Enabled = aws.Bool(true)
		needsUpdate = true
	}
	return update, needsUpdate, nil
}

func int32PointersEqual(a, b *int32) bool {
	return a != nil && b != nil && *a == *b
}

func lambdaWaitEventSourceMappingSettled(ctx context.Context, client lambdaEventSourceMappingClient, uuid string) (*lambdaDynamoDBCurrentMapping, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		output, err := client.GetEventSourceMapping(waitCtx, &lambda.GetEventSourceMappingInput{UUID: aws.String(uuid)})
		if lambdaEventSourceMappingNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		mapping, err := lambdaDynamoDBCurrentMappingFromGet(output)
		if err != nil {
			return nil, err
		}
		switch aws.ToString(mapping.state) {
		case "Enabled", "Disabled":
			return mapping, nil
		case "Creating", "Enabling", "Disabling", "Updating", "Deleting":
		default:
			return nil, fmt.Errorf("lambda event source mapping %s has unknown state %q", uuid, aws.ToString(mapping.state))
		}
		if err := lambdaEventSourceMappingPoll(waitCtx); err != nil {
			return nil, fmt.Errorf("wait for Lambda event source mapping %s to settle in state %s (%s): %w", uuid, aws.ToString(mapping.state), aws.ToString(mapping.stateTransitionReason), err)
		}
	}
}

func lambdaWaitEventSourceMappingEnabled(ctx context.Context, client lambdaEventSourceMappingClient, uuid string) (*lambdaDynamoDBCurrentMapping, error) {
	mapping, err := lambdaWaitEventSourceMappingSettled(ctx, client, uuid)
	if err != nil {
		return nil, err
	}
	if mapping == nil {
		return nil, fmt.Errorf("lambda event source mapping %s disappeared before reaching Enabled", uuid)
	}
	if aws.ToString(mapping.state) != "Enabled" {
		return nil, fmt.Errorf("lambda event source mapping %s reached %s instead of Enabled: %s", uuid, aws.ToString(mapping.state), aws.ToString(mapping.stateTransitionReason))
	}
	return mapping, nil
}

func lambdaWaitEventSourceMappingDeleted(ctx context.Context, client lambdaEventSourceMappingClient, uuid string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		output, err := client.GetEventSourceMapping(waitCtx, &lambda.GetEventSourceMappingInput{UUID: aws.String(uuid)})
		if lambdaEventSourceMappingNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		mapping, err := lambdaDynamoDBCurrentMappingFromGet(output)
		if err != nil {
			return err
		}
		if err := lambdaEventSourceMappingPoll(waitCtx); err != nil {
			return fmt.Errorf("wait for Lambda event source mapping %s deletion in state %s (%s): %w", uuid, aws.ToString(mapping.state), aws.ToString(mapping.stateTransitionReason), err)
		}
	}
}

func lambdaEventSourceMappingPoll(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func lambdaEventSourceMappingNotFound(err error) bool {
	var notFound *lambdatypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func lambdaDynamoDBResourceNotFound(err error) bool {
	var notFound *ddbtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func lambdaDynamoDBStreamARN(arn string) bool {
	parts := strings.SplitN(arn, ":", 6)
	return len(parts) == 6 && parts[0] == "arn" && parts[2] == lambdaTriggerDynamoDB
}

func lambdaDynamoDBStreamTableName(arn string) (string, error) {
	if !lambdaDynamoDBStreamARN(arn) {
		return "", fmt.Errorf("invalid DynamoDB stream ARN: %s", arn)
	}
	resource := strings.Split(strings.SplitN(arn, ":", 6)[5], "/")
	if len(resource) < 4 || resource[0] != "table" || resource[1] == "" || resource[2] != "stream" || resource[3] == "" {
		return "", fmt.Errorf("invalid DynamoDB stream ARN: %s", arn)
	}
	return resource[1], nil
}

func lambdaListEventSourceMappings(ctx context.Context, name string) ([]lambdatypes.EventSourceMappingConfiguration, error) {
	return lambdaListEventSourceMappingsWithClient(ctx, LambdaClient(), name)
}

func lambdaListEventSourceMappingsWithClient(ctx context.Context, client lambdaEventSourceMappingClient, name string) ([]lambdatypes.EventSourceMappingConfiguration, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaListEventSourceMappings"}
		d.Start()
		defer d.End()
	}
	var marker *string
	var eventSourceMappings []lambdatypes.EventSourceMappingConfiguration
	for {
		out, err := client.ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(name),
			Marker:       marker,
		})
		if err != nil {
			return nil, err
		}
		eventSourceMappings = append(eventSourceMappings, out.EventSourceMappings...)
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return eventSourceMappings, nil
}

func lambdaSQSTriggerAttrShortcut(s string) string {
	s2, ok := map[string]string{
		"batch":  "BatchSize",
		"window": "MaximumBatchingWindowInSeconds",
	}[s]
	if ok {
		return s2
	}
	return s
}

func LambdaEnsureTriggerSQS(ctx context.Context, infraLambda *InfraLambda, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerSQS"}
		d.Start()
		defer d.End()
	}
	var triggers [][]string
	var queueNames []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerSQS {
			triggers = append(triggers, trigger.Attr)
			queueNames = append(queueNames, trigger.Attr[0])
		}
	}
	if len(triggers) > 0 {
		for _, triggerAttrs := range triggers {
			queueName := triggerAttrs[0]
			triggerAttrs := triggerAttrs[1:]
			sqsArn, err := SQSArn(ctx, queueName)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			input := &lambda.CreateEventSourceMappingInput{
				FunctionName:                   aws.String(infraLambda.Name),
				EventSourceArn:                 aws.String(sqsArn),
				Enabled:                        aws.Bool(true),
				BatchSize:                      aws.Int32(10),
				MaximumBatchingWindowInSeconds: aws.Int32(0),
			}
			for _, line := range triggerAttrs {
				attr, value, err := SplitOnce(line, "=")
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				attr = lambdaSQSTriggerAttrShortcut(attr)
				switch attr {
				case "BatchSize":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.BatchSize = aws.Int32(int32(size))
				case "MaximumBatchingWindowInSeconds":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.MaximumBatchingWindowInSeconds = aws.Int32(int32(size))
				default:
					err := fmt.Errorf("unknown sqs trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			eventSourceMappings, err := lambdaListEventSourceMappings(ctx, infraLambda.Name)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			count := 0
			var found *lambdatypes.EventSourceMappingConfiguration
			for _, mapping := range eventSourceMappings {
				if mapping.EventSourceArn != nil && *mapping.EventSourceArn == sqsArn && mapping.FunctionArn != nil && *mapping.FunctionArn == infraLambda.Arn {
					found = &mapping
					count++
				}
			}
			switch count {
			case 0:
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().CreateEventSourceMapping(ctx, input)
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"created event source mapping:", infraLambda.Name, infraLambda.Arn, sqsArn, strings.Join(triggerAttrs, " "))
			case 1:
				needsUpdate := false
				update := &lambda.UpdateEventSourceMappingInput{UUID: found.UUID}
				update.FunctionName = input.FunctionName
				if *found.BatchSize != *input.BatchSize {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d\n", infraLambda.Name, queueName, *found.BatchSize, *input.BatchSize)
					update.BatchSize = input.BatchSize
					needsUpdate = true
				}
				if *found.MaximumBatchingWindowInSeconds != *input.MaximumBatchingWindowInSeconds {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d\n", infraLambda.Name, queueName, *found.MaximumBatchingWindowInSeconds, *input.MaximumBatchingWindowInSeconds)
					update.MaximumBatchingWindowInSeconds = input.MaximumBatchingWindowInSeconds
					needsUpdate = true
				}
				if needsUpdate {
					if !preview {
						_, err := LambdaClient().UpdateEventSourceMapping(ctx, update)
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
					}
					Logger.Println(PreviewString(preview)+"updated event source mapping for", infraLambda.Name, queueName)
				}
			default:
				err := fmt.Errorf("found more than 1 event source mapping for %s %s", infraLambda.Name, queueName)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(infraLambda.Arn),
			Marker:       marker,
		})
		if err != nil {
			if !preview {
				Logger.Println("error:", err)
				return err
			}
			out = &lambda.ListEventSourceMappingsOutput{}
		}
		for _, mapping := range out.EventSourceMappings {
			infra := ArnToInfraName(*mapping.EventSourceArn)
			if infra != lambdaTriggerSQS {
				continue
			}
			queueName := SQSArnToName(*mapping.EventSourceArn)
			if !slices.Contains(queueNames, queueName) {
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{
							UUID: mapping.UUID,
						})
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted trigger:", infraLambda.Name, queueName)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return nil
}

func lambdaUpdateZipGo(infraLambda *InfraLambda) error {
	return lambdaCreateZipGo(infraLambda)
}

func lambdaCreateZipGo(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaCreateZipGo"}
		d.Start()
		defer d.End()
	}
	entrypointInfo, err := os.Stat(infraLambda.Entrypoint)
	if err != nil {
		return fmt.Errorf("lambda Go entrypoint %q: %w", infraLambda.Entrypoint, err)
	}
	if !entrypointInfo.Mode().IsRegular() {
		return fmt.Errorf("lambda Go entrypoint is not a regular file: %s", infraLambda.Entrypoint)
	}
	dir, err := resetLambdaPackageDir(infraLambda.Name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	prefix := ""
	ldflags := os.Getenv("LDFLAGS")
	if ldflags != " " {
		prefix = " " // ldflags might contain secrets, shellAt() logs cmdString on error unless it starts with whitespace
	}
	err = shellAt(path.Dir(infraLambda.Entrypoint), "%sCGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags='-s -w %s' -tags 'netgo osusergo purego' -o %s .",
		prefix,
		ldflags,
		path.Join(dir, "bootstrap"),
	)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(dir, "zip -0 %s ./bootstrap", zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func removeLambdaPythonBuildArtifacts(root string) error {
	return filepath.Walk(root, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "__pycache__" {
			if err := os.RemoveAll(filePath); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		if !info.IsDir() && (strings.HasSuffix(info.Name(), ".pyc") || strings.HasSuffix(info.Name(), ".pyo") ||
			strings.HasSuffix(info.Name(), ".virtualenv")) {
			return os.Remove(filePath)
		}
		return nil
	})
}

func lambdaCreateZipPy(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaCreateZipPy"}
		d.Start()
		defer d.End()
	}
	dir, err := resetLambdaPackageDir(infraLambda.Name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	err = shell("virtualenv --python python3 %s/env", dir)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(infraLambda.Require) > 0 {
		var args []string
		for _, require := range infraLambda.Require {
			args = append(args, fmt.Sprintf(`"%s"`, require))
		}
		arg := strings.Join(args, " ")
		err = shell("%s/env/bin/pip install --no-compile %s", dir, arg)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	site_packages, err := filepath.Glob(fmt.Sprintf("%s/env/lib/python3*/site-packages", dir))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(site_packages) != 1 {
		err := fmt.Errorf("expected 1 site-package dir: %v", site_packages)
		Logger.Println("error:", err)
		return err
	}
	site_package := site_packages[0]
	err = shellAt(site_package, "cp %s .", infraLambda.Entrypoint)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(site_package, "rm -rf wheel pip setuptools pkg_resources easy_install.py")
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(site_package, "ls | grep -E 'info$' | grep -v ' ' | xargs rm -rf")
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if err := removeLambdaPythonBuildArtifacts(site_package); err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(site_package, "zip -0 -r %s .", zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func LambdaZipBytes(infraLambda *InfraLambda) ([]byte, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaZipBytes"}
		d.Start()
		defer d.End()
	}
	if err := ensureLambdaPackageRoot(); err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	data, err := os.ReadFile(zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return data, nil
}

func LambdaIncludeInZip(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaIncludeInZip"}
		d.Start()
		defer d.End()
	}
	if err := ensureLambdaPackageRoot(); err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	dir := infraLambda.dir
	var includes []string
	for _, include := range infraLambda.Include {
		if !strings.Contains(include, "*") {
			includes = append(includes, include)
		} else {
			paths, err := filepath.Glob(path.Join(dir, include))
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, pth := range paths {
				pth, err := filepath.Rel(dir, pth)
				if err != nil {
					panic(err)
				}
				includes = append(includes, pth)
			}
		}
	}
	for _, include := range includes {
		_, errLink := os.Readlink(include)
		if !Exists(include) && errLink != nil {
			err := fmt.Errorf("no such path for include: %s", include)
			Logger.Println("error:", err)
			return err
		}
		args := ""
		if strings.HasPrefix(include, "/") {
			args = "--junk-paths"
		}
		err := shellAt(dir, "zip -0 %s --symlinks -r %s '%s'", args, zipFile, include)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	if err := normalizeLambdaPackage(zipFile); err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaUpdateZipPy(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaUpdateZipPy"}
		d.Start()
		defer d.End()
	}
	if err := ensureLambdaPackageRoot(); err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	dir := path.Dir(zipFile)
	site_packages, err := filepath.Glob(fmt.Sprintf("%s/env/lib/python3*/site-packages", dir))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(site_packages) != 1 {
		err := fmt.Errorf("expected 1 site-package dir: %v", site_packages)
		Logger.Println("error:", err)
		return err
	}
	site_package := site_packages[0]
	err = shellAt(site_package, "cp %s .", infraLambda.Entrypoint)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(site_package, "zip -0 %s %s", zipFile, path.Base(infraLambda.Entrypoint))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	Logger.Println("updated zip:", zipFile, infraLambda.Entrypoint)
	return nil
}

func LambdaListFunctions(ctx context.Context) ([]lambdatypes.FunctionConfiguration, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaListFunctions"}
		d.Start()
		defer d.End()
	}
	var marker *string
	var functions []lambdatypes.FunctionConfiguration
	for {
		out, err := LambdaClient().ListFunctions(ctx, &lambda.ListFunctionsInput{
			Marker: marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		functions = append(functions, out.Functions...)
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return functions, nil
}

type LambdaUpdateZipFn func(infraLambda *InfraLambda) error

type LambdaCreateZipFn func(infraLambda *InfraLambda) error

func applyLambdaUpdateStages(updateConfiguration func() error, updateCode func() error) error {
	if err := updateConfiguration(); err != nil {
		return err
	}
	return updateCode()
}

type lambdaConfigurationUpdateClient interface {
	lambda.GetFunctionConfigurationAPIClient
	lambda.GetFunctionAPIClient
	UpdateFunctionConfiguration(
		context.Context,
		*lambda.UpdateFunctionConfigurationInput,
		...func(*lambda.Options),
	) (*lambda.UpdateFunctionConfigurationOutput, error)
}

func lambdaEnsureFunctionConfigurationWithClient(
	ctx context.Context,
	client lambdaConfigurationUpdateClient,
	infraLambda *InfraLambda,
	environmentVariables map[string]string,
	timeout int,
	memory int,
	preview bool,
	showEnvVarValues bool,
	maxWait time.Duration,
) error {
	if maxWait <= 0 {
		return errors.New("lambda configuration update wait must be positive")
	}
	outConf, err := client.GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{
		FunctionName: aws.String(infraLambda.Name),
	})
	if err != nil {
		if !preview {
			Logger.Println("error:", err)
			return err
		}
		outConf = &lambda.GetFunctionConfigurationOutput{}
	}
	if outConf.Environment == nil {
		outConf.Environment = &lambdatypes.EnvironmentResponse{Variables: map[string]string{}}
	}
	needsUpdate := false
	logPrefix := PreviewString(preview) + "updated env var for: " + infraLambda.Name + ","
	different, err := diffMapStringStringExact(
		environmentVariables,
		outConf.Environment.Variables,
		logPrefix,
		showEnvVarValues,
	)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if different {
		needsUpdate = true
	}
	if outConf.Timeout == nil {
		outConf.Timeout = aws.Int32(0)
	}
	if *outConf.Timeout != int32(timeout) {
		needsUpdate = true
		Logger.Printf(PreviewString(preview)+"update timeout: %d => %d\n", *outConf.Timeout, timeout)
	}
	if outConf.MemorySize == nil {
		outConf.MemorySize = aws.Int32(0)
	}
	if *outConf.MemorySize != int32(memory) {
		needsUpdate = true
		Logger.Printf(PreviewString(preview)+"update memory: %d => %d\n", *outConf.MemorySize, memory)
	}
	if !needsUpdate {
		return nil
	}
	if !preview {
		err := Retry(ctx, func() error {
			_, err := client.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
				FunctionName: aws.String(infraLambda.Name),
				Timeout:      aws.Int32(int32(timeout)),
				MemorySize:   aws.Int32(int32(memory)),
				Environment:  &lambdatypes.Environment{Variables: environmentVariables},
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = lambda.NewFunctionUpdatedV2Waiter(client).WaitForOutput(
			ctx,
			&lambda.GetFunctionInput{FunctionName: aws.String(infraLambda.Name)},
			maxWait,
		)
		if err != nil {
			err = fmt.Errorf("wait for lambda configuration update %s: %w", infraLambda.Name, err)
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated function configuration:", infraLambda.Name)
	return nil
}

func lambdaEnsureFunctionConfiguration(
	ctx context.Context,
	infraLambda *InfraLambda,
	environmentVariables map[string]string,
	timeout int,
	memory int,
	preview bool,
	showEnvVarValues bool,
) error {
	return lambdaEnsureFunctionConfigurationWithClient(
		ctx,
		LambdaClient(),
		infraLambda,
		environmentVariables,
		timeout,
		memory,
		preview,
		showEnvVarValues,
		5*time.Minute,
	)
}

func lambdaPrepareQuickPackage(infraLambda *InfraLambda, updateZipFn LambdaUpdateZipFn, createZipFn LambdaCreateZipFn) error {
	if err := ensureLambdaPackageRoot(); err != nil {
		return err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	var err error
	if infraLambda.runtime == lambdaRuntimePython && !Exists(zipFile) {
		err = createZipFn(infraLambda)
	} else {
		err = updateZipFn(infraLambda)
	}
	if err != nil {
		return err
	}
	return LambdaIncludeInZip(infraLambda)
}

func lambdaEnsure(ctx context.Context, infraLambda *InfraLambda, quick, preview, showEnvVarValues bool, updateZipFn LambdaUpdateZipFn, createZipFn LambdaCreateZipFn) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsure"}
		d.Start()
		defer d.End()
	}
	if err := validateLambdaName(infraLambda.Name); err != nil {
		Logger.Println("error:", err)
		return err
	}
	var err error
	var concurrency *int32
	memory := lambdaAttrMemoryDefault
	timeout := lambdaAttrTimeoutDefault
	logsTTLDays := lambdaAttrLogsTTLDaysDefault
	for _, attr := range infraLambda.Attr {
		k, v, err := SplitOnce(attr, "=")
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		switch k {
		case lambdaAttrConcurrency:
			concurrency, err = parseLambdaConcurrency(v)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		case lambdaAttrMemory:
			memory = Atoi(v)
		case lambdaAttrTimeout:
			timeout = Atoi(v)
		case lambdaAttrLogsTTLDays:
			logsTTLDays = Atoi(v)
		default:
			err := fmt.Errorf("unknown attr: %s", k)
			Logger.Println("error:", err)
			return err
		}
	}
	environmentVariables, err := lambdaEnvironmentVariables(infraLambda.Env)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if quick {
		err = lambdaPrepareQuickPackage(infraLambda, updateZipFn, createZipFn)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = applyLambdaUpdateStages(
			func() error {
				return lambdaEnsureFunctionConfiguration(
					ctx, infraLambda, environmentVariables, timeout, memory, preview, showEnvVarValues,
				)
			},
			func() error { return LambdaUpdateFunctionCode(ctx, infraLambda, preview) },
		)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	err = LogsEnsureGroup(ctx, infraLambda.infraSetName, "/aws/lambda/"+infraLambda.Name, logsTTLDays, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRole(ctx, infraLambda.infraSetName, infraLambda.Name, "lambda", preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRolePolicies(ctx, infraLambda.Name, infraLambda.Policy, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	arnRole, err := IamRoleArn(ctx, "lambda", infraLambda.Name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var zipBytes []byte
	if infraLambda.runtime != lambdaRuntimeContainer {
		err = createZipFn(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaIncludeInZip(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		zipBytes, err = LambdaZipBytes(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	var getFunctionOut *lambda.GetFunctionOutput
	var expectedErr error
	err = Retry(ctx, func() error {
		out, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
			FunctionName: aws.String(infraLambda.Name),
		})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				expectedErr = err
				return nil
			}
			return err
		}
		getFunctionOut = out
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if getFunctionOut == nil {
		getFunctionOut = &lambda.GetFunctionOutput{}
	}
	createInput := &lambda.CreateFunctionInput{
		FunctionName: aws.String(infraLambda.Name),
		Timeout:      aws.Int32(int32(timeout)),
		MemorySize:   aws.Int32(int32(memory)),
		Role:         aws.String(arnRole),
		Code:         &lambdatypes.FunctionCode{},
		Environment:  &lambdatypes.Environment{Variables: environmentVariables},
		Tags:         map[string]string{infraSetTagName: infraLambda.infraSetName},
	}
	if infraLambda.runtime == lambdaRuntimeContainer {
		createInput.Code.ImageUri = aws.String(infraLambda.Entrypoint)
		createInput.PackageType = lambdatypes.PackageTypeImage
	} else {
		createInput.Code.ZipFile = zipBytes
		createInput.PackageType = lambdatypes.PackageTypeZip
		createInput.Runtime = lambdatypes.Runtime(infraLambda.runtime)
		createInput.Handler = aws.String(infraLambda.handler)
	}
	if expectedErr != nil { // create lambda
		if infraLambda.runtime == lambdaRuntimeContainer {
			existing := map[string]string{}
			new := map[string]string{"entrypoint": *createInput.Code.ImageUri}
			_, err := diffMapStringString(new, existing, PreviewString(preview)+"container", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else {
			existing := map[string]string{}
			new, err := zipSha256Hex(zipBytes)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = diffMapStringString(new, existing, PreviewString(preview)+"zip", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		if !preview {
			getFunctionOut, err = createLambdaFunction(ctx, LambdaClient(), createInput, 5*time.Minute)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		logPrefix := PreviewString(preview) + "updated env var for: " + infraLambda.Name + ","
		_, err := diffMapStringStringExact(createInput.Environment.Variables, map[string]string{}, logPrefix, showEnvVarValues)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Printf(PreviewString(preview)+"update timeout: %d => %d\n", 0, timeout)
		Logger.Printf(PreviewString(preview)+"update memory: %d => %d\n", 0, memory)
		Logger.Println(PreviewString(preview) + "created function: " + infraLambda.Name)
	} else { // update lambda
		var diff bool
		if infraLambda.runtime == lambdaRuntimeContainer {
			existing := map[string]string{"entrypoint": *getFunctionOut.Code.ImageUri}
			new := map[string]string{"entrypoint": *createInput.Code.ImageUri}
			diff, err = diffMapStringString(new, existing, PreviewString(preview)+"entrypoint", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else {
			if getFunctionOut.Configuration == nil || getFunctionOut.Configuration.CodeSha256 == nil {
				err := fmt.Errorf("lambda function returned no code hash: %s", infraLambda.Name)
				Logger.Println("error:", err)
				return err
			}
			newCodeHash, _, err := lambdaExpectedPublishedCode(zipBytes, "")
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			existing := map[string]string{"package": aws.ToString(getFunctionOut.Configuration.CodeSha256)}
			new := map[string]string{"package": newCodeHash}
			diff, err = diffMapStringString(new, existing, PreviewString(preview)+"zip", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		err = applyLambdaUpdateStages(
			func() error {
				return lambdaEnsureFunctionConfiguration(
					ctx, infraLambda, createInput.Environment.Variables, timeout, memory, preview, showEnvVarValues,
				)
			},
			func() error {
				if !diff {
					return nil
				}
				return LambdaUpdateFunctionCode(ctx, infraLambda, preview)
			},
		)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	if getFunctionOut.Configuration != nil {
		infraLambda.Arn = aws.ToString(getFunctionOut.Configuration.FunctionArn)
		if !lambdaFunctionARNMatches(infraLambda.Arn, Region(), infraLambda.Name) {
			return fmt.Errorf("invalid Lambda function ARN %q", infraLambda.Arn)
		}
	} else if expectedErr != nil && preview {
		callerARN, err := StsArn(ctx)
		if err != nil {
			return err
		}
		identity, err := lambdaIdentityFromCallerARN(infraLambda.Name, infraLambda.infraSetName, Region(), callerARN)
		if err != nil {
			return err
		}
		infraLambda.Arn = identity.arn
	} else {
		return fmt.Errorf("lambda %q returned no function configuration", infraLambda.Name)
	}
	var permissionSids []string
	sids, err := LambdaEnsureTriggerApi(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	err = IamEnsureRoleAllows(ctx, infraLambda.Name, infraLambda.Allow, preview) // ensure role allows after api trigger because it defines $API_ID and WEBSOCKET_ID
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaEnsureTriggerDynamoDB(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	sids, err = LambdaEnsureTriggerSchedule(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	sids, err = LambdaEnsureTriggerAlarm(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	sid, err := LambdaEnsureTriggerSes(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sid)
	sids, err = LambdaEnsureTriggerEcr(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	sids, err = LambdaEnsureTriggerURL(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	sids, err = LambdaEnsureTriggerS3(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	err = LambdaEnsureTriggerSQS(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaSetConcurrency(ctx, infraLambda.Name, concurrency, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = lambdaRemoveUnusedPermissions(ctx, infraLambda.Name, permissionSids, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaExpectedPublishedCode(zipBytes []byte, imageURI string) (string, string, error) {
	if (len(zipBytes) == 0) == (imageURI == "") {
		return "", "", errors.New("lambda code requires exactly one nonempty zip or image URI")
	}
	if imageURI != "" {
		digest, err := lambdaContainerImageDigest(imageURI)
		return "", digest, err
	}
	hash := sha256.Sum256(zipBytes)
	return base64.StdEncoding.EncodeToString(hash[:]), "", nil
}

func validateLambdaPublishedCode(
	name string,
	out *lambda.GetFunctionOutput,
	expectedCodeHash string,
	expectedImageDigest string,
) error {
	if out == nil || out.Configuration == nil {
		return fmt.Errorf("lambda returned no final configuration for %s", name)
	}
	if expectedImageDigest != "" {
		if out.Code == nil {
			return fmt.Errorf("lambda published image digest unavailable for %s", name)
		}
		actualImageDigest, err := lambdaContainerImageDigest(aws.ToString(out.Code.ResolvedImageUri))
		if err != nil {
			return fmt.Errorf("lambda published image digest unavailable for %s: %w", name, err)
		}
		if actualImageDigest != expectedImageDigest {
			return fmt.Errorf("lambda published image digest mismatch for %s", name)
		}
		return nil
	}
	if aws.ToString(out.Configuration.CodeSha256) != expectedCodeHash {
		return fmt.Errorf("lambda published code hash mismatch for %s", name)
	}
	return nil
}

type lambdaCreateFunctionClient interface {
	lambda.GetFunctionAPIClient
	CreateFunction(
		context.Context,
		*lambda.CreateFunctionInput,
		...func(*lambda.Options),
	) (*lambda.CreateFunctionOutput, error)
}

func createLambdaFunction(
	ctx context.Context,
	client lambdaCreateFunctionClient,
	input *lambda.CreateFunctionInput,
	maxWait time.Duration,
) (*lambda.GetFunctionOutput, error) {
	if maxWait <= 0 {
		return nil, errors.New("lambda creation wait must be positive")
	}
	if input == nil || input.Code == nil || aws.ToString(input.FunctionName) == "" {
		return nil, errors.New("lambda creation requires a function name and code")
	}
	expectedCodeHash, expectedImageDigest, err := lambdaExpectedPublishedCode(
		input.Code.ZipFile,
		aws.ToString(input.Code.ImageUri),
	)
	if err != nil {
		return nil, err
	}

	var createOut *lambda.CreateFunctionOutput
	err = Retry(ctx, func() error {
		var err error
		createOut, err = client.CreateFunction(ctx, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	if createOut == nil {
		return nil, errors.New("lambda creation returned no response")
	}

	name := aws.ToString(input.FunctionName)
	finalOut, err := lambda.NewFunctionActiveV2Waiter(client).WaitForOutput(
		ctx,
		&lambda.GetFunctionInput{FunctionName: input.FunctionName},
		maxWait,
	)
	if err != nil {
		return nil, fmt.Errorf("wait for lambda creation %s: %w", name, err)
	}
	if err := validateLambdaPublishedCode(name, finalOut, expectedCodeHash, expectedImageDigest); err != nil {
		return nil, err
	}
	if aws.ToString(finalOut.Configuration.FunctionArn) == "" {
		return nil, fmt.Errorf("lambda creation returned no function ARN for %s", name)
	}
	return finalOut, nil
}

type lambdaCodeUpdateClient interface {
	lambda.GetFunctionAPIClient
	UpdateFunctionCode(
		context.Context,
		*lambda.UpdateFunctionCodeInput,
		...func(*lambda.Options),
	) (*lambda.UpdateFunctionCodeOutput, error)
}

func updateLambdaFunctionCode(
	ctx context.Context,
	client lambdaCodeUpdateClient,
	name string,
	zipBytes []byte,
	imageURI string,
	maxWait time.Duration,
) error {
	if maxWait <= 0 {
		return errors.New("lambda code update wait must be positive")
	}
	expectedCodeHash, expectedImageDigest, err := lambdaExpectedPublishedCode(zipBytes, imageURI)
	if err != nil {
		return err
	}

	updateInput := &lambda.UpdateFunctionCodeInput{FunctionName: aws.String(name)}
	if imageURI != "" {
		updateInput.ImageUri = aws.String(imageURI)
	} else {
		updateInput.ZipFile = zipBytes
	}

	var updateOut *lambda.UpdateFunctionCodeOutput
	var expectedErr error
	err = Retry(ctx, func() error {
		var err error
		updateOut, err = client.UpdateFunctionCode(ctx, updateInput)
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) || strings.Contains(err.Error(), "RequestEntityTooLargeException: Request must be smaller than ") {
				expectedErr = err
				return nil
			}
			Logger.Printf("UpdateFunctionCode error, retrying: %v", err)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if expectedErr != nil {
		return expectedErr
	}
	if updateOut == nil {
		return errors.New("lambda code update returned no response")
	}
	if expectedCodeHash != "" && aws.ToString(updateOut.CodeSha256) != expectedCodeHash {
		return fmt.Errorf("lambda code update response code hash mismatch for %s", name)
	}

	finalOut, err := lambda.NewFunctionUpdatedV2Waiter(client).WaitForOutput(
		ctx,
		&lambda.GetFunctionInput{FunctionName: aws.String(name)},
		maxWait,
	)
	if err != nil {
		return fmt.Errorf("wait for lambda code update %s: %w", name, err)
	}
	return validateLambdaPublishedCode(name, finalOut, expectedCodeHash, expectedImageDigest)
}

func LambdaUpdateFunctionCode(ctx context.Context, infraLambda *InfraLambda, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaUpdateFunctionCode"}
		d.Start()
		defer d.End()
	}
	if !preview {
		var zipBytes []byte
		var imageURI string
		var err error
		if infraLambda.runtime == lambdaRuntimeContainer {
			imageURI = infraLambda.Entrypoint
		} else {
			zipBytes, err = LambdaZipBytes(infraLambda)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		err = updateLambdaFunctionCode(
			ctx, LambdaClient(), infraLambda.Name, zipBytes, imageURI, 5*time.Minute,
		)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview) + "lambda updated code for: " + infraLambda.Name)
	return nil
}

func LambdaDeleteFunction(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaDeleteFunction"}
		d.Start()
		defer d.End()
	}
	_, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
		var notFound *lambdatypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
	}
	if !preview {
		err := Retry(ctx, func() error {
			_, err := LambdaClient().DeleteFunction(ctx, &lambda.DeleteFunctionInput{
				FunctionName: aws.String(name),
			})
			if err != nil {
				var notFound *lambdatypes.ResourceNotFoundException
				if errors.As(err, &notFound) {
					return nil
				}
				return err
			}
			return nil
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted function:", name)
	return nil
}

func LambdaDelete(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaDelete"}
		d.Start()
		defer d.End()
	}
	triggerChan := make(chan *InfraTrigger)
	close(triggerChan)
	infraLambdas, err := InfraListLambda(ctx, triggerChan, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if _, ok := infraLambdas[name]; !ok {
		infraLambdas[name] = &InfraLambda{Name: name}
	}
	for lambdaName, infraLambda := range infraLambdas {
		if lambdaName != name {
			continue
		}
		infraLambda.Name = lambdaName
		identity, err := lambdaResolveIdentity(ctx, lambdaName, infraLambda.infraSetName)
		if err != nil {
			return err
		}
		if err := lambdaCleanupExternalTriggers(ctx, identity, preview); err != nil {
			return err
		}
		infraLambda.Arn = identity.arn
		infraLambda.Trigger = nil
		if identity.exists {
			if err := LambdaEnsureTriggerDynamoDB(ctx, infraLambda, preview); err != nil {
				return err
			}
			if err := LambdaEnsureTriggerSQS(ctx, infraLambda, preview); err != nil {
				return err
			}
		}
		if err := IamDeleteRole(ctx, lambdaName, preview); err != nil {
			return err
		}
		if err := LambdaDeleteFunction(ctx, lambdaName, preview); err != nil {
			return err
		}
		if err := LogsDeleteGroup(ctx, "/aws/lambda/"+lambdaName, preview); err != nil {
			return err
		}
	}
	return nil
}

func FuncUrl(ctx context.Context, lambdaName string) (string, error) {
	out, err := LambdaClient().GetFunctionUrlConfig(ctx, &lambda.GetFunctionUrlConfigInput{
		FunctionName: aws.String(lambdaName),
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if out.FunctionUrl == nil {
		return "", fmt.Errorf("no function url configured for: %s", lambdaName)
	}
	return strings.Trim(*out.FunctionUrl, "/"), nil
}
