package lib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/apigatewaymanagementapi"
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "ApiList"}
		defer d.Log()
	}
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

const (
	ErrApiNotFound = "ErrApiNotFound"
)

func Api(ctx context.Context, name string) (*apigatewayv2.Api, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ApiList"}
		defer d.Log()
	}
	var count int
	var result *apigatewayv2.Api
	apis, err := ApiList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, api := range apis {
		if api.Name != nil && *api.Name == name {
			count++
			result = api
		}
	}
	switch count {
	case 0:
		return nil, fmt.Errorf("%s", ErrApiNotFound)
	case 1:
		return result, nil
	default:
		err := fmt.Errorf("more than 1 api (%d) with name: %s", count, name)
		Logger.Println("error:", err)
		return nil, err
	}
}

func ApiListDomains(ctx context.Context) ([]*apigatewayv2.DomainName, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ApiListDomains"}
		defer d.Log()
	}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "ApiUrl"}
		defer d.Log()
	}
	api, err := Api(ctx, name)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf(
		"https://%s.execute-api.%s.amazonaws.com",
		*api.ApiId,
		Region(),
	)
	return url, nil
}

func apiWebsocketApi(domain string) *apigatewaymanagementapi.ApiGatewayManagementApi {
	return apigatewaymanagementapi.New(
		Session(),
		aws.NewConfig().WithEndpoint("https://"+domain),
	)
}

func ApiWebsocketSend(ctx context.Context, domain, connectionID string, data []byte) error {
	_, err := apiWebsocketApi(domain).PostToConnectionWithContext(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: aws.String(connectionID),
		Data:         data,
	})
	return err
}

func ApiWebsocketClose(ctx context.Context, domain, connectionID string) error {
	_, err := apiWebsocketApi(domain).DeleteConnectionWithContext(ctx, &apigatewaymanagementapi.DeleteConnectionInput{
		ConnectionId: aws.String(connectionID),
	})
	return err
}
