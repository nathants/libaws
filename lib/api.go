package lib

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/apigateway"
)

const (
	apiStageName             = "main"
	apiAuthType              = "NONE"
	apiHttpMethod            = "ANY"
	apiType                  = "AWS_PROXY"
	apiIntegrationHttpMethod = "POST"
	apiPath                  = "/{proxy+}"
	apiPathPart              = "{proxy+}"
	apiMappingBasePath       = ""
	apiMappingBasePathEmpty  = "(none)"
)

var (
	apiBinaryMediaTypes           = []*string{aws.String("*/*")}
	apiEndpointConfigurationTypes = []*string{aws.String("REGIONAL")}
)

var apiClient *apigateway.APIGateway
var apiClientLock sync.RWMutex

func ApiClientClear() {
	apiClientLock.Lock()
	defer apiClientLock.Unlock()
	apiClient = nil
	sessionClear()
}

func ApiClient() *apigateway.APIGateway {
	apiClientLock.Lock()
	defer apiClientLock.Unlock()
	if apiClient == nil {
		apiClient = apigateway.New(Session())
	}
	return apiClient
}

func ApiList(ctx context.Context) ([]*apigateway.RestApi, error) {
	var position *string
	var items []*apigateway.RestApi
	for {
		out, err := ApiClient().GetRestApisWithContext(ctx, &apigateway.GetRestApisInput{
			Position: position,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		items = append(items, out.Items...)
		if out.Position == nil {
			break
		}
		position = out.Position

	}
	return items, nil
}

func Api(ctx context.Context, name string) (*apigateway.RestApi, error) {
	var count int
	var result *apigateway.RestApi
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

func ApiResourceID(ctx context.Context, restApiID, path string) (string, error) {
	var position *string
	for {
		out, err := ApiClient().GetResourcesWithContext(ctx, &apigateway.GetResourcesInput{
			RestApiId: aws.String(restApiID),
			Position:  position,
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		for _, resource := range out.Items {
			if path == *resource.Path {
				return *resource.Id, nil
			}
		}
		if out.Position == nil {
			break
		}
		position = out.Position
	}
	return "", nil
}

func ApiUrl(ctx context.Context, name string) (string, error) {
	api, err := Api(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	url := fmt.Sprintf(
		"https://%s.execute-api.%s.amazonaws.com/%s",
		*api.Id,
		Region(),
		apiStageName,
	)
	return url, nil
}

func ApiListDomains(ctx context.Context) ([]*apigateway.DomainName, error) {
	var position *string
	var result []*apigateway.DomainName
	for {
		out, err := ApiClient().GetDomainNamesWithContext(ctx, &apigateway.GetDomainNamesInput{
			Position: position,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		result = append(result, out.Items...)
		if out.Position == nil {
			break
		}
		position = out.Position
	}
	return result, nil
}
