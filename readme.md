# Libaws

## Why

AWS is amazing, but it's hard to ship fast unless you're an expert.

I want to write down best practices as code, then forget about them so I can just ship.

I want to serve [http](#api), react to database [writes](#dynamodb-1), and use [cron](#schedule) to schedule actions.

I want to [push data](https://github.com/nathants/libaws/tree/master/examples/complex/s3-ec2) S3 => EC2 => S3 with ephemeral Spot instances that live for seconds.

Did you know that EC2 is billed by the second, that Spot is 1/5 the price, and that Spot in Local Zones is 1/2 that price?

It's basically free. Oh and no egress fees between EC2 and S3 in the same region. Lambda's not the only thing that scales to zero. AWS is pretty awesome.

AWS should:

* Have fewer knobs
* Have sane defaults
* Be easy to use
* Be hard to screw up
* Be fast
* Be fun
* Have a [tldr](#tldr)

It should be easy for a [lambda](#lambda) to react to:

* Docker push to [ecr](#ecr)
* [S3](#s3-1) put object
* [DynamoDB](#dynamodb-1) put item
* [SQS](#sqs-1) send message
* [Time](#schedule) passing
* [http](#api) requests
* [url](#url) streaming HTTP requests
* [websocket](#websocket) messages

It should be easy to create:

* [VPCs](#vpc)
* [Security groups](#security-group)
* [Instance profiles](#instance-profile)
* [Keypairs](#keypair)
* [Instances](https://github.com/nathants/libaws/blob/9b4ba3cb597519b860e833a0147ea7963d9f7598/cmd/ec2/new.go#L22)

## How

[Declare](#define-an-infrastructure-set) and [deploy](#ensure-the-infrastructure-set) groups of related AWS infrastructure as [infrastructure sets](#infrastructure-set):

* That contain:

  * [Lambdas](#lambda)
  * [S3](#s3) buckets
  * [IAM users](#iam-user)
  * [DynamoDB](#dynamodb) tables
  * [SQS](#sqs) queues
  * [VPCs](#vpc)
  * [Security groups](#security-group)
  * [Instance profiles](#instance-profile)
  * [Keypairs](#keypair)

* That react to Lambda [triggers](#trigger):

  * [SES](#ses) emails
  * HTTP [apis](#api)
  * Streaming HTTP [URLs](#url)
  * [Websocket](#websocket) messages
  * [S3](#s3-1) bucket writes
  * [DynamoDB](#dynamodb-1) table writes
  * [SQS](#sqs-1) queue puts
  * Cron [schedules](#schedule)
  * [ECR](#ecr) Docker pushes
  * Lambda invocation [alarms](#alarm)

## What

A simpler way to [declare](#infrayaml) AWS infrastructure that is easy to [use](#typical-usage) and [extend](#extending).

There are two ways to use it:

* [YAML](#infrayaml) and the [CLI](#explore-the-cli)

* [Go structs](https://github.com/nathants/libaws/blob/9b4ba3cb597519b860e833a0147ea7963d9f7598/lib/infra.go#L60) and the [Go API](#explore-the-go-api)

The primary entrypoints are:

* [infra-ensure](#ensure-the-infrastructure-set): deploy an infrastructure set.

  ```bash
  libaws infra-ensure ./infra.yaml --preview
  libaws infra-ensure ./infra.yaml
  ```

* [infra-ls](#view-the-infrastructure-set): view infrastructure sets.

  ```bash
  libaws infra-ls
  ```

* [infra-ensure --quick](#quickly-update-lambda-code): quickly update Lambda code.

  ```bash
  libaws infra-ensure ./infra.yaml --quick LAMBDA_NAME
  ```

* [infra-rm](#delete-the-infrastructure-set): remove an infrastructure set.

  ```bash
  libaws infra-rm ./infra.yaml --preview
  libaws infra-rm ./infra.yaml
  ```

`infra-ensure` is a [positive assertion](#tradeoffs). It asserts that some named infrastructure exists, and is configured correctly, creating or updating it if needed.

Other commands fall into two categories:

* Mutate AWS state: `ensure`, `new`, and `rm`.
* View AWS state: `ls`, `get`, `scan`, and `describe`.

## AWS SDK, Pulumi, Terraform, CloudFormation, and Serverless

Compared to the full AWS API, systems declared as [infrastructure sets](#infrastructure-set):

* [Have](https://github.com/nathants/libaws/tree/master/examples/simple/python) [simpler](https://github.com/nathants/libaws/tree/master/examples/simple/go) [examples](https://github.com/nathants/libaws/tree/master/examples/simple/docker).

* Have [fewer](#typical-usage) [knobs](#infrayaml).

* Are easier to use.

* Are harder to screw up.

* Are almost always enough, and easy to [extend](#extending).

* Are more fun.

If you want to use the full AWS API, there are many great tools:

* [AWS SDK for Go](https://aws.amazon.com/sdk-for-go/)
* [Pulumi](https://www.pulumi.com/)
* [Terraform](https://www.terraform.io/)
* [CloudFormation](https://aws.amazon.com/cloudformation/)
* [Serverless](https://www.serverless.com/)

## Readme Index

* [Install](#install)

  * [CLI](#cli)
  * [Go API](#go-api)
* [TLDR](#tldr)

  * [Define an infrastructure set](#define-an-infrastructure-set)
  * [Ensure the infrastructure set](#ensure-the-infrastructure-set)
  * [View the infrastructure set](#view-the-infrastructure-set)
  * [Trigger the infrastructure set](#trigger-the-infrastructure-set)
  * [Quickly update Lambda code](#quickly-update-lambda-code)
  * [Delete the infrastructure set](#delete-the-infrastructure-set)
* [Usage](#usage)

  * [Explore the CLI](#explore-the-cli)
  * [Explore a CLI entrypoint](#explore-a-cli-entrypoint)
  * [Explore the Go API](#explore-the-go-api)
  * [Explore simple examples](#explore-simple-examples)
  * [Explore complex examples](#explore-complex-examples)
  * [Explore external examples](#explore-external-examples)
* [Infrastructure set](#infrastructure-set)
* [Typical usage](#typical-usage)
* [Design](#design)
* [Tradeoffs](#tradeoffs)
* [infra.yaml](#infrayaml)

  * [Environment variable substitution](#environment-variable-substitution)
  * [Name](#name)
  * [S3](#s3)
  * [IAM user](#iam-user)
  * [DynamoDB](#dynamodb)
  * [SQS](#sqs)
  * [Keypair](#keypair)
  * [VPC](#vpc)

    * [Security group](#security-group)
  * [Instance profile](#instance-profile)
  * [Lambda](#lambda)

    * [Entrypoint](#entrypoint)
    * [Attr](#attr)
    * [Policy](#policy)
    * [Allow](#allow)
    * [Env](#env)
    * [Include](#include)
    * [Require](#require)
    * [Trigger](#trigger)

      * [SES](#ses)
      * [API](#api)
      * [URL](#url)
      * [Websocket](#websocket)
      * [S3](#s3-1)
      * [DynamoDB](#dynamodb-1)
      * [SQS](#sqs-1)
      * [Schedule](#schedule)
      * [ECR](#ecr)
      * [Alarm](#alarm)
* [Bash completion](#bash-completion)
* [Extending](#extending)
* [Testing](#testing)

## Install

### CLI

```bash
go install github.com/nathants/libaws@latest

export PATH=$PATH:$(go env GOPATH)/bin
```

### Go API

```bash
go get github.com/nathants/libaws@latest
```

## TLDR

### Define an Infrastructure Set

```bash
>> cd examples/simple/go/s3 && tree
.
├── infra.yaml
└── main.go
```

```yaml
name: test-infraset-${uid}

s3:
  test-bucket-${uid}:
    attr:
      - acl=private

lambda:
  test-lambda-${uid}:
    entrypoint: main.go
    attr:
      - memory=128
      - timeout=60
    policy:
      - AWSLambdaBasicExecutionRole
    trigger:
      - type: s3
        attr:
          - test-bucket-${uid}
```

```go
package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, e events.S3Event) (events.APIGatewayProxyResponse, error) {
	for _, record := range e.Records {
		fmt.Println(record.S3.Object.Key)
	}
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
```

### Ensure the Infrastructure Set

![](https://github.com/nathants/libaws/raw/master/gif/ensure.gif)

### View the Infrastructure Set

`infra-ls` lists the account. Select one exact set with `--infraset NAME`, or filter names with a positional substring; these selectors cannot be combined:

```bash
libaws infra-ls --infraset my-project
```

Depth-based colors by [YAML](https://gist.github.com/nathants/1955b2c3130b7d1a00c8420ad6231639)

![](https://github.com/nathants/libaws/raw/master/gif/ls.gif)

### Trigger the Infrastructure Set

![](https://github.com/nathants/libaws/raw/master/gif/trigger.gif)

### Quickly Update Lambda Code

![](https://github.com/nathants/libaws/raw/master/gif/update.gif)

### Delete the Infrastructure Set

![](https://github.com/nathants/libaws/raw/master/gif/rm.gif)

## Usage

### Explore the CLI

```bash
libaws --help
```

### Explore a CLI Entrypoint

```bash
libaws s3-ensure --help
```

### Explore the Go API

Use editor completion, the [API reference](https://pkg.go.dev/github.com/nathants/libaws/lib), or local documentation:

```bash
go doc github.com/nathants/libaws/lib
go doc github.com/nathants/libaws/lib.InfraListSet
```

### Explore Simple Examples

* Alarm: [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/alarm)
* API: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/api), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/api), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/api), [custom domain](https://github.com/nathants/libaws/tree/master/examples/simple/go/api-domain)
* DynamoDB: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/dynamodb), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/dynamodb), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/dynamodb)
* EC2: [CLI](https://github.com/nathants/libaws/tree/master/examples/misc/ec2) with Spot/on-demand launches and VPC-selected or explicit subnets.
* ECR: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/ecr), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/ecr), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/ecr)
* Function URL: [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/api_and_stream)
* Includes: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/includes), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/includes)
* S3: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/s3), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/s3), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/s3), [R2 CLI](https://github.com/nathants/libaws/tree/master/examples/misc/r2)
* Schedule: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/schedule), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/schedule), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/schedule)
* SES: [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/ses)
* SQS: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/sqs), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/sqs), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/sqs)
* Websocket: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/websocket), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/websocket), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/websocket)

### Explore Complex Examples

* [Coexisting infrastructure sets](https://github.com/nathants/libaws/tree/master/examples/misc/infrasets): exact-set inventory while an independent set is being removed.
* [Literal S3 keys](https://github.com/nathants/libaws/tree/master/examples/misc/s3-keys): object/version CLI operations without path normalization.
* [DynamoDB indexes](https://github.com/nathants/libaws/tree/master/examples/misc/dynamodb-indexes): provisioned global indexes and local-index projections.

* [S3 append-only](https://github.com/nathants/libaws/tree/master/examples/complex/s3-appendonly): append-only storage with separate writer and reader IAM users.

* [S3-EC2](https://github.com/nathants/libaws/tree/master/examples/complex/s3-ec2):

  * Write to S3 in-bucket
  * Which triggers Lambda
  * Which launches EC2 Spot
  * Which reads from in-bucket, writes to out-bucket, and terminates

### Explore External Examples

* [aws-gocljs](https://github.com/nathants/aws-gocljs)

* [aws-exec](https://github.com/nathants/aws-exec)

* [aws-ensure-route53](https://github.com/nathants/aws-ensure-route53)

## Infrastructure Set

An infrastructure set is defined by [YAML](#infrayaml) or [Go struct](https://github.com/nathants/libaws/blob/9b4ba3cb597519b860e833a0147ea7963d9f7598/lib/infra.go#L60) and contains:

* Stateful infrastructure:

  * [S3](#s3)
  * [DynamoDB](#dynamodb)
  * [SQS](#sqs)
* [IAM users](#iam-user)
* EC2 infrastructure:

  * [Keypairs](#keypair)
  * [Instance profiles](#instance-profile)
  * [VPCs](#vpc)

    * [Security groups](#security-group)
* [Lambdas](#lambda):

  * [Triggers](#trigger):

    * [SES](#ses)
    * [API](#api)
    * [URL](#url)
    * [Websocket](#websocket)
    * [S3](#s3-1)
    * [DynamoDB](#dynamodb-1)
    * [SQS](#sqs-1)
    * [Schedule](#schedule)
    * [ECR](#ecr)
    * [Alarm](#alarm)

## Typical Usage

* Use [infra-ensure](#ensure-the-infrastructure-set) to deploy an infrastructure set.

  ```bash
  libaws infra-ensure ./infra.yaml --preview
  libaws infra-ensure ./infra.yaml
  ```

* Use [infra-ls](#view-the-infrastructure-set) to view infrastructure sets.

  ```bash
  libaws infra-ls
  ```

* Use [infra-ensure --quick LAMBDA\_NAME](#quickly-update-lambda-code) to quickly update Lambda code.

  ```bash
  libaws infra-ensure ./infra.yaml --quick LAMBDA_NAME
  ```

* Use [infra-rm](#delete-the-infrastructure-set) to remove an infrastructure set.

  ```bash
  libaws infra-rm ./infra.yaml --preview
  libaws infra-rm ./infra.yaml
  ```

## Design

* There is no implicit coordination.

  * If you aren't already serializing your infrastructure mutations, lock around [DynamoDB](https://github.com/nathants/go-dynamolock).

* No databases for infrastructure state. There are only two state locations:

  * AWS.
  * Your code.

* AWS infrastructure is uniquely identified by name.

  * All AWS infrastructure share a private namespace scoped to account/region. Use good names.
  * Except S3, which shares a public namespace scoped to Earth. Use better names.

* Mutative operations manipulate AWS state.

  * Mutative operations are idempotent. If they fail due to a transient error, run them again.
  * Mutative operations can `--preview`. No output means no changes.

* `ensure` are mutative operations that create or update infrastructure.

* `rm` are mutative operations that delete infrastructure.

* `ls`, `get`, `scan`, and `describe` operations are non-mutative.

* Multiple infrastructure sets can be deployed into the same account/region.

## Tradeoffs

* `ensure` operations are positive assertions. They assert that some named infrastructure exists, and is configured correctly, creating or updating it if needed.

  * When possible, an existing same-named resource is automatically adopted and converged to the requested configuration.

  * Positive assertions **CANNOT** remove top-level infrastructure, but **CAN** remove configuration from them.

  * Removing a `trigger`, `policy`, or `allow` **WILL** remove that from the `lambda`.

  * Removing `policy`, or `allow` **WILL** remove that from the `instance-profile`.

  * Removing a `security-group` **WILL** remove that from the `vpc`.

  * Removing a `rule` **WILL** remove that from the `security-group`.

  * Removing an `attr` **WILL** remove that from a `sqs`, `s3`, `dynamodb`, or `lambda`.

  * Removing a `keypair`, `vpc`, `instance-profile`, `sqs`, `s3`, `dynamodb`, or `lambda` **WON'T** remove that from the account/region.

    * The operator decides **IF** and **WHEN** top-level infrastructure should be deleted, then uses an `rm` operation to do so.

    * As a convenience, `infra-rm` will remove **ALL** infrastructure **CURRENTLY** declared in an `infra.yaml`.

    * If a declared Lambda is already gone, `infra-rm` removes verifiably owned triggers and preserves ambiguous or independently owned resources.

* When using `ensure` operations, no output means no changes.

  * For large infrastructure sets, this can mean a minute or two without output if no changes are needed.

  * To see a lot of output instead of none, set this environment variable:

    ```bash
    export DEBUG=yes
    ```

* Since `ensure` operations are idempotent, if you encounter errors like rate limits, just try again.

* `infra-ls` is designed to list AWS accounts managed with `infra-ensure`. It will not work well in other scenarios.

## infra.yaml

Use an `infra.yaml` file to declare an infrastructure set. The schema is as follows:

```yaml
name: VALUE
lambda:
  VALUE:
    entrypoint: VALUE
    policy:     [VALUE ...]
    allow:      [VALUE ...]
    attr:       [VALUE ...]
    require:    [VALUE ...]
    env:        [VALUE ...]
    include:    [VALUE ...]
    trigger:
      - type: VALUE
        attr: [VALUE ...]
s3:
  VALUE:
    attr: [VALUE ...]
user:
  VALUE:
    allow:  [VALUE ...]
    policy: [VALUE ...]
dynamodb:
  VALUE:
    key:  [VALUE ...]
    attr: [VALUE ...]
sqs:
  VALUE:
    attr: [VALUE ...]
vpc:
  VALUE:
    security-group:
      VALUE:
        rule: [VALUE ...]
keypair:
  VALUE:
    pubkey-content: VALUE
instance-profile:
  VALUE:
    allow: [VALUE ...]
    policy: [VALUE ...]
```

### Environment Variable Substitution

Anywhere in `infra.yaml` you can substitute environment variables from the caller's environment:

* Example:

  ```yaml
  s3:
    test-bucket-${uid}:
      attr:
        - versioning=${versioning}
  ```

The following variables are defined during deployment, and are useful in `allow` declarations:

* `${API_ID}` the ID of the API Gateway v2 API created by an `api` trigger.

* `${WEBSOCKET_ID}` the ID of the API Gateway v2 websocket created by a `websocket` trigger.

### Name

Defines the name of the infrastructure set.

* Schema:

  ```yaml
  name: VALUE
  ```

* Example:

  ```yaml
  name: test-infraset
  ```

### S3

Defines a [S3](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-s3-bucket.html) bucket:

* The following [attributes](https://github.com/nathants/libaws/tree/master/cmd/s3/ensure.go) can be defined:

  * `acl=VALUE`, values: `public | private`, default: `private`
  * `versioning=VALUE`, values: `true | false`, default: `false`
  * `appendonly=VALUE`, values: `true | false`, default: `false`
  * `metrics=VALUE`, values: `true | false`, default: `false`
  * `cors=VALUE`, values: `true | false`, default: `false`
  * `ttldays=VALUE`, values: `0 | n`, default: `0`. Bucket-wide expiration in days.
  * `allow_put=VALUE`, values: `$principal.amazonaws.com`

* Managed buckets require TLS, use SSE-S3 encryption, and reject SSE-C.

* S3 CLI paths preserve literal keys, including dot segments and repeated slashes; `s3-ls --quiet` prints full `bucket/key` paths.

* `appendonly=true` requires `If-None-Match: *` for object creation and multipart completion, denies deletion, and cannot be combined with expiration.

* Setting `cors=true` uses `*` for allowed origins. To specify one or more explicit origins, do this instead:

  * `corsorigin=http://localhost:8080`
  * `corsorigin=https://example.com`

* Bucket ACL can only be set at creation. Restore missing public-access-block settings before re-ensuring a bucket.

* Schema:

  ```yaml
  s3:
    VALUE:
      attr:
        - VALUE
  ```

* Example:

  ```yaml
  s3:
    test-bucket:
      attr:
        - versioning=true
        - acl=public
  ```

### IAM user

Defines an IAM user without creating credentials.

* `allow` entries use `SERVICE:ACTION RESOURCE`.
* `policy` entries name existing managed policies.
* Undeclared inline and attached policies are removed.
* Create the user's sole access key explicitly with `libaws iam-ensure-user-api-key USER`.

* Schema:

  ```yaml
  user:
    VALUE:
      allow:
        - SERVICE:ACTION RESOURCE
      policy:
        - VALUE
  ```

* Example:

  ```yaml
  user:
    backup-writer:
      allow:
        - s3:PutObject arn:aws:s3:::backup-bucket/*
  ```

### DynamoDB

Defines a [DynamoDB](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-dynamodb-table.html) table:

* Specify key as:

  * `NAME:ATTR_TYPE:KEY_TYPE`

* The following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-dynamodb-table.html) can be defined:

  * `read=VALUE`, provisioned read capacity, default: `0`
  * `write=VALUE`, provisioned write capacity, default: `0`
  * `ttl=ATTR_NAME`, optional expiration attribute.

* On global indices the following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-dynamodb-gsi.html) can be defined:

  * `projection=VALUE`, projection type, default: `ALL`
  * `read=VALUE`, provisioned read capacity, default: `0`
  * `write=VALUE`, provisioned write capacity, default: `0`

* On local indices the following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-dynamodb-lsi.html) can be defined:

  * `projection=VALUE`, projection type, default: `ALL`

* Schema:

  ```yaml
  dynamodb:
    VALUE:
      key:
        - NAME:ATTR_TYPE:KEY_TYPE
      attr:
        - VALUE
      global-index:
        VALUE:
          key:
            - NAME:ATTR_TYPE:KEY_TYPE
          non-key:
            - NAME
          attr:
            - VALUE
      local-index:
        VALUE:
          key:
            - NAME:ATTR_TYPE:KEY_TYPE
          non-key:
            - NAME
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  dynamodb:
    stream-table:
      key:
        - userid:s:hash
        - timestamp:n:range
      attr:
        - stream=keys_only
    auth-table:
      key:
        - id:s:hash
      attr:
        - write=50
        - read=150
  ```

* Example global secondary index:

  ```yaml
  dynamodb:
    test-table:
      key:
        - id:s:hash
      global-index:
        test-index:
          key:
            - hometown:s:hash
  ```

* Example local secondary index:

  ```yaml
  dynamodb:
    test-table:
      key:
        - id:s:hash
        - created:n:range
      local-index:
        test-index:
          key:
            - id:s:hash
            - hometown:s:range
  ```

### SQS

Defines a [SQS](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-sqs-queue.html) queue:

* The following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-sqs-queue.html#aws-resource-sqs-queue-syntax) can be defined:

  * `delay=VALUE`, delay seconds, default: `0`
  * `size=VALUE`, maximum message size bytes, default: `1048576`
  * `retention=VALUE`, message retention period seconds, default: `345600`
  * `wait=VALUE`, receive wait time seconds, default: `0`
  * `timeout=VALUE`, visibility timeout seconds, default: `30`

* Schema:

  ```yaml
  sqs:
    VALUE:
      attr:
        - VALUE
  ```

* Example:

  ```yaml
  sqs:
    test-queue:
      attr:
        - delay=20
        - timeout=300
  ```

### Keypair

Defines an EC2 [keypair](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ec2-keypair.html).

* Schema:

  ```yaml
  keypair:
    VALUE:
      pubkey-content: VALUE
  ```

* Example:

  ```yaml
  keypair:
    test-keypair:
      pubkey-content: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICVp11Z99AySWfbLrMBewZluh7cwLlkjifGH5u22RXor
  ```

### VPC

Defines a default-like [VPC](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ec2-vpc.html) with an [Internet Gateway](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ec2-internetgateway.html) and [public access](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-dns.html#vpc-dns-support).

`ec2-new --vpc NAME_OR_ID` selects subnets in zones offering `--type`: all matching VPC subnets for `--spot`, or one at random for on-demand. Explicit `--subnets "subnet-A subnet-B"` overrides this selection.

* Schema:

  ```yaml
  vpc:
    VALUE: {}
  ```

* Example:

  ```yaml
  vpc:
    test-vpc: {}
  ```

#### Security Group

Defines a [security group](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-ec2-security-group.html) on a VPC.

* Schema:

  ```yaml
  vpc:
    VALUE:
      security-group:
        VALUE:
          rule:
            - PROTO:PORT:SOURCE
  ```

* Example:

  ```yaml
  vpc:
    test-vpc:
      security-group:
        test-sg:
          rule:
            - tcp:22:0.0.0.0/0
  ```

### Instance Profile

Defines an EC2 [instance profile](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-iam-instanceprofile.html).

* Schema:

  ```yaml
  instance-profile:
    VALUE:
      allow:
        - SERVICE:ACTION ARN
      policy:
        - VALUE
  ```

* Example:

  ```yaml
  instance-profile:
    test-profile:
      allow:
        - s3:* *
      policy:
        - AWSLambdaBasicExecutionRole
  ```

### Lambda

Defines a [Lambda](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-function.html).

* Schema:

  ```yaml
  lambda:
    VALUE: {}
  ```

* Example:

  ```yaml
  lambda:
    test-lambda: {}
  ```

#### Entrypoint

Defines the code of the Lambda. It is one of:

* A Python file.

* A Go file. Libaws builds its complete package.

* An ECR container URI ending in `@sha256:` followed by 64 lowercase hexadecimal characters.

* Schema:

  ```yaml
  lambda:
    VALUE:
      entrypoint: VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      entrypoint: main.go
  ```

#### Attr

Defines Lambda attributes. The following can be defined:

* `concurrency=VALUE`, reserved concurrency; omit for the unreserved pool or use `0` to disable the function.

* `memory=VALUE`, RAM in megabytes, default: `128`

* `timeout=VALUE`, timeout in seconds, default: `300`

* `logs-ttl-days=VALUE`, CloudWatch Logs retention in days; `0` disables expiration, default: `7`

* Schema:

  ```yaml
  lambda:
    VALUE:
      attr:
        - KEY=VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      attr:
        - concurrency=100
        - memory=256
        - timeout=60
        - logs-ttl-days=1
  ```

#### Policy

Defines policies on the Lambda's IAM role.

* Schema:

  ```yaml
  lambda:
    VALUE:
      policy:
        - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      policy:
        - AWSLambdaBasicExecutionRole
  ```

#### Allow

Defines allows on the Lambda's IAM role.

* Schema:

  ```yaml
  lambda:
    VALUE:
      allow:
        - SERVICE:ACTION ARN
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      allow:
        - s3:* *
        - dynamodb:* arn:aws:dynamodb:*:*:table/test-table
  ```

#### Env

Defines environment variables on the Lambda:

* Names must match `[a-zA-Z][a-zA-Z0-9_]+` and may not be repeated.
* The environment JSON is limited to 4 KiB; the complete configuration update to 5 KiB.
* Values may be empty and are preserved exactly.

* Schema:

  ```yaml
  lambda:
    VALUE:
      env:
        - KEY=VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      env:
        - kind=production
  ```

#### Include

Defines extra content to include in the Lambda zip. Relative paths resolve beside `infra.yaml`:

* This is ignored when `entrypoint` is an ECR container URI.

* Schema:

  ```yaml
  lambda:
    VALUE:
      include:
        - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      include:
        - ./cacerts.crt
        - ../frontend/public/*
  ```

#### Require

Defines dependencies to install with pip in the virtualenv zip.

* This is ignored unless the `entrypoint` is a Python file.

* Relative paths, including `-rrequirements.txt` and local packages, resolve from the directory containing `infra.yaml`.

* For source dependencies, add `--build-constraint=build-requirements.txt` with exact build-backend pins.

* Schema:

  ```yaml
  lambda:
    VALUE:
      require:
        - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      require:
        - fastapi==0.76.0
  ```

#### Trigger

Defines triggers for the Lambda:

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: VALUE
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: dynamodb
          attr:
            - test-table
            - start=latest
  ```

#### Trigger Types

##### SES

Defines an [SES](https://docs.aws.amazon.com/ses/latest/dg/receiving-email.html) email trigger.

* Required attrs: `dns=DOMAIN` and `bucket=BUCKET`; optional: `prefix=PREFIX`.
* Route53 and SES must already be configured for the domain, and the bucket must allow puts from SES.
* The domain must be ASCII and at most 64 characters because it names the SES rule set.
* Each infrastructure set may declare one SES trigger. Ensuring it replaces the active regional rule set.
* Ensure rejects a same-named rule set containing other rules.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: ses
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  s3:
    my-bucket:
      attr:
        - allow_put=ses.amazonaws.com

  lambda:
    test-lambda:
      trigger:
        - type: ses
          attr:
            - dns=my-email-domain.com
            - bucket=my-bucket
            - prefix=emails/
  ```

##### API

Defines an [API Gateway v2](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-apigatewayv2-api.html) HTTP API:

* Add a custom domain with attr: `domain=api.example.com`

* Add a custom domain and update Route53 with attr: `dns=api.example.com`

  * Use `dns=*.example.com` to accept any subdomain

  * This domain, or its parent domain, must already exist as a hosted zone in [Route53](https://github.com/nathants/libaws/tree/master/cmd/route53/ls.go).

  * An exact or matching wildcard [ACM](https://github.com/nathants/libaws/tree/master/cmd/acm/ls.go) certificate must already exist.

* `infra-rm` removes exclusively managed domains and aliases; shared domains, routing-rule domains, hosted zones, and certificates remain.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: api
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: api
          attr:
            - dns=api.example.com
  ```

##### URL

Defines a Lambda [function URL](https://docs.aws.amazon.com/lambda/latest/dg/urls-configuration.html) trigger with streaming HTTP responses.

* No attributes are required.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: url
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: url
  ```

##### Websocket

Defines an [API Gateway v2](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-apigatewayv2-api.html) websocket API:

* Add a custom domain with attr: `domain=ws.example.com`

* Add a custom domain and update Route53 with attr: `dns=ws.example.com`

  * This domain, or its parent domain, must already exist as a hosted zone in [route53-ls](https://github.com/nathants/libaws/tree/master/cmd/route53/ls.go).

  * An exact or matching wildcard [ACM](https://github.com/nathants/libaws/tree/master/cmd/acm/ls.go) certificate must already exist.

* `infra-rm` removes exclusively managed domains and aliases; shared domains, routing-rule domains, hosted zones, and certificates remain.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: websocket
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: websocket
          attr:
            - dns=ws.example.com
  ```

##### S3

Defines an [S3 trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-s3-bucket-notificationconfig.html):

* The only attribute must be a nonempty bucket name.

* Object creation and deletion invoke the trigger.

* Ensure rejects an existing conflicting notification instead of creating duplicate invocations.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: s3
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: s3
          attr:
            - test-bucket
  ```

##### DynamoDB

Defines a [DynamoDB trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html):

* The first attribute must be the table name.

* The following trigger [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html) can be defined:

  * `batch=VALUE`, maximum batch size, default: `100`
  * `parallel=VALUE`, parallelization factor, default: `1`
  * `retry=VALUE`, maximum retry attempts, default: `-1`
  * `window=VALUE`, maximum batching window in seconds, default: `0`
  * `start=VALUE`, required starting position: `latest | trim_horizon`; immutable on existing mappings.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: dynamodb
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: dynamodb
          attr:
            - test-table
            - start=trim_horizon
  ```

##### SQS

Defines a [SQS trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html). Ensuring a declared trigger re-enables a disabled mapping:

* The first attribute must be a nonempty queue name.

* The following trigger [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html) can be defined:

  * `batch=VALUE`, maximum batch size, default: `10`
  * `window=VALUE`, maximum batching window in seconds, default: `0`

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: sqs
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: sqs
          attr:
            - test-queue
  ```

##### Schedule

Defines a [schedule trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-events-rule.html):

* The only attribute must be the [schedule expression](https://docs.aws.amazon.com/lambda/latest/dg/services-cloudwatchevents-expressions.html).

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: schedule
          attr:
            - VALUE
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: schedule
          attr:
            - rate(24 hours)
  ```

##### ECR

Defines an [ECR trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-events-rule.html):

* Successful [image actions](https://github.com/nathants/libaws/blob/163533034af790187e56d4e267a797d8131f1307/lib/lambda.go#L153) from any ECR repository invoke the trigger; delivery is best-effort.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: ecr
  ```

* Example:

  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: ecr
  ```

##### Alarm

Defines a [CloudWatch alarm](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/AlarmThatSendsEmail.html) that invokes the declaring Lambda when the monitored Lambda records at least the specified number of invocations in one minute.

* The following attributes are required:

  * `name=VALUE`, account-and-region-unique alarm name, 1–255 ASCII letters, digits, dots, hyphens, or underscores.
  * `lambda-invocations=VALUE`, Lambda to monitor.
  * `at-least=VALUE/minute`, positive integer invocation threshold, maximum: `2147483647`.

* Missing data does not breach the alarm.

* Schema:

  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: alarm
          attr:
            - name=VALUE
            - lambda-invocations=VALUE
            - at-least=VALUE/minute
  ```

* Example:

  ```yaml
  lambda:
    alert-handler:
      trigger:
        - type: alarm
          attr:
            - name=beta-invocations-runaway
            - lambda-invocations=beta
            - at-least=300/minute
  ```

## Bash Completion

```
source completions.d/libaws.sh
```

## Extending

Drop down to the [AWS Go SDK](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service) and implement what you need.

Extend an [existing](https://github.com/nathants/libaws/tree/master/cmd/sqs/ensure.go) [mutative](https://github.com/nathants/libaws/tree/master/cmd/s3/ensure.go) [operation](https://github.com/nathants/libaws/tree/master/cmd/dynamodb/ensure.go) or add a new one.

* Make sure that mutative operations are **IDEMPOTENT** and can be **PREVIEWED**.

You will find examples in [cmd/](https://github.com/nathants/libaws/tree/master/cmd) and [lib/](https://github.com/nathants/libaws/tree/master/lib) that can provide a good place to start.

You can reuse many existing operations like:

* [lib/iam.go](https://github.com/nathants/libaws/tree/master/lib/iam.go)

* [lib/lambda.go](https://github.com/nathants/libaws/tree/master/lib/lambda.go)

* [lib/ec2.go](https://github.com/nathants/libaws/tree/master/lib/ec2.go)

Alternatively, lift and shift to [other](https://www.pulumi.com/) [infrastructure](https://www.terraform.io/) [automation](https://aws.amazon.com/cloudformation/) [tooling](https://www.serverless.com/). `ls` and `describe` operations will give you all the information you need.

## Testing

Tests require Go 1.27+, Python 3.12+, [uv](https://docs.astral.sh/uv/), rootless Docker, and the [check tools](Makefile). Use scratch-account credentials and a permanent, publicly delegated Route53 zone in that account:

```bash
export LIBAWS_TEST_ACCOUNT=$ACCOUNT_NUM
export LIBAWS_TEST_DOMAIN=scratch.example.com
make test
```

Set `LIBAWS_TEST_JOBS` to change concurrency (default: `4`; `1` for sequential runs). Do not overlap live suites in one account.

Tests retain shared certificate, DNS, and API-domain fixtures; see [testing guidance](NINA.md#architecture-and-testing) for details.

R2 tests are opt-in: set `LIBAWS_R2_TEST_ACCOUNT` to your `R2_ACCOUNT_ID` and supply `R2_ACCESS_KEY_ID` and `R2_ACCESS_KEY_SECRET`. They create and remove unique `libaws-testing-*` buckets.

Run one example with:

```bash
make
export PATH="$PWD:$PATH"
cd examples/simple/python/api
uv run --locked python -u test.py
```
