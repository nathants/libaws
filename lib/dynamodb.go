package lib

import (
	"sync"

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
