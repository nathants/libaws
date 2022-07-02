package lib

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchevents"
)

var eventsClient *cloudwatchevents.CloudWatchEvents
var eventsClientLock sync.RWMutex

func EventsClient() *cloudwatchevents.CloudWatchEvents {
	eventsClientLock.Lock()
	defer eventsClientLock.Unlock()
	if eventsClient == nil {
		eventsClient = cloudwatchevents.New(Session())
	}
	return eventsClient
}

func EventsClientExplicit(accessKeyID, accessKeySecret, region string) *cloudwatchevents.CloudWatchEvents {
	return cloudwatchevents.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func EventsListBuses(ctx context.Context) ([]*cloudwatchevents.EventBus, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EventsListBuses"}
		defer d.Log()
	}
	var token *string
	var buses []*cloudwatchevents.EventBus
	for {
		out, err := EventsClient().ListEventBusesWithContext(ctx, &cloudwatchevents.ListEventBusesInput{
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

func EventsListRules(ctx context.Context, busName *string) ([]*cloudwatchevents.Rule, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EventsListRules"}
		defer d.Log()
	}
	var token *string
	var rules []*cloudwatchevents.Rule
	for {
		out, err := EventsClient().ListRulesWithContext(ctx, &cloudwatchevents.ListRulesInput{
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

func EventsListRuleTargets(ctx context.Context, ruleName string, busName *string) ([]*cloudwatchevents.Target, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EventsListRuleTargets"}
		defer d.Log()
	}
	var targets []*cloudwatchevents.Target
	var token *string
	for {
		out, err := EventsClient().ListTargetsByRuleWithContext(ctx, &cloudwatchevents.ListTargetsByRuleInput{
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
