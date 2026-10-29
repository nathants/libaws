package lib

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

const lambdaAPIAccessLogsAttr = "access-logs"

// Keep request bodies, headers, query strings, paths and client identity out of
// these records. The existing Lambda log group owns retention and retrieval.
const lambdaAPIAccessLogFormat = `{"kind":"http-api-access","api_id":"$context.apiId","stage":"$context.stage","request_id":"$context.requestId","request_time_epoch":"$context.requestTimeEpoch","method":"$context.httpMethod","route":"$context.routeKey","status":"$context.status","response_ms":"$context.responseLatency","response_bytes":"$context.responseLength","integration_request_id":"$context.integration.requestId","integration_status":"$context.integration.integrationStatus","backend_status":"$context.integration.status","integration_ms":"$context.integration.latency","error_type":"$context.error.responseType","integration_error":"$context.integrationErrorMessage"}`

func lambdaAPIAccessLogsEnabled(trigger *InfraTrigger) (*bool, error) {
	if trigger.Type != lambdaTriggerApi && trigger.Type != lambdaTriggerWebsocket {
		return nil, nil
	}
	var enabled *bool
	for _, attr := range trigger.Attr {
		key, value, _ := strings.Cut(attr, "=")
		if key != lambdaAPIAccessLogsAttr {
			continue
		}
		if trigger.Type != lambdaTriggerApi {
			return nil, errors.New("access-logs is supported only for HTTP api triggers")
		}
		if enabled != nil || (value != "true" && value != "false") {
			return nil, errors.New("access-logs requires one true or false value")
		}
		enabled = aws.Bool(value == "true")
	}
	return enabled, nil
}

func lambdaValidateAPIAccessLogs(function *InfraLambda) error {
	for _, trigger := range function.Trigger {
		if _, err := lambdaAPIAccessLogsEnabled(trigger); err != nil {
			return err
		}
	}
	return nil
}

func lambdaAPIAccessLogSettings(functionARN string) (*apitypes.AccessLogSettings, error) {
	name, ok := lambdaUnqualifiedFunctionName(functionARN)
	identity, err := arn.Parse(functionARN)
	if !ok || err != nil || identity.Partition == "" || identity.Region == "" || identity.AccountID == "" {
		return nil, fmt.Errorf("invalid Lambda identity for API access logs: %q", functionARN)
	}
	return &apitypes.AccessLogSettings{
		DestinationArn: aws.String(fmt.Sprintf("arn:%s:logs:%s:%s:log-group:/aws/lambda/%s", identity.Partition, identity.Region, identity.AccountID, name)),
		Format:         aws.String(lambdaAPIAccessLogFormat),
	}, nil
}

func lambdaAPIAccessLogSettingsEqual(left, right *apitypes.AccessLogSettings) bool {
	return left != nil && right != nil && aws.ToString(left.DestinationArn) == aws.ToString(right.DestinationArn) && aws.ToString(left.Format) == aws.ToString(right.Format)
}

type lambdaAPIAccessLogClient interface {
	GetStage(context.Context, *apigatewayv2.GetStageInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.GetStageOutput, error)
	UpdateStage(context.Context, *apigatewayv2.UpdateStageInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.UpdateStageOutput, error)
	DeleteAccessLogSettings(context.Context, *apigatewayv2.DeleteAccessLogSettingsInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteAccessLogSettingsOutput, error)
}

func lambdaReadAPIAccessLogs(ctx context.Context, client lambdaAPIAccessLogClient, apiID, functionARN string) (*apitypes.AccessLogSettings, error) {
	wanted, err := lambdaAPIAccessLogSettings(functionARN)
	if err != nil {
		return nil, err
	}
	stage, err := client.GetStage(ctx, &apigatewayv2.GetStageInput{ApiId: aws.String(apiID), StageName: aws.String(lambdaDollarDefault)})
	if err != nil {
		return nil, err
	}
	if stage == nil || aws.ToString(stage.StageName) != lambdaDollarDefault {
		return nil, fmt.Errorf("API %s has no stable default stage", apiID)
	}
	if stage.AccessLogSettings == nil || (aws.ToString(stage.AccessLogSettings.DestinationArn) == "" && aws.ToString(stage.AccessLogSettings.Format) == "") {
		return nil, nil
	}
	if aws.ToString(stage.AccessLogSettings.DestinationArn) != aws.ToString(wanted.DestinationArn) {
		return nil, fmt.Errorf("API %s access logs do not match the managed Lambda destination", apiID)
	}
	return stage.AccessLogSettings, nil
}

func lambdaEnsureAPIAccessLogs(ctx context.Context, client lambdaAPIAccessLogClient, api *apitypes.Api, functionARN string, enabled *bool, preview bool) error {
	if enabled == nil {
		return nil
	}
	wanted, err := lambdaAPIAccessLogSettings(functionARN)
	if err != nil {
		return err
	}
	apiID := ""
	if api != nil {
		apiID = aws.ToString(api.ApiId)
	}
	if apiID == "" {
		if !preview {
			return errors.New("cannot configure API access logs without an API ID")
		}
		if *enabled {
			Logger.Println(PreviewString(true)+"enabled api access logs:", aws.ToString(wanted.DestinationArn))
		}
		return nil
	}
	current, err := lambdaReadAPIAccessLogs(ctx, client, apiID, functionARN)
	if err != nil {
		if _, missing := errors.AsType[*apitypes.NotFoundException](err); !preview || !missing {
			return err
		}
	}
	if (*enabled && lambdaAPIAccessLogSettingsEqual(current, wanted)) || (!*enabled && current == nil) {
		return nil
	}
	if !preview {
		if *enabled {
			_, err = client.UpdateStage(ctx, &apigatewayv2.UpdateStageInput{ApiId: aws.String(apiID), StageName: aws.String(lambdaDollarDefault), AccessLogSettings: wanted})
		} else {
			_, err = client.DeleteAccessLogSettings(ctx, &apigatewayv2.DeleteAccessLogSettingsInput{ApiId: aws.String(apiID), StageName: aws.String(lambdaDollarDefault)})
		}
		if err != nil {
			return err
		}
	}
	Logger.Printf(PreviewString(preview)+"updated api access logs for %s: enabled=%t\n", apiID, *enabled)
	return nil
}
