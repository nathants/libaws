package lib

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func readmeDynamoDBExample(t *testing.T, headings ...string) string {
	t.Helper()
	data, err := os.ReadFile("../readme.md")
	if err != nil {
		t.Fatal(err)
	}
	section := string(data)
	for _, marker := range append(headings, "  ```yaml\n") {
		var found bool
		_, section, found = strings.Cut(section, marker)
		if !found {
			t.Fatalf("missing README example marker %q", marker)
		}
	}
	snippet, _, found := strings.Cut(section, "  ```")
	if !found {
		t.Fatal("missing README example closing fence")
	}
	lines := strings.Split(snippet, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimPrefix(line, "  ")
	}
	return strings.Join(lines, "\n")
}

func parseReadmeDynamoDBExample(t *testing.T, definition string) *InfraSet {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	infra, err := InfraParse(path)
	if err != nil {
		t.Fatalf("parse actual README example: %v", err)
	}
	return infra
}

func TestReadmeDynamoDBTriggerExample(t *testing.T) {
	definition := readmeDynamoDBExample(t, "#### Trigger\n", "* Example:")
	// This section documents only the trigger fragment, not its entrypoint.
	definition = strings.Replace(definition, "  test-lambda:\n", "  test-lambda:\n    entrypoint: main.go\n", 1)
	infra := parseReadmeDynamoDBExample(t, definition)
	mappings, err := lambdaDynamoDBDesiredMappings(context.Background(), func(context.Context, string) (string, error) {
		return "arn:aws:dynamodb:us-east-1:123456789012:table/test-table/stream/2026-01-01T00:00:00.000", nil
	}, infra.Lambda["test-lambda"], true)
	if err != nil || len(mappings) != 1 || mappings[0].create.StartingPosition != lambdatypes.EventSourcePositionLatest {
		t.Fatalf("actual README trigger configuration is invalid: mappings=%v err=%v", mappings, err)
	}
}

func TestReadmeDynamoDBLocalIndexExample(t *testing.T) {
	infra := parseReadmeDynamoDBExample(t, readmeDynamoDBExample(t, "* Example local secondary index:"))
	table := infra.DynamoDB["test-table"]
	if err := infraEnsureDynamoDBLocalIndexToAttrs(table); err != nil {
		t.Fatal(err)
	}
	input, _, err := DynamoDBEnsureInput("fixture", "test-table", table.Key, table.Attr)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.LocalSecondaryIndexes) != 1 {
		t.Fatal("README must declare one local index")
	}
	// AWS requires a composite table key and an LSI sharing its partition key.
	for name, keys := range map[string][]ddbtypes.KeySchemaElement{"table": input.KeySchema, "local index": input.LocalSecondaryIndexes[0].KeySchema} {
		if len(keys) != 2 || keys[0].KeyType != ddbtypes.KeyTypeHash || aws.ToString(keys[0].AttributeName) != "id" || keys[1].KeyType != ddbtypes.KeyTypeRange {
			t.Errorf("README generates AWS-invalid %s keys: %s", name, Json(keys))
		}
	}
}
