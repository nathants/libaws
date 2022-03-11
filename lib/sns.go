package lib

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/sns"
)

var snsClient *sns.SNS
var snsClientLock sync.RWMutex

func SNSClientExplicit(accessKeyID, accessKeySecret, region string) *sns.SNS {
	return sns.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func SNSClient() *sns.SNS {
	snsClientLock.Lock()
	defer snsClientLock.Unlock()
	if snsClient == nil {
		snsClient = sns.New(Session())
	}
	return snsClient
}

func SNSArn(ctx context.Context, name string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return fmt.Sprintf("arn:aws:sns:%s:%s:%s", Region(), account, name), nil
}

func SNSListSubscriptions(ctx context.Context, topicArn string) ([]*sns.Subscription, error) {
	var nextToken *string
	var subscriptions []*sns.Subscription
	for {
		out, err := SNSClient().ListSubscriptionsByTopicWithContext(ctx, &sns.ListSubscriptionsByTopicInput{
			NextToken: nextToken,
			TopicArn:  aws.String(topicArn),
		})
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, out.Subscriptions...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return subscriptions, nil
}

func SNSEnsure(ctx context.Context, name string, preview bool) error {
	snsArn, err := SNSArn(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = SNSClient().GetTopicAttributesWithContext(ctx, &sns.GetTopicAttributesInput{
		TopicArn: aws.String(snsArn),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != sns.ErrCodeNotFoundException {
			return err
		}
		if !preview {
			_, err := SNSClient().CreateTopicWithContext(ctx, &sns.CreateTopicInput{
				Name: aws.String(name),
				Attributes: map[string]*string{
					"KmsMasterKeyId": aws.String("alias/aws/sns"),
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"create sns topic:", name)
	}
	return nil
}
