package lib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

func TestAPIAccessLogDeclarationValidation(t *testing.T) {
	for _, test := range []struct {
		kind  string
		attrs []string
		valid bool
	}{
		{"api", nil, true},
		{"api", []string{"access-logs=true"}, true},
		{"api", []string{"access-logs=false"}, true},
		{"api", []string{"access-logs=yes"}, false},
		{"api", []string{"access-logs="}, false},
		{"api", []string{"access-logs=true", "access-logs=false"}, false},
		{"websocket", []string{"access-logs=true"}, false},
	} {
		t.Run(test.kind+"/"+Json(test.attrs), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "infra.yaml")
			data := "name: fixture\nlambda:\n  function:\n    entrypoint: main.go\n    trigger:\n      - type: " + test.kind + "\n"
			if test.attrs != nil {
				data += "        attr: " + Json(test.attrs) + "\n"
			}
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := InfraParse(path)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}

type accessLogClient struct {
	stage             *apigatewayv2.GetStageOutput
	readErr, writeErr error
	reads             int
	updates           []*apigatewayv2.UpdateStageInput
	deletes           []*apigatewayv2.DeleteAccessLogSettingsInput
}

func (client *accessLogClient) GetStage(_ context.Context, _ *apigatewayv2.GetStageInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.GetStageOutput, error) {
	client.reads++
	return client.stage, client.readErr
}
func (client *accessLogClient) UpdateStage(_ context.Context, input *apigatewayv2.UpdateStageInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.UpdateStageOutput, error) {
	client.updates = append(client.updates, input)
	return &apigatewayv2.UpdateStageOutput{}, client.writeErr
}
func (client *accessLogClient) DeleteAccessLogSettings(_ context.Context, input *apigatewayv2.DeleteAccessLogSettingsInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteAccessLogSettingsOutput, error) {
	client.deletes = append(client.deletes, input)
	return &apigatewayv2.DeleteAccessLogSettingsOutput{}, client.writeErr
}

func TestAPIAccessLogsReconcilePreviewAndOwnership(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:fixture"
	settings, err := lambdaAPIAccessLogSettings(functionARN)
	if err != nil {
		t.Fatal(err)
	}
	api := &apitypes.Api{ApiId: aws.String("api-id")}
	for _, test := range []struct {
		name                    string
		enabled                 *bool
		current                 *apitypes.AccessLogSettings
		preview                 bool
		readErr, writeErr       error
		wantErr                 bool
		reads, updates, deletes int
	}{
		{name: "undeclared leaves configuration untouched"},
		{name: "enable", enabled: aws.Bool(true), reads: 1, updates: 1},
		{name: "already enabled", enabled: aws.Bool(true), current: settings, reads: 1},
		{name: "already disabled", enabled: aws.Bool(false), reads: 1},
		{name: "disable", enabled: aws.Bool(false), current: settings, reads: 1, deletes: 1},
		{name: "preview enable", enabled: aws.Bool(true), preview: true, reads: 1},
		{name: "preview disable", enabled: aws.Bool(false), current: settings, preview: true, reads: 1},
		{name: "preview absent stage", enabled: aws.Bool(true), preview: true, readErr: &apitypes.NotFoundException{}, reads: 1},
		{name: "preview denied", enabled: aws.Bool(true), preview: true, readErr: errors.New("denied"), wantErr: true, reads: 1},
		{name: "missing stage", enabled: aws.Bool(true), readErr: &apitypes.NotFoundException{}, wantErr: true, reads: 1},
		{name: "unknown destination", enabled: aws.Bool(true), current: &apitypes.AccessLogSettings{DestinationArn: aws.String("elsewhere"), Format: settings.Format}, wantErr: true, reads: 1},
		{name: "reconcile format", enabled: aws.Bool(true), current: &apitypes.AccessLogSettings{DestinationArn: settings.DestinationArn, Format: aws.String("custom")}, reads: 1, updates: 1},
		{name: "disable changed format", enabled: aws.Bool(false), current: &apitypes.AccessLogSettings{DestinationArn: settings.DestinationArn, Format: aws.String("custom")}, reads: 1, deletes: 1},
		{name: "failed update", enabled: aws.Bool(true), writeErr: errors.New("denied"), wantErr: true, reads: 1, updates: 1},
		{name: "failed disable", enabled: aws.Bool(false), current: settings, writeErr: errors.New("denied"), wantErr: true, reads: 1, deletes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &accessLogClient{stage: &apigatewayv2.GetStageOutput{StageName: aws.String("$default"), AccessLogSettings: test.current}, readErr: test.readErr, writeErr: test.writeErr}
			err := lambdaEnsureAPIAccessLogs(t.Context(), client, api, functionARN, test.enabled, test.preview)
			if (err != nil) != test.wantErr || client.reads != test.reads || len(client.updates) != test.updates || len(client.deletes) != test.deletes {
				t.Fatalf("error=%v reads=%d updates=%d deletes=%d", err, client.reads, len(client.updates), len(client.deletes))
			}
			for _, update := range client.updates {
				if aws.ToString(update.ApiId) != "api-id" || aws.ToString(update.StageName) != "$default" || !lambdaAPIAccessLogSettingsEqual(update.AccessLogSettings, settings) || update.AutoDeploy != nil || update.DefaultRouteSettings != nil || update.DeploymentId != nil {
					t.Fatalf("update changed unrelated stage settings: %+v", update)
				}
			}
			for _, deletion := range client.deletes {
				if aws.ToString(deletion.ApiId) != "api-id" || aws.ToString(deletion.StageName) != "$default" {
					t.Fatalf("wrong target: %+v", deletion)
				}
			}
		})
	}
	client := &accessLogClient{}
	if err := lambdaEnsureAPIAccessLogs(t.Context(), client, nil, functionARN, aws.Bool(true), true); err != nil || client.reads != 0 {
		t.Fatalf("new API preview: %v", err)
	}
	if err := lambdaEnsureAPIAccessLogs(t.Context(), client, nil, functionARN, aws.Bool(true), false); err == nil {
		t.Fatal("missing API identity accepted")
	}
}
