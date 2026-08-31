package lib

import (
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// LambdaFormatVariables formats sorted environment variables, hashing values unless requested.
func LambdaFormatVariables(variables map[string]string, showValues bool) []string {
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := variables[key]
		if !showValues {
			value = SensitiveValueHash(value)
		}
		lines = append(lines, key+"="+value)
	}
	return lines
}

func hashLambdaVariables(variables map[string]string) {
	for key, value := range variables {
		variables[key] = SensitiveValueHash(value)
	}
}

// LambdaSanitizeDescription protects signed code URLs and, by default, environment values.
func LambdaSanitizeDescription(
	out *lambda.GetFunctionOutput,
	configuration *lambda.GetFunctionConfigurationOutput,
	showEnvVarValues bool,
) {
	if out != nil && out.Code != nil && out.Code.Location != nil {
		out.Code.Location = aws.String(SensitiveValueHash(aws.ToString(out.Code.Location)))
	}
	if showEnvVarValues {
		return
	}
	if out != nil && out.Configuration != nil && out.Configuration.Environment != nil {
		hashLambdaVariables(out.Configuration.Environment.Variables)
	}
	if configuration != nil && configuration.Environment != nil {
		hashLambdaVariables(configuration.Environment.Variables)
	}
}
