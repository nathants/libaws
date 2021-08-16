package lib

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/satori/go.uuid"
	"os"
	"reflect"
	"testing"
)

func TestAttrSplitOnce(t *testing.T) {
	type test struct {
		input string
		head  string
		tail  string
		err   bool
	}
	tests := []test{
		{"a", "", "", true},
		{"a.b", "a", "b", false},
		{"a.b.c", "a", "b.c", false},
	}
	for _, test := range tests {
		head, tail, err := splitOnce(test.input, ".")
		if test.err {
			if err == nil {
				t.Errorf("\nexpected error")
				return
			}
			continue
		}
		if head != test.head {
			t.Errorf("\ngot:\n%s\nwant:\n%s\n", head, test.head)
			return
		}
		if tail != test.tail {
			t.Errorf("\ngot:\n%s\nwant:\n%s\n", tail, test.tail)
			return
		}
	}
}

func TestDynamoDBEnsureInput(t *testing.T) {
	type test struct {
		name  string
		keys  []string
		attrs []string
		input *dynamodb.CreateTableInput
		err   bool
	}
	tests := []test{
		{
			"table",
			[]string{"userid:s:hash"},
			[]string{},
			&dynamodb.CreateTableInput{
				TableName:   aws.String("table"),
				BillingMode: aws.String("PAY_PER_REQUEST"),
				StreamSpecification: &dynamodb.StreamSpecification{
					StreamEnabled: aws.Bool(false),
				},
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{AttributeName: aws.String("userid"), AttributeType: aws.String("S")},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{AttributeName: aws.String("userid"), KeyType: aws.String("HASH")},
				},
			},
			false,
		},
		{
			"table",
			[]string{
				"userid:s:hash",
				"date:n:range",
			},
			[]string{},
			&dynamodb.CreateTableInput{
				TableName:   aws.String("table"),
				BillingMode: aws.String("PAY_PER_REQUEST"),
				StreamSpecification: &dynamodb.StreamSpecification{
					StreamEnabled: aws.Bool(false),
				},
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{AttributeName: aws.String("userid"), AttributeType: aws.String("S")},
					{AttributeName: aws.String("date"), AttributeType: aws.String("N")},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{AttributeName: aws.String("userid"), KeyType: aws.String("HASH")},
					{AttributeName: aws.String("date"), KeyType: aws.String("RANGE")},
				},
			},
			false,
		},
		{
			"table",
			[]string{
				"userid:s:hash",
				"date:n:range",
			},
			[]string{
				"read=10",
				"write=10",
				"stream=keys_only",
			},
			&dynamodb.CreateTableInput{
				TableName:   aws.String("table"),
				BillingMode: aws.String("PROVISIONED"),
				ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
					ReadCapacityUnits:  aws.Int64(10),
					WriteCapacityUnits: aws.Int64(10),
				},
				StreamSpecification: &dynamodb.StreamSpecification{
					StreamEnabled:  aws.Bool(true),
					StreamViewType: aws.String("KEYS_ONLY"),
				},
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{AttributeName: aws.String("userid"), AttributeType: aws.String("S")},
					{AttributeName: aws.String("date"), AttributeType: aws.String("N")},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{AttributeName: aws.String("userid"), KeyType: aws.String("HASH")},
					{AttributeName: aws.String("date"), KeyType: aws.String("RANGE")},
				},
			},
			false,
		},
		{
			"table",
			[]string{
				"userid:s:hash",
				"date:n:range",
			},
			[]string{
				"ProvisionedThroughput.ReadCapacityUnits=10",
				"ProvisionedThroughput.WriteCapacityUnits=10",
				"LocalSecondaryIndexes.0.IndexName=index",
				"LocalSecondaryIndexes.0.Key.0=name:s:hash",
				"LocalSecondaryIndexes.0.Key.1=value:n:range",
				"LocalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
				"LocalSecondaryIndexes.0.Projection.ProjectionType=keys_only",
			},
			&dynamodb.CreateTableInput{
				TableName:   aws.String("table"),
				BillingMode: aws.String("PROVISIONED"),
				ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
					ReadCapacityUnits:  aws.Int64(10),
					WriteCapacityUnits: aws.Int64(10),
				},
				StreamSpecification: &dynamodb.StreamSpecification{
					StreamEnabled: aws.Bool(false),
				},
				LocalSecondaryIndexes: []*dynamodb.LocalSecondaryIndex{
					{
						IndexName: aws.String("index"),
						KeySchema: []*dynamodb.KeySchemaElement{
							{AttributeName: aws.String("name"), KeyType: aws.String("HASH")},
							{AttributeName: aws.String("value"), KeyType: aws.String("RANGE")},
						},
						Projection: &dynamodb.Projection{
							NonKeyAttributes: []*string{aws.String("foo")},
							ProjectionType:   aws.String("KEYS_ONLY"),
						},
					},
				},
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{AttributeName: aws.String("userid"), AttributeType: aws.String("S")},
					{AttributeName: aws.String("date"), AttributeType: aws.String("N")},
					{AttributeName: aws.String("name"), AttributeType: aws.String("S")},
					{AttributeName: aws.String("value"), AttributeType: aws.String("N")},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{AttributeName: aws.String("userid"), KeyType: aws.String("HASH")},
					{AttributeName: aws.String("date"), KeyType: aws.String("RANGE")},
				},
			},
			false,
		},
		{
			"table",
			[]string{
				"userid:s:hash",
				"date:n:range",
			},
			[]string{
				"ProvisionedThroughput.ReadCapacityUnits=10",
				"ProvisionedThroughput.WriteCapacityUnits=10",
				"LocalSecondaryIndexes.0.IndexName=index",
				"LocalSecondaryIndexes.0.Key.1=name:s:hash", // 1 before 0. array attrs must be specified in order
				"LocalSecondaryIndexes.0.Key.0=value:n:range",
				"LocalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
				"LocalSecondaryIndexes.0.Projection.ProjectionType=keys_only",
			},
			&dynamodb.CreateTableInput{},
			true,
		},
		{
			"table",
			[]string{
				"userid:s:hash",
				"date:n:range",
			},
			[]string{
				"ProvisionedThroughput.ReadCapacityUnits=10",
				"ProvisionedThroughput.WriteCapacityUnits=10",
				"GlobalSecondaryIndexes.0.IndexName=index",
				"GlobalSecondaryIndexes.0.ProvisionedThroughput.ReadCapacityUnits=5",
				"GlobalSecondaryIndexes.0.ProvisionedThroughput.WriteCapacityUnits=5",
				"GlobalSecondaryIndexes.0.Key.0=name:s:hash",
				"GlobalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
				"GlobalSecondaryIndexes.0.Projection.ProjectionType=keys_only",
				"GlobalSecondaryIndexes.1.IndexName=index2",
				"GlobalSecondaryIndexes.1.ProvisionedThroughput.ReadCapacityUnits=7",
				"GlobalSecondaryIndexes.1.ProvisionedThroughput.WriteCapacityUnits=7",
				"GlobalSecondaryIndexes.1.Key.0=value:n:range",
				"GlobalSecondaryIndexes.1.Projection.NonKeyAttributes.0=bar",
				"GlobalSecondaryIndexes.1.Projection.ProjectionType=keys_only",
			},
			&dynamodb.CreateTableInput{
				TableName:   aws.String("table"),
				BillingMode: aws.String("PROVISIONED"),
				ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
					ReadCapacityUnits:  aws.Int64(10),
					WriteCapacityUnits: aws.Int64(10),
				},
				StreamSpecification: &dynamodb.StreamSpecification{
					StreamEnabled: aws.Bool(false),
				},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{
					{
						IndexName: aws.String("index"),
						ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
							ReadCapacityUnits:  aws.Int64(5),
							WriteCapacityUnits: aws.Int64(5),
						},
						KeySchema: []*dynamodb.KeySchemaElement{
							{AttributeName: aws.String("name"), KeyType: aws.String("HASH")},
						},
						Projection: &dynamodb.Projection{
							NonKeyAttributes: []*string{aws.String("foo")},
							ProjectionType:   aws.String("KEYS_ONLY"),
						},
					},
					{
						IndexName: aws.String("index2"),
						ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
							ReadCapacityUnits:  aws.Int64(7),
							WriteCapacityUnits: aws.Int64(7),
						},
						KeySchema: []*dynamodb.KeySchemaElement{
							{AttributeName: aws.String("value"), KeyType: aws.String("RANGE")},
						},
						Projection: &dynamodb.Projection{
							NonKeyAttributes: []*string{aws.String("bar")},
							ProjectionType:   aws.String("KEYS_ONLY"),
						},
					},
				},
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{AttributeName: aws.String("userid"), AttributeType: aws.String("S")},
					{AttributeName: aws.String("date"), AttributeType: aws.String("N")},
					{AttributeName: aws.String("name"), AttributeType: aws.String("S")},
					{AttributeName: aws.String("value"), AttributeType: aws.String("N")},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{AttributeName: aws.String("userid"), KeyType: aws.String("HASH")},
					{AttributeName: aws.String("date"), KeyType: aws.String("RANGE")},
				},
			},
			false,
		},
		{
			"table",
			[]string{"userid:s:hash"},
			[]string{
				"Tags.foo=bar",
				"Tags.asdf=123",
			},
			&dynamodb.CreateTableInput{
				TableName:   aws.String("table"),
				BillingMode: aws.String("PAY_PER_REQUEST"),
				StreamSpecification: &dynamodb.StreamSpecification{
					StreamEnabled: aws.Bool(false),
				},
				Tags: []*dynamodb.Tag{
					{Key: aws.String("foo"), Value: aws.String("bar")},
					{Key: aws.String("asdf"), Value: aws.String("123")},
				},
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{AttributeName: aws.String("userid"), AttributeType: aws.String("S")},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{AttributeName: aws.String("userid"), KeyType: aws.String("HASH")},
				},
			},
			false,
		},
		{
			"table",
			[]string{
				"userid:s:hash",
				"date:n:range",
			},
			[]string{
				"ProvisionedThroughput.FakeName=10",
			},
			&dynamodb.CreateTableInput{TableName: aws.String("table")},
			true,
		},
	}
	for _, test := range tests {
		input, err := DynamoDBEnsureInput(test.name, test.keys, test.attrs)
		if err != nil && !test.err {
			t.Errorf("\nerror: %s", err)
			continue
		}
		if test.err {
			if err == nil {
				t.Errorf("\nexpected error")
			}
			continue
		}
		if !reflect.DeepEqual(input, test.input) {
			t.Errorf("\ngot:\n%v\nwant:\n%v\n", input, test.input)
			continue
		}
	}
}

func TestDynamoDBEnsureTableAdjustIoThenTurnOffStreaming(t *testing.T) {
	ctx := context.Background()
	name := "test-table-" + uuid.NewV4().String()
	//
	input, err := DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{
			"read=10",
			"write=10",
			"stream=keys_only",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	//
	defer func() {
		err := DynamoDBDeleteTable(ctx, name, false)
		if err != nil {
			panic(err)
		}
		fmt.Println("deleted table:", name)
	}()
	//
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err := DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if *table.Table.ProvisionedThroughput.ReadCapacityUnits != 10 {
		t.Errorf("\nattr mismatch read != 10")
		return
	}
	if *table.Table.ProvisionedThroughput.WriteCapacityUnits != 10 {
		t.Errorf("\nattr mismatch write != 10")
		return
	}
	if !*table.Table.StreamSpecification.StreamEnabled {
		t.Errorf("\nattr mismatch stream !enabled")
		return
	}
	if *table.Table.StreamSpecification.StreamViewType != "KEYS_ONLY" {
		t.Errorf("\nattr mismatch stream != keys_only")
		return
	}
	if len(table.Table.KeySchema) != 1 ||
		len(table.Table.AttributeDefinitions) != 1 ||
		*table.Table.KeySchema[0].AttributeName != "userid" ||
		*table.Table.KeySchema[0].KeyType != "HASH" ||
		*table.Table.AttributeDefinitions[0].AttributeName != "userid" ||
		*table.Table.AttributeDefinitions[0].AttributeType != "S" {
		t.Errorf("\nkeys != [userid:s:hash]")
		return
	}
	//
	input, err = DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{
			"read=5",
			"write=5",
			"stream=keys_only",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err = DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if *table.Table.ProvisionedThroughput.ReadCapacityUnits != 5 {
		t.Errorf("\nattr mismatch read != 5")
		return
	}
	if *table.Table.ProvisionedThroughput.WriteCapacityUnits != 5 {
		t.Errorf("\nattr mismatch write != 5")
		return
	}
	if !*table.Table.StreamSpecification.StreamEnabled {
		t.Errorf("\nattr mismatch stream !enabled")
		return
	}
	if *table.Table.StreamSpecification.StreamViewType != "KEYS_ONLY" {
		t.Errorf("\nattr mismatch stream != keys_only")
		return
	}
	if len(table.Table.KeySchema) != 1 ||
		len(table.Table.AttributeDefinitions) != 1 ||
		*table.Table.KeySchema[0].AttributeName != "userid" ||
		*table.Table.KeySchema[0].KeyType != "HASH" ||
		*table.Table.AttributeDefinitions[0].AttributeName != "userid" ||
		*table.Table.AttributeDefinitions[0].AttributeType != "S" {
		t.Errorf("\nkeys != [userid:s:hash]")
		return
	}
	//
	input, err = DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{
			"read=5",
			"write=5",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err = DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if *table.Table.ProvisionedThroughput.ReadCapacityUnits != 5 {
		t.Errorf("\nattr mismatch read != 5")
		return
	}
	if *table.Table.ProvisionedThroughput.WriteCapacityUnits != 5 {
		t.Errorf("\nattr mismatch write != 5")
		return
	}
	if table.Table.StreamSpecification != nil {
		t.Errorf("\nattr mismatch stream enabled")
		return
	}
	if len(table.Table.KeySchema) != 1 ||
		len(table.Table.AttributeDefinitions) != 1 ||
		*table.Table.KeySchema[0].AttributeName != "userid" ||
		*table.Table.KeySchema[0].KeyType != "HASH" ||
		*table.Table.AttributeDefinitions[0].AttributeName != "userid" ||
		*table.Table.AttributeDefinitions[0].AttributeType != "S" {
		t.Errorf("\nkeys != [userid:s:hash]")
		return
	}
}

func TestDynamoDBEnsureTableAddTagsRemoveTags(t *testing.T) {
	ctx := context.Background()
	name := "test-table-" + uuid.NewV4().String()
	//
	input, err := DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	//
	defer func() {
		err := DynamoDBDeleteTable(ctx, name, false)
		if err != nil {
			panic(err)
		}
		fmt.Println("deleted table:", name)
	}()
	//
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err := DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if *table.Table.ProvisionedThroughput.ReadCapacityUnits != 0 || *table.Table.ProvisionedThroughput.WriteCapacityUnits != 0 {
		t.Errorf("\nattr mismatch throughput != nil")
		return
	}
	if table.Table.StreamSpecification != nil {
		t.Errorf("\nattr mismatch stream != nil")
		return
	}
	if len(table.Table.KeySchema) != 1 ||
		len(table.Table.AttributeDefinitions) != 1 ||
		*table.Table.KeySchema[0].AttributeName != "userid" ||
		*table.Table.KeySchema[0].KeyType != "HASH" ||
		*table.Table.AttributeDefinitions[0].AttributeName != "userid" ||
		*table.Table.AttributeDefinitions[0].AttributeType != "S" {
		t.Errorf("\nkeys != [userid:s:hash]")
		return
	}
	tags, err := DynamoDBListTags(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if len(tags) != 0 {
		t.Errorf("\nlen(tags) != 0")
		return
	}
	//
	input, err = DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{
			"Tags.foo=bar",
			"Tags.asdf=123",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err = DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if *table.Table.ProvisionedThroughput.ReadCapacityUnits != 0 || *table.Table.ProvisionedThroughput.WriteCapacityUnits != 0 {
		t.Errorf("\nattr mismatch throughput != nil")
		return
	}
	if table.Table.StreamSpecification != nil {
		t.Errorf("\nattr mismatch stream != nil")
		return
	}
	if len(table.Table.KeySchema) != 1 ||
		len(table.Table.AttributeDefinitions) != 1 ||
		*table.Table.KeySchema[0].AttributeName != "userid" ||
		*table.Table.KeySchema[0].KeyType != "HASH" ||
		*table.Table.AttributeDefinitions[0].AttributeName != "userid" ||
		*table.Table.AttributeDefinitions[0].AttributeType != "S" {
		t.Errorf("\nkeys != [userid:s:hash]")
		return
	}
	tags, err = DynamoDBListTags(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if len(tags) != 2 {
		t.Errorf("\nlen(tags) != 2")
		return
	}
	if *tags[0].Key != "foo" || *tags[0].Value != "bar" {
		t.Errorf("\ntag:foo != bar")
		return
	}
	if *tags[1].Key != "asdf" || *tags[1].Value != "123" {
		t.Errorf("\ntag:asdf != 123")
		return
	}
	//
	input, err = DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{
			"Tags.asdf=123",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err = DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if *table.Table.ProvisionedThroughput.ReadCapacityUnits != 0 || *table.Table.ProvisionedThroughput.WriteCapacityUnits != 0 {
		t.Errorf("\nattr mismatch throughput != nil")
		return
	}
	if table.Table.StreamSpecification != nil {
		t.Errorf("\nattr mismatch stream != nil")
		return
	}
	if len(table.Table.KeySchema) != 1 ||
		len(table.Table.AttributeDefinitions) != 1 ||
		*table.Table.KeySchema[0].AttributeName != "userid" ||
		*table.Table.KeySchema[0].KeyType != "HASH" ||
		*table.Table.AttributeDefinitions[0].AttributeName != "userid" ||
		*table.Table.AttributeDefinitions[0].AttributeType != "S" {
		t.Errorf("\nkeys != [userid:s:hash]")
		return
	}
	tags, err = DynamoDBListTags(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if len(tags) != 1 {
		t.Errorf("\nlen(tags) != 1")
		return
	}
	if *tags[0].Key != "asdf" || *tags[0].Value != "123" {
		t.Errorf("\ntag:asdf != 123")
		return
	}
}

func TestDynamoDBEnsureTableGlobalIndices(t *testing.T) {
	ctx := context.Background()
	name := "test-table-" + uuid.NewV4().String()
	//
	input, err := DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
		},
		[]string{
			"GlobalSecondaryIndexes.0.IndexName=index",
			"GlobalSecondaryIndexes.0.Key.0=name:s:hash",
			"GlobalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
			"GlobalSecondaryIndexes.0.Projection.ProjectionType=include",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	//
	defer func() {
		err := DynamoDBDeleteTable(ctx, name, false)
		if err != nil {
			panic(err)
		}
		fmt.Println("deleted table:", name)
	}()
	//
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err := DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if len(table.Table.GlobalSecondaryIndexes) != 1 {
		t.Errorf("len(globalIndices) != 1")
		return
	}
	if *table.Table.GlobalSecondaryIndexes[0].IndexName != "index" {
		t.Errorf("\nattr mismatch indexName != index")
		return
	}
	if *table.Table.GlobalSecondaryIndexes[0].KeySchema[0].AttributeName != "name" {
		t.Errorf("\nattr mismatch attrName != name")
		return
	}
	if *table.Table.GlobalSecondaryIndexes[0].KeySchema[0].KeyType != "HASH" {
		t.Errorf("\nattr mismatch keyType != HASH")
		return
	}
	if *table.Table.GlobalSecondaryIndexes[0].Projection.NonKeyAttributes[0] != "foo" {
		t.Errorf("\nattr mismatch nonKeyAttr != foo")
		return
	}
	if *table.Table.GlobalSecondaryIndexes[0].Projection.ProjectionType != "INCLUDE" {
		t.Errorf("\nattr mismatch projectionType != include")
		return
	}
	// NOTE: these tests are slow because updating tables indices is slow
	if os.Getenv("SLOW_TESTS") == "y" {
		//
		input, err = DynamoDBEnsureInput(
			name,
			[]string{
				"userid:s:hash",
			},
			[]string{},
		)
		if err != nil {
			t.Errorf("%w", err)
			return
		}
		err = DynamoDBEnsure(ctx, input, false)
		if err != nil {
			t.Errorf("%w", err)
			return
		}
		err = DynamoDBWaitForReady(ctx, name)
		if err != nil {
			t.Errorf("%w", err)
		}
		table, err = DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
			TableName: aws.String(name),
		})
		if err != nil {
			t.Errorf("%w", err)
			return
		}
		if len(table.Table.GlobalSecondaryIndexes) != 0 {
			t.Errorf("len(globalIndices) != 0")
			return
		}
		//
		input, err = DynamoDBEnsureInput(
			name,
			[]string{
				"userid:s:hash",
			},
			[]string{
				"GlobalSecondaryIndexes.0.IndexName=index",
				"GlobalSecondaryIndexes.0.Key.0=name:s:hash",
				"GlobalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
				"GlobalSecondaryIndexes.0.Projection.ProjectionType=include",
				"GlobalSecondaryIndexes.1.IndexName=index2",
				"GlobalSecondaryIndexes.1.Key.0=title:s:hash",
				"GlobalSecondaryIndexes.1.Key.1=date:n:range",
				"GlobalSecondaryIndexes.1.Projection.ProjectionType=ALL",
			},
		)
		if err != nil {
			t.Errorf("%w", err)
			return
		}
		err = DynamoDBEnsure(ctx, input, false)
		if err != nil {
			t.Errorf("%w", err)
			return
		}
		err = DynamoDBWaitForReady(ctx, name)
		if err != nil {
			t.Errorf("%w", err)
		}
		table, err = DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
			TableName: aws.String(name),
		})
		if err != nil {
			t.Errorf("%w", err)
			return
		}
		if len(table.Table.GlobalSecondaryIndexes) != 2 {
			t.Errorf("len(globalIndices) != 2")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[0].IndexName != "index" {
			t.Errorf("\nattr mismatch indexName != index")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[0].KeySchema[0].AttributeName != "name" {
			t.Errorf("\nattr mismatch attrName != name")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[0].KeySchema[0].KeyType != "HASH" {
			t.Errorf("\nattr mismatch keyType != HASH")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[0].Projection.NonKeyAttributes[0] != "foo" {
			t.Errorf("\nattr mismatch nonKeyAttr != foo")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[0].Projection.ProjectionType != "INCLUDE" {
			t.Errorf("\nattr mismatch projectionType != include")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[1].IndexName != "index2" {
			t.Errorf("\nattr mismatch indexName != index2")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[1].KeySchema[0].AttributeName != "title" {
			t.Errorf("\nattr mismatch attrName != title")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[1].KeySchema[0].KeyType != "HASH" {
			t.Errorf("\nattr mismatch keyType != HASH")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[1].KeySchema[1].AttributeName != "date" {
			t.Errorf("\nattr mismatch attrName != date")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[1].KeySchema[1].KeyType != "RANGE" {
			t.Errorf("\nattr mismatch keyType != RANGE")
			return
		}
		if *table.Table.GlobalSecondaryIndexes[1].Projection.ProjectionType != "ALL" {
			t.Errorf("\nattr mismatch projectionType != all")
			return
		}
	}
}

func TestDynamoDBEnsureTableLocalIndices(t *testing.T) {
	ctx := context.Background()
	name := "test-table-" + uuid.NewV4().String()
	//
	input, err := DynamoDBEnsureInput(
		name,
		[]string{
			"userid:s:hash",
			"date:n:range",
		},
		[]string{
			"LocalSecondaryIndexes.0.IndexName=index",
			"LocalSecondaryIndexes.0.Key.0=userid:s:hash",
			"LocalSecondaryIndexes.0.Key.1=value:n:range",
			"LocalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
			"LocalSecondaryIndexes.0.Projection.ProjectionType=include",
		},
	)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	err = DynamoDBEnsure(ctx, input, false)
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	//
	defer func() {
		err := DynamoDBDeleteTable(ctx, name, false)
		if err != nil {
			panic(err)
		}
		fmt.Println("deleted table:", name)
	}()
	//
	err = DynamoDBWaitForReady(ctx, name)
	if err != nil {
		t.Errorf("%w", err)
	}
	table, err := DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		t.Errorf("%w", err)
		return
	}
	if len(table.Table.LocalSecondaryIndexes) != 1 {
		t.Errorf("len(localIndices) != 1")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].IndexName != "index" {
		t.Errorf("\nattr mismatch indexName != index")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].KeySchema[0].AttributeName != "userid" {
		t.Errorf("\nattr mismatch attrName != userid")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].KeySchema[0].KeyType != "HASH" {
		t.Errorf("\nattr mismatch keyType != HASH")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].KeySchema[1].AttributeName != "value" {
		t.Errorf("\nattr mismatch attrName != value")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].KeySchema[1].KeyType != "RANGE" {
		t.Errorf("\nattr mismatch keyType != RANGE")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].Projection.NonKeyAttributes[0] != "foo" {
		t.Errorf("\nattr mismatch nonKeyAttr != foo")
		return
	}
	if *table.Table.LocalSecondaryIndexes[0].Projection.ProjectionType != "INCLUDE" {
		t.Errorf("\nattr mismatch projectionType != include")
		return
	}
}
