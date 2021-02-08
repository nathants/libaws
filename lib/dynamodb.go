package lib

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
)

var dynamoDBClient *dynamodb.DynamoDB
var dynamoDBClientLock sync.RWMutex

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

func splitOnce(s string, sep string) (head, tail string, err error) {
	parts := strings.SplitN(s, sep, 2)
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("cannot attrSplitOnce: %s", s)
}

func DynamoDBEnsureInput(name string, keys []string, attrs []string) (*dynamodb.CreateTableInput, error) {

	input := &dynamodb.CreateTableInput{
		TableName:              aws.String(name),
		BillingMode:            aws.String("PAY_PER_REQUEST"),
		SSESpecification:       &dynamodb.SSESpecification{},
		StreamSpecification:    &dynamodb.StreamSpecification{},
		ProvisionedThroughput:  &dynamodb.ProvisionedThroughput{},
		LocalSecondaryIndexes:  []*dynamodb.LocalSecondaryIndex{},
		GlobalSecondaryIndexes: []*dynamodb.GlobalSecondaryIndex{},
		Tags:                   []*dynamodb.Tag{},
	}

	// unpack keys like "name:s:hash" and "date:n:range"
	for _, key := range keys {
		parts := strings.SplitN(key, ":", 3)
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
		attr, value, err := splitOnce(line, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		attr = dynamoDBTableAttrShortcut(attr)
		head, tail, err := splitOnce(attr, ".")
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
				err := fmt.Errorf("unknown attr: %s", line)
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
				err := fmt.Errorf("unknown attr: %s", line)
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
				err := fmt.Errorf("unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}

		case "LocalSecondaryIndexes":
			head, tail, err := splitOnce(tail, ".")
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
				input.LocalSecondaryIndexes = append(input.LocalSecondaryIndexes, &dynamodb.LocalSecondaryIndex{Projection: &dynamodb.Projection{}})
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
				head, tail, err = splitOnce(tail, ".")
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				switch head {
				case "KeySchema":
					head, tail, err = splitOnce(tail, ".")
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					j, err := strconv.Atoi(head)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					switch len(input.LocalSecondaryIndexes[i].KeySchema) {
					case j:
						input.LocalSecondaryIndexes[i].KeySchema = append(input.LocalSecondaryIndexes[i].KeySchema, &dynamodb.KeySchemaElement{})
					case j + 1:
					default:
						err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
					switch tail {
					case "AttributeName":
						input.LocalSecondaryIndexes[i].KeySchema[j].AttributeName = aws.String(value)
					case "KeyType":
						input.LocalSecondaryIndexes[i].KeySchema[j].KeyType = aws.String(value)
					default:
						err := fmt.Errorf("unknown attr: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
				case "Projection":
					switch tail {
					case "ProjectionType":
						input.LocalSecondaryIndexes[i].Projection.ProjectionType = aws.String(strings.ToUpper(value))
					default:
						head, tail, err = splitOnce(tail, ".")
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
								input.LocalSecondaryIndexes[i].Projection.NonKeyAttributes = append(input.LocalSecondaryIndexes[i].Projection.NonKeyAttributes, aws.String(value))
							default:
								err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
								Logger.Println("error:", err)
								return nil, err
							}
						default:
							err := fmt.Errorf("unknown attr: %s", line)
							Logger.Println("error:", err)
							return nil, err
						}
					}
				default:
					err := fmt.Errorf("unknown attr: %s", line)
					Logger.Println("error:", err)
					return nil, err

				}
			}

		case "GlobalSecondaryIndexes":
			head, tail, err := splitOnce(tail, ".")
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
				input.GlobalSecondaryIndexes = append(input.GlobalSecondaryIndexes, &dynamodb.GlobalSecondaryIndex{Projection: &dynamodb.Projection{}, ProvisionedThroughput: &dynamodb.ProvisionedThroughput{}})
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
				head, tail, err = splitOnce(tail, ".")
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				switch head {
				case "KeySchema":
					head, tail, err = splitOnce(tail, ".")
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					j, err := strconv.Atoi(head)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					switch len(input.GlobalSecondaryIndexes[i].KeySchema) {
					case j:
						input.GlobalSecondaryIndexes[i].KeySchema = append(input.GlobalSecondaryIndexes[i].KeySchema, &dynamodb.KeySchemaElement{})
					case j + 1:
					default:
						err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
					switch tail {
					case "AttributeName":
						input.GlobalSecondaryIndexes[i].KeySchema[j].AttributeName = aws.String(value)
					case "KeyType":
						input.GlobalSecondaryIndexes[i].KeySchema[j].KeyType = aws.String(value)
					default:
						err := fmt.Errorf("unknown attr: %s", line)
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
						err := fmt.Errorf("unknown attr: %s", line)
						Logger.Println("error:", err)
						return nil, err
					}
				case "Projection":
					switch tail {
					case "ProjectionType":
						input.GlobalSecondaryIndexes[i].Projection.ProjectionType = aws.String(strings.ToUpper(value))
					default:
						head, tail, err = splitOnce(tail, ".")
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
								input.GlobalSecondaryIndexes[i].Projection.NonKeyAttributes = append(input.GlobalSecondaryIndexes[i].Projection.NonKeyAttributes, aws.String(value))
							default:
								err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
								Logger.Println("error:", err)
								return nil, err
							}
						default:
							err := fmt.Errorf("unknown attr: %s", line)
							Logger.Println("error:", err)
							return nil, err
						}
					}
				default:
					err := fmt.Errorf("unknown attr: %s", line)
					Logger.Println("error:", err)
					return nil, err
				}
			}

		case "Tags":
			head, tail, err := splitOnce(tail, ".")
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			i, err := strconv.Atoi(head)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			switch len(input.Tags) {
			case i:
				input.Tags = append(input.Tags, &dynamodb.Tag{})
			case i + 1:
			default:
				err := fmt.Errorf("attrs with indices must be in ascending order: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
			switch tail {
			case "Key":
				input.Tags[i].Key = aws.String(value)
			case "Value":
				input.Tags[i].Value = aws.String(value)
			default:
				err := fmt.Errorf("unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}

		default:
			err := fmt.Errorf("unknown attr: %s", line)
			Logger.Println("error:", err)
			return nil, err

		}

	}

	return input, nil
}

func DynamoDBEnsureTable(ctx context.Context, input *dynamodb.CreateTableInput) error {

	_, err := DynamoDBClient().CreateTableWithContext(ctx, input)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}

	_, err = DynamoDBClient().UpdateTableWithContext(ctx, &dynamodb.UpdateTableInput{
		TableName:                   aws.String(""),
		BillingMode:                 aws.String(""),
		ProvisionedThroughput:       &dynamodb.ProvisionedThroughput{},
		SSESpecification:            &dynamodb.SSESpecification{},
		StreamSpecification:         &dynamodb.StreamSpecification{},
		AttributeDefinitions:        []*dynamodb.AttributeDefinition{},
		GlobalSecondaryIndexUpdates: []*dynamodb.GlobalSecondaryIndexUpdate{},
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}

	_, err = DynamoDBClient().TagResourceWithContext(ctx, &dynamodb.TagResourceInput{
		ResourceArn: aws.String(""),
		Tags:        []*dynamodb.Tag{},
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}

	_, err = dynamoDBClient.UntagResourceWithContext(ctx, &dynamodb.UntagResourceInput{
		ResourceArn: aws.String(""),
		TagKeys:     []*string{},
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}

	return nil
}
