module github.com/nathants/libaws

go 1.26.0

require (
	github.com/alexflint/go-arg v1.6.1
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/aws/aws-lambda-go v1.54.0
	github.com/aws/aws-sdk-go-v2 v1.42.1
	github.com/aws/aws-sdk-go-v2/config v1.32.27
	github.com/aws/aws-sdk-go-v2/credentials v1.19.26
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.20.50
	github.com/aws/aws-sdk-go-v2/service/acm v1.41.1
	github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi v1.30.5
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.35.8
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.61.1
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.78.2
	github.com/aws/aws-sdk-go-v2/service/codecommit v1.34.6
	github.com/aws/aws-sdk-go-v2/service/costexplorer v1.65.3
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.59.2
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.311.0
	github.com/aws/aws-sdk-go-v2/service/ecr v1.58.6
	github.com/aws/aws-sdk-go-v2/service/ecs v1.86.2
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.46.8
	github.com/aws/aws-sdk-go-v2/service/iam v1.54.7
	github.com/aws/aws-sdk-go-v2/service/lambda v1.94.1
	github.com/aws/aws-sdk-go-v2/service/organizations v1.51.12
	github.com/aws/aws-sdk-go-v2/service/pricing v1.42.9
	github.com/aws/aws-sdk-go-v2/service/route53 v1.63.5
	github.com/aws/aws-sdk-go-v2/service/s3 v1.104.2
	github.com/aws/aws-sdk-go-v2/service/ses v1.35.4
	github.com/aws/aws-sdk-go-v2/service/sns v1.40.3
	github.com/aws/aws-sdk-go-v2/service/sqs v1.44.2
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.5
	github.com/aws/smithy-go v1.27.3
	github.com/buger/goterm v1.0.4
	github.com/dustin/go-humanize v1.0.1
	github.com/gofrs/uuid v4.4.0+incompatible
	github.com/mattn/go-isatty v0.0.22
	github.com/mikesmitty/edkey v0.0.0-20170222072505-3356ea4e686a
	github.com/r3labs/diff/v2 v2.15.1
	github.com/sethvargo/go-password v0.3.1
	golang.org/x/crypto v0.53.0
	golang.org/x/sync v0.22.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.14 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.31 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.34.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.12.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.30 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.31 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.2.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/vmihailenco/msgpack v4.0.4+incompatible // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

exclude gopkg.in/yaml.v2 v2.2.2
