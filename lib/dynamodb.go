package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

var dynamoDBClient *dynamodb.DynamoDB
var dynamoDBClientLock sync.RWMutex

func DynamoDBClientExplicit(accessKeyID, accessKeySecret, region string) *dynamodb.DynamoDB {
	return dynamodb.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func DynamoDBClient() *dynamodb.DynamoDB {
	dynamoDBClientLock.Lock()
	defer dynamoDBClientLock.Unlock()
	if dynamoDBClient == nil {
		dynamoDBClient = dynamodb.New(Session())
	}
	return dynamoDBClient
}

func dynamoDBTableAttrShortcut(s string) string {
	s2, ok := map[string]string{
		"read":   "ProvisionedThroughput.ReadCapacityUnits",
		"write":  "ProvisionedThroughput.WriteCapacityUnits",
		"stream": "StreamSpecification.StreamViewType",
	}[s]
	if ok {
		return s2
	}
	return s
}

func DynamoDBEnsureInput(infraSetName, tableName string, keys []string, attrs []string) (*dynamodb.CreateTableInput, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBEnsureInput"}
		defer d.Log()
	}
	input := &dynamodb.CreateTableInput{
		TableName:        aws.String(tableName),
		BillingMode:      aws.String("PAY_PER_REQUEST"),
		SSESpecification: &dynamodb.SSESpecification{},
		StreamSpecification: &dynamodb.StreamSpecification{
			StreamEnabled: aws.Bool(false),
		},
		ProvisionedThroughput:  &dynamodb.ProvisionedThroughput{},
		LocalSecondaryIndexes:  []*dynamodb.LocalSecondaryIndex{},
		GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
		Tags: []*dynamodb.Tag{{
			Key:   aws.String(infraSetTagName),
			Value: aws.String(infraSetName),
		}},
	}
	// unpack keys like "name:s:hash" and "date:n:range"
	for _, key := range keys {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			err := fmt.Errorf("keys must be in format: 'Name:AttrType:KeyType', got: %s", key)
			Logger.Println("error:", err)
			return nil, err
		}
		attrName, attrType, keyType := parts[0], parts[1], parts[2]
		input.KeySchema = append(input.KeySchema, &dynamodb.KeySchemaElement{
			AttributeName: aws.String(attrName),
			KeyType:       aws.String(strings.ToUpper(keyType)),
		})
		input.AttributeDefinitions = append(input.AttributeDefinitions, &dynamodb.AttributeDefinition{
			AttributeName: aws.String(attrName),
			AttributeType: aws.String(strings.ToUpper(attrType)),
		})
	}
	// unpack attrs
	for _, line := range attrs {
		attr, value, err := SplitOnce(line, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		attr = dynamoDBTableAttrShortcut(attr)
		head, tail, err := SplitOnce(attr, ".")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch head {
		case "BillingMode":
			err := fmt.Errorf("BillingMode is implied by the existence of provisioned throughput attrs: %s", line)
			Logger.Println("error:", err)
			return nil, err
		case "SSESpecification":
			switch tail {
			case "Enabled":
				err := fmt.Errorf("SSESpecification.Enabled is implied by the existance of SSESpecification attrs: %s", line)
				Logger.Println("error:", err)
				return nil, err
			case "KMSMasterKeyId":
				input.SSESpecification.Enabled = aws.Bool(true)
				input.SSESpecification.KMSMasterKeyId = aws.String(value)
				input.SSESpecification.SSEType = aws.String("KMS")
			case "SSEType":
				err := fmt.Errorf("SSESpecification.SSEType has only one value: %s", line)
				Logger.Println("error:", err)
				return nil, err
			default:
				err := fmt.Errorf("unknown dynamodb attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "ProvisionedThroughput":
			switch tail {
			case "ReadCapacityUnits":
				input.BillingMode = aws.String("PROVISIONED")
				units, err := strconv.Atoi(value)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				input.ProvisionedThroughput.ReadCapacityUnits = aws.Int64(int64(units))
			case "WriteCapacityUnits":
				input.BillingMode = aws.String("PROVISIONED")
				units, err := strconv.Atoi(value)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				input.ProvisionedThroughput.WriteCapacityUnits = aws.Int64(int64(units))
			default:
				err := fmt.Errorf("unknown dynamodb attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "StreamSpecification":
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			switch tail {
			case "StreamViewType":
				input.StreamSpecification.StreamEnabled = aws.Bool(true)
				input.StreamSpecification.StreamViewType = aws.String(strings.ToUpper(value))
			default:
				err := fmt.Errorf("unknown dynamodb attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "LocalSecondaryIndexes":
			head, tail, err := SplitOnce(tail, ".")
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			i, err := strconv.Atoi(head)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			switch len(input.LocalSecondaryIndexes) {
			case i:
				input.LocalSecondaryIndexes = append(
					input.LocalSecondaryIndexes,
					&dynamodb.LocalSecondaryIndex{Projection: &dynamodb.Projection{}},
				)
			case i + 1:
			default:
				err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
			switch tail {
			case "IndexName":
				input.LocalSecondaryIndexes[i].IndexName = aws.String(value)
			default:
				head, tail, err = SplitOnce(tail, ".")
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				switch head {
				case "Key":
					j, err := strconv.Atoi(tail)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					switch len(input.LocalSecondaryIndexes[i].KeySchema) {
					case j:
						parts := strings.SplitN(value, ":", 3)
						if len(parts) != 3 {
							err := fmt.Errorf("keys must be in format: 'Name:AttrType:KeyType', got: %s", value)
							Logger.Println("error:", err)
							return nil, err
						}
						attrName, attrType, keyType := parts[0], parts[1], parts[2]
						input.LocalSecondaryIndexes[i].KeySchema = append(
							input.LocalSecondaryIndexes[i].KeySchema,
							&dynamodb.KeySchemaElement{
								AttributeName: aws.String(attrName),
								KeyType:       aws.String(strings.ToUpper(keyType)),
							},
						)
						exists := false
						for _, attr := range input.AttributeDefinitions {
							if *attr.AttributeName == attrName {
								exists = true
								if *attr.AttributeType != strings.ToUpper(attrType) {
									return nil, fmt.Errorf("GlobalIndex attrType didn't equal existing with same name: %s, %s != %s", *attr.AttributeName, *attr.AttributeType, strings.ToUpper(attrType))
								}
								break
							}
						}
						if !exists {
							input.AttributeDefinitions = append(input.AttributeDefinitions, &dynamodb.AttributeDefinition{
								AttributeName: aws.String(attrName),
								AttributeType: aws.String(strings.ToUpper(attrType)),
							})
						}
					default:
						err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
				case "Projection":
					switch tail {
					case "ProjectionType":
						input.LocalSecondaryIndexes[i].Projection.ProjectionType = aws.String(strings.ToUpper(value))
					default:
						head, tail, err = SplitOnce(tail, ".")
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
						switch head {
						case "NonKeyAttributes":
							j, err := strconv.Atoi(tail)
							if err != nil {
								Logger.Println("error:", err)
								return nil, err
							}
							switch len(input.LocalSecondaryIndexes[i].Projection.NonKeyAttributes) {
							case j:
								input.LocalSecondaryIndexes[i].Projection.NonKeyAttributes = append(
									input.LocalSecondaryIndexes[i].Projection.NonKeyAttributes,
									aws.String(value),
								)
							default:
								err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
								Logger.Println("error:", err)
								return nil, err
							}
						default:
							err := fmt.Errorf("unknown dynamodb attr: %s", line)
							Logger.Println("error:", err)
							return nil, err
						}
					}
				default:
					err := fmt.Errorf("unknown dynamodb attr: %s", line)
					Logger.Println("error:", err)
					return nil, err
				}
			}
		case "GlobalSecondaryIndexes":
			head, tail, err := SplitOnce(tail, ".")
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			i, err := strconv.Atoi(head)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			switch len(input.GlobalSecondaryIndexes) {
			case i:
				input.GlobalSecondaryIndexes = append(
					input.GlobalSecondaryIndexes,
					&dynamodb.GlobalSecondaryIndex{
						Projection:            &dynamodb.Projection{},
						ProvisionedThroughput: &dynamodb.ProvisionedThroughput{},
					},
				)
			case i + 1:
			default:
				err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
			switch tail {
			case "IndexName":
				input.GlobalSecondaryIndexes[i].IndexName = aws.String(value)
			default:
				head, tail, err = SplitOnce(tail, ".")
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				switch head {
				case "Key":
					j, err := strconv.Atoi(tail)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					switch len(input.GlobalSecondaryIndexes[i].KeySchema) {
					case j:
						parts := strings.SplitN(value, ":", 3)
						if len(parts) != 3 {
							err := fmt.Errorf("keys must be in format: 'Name:AttrType:KeyType', got: %s", value)
							Logger.Println("error:", err)
							return nil, err
						}
						attrName, attrType, keyType := parts[0], parts[1], parts[2]
						input.GlobalSecondaryIndexes[i].KeySchema = append(
							input.GlobalSecondaryIndexes[i].KeySchema,
							&dynamodb.KeySchemaElement{
								AttributeName: aws.String(attrName),
								KeyType:       aws.String(strings.ToUpper(keyType)),
							},
						)
						exists := false
						for _, attr := range input.AttributeDefinitions {
							if *attr.AttributeName == attrName {
								exists = true
								if *attr.AttributeType != strings.ToUpper(attrType) {
									return nil, fmt.Errorf("LocalIndex attrType didn't equal existing with same name: %s, %s != %s", *attr.AttributeName, *attr.AttributeType, strings.ToUpper(attrType))
								}
								break
							}
						}
						if !exists {
							input.AttributeDefinitions = append(input.AttributeDefinitions, &dynamodb.AttributeDefinition{
								AttributeName: aws.String(attrName),
								AttributeType: aws.String(strings.ToUpper(attrType)),
							})
						}
					default:
						err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
				case "ProvisionedThroughput":
					switch tail {
					case "ReadCapacityUnits":
						units, err := strconv.Atoi(value)
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
						input.GlobalSecondaryIndexes[i].ProvisionedThroughput.ReadCapacityUnits = aws.Int64(int64(units))
					case "WriteCapacityUnits":
						units, err := strconv.Atoi(value)
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
						input.GlobalSecondaryIndexes[i].ProvisionedThroughput.WriteCapacityUnits = aws.Int64(int64(units))
					default:
						err := fmt.Errorf("unknown dynamodb attr: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
				case "Projection":
					switch tail {
					case "ProjectionType":
						input.GlobalSecondaryIndexes[i].Projection.ProjectionType = aws.String(strings.ToUpper(value))
					default:
						head, tail, err = SplitOnce(tail, ".")
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
						switch head {
						case "NonKeyAttributes":
							j, err := strconv.Atoi(tail)
							if err != nil {
								Logger.Println("error:", err)
								return nil, err
							}
							switch len(input.GlobalSecondaryIndexes[i].Projection.NonKeyAttributes) {
							case j:
								input.GlobalSecondaryIndexes[i].Projection.NonKeyAttributes = append(
									input.GlobalSecondaryIndexes[i].Projection.NonKeyAttributes,
									aws.String(value),
								)
							default:
								err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
								Logger.Println("error:", err)
								return nil, err
							}
						default:
							err := fmt.Errorf("unknown dynamodb attr: %s", line)
							Logger.Println("error:", err)
							return nil, err
						}
					}
				default:
					err := fmt.Errorf("unknown dynamodb attr: %s", line)
					Logger.Println("error:", err)
					return nil, err
				}
			}
		case "Tags":
			input.Tags = append(input.Tags, &dynamodb.Tag{
				Key:   aws.String(tail),
				Value: aws.String(value),
			})
		default:
			err := fmt.Errorf("unknown dynamodb attr: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	if len(input.LocalSecondaryIndexes) == 0 {
		input.LocalSecondaryIndexes = nil
	} else {
		for _, index := range input.LocalSecondaryIndexes {
			if len(index.Projection.NonKeyAttributes) == 0 && index.Projection.ProjectionType == nil {
				index.Projection = nil
			}
		}
	}
	if len(input.GlobalSecondaryIndexes) == 0 {
		input.GlobalSecondaryIndexes = nil
	} else {
		for _, index := range input.GlobalSecondaryIndexes {
			if index.ProvisionedThroughput.ReadCapacityUnits == nil && index.ProvisionedThroughput.WriteCapacityUnits == nil {
				index.ProvisionedThroughput = nil
			}
		}
	}
	if len(input.Tags) == 0 {
		input.Tags = nil
	}
	if input.ProvisionedThroughput.ReadCapacityUnits == nil &&
		input.ProvisionedThroughput.WriteCapacityUnits == nil {
		input.ProvisionedThroughput = nil
	}
	if input.SSESpecification.Enabled == nil &&
		input.SSESpecification.KMSMasterKeyId == nil &&
		input.SSESpecification.SSEType == nil {
		input.SSESpecification = nil
	}
	return input, nil
}

func DynamoDBEnsure(ctx context.Context, input *dynamodb.CreateTableInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBEnsure"}
		defer d.Log()
	}
	table, err := DynamoDBClient().DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
		TableName: input.TableName,
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != dynamodb.ErrCodeResourceNotFoundException {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err = DynamoDBClient().CreateTableWithContext(ctx, input)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created table:", *input.TableName)
		return nil
	}
	if !reflect.DeepEqual(input.KeySchema, table.Table.KeySchema) {
		err := fmt.Errorf("KeySchema can only be set at table creation time for: %s", *input.TableName)
		Logger.Println("error:", err)
		return err
	}
	needsUpdate := false
	update := &dynamodb.UpdateTableInput{
		TableName:                   input.TableName,
		BillingMode:                 nil,
		ProvisionedThroughput:       &dynamodb.ProvisionedThroughput{},
		SSESpecification:            nil, // TODO update see
		StreamSpecification:         &dynamodb.StreamSpecification{},
		AttributeDefinitions:        []*dynamodb.AttributeDefinition{},
		GlobalSecondaryIndexUpdates: []*dynamodb.GlobalSecondaryIndexUpdate{},
	}
	// dynamo treats these as unsorted set
	sort.Slice(table.Table.AttributeDefinitions, func(i, j int) bool {
		return *table.Table.AttributeDefinitions[i].AttributeName < *table.Table.AttributeDefinitions[j].AttributeName
	})
	// dynamo treats these as unsorted set
	sort.Slice(input.AttributeDefinitions, func(i, j int) bool {
		return *input.AttributeDefinitions[i].AttributeName < *input.AttributeDefinitions[j].AttributeName
	})
	if !reflect.DeepEqual(table.Table.AttributeDefinitions, input.AttributeDefinitions) {
		needsUpdate = true
		update.AttributeDefinitions = input.AttributeDefinitions
	}
	existingThroughputNil := table.Table.ProvisionedThroughput == nil && input.ProvisionedThroughput != nil
	readNotEqual := table.Table.ProvisionedThroughput != nil &&
		input.ProvisionedThroughput != nil &&
		*table.Table.ProvisionedThroughput.ReadCapacityUnits != *input.ProvisionedThroughput.ReadCapacityUnits
	if existingThroughputNil || readNotEqual {
		needsUpdate = true
		update.ProvisionedThroughput.ReadCapacityUnits = input.ProvisionedThroughput.ReadCapacityUnits
		old := int64(0)
		if !existingThroughputNil {
			old = *table.Table.ProvisionedThroughput.ReadCapacityUnits
		}
		Logger.Printf(
			PreviewString(preview)+"will update ProvisionedThroughput.ReadCapacityUnits for table %s: %d => %d\n",
			*input.TableName,
			old,
			*input.ProvisionedThroughput.ReadCapacityUnits,
		)
	}
	writeNotEqual := table.Table.ProvisionedThroughput != nil &&
		input.ProvisionedThroughput != nil &&
		*table.Table.ProvisionedThroughput.WriteCapacityUnits != *input.ProvisionedThroughput.WriteCapacityUnits
	if existingThroughputNil || writeNotEqual {
		needsUpdate = true
		update.ProvisionedThroughput.WriteCapacityUnits = input.ProvisionedThroughput.WriteCapacityUnits
		old := int64(0)
		if !existingThroughputNil {
			old = *table.Table.ProvisionedThroughput.WriteCapacityUnits
		}
		Logger.Printf(
			PreviewString(preview)+"will update ProvisionedThroughput.WriteCapacityUnits for table %s: %d => %d\n",
			*input.TableName,
			old,
			*input.ProvisionedThroughput.WriteCapacityUnits,
		)
	}
	existingStreamNil := table.Table.StreamSpecification == nil &&
		input.StreamSpecification != nil &&
		*input.StreamSpecification.StreamEnabled
	streamEnabledNotEqual := table.Table.StreamSpecification != nil &&
		input.StreamSpecification != nil &&
		*table.Table.StreamSpecification.StreamEnabled != *input.StreamSpecification.StreamEnabled
	if existingStreamNil || streamEnabledNotEqual {
		needsUpdate = true
		update.StreamSpecification.StreamEnabled = input.StreamSpecification.StreamEnabled
		old := false
		if !existingStreamNil {
			old = *table.Table.StreamSpecification.StreamEnabled
		}
		Logger.Printf(
			PreviewString(preview)+"will update StreamSpecification.StreamEnabled for table %s: %t => %t\n",
			*input.TableName,
			old,
			*input.StreamSpecification.StreamEnabled,
		)
	}
	streamViewTypeNotEqual := table.Table.StreamSpecification != nil &&
		input.StreamSpecification != nil &&
		*input.StreamSpecification.StreamEnabled &&
		*table.Table.StreamSpecification.StreamViewType != *input.StreamSpecification.StreamViewType
	if streamViewTypeNotEqual && *input.StreamSpecification.StreamEnabled {
		err := fmt.Errorf("stream type cannot be changed, instead disable it, then enable with a new type")
		Logger.Println("error:", err)
		return err
	}
	if existingStreamNil && *input.StreamSpecification.StreamEnabled {
		needsUpdate = true
		update.StreamSpecification.StreamViewType = input.StreamSpecification.StreamViewType
		old := ""
		if !existingStreamNil {
			old = *table.Table.StreamSpecification.StreamViewType
		}
		Logger.Printf(
			PreviewString(preview)+"will update StreamSpecification.StreamViewType for table %s: %s => %s\n",
			*input.TableName,
			old,
			*input.StreamSpecification.StreamViewType,
		)
	}
	existingLocalIndices := make(map[string]*dynamodb.LocalSecondaryIndexDescription)
	for _, index := range table.Table.LocalSecondaryIndexes {
		existingLocalIndices[*index.IndexName] = index
	}
	for _, index := range input.LocalSecondaryIndexes {
		existing, ok := existingLocalIndices[*index.IndexName]
		if !ok {
			err := fmt.Errorf("LocalSecondaryIndices can only be set at table creation time for: %s", *input.TableName)
			Logger.Println("error:", err)
			return err
		}
		if *existing.Projection.ProjectionType != *index.Projection.ProjectionType {
			err := fmt.Errorf("ProjectionType not updated. LocalSecondaryIndices can only be set at table creation time for: %s", *input.TableName)
			Logger.Println("error:", err)
			return err
		}
		if len(existing.Projection.NonKeyAttributes) != len(index.Projection.NonKeyAttributes) {
			err := fmt.Errorf("NonKeyAttributes not updated. LocalSecondaryIndices can only be set at table creation time for %s", *input.TableName)
			Logger.Println("error:", err)
			return err
		}
		attrs := make(map[string]interface{})
		for _, attr := range existing.Projection.NonKeyAttributes {
			attrs[*attr] = nil
		}
		for _, attr := range index.Projection.NonKeyAttributes {
			_, ok := attrs[*attr]
			if !ok {
				err := fmt.Errorf("NonKeyAttributes not updated. LocalSecondaryIndices can only be set at table creation time for: %s", *input.TableName)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	updateLocalIndices := make(map[string]interface{})
	for _, index := range input.LocalSecondaryIndexes {
		updateLocalIndices[*index.IndexName] = nil
	}
	for _, index := range table.Table.LocalSecondaryIndexes {
		_, ok := updateLocalIndices[*index.IndexName]
		if !ok {
			err := fmt.Errorf("local secondary indices cannot be deleted: %s", *index.IndexName)
			Logger.Println("error:", err)
			return err
		}
	}
	existingGlobalIndices := make(map[string]*dynamodb.GlobalSecondaryIndexDescription)
	for _, index := range table.Table.GlobalSecondaryIndexes {
		existingGlobalIndices[*index.IndexName] = index
	}
	for _, index := range input.GlobalSecondaryIndexes {
		existing, ok := existingGlobalIndices[*index.IndexName]
		if !ok {
			update.GlobalSecondaryIndexUpdates = append(
				update.GlobalSecondaryIndexUpdates,
				&dynamodb.GlobalSecondaryIndexUpdate{
					Create: &dynamodb.CreateGlobalSecondaryIndexAction{
						IndexName:             index.IndexName,
						KeySchema:             index.KeySchema,
						Projection:            index.Projection,
						ProvisionedThroughput: index.ProvisionedThroughput,
					},
				},
			)
		} else {
			if *existing.Projection.ProjectionType != *index.Projection.ProjectionType {
				err := fmt.Errorf("ProjectionType not updated. this GlobalSecondaryIndex attr can only be set at index creation time for: %s", *input.TableName)
				Logger.Println("error:", err)
				return err
			}
			if len(existing.Projection.NonKeyAttributes) != len(index.Projection.NonKeyAttributes) {
				err := fmt.Errorf("NonKeyAttributes not updated. this GlobalSecondaryIndex attr can only be set at index creation time for: %s", *input.TableName)
				Logger.Println("error:", err)
				return err
			}
			attrs := make(map[string]interface{})
			for _, attr := range existing.Projection.NonKeyAttributes {
				attrs[*attr] = nil
			}
			for _, attr := range index.Projection.NonKeyAttributes {
				_, ok := attrs[*attr]
				if !ok {
					err := fmt.Errorf("NonKeyAttributes not updated. this GlobalSecondaryIndex attr can only be set at index creation time for: %s", *input.TableName)
					Logger.Println("error:", err)
					return err
				}
			}
			updateIndex := false
			if index.ProvisionedThroughput != nil && *existing.ProvisionedThroughput.ReadCapacityUnits != *index.ProvisionedThroughput.ReadCapacityUnits {
				updateIndex = true
				Logger.Printf(
					PreviewString(preview)+"will update GlobalSecondaryIndex %s ProvisionedThroughput.ReadCapacityUnits for table %s: %d => %d\n",
					*index.IndexName,
					*input.TableName,
					*existing.ProvisionedThroughput.ReadCapacityUnits,
					*index.ProvisionedThroughput.ReadCapacityUnits,
				)
			}
			if !reflect.DeepEqual(existing.KeySchema, index.KeySchema) {
				err := fmt.Errorf("KeySchema not updated. this GlobalSecondaryIndex attr can only be set at index creation time: %s", *input.TableName)
				Logger.Println("error:", err)
				return err
			}
			if index.ProvisionedThroughput != nil && *existing.ProvisionedThroughput.WriteCapacityUnits != *index.ProvisionedThroughput.WriteCapacityUnits {
				updateIndex = true
				Logger.Printf(
					PreviewString(preview)+"will update GlobalSecondaryIndex %s ProvisionedThroughput.WriteCapacityUnits for table %s: %d => %d\n",
					*index.IndexName,
					*input.TableName,
					*existing.ProvisionedThroughput.WriteCapacityUnits,
					*index.ProvisionedThroughput.WriteCapacityUnits,
				)
			}
			if updateIndex {
				update.GlobalSecondaryIndexUpdates = append(
					update.GlobalSecondaryIndexUpdates,
					&dynamodb.GlobalSecondaryIndexUpdate{
						Update: &dynamodb.UpdateGlobalSecondaryIndexAction{
							IndexName:             index.IndexName,
							ProvisionedThroughput: index.ProvisionedThroughput,
						},
					},
				)
			}
		}
	}
	updateGlobalIndices := make(map[string]interface{})
	for _, index := range input.GlobalSecondaryIndexes {
		updateGlobalIndices[*index.IndexName] = nil
	}
	for _, index := range table.Table.GlobalSecondaryIndexes {
		_, ok := updateGlobalIndices[*index.IndexName]
		if !ok {
			update.GlobalSecondaryIndexUpdates = append(update.GlobalSecondaryIndexUpdates, &dynamodb.GlobalSecondaryIndexUpdate{
				Delete: &dynamodb.DeleteGlobalSecondaryIndexAction{
					IndexName: index.IndexName,
				},
			})
			Logger.Println(PreviewString(preview)+"deleted global index:", *index.IndexName)
		}
	}
	if len(update.GlobalSecondaryIndexUpdates) == 0 {
		update.GlobalSecondaryIndexUpdates = nil
	} else {
		needsUpdate = true
	}
	if update.StreamSpecification.StreamEnabled == nil && update.StreamSpecification.StreamViewType == nil {
		update.StreamSpecification = nil
	}
	if update.ProvisionedThroughput.ReadCapacityUnits == nil && update.ProvisionedThroughput.WriteCapacityUnits == nil {
		update.ProvisionedThroughput = nil
	} else {
		readChanged := *table.Table.ProvisionedThroughput.ReadCapacityUnits != *update.ProvisionedThroughput.ReadCapacityUnits
		writeChanged := *table.Table.ProvisionedThroughput.WriteCapacityUnits != *update.ProvisionedThroughput.WriteCapacityUnits
		if !readChanged && !writeChanged {
			update.ProvisionedThroughput = nil
		}
	}
	if needsUpdate {
		if !preview {
			if len(update.GlobalSecondaryIndexUpdates) > 1 {
				// index updates must be applied one at a time when table is ready
				indexUpdates := update.GlobalSecondaryIndexUpdates
				for _, indexUpdate := range indexUpdates {
					update.GlobalSecondaryIndexUpdates = []*dynamodb.GlobalSecondaryIndexUpdate{indexUpdate}
					err := DynamoDBWaitForReady(ctx, *update.TableName)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					_, err = DynamoDBClient().UpdateTableWithContext(ctx, update)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
			} else {
				_, err = DynamoDBClient().UpdateTableWithContext(ctx, update)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
		}
		Logger.Println(PreviewString(preview)+"updated table:", *update.TableName, DropLinesWithAny(PformatAlways(update), "null"))
	}
	arn, err := DynamoDBArn(ctx, *update.TableName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	tags, err := DynamoDBListTags(ctx, *update.TableName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	tagInput := &dynamodb.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        []*dynamodb.Tag{},
	}
	existingTags := make(map[string]string)
	for _, tag := range tags {
		existingTags[*tag.Key] = *tag.Value
	}
	for _, tag := range input.Tags {
		val, ok := existingTags[*tag.Key]
		if !ok || val != *tag.Value {
			tagInput.Tags = append(tagInput.Tags, tag)
			Logger.Printf(
				PreviewString(preview)+"will update tag %s for table %s: %s => %s\n",
				*tag.Key,
				*input.TableName,
				val,
				*tag.Value,
			)
		}
	}
	if len(tagInput.Tags) > 0 {
		if !preview {
			_, err = DynamoDBClient().TagResourceWithContext(ctx, tagInput)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"updated tags for table:", *input.TableName)
	}
	untagInput := &dynamodb.UntagResourceInput{
		ResourceArn: aws.String(arn),
		TagKeys:     []*string{},
	}
	updateTags := make(map[string]interface{})
	for _, tag := range input.Tags {
		updateTags[*tag.Key] = nil
	}
	for _, tag := range tags {
		_, ok := updateTags[*tag.Key]
		if !ok {
			Logger.Printf("remove tag %s for table %s\n", *tag.Key, *input.TableName)
			untagInput.TagKeys = append(untagInput.TagKeys, tag.Key)
		}
	}
	if len(untagInput.TagKeys) > 0 {
		if !preview {
			_, err = dynamoDBClient.UntagResourceWithContext(ctx, untagInput)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"removed tags for table:", *input.TableName)
	}
	return nil
}

func DynamoDBArn(ctx context.Context, tableName string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	arn := fmt.Sprintf(
		"arn:aws:dynamodb:%s:%s:table/%s",
		Region(),
		account,
		tableName,
	)
	return arn, nil
}

func DynamoDBListTags(ctx context.Context, tableName string) ([]*dynamodb.Tag, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBListTags"}
		defer d.Log()
	}
	arn, err := DynamoDBArn(ctx, tableName)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	var tags []*dynamodb.Tag
	var nextToken *string
	for {
		out, err := DynamoDBClient().ListTagsOfResourceWithContext(ctx, &dynamodb.ListTagsOfResourceInput{
			ResourceArn: aws.String(arn),
			NextToken:   nextToken,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		tags = append(tags, out.Tags...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return tags, nil
}

func DynamoDBListTables(ctx context.Context) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBListTables"}
		defer d.Log()
	}
	var start *string
	var tables []string
	for {
		out, err := DynamoDBClient().ListTablesWithContext(ctx, &dynamodb.ListTablesInput{ExclusiveStartTableName: start})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, name := range out.TableNames {
			tables = append(tables, *name)
		}
		if out.LastEvaluatedTableName == nil {
			break
		}
		start = out.LastEvaluatedTableName
	}
	return tables, nil
}

func DynamoDBDeleteTable(ctx context.Context, tableName string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBDeleteTable"}
		defer d.Log()
	}
	err := DynamoDBWaitForReady(ctx, tableName)
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != dynamodb.ErrCodeResourceNotFoundException {
			return err
		}
		return nil
	}
	if !preview {
		_, err = DynamoDBClient().DeleteTableWithContext(ctx, &dynamodb.DeleteTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = DynamoDBWaitForGone(ctx, tableName)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted table:", tableName)
	return nil
}

func DynamoDBWaitForGone(ctx context.Context, tableName string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBWaitForGone"}
		defer d.Log()
	}
	for {
		_, err := DynamoDBClient().DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if ok && aerr.Code() == dynamodb.ErrCodeResourceNotFoundException {
				return nil
			}
			return err
		}
		Logger.Println("waiting for table to be gone:", tableName)
		time.Sleep(2 * time.Second)
	}
}

func DynamoDBWaitForReady(ctx context.Context, tableName string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBWaitForReady"}
		defer d.Log()
	}
	for {
		description, err := DynamoDBClient().DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			return err
		}
		ready := *description.Table.TableStatus == dynamodb.TableStatusActive
		if !ready {
			Logger.Println("waiting for table active:", tableName)
		} else {
			for _, index := range description.Table.GlobalSecondaryIndexes {
				if *index.IndexStatus != dynamodb.IndexStatusActive {
					Logger.Println("waiting for table index:", *index.IndexName)
					ready = false
					break
				}
			}
		}
		if ready {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

func DynamoDBStreamArnToTableName(arn string) string {
	// arn:aws:dynamodb:region:accountID:table/TableName/stream/timestamp
	return strings.Split(Last(strings.SplitN(arn, ":", 6)), "/")[1]
}

func DynamoDBStreamArn(ctx context.Context, tableName string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBStreamArn"}
		defer d.Log()
	}
	var expectedErr error
	var streamArn string
	err := Retry(ctx, func() error {
		out, err := DynamoDBClient().DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != dynamodb.ErrCodeResourceNotFoundException {
				return err
			}
			expectedErr = err
			return nil
		}
		streamArn = *out.Table.LatestStreamArn
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if expectedErr != nil {
		return "", expectedErr
	}
	return streamArn, nil
}

func DynamoDBItemDeleteAll(ctx context.Context, tableName string, keyNames []string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "DynamoDBItemDeleteAll"}
		defer d.Log()
	}
	var start map[string]*dynamodb.AttributeValue
	for {
		out, err := DynamoDBClient().ScanWithContext(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(tableName),
			ExclusiveStartKey: start,
			Limit:             aws.Int64(25),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		reqs := []*dynamodb.WriteRequest{}
		for _, item := range out.Items {
			key := map[string]*dynamodb.AttributeValue{}
			for _, k := range keyNames {
				key[k] = item[k]
			}
			reqs = append(reqs, &dynamodb.WriteRequest{
				DeleteRequest: &dynamodb.DeleteRequest{Key: key},
			})
		}
		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]*dynamodb.WriteRequest{tableName: reqs},
		}
		_, err = DynamoDBClient().BatchWriteItemWithContext(ctx, input)
		if err != nil {
			if !strings.Contains(err.Error(), "[Member must have length less than or equal to 25, Member must have length greater than or equal to 1]") { // table already empty
				Logger.Println("error:", err)
				return err
			}
		}
		for _, req := range reqs {
			val := make(map[string]interface{})
			err := dynamodbattribute.UnmarshalMap(req.DeleteRequest.Key, &val)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			bytes, err := json.Marshal(val)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			fmt.Println("dynamodb deleted:", tableName, string(bytes))
		}
		if out.LastEvaluatedKey == nil {
			break
		}
		start = out.LastEvaluatedKey
	}
	return nil
}
