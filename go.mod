module github.com/nathants/libaws

go 1.27.0

require (
	github.com/alexflint/go-arg v1.6.1
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/aws/aws-lambda-go v1.54.0
	github.com/aws/aws-sdk-go-v2 v1.43.6
	github.com/aws/aws-sdk-go-v2/config v1.32.37
	github.com/aws/aws-sdk-go-v2/credentials v1.19.36
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.20.61
	github.com/aws/aws-sdk-go-v2/service/acm v1.44.1
	github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi v1.32.6
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.37.6
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.66.5
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.82.2
	github.com/aws/aws-sdk-go-v2/service/codecommit v1.38.1
	github.com/aws/aws-sdk-go-v2/service/costexplorer v1.67.6
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.63.3
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.321.3
	github.com/aws/aws-sdk-go-v2/service/ecr v1.60.6
	github.com/aws/aws-sdk-go-v2/service/ecs v1.90.2
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.48.6
	github.com/aws/aws-sdk-go-v2/service/iam v1.59.1
	github.com/aws/aws-sdk-go-v2/service/lambda v1.101.4
	github.com/aws/aws-sdk-go-v2/service/organizations v1.54.0
	github.com/aws/aws-sdk-go-v2/service/pricing v1.44.6
	github.com/aws/aws-sdk-go-v2/service/route53 v1.65.8
	github.com/aws/aws-sdk-go-v2/service/s3 v1.107.2
	github.com/aws/aws-sdk-go-v2/service/ses v1.37.6
	github.com/aws/aws-sdk-go-v2/service/sns v1.42.6
	github.com/aws/aws-sdk-go-v2/service/sqs v1.46.6
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.6
	github.com/aws/smithy-go v1.27.8
	github.com/buger/goterm v1.0.4
	github.com/dustin/go-humanize v1.0.1
	github.com/gofrs/uuid v4.4.0+incompatible
	github.com/mattn/go-isatty v0.0.24
	github.com/mikesmitty/edkey v0.0.0-20170222072505-3356ea4e686a
	github.com/r3labs/diff/v2 v2.15.1
	github.com/sethvargo/go-password v0.4.0
	golang.org/x/crypto v0.56.0
	golang.org/x/sync v0.22.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.18 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.38 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.36.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.30 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.12.14 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.37 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.38 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.6 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/vmihailenco/msgpack v4.0.4+incompatible // indirect
	// Keep the transitive module graph above GO-2026-5320, GO-2026-6179, and GO-2026-6180.
	github.com/yuin/goldmark v1.7.17 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

exclude gopkg.in/yaml.v2 v2.2.2
