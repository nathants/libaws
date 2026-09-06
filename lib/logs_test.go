package lib

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/gofrs/uuid"
)

type fakeLogsRecentClient struct {
	lock      sync.Mutex
	active    int
	maxActive int
	describes int
	lastLimit int32
	calls     int
	streams   []cwlogstypes.LogStream
}

func (client *fakeLogsRecentClient) DescribeLogStreams(
	_ context.Context,
	_ *cloudwatchlogs.DescribeLogStreamsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	client.describes++
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
	client.lastLimit = aws.ToInt32(input.Limit)
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
	for _, numLines := range []int{0, -1, 10001} {
		if _, err := logsRecentWithClient(context.Background(), client, "group", numLines, 4); err == nil {
			t.Fatalf("expected numLines %d to fail", numLines)
		}
	}
	if _, err := logsRecentWithClient(context.Background(), client, "group", 1, 0); err == nil {
		t.Fatal("expected zero concurrency to fail")
	}
	if client.calls != 0 || client.describes != 0 {
		t.Fatalf("invalid input performed %d describes and %d reads", client.describes, client.calls)
	}
}

func TestLogsRecentAcceptsAWSMaximum(t *testing.T) {
	client := &fakeLogsRecentClient{streams: []cwlogstypes.LogStream{{LogStreamName: aws.String("stream-0")}}}
	if _, err := logsRecentWithClient(context.Background(), client, "group", 10000, 1); err != nil {
		t.Fatal(err)
	}
	if client.lastLimit != 10000 {
		t.Fatalf("request limit=%d, want AWS maximum10000", client.lastLimit)
	}
}

func TestLogsRecentLimitIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := "/libaws/test-recent-" + uuid.Must(uuid.NewV4()).String()
	client := LogsClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		var absent *cwlogstypes.ResourceNotFoundException
		if _, err := client.DeleteLogGroup(cleanupCtx, &cloudwatchlogs.DeleteLogGroupInput{LogGroupName: aws.String(name)}); err != nil && !errors.As(err, &absent) {
			t.Errorf("delete log fixture: %v", err)
		}
		if _, err := client.DescribeLogStreams(cleanupCtx, &cloudwatchlogs.DescribeLogStreamsInput{LogGroupName: aws.String(name)}); !errors.As(err, &absent) {
			t.Errorf("log fixture remains or deletion could not be verified: %v", err)
		}
	})
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String(name)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{LogGroupName: aws.String(name), LogStreamName: aws.String("fixture")}); err != nil {
		t.Fatal(err)
	}
	out, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{LogGroupName: aws.String(name), LogStreamName: aws.String("fixture"), LogEvents: []cwlogstypes.InputLogEvent{{Timestamp: aws.Int64(time.Now().UnixMilli()), Message: aws.String("limit fixture")}}})
	if err != nil || out.RejectedLogEventsInfo != nil {
		t.Fatalf("put fixture: out=%v err=%v", out, err)
	}
	if err := Retry(ctx, func() error {
		out, err := client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{LogGroupName: aws.String(name), LogStreamName: aws.String("fixture"), StartFromHead: aws.Bool(false)})
		if err != nil {
			return err
		}
		if len(out.Events) != 1 {
			return errors.New("fixture event not yet visible")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lines, err := LogsRecent(ctx, name, 10000)
	if err != nil || len(lines) != 1 || !strings.HasSuffix(lines[0], " limit fixture") {
		t.Fatalf("AWS maximum query: lines=%v err=%v", lines, err)
	}
	if _, err := LogsRecent(ctx, name, 10001); err == nil || !strings.Contains(err.Error(), "between 1 and 10000") {
		t.Fatalf("expected local limit validation, got %v", err)
	}
}

type blockingMalformedLogsClient struct {
	started  chan context.Context
	finished chan struct{}
}

func (client *blockingMalformedLogsClient) DescribeLogStreams(context.Context, *cloudwatchlogs.DescribeLogStreamsInput, ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	return &cloudwatchlogs.DescribeLogStreamsOutput{LogStreams: []cwlogstypes.LogStream{{LogStreamName: aws.String("valid")}, {}}}, nil
}

func (client *blockingMalformedLogsClient) GetLogEvents(ctx context.Context, _ *cloudwatchlogs.GetLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error) {
	client.started <- ctx
	<-ctx.Done()
	close(client.finished)
	return nil, ctx.Err()
}

func TestLogsRecentMalformedStreamDoesNotLeaveWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &blockingMalformedLogsClient{started: make(chan context.Context, 1), finished: make(chan struct{})}
	_, err := logsRecentWithClient(ctx, client, "group", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "empty log stream name") {
		t.Fatalf("expected malformed-stream rejection, got %v", err)
	}
	select {
	case workerCtx := <-client.started:
		if workerCtx.Err() == nil {
			t.Error("returned with an uncancelled GetLogEvents worker")
		}
		cancel()
		select {
		case <-client.finished:
		case <-time.After(time.Second):
			t.Fatal("worker did not terminate after parent cancellation")
		}
	case <-time.After(100 * time.Millisecond):
		// Prevalidation can reject the entire list without starting any workers.
	}
}
