package tools

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Converts DynamoDB item to a Go map for JSON encoding
func DynamoItemToMap(item map[string]types.AttributeValue) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for key, value := range item {
		var err error
		result[key], err = attributeValueToInterface(value)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Converts a DynamoDB AttributeValue to an interface{} for JSON encoding
func attributeValueToInterface(av types.AttributeValue) (interface{}, error) {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return v.Value, nil
	case *types.AttributeValueMemberN:
		return v.Value, nil
	case *types.AttributeValueMemberBOOL:
		return v.Value, nil
	case *types.AttributeValueMemberSS:
		return v.Value, nil
	case *types.AttributeValueMemberNS:
		return v.Value, nil
	case *types.AttributeValueMemberL:
		list := make([]interface{}, len(v.Value))
		for i, item := range v.Value {
			val, err := attributeValueToInterface(item)
			if err != nil {
				return nil, err
			}
			list[i] = val
		}
		return list, nil
	case *types.AttributeValueMemberM:
		m := make(map[string]interface{})
		for key, item := range v.Value {
			val, err := attributeValueToInterface(item)
			if err != nil {
				return nil, err
			}
			m[key] = val
		}
		return m, nil
	case *types.AttributeValueMemberNULL:
		return nil, nil
	case *types.AttributeValueMemberB:
		return v.Value, nil // Binary data, returned as []byte
	case *types.AttributeValueMemberBS:
		binarySet := make([][]byte, len(v.Value))
		for i, b := range v.Value {
			binarySet[i] = b
		}
		return binarySet, nil
	default:
		return nil, fmt.Errorf("unsupported AttributeValue type %T", v)
	}
}
