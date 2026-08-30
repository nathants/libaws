package lib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
)

func TestLambdaScheduleDeletionSurvivesMissingFunction(t *testing.T) {
	const functionName = "usbside-quest"
	var lock sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target := request.Header.Get("X-Amz-Target")
		lock.Lock()
		calls = append(calls, target)
		lock.Unlock()
		writer.Header().Set("Content-Type", "application/x-amz-json-1.1")
		var response any
		switch target {
		case "AWSEvents.ListRules":
			response = map[string]any{"Rules": []map[string]any{{
				"Arn":                "arn:aws:events:us-west-2:337909772623:rule/usbside-quest___cmF0ZSgxIG1pbnV0ZSk",
				"Name":               "usbside-quest___cmF0ZSgxIG1pbnV0ZSk",
				"ScheduleExpression": "rate(1 minute)",
			}}}
		case "AWSEvents.ListTargetsByRule":
			response = map[string]any{"Targets": []map[string]any{{
				"Arn": "arn:aws:lambda:us-west-2:337909772623:function:" + functionName,
				"Id":  "1",
			}}}
		case "AWSEvents.RemoveTargets", "AWSEvents.DeleteRule":
			response = map[string]any{}
		default:
			http.Error(writer, "unexpected operation: "+target, http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode EventBridge response: %v", err)
		}
	}))
	defer server.Close()

	client := eventbridge.New(eventbridge.Options{
		BaseEndpoint: aws.String(server.URL),
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test-access", "test-secret", "")),
		Region:       "us-west-2",
	})
	eventsClientLock.Lock()
	previousClient := eventsClient
	eventsClient = client
	eventsClientLock.Unlock()
	t.Cleanup(func() {
		eventsClientLock.Lock()
		eventsClient = previousClient
		eventsClientLock.Unlock()
	})

	_, err := LambdaEnsureTriggerSchedule(context.Background(), &InfraLambda{Name: functionName}, false)
	if err != nil {
		t.Fatalf("delete stale schedule after function removal: %v", err)
	}
	want := []string{
		"AWSEvents.ListRules",
		"AWSEvents.ListTargetsByRule",
		"AWSEvents.RemoveTargets",
		"AWSEvents.DeleteRule",
	}
	lock.Lock()
	defer lock.Unlock()
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("EventBridge calls = %v, want %v", calls, want)
	}
}
