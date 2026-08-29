package libaws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type fakeLogsEventsClient struct {
	inputs    []*cloudwatchlogs.FilterLogEventsInput
	pages     []*cloudwatchlogs.FilterLogEventsOutput
	errs      []error
	onRequest func(int)
}

type failingLogsEventsWriter struct {
	err error
}

func (writer failingLogsEventsWriter) Write(_ []byte) (int, error) {
	return 0, writer.err
}

func (client *fakeLogsEventsClient) FilterLogEvents(
	_ context.Context,
	input *cloudwatchlogs.FilterLogEventsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	index := len(client.inputs)
	client.inputs = append(client.inputs, input)
	if client.onRequest != nil {
		client.onRequest(index)
	}
	if index < len(client.errs) && client.errs[index] != nil {
		return nil, client.errs[index]
	}
	if index >= len(client.pages) {
		return nil, fmt.Errorf("unexpected FilterLogEvents request %d", index+1)
	}
	return client.pages[index], nil
}

func filteredLogEvent(id, stream, message string, timestamp int64) cwlogstypes.FilteredLogEvent {
	return cwlogstypes.FilteredLogEvent{
		EventId:       aws.String(id),
		IngestionTime: aws.Int64(timestamp + 10),
		LogStreamName: aws.String(stream),
		Message:       aws.String(message),
		Timestamp:     aws.Int64(timestamp),
	}
}

func TestWriteLogsEventsStreamsEveryPageAndPreservesCloudWatchIdentity(t *testing.T) {
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{
			Events: []cwlogstypes.FilteredLogEvent{
				filteredLogEvent("event-a", "stream-a", "prefix [marker] {\"schema\":1}", 1_000),
				filteredLogEvent("event-b", "stream-b", "second", 1_001),
			},
			NextToken: aws.String("page-2"),
		},
		{
			Events:    nil,
			NextToken: aws.String("page-3"),
		},
		{
			Events: []cwlogstypes.FilteredLogEvent{
				filteredLogEvent("event-c", "stream-c", "third", 1_019),
			},
		},
	}}
	const firstPage = "{\"eventId\":\"event-a\",\"ingestionTime\":1010,\"logStreamName\":\"stream-a\",\"message\":\"prefix [marker] {\\\"schema\\\":1}\",\"timestamp\":1000}\n" +
		"{\"eventId\":\"event-b\",\"ingestionTime\":1011,\"logStreamName\":\"stream-b\",\"message\":\"second\",\"timestamp\":1001}\n"
	const allPages = firstPage +
		"{\"eventId\":\"event-c\",\"ingestionTime\":1029,\"logStreamName\":\"stream-c\",\"message\":\"third\",\"timestamp\":1019}\n"
	var output bytes.Buffer
	streamedBeforeSecondRequest := false
	client.onRequest = func(index int) {
		if index == 1 {
			streamedBeforeSecondRequest = output.String() == firstPage
		}
	}
	start := int64(900)
	end := int64(2_000)

	err := writeLogsEvents(context.Background(), client, &output, "group", "[marker]", start, &end, nil)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != allPages {
		t.Fatalf("JSONL output mismatch:\n got: %q\nwant: %q", output.String(), allPages)
	}
	if !streamedBeforeSecondRequest {
		t.Fatal("first page was not written before requesting the second page")
	}
	if len(client.inputs) != 3 {
		t.Fatalf("expected all three pages, got %d requests", len(client.inputs))
	}
	for index, input := range client.inputs {
		if aws.ToString(input.LogGroupName) != "group" || aws.ToString(input.FilterPattern) != "[marker]" ||
			aws.ToInt64(input.StartTime) != start || aws.ToInt64(input.EndTime) != end-1 {
			t.Fatalf("request %d lost exact half-open query bounds: %#v", index, input)
		}
		if input.Limit != nil {
			t.Fatalf("unbounded request %d unexpectedly set an AWS page limit: %d", index, *input.Limit)
		}
	}
	if client.inputs[0].NextToken != nil || aws.ToString(client.inputs[1].NextToken) != "page-2" ||
		aws.ToString(client.inputs[2].NextToken) != "page-3" {
		t.Fatalf("pagination tokens were not preserved: %#v", client.inputs)
	}
}

func TestWriteLogsEventsPreservesAStreamedPrefixWhenALaterPageFails(t *testing.T) {
	requestErr := errors.New("page two failed")
	client := &fakeLogsEventsClient{
		pages: []*cloudwatchlogs.FilterLogEventsOutput{{
			Events:    []cwlogstypes.FilteredLogEvent{filteredLogEvent("event-a", "stream", "first", 1_000)},
			NextToken: aws.String("page-2"),
		}},
		errs: []error{nil, requestErr},
	}
	var output bytes.Buffer

	err := writeLogsEvents(context.Background(), client, &output, "group", "marker", 900, nil, nil)
	if !errors.Is(err, requestErr) {
		t.Fatalf("error = %v, want wrapped page error", err)
	}
	const want = "{\"eventId\":\"event-a\",\"ingestionTime\":1010,\"logStreamName\":\"stream\",\"message\":\"first\",\"timestamp\":1000}\n"
	if output.String() != want {
		t.Fatalf("streamed prefix mismatch:\n got: %q\nwant: %q", output.String(), want)
	}
}

func TestWriteLogsEventsReturnsOutputErrors(t *testing.T) {
	writeErr := errors.New("output closed")
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{{
		Events: []cwlogstypes.FilteredLogEvent{
			filteredLogEvent("event-a", "stream", "first", 1_000),
		},
	}}}

	err := writeLogsEvents(
		context.Background(),
		client,
		failingLogsEventsWriter{err: writeErr},
		"group",
		"marker",
		900,
		nil,
		nil,
	)
	if !errors.Is(err, writeErr) {
		t.Fatalf("error = %v, want wrapped output error", err)
	}
}

func TestWriteLogsEventsFailsClearlyBeforeEmittingMoreThanMaxEvents(t *testing.T) {
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{
			Events: []cwlogstypes.FilteredLogEvent{
				filteredLogEvent("event-a", "stream", "first", 1_000),
				filteredLogEvent("event-b", "stream", "second", 1_001),
			},
			NextToken: aws.String("page-2"),
		},
		{
			NextToken: aws.String("page-3"),
		},
		{
			Events: []cwlogstypes.FilteredLogEvent{
				filteredLogEvent("event-c", "stream", "third", 1_002),
			},
		},
	}}
	maxEvents := int64(2)
	var output bytes.Buffer

	err := writeLogsEvents(context.Background(), client, &output, "group", "marker", 900, nil, &maxEvents)
	if err == nil || !strings.Contains(err.Error(), "result exceeds --max-events 2") {
		t.Fatalf("error = %v, want explicit max-events error", err)
	}
	const want = "{\"eventId\":\"event-a\",\"ingestionTime\":1010,\"logStreamName\":\"stream\",\"message\":\"first\",\"timestamp\":1000}\n" +
		"{\"eventId\":\"event-b\",\"ingestionTime\":1011,\"logStreamName\":\"stream\",\"message\":\"second\",\"timestamp\":1001}\n"
	if output.String() != want {
		t.Fatalf("bounded output mismatch:\n got: %q\nwant: %q", output.String(), want)
	}
	if len(client.inputs) != 3 {
		t.Fatalf("requests = %d, want 3 to detect an event after an empty page", len(client.inputs))
	}
	wantPageLimits := []int32{3, 1, 1}
	for index, input := range client.inputs {
		if aws.ToInt32(input.Limit) != wantPageLimits[index] {
			t.Fatalf("request %d page limit = %d, want %d", index, aws.ToInt32(input.Limit), wantPageLimits[index])
		}
	}
}

func TestWriteLogsEventsAllowsExactlyMaxEvents(t *testing.T) {
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{{
		Events: []cwlogstypes.FilteredLogEvent{
			filteredLogEvent("event-a", "stream", "first", 1_000),
			filteredLogEvent("event-b", "stream", "second", 1_001),
		},
	}}}
	maxEvents := int64(2)
	var output bytes.Buffer

	if err := writeLogsEvents(context.Background(), client, &output, "group", "marker", 900, nil, &maxEvents); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 2 {
		t.Fatalf("output = %q, want exactly two JSON records", output.String())
	}
}

func TestWriteLogsEventsValidatesQueryBeforeCallingCloudWatch(t *testing.T) {
	endBeforeStart := int64(899)
	endEqualStart := int64(900)
	negativeEnd := int64(-1)
	zeroMax := int64(0)
	negativeMax := int64(-1)
	tests := []struct {
		name        string
		group       string
		filter      string
		startMillis int64
		endMillis   *int64
		maxEvents   *int64
		wantError   string
	}{
		{name: "empty group", group: "", filter: "marker", startMillis: 900, wantError: "log group name must not be empty"},
		{name: "empty filter", group: "group", filter: "", startMillis: 900, wantError: "CloudWatch filter pattern must not be empty"},
		{name: "negative start", group: "group", filter: "marker", startMillis: -1, wantError: "start milliseconds must not be negative"},
		{name: "negative end", group: "group", filter: "marker", startMillis: 900, endMillis: &negativeEnd, wantError: "end milliseconds must not be negative"},
		{name: "end before start", group: "group", filter: "marker", startMillis: 900, endMillis: &endBeforeStart, wantError: "end milliseconds must be greater than start milliseconds"},
		{name: "end equals start", group: "group", filter: "marker", startMillis: 900, endMillis: &endEqualStart, wantError: "end milliseconds must be greater than start milliseconds"},
		{name: "zero max", group: "group", filter: "marker", startMillis: 900, maxEvents: &zeroMax, wantError: "maximum events must be positive"},
		{name: "negative max", group: "group", filter: "marker", startMillis: 900, maxEvents: &negativeMax, wantError: "maximum events must be positive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeLogsEventsClient{}
			err := writeLogsEvents(
				context.Background(),
				client,
				&bytes.Buffer{},
				test.group,
				test.filter,
				test.startMillis,
				test.endMillis,
				test.maxEvents,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if len(client.inputs) != 0 {
				t.Fatalf("CloudWatch requests = %d, want 0", len(client.inputs))
			}
		})
	}
}

func TestWriteLogsEventsRejectsAnEventWithoutDurableIdentity(t *testing.T) {
	client := &fakeLogsEventsClient{pages: []*cloudwatchlogs.FilterLogEventsOutput{{
		Events: []cwlogstypes.FilteredLogEvent{{
			IngestionTime: aws.Int64(1_010),
			LogStreamName: aws.String("stream"),
			Message:       aws.String("message"),
			Timestamp:     aws.Int64(1_000),
		}},
	}}}
	if err := writeLogsEvents(context.Background(), client, &bytes.Buffer{}, "group", "marker", 900, nil, nil); err == nil {
		t.Fatal("event without CloudWatch event ID was accepted")
	}
}

func TestLogsEventsArgsRequireStartMillis(t *testing.T) {
	var missing logsEventsArgs
	parser, err := arg.NewParser(arg.Config{IgnoreEnv: true}, &missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := parser.Parse([]string{"group", "marker"}); err == nil {
		t.Fatal("logs-events accepted a query without --start-millis")
	}

	var present logsEventsArgs
	parser, err = arg.NewParser(arg.Config{IgnoreEnv: true}, &present)
	if err != nil {
		t.Fatal(err)
	}
	if err := parser.Parse([]string{"--start-millis", "0", "group", "marker"}); err != nil {
		t.Fatalf("logs-events rejected an explicit zero start: %v", err)
	}
	if present.StartMillis != 0 {
		t.Fatalf("start millis = %d, want 0", present.StartMillis)
	}
}
