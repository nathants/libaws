package lib

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
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

func SesListReceiptRulesets(ctx context.Context) ([]sestypes.ReceiptRuleSetMetadata, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesListReceiptRulesets"}
		defer d.Log()
	}
	var token *string
	var result []sestypes.ReceiptRuleSetMetadata
	for {
		out, err := SesClient().ListReceiptRuleSets(ctx, &sesv2.ListReceiptRuleSetsInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		result = append(result, out.RuleSets...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return result, nil
}

func SesRmReceiptRuleset(ctx context.Context, domain string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesRmReceiptRuleset"}
		defer d.Log()
	}
	rules, err := SesListReceiptRulesets(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, rule := range rules {
		name := *rule.Name
		if name != domain {
			continue
		}
		if !preview {
			// TODO improve this, we ignore errors to make deletes idempotent. instead only delete things that exist, and check errors.
			_, err = SesClient().SetActiveReceiptRuleSet(ctx, &sesv2.SetActiveReceiptRuleSetInput{
				RuleSetName: nil,
			})
			if err != nil {
				Logger.Println("error:", err)
			}
			_, err = SesClient().DeleteReceiptRule(ctx, &sesv2.DeleteReceiptRuleInput{
				RuleName:    aws.String(domain),
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				Logger.Println("error:", err)
			}
			_, err = SesClient().DeleteReceiptRuleSet(ctx, &sesv2.DeleteReceiptRuleSetInput{
				RuleSetName: aws.String(name),
			})
			if err != nil {
				Logger.Println("error:", err)
			}
		}
		Logger.Println(PreviewString(preview)+"delete receive rule:", domain)
	}
	return nil
}

func SesEnsureReceiptRuleset(ctx context.Context, domain string, bucket string, prefix string, lambdaArn string, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesEnsureReceiptRuleset"}
		defer d.Log()
	}
	lambdaName := Last(strings.Split(lambdaArn, ":"))
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sid, err := lambdaEnsurePermission(ctx, lambdaName, "ses.amazonaws.com", "arn:aws:ses:"+Region()+":"+account+":receipt-rule-set/"+domain+":receipt-rule/"+domain, preview)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	rules, err := SesListReceiptRulesets(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return sid, err
	}
	found := false
	for _, rule := range rules {
		if *rule.Name == domain {
			found = true
			break
		}
	}
	var prefixParam *string
	if prefix != "" {
		prefixParam = aws.String(prefix)
	}
	ruleInput := &sesv2.CreateReceiptRuleInput{
		RuleSetName: aws.String(domain),
		Rule: &sestypes.ReceiptRule{
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
						FunctionArn:    aws.String(lambdaArn),
						InvocationType: sestypes.InvocationTypeEvent,
					},
				},
			},
		},
	}
	if !found {
		if !preview {
			_, err = SesClient().CreateReceiptRuleSet(ctx, &sesv2.CreateReceiptRuleSetInput{
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				var exists *sestypes.AlreadyExistsException
				if errors.As(err, &exists) {
				} else {
					Logger.Println("error:", err)
					return sid, err
				}
			}
			_, err = SesClient().SetActiveReceiptRuleSet(ctx, &sesv2.SetActiveReceiptRuleSetInput{
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				Logger.Println("error:", err)
				return sid, err
			}
			_, err = SesClient().CreateReceiptRule(ctx, ruleInput)
			if err != nil {
				Logger.Println("error:", err)
				return sid, err
			}
		}
		Logger.Println(PreviewString(preview)+"created ses receive rule:", "domain="+domain, "bucket="+bucket, "prefix="+prefix)
	} else {
		if !preview {
			out, err := SesClient().DescribeReceiptRule(ctx, &sesv2.DescribeReceiptRuleInput{
				RuleName:    aws.String(domain),
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				Logger.Println("error:", err)
				return sid, err
			}
			if !reflect.DeepEqual(out.Rule, ruleInput.Rule) {
				oldDomain := ""
				oldBucket := ""
				oldPrefix := ""
				if len(out.Rule.Recipients) > 0 {
					oldDomain = out.Rule.Recipients[0]
				}
				Logger.Println(PformatAlways(*out))
				if len(out.Rule.Actions) > 0 {
					if out.Rule.Actions[0].S3Action != nil {
						if out.Rule.Actions[0].S3Action.BucketName != nil {
							oldBucket = *out.Rule.Actions[0].S3Action.BucketName
						}
						if out.Rule.Actions[0].S3Action.ObjectKeyPrefix != nil {
							oldPrefix = *out.Rule.Actions[0].S3Action.ObjectKeyPrefix
						}
					}
				}
				if !preview {
					_, err = SesClient().UpdateReceiptRule(ctx, &sesv2.UpdateReceiptRuleInput{
						Rule:        ruleInput.Rule,
						RuleSetName: aws.String(domain),
					})
					if err != nil {
						Logger.Println("error:", err)
						return sid, err
					}
				}
				Logger.Println(PreviewString(preview)+"update ses receive rule: domain="+
					oldDomain+" bucket="+oldBucket+" prefix="+oldPrefix,
					"=> domain="+domain+" bucket="+bucket+" prefix="+prefix)
			}
		}
	}
	return sid, nil
}
