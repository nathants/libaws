package lib

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

var eventsClient *eventbridge.Client
var eventsClientLock sync.Mutex

func EventsClient() *eventbridge.Client {
	eventsClientLock.Lock()
	defer eventsClientLock.Unlock()
	if eventsClient == nil {
		eventsClient = eventbridge.NewFromConfig(*Session())
	}
	return eventsClient
}

func EventsClientExplicit(accessKeyID, accessKeySecret, region string) *eventbridge.Client {
	return eventbridge.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func EventsListBuses(ctx context.Context) ([]eventbridgetypes.EventBus, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EventsListBuses"}
		d.Start()
		defer d.End()
	}
	var token *string
	var buses []eventbridgetypes.EventBus
	for {
		out, err := EventsClient().ListEventBuses(ctx, &eventbridge.ListEventBusesInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		buses = append(buses, out.EventBuses...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return buses, nil
}

func EventsListRules(ctx context.Context, busName *string) ([]eventbridgetypes.Rule, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EventsListRules"}
		d.Start()
		defer d.End()
	}
	var token *string
	var rules []eventbridgetypes.Rule
	for {
		out, err := EventsClient().ListRules(ctx, &eventbridge.ListRulesInput{
			NextToken:    token,
			EventBusName: busName,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		rules = append(rules, out.Rules...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return rules, nil
}

func EventsListRuleTargets(ctx context.Context, ruleName string, busName *string) ([]eventbridgetypes.Target, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EventsListRuleTargets"}
		d.Start()
		defer d.End()
	}
	var targets []eventbridgetypes.Target
	var token *string
	for {
		out, err := EventsClient().ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{
			Rule:         aws.String(ruleName),
			NextToken:    token,
			EventBusName: busName,
		})
		if err != nil {
			return nil, err
		}
		targets = append(targets, out.Targets...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return targets, nil
}
