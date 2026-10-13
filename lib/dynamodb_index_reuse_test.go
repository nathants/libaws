package lib

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gofrs/uuid"
)

func TestDynamoDBEnsureIndexReusesKeyDefinitions(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	name := "test-index-reuse-" + uuid.Must(uuid.NewV4()).String()
	_, err := DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
	if _, absent := errors.AsType[*ddbtypes.ResourceNotFoundException](err); !absent {
		t.Fatalf("fixture must start absent: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := DynamoDBDeleteTable(ctx, name, false, false); err != nil {
			t.Error("fixture cleanup:", err)
		}
		_, err := DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
		if _, absent := errors.AsType[*ddbtypes.ResourceNotFoundException](err); !absent {
			t.Error("fixture remains:", err)
		}
	})
	input, ttl, err := DynamoDBEnsureInput(name, name, []string{"id:s:hash"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input.AttributeDefinitions = append(input.AttributeDefinitions,
		ddbtypes.AttributeDefinition{AttributeName: aws.String("region"), AttributeType: ddbtypes.ScalarAttributeTypeS},
		ddbtypes.AttributeDefinition{AttributeName: aws.String("position"), AttributeType: ddbtypes.ScalarAttributeTypeS})
	original := ddbtypes.GlobalSecondaryIndex{
		IndexName: aws.String("waiting"),
		KeySchema: []ddbtypes.KeySchemaElement{
			{AttributeName: aws.String("region"), KeyType: ddbtypes.KeyTypeHash},
			{AttributeName: aws.String("position"), KeyType: ddbtypes.KeyTypeRange}},
		Projection: &ddbtypes.Projection{ProjectionType: ddbtypes.ProjectionTypeKeysOnly},
	}
	input.GlobalSecondaryIndexes = []ddbtypes.GlobalSecondaryIndex{original}
	if err := DynamoDBEnsure(ctx, input, ttl, false); err != nil {
		t.Fatal(err)
	}
	if err := DynamoDBWaitForReady(ctx, name); err != nil {
		t.Fatal(err)
	}
	item := map[string]ddbtypes.AttributeValue{
		"id": &ddbtypes.AttributeValueMemberS{Value: "retained"}, "region": &ddbtypes.AttributeValueMemberS{Value: "na"},
		"position": &ddbtypes.AttributeValueMemberS{Value: "first"}, "support": &ddbtypes.AttributeValueMemberBOOL{Value: true},
	}
	if _, err := DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{TableName: input.TableName, Item: item}); err != nil {
		t.Fatal(err)
	}
	next := original
	next.IndexName = aws.String("waiting-support")
	next.Projection = &ddbtypes.Projection{ProjectionType: ddbtypes.ProjectionTypeInclude, NonKeyAttributes: []string{"support"}}
	input.GlobalSecondaryIndexes = append(input.GlobalSecondaryIndexes, next)
	if err := DynamoDBEnsure(ctx, input, ttl, true); err != nil {
		t.Fatal(err)
	}
	before, err := DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: input.TableName})
	if err != nil || len(before.Table.GlobalSecondaryIndexes) != 1 {
		t.Fatalf("preview changed indexes: %v", err)
	}
	if err := DynamoDBEnsure(ctx, input, ttl, false); err != nil {
		t.Fatal("add an index using already-defined keys:", err)
	}
	if err := DynamoDBWaitForReady(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := DynamoDBEnsure(ctx, input, ttl, false); err != nil {
		t.Fatal("converged re-ensure:", err)
	}
	for _, index := range []string{"waiting", "waiting-support"} {
		out, err := DynamoDBClient().Query(ctx, &dynamodb.QueryInput{
			TableName: input.TableName, IndexName: aws.String(index), KeyConditionExpression: aws.String("#region = :region"),
			ExpressionAttributeNames:  map[string]string{"#region": "region"},
			ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":region": item["region"]},
		})
		if err != nil || len(out.Items) != 1 || !reflect.DeepEqual(out.Items[0]["id"], item["id"]) {
			t.Fatalf("retained item missing from %s: %v", index, err)
		}
		if index == "waiting-support" && !reflect.DeepEqual(out.Items[0], item) {
			t.Fatal("new projection lost retained content")
		}
	}
	out, err := DynamoDBClient().GetItem(ctx, &dynamodb.GetItemInput{TableName: input.TableName, Key: map[string]ddbtypes.AttributeValue{"id": item["id"]}, ConsistentRead: aws.Bool(true)})
	if err != nil || !reflect.DeepEqual(out.Item, item) {
		t.Fatal("table item changed:", err)
	}
}
