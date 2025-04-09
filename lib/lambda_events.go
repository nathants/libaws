package lib

import (
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"slices"
)

func FromDynamoDBEventAVList(from []events.DynamoDBAttributeValue) (to []ddbtypes.AttributeValue, err error) {
	to = make([]ddbtypes.AttributeValue, len(from))
	for i := range len(from) {
		to[i], err = FromDynamoDBEventAV(from[i])
		if err != nil {
			return nil, err
		}
	}
	return to, nil
}

func FromDynamoDBEventAV(from events.DynamoDBAttributeValue) (ddbtypes.AttributeValue, error) {
	switch from.DataType() {
	case events.DataTypeNull:
		return &ddbtypes.AttributeValueMemberNULL{Value: from.IsNull()}, nil
	case events.DataTypeBoolean:
		return &ddbtypes.AttributeValueMemberBOOL{Value: from.Boolean()}, nil
	case events.DataTypeBinary:
		return &ddbtypes.AttributeValueMemberB{Value: from.Binary()}, nil
	case events.DataTypeBinarySet:
		bs := make([][]byte, len(from.BinarySet()))
		for i := range len(from.BinarySet()) {
			bs[i] = slices.Clone(from.BinarySet()[i])
		}
		return &ddbtypes.AttributeValueMemberBS{Value: bs}, nil
	case events.DataTypeNumber:
		return &ddbtypes.AttributeValueMemberN{Value: from.Number()}, nil
	case events.DataTypeNumberSet:
		return &ddbtypes.AttributeValueMemberNS{Value: slices.Clone(from.NumberSet())}, nil
	case events.DataTypeString:
		return &ddbtypes.AttributeValueMemberS{Value: from.String()}, nil
	case events.DataTypeStringSet:
		return &ddbtypes.AttributeValueMemberSS{Value: slices.Clone(from.StringSet())}, nil
	case events.DataTypeList:
		values, err := FromDynamoDBEventAVList(from.List())
		if err != nil {
			return nil, err
		}
		return &ddbtypes.AttributeValueMemberL{Value: values}, nil
	case events.DataTypeMap:
		values, err := FromDynamoDBEventAVMap(from.Map())
		if err != nil {
			return nil, err
		}
		return &ddbtypes.AttributeValueMemberM{Value: values}, nil
	default:
		return nil, fmt.Errorf("unknown AttributeValue union member, %T", from)
	}
}

func FromDynamoDBEventAVMap(from map[string]events.DynamoDBAttributeValue) (to map[string]ddbtypes.AttributeValue, err error) {
	to = make(map[string]ddbtypes.AttributeValue, len(from))
	for field, value := range from {
		to[field], err = FromDynamoDBEventAV(value)
		if err != nil {
			return nil, err
		}
	}
	return to, nil
}
