package lib

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type fakeLogsRecentClient struct {
	lock      sync.Mutex
	active    int
	maxActive int
	calls     int
	streams   []cwlogstypes.LogStream
}

func (client *fakeLogsRecentClient) DescribeLogStreams(
	_ context.Context,
	_ *cloudwatchlogs.DescribeLogStreamsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	return &cloudwatchlogs.DescribeLogStreamsOutput{LogStreams: client.streams}, nil
}

func (client *fakeLogsRecentClient) GetLogEvents(
	_ context.Context,
	input *cloudwatchlogs.GetLogEventsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.GetLogEventsOutput, error) {
	client.lock.Lock()
	client.active++
	client.calls++
	if client.active > client.maxActive {
		client.maxActive = client.active
	}
	client.lock.Unlock()

	time.Sleep(20 * time.Millisecond)

	client.lock.Lock()
	client.active--
	client.lock.Unlock()

	var ordinal int64
	if _, err := fmt.Sscanf(aws.ToString(input.LogStreamName), "stream-%d", &ordinal); err != nil {
		return nil, err
	}
	timestamp := int64(1_000 + ordinal)
	return &cloudwatchlogs.GetLogEventsOutput{
		Events: []cwlogstypes.OutputLogEvent{{
			Timestamp: aws.Int64(timestamp),
			Message:   aws.String(fmt.Sprintf("line-%02d", ordinal)),
		}},
	}, nil
}

func TestLogsRecentFetchesStreamsConcurrentlyAndKeepsNewestLines(t *testing.T) {
	client := &fakeLogsRecentClient{}
	for index := range 12 {
		client.streams = append(client.streams, cwlogstypes.LogStream{
			LogStreamName: aws.String(fmt.Sprintf("stream-%d", index)),
		})
	}

	lines, err := logsRecentWithClient(context.Background(), client, "group", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 12 {
		t.Fatalf("expected all 12 streams to be inspected, got %d", client.calls)
	}
	if client.maxActive < 2 || client.maxActive > 4 {
		t.Fatalf("expected bounded concurrent reads, max active was %d", client.maxActive)
	}
	if len(lines) != 3 {
		t.Fatalf("expected three lines, got %d", len(lines))
	}
	if lines[0][len(lines[0])-7:] != "line-09" ||
		lines[1][len(lines[1])-7:] != "line-10" ||
		lines[2][len(lines[2])-7:] != "line-11" {
		t.Fatalf("unexpected newest-line order: %#v", lines)
	}
}

func TestLogsRecentRejectsInvalidBoundsBeforeNetworking(t *testing.T) {
	client := &fakeLogsRecentClient{}
	for _, numLines := range []int{0, -1} {
		if _, err := logsRecentWithClient(context.Background(), client, "group", numLines, 4); err == nil {
			t.Fatalf("expected numLines %d to fail", numLines)
		}
	}
	if _, err := logsRecentWithClient(context.Background(), client, "group", 1, 0); err == nil {
		t.Fatal("expected zero concurrency to fail")
	}
	if client.calls != 0 {
		t.Fatalf("invalid input performed %d reads", client.calls)
	}
}
