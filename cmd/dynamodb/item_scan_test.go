package libaws

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestDynamoDBItemScanLimit(t *testing.T) {
	if err := validateDynamoDBItemScanLimit(-1); err == nil {
		t.Fatal("negative limit should fail")
	}
	for _, limit := range []int{0, 1, 10} {
		if err := validateDynamoDBItemScanLimit(limit); err != nil {
			t.Fatalf("limit %d failed: %v", limit, err)
		}
	}
	if dynamoDBItemScanLimitReached(0, 100) {
		t.Fatal("zero means unlimited")
	}
	if dynamoDBItemScanLimitReached(2, 1) {
		t.Fatal("limit reached too early")
	}
	if !dynamoDBItemScanLimitReached(2, 2) {
		t.Fatal("limit was not reached exactly")
	}
	for _, pageSize := range []int{0, -1} {
		if err := validateDynamoDBItemScanPageSize(pageSize); err == nil {
			t.Fatalf("page size %d should fail", pageSize)
		}
	}
	for _, pageSize := range []int{1, 1000} {
		if err := validateDynamoDBItemScanPageSize(pageSize); err != nil {
			t.Fatalf("page size %d failed: %v", pageSize, err)
		}
	}
}

type fakeDynamoDBItemScanClient struct {
	outputs []*dynamodb.ScanOutput
	limits  []int32
}

func (client *fakeDynamoDBItemScanClient) Scan(
	_ context.Context,
	input *dynamodb.ScanInput,
	_ ...func(*dynamodb.Options),
) (*dynamodb.ScanOutput, error) {
	client.limits = append(client.limits, aws.ToInt32(input.Limit))
	if len(client.outputs) == 0 {
		return nil, errors.New("unexpected extra DynamoDB scan request")
	}
	output := client.outputs[0]
	client.outputs = client.outputs[1:]
	return output, nil
}

func dynamoDBScanItem(id string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		"id": &ddbtypes.AttributeValueMemberS{Value: id},
	}
}

func TestDynamoDBItemScanPreservesExactNumbers(t *testing.T) {
	client := &fakeDynamoDBItemScanClient{outputs: []*dynamodb.ScanOutput{{
		Items: []map[string]ddbtypes.AttributeValue{{
			"integer": &ddbtypes.AttributeValueMemberN{Value: "1788015457991966744"},
			"decimal": &ddbtypes.AttributeValueMemberN{Value: "0.12345678901234567890123456789"},
			"numbers": &ddbtypes.AttributeValueMemberNS{Value: []string{"9007199254740993", "-0.0000000000000000001"}},
		}},
	}}}
	var output bytes.Buffer
	if err := dynamoDBItemScanWithClient(context.Background(), client, "table", 0, 1000, &output); err != nil {
		t.Fatal(err)
	}
	want := `{"decimal":0.12345678901234567890123456789,"integer":1788015457991966744,"numbers":[9007199254740993,-0.0000000000000000001]}` + "\n"
	if output.String() != want {
		t.Fatalf("DynamoDB scan changed exact numbers:\n got %s want %s", output.String(), want)
	}
}

func TestDynamoDBItemScanBoundsEachRequestAndStopsAtTotalLimit(t *testing.T) {
	client := &fakeDynamoDBItemScanClient{
		outputs: []*dynamodb.ScanOutput{
			{Items: []map[string]ddbtypes.AttributeValue{dynamoDBScanItem("a")}, LastEvaluatedKey: dynamoDBScanItem("a")},
			{Items: []map[string]ddbtypes.AttributeValue{dynamoDBScanItem("b")}, LastEvaluatedKey: dynamoDBScanItem("b")},
			{Items: []map[string]ddbtypes.AttributeValue{dynamoDBScanItem("c")}, LastEvaluatedKey: dynamoDBScanItem("c")},
		},
	}
	var output bytes.Buffer

	err := dynamoDBItemScanWithClient(context.Background(), client, "table", 3, 2, &output)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.limits, []int32{2, 2, 1}) {
		t.Fatalf("DynamoDB scan request limits=%v, want [2 2 1]", client.limits)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if !slices.Equal(lines, []string{`{"id":"a"}`, `{"id":"b"}`, `{"id":"c"}`}) {
		t.Fatalf("DynamoDB scan output=%v", lines)
	}
}

func TestDynamoDBItemScanUnlimitedUsesPageSizeAndStopsAtEmptyLastKey(t *testing.T) {
	client := &fakeDynamoDBItemScanClient{
		outputs: []*dynamodb.ScanOutput{{LastEvaluatedKey: map[string]ddbtypes.AttributeValue{}}},
	}

	if err := dynamoDBItemScanWithClient(context.Background(), client, "table", 0, 7, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.limits, []int32{7}) {
		t.Fatalf("unlimited DynamoDB scan request limits=%v, want [7]", client.limits)
	}
}
