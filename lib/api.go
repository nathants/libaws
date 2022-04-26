package lib

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/service/apigatewayv2"
)

var apiClient *apigatewayv2.ApiGatewayV2
var apiClientLock sync.RWMutex

func ApiClientExplicit(accessKeyID, accessKeySecret, region string) *apigatewayv2.ApiGatewayV2 {
	return apigatewayv2.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func ApiClient() *apigatewayv2.ApiGatewayV2 {
	apiClientLock.Lock()
	defer apiClientLock.Unlock()
	if apiClient == nil {
		apiClient = apigatewayv2.New(Session())
	}
	return apiClient
}

func ApiList(ctx context.Context) ([]*apigatewayv2.Api, error) {
	var token *string
	var items []*apigatewayv2.Api
	for {
		out, err := ApiClient().GetApisWithContext(ctx, &apigatewayv2.GetApisInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		items = append(items, out.Items...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return items, nil
}

func Api(ctx context.Context, name string) (*apigatewayv2.Api, error) {
	var count int
	var result *apigatewayv2.Api
	apis, err := ApiList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, api := range apis {
		if *api.Name == name {
			count++
			result = api
		}
	}
	switch count {
	case 0:
		return nil, nil
	case 1:
		return result, nil
	default:
		err := fmt.Errorf("more than 1 api (%d) with name: %s", count, name)
		Logger.Println("error:", err)
		return nil, err
	}
}

func ApiListDomains(ctx context.Context) ([]*apigatewayv2.DomainName, error) {
	var token *string
	var result []*apigatewayv2.DomainName
	for {
		out, err := ApiClient().GetDomainNamesWithContext(ctx, &apigatewayv2.GetDomainNamesInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		result = append(result, out.Items...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return result, nil
}

func ApiUrl(ctx context.Context, name string) (string, error) {
	api, err := Api(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	url := fmt.Sprintf(
		"https://%s.execute-api.%s.amazonaws.com",
		*api.ApiId,
		Region(),
	)
	return url, nil
}
