package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/gofrs/uuid"
)

func decodeLogsEventsJSONL(data []byte) ([]logsEventRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []logsEventRecord
	for {
		var record logsEventRecord
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode JSONL record: %w", err)
		}
		records = append(records, record)
	}
}

func normalizeDynamicLogsEventIdentity(records []logsEventRecord) error {
	seenEventIDs := map[string]struct{}{}
	for index := range records {
		if records[index].EventID == "" {
			return fmt.Errorf("record %d has an empty event ID", index)
		}
		if _, exists := seenEventIDs[records[index].EventID]; exists {
			return fmt.Errorf("record %d repeats event ID %q", index, records[index].EventID)
		}
		seenEventIDs[records[index].EventID] = struct{}{}
		if records[index].IngestionTime <= 0 {
			return fmt.Errorf("record %d has invalid ingestion time %d", index, records[index].IngestionTime)
		}
		records[index].EventID = "<cloudwatch-event-id>"
		records[index].IngestionTime = 0
	}
	return nil
}

func TestLogsEventsAgainstCloudWatch(t *testing.T) {
	if os.Getenv("LIBAWS_INTEGRATION") != "1" {
		t.Skip("set LIBAWS_INTEGRATION=1 to run AWS integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	expectedAccount := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expectedAccount == "" {
		t.Fatal("LIBAWS_TEST_ACCOUNT must identify the authorized scratch account")
	}
	actualAccount, err := StsAccount(ctx)
	if err != nil {
		t.Fatalf("verify AWS account: %v", err)
	}
	if actualAccount != expectedAccount {
		t.Fatalf("refusing CloudWatch integration test in AWS account %q; expected scratch account %q", actualAccount, expectedAccount)
	}

	client := LogsClient()
	group := "/libaws/integration/logs-events-" + uuid.Must(uuid.NewV4()).String()
	stream := "events"
	_, err = client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(group),
	})
	if err != nil {
		t.Fatalf("create log group: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		_, cleanupErr := client.DeleteLogGroup(cleanupCtx, &cloudwatchlogs.DeleteLogGroupInput{
			LogGroupName: aws.String(group),
		})
		var notFound *cwlogstypes.ResourceNotFoundException
		if cleanupErr != nil && !errors.As(cleanupErr, &notFound) {
			t.Errorf("delete integration log group %q: %v", group, cleanupErr)
		}
	})
	_, err = client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(group),
		LogStreamName: aws.String(stream),
	})
	if err != nil {
		t.Fatalf("create log stream: %v", err)
	}

	// Keep all events after the log group's creation time and safely in the past.
	time.Sleep(time.Second)
	startMillis := time.Now().Add(-500 * time.Millisecond).UnixMilli()
	endMillis := startMillis + 4
	putOut, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogEvents: []cwlogstypes.InputLogEvent{
			{Message: aws.String("MATCH before-start"), Timestamp: aws.Int64(startMillis - 1)},
			{Message: aws.String("MATCH at-start"), Timestamp: aws.Int64(startMillis)},
			{Message: aws.String("IGNORE in-window"), Timestamp: aws.Int64(startMillis + 1)},
			{Message: aws.String("MATCH before-end"), Timestamp: aws.Int64(endMillis - 1)},
			{Message: aws.String("MATCH at-end"), Timestamp: aws.Int64(endMillis)},
		},
		LogGroupName:  aws.String(group),
		LogStreamName: aws.String(stream),
	})
	if err != nil {
		t.Fatalf("put log events: %v", err)
	}
	if putOut.RejectedLogEventsInfo != nil {
		t.Fatalf("CloudWatch rejected integration events: %#v", putOut.RejectedLogEventsInfo)
	}

	want := []logsEventRecord{
		{
			EventID:       "<cloudwatch-event-id>",
			LogStreamName: stream,
			Message:       "MATCH at-start",
			Timestamp:     startMillis,
		},
		{
			EventID:       "<cloudwatch-event-id>",
			LogStreamName: stream,
			Message:       "MATCH before-end",
			Timestamp:     endMillis - 1,
		},
	}
	var output bytes.Buffer
	var got []logsEventRecord
	deadline := time.Now().Add(30 * time.Second)
	for {
		output.Reset()
		err = logsEventsWithClient(ctx, client, &output, group, "MATCH", startMillis, &endMillis, nil)
		if err != nil {
			t.Fatalf("stream log events: %v", err)
		}
		got, err = decodeLogsEventsJSONL(output.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == len(want) {
			break
		}
		if len(got) > len(want) {
			t.Fatalf("half-open filtered query returned %d records, want %d: %s", len(got), len(want), output.String())
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for filtered events; last output: %s", output.String())
		}
		time.Sleep(500 * time.Millisecond)
	}
	if strings.Count(output.String(), "\n") != len(want) || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("output is not one newline-terminated JSON object per event: %q", output.String())
	}
	if err := normalizeDynamicLogsEventIdentity(got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered records mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	maxEvents := int64(1)
	output.Reset()
	err = logsEventsWithClient(ctx, client, &output, group, "MATCH", startMillis, &endMillis, &maxEvents)
	if err == nil || !strings.Contains(err.Error(), "result exceeds --max-events 1") {
		t.Fatalf("bounded query error = %v, want explicit max-events error", err)
	}
	got, err = decodeLogsEventsJSONL(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeDynamicLogsEventIdentity(got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("bounded records mismatch:\n got: %#v\nwant: %#v", got, want[:1])
	}
}
