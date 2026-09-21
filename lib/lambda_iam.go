package lib

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

// API identities are prerequisites for role grants, not a reason to activate
// code first. Invocation permissions, integration reconciliation and custom
// domains remain owned by LambdaEnsureTriggerApi after activation.
func lambdaPrepareIAMAPIs(ctx context.Context, declaration *InfraLambda, preview bool) error {
	for _, key := range []string{lambdaEnvVarApiID, lambdaEnvVarWebsocketID} {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, trigger := range declaration.Trigger {
		name, key, protocol := declaration.Name, lambdaEnvVarApiID, apitypes.ProtocolTypeHttp
		switch trigger.Type {
		case lambdaTriggerApi:
		case lambdaTriggerWebsocket:
			name, key, protocol = name+LambdaWebsocketSuffix, lambdaEnvVarWebsocketID, apitypes.ProtocolTypeWebsocket
		default:
			continue
		}
		if seen[key] {
			return fmt.Errorf("cannot have more than one %s trigger", trigger.Type)
		}
		seen[key] = true
		caller, err := StsArn(ctx)
		if err != nil {
			return err
		}
		identity, err := lambdaIdentityFromCallerARN(declaration.Name, declaration.infraSetName, Region(), caller)
		if err != nil {
			return err
		}
		api, err := lambdaEnsureTriggerApi(ctx, declaration.infraSetName, name, identity.arn, protocol, preview)
		if err != nil {
			return err
		}
		value := key // Only a genuinely absent preview uses an unresolved identity.
		if api != nil {
			value = aws.ToString(api.ApiId)
			if value == "" {
				return fmt.Errorf("API %s returned no identity", name)
			}
		} else if !preview {
			return fmt.Errorf("API %s was not created", name)
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
