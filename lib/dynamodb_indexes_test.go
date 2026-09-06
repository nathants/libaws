package lib

import (
	"context"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestDynamoDBIndexAttributes(t *testing.T) {
	t.Run("global throughput", func(t *testing.T) {
		table := &InfraDynamoDB{Key: []string{"id:s:hash"}, Attr: []string{"read=1", "write=1"}, GlobalIndex: map[string]*InfraDynamoDBIndex{
			"by-name": {Key: []string{"name:s:hash"}, Attrs: []string{"read=2", "write=3"}},
		}}
		if err := infraEnsureDynamoDBGlobalIndexToAttrs(table); err != nil {
			t.Fatal(err)
		}
		input, _, err := DynamoDBEnsureInput("fixture", "fixture", table.Key, table.Attr)
		if err != nil {
			t.Fatal(err)
		}
		if len(input.GlobalSecondaryIndexes) != 1 {
			t.Fatal("missing global index")
		}
		index := input.GlobalSecondaryIndexes[0]
		if index.ProvisionedThroughput == nil || aws.ToInt64(index.ProvisionedThroughput.ReadCapacityUnits) != 2 || aws.ToInt64(index.ProvisionedThroughput.WriteCapacityUnits) != 3 {
			t.Fatalf("wrong throughput: %s", Json(index))
		}
	})
	for _, projection := range []string{"", "keys_only", "include"} {
		t.Run("local projection/"+projection, func(t *testing.T) {
			index := &InfraDynamoDBIndex{Key: []string{"id:s:hash", "name:s:range"}}
			want := ddbtypes.ProjectionTypeAll
			if projection != "" {
				index.Attrs = []string{"projection=" + projection}
			}
			if projection == "keys_only" {
				want = ddbtypes.ProjectionTypeKeysOnly
			}
			if projection == "include" {
				want, index.NonKey = ddbtypes.ProjectionTypeInclude, []string{"extra"}
			}
			table := &InfraDynamoDB{Key: []string{"id:s:hash", "created:n:range"}, LocalIndex: map[string]*InfraDynamoDBIndex{"by-name": index}}
			if err := infraEnsureDynamoDBLocalIndexToAttrs(table); err != nil {
				t.Fatal(err)
			}
			input, _, err := DynamoDBEnsureInput("fixture", "fixture", table.Key, table.Attr)
			if err != nil {
				t.Fatal(err)
			}
			if len(input.GlobalSecondaryIndexes) != 0 || len(input.LocalSecondaryIndexes) != 1 || input.LocalSecondaryIndexes[0].Projection == nil || input.LocalSecondaryIndexes[0].Projection.ProjectionType != want {
				t.Fatalf("local projection leaked into global configuration: %s", Json(input))
			}
		})
	}
}

// The checked-in infrastructure example owns provisioning and teardown.
func TestDynamoDBIndexesIntegration(t *testing.T) {
	name := os.Getenv("LIBAWS_DDB_INDEX_TEST_TABLE")
	if name == "" {
		t.Skip("run through examples/misc/dynamodb-indexes")
	}
	if !regexp.MustCompile(`^test-ddb-index-[0-9a-f]{12}$`).MatchString(name) {
		t.Fatal("requires unique example table")
	}
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := DynamoDBWaitForReady(ctx, name); err != nil {
		t.Fatal(err)
	}
	out, err := DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
	if err != nil {
		t.Fatal(err)
	}
	table := out.Table
	if len(table.GlobalSecondaryIndexes) != 1 || len(table.LocalSecondaryIndexes) != 1 {
		t.Fatalf("incorrect real indexes: %s", Json(table))
	}
	global, local := table.GlobalSecondaryIndexes[0], table.LocalSecondaryIndexes[0]
	if aws.ToString(global.IndexName) != "by-name" || global.ProvisionedThroughput == nil || aws.ToInt64(global.ProvisionedThroughput.ReadCapacityUnits) != 2 || aws.ToInt64(global.ProvisionedThroughput.WriteCapacityUnits) != 3 || global.Projection.ProjectionType != ddbtypes.ProjectionTypeInclude || !slices.Equal(global.Projection.NonKeyAttributes, []string{"extra"}) {
		t.Fatalf("incorrect real global index: %s", Json(global))
	}
	if aws.ToString(local.IndexName) != "by-category" || local.Projection.ProjectionType != ddbtypes.ProjectionTypeKeysOnly {
		t.Fatalf("incorrect real local index: %s", Json(local))
	}
}
