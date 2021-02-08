package lib

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
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
				TableName:              aws.String("table"),
				BillingMode:            aws.String("PAY_PER_REQUEST"),
				ProvisionedThroughput:  &dynamodb.ProvisionedThroughput{},
				SSESpecification:       &dynamodb.SSESpecification{},
				StreamSpecification:    &dynamodb.StreamSpecification{},
				Tags:                   []*dynamodb.Tag{},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
				LocalSecondaryIndexes:  []*dynamodb.LocalSecondaryIndex{},
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
				TableName:              aws.String("table"),
				BillingMode:            aws.String("PAY_PER_REQUEST"),
				ProvisionedThroughput:  &dynamodb.ProvisionedThroughput{},
				SSESpecification:       &dynamodb.SSESpecification{},
				StreamSpecification:    &dynamodb.StreamSpecification{},
				Tags:                   []*dynamodb.Tag{},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
				LocalSecondaryIndexes:  []*dynamodb.LocalSecondaryIndex{},
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
				SSESpecification:       &dynamodb.SSESpecification{},
				StreamSpecification:    &dynamodb.StreamSpecification{
					StreamEnabled: aws.Bool(true),
					StreamViewType: aws.String("KEYS_ONLY"),
				},
				Tags:                   []*dynamodb.Tag{},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
				LocalSecondaryIndexes:  []*dynamodb.LocalSecondaryIndex{},
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
				"LocalSecondaryIndexes.0.KeySchema.0.AttributeName=date",
				"LocalSecondaryIndexes.0.KeySchema.0.KeyType=range",
				"LocalSecondaryIndexes.0.KeySchema.1.AttributeName=userid",
				"LocalSecondaryIndexes.0.KeySchema.1.KeyType=hash",
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
				SSESpecification:       &dynamodb.SSESpecification{},
				StreamSpecification:    &dynamodb.StreamSpecification{},
				Tags:                   []*dynamodb.Tag{},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
				LocalSecondaryIndexes: []*dynamodb.LocalSecondaryIndex{
					{
						IndexName: aws.String("index"),
						KeySchema: []*dynamodb.KeySchemaElement{
							{AttributeName: aws.String("date"), KeyType: aws.String("range")},
							{AttributeName: aws.String("userid"), KeyType: aws.String("hash")},
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
				"LocalSecondaryIndexes.0.KeySchema.1.AttributeName=userid", // 1 before 0. array attrs must be specified in order
				"LocalSecondaryIndexes.0.KeySchema.1.KeyType=hash",
				"LocalSecondaryIndexes.0.KeySchema.0.AttributeName=date",
				"LocalSecondaryIndexes.0.KeySchema.0.KeyType=range",
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
				"GlobalSecondaryIndexes.0.KeySchema.0.AttributeName=userid",
				"GlobalSecondaryIndexes.0.KeySchema.0.KeyType=hash",
				"GlobalSecondaryIndexes.0.Projection.NonKeyAttributes.0=foo",
				"GlobalSecondaryIndexes.0.Projection.ProjectionType=keys_only",
				"GlobalSecondaryIndexes.1.IndexName=index2",
				"GlobalSecondaryIndexes.1.ProvisionedThroughput.ReadCapacityUnits=7",
				"GlobalSecondaryIndexes.1.ProvisionedThroughput.WriteCapacityUnits=7",
				"GlobalSecondaryIndexes.1.KeySchema.0.AttributeName=date",
				"GlobalSecondaryIndexes.1.KeySchema.0.KeyType=range",
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
				SSESpecification:    &dynamodb.SSESpecification{},
				StreamSpecification: &dynamodb.StreamSpecification{},
				Tags:                []*dynamodb.Tag{},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{
					{
						IndexName: aws.String("index"),
						ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
							ReadCapacityUnits: aws.Int64(5),
							WriteCapacityUnits: aws.Int64(5),
						},
						KeySchema: []*dynamodb.KeySchemaElement{
							{AttributeName: aws.String("userid"), KeyType: aws.String("hash")},
						},
						Projection: &dynamodb.Projection{
							NonKeyAttributes: []*string{aws.String("foo")},
							ProjectionType:   aws.String("KEYS_ONLY"),
						},
					},
					{
						IndexName: aws.String("index2"),
						ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
							ReadCapacityUnits: aws.Int64(7),
							WriteCapacityUnits: aws.Int64(7),
						},
						KeySchema: []*dynamodb.KeySchemaElement{
							{AttributeName: aws.String("date"), KeyType: aws.String("range")},
						},
						Projection: &dynamodb.Projection{
							NonKeyAttributes: []*string{aws.String("bar")},
							ProjectionType:   aws.String("KEYS_ONLY"),
						},
					},
				},
				LocalSecondaryIndexes: []*dynamodb.LocalSecondaryIndex{},
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
			[]string{"userid:s:hash"},
			[]string{
				"Tags.0.Key=foo",
				"Tags.0.Value=bar",
				"Tags.1.Key=asdf",
				"Tags.1.Value=123",
			},
			&dynamodb.CreateTableInput{
				TableName:              aws.String("table"),
				BillingMode:            aws.String("PAY_PER_REQUEST"),
				ProvisionedThroughput:  &dynamodb.ProvisionedThroughput{},
				SSESpecification:       &dynamodb.SSESpecification{},
				StreamSpecification:    &dynamodb.StreamSpecification{},
				Tags:                   []*dynamodb.Tag{
					{Key: aws.String("foo"), Value: aws.String("bar")},
					{Key: aws.String("asdf"), Value: aws.String("123")},
				},
				GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
				LocalSecondaryIndexes:  []*dynamodb.LocalSecondaryIndex{},
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

		//
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
