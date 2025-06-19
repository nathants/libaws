package lib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

var apiClient *apigatewayv2.Client
var apiClientLock sync.Mutex

func ApiClientExplicit(accessKeyID, accessKeySecret, region string) *apigatewayv2.Client {
	return apigatewayv2.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func ApiClient() *apigatewayv2.Client {
	apiClientLock.Lock()
	defer apiClientLock.Unlock()
	if apiClient == nil {
		apiClient = apigatewayv2.NewFromConfig(*Session())
	}
	return apiClient
}

func ApiList(ctx context.Context) ([]apitypes.Api, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ApiList"}
		d.Start()
		defer d.End()
	}
	var token *string
	var items []apitypes.Api
	for {
		out, err := ApiClient().GetApis(ctx, &apigatewayv2.GetApisInput{
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

func Api(ctx context.Context, name string) (*apitypes.Api, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Api"}
		d.Start()
		defer d.End()
	}
	var count int
	var result *apitypes.Api
	apis, err := ApiList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, api := range apis {
		if api.Name != nil && *api.Name == name {
			count++
			result = &api
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

func ApiListDomains(ctx context.Context) ([]apitypes.DomainName, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ApiListDomains"}
		d.Start()
		defer d.End()
	}
	var token *string
	var result []apitypes.DomainName
	for {
		out, err := ApiClient().GetDomainNames(ctx, &apigatewayv2.GetDomainNamesInput{
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
		d.Start()
		defer d.End()
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

func apiWebsocketApi(domain string) *apigatewaymanagementapi.Client {
	return apigatewaymanagementapi.NewFromConfig(
		*Session(),
		func(o *apigatewaymanagementapi.Options) {
			o.BaseEndpoint = aws.String("https://" + domain)
		},
	)
}

func ApiWebsocketSend(ctx context.Context, domain, connectionID string, data []byte) error {
	_, err := apiWebsocketApi(domain).PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: aws.String(connectionID),
		Data:         data,
	})
	return err
}

func ApiWebsocketClose(ctx context.Context, domain, connectionID string) error {
	_, err := apiWebsocketApi(domain).DeleteConnection(ctx, &apigatewaymanagementapi.DeleteConnectionInput{
		ConnectionId: aws.String(connectionID),
	})
	return err
}
