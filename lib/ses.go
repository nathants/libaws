package lib

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ses"
)

var sesClient *ses.SES
var sesClientLock sync.RWMutex

func SesClientExplicit(accessKeyID, accessKeySecret, region string) *ses.SES {
	return ses.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func SesClient() *ses.SES {
	sesClientLock.Lock()
	defer sesClientLock.Unlock()
	if sesClient == nil {
		sesClient = ses.New(Session())
	}
	return sesClient
}

func SesListReceiptRulesets(ctx context.Context) ([]*ses.ReceiptRuleSetMetadata, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SesListReceiptRulesets"}
		defer d.Log()
	}
	var token *string
	var result []*ses.ReceiptRuleSetMetadata
	for {
		out, err := SesClient().ListReceiptRuleSetsWithContext(ctx, &ses.ListReceiptRuleSetsInput{
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
			_, err := SesClient().SetActiveReceiptRuleSet(&ses.SetActiveReceiptRuleSetInput{
				RuleSetName: nil,
			})
			if err != nil {
				Logger.Println("error:", err)
			}
			_, err = SesClient().DeleteReceiptRuleWithContext(ctx, &ses.DeleteReceiptRuleInput{
				RuleName:    aws.String(domain),
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				Logger.Println("error:", err)
			}
			_, err = SesClient().DeleteReceiptRuleSetWithContext(ctx, &ses.DeleteReceiptRuleSetInput{
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
	if !found {
		if !preview {
			_, err := SesClient().CreateReceiptRuleSetWithContext(ctx, &ses.CreateReceiptRuleSetInput{
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				aerr, ok := err.(awserr.Error)
				if !ok || aerr.Code() != ses.ErrCodeAlreadyExistsException {
					Logger.Println("error:", err)
					return sid, err
				}
			}
			_, err = SesClient().SetActiveReceiptRuleSet(&ses.SetActiveReceiptRuleSetInput{
				RuleSetName: aws.String(domain),
			})
			if err != nil {
				Logger.Println("error:", err)
				return sid, err
			}
			_, err = SesClient().CreateReceiptRuleWithContext(ctx, &ses.CreateReceiptRuleInput{
				RuleSetName: aws.String(domain),
				Rule: &ses.ReceiptRule{
					Enabled:     aws.Bool(true),
					Name:        aws.String(domain),
					Recipients:  []*string{aws.String(domain)},
					TlsPolicy:   aws.String(ses.TlsPolicyRequire),
					ScanEnabled: aws.Bool(false),
					Actions: []*ses.ReceiptAction{
						&ses.ReceiptAction{
							S3Action: &ses.S3Action{
								BucketName:      aws.String(bucket),
								ObjectKeyPrefix: aws.String(prefix),
							},
						},
						&ses.ReceiptAction{
							LambdaAction: &ses.LambdaAction{
								FunctionArn:    aws.String(lambdaArn),
								InvocationType: aws.String(ses.InvocationTypeEvent),
							},
						},
					},
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return sid, err
			}
		}
		Logger.Println(PreviewString(preview)+"created ses receive rule:", domain)
	}
	return sid, nil
}
