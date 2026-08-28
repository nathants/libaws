package libaws

import (
	"context"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type fakeLogsEventsClient struct {
	inputs []*cloudwatchlogs.FilterLogEventsInput
	pages  []*cloudwatchlogs.FilterLogEventsOutput
}

func (client *fakeLogsEventsClient) FilterLogEvents(
	_ context.Context,
	input *cloudwatchlogs.FilterLogEventsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	client.inputs = append(client.inputs, input)
	page := client.pages[len(client.inputs)-1]
	return page, nil
}

func TestReadLogsEventsPreservesCloudWatchIdentityAcrossEveryPage(t *testing.T) {
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{
			Events: []cwlogstypes.FilteredLogEvent{
				{
					EventId:       aws.String("event-a"),
					IngestionTime: aws.Int64(1_010),
					LogStreamName: aws.String("stream-a"),
					Message:       aws.String("prefix [marker] {\"schema\":1}"),
					Timestamp:     aws.Int64(1_000),
				},
				{
					EventId:       aws.String("event-b"),
					IngestionTime: aws.Int64(1_011),
					LogStreamName: aws.String("stream-b"),
					Message:       aws.String("second"),
					Timestamp:     aws.Int64(1_000),
				},
			},
			NextToken: aws.String("page-2"),
		},
		{
			Events:    nil,
			NextToken: aws.String("page-3"),
		},
		{
			Events: []cwlogstypes.FilteredLogEvent{{
				EventId:       aws.String("event-c"),
				IngestionTime: aws.Int64(1_020),
				LogStreamName: aws.String("stream-c"),
				Message:       aws.String("third"),
				Timestamp:     aws.Int64(1_019),
			}},
		},
	}}
	start := int64(900)
	end := int64(2_000)

	events, err := readLogsEvents(context.Background(), client, "group", "[marker]", &start, &end)
	if err != nil {
		t.Fatal(err)
	}
	want := []logsEventRecord{
		{EventID: "event-a", IngestionTime: 1_010, LogStreamName: "stream-a", Message: "prefix [marker] {\"schema\":1}", Timestamp: 1_000},
		{EventID: "event-b", IngestionTime: 1_011, LogStreamName: "stream-b", Message: "second", Timestamp: 1_000},
		{EventID: "event-c", IngestionTime: 1_020, LogStreamName: "stream-c", Message: "third", Timestamp: 1_019},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("event output mismatch:\n got: %#v\nwant: %#v", events, want)
	}
	if len(client.inputs) != 3 {
		t.Fatalf("expected all three pages, got %d requests", len(client.inputs))
	}
	for index, input := range client.inputs {
		if aws.ToString(input.LogGroupName) != "group" || aws.ToString(input.FilterPattern) != "[marker]" ||
			aws.ToInt64(input.StartTime) != start || aws.ToInt64(input.EndTime) != end {
			t.Fatalf("request %d lost exact query bounds: %#v", index, input)
		}
	}
	if client.inputs[0].NextToken != nil || aws.ToString(client.inputs[1].NextToken) != "page-2" ||
		aws.ToString(client.inputs[2].NextToken) != "page-3" {
		t.Fatalf("pagination tokens were not preserved: %#v", client.inputs)
	}
}

func TestReadLogsEventsRejectsAnEventWithoutDurableIdentity(t *testing.T) {
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{{
		Events: []cwlogstypes.FilteredLogEvent{{
			IngestionTime: aws.Int64(1_010),
			LogStreamName: aws.String("stream"),
			Message:       aws.String("message"),
			Timestamp:     aws.Int64(1_000),
		}},
	}}}
	if _, err := readLogsEvents(context.Background(), client, "group", "marker", nil, nil); err == nil {
		t.Fatal("event without CloudWatch event ID was accepted")
	}
}
