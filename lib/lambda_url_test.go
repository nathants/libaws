package lib

import (
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func TestLambdaURLPermissionsRequireURLAndFunctionInvoke(t *testing.T) {
	const functionName = "test-function"
	const functionARN = "arn:aws:lambda:us-east-1:123456789012:function:test-function"
	permissions := lambdaURLPermissions(functionName, functionARN)
	if len(permissions) != 2 {
		t.Fatalf("function URL permissions = %d, want 2", len(permissions))
	}

	bySID := make(map[string]lambdaURLPermission, len(permissions))
	for _, permission := range permissions {
		bySID[permission.statement.Sid] = permission
	}
	urlPermission, ok := bySID[lambdaUrlFuncSid]
	if !ok {
		t.Fatalf("missing %s permission", lambdaUrlFuncSid)
	}
	if got := aws.ToString(urlPermission.input.Action); got != "lambda:InvokeFunctionUrl" {
		t.Fatalf("URL permission action = %q", got)
	}
	if urlPermission.input.FunctionUrlAuthType != lambdatypes.FunctionUrlAuthTypeNone {
		t.Fatalf("URL permission auth type = %q", urlPermission.input.FunctionUrlAuthType)
	}
	if urlPermission.input.InvokedViaFunctionUrl != nil {
		t.Fatal("URL permission unexpectedly sets InvokedViaFunctionUrl")
	}

	invokePermission, ok := bySID[lambdaUrlInvokeSid]
	if !ok {
		t.Fatalf("missing %s permission", lambdaUrlInvokeSid)
	}
	if got := aws.ToString(invokePermission.input.Action); got != "lambda:InvokeFunction" {
		t.Fatalf("invoke permission action = %q", got)
	}
	if invokePermission.input.FunctionUrlAuthType != "" {
		t.Fatalf("invoke permission unexpectedly sets auth type %q", invokePermission.input.FunctionUrlAuthType)
	}
	if !aws.ToBool(invokePermission.input.InvokedViaFunctionUrl) {
		t.Fatal("invoke permission does not restrict invocation to the function URL")
	}

	document := IamPolicyDocument{Version: "2012-10-17"}
	for _, permission := range permissions {
		document.Statement = append(document.Statement, permission.statement)
	}
	actual, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{
		"Version":"2012-10-17",
		"Statement":[
			{
				"Sid":"FunctionUrlInvoke",
				"Effect":"Allow",
				"Principal":"*",
				"Action":"lambda:InvokeFunctionUrl",
				"Resource":"arn:aws:lambda:us-east-1:123456789012:function:test-function",
				"Condition":{"StringEquals":{"lambda:FunctionUrlAuthType":"NONE"}}
			},
			{
				"Sid":"FunctionUrlInvokeFunction",
				"Effect":"Allow",
				"Principal":"*",
				"Action":"lambda:InvokeFunction",
				"Resource":"arn:aws:lambda:us-east-1:123456789012:function:test-function",
				"Condition":{"Bool":{"lambda:InvokedViaFunctionUrl":"true"}}
			}
		]
	}`
	equal, err := iamPolicyEqual(string(actual), expected)
	if err != nil {
		t.Fatal(err)
	}
	if !equal {
		t.Fatalf("function URL policy = %s", actual)
	}
}
