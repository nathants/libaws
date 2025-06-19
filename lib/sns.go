package lib

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

var snsClient *sns.Client
var snsClientLock sync.Mutex

func SNSClientExplicit(accessKeyID, accessKeySecret, region string) *sns.Client {
	return sns.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func SNSClient() *sns.Client {
	snsClientLock.Lock()
	defer snsClientLock.Unlock()
	if snsClient == nil {
		snsClient = sns.NewFromConfig(*Session())
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

func SNSListSubscriptions(ctx context.Context, topicArn string) ([]snstypes.Subscription, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SNSListSubscriptions"}
		d.Start()
		defer d.End()
	}
	var nextToken *string
	var subscriptions []snstypes.Subscription
	for {
		out, err := SNSClient().ListSubscriptionsByTopic(ctx, &sns.ListSubscriptionsByTopicInput{
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "SNSEnsure"}
		d.Start()
		defer d.End()
	}
	snsArn, err := SNSArn(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = SNSClient().GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
		TopicArn: aws.String(snsArn),
	})
	if err != nil {
		var nfe *snstypes.NotFoundException
		if !errors.As(err, &nfe) {
			return err
		}
		if !preview {
			_, err = SNSClient().CreateTopic(ctx, &sns.CreateTopicInput{
				Name: aws.String(name),
				Attributes: map[string]string{
					"KmsMasterKeyId": "alias/aws/sns",
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
