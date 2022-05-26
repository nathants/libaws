# libaws

## why

aws is amazing, but it's hard to see the forest for the trees.

aws should have fewer knobs.

aws should be easy to use, and hard to screw up.

aws should be fast, and fun.

aws should have a [tldr](#tldr).

aws should be on rails!

it should be easy to have a [lambda](#lambda):
- react to docker push to [ecr](#ecr)
- react to [s3](#s3-1) put object
- react to [dynamodb](#dynamodb-1) put item
- react to [sqs](#sqs-1) send message
- react to [time](#schedule) passing
- react to [http](#api) requests
- react to [websocket](#websocket) messages

it should be easy to create:
- [vpcs](#vpc)
- [security groups](#security-group)
- [instance profiles](#instance-profile)
- [keypairs](#keypair)

## how

[declare](#define-an-infrastructure-set) and [deploy](#ensure-the-infrastructure-set) groups of related aws infrastructure as [infrastructure sets](#infrastructure-set):

- that contain:
  - [lambdas](#lambda)
  - [s3](#s3) buckets
  - [dynamodb](#dynamodb) tables
  - [sqs](#sqs) queues
  - [vpcs](#vpc)
  - [security groups](#security-group)
  - [instance profiles](#instance-profile)
  - [keypairs](#keypair)

- that react to lambda [triggers](#trigger):
  - http [apis](#api)
  - [websocket](#websocket) messages
  - [s3](#s3-1) bucket writes
  - [dynamodb](#dynamodb-1) table writes
  - [sqs](#sqs-1) queue puts
  - cron [schedules](#schedule)
  - [ecr](#ecr) docker pushes

## what

a simpler way to [declare](#infrayaml) aws infrastructure that is easy to [use](#typical-usage) and [extend](#extending).

there are two ways to use it:

- [yaml](#infrayaml) and the [cli](#explore-the-cli)

- [go structs](https://github.com/nathants/libaws/blob/master/lib/infra.go#L52) and the [go api](#explore-the-go-api)

the primary entrypoints are:

- [infra-ensure](#ensure-the-infrastructure-set): deploy an infrastructure set.

  ```bash
  libaws infra-ensure ./infra.yaml --preview
  libaws infra-ensure ./infra.yaml
  ```

- [infra-ls](#view-the-infrastructure-set): view infrastructure sets.

  ```bash
  libaws infra-ls
  ```

- [infra-ensure --quick](#quickly-update-lambda-code): quickly update lambda code.

  ```bash
  libaws infra-ensure ./infra.yaml --quick LAMBDA_NAME
  ```

- [infra-rm](#delete-the-infrastructure-set): remove an infrastructure set.

  ```bash
  libaws infra-rm ./infra.yaml --preview
  libaws infra-rm ./infra.yaml
  ```


`infra-ensure` is a [positive assertion](#tradeoffs). it asserts that some named infrastructure exists, and is configured correctly, creating or updating it if needed.

many other entrypoints exist, and can be explored by type. they fall into two categories:

- mutate aws state:

  ```bash
  >> libaws -h | grep ensure | wc -l
  19

  >> libaws -h | grep new | wc -l
  1

  >> libaws -h | grep rm | wc -l
  26

  ```

- view aws state:

  ```bash
  >> libaws -h | grep ls | wc -l
  33

  >> libaws -h | grep describe | wc -l
  6

  >> libaws -h | grep get | wc -l
  16

  >> libaws -h | grep scan | wc -l
  1
  ```

## aws sdk, pulumi, terraform, cloudformation, and serverless

compared to the full aws api, systems declared as [infrastructure sets](#infrastructure-set):

- [have](https://github.com/nathants/libaws/tree/master/examples/simple/python) [simpler](https://github.com/nathants/libaws/tree/master/examples/simple/go) [examples](https://github.com/nathants/libaws/tree/master/examples/simple/docker).

- have [fewer](#typical-usage) [knobs](#infrayaml).

- are easier to use.

- are harder to screw up.

- are almost always enough, and easy to [extend](#extending).

- are more fun.

if you want to use the full aws api, there are many great tools:
- [aws sdk for go](https://aws.amazon.com/sdk-for-go/)
- [pulumi](https://www.pulumi.com/)
- [terraform](https://www.terraform.io/)
- [cloudformation](https://aws.amazon.com/cloudformation/)
- [serverless](https://www.serverless.com/)

## readme index

- [install](#install)
  - [cli](#cli)
  - [go api](#go-api)
- [tldr](#tldr)
  - [define an infrastructure set](#define-an-infrastructure-set)
  - [ensure the infrastructure set](#ensure-the-infrastructure-set)
  - [view the infrastructure set](#view-the-infrastructure-set)
  - [trigger the infrastructure set](#trigger-the-infrastructure-set)
  - [quickly update lambda code](#quickly-update-lambda-code)
  - [delete the infrastructure set](#delete-the-infrastructure-set)
- [usage](#usage)
  - [explore the cli](#explore-the-cli)
  - [explore a cli entrypoint](#explore-a-cli-entrypoint)
  - [explore the go api](#explore-the-go-api)
  - [explore simple examples](#explore-simple-examples)
  - [explore complex examples](#explore-complex-examples)
  - [explore external examples](#explore-external-examples)
- [infrastructure set](#infrastructure-set)
- [typical usage](#typical-usage)
- [design](#design)
- [tradeoffs](#tradeoffs)
- [infra.yaml](#infrayaml)
  - [environment variable substitution](#environment-variable-substitution)
  - [name](#name)
  - [s3](#s3)
  - [dynamodb](#dynamodb)
  - [sqs](#sqs)
  - [keypair](#keypair)
  - [vpc](#vpc)
    - [security group](#security-group)
  - [instance profile](#instance-profile)
  - [lambda](#lambda)
    - [entrypoint](#entrypoint)
    - [attr](#attr)
    - [policy](#policy)
    - [allow](#allow)
    - [env](#env)
    - [include](#include)
    - [require](#require)
    - [trigger](#trigger)
      - [api](#api)
      - [websocket](#websocket)
      - [s3](#s3-1)
      - [dynamodb](#dynamodb-1)
      - [sqs](#sqs-1)
      - [schedule](#schedule)
      - [ecr](#ecr)
- [bash completion](#bash-completion)
- [extending](#extending)
- [testing](#testing)

## install

### cli

```bash
go install github.com/nathants/libaws@latest

export PATH=$PATH:$(go env GOPATH)/bin
```

### go api

```bash
go get github.com/nathants/libaws@latest
```

## tldr

### define an infrastructure set

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
      - concurrency=0
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

### ensure the infrastructure set

![](https://github.com/nathants/libaws/raw/master/gif/ensure.gif)

### view the infrastructure set

depth based colors by [yaml](https://gist.github.com/nathants/1955b2c3130b7d1a00c8420ad6231639)

![](https://github.com/nathants/libaws/raw/master/gif/ls.gif)

### trigger the infrastructure set

![](https://github.com/nathants/libaws/raw/master/gif/trigger.gif)

### quickly update lambda code

![](https://github.com/nathants/libaws/raw/master/gif/update.gif)

### delete the infrastructure set

![](https://github.com/nathants/libaws/raw/master/gif/rm.gif)

## usage

### explore the cli

```bash
>> libaws -h | grep ensure | head

codecommit-ensure             - ensure a codecommit repository
dynamodb-ensure               - ensure a dynamodb table
ec2-ensure-keypair            - ensure a keypair
ec2-ensure-sg                 - ensure a sg
ecr-ensure                    - ensure ecr image
iam-ensure-ec2-spot-roles     - ensure iam ec2 spot roles that are needed to use ec2 spot
iam-ensure-instance-profile   - ensure an iam instance-profile
iam-ensure-role               - ensure an iam role
iam-ensure-user-api           - ensure an iam user with api key
iam-ensure-user-login         - ensure an iam user with login
```

### explore a cli entrypoint

```bash
>> libaws s3-ensure -h

ensure a s3 bucket

example:
 - libaws s3-ensure test-bucket acl=public versioning=true

optional attrs:
 - acl=VALUE        (values = public | private, default = private)
 - versioning=VALUE (values = true | false,     default = false)
 - metrics=VALUE    (values = true | false,     default = true)
 - cors=VALUE       (values = true | false,     default = false)
 - ttldays=VALUE    (values = 0 | n,            default = 0)

Usage: s3-ensure [--preview] NAME [ATTR [ATTR ...]]

Positional arguments:
  NAME
  ATTR

Options:
  --preview, -p
  --help, -h             display this help and exit
```

### explore the go api

```go
package main

import (
	"github.com/nathants/libaws/lib"
)

func main() {
    lib. (TAB =>)
      |--------------------------------------------------------------------------------|
      |f AcmClient func() *acm.ACM (Function)                                          |
      |f AcmClientExplicit func(accessKeyID string, accessKeySecret string, region stri|
      |f AcmListCertificates func(ctx context.Context) ([]*acm.CertificateSummary, erro|
      |f Api func(ctx context.Context, name string) (*apigatewayv2.Api, error) (Functio|
      |f ApiClient func() *apigatewayv2.ApiGatewayV2 (Function)                        |
      |f ApiClientExplicit func(accessKeyID string, accessKeySecret string, region stri|
      |f ApiList func(ctx context.Context) ([]*apigatewayv2.Api, error) (Function)     |
      |f ApiListDomains func(ctx context.Context) ([]*apigatewayv2.DomainName, error) (|
      |f ApiUrl func(ctx context.Context, name string) (string, error) (Function)      |
      |f ApiUrlDomain func(ctx context.Context, name string) (string, error) (Function)|
      |...                                                                             |
      |--------------------------------------------------------------------------------|
}
```

### explore simple examples

- api: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/api), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/api), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/api)
- dynamodb: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/dynamodb), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/dynamodb), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/dynamodb)
- ecr: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/ecr), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/ecr), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/ecr)
- includes: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/includes), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/includes)
- s3: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/s3), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/s3), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/s3)
- schedule: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/schedule), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/schedule), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/schedule)
- sqs: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/sqs), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/sqs), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/sqs)
- websocket: [python](https://github.com/nathants/libaws/tree/master/examples/simple/python/websocket), [go](https://github.com/nathants/libaws/tree/master/examples/simple/go/websocket), [docker](https://github.com/nathants/libaws/tree/master/examples/simple/docker/websocket)

### explore complex examples

- [s3-ec2](https://github.com/nathants/libaws/tree/master/examples/complex/s3-ec2):
  - write to s3 in-bucket
  - which triggers lambda
  - which launches ec2 spot
  - which reads from in-bucket, writes to out-bucket, and terminates

### explore external examples

- [new-gocljs](https://github.com/nathants/new-gocljs)

- [aws-rce](https://github.com/nathants/aws-rce)

- [aws-ensure-route53](https://github.com/nathants/aws-ensure-route53)

## infrastructure set

an infrastructure set is defined by [yaml](#infrayaml) or [go struct](https://github.com/nathants/libaws/blob/master/lib/infra.go#L52) and contains:

- stateful services
  - [s3](#s3)
  - [dynamodb](#dynamodb)
  - [sqs](#sqs)
- ec2 services
  - [keypairs](#keypair)
  - [instance profiles](#instance-profile)
  - [vpcs](#vpc)
    - [security groups](#security-group)
- [lambdas](#lambda)
  - [lambda triggers](#trigger)
    - [api](#api)
    - [websocket](#websocket)
    - [s3](#s3-1)
    - [dynamodb](#dynamodb-1)
    - [sqs](#sqs-1)
    - [schedule](#schedule)
    - [ecr](#ecr)

## typical usage

- use [infra-ensure](#ensure-the-infrastructure-set) to deploy an infrastructure set.

  ```bash
  libaws infra-ensure ./infra.yaml --preview
  libaws infra-ensure ./infra.yaml
  ```

- use [infra-ls](#view-the-infrastructure-set) to view infrastructure sets.

  ```bash
  libaws infra-ls
  ```

- use [infra-ensure --quick LAMBDA_NAME](#quickly-update-lambda-code) to quickly update lambda code.

  ```bash
  libaws infra-ensure ./infra.yaml --quick LAMBDA_NAME
  ```

- use [infra-rm](#delete-the-infrastructure-set) to remove an infrastructure set.

  ```bash
  libaws infra-rm ./infra.yaml --preview
  libaws infra-rm ./infra.yaml
  ```

## design

- there is no implicit coordination.

  - if you aren't already serializing your infrastructure mutations, lock around [dynamodb](https://github.com/nathants/go-dynamolock).

- there are only two state locations:
  - aws.
  - your code.

- aws infrastructure is uniquely identified by name.
  - all aws infrastructure share a private namespace scoped to account/region. use good names.
  - except s3, which shares a public namespace scoped to earth. use better names.

- mutative operations manipulate aws state.
  - mutative operations are idempotent. if they fail due to a transient error, run them again.
  - mutative operations can `--preview`. no output means no changes.

- `ensure` are mutative operations that create or update infrastructure.

- `rm` are mutative operations that delete infrastructure.

- `ls`, `get`, `scan`, and `describe` operations are non-mutative.

- multiple infrastructure sets can be deployed into the same account/region.

## tradeoffs

- no attempt is made to avoid vendor lock-in.
  - migrating between cloud providers will always be non-trivial.
  - attempting to mitigate future migrations has more cost than benefit in the typical case.

- `ensure` operations are positive assertions. they assert that some named infrastructure exists, and is configured correctly, creating or updating it if needed.

  - positive assertions **CANNOT** remove top level infrastructure, but **CAN** remove configuration from them.

  - removing a `trigger`, `policy`, or `allow` **WILL** remove that from the `lambda`.

  - removing `policy`, or `allow` **WILL** remove that from the `instance-profile`.

  - removing a `security-group` **WILL** remove that from the `vpc`.

  - removing a `rule` **WILL** remove that from the `security-group`.

  - removing an `attr` **WILL** remove that from a `sqs`, `s3`, `dynamodb`, or `lambda`.

  - removing a `keypair`, `vpc`, `instance-profile`, `sqs`, `s3`, `dynamodb`, or `lambda` **WON'T** remove that from the account/region.

    - the operator decides **IF** and **WHEN** top level infrastructure should be deleted, then uses an `rm` operation to do so.

    - as a convenience, `infra-rm` will remove **ALL** infrastructure **CURRENTLY** declared in an `infra.yaml`.

- when using `ensure` operations, no output means no changes.

  - for large infrastructure sets, this can mean a minute or two without output if no changes are needed.

  - to see a lot of output instead of none, set this environment variable:

    ```bash
    export DEBUG=yes
    ```

## infra.yaml

use an `infra.yaml` file to declare an infrastructure set. the schema is as follows:

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

### environment variable substitution

anywhere in `infra.yaml` you can substitute environment variables from the caller's environment:

- example:
  ```yaml
  s3:
    test-bucket-${uid}:
      attr:
        - versioning=${versioning}
  ```

the following variables are defined during deployment, and are useful in `allow` declarations:

- `${API_ID}` the id of the apigateway v2 api created by an `api` trigger.

- `${WEBSOCKET_ID}` the id of the apigateway v2 websocket created by a `websocket` trigger.

### name

defines the name of the infrastructure set.

- schema:
  ```yaml
  name: VALUE
  ```

- example:
  ```yaml
  name: test-infraset
  ```

### s3

defines a [s3](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-s3-bucket.html) bucket:

- the following [attributes](https://github.com/nathants/libaws/tree/master/cmd/s3/ensure.go) can be defined:
  - `acl=VALUE`, values: `public | private`, default: `private`
  - `versioning=VALUE`, values: `true | false`, default: `false`
  - `metrics=VALUE`, values: `true | false`, default: `true`
  - `cors=VALUE`, values: `true | false`, default: `false`
  - `ttldays=VALUE`, values: `0 | n`, default: `0`

- schema:
  ```yaml
  s3:
    VALUE:
      attr:
        - VALUE
  ```

- example:
  ```yaml
  s3:
    test-bucket:
      attr:
        - versioning=true
        - acl=public
  ```

### dynamodb

defines a [dynamodb](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-dynamodb-table.html) table:

- specify key as:
  - `NAME:ATTR_TYPE:KEY_TYPE`

- the following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-dynamodb-table.html) can be defined:
  - `read=VALUE`, provisioned read capacity, default: `0`
  - `write=VALUE`, provisioined write capacity, default: `0`

- on global indices the following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-dynamodb-gsi.html) can be defined:
  - `projection=VALUE`, provisioned read capacity, default: `ALL`
  - `read=VALUE`, provisioned read capacity, default: `0`
  - `write=VALUE`, provisioined write capacity, default: `0`

- on local indices the following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-dynamodb-lsi.html) can be defined:
  - `projection=VALUE`, provisioned read capacity, default: `ALL`

- schema:
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

- example:
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

- example global secondary index:
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

- example local secondary index:
  ```yaml
  dynamodb:
    test-table:
      key:
        - id:s:hash
      local-index:
        test-index:
          key:
            - hometown:s:hash
  ```

### sqs

defines a [sqs](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-sqs-queue.html) queue:

- the following [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-sqs-queue.html#aws-resource-sqs-queue-syntax) can be defined:

  - `delay=VALUE`, delay seconds, default: `0`
  - `size=VALUE`, maximum message size bytes, default: `262144`
  - `retention=VALUE`, message rentention period seconds, default: `345600`
  - `wait=VALUE`, receive wait time seconds, default: `0`
  - `timeout=VALUE`, visibility timeout seconds, default: `30`

- schema:
  ```yaml
  sqs:
    VALUE:
      attr:
        - VALUE
  ```

- example:
  ```yaml
  sqs:
    test-queue:
      attr:
        - delay=20
        - timeout=300
  ```

### keypair

defines an ec2 [keypair](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ec2-keypair.html).

- example:
  ```yaml
  keypair:
    VALUE:
      pubkey-content: VALUE
  ```

- example:
  ```yaml
  keypair:
    test-keypair:
      pubkey-content: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICVp11Z99AySWfbLrMBewZluh7cwLlkjifGH5u22RXor
  ```

### vpc

defines a default-like [vpc](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ec2-vpc.html) with an [internet gateway](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ec2-internetgateway.html) and [public access](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-dns.html#vpc-dns-support).

- schema:
  ```yaml
  vpc:
    VALUE: {}
  ```

- example:
  ```yaml
  vpc:
    test-vpc: {}
  ```

#### security group

defines a [security group](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-ec2-security-group.html) on a vpc

- schema:
  ```yaml
  vpc:
    VALUE:
      security-group:
        VALUE:
          rule:
            - PROTO:PORT:SOURCE
  ```

- example:
  ```yaml
  vpc:
    test-vpc:
      security-group:
        test-sg:
          rule:
            - tcp:22:0.0.0.0/0
  ```

### instance profile

defines an ec2 [instance profile](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-iam-instanceprofile.html).

- schema:
  ```yaml
  instance-profile:
    VALUE:
      allow:
        - SERVICE:ACTION ARN
      policy:
        - VALUE
  ```

- example:
  ```yaml
  instance-profile:
    test-profile:
      allow:
        - s3:* *
      policy:
        - AWSLambdaBasicExecutionRole
  ```

### lambda

defines a [lambda](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-function.html).

- schema:
  ```yaml
  lambda:
    VALUE: {}
  ```

- example:
  ```yaml
  lambda:
    test-lambda: {}
  ```

#### entrypoint

defines the code of the lambda. it is one of:

- a python file.

- a go file.

- an ecr container uri.

- schema:
  ```yaml
  lambda:
    VALUE:
      entrypoint: VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      entrypoint: main.go
  ```

#### attr

defines lambda attributes. the following can be defined:

- `concurrency` defines the reserved concurrent executions, default: `0`

- `memory` defines lambda ram in megabytes, default: `128`

- `timeout` defines the lambda timeout in seconds, default: `300`

- `logs-ttl-days` defines the ttl days for cloudwatch logs, default: `7`

- schema:
  ```yaml
  lambda:
    VALUE:
      attr:
        - KEY=VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      attr:
        - concurrency=100
        - memory=256
        - timeout=60
        - logs-ttl-days=1
  ```

#### policy

defines policies on the lambda's iam role.

- schema:
  ```yaml
  lambda:
    VALUE:
      policy:
        - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      policy:
        - AWSLambdaBasicExecutionRole
  ```

#### allow

defines allows on the lambda's iam role.

- schema:
  ```yaml
  lambda:
    VALUE:
      allow:
        - SERVICE:ACTION ARN
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      allow:
        - s3:* *
        - dynamodb:* arn:aws:dynamodb:*:*:table/test-table
  ```

#### env

defines environment variables on the lambda:

- schema:
  ```yaml
  lambda:
    VALUE:
      env:
        - KEY=VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      env:
        - kind=production
  ```

#### include

defines extra content to include in the lambda zip:

- this is ignored when `entrypoint` is an ecr container uri.

- schema:
  ```yaml
  lambda:
    VALUE:
      include:
        - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      include:
        - ./cacerts.crt
        - ../frontend/public/*
  ```

#### require

defines dependencies to install with pip in the virtualenv zip.

- this is ignored unless the `entrypoint` is a python file.

- schema:
  ```yaml
  lambda:
    VALUE:
      require:
        - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      require:
        - fastapi==0.76.0
  ```

#### trigger

defines triggers for the lambda:

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: VALUE
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: dynamodb
          attr:
            - test-table
  ```

#### trigger types

##### api

defines an [apigateway v2](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-apigatewayv2-api.html) http api:

- add a custom domain with attr: `domain=api.example.com`

- add a custom domain and update route53 with attr: `dns=api.example.com`

  - this domain, or its parent domain, must already exist as a hosted zone in [route53](https://github.com/nathants/libaws/tree/master/cmd/route53/ls.go).

  - this domain, or its parent domain, must already have an [acm](https://github.com/nathants/libaws/tree/master/cmd/acm/ls.go) certificate with subdomain wildcard.

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: api
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: api
          attr:
            - dns=api.example.com
  ```

##### websocket

defines an [apigateway v2](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-apigatewayv2-api.html) websocket api:

- add a custom domain with attr: `domain=ws.example.com`

- add a custom domain and update route53 with attr: `dns=ws.example.com`

  - this domain, or its parent domain, must already exist as a hosted zone in [route53-ls](https://github.com/nathants/libaws/tree/master/cmd/route53/ls.go).

  - this domain, or its parent domain, must already have an [acm](https://github.com/nathants/libaws/tree/master/cmd/acm/ls.go) certificate with subdomain wildcard.

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: websocket
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: websocket
          attr:
            - dns=ws.example.com
  ```

##### s3

defines an [s3 trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-s3-bucket-notificationconfig.html):

- the only attribute must be the bucket name.

- object creation and deletion invoke the trigger.

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: s3
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: s3
          attr:
            - test-bucket
  ```

##### dynamodb

defines a [dynamodb trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html):

- the first attribute must be the table name.

- the following trigger [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html) can be defined:
  - `batch=VALUE`, maximum batch size, default: `100`
  - `parallel=VALUE`, parallelization factor, default: `1`
  - `retry=VALUE`, maximum retry attempts, default: `-1`
  - `window=VALUE`, maximum batching window in seconds, default: `0`
  - `start=VALUE`, starting position

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: dynamodb
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: dynamodb
          attr:
            - test-table
            - start=trim_horizon
  ```

##### sqs

defines a [sqs trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html):

- the first attribute must be the queue name.

- the following trigger [attributes](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-lambda-eventsourcemapping.html) can be defined:
  - `batch=VALUE`, maximum batch size, default: `10`
  - `window=VALUE`, maximum batching window in seconds, default: `0`

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: sqs
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: sqs
          attr:
            - test-queue
  ```

##### schedule

defines a [schedule trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-events-rule.html):

- the only attribute must be the [schedule expression](https://docs.aws.amazon.com/lambda/latest/dg/services-cloudwatchevents-expressions.html).

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: schedule
          attr:
            - VALUE
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: schedule
          attr:
            - rate(24 hours)
  ```

##### ecr

defines an [ecr trigger](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-events-rule.html):

- successful [image actions](https://github.com/nathants/libaws/blob/163533034af790187e56d4e267a797d8131f1307/lib/lambda.go#L153) to any ecr repository will invoke the trigger.

- schema:
  ```yaml
  lambda:
    VALUE:
      trigger:
        - type: ecr
  ```

- example:
  ```yaml
  lambda:
    test-lambda:
      trigger:
        - type: ecr
  ```

## bash completion

```
source completions.d/libaws.sh
source completions.d/aws-creds.sh
source completions.d/aws-creds-temp.sh
```

## extending

drop down to the [aws go sdk](https://pkg.go.dev/github.com/aws/aws-sdk-go/service) and implement what you need.

extend an [existing](https://github.com/nathants/libaws/tree/master/cmd/sqs/ensure.go) [mutative](https://github.com/nathants/libaws/tree/master/cmd/s3/ensure.go) [operation](https://github.com/nathants/libaws/tree/master/cmd/dynamodb/ensure.go) or add a new one.

- make sure that mutative operations are **IDEMPOTENT** and can be **PREVIEWED**.

you will find examples in [cmd/](https://github.com/nathants/libaws/tree/master/cmd) and [lib/](https://github.com/nathants/libaws/tree/master/lib) that can provide a good place to start.

you can reuse many existing operations like:

- [lib/iam.go](https://github.com/nathants/libaws/tree/master/lib/iam.go)

- [lib/lambda.go](https://github.com/nathants/libaws/tree/master/lib/lambda.go)

- [lib/ec2.go](https://github.com/nathants/libaws/tree/master/lib/ec2.go)

alternatively, lift and shift to [other](https://www.pulumi.com/) [infrastructure](https://www.terraform.io/) [automation](https://aws.amazon.com/cloudformation/) [tooling](https://www.serverless.com/). `ls` and `describe` operations will give you all the information you need.

## testing

run all integration tests aws with [tox](https://tox.wiki/en/latest/):

```bash
export LIBAWS_TEST_ACCOUNT=$ACCOUNT_NUM

tox
```

run one integration test aws with [tox](https://tox.wiki/en/latest/):

```bash
export LIBAWS_TEST_ACCOUNT=$ACCOUNT_NUM

tox -- bash -c 'cd examples/simple/python/api/ && python test.py'
```
