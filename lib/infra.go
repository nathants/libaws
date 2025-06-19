package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	events "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"gopkg.in/yaml.v3"
)

const (
	infraSetTagName  = "libaws.infraset"
	infraSetNameNone = "none"
)

type InfraListOutput struct {
	Account  string               `yaml:"account"`
	Region   string               `yaml:"region"`
	InfraSet map[string]*InfraSet `yaml:"infraset,omitempty"`
}

const (
	infraKeyName            = "name"
	infraKeyLambda          = "lambda"
	infraKeyS3              = "s3"
	infraKeyDynamoDB        = "dynamodb"
	infraKeySqs             = "sqs"
	infraKeyKeypair         = "keypair"
	infraKeyVpc             = "vpc"
	infraKeyInstanceProfile = "instance-profile"
)

type InfraSet struct {

	// infra set name
	Name string `yaml:"name,omitempty"`

	// lambda
	Lambda map[string]*InfraLambda `yaml:"lambda,omitempty"`

	// stateful infra
	DynamoDB map[string]*InfraDynamoDB `yaml:"dynamodb,omitempty"`
	SQS      map[string]*InfraSQS      `yaml:"sqs,omitempty"`
	S3       map[string]*InfraS3       `yaml:"s3,omitempty"`

	// ec2 infra
	Keypair         map[string]*InfraKeypair         `yaml:"keypair,omitempty"`
	Vpc             map[string]*InfraVpc             `yaml:"vpc,omitempty"`
	InstanceProfile map[string]*InfraInstanceProfile `yaml:"instance-profile,omitempty"`

	// "none" infraset gets a few extra slots for resources not associated with any infraset
	User  map[string]*InfraUser  `yaml:"user,omitempty"`
	Role  map[string]*InfraRole  `yaml:"role,omitempty"`  // any role  not associated with an infraset shows up here
	Api   map[string]*InfraApi   `yaml:"api,omitempty"`   // any api  not associated with an infraset shows up here
	Event map[string]*InfraEvent `yaml:"event,omitempty"` // any event  not associated with an infraset shows up here
}

type InfraApi struct {
	apiID        string
	infraSetName string
	Dns          string `json:"dns,omitempty"    yaml:"dns,omitempty"`
	Domain       string `json:"domain,omitempty" yaml:"domain,omitempty"`
	ReadOnlyUrl  string `json:"url,omitempty"    yaml:"url,omitempty"`
}

type InfraUser struct {
	Allow  []string `json:"allow,omitempty"  yaml:"allow,omitempty"`
	Policy []string `json:"policy,omitempty" yaml:"policy,omitempty"`
}

type InfraRole struct {
	infraSetName string
	Allow        []string `json:"allow,omitempty"  yaml:"allow,omitempty"`
	Policy       []string `json:"policy,omitempty" yaml:"policy,omitempty"`
}

const (
	infraKeyDynamoDBIndexKey    = "key"
	infraKeyDynamoDBIndexNonKey = "non-key"
	infraKeyDynamoDBIndexAttr   = "attr"
)

type InfraDynamoDBIndex struct {
	Key    []string `json:"key"                  yaml:"key"`
	NonKey []string `json:"non-key,omitempty"    yaml:"non-key,omitempty"`
	Attrs  []string `json:"attr,omitempty"       yaml:"attr,omitempty"`
}

const (
	infraKeyDynamoDBKey         = "key"
	infraKeyDynamoDBAttr        = "attr"
	infraKeyDynamoDBGlobalIndex = "global-index"
	infraKeyDynamoDBLocalIndex  = "local-index"
)

type InfraDynamoDB struct {
	infraSetName string
	Key          []string                       `json:"key"                    yaml:"key"`
	Attr         []string                       `json:"attr,omitempty"         yaml:"attr,omitempty"`
	GlobalIndex  map[string]*InfraDynamoDBIndex `json:"global-index,omitempty" yaml:"global-index,omitempty"`
	LocalIndex   map[string]*InfraDynamoDBIndex `json:"local-index,omitempty"  yaml:"local-index,omitempty"`
}

const (
	infraKeyKeypairPubkeyContent = "pubkey-content"
)

type InfraKeypair struct {
	infraSetName  string
	PubkeyContent string `json:"pubkey-content" yaml:"pubkey-content"`
}

const (
	infraKeyVpcSecurityGroup = "security-group"
	infraKeyVpcEC2           = "ec2"
)

type InfraVpc struct {
	infraSetName  string
	SecurityGroup map[string]*InfraSecurityGroup `json:"security-group" yaml:"security-group"`
	ReadOnlyEC2   map[string]*InfraEC2           `json:"ec2,omitempty"  yaml:"ec2,omitempty"`
}

const (
	infraKeySecurityGroupRule = "rule"
)

type InfraSecurityGroup struct {
	Rule []string `json:"rule,omitempty" yaml:"rule,omitempty"`
}

const (
	infraKeyInstanceProfilePolicy = "policy"
	infraKeyInstanceProfileAllow  = "allow"
)

type InfraInstanceProfile struct {
	infraSetName string
	Policy       []string `json:"policy,omitempty" yaml:"policy,omitempty"`
	Allow        []string `json:"allow,omitempty"  yaml:"allow,omitempty"`
}

type InfraEC2 struct {
	vpcID      string
	instanceID string
	name       string
	Attr       []string `json:"attr,omitempty"  yaml:"attr,omitempty"`
	Count      int      `json:"count,omitempty" yaml:"count,omitempty"`
}

const (
	infraKeyLambdaName       = "name"
	infraKeyLambdaEntrypoint = "entrypoint"
	infraKeyLambdaPolicy     = "policy"
	infraKeyLambdaAllow      = "allow"
	infraKeyLambdaTrigger    = "trigger"
	infraKeyLambdaAttr       = "attr"
	infraKeyLambdaRequire    = "require"
	infraKeyLambdaEnv        = "env"
	infraKeyLambdaInclude    = "include"
)

type InfraLambda struct {
	dir          string // parent dir of infra.yaml file
	runtime      string // provided (container) or python (zip) or go (zip)
	handler      string // "main" (go), "filename.main" (python), or "" (container)
	infraSetName string

	Name       string          `json:"name,omitempty"       yaml:"name,omitempty"`
	Arn        string          `json:"arn,omitempty"        yaml:"arn,omitempty"`
	Entrypoint string          `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Policy     []string        `json:"policy,omitempty"     yaml:"policy,omitempty"`
	Allow      []string        `json:"allow,omitempty"      yaml:"allow,omitempty"`
	Attr       []string        `json:"attr,omitempty"       yaml:"attr,omitempty"`
	Require    []string        `json:"require,omitempty"    yaml:"require,omitempty"`
	Env        []string        `json:"env,omitempty"        yaml:"env,omitempty"`
	Include    []string        `json:"include,omitempty"    yaml:"include,omitempty"`
	Trigger    []*InfraTrigger `json:"trigger,omitempty"    yaml:"trigger,omitempty"`
}

const (
	infraKeySQSAttr = "attr"
)

type InfraSQS struct {
	infraSetName string
	Attr         []string `json:"attr,omitempty" yaml:"attr,omitempty"`
}

const (
	infraKeyS3Attr = "attr"
)

type InfraS3 struct {
	infraSetName string
	Attr         []string `json:"attr,omitempty" yaml:"attr,omitempty"`
}

type InfraEvent struct {
	infraSetName string
	Target       string   `json:"target,omitempty" yaml:"target,omitempty"`
	Attr         []string `json:"attr,omitempty"   yaml:"attr,omitempty"`
}

const (
	infraKeyTriggerType = "type"
	infraKeyTriggerAttr = "attr"
)

type InfraTrigger struct {
	lambdaName string
	Type       string   `json:"type,omitempty" yaml:"type,omitempty"`
	Attr       []string `json:"attr,omitempty" yaml:"attr,omitempty"`
}

func InfraList(ctx context.Context, filter string, showEnvVarValues bool) (*InfraListOutput, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraList"}
		d.Start()
		defer d.End()
	}
	var err error
	lock := &sync.Mutex{}
	infra := &InfraListOutput{
		InfraSet: map[string]*InfraSet{},
	}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Fatal("error: ", err)
	}
	infra.Account = account
	infra.Region = Region()
	errs := make(chan error)
	count := 0
	triggersChan := make(chan *InfraTrigger, 1024)

	// list keypair
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		keypairs, err := InfraListKeypair(ctx)
		if err != nil {
			errs <- err
			return
		}
		for name, keypair := range keypairs {
			infraSetName := keypair.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].Keypair == nil {
				infra.InfraSet[infraSetName].Keypair = map[string]*InfraKeypair{}
			}
			infra.InfraSet[infraSetName].Keypair[name] = keypair
			lock.Unlock()
		}
		errs <- nil
	}()

	// list api
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		apis, err := InfraListApi(ctx, triggersChan)
		if err != nil {
			errs <- err
			return
		}
		for name, api := range apis {
			infraSetName := api.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].Api == nil {
				infra.InfraSet[infraSetName].Api = map[string]*InfraApi{}
			}
			infra.InfraSet[infraSetName].Api[name] = api
			lock.Unlock()
		}
		errs <- nil
	}()

	// list dynamo
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		tables, err := InfraListDynamoDB(ctx)
		if err != nil {
			errs <- err
			return
		}
		for name, table := range tables {
			infraSetName := table.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].DynamoDB == nil {
				infra.InfraSet[infraSetName].DynamoDB = map[string]*InfraDynamoDB{}
			}
			infra.InfraSet[infraSetName].DynamoDB[name] = table
			lock.Unlock()
		}
		errs <- nil
	}()

	// list vpc
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		vpcs, err := InfraListVpc(ctx)
		if err != nil {
			errs <- err
			return
		}
		for name, vpc := range vpcs {
			infraSetName := vpc.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].Vpc == nil {
				infra.InfraSet[infraSetName].Vpc = map[string]*InfraVpc{}
			}
			infra.InfraSet[infraSetName].Vpc[name] = vpc
			lock.Unlock()
		}
		errs <- nil
	}()

	// list sqs
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		queues, err := InfraListSQS(ctx)
		if err != nil {
			errs <- err
			return
		}
		for name, queue := range queues {
			infraSetName := queue.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].SQS == nil {
				infra.InfraSet[infraSetName].SQS = map[string]*InfraSQS{}
			}
			infra.InfraSet[infraSetName].SQS[name] = queue
			lock.Unlock()
		}
		errs <- nil
	}()

	// list s3
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		buckets, err := InfraListS3(ctx, triggersChan)
		if err != nil {
			errs <- err
			return
		}
		for name, bucket := range buckets {
			infraSetName := bucket.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].S3 == nil {
				infra.InfraSet[infraSetName].S3 = map[string]*InfraS3{}
			}
			infra.InfraSet[infraSetName].S3[name] = bucket
			lock.Unlock()
		}
		errs <- nil
	}()

	// list event triggers
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		events, err := InfraListEvent(ctx, triggersChan)
		if err != nil {
			errs <- err
			return
		}
		for name, event := range events {
			infraSetName := event.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].Event == nil {
				infra.InfraSet[infraSetName].Event = map[string]*InfraEvent{}
			}
			infra.InfraSet[infraSetName].Event[name] = event
			lock.Unlock()
		}
		errs <- nil
	}()

	// list user
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		users, err := InfraListUser(ctx)
		if err != nil {
			errs <- err
			return
		}
		lock.Lock()
		if infra.InfraSet[infraSetNameNone] == nil {
			infra.InfraSet[infraSetNameNone] = &InfraSet{}
		}
		if infra.InfraSet[infraSetNameNone].User == nil {
			infra.InfraSet[infraSetNameNone].User = map[string]*InfraUser{}
		}
		maps.Copy(infra.InfraSet[infraSetNameNone].User, users)
		lock.Unlock()
		errs <- nil
	}()

	// list role
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		roles, err := InfraListRole(ctx)
		if err != nil {
			errs <- err
			return
		}
		for name, role := range roles {
			infraSetName := role.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].Role == nil {
				infra.InfraSet[infraSetName].Role = map[string]*InfraRole{}
			}
			infra.InfraSet[infraSetName].Role[name] = role
			lock.Unlock()
		}
		errs <- nil
	}()

	// list instance profile
	count++
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		profiles, err := InfraListInstanceProfile(ctx)
		if err != nil {
			errs <- err
			return
		}
		for name, profile := range profiles {
			infraSetName := profile.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			if filter != "" && !(strings.Contains(infraSetName, filter) || strings.Contains(name, filter)) {
				continue
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].InstanceProfile == nil {
				infra.InfraSet[infraSetName].InstanceProfile = map[string]*InfraInstanceProfile{}
			}
			infra.InfraSet[infraSetName].InstanceProfile[name] = profile
			lock.Unlock()
		}
		errs <- nil
	}()

	// list lambda
	lambdaErr := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logRecover(r)
			}
		}()
		lambdas, err := InfraListLambda(ctx, triggersChan, filter)
		if err != nil {
			lambdaErr <- err
			return
		}
		for name, lambda := range lambdas {
			lambda.Name = "" // name is not a private field on lambda, we don't want this exported as yaml/json
			infraSetName := lambda.infraSetName
			if infraSetName == "" {
				infraSetName = infraSetNameNone
			}
			lock.Lock()
			if infra.InfraSet[infraSetName] == nil {
				infra.InfraSet[infraSetName] = &InfraSet{}
			}
			if infra.InfraSet[infraSetName].Lambda == nil {
				infra.InfraSet[infraSetName].Lambda = map[string]*InfraLambda{}
			}
			infra.InfraSet[infraSetName].Lambda[name] = lambda
			lock.Unlock()
		}
		lambdaErr <- nil
	}()

	for range count {
		err := <-errs
		if err != nil {
			Logger.Fatal("error: ", err)
		}
	}
	close(triggersChan)
	err = <-lambdaErr
	if err != nil {
		Logger.Fatal("error: ", err)
	}

	// remove resources which are implicit to an existing lambda
	var instanceProfileNames []string
	var lambdaNames []string
	var websocketNames []string
	for _, infraSet := range infra.InfraSet {
		for name := range infraSet.Lambda {
			lambdaNames = append(lambdaNames, name)
			websocketNames = append(websocketNames, name+LambdaWebsocketSuffix)
		}
		for name := range infraSet.InstanceProfile {
			instanceProfileNames = append(instanceProfileNames, name)
		}
	}

	for _, infraSet := range infra.InfraSet {

		for _, vpc := range infraSet.Vpc {
			for sgName, sg := range vpc.SecurityGroup {
				if sgName == "default" && len(sg.Rule) == 0 {
					delete(vpc.SecurityGroup, sgName) // do not show empty default sg
				}
			}
			for _, ec2 := range vpc.ReadOnlyEC2 {
				var attrs []string
				for _, attr := range ec2.Attr {
					if !strings.HasPrefix(attr, "vpc=") && !strings.HasPrefix(attr, "tag.user=") {
						attrs = append(attrs, attr) // these attrs are important for instance grouping, but needn't be shown
					}
				}
				ec2.Attr = attrs
			}
		}

		for name := range infraSet.Event {
			if slices.Contains(lambdaNames, strings.Split(name, lambdaEventRuleNameSeparator)[0]) {
				delete(infraSet.Event, name) // shown as trigger of the lambda
			}
		}

		for name := range infraSet.Api {
			if slices.Contains(lambdaNames, name) || slices.Contains(websocketNames, name) {
				delete(infraSet.Api, name) // shown as trigger of the lambda
			}
		}

		for name := range infraSet.Role {
			if slices.Contains(lambdaNames, name) {
				delete(infraSet.Role, name) // shown as allows/policies of the lambda
			}
			if slices.Contains(instanceProfileNames, name) {
				delete(infraSet.Role, name) // shown as instanceProfile
			}
			if name == "OrganizationAccountAccessRole" {
				delete(infraSet.Role, name) // ignore always present roles
			}
		}

		if !showEnvVarValues {
			for _, infraLambda := range infraSet.Lambda {
				for i, env := range infraLambda.Env {
					k, v, err := SplitOnce(env, "=")
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					infraLambda.Env[i] = k + "=" + sha256Short([]byte(v))
				}
			}
		}
	}

	if filter != "" {
		infra.InfraSet[infraSetNameNone] = nil
	}

	infraSet, ok := infra.InfraSet[infraSetNameNone]
	if ok {
		data, err := yaml.Marshal(infraSet)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		if strings.Trim(string(data), "\n") == "{}" {
			delete(infra.InfraSet, infraSetNameNone)
		}
	}

	return infra, nil
}

func InfraListEvent(ctx context.Context, triggersChan chan<- *InfraTrigger) (map[string]*InfraEvent, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListEvent"}
		d.Start()
		defer d.End()
	}
	results := map[string]*InfraEvent{}
	lock := sync.Mutex{}
	rules, err := EventsListRules(ctx, nil)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	errChan := make(chan error)
	for _, rule := range rules {
		rule := rule
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			targets, err := EventsListRuleTargets(ctx, *rule.Name, nil)
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			tagsOut, err := EventsClient().ListTagsForResource(ctx, &events.ListTagsForResourceInput{
				ResourceARN: rule.Arn,
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			infraSetName := ""
			for _, tag := range tagsOut.Tags {
				if *tag.Key == infraSetTagName {
					infraSetName = *tag.Value
					break
				}
			}
			for _, target := range targets {
				if strings.HasPrefix(*target.Arn, "arn:aws:lambda:") {
					if rule.ScheduleExpression != nil {
						triggersChan <- &InfraTrigger{
							lambdaName: Last(strings.Split(*target.Arn, ":")),
							Type:       lambdaTriggerSchedule,
							Attr:       []string{*rule.ScheduleExpression},
						}
					} else if rule.EventPattern != nil && *rule.EventPattern == lambdaEcrEventPattern {
						triggersChan <- &InfraTrigger{
							lambdaName: Last(strings.Split(*target.Arn, ":")),
							Type:       lambdaTriggerEcr,
						}
					}
					if rule.Name == nil {
						rule.Name = aws.String("-")
					}
					if rule.EventPattern == nil {
						rule.EventPattern = aws.String("-")
					}
					if target.Arn == nil {
						target.Arn = aws.String("-")
					}
					infraEvent := &InfraEvent{
						infraSetName: infraSetName,
						Target:       *target.Arn,
						Attr:         []string{"eventpattern=" + *rule.EventPattern, "target=" + *target.Arn},
					}
					lock.Lock()
					results[*rule.Name] = infraEvent
					lock.Unlock()
				}
			}
			errChan <- nil
		}()
	}
	for range rules {
		err := <-errChan
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return results, nil
}

func InfraListLambda(ctx context.Context, triggersChan <-chan *InfraTrigger, filter string) (map[string]*InfraLambda, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListLambda"}
		d.Start()
		defer d.End()
	}
	allFns, err := LambdaListFunctions(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	var fns []lambdatypes.FunctionConfiguration
	for _, fn := range allFns {
		if filter != "" && !strings.Contains(*fn.FunctionName, filter) {
			continue
		}
		fns = append(fns, fn)
	}
	errChan := make(chan error)
	triggers := map[string][]*InfraTrigger{}
	res := map[string]*InfraLambda{}
	for _, fn := range fns {
		fn := fn
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			infraLambda := &InfraLambda{
				Name: *fn.FunctionName,
			}
			if fn.Environment != nil {
				for k, v := range fn.Environment.Variables {
					infraLambda.Env = append(infraLambda.Env, k+"="+v)
				}
			}
			sort.Strings(infraLambda.Env)
			tagsOut, err := LambdaClient().ListTags(ctx, &lambda.ListTagsInput{
				Resource: fn.FunctionArn,
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			for k, v := range tagsOut.Tags {
				if k == infraSetTagName {
					infraLambda.infraSetName = v
					break
				}
			}
			res[infraLambda.Name] = infraLambda
			if fn.MemorySize != nil && *fn.MemorySize != lambdaAttrMemoryDefault {
				infraLambda.Attr = append(infraLambda.Attr, fmt.Sprintf("memory=%d", *fn.MemorySize))
			}
			if fn.Timeout != nil && *fn.Timeout != lambdaAttrTimeoutDefault {
				infraLambda.Attr = append(infraLambda.Attr, fmt.Sprintf("timeout=%d", *fn.Timeout))
			}
			out, err := LambdaClient().GetFunctionConcurrency(ctx, &lambda.GetFunctionConcurrencyInput{
				FunctionName: aws.String(*fn.FunctionName),
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			if out.ReservedConcurrentExecutions != nil {
				infraLambda.Attr = append(infraLambda.Attr, fmt.Sprintf("concurrency=%d", *out.ReservedConcurrentExecutions))
			}
			outUrl, err := LambdaClient().GetFunctionUrlConfig(ctx, &lambda.GetFunctionUrlConfigInput{
				FunctionName: aws.String(*fn.FunctionName),
			})
			if err == nil && outUrl.FunctionUrl != nil {
				triggers[*fn.FunctionName] = append(triggers[*fn.FunctionName], &InfraTrigger{
					lambdaName: *fn.FunctionName,
					Type:       lambdaTriggerUrl,
					Attr:       []string{"url=" + strings.Trim(*outUrl.FunctionUrl, "/")},
				})
			} else {
				var notFound *lambdatypes.ResourceNotFoundException
				if !errors.As(err, &notFound) {
					Logger.Println("error:", err)
					errChan <- err
					return
				}
			}
			logGroupName := "/aws/lambda/" + *fn.FunctionName
			outGroups, err := LogsClient().DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
				LogGroupNamePrefix: aws.String(logGroupName),
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			var logGroup *logstypes.LogGroup
			for _, lg := range outGroups.LogGroups {
				if logGroupName == *lg.LogGroupName {
					logGroup = &lg
					break
				}
			}
			if logGroup != nil {
				if logGroup.RetentionInDays == nil {
					infraLambda.Attr = append(infraLambda.Attr, "logs-ttl-days=0")
				} else if *logGroup.RetentionInDays != lambdaAttrLogsTTLDaysDefault {
					infraLambda.Attr = append(infraLambda.Attr, fmt.Sprintf("logs-ttl-days=%d", *logGroup.RetentionInDays))
				}
			}
			roleName := Last(strings.Split(*fn.Role, "/"))
			policies, err := IamListRolePolicies(ctx, roleName)
			if err != nil {
				errChan <- err
				return
			}
			for _, policy := range policies {
				infraLambda.Policy = append(infraLambda.Policy, *policy.PolicyName)
			}
			allows, err := IamListRoleAllows(ctx, roleName)
			if err != nil {
				errChan <- err
				return
			}
			for _, allow := range allows {
				infraLambda.Allow = append(infraLambda.Allow, allow.String())
			}
			rules, err := SesListReceiptRulesets(ctx)
			if err != nil {
				errChan <- err
				return
			}
			for _, rule := range rules {
				out, err := SesClient().DescribeReceiptRule(ctx, &ses.DescribeReceiptRuleInput{
					RuleName:    rule.Name,
					RuleSetName: rule.Name,
				})
				if err == nil {
					bucket := ""
					prefix := ""
					dns := ""
					for _, action := range out.Rule.Actions {
						if action.S3Action != nil {
							if action.S3Action.BucketName != nil {
								bucket = *action.S3Action.BucketName
							}
							if action.S3Action.ObjectKeyPrefix != nil {
								prefix = *action.S3Action.ObjectKeyPrefix
							}
						}
						if action.LambdaAction != nil &&
							action.LambdaAction.FunctionArn != nil &&
							*action.LambdaAction.FunctionArn == *fn.FunctionArn &&
							len(out.Rule.Recipients) == 1 &&
							out.Rule.Recipients[0] == *rule.Name {
							dns = *rule.Name
						}
					}
					if dns == "" {
						continue
					}
					attrs := []string{
						"dns=" + dns,
						"bucket=" + bucket,
					}
					if prefix != "" {
						attrs = append(attrs, "prefix="+prefix)
					}
					triggers[*fn.FunctionName] = append(triggers[*fn.FunctionName], &InfraTrigger{
						lambdaName: *fn.FunctionName,
						Type:       lambdaTriggerSes,
						Attr:       attrs,
					})
				}
			}
			var marker *string
			for {
				out, err := LambdaClient().ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
					FunctionName: fn.FunctionArn,
					Marker:       marker,
				})
				if err != nil {
					Logger.Println("error:", err)
					errChan <- err
					return
				}
				for _, mapping := range out.EventSourceMappings {
					if mapping.State != nil && slices.Contains([]string{"Disabled", "Disabling"}, *mapping.State) {
						continue
					}
					infra := ArnToInfraName(*mapping.EventSourceArn)
					switch infra {
					case lambdaTriggerDynamoDB:
						triggers[*fn.FunctionName] = append(triggers[*fn.FunctionName], &InfraTrigger{
							lambdaName: *fn.FunctionName,
							Type:       infra,
							Attr: []string{
								DynamoDBStreamArnToTableName(*mapping.EventSourceArn),
								fmt.Sprintf("batch=%d", *mapping.BatchSize),
								fmt.Sprintf("parallel=%d", *mapping.ParallelizationFactor),
								fmt.Sprintf("retry=%d", *mapping.MaximumRetryAttempts),
								fmt.Sprintf("start=%s", strings.ToLower(string(mapping.StartingPosition))),
								fmt.Sprintf("window=%d", *mapping.MaximumBatchingWindowInSeconds),
							},
						})
					case lambdaTriggerSQS:
						triggers[*fn.FunctionName] = append(triggers[*fn.FunctionName], &InfraTrigger{
							lambdaName: *fn.FunctionName,
							Type:       infra,
							Attr: []string{
								SQSArnToName(*mapping.EventSourceArn),
								fmt.Sprintf("batch=%d", *mapping.BatchSize),
								fmt.Sprintf("window=%d", *mapping.MaximumBatchingWindowInSeconds),
							},
						})
					default:
						Logger.Println("ignoring event source mapping:", *mapping.FunctionArn, *mapping.EventSourceArn)
					}
				}
				if out.NextMarker == nil {
					break
				}
				marker = out.NextMarker
			}
			errChan <- nil
		}()
	}
	for range fns {
		err := <-errChan
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	for trigger := range triggersChan {
		triggers[trigger.lambdaName] = append(triggers[trigger.lambdaName], trigger)
	}
	for _, fn := range fns {
		ts, ok := triggers[*fn.FunctionName]
		if ok {
			for _, trigger := range ts {
				infraLambda, ok := res[*fn.FunctionName]
				if !ok {
					panic(*fn.FunctionName)
				}
				infraLambda.Trigger = append(infraLambda.Trigger, trigger)
			}
		}
	}
	// sort so triggers have deterministic order
	for _, infraLambda := range res {
		sort.Slice(infraLambda.Trigger, func(i, j int) bool {
			return Json(infraLambda.Trigger[i]) > Json(infraLambda.Trigger[j])
		})
	}
	return res, nil
}

func InfraListKeypair(ctx context.Context) (map[string]*InfraKeypair, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListKeypair"}
		d.Start()
		defer d.End()
	}
	result := map[string]*InfraKeypair{}
	out, err := EC2Client().DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		IncludePublicKey: aws.Bool(true),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, keypair := range out.KeyPairs {
		if keypair.KeyName == nil || keypair.PublicKey == nil {
			Logger.Println("skipping keypair without name:", keypair.Tags)
			continue
		}
		infraKeypair := &InfraKeypair{
			PubkeyContent: strings.TrimSpace(*keypair.PublicKey),
		}
		for _, tag := range keypair.Tags {
			if *tag.Key == infraSetTagName {
				infraKeypair.infraSetName = *tag.Value
				break
			}
		}
		result[*keypair.KeyName] = infraKeypair
	}
	return result, nil
}

func InfraListApi(ctx context.Context, triggersChan chan<- *InfraTrigger) (map[string]*InfraApi, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListApi"}
		d.Start()
		defer d.End()
	}
	result := map[string]*InfraApi{}
	lock := &sync.Mutex{}
	apis, err := ApiList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	domains, err := ApiListDomains(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	apiToDomain := map[string]string{}
	for _, domain := range domains {
		mappings, err := ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
			DomainName: domain.DomainName,
			MaxResults: aws.String(fmt.Sprint(500)),
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		if len(mappings.Items) == 500 {
			err := fmt.Errorf("api overflow without pagination")
			Logger.Println("error:", err)
			return nil, err
		}
		if mappings.Items != nil {
			for _, mapping := range mappings.Items {
				if *mapping.Stage == lambdaDollarDefault {
					apiToDomain[*mapping.ApiId] = *domain.DomainName
				}
			}
		}
	}
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	apiToDns := map[string]string{}
	for _, zone := range zones {
		records, err := Route53ListRecords(ctx, *zone.Id)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, record := range records {
			if record.Name != nil {
				domain := strings.TrimRight(*record.Name, ".")
				mappings, err := ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
					DomainName: aws.String(domain),
					MaxResults: aws.String(fmt.Sprint(500)),
				})
				if err != nil {
					if strings.Contains(err.Error(), "NotFoundException") {
						continue
					}
					Logger.Println("error:", err)
					return nil, err
				}
				if len(mappings.Items) == 500 {
					err := fmt.Errorf("api overflow without pagination")
					Logger.Println("error:", err)
					return nil, err
				}
				if mappings.Items != nil {
					for _, mapping := range mappings.Items {
						if mapping.Stage != nil && *mapping.Stage == lambdaDollarDefault && mapping.ApiId != nil {
							apiToDns[*mapping.ApiId] = domain
						}
					}
				}
			}
		}
	}
	errChan := make(chan error)
	for _, api := range apis {
		api := api
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			infraApi := &InfraApi{
				apiID: *api.ApiId,
			}
			if api.Tags != nil {
				for k, v := range api.Tags {
					if k == infraSetTagName {
						infraApi.infraSetName = v
						break
					}
				}
			}
			out, err := ApiClient().GetIntegrations(ctx, &apigatewayv2.GetIntegrationsInput{
				ApiId:      api.ApiId,
				MaxResults: aws.String(fmt.Sprint(500)),
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			if len(out.Items) != 1 {
				errChan <- nil
				return
			}
			attrs := []string{}
			dns, ok := apiToDns[*api.ApiId]
			if ok {
				attrs = append(attrs, fmt.Sprintf("dns=%s", dns))
				infraApi.Dns = dns
			} else {
				domain, ok := apiToDomain[*api.ApiId]
				if ok {
					attrs = append(attrs, fmt.Sprintf("domain=%s", domain))
					infraApi.Domain = domain
				}
			}
			triggerType := lambdaTriggerApi
			lambdaName := *api.Name
			if api.RouteSelectionExpression != nil && *api.RouteSelectionExpression == lambdaRouteSelection { // websocket uses a suffix in addition to the lambda name
				if !strings.HasSuffix(lambdaName, LambdaWebsocketSuffix) {
					Logger.Println(*api.RouteSelectionExpression)
					panic(lambdaName)
				}
				lambdaName = lambdaName[:len(lambdaName)-len(LambdaWebsocketSuffix)]
				triggerType = lambdaTriggerWebsocket
			}
			if infraApi.Dns == "" && infraApi.Domain == "" {
				infraApi.ReadOnlyUrl = fmt.Sprintf("%s.execute-api.%s.amazonaws.com", infraApi.apiID, Region())
			} else if infraApi.Domain != "" {
				for _, d := range domains {
					if *d.DomainName == infraApi.Domain {
						infraApi.ReadOnlyUrl = *d.DomainNameConfigurations[0].ApiGatewayDomainName
						break
					}
				}
			}
			if infraApi.ReadOnlyUrl != "" {
				attrs = append(attrs, fmt.Sprintf("url=https://%s", infraApi.ReadOnlyUrl))
			}
			triggersChan <- &InfraTrigger{
				lambdaName: lambdaName,
				Type:       triggerType,
				Attr:       attrs,
			}
			lock.Lock()
			if api.Name != nil {
				result[*api.Name] = infraApi
			}
			lock.Unlock()
			errChan <- nil
		}()
	}
	for range apis {
		err := <-errChan
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return result, nil
}

func InfraListDynamoDB(ctx context.Context) (map[string]*InfraDynamoDB, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListDynamoDB"}
		d.Start()
		defer d.End()
	}
	lock := &sync.Mutex{}
	result := map[string]*InfraDynamoDB{}
	tableNames, err := DynamoDBListTables(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	errChan := make(chan error)
	for _, tableName := range tableNames {
		tableName := tableName
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			infraDynamoDB := &InfraDynamoDB{}
			out, err := DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				var notFoundErr *dynamodbtypes.ResourceNotFoundException
				if errors.As(err, &notFoundErr) {
					errChan <- nil
					return
				}
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			if out.Table == nil {
				errChan <- nil
				return
			}
			attrTypes := map[string]string{}
			for _, attr := range out.Table.AttributeDefinitions {
				attrTypes[*attr.AttributeName] = string(attr.AttributeType)
			}
			for _, key := range out.Table.KeySchema {
				infraDynamoDB.Key = append(infraDynamoDB.Key, fmt.Sprintf("%s:%s:%s",
					*key.AttributeName,
					strings.ToLower(attrTypes[*key.AttributeName]),
					strings.ToLower(string(key.KeyType)),
				))
			}
			if out.Table.ProvisionedThroughput != nil {
				if out.Table.ProvisionedThroughput.ReadCapacityUnits != nil && *out.Table.ProvisionedThroughput.ReadCapacityUnits != 0 {
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("read=%d",
						*out.Table.ProvisionedThroughput.ReadCapacityUnits))
				}
				if out.Table.ProvisionedThroughput.WriteCapacityUnits != nil && *out.Table.ProvisionedThroughput.WriteCapacityUnits != 0 {
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("write=%d",
						*out.Table.ProvisionedThroughput.WriteCapacityUnits))
				}
			}
			if out.Table.StreamSpecification != nil {
				infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("stream=%s",
					strings.ToLower(string(out.Table.StreamSpecification.StreamViewType))))
			}
			if out.Table.LocalSecondaryIndexes != nil {
				for i, index := range out.Table.LocalSecondaryIndexes {
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.IndexName=%s",
						i, *index.IndexName))
					for j, key := range index.KeySchema {
						infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.Key.%d=%s:%s:%s",
							i, j, *key.AttributeName, attrTypes[*key.AttributeName], string(key.KeyType)))
					}
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.ProjectionType=%s",
						i, string(index.Projection.ProjectionType)))
					for j, attr := range index.Projection.NonKeyAttributes {
						infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s",
							i, j, attr))
					}
				}
			}
			if out.Table.GlobalSecondaryIndexes != nil {
				for i, index := range out.Table.GlobalSecondaryIndexes {
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.IndexName=%s",
						i, *index.IndexName))
					for j, key := range index.KeySchema {
						if key.AttributeName != nil && key.KeyType != "" {
							infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Key.%d=%s:%s:%s",
								i, j, *key.AttributeName, attrTypes[*key.AttributeName], string(key.KeyType)))
						}
					}
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=%s",
						i, string(index.Projection.ProjectionType)))
					for j, attr := range index.Projection.NonKeyAttributes {
						infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s",
							i, j, attr))
					}
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.ReadCapacityUnits=%d",
						i, *index.ProvisionedThroughput.ReadCapacityUnits))
					infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.WriteCapacityUnits=%d",
						i, *index.ProvisionedThroughput.WriteCapacityUnits))
				}
			}
			tags, err := DynamoDBListTags(ctx, tableName)
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			for _, tag := range tags {
				if tag.Key != nil && *tag.Key == infraSetTagName && tag.Value != nil {
					infraDynamoDB.infraSetName = *tag.Value
					break
				}
			}
			ttlOut, err := DynamoDBClient().DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			for {
				status := ttlOut.TimeToLiveDescription.TimeToLiveStatus
				if status == dynamodbtypes.TimeToLiveStatusDisabled ||
					status == dynamodbtypes.TimeToLiveStatusEnabled {
					break
				}
				Logger.Println("waiting for table ttl status to finish updating:", tableName, status)
				time.Sleep(2 * time.Second)
			}
			if ttlOut.TimeToLiveDescription.TimeToLiveStatus == dynamodbtypes.TimeToLiveStatusEnabled {
				infraDynamoDB.Attr = append(infraDynamoDB.Attr, "ttl="+*ttlOut.TimeToLiveDescription.AttributeName)
			}
			lock.Lock()
			result[tableName] = infraDynamoDB
			lock.Unlock()
			errChan <- nil
		}()
	}
	for range tableNames {
		err := <-errChan
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return result, nil
}

func InfraListVpc(ctx context.Context) (map[string]*InfraVpc, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListVpc"}
		d.Start()
		defer d.End()
	}
	result := map[string]*InfraVpc{}
	vpcs, err := VpcList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	sgs, err := EC2ListSgs(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	ec2s, err := InfraListEC2(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, vpc := range vpcs {
		infraVpc := &InfraVpc{
			ReadOnlyEC2:   map[string]*InfraEC2{},
			SecurityGroup: map[string]*InfraSecurityGroup{},
		}
		for _, tag := range vpc.Tags {
			if *tag.Key == infraSetTagName {
				infraVpc.infraSetName = *tag.Value
				break
			}
		}
		for name, ec2 := range ec2s {
			if ec2.vpcID == *vpc.VpcId {
				infraVpc.ReadOnlyEC2[name] = ec2
			}
		}
		for _, sg := range sgs {
			if *sg.VpcId == *vpc.VpcId {
				sgName := *sg.GroupName
				var rule []string
				if sg.IpPermissions != nil {
					for _, p := range sg.IpPermissions {
						rs, err := EC2SgRules(p)
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
						for _, r := range rs {
							rule = append(rule, r.String())
						}
					}
				}
				infraVpc.SecurityGroup[sgName] = &InfraSecurityGroup{
					Rule: rule,
				}
			}
		}
		result[EC2Name(vpc.Tags)] = infraVpc
	}
	return result, nil
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func InfraListEC2(ctx context.Context) (map[string]*InfraEC2, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListEC2"}
		d.Start()
		defer d.End()
	}
	result := map[string]*InfraEC2{}
	instances, err := EC2ListInstances(ctx, nil, "")
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	ec2s := map[string][]*InfraEC2{}
	for _, instance := range instances {
		if instance.State.Name == ec2types.InstanceStateNameTerminated {
			continue
		}
		infraEC2 := &InfraEC2{}
		infraEC2.name = EC2Name(instance.Tags)
		infraEC2.instanceID = *instance.InstanceId
		infraEC2.vpcID = orDash(instance.VpcId)
		infraEC2.Count = 1
		infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("type=%s", instance.InstanceType))
		infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("ami=%s", *instance.ImageId))
		infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("kind=%s", EC2Kind(instance)))
		infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("vpc=%s", infraEC2.vpcID))
		for _, sg := range instance.SecurityGroups {
			infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("sg=%s", orDash(sg.GroupName)))
		}
		if instance.State.Name != ec2types.InstanceStateNameRunning {
			infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("state=%s", instance.State.Name))
		}
		for _, tag := range instance.Tags {
			if *tag.Key != "creation-date" && *tag.Key != "Name" && *tag.Key != "aws:ec2spot:fleet-request-id" {
				infraEC2.Attr = append(infraEC2.Attr, fmt.Sprintf("tag.%s=%s", *tag.Key, *tag.Value))
			}
		}
		key := infraEC2.name + "::" + strings.Join(infraEC2.Attr, "::")
		ec2s[key] = append(ec2s[key], infraEC2)
	}
	var keys []string
	for k := range ec2s {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(ec2s[keys[i]]) > len(ec2s[keys[j]])
	})
	for _, k := range keys {
		vs := ec2s[k]
		ec2 := vs[0]
		ec2.Count = len(vs)
		name := strings.Split(k, "::")[0]
		_, ok := result[name]
		if !ok {
			result[name] = ec2
		} else {
			attrs := []string{fmt.Sprintf("name=%s", name)}
			attrs = append(attrs, ec2.Attr...)
			ec2.Attr = attrs
			result[ec2.instanceID] = ec2
		}
	}
	return result, nil
}

func InfraListUser(ctx context.Context) (map[string]*InfraUser, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListUser"}
		d.Start()
		defer d.End()
	}
	out, err := IamListUsers(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	result := map[string]*InfraUser{}
	for _, user := range out {
		result[*user.UserName] = &InfraUser{
			Allow:  user.Allows,
			Policy: user.Policies,
		}
	}
	return result, nil
}

func InfraListRole(ctx context.Context) (map[string]*InfraRole, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListRole"}
		d.Start()
		defer d.End()
	}
	out, err := IamListRoles(ctx, nil)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	result := map[string]*InfraRole{}
	for _, role := range out {
		if strings.HasPrefix(*role.RoleName, "AWSServiceRoleFor") || *role.RoleName == EC2SpotFleetTaggingRole {
			continue
		}
		infraRole := &InfraRole{
			Allow:  role.Allow,
			Policy: role.Policy,
		}
		for _, tag := range role.Tags {
			if tag.Key != nil && *tag.Key == infraSetTagName && tag.Value != nil {
				infraRole.infraSetName = *tag.Value
				break
			}
		}
		result[*role.RoleName] = infraRole
	}
	return result, nil
}

func InfraListInstanceProfile(ctx context.Context) (map[string]*InfraInstanceProfile, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListInstanceProfile"}
		d.Start()
		defer d.End()
	}
	out, err := IamListInstanceProfiles(ctx, nil)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	result := map[string]*InfraInstanceProfile{}
	for _, profile := range out {
		infraProfile := &InfraInstanceProfile{}
		for _, tag := range profile.tags {
			if tag.Key != nil && *tag.Key == infraSetTagName && tag.Value != nil {
				infraProfile.infraSetName = *tag.Value
				break
			}
		}
		for _, role := range profile.Roles {
			infraProfile.Allow = append(infraProfile.Allow, role.Allow...)
			infraProfile.Policy = append(infraProfile.Policy, role.Policy...)
		}
		result[*profile.Name] = infraProfile
	}
	return result, nil
}

func InfraListS3(ctx context.Context, triggersChan chan<- *InfraTrigger) (map[string]*InfraS3, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListS3"}
		d.Start()
		defer d.End()
	}
	lock := &sync.Mutex{}
	res := map[string]*InfraS3{}
	buckets, err := S3Client().ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	errChan := make(chan error)
	for _, bucket := range buckets.Buckets {
		bucket := bucket
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			infraS3 := &InfraS3{}
			s3Client, err := S3ClientBucketRegion(*bucket.Name)
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			policyOut, err := s3Client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
				Bucket: bucket.Name,
			})
			if err == nil {
				policy := IamPolicyDocument{}
				err := json.Unmarshal([]byte(*policyOut.Policy), &policy)
				if err != nil {
					Logger.Println("error:", err)
					errChan <- err
					return
				}
				for _, statement := range policy.Statement {
					if statement.Effect == "Allow" &&
						statement.Action == "s3:PutObject" &&
						statement.Resource == "arn:aws:s3:::"+*bucket.Name+"/*" &&
						strings.HasPrefix(statement.Sid, "allow put from ") {
						principal := statement.Principal.(map[string]any)["Service"].(string)
						infraS3.Attr = append(infraS3.Attr, "allow_put="+principal)
					}
				}
			}

			tagsOut, err := s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
				Bucket: bucket.Name,
			})
			if err != nil {
				if strings.Contains(err.Error(), "NoSuchTagSet") {
				} else {
					Logger.Println("error:", err)
					errChan <- nil
					return
				}
			}
			for _, tag := range tagsOut.TagSet {
				if *tag.Key == infraSetTagName {
					infraS3.infraSetName = *tag.Value
					break
				}
			}
			descr, err := S3GetBucketDescription(ctx, *bucket.Name)
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			s3Default := s3EnsureInputDefault()
			if descr.Policy == nil {
				infraS3.Attr = append(infraS3.Attr, "acl=private")
			} else if descr.Policy != nil && reflect.DeepEqual(s3PublicPolicy(*bucket.Name), *descr.Policy) {
				infraS3.Attr = append(infraS3.Attr, "acl=public")
			} else if descr.Policy != nil {
				privateWithAllowPuts := true
				for _, statement := range descr.Policy.Statement {
					if !(statement.Effect == "Allow" &&
						statement.Action == "s3:PutObject" &&
						strings.HasPrefix(statement.Sid, "allow put from ")) {
						privateWithAllowPuts = false
					}
				}
				if privateWithAllowPuts {
					infraS3.Attr = append(infraS3.Attr, "acl=private")
				} else {
					infraS3.Attr = append(infraS3.Attr, "acl=custom")
				}
			}
			if descr.Cors == nil && s3Default.cors != nil && *s3Default.cors {
				infraS3.Attr = append(infraS3.Attr, "cors=false")
			} else if reflect.DeepEqual(s3Cors(nil), descr.Cors) {
				infraS3.Attr = append(infraS3.Attr, "cors=true")
			} else if len(descr.Cors) > 1 {
				err := fmt.Errorf("cors misconfigured for %s: %s", *bucket.Name, Pformat(descr.Cors))
				Logger.Println("error:", err)
				errChan <- err
				return
			} else if len(descr.Cors) == 1 {
				var allowedOrigins []string
				allowedOrigins = append(allowedOrigins, descr.Cors[0].AllowedOrigins...)
				if reflect.DeepEqual(s3Cors(allowedOrigins), descr.Cors) {
					for _, origin := range allowedOrigins {
						infraS3.Attr = append(infraS3.Attr, "corsorigin="+origin)
					}
				} else {
					err := fmt.Errorf("cors misconfigured for %s: %s", *bucket.Name, Pformat(descr.Cors))
					Logger.Println("error:", err)
					errChan <- err
					return
				}
			}
			if descr.Versioning != s3Default.versioning {
				infraS3.Attr = append(infraS3.Attr, fmt.Sprintf("versioning=%t", descr.Versioning))
			}
			encryption := reflect.DeepEqual(descr.Encryption, s3EncryptionConfig)
			if encryption != s3Default.encryption {
				infraS3.Attr = append(infraS3.Attr, fmt.Sprintf("encryption=%t", encryption))
			}
			metrics := descr.Metrics != nil
			if s3Default.metrics != metrics {
				infraS3.Attr = append(infraS3.Attr, fmt.Sprintf("metrics=%t", metrics))
			}
			ttl := descr.Lifecycle
			if len(ttl) == 1 && int32(s3Default.ttlDays) != *ttl[0].Expiration.Days {
				infraS3.Attr = append(infraS3.Attr, fmt.Sprintf("ttldays=%d", *ttl[0].Expiration.Days))
			}
			if descr.Notifications != nil {
				for _, conf := range descr.Notifications.LambdaFunctionConfigurations {
					if conf.LambdaFunctionArn != nil {
						triggersChan <- &InfraTrigger{
							lambdaName: LambdaArnToLambdaName(*conf.LambdaFunctionArn),
							Type:       lambdaTrigerS3,
							Attr:       []string{*bucket.Name},
						}
					}
				}
			}
			lock.Lock()
			res[*bucket.Name] = infraS3
			lock.Unlock()
			errChan <- nil
		}()
	}
	for range buckets.Buckets {
		err := <-errChan
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return res, nil
}

func InfraListSQS(ctx context.Context) (map[string]*InfraSQS, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraListSQS"}
		d.Start()
		defer d.End()
	}
	urls, err := SQSListQueueUrls(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	errChan := make(chan error)
	lock := &sync.Mutex{}
	res := map[string]*InfraSQS{}
	for _, url := range urls {
		url := url
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			out, err := SQSClient().GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl: aws.String(url),
				AttributeNames: []sqstypes.QueueAttributeName{
					sqstypes.QueueAttributeNameDelaySeconds,
					sqstypes.QueueAttributeNameMaximumMessageSize,
					sqstypes.QueueAttributeNameMessageRetentionPeriod,
					sqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds,
					sqstypes.QueueAttributeNameVisibilityTimeout,
					sqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds,
				},
			})
			if err != nil {
				if strings.Contains(err.Error(), "AWS.SimpleQueueService.NonExistentQueue") {
					errChan <- nil
					return
				}
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			infraSQS := &InfraSQS{}
			outTags, err := SQSClient().ListQueueTags(ctx, &sqs.ListQueueTagsInput{
				QueueUrl: aws.String(url),
			})
			if err != nil {
				Logger.Println("error:", err)
				errChan <- err
				return
			}
			for k, v := range outTags.Tags {
				if k == infraSetTagName {
					infraSQS.infraSetName = v
					break
				}
			}
			if out.Attributes["DelaySeconds"] != "0" { // default
				infraSQS.Attr = append(infraSQS.Attr, "DelaySeconds="+out.Attributes["DelaySeconds"])
			}
			if out.Attributes["MaximumMessageSize"] != "262144" { // default
				infraSQS.Attr = append(infraSQS.Attr, "MaximumMessageSize="+out.Attributes["MaximumMessageSize"])
			}
			if out.Attributes["MessageRetentionPeriod"] != "345600" { // default
				infraSQS.Attr = append(infraSQS.Attr, "MessageRetentionPeriod="+out.Attributes["MessageRetentionPeriod"])
			}
			if out.Attributes["ReceiveMessageWaitTimeSeconds"] != "0" { // default
				infraSQS.Attr = append(infraSQS.Attr, "ReceiveMessageWaitTimeSeconds="+out.Attributes["ReceiveMessageWaitTimeSeconds"])
			}
			if out.Attributes["VisibilityTimeout"] != "30" { // default
				infraSQS.Attr = append(infraSQS.Attr, "VisibilityTimeout="+out.Attributes["VisibilityTimeout"])
			}
			if out.Attributes["KmsDataKeyReusePeriodSeconds"] != "300" { // default
				infraSQS.Attr = append(infraSQS.Attr, "KmsDataKeyReusePeriodSeconds="+out.Attributes["KmsDataKeyReusePeriodSeconds"])
			}
			lock.Lock()
			res[SQSUrlToName(url)] = infraSQS
			lock.Unlock()
			errChan <- nil
		}()
	}
	for range urls {
		err := <-errChan
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return res, nil
}

func InfraEnsureKeypair(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureKeypair"}
		d.Start()
		defer d.End()
	}
	for keypairName, infraKeypair := range infraSet.Keypair {
		err := EC2EnsureKeypair(ctx, infraSet.Name, keypairName, infraKeypair.PubkeyContent, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureInstanceProfile(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureInstanceProfile"}
		d.Start()
		defer d.End()
	}
	for profileName, infraProfile := range infraSet.InstanceProfile {
		err := IamEnsureInstanceProfile(ctx, infraSet.Name, profileName, infraProfile.Policy, infraProfile.Allow, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureVpc(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureVpc"}
		d.Start()
		defer d.End()
	}
	hasVpc := false
	for vpcName, infraVpc := range infraSet.Vpc {
		hasVpc = true
		vpcID, err := VpcEnsure(ctx, infraSet.Name, vpcName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		var sgNames []string
		for sgName, infraSg := range infraVpc.SecurityGroup {
			input, err := EC2EnsureSgInput(infraSet.Name, vpcName, sgName, infraSg.Rule)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = EC2EnsureSg(ctx, input, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			sgNames = append(sgNames, sgName)
		}
		sgs, err := EC2ListSgs(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, sg := range sgs {
			if *sg.VpcId == vpcID && *sg.GroupName != "default" && !slices.Contains(sgNames, *sg.GroupName) {
				err := EC2DeleteSg(ctx, vpcName, *sg.GroupName, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
		}
	}
	if hasVpc {
		err := IamEnsureEC2SpotRoles(ctx, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureS3(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureS3"}
		d.Start()
		defer d.End()
	}
	for bucketName, infraS3 := range infraSet.S3 {
		input, err := S3EnsureInput(infraSet.Name, bucketName, infraS3.Attr)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = S3Ensure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func infraEnsureDynamoDBGlobalIndexToAttrs(infraDynamoDB *InfraDynamoDB) error {
	count := 0
	for name, index := range infraDynamoDB.GlobalIndex {
		infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.IndexName=%s", count, name))
		for i, key := range index.Key {
			infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Key.%d=%s", count, i, key))
		}
		for i, nonKey := range index.NonKey {
			infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s", count, i, nonKey))
		}
		projection := false
		for _, attr := range index.Attrs {
			k, v, err := SplitOnce(attr, "=")
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			switch k {
			case "projection":
				infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=%s", count, strings.ToUpper(v)))
				projection = true
			case "read":
				capacity, err := strconv.Atoi(v)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.ReadCapacityUnits=%d", count, capacity))
			case "write:":
				capacity, err := strconv.Atoi(v)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.WriteCapacityUnits=%d", count, capacity))
			default:
				err := fmt.Errorf("unknown dynamodb global index attr: %s", attr)
				Logger.Println("error:", err)
				return err
			}
		}
		if !projection {
			infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=ALL", count))
		}
		count++
	}
	return nil
}

func infraEnsureDynamoDBLocalIndexToAttrs(infraDynamoDB *InfraDynamoDB) error {
	count := 0
	for name, index := range infraDynamoDB.LocalIndex {
		infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.IndexName=%s", count, name))
		for i, key := range index.Key {
			infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.Key.%d=%s", count, i, key))
		}
		for i, nonKey := range index.NonKey {
			infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s", count, i, nonKey))
		}
		projection := false
		for _, attr := range index.Attrs {
			k, v, err := SplitOnce(attr, "=")
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			switch k {
			case "projection":
				infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=%s", count, strings.ToUpper(v)))
				projection = true
			default:
				err := fmt.Errorf("unknown dynamodb local index attr: %s", attr)
				Logger.Println("error:", err)
				return err
			}
		}
		if !projection {
			infraDynamoDB.Attr = append(infraDynamoDB.Attr, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=ALL", count))
		}
		count++
	}
	return nil
}

func InfraEnsureDynamoDB(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureDynamoDB"}
		d.Start()
		defer d.End()
	}
	for tableName, infraDynamoDB := range infraSet.DynamoDB {
		err := infraEnsureDynamoDBGlobalIndexToAttrs(infraDynamoDB)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = infraEnsureDynamoDBLocalIndexToAttrs(infraDynamoDB)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		input, ttl, err := DynamoDBEnsureInput(infraSet.Name, tableName, infraDynamoDB.Key, infraDynamoDB.Attr)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = DynamoDBEnsure(ctx, input, ttl, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureSQS(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureSQS"}
		d.Start()
		defer d.End()
	}
	for queueName, infraSQS := range infraSet.SQS {
		input, err := SQSEnsureInput(infraSet.Name, queueName, infraSQS.Attr)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = SQSEnsure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureLambda(ctx context.Context, infraSet *InfraSet, quick string, preview, showEnvVarValues bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsureLambda"}
		d.Start()
		defer d.End()
	}
	if quick != "" {
		found := false
		for lambdaName := range infraSet.Lambda {
			if quick == lambdaName {
				found = true
			}
		}
		if !found {
			err := fmt.Errorf("cannot use quick mode for unknown lambda name: %s", quick)
			Logger.Println("error:", err)
			return err
		}
	}
	for lambdaName, infraLambda := range infraSet.Lambda {
		if quick != "" && quick != lambdaName {
			continue
		}
		infraLambda.Name = lambdaName
		if strings.HasSuffix(infraLambda.Entrypoint, ".py") {
			infraLambda.runtime = lambdaRuntimePython
			infraLambda.handler = strings.TrimSuffix(path.Base(infraLambda.Entrypoint), ".py") + ".main"
			err := lambdaEnsure(ctx, infraLambda, quick != "", preview, showEnvVarValues, lambdaUpdateZipPy, lambdaCreateZipPy)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else if strings.HasSuffix(infraLambda.Entrypoint, ".go") {
			infraLambda.runtime = lambdaRuntimeGo
			infraLambda.handler = "main"
			err := lambdaEnsure(ctx, infraLambda, quick != "", preview, showEnvVarValues, lambdaUpdateZipGo, lambdaCreateZipGo)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else if strings.Contains(infraLambda.Entrypoint, ".dkr.ecr.") {
			infraLambda.runtime = lambdaRuntimeContainer
			infraLambda.handler = "main"
			err := lambdaEnsure(ctx, infraLambda, quick != "", preview, showEnvVarValues, lambdaUpdateZipFake, lambdaCreateZipFake)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else {
			err := fmt.Errorf("unknown entrypoint type: %s", infraLambda.Entrypoint)
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func resolveEnvVars(s string, ignore []string) (string, error) {
	for _, variable := range regexp.MustCompile(`(\$\{[^\}]+})`).FindAllString(s, -1) {
		variableName := variable[2 : len(variable)-1]
		variableValue := os.Getenv(variableName)
		if slices.Contains(ignore, variableName) {
			continue
		}
		if variableValue == "" {
			err := fmt.Errorf("missing environment variable: %s", variableName)
			Logger.Println("error:", err)
			return "", err
		}
		s = strings.Replace(s, variable, variableValue, 1)
	}
	return s, nil
}

func lambdaUpdateZipFake(_ *InfraLambda) error { return nil }
func lambdaCreateZipFake(_ *InfraLambda) error { return nil }

func InfraEnsure(ctx context.Context, infraSet *InfraSet, quick string, preview, showEnvVarValues bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraEnsure"}
		d.Start()
		defer d.End()
	}
	if quick == "" {
		err := InfraEnsureKeypair(ctx, infraSet, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = InfraEnsureVpc(ctx, infraSet, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = InfraEnsureInstanceProfile(ctx, infraSet, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = InfraEnsureS3(ctx, infraSet, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = InfraEnsureDynamoDB(ctx, infraSet, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = InfraEnsureSQS(ctx, infraSet, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	err := InfraEnsureLambda(ctx, infraSet, quick, preview, showEnvVarValues)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func infraParseValidateDynamoDBIndex(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraDynamoDB should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, index := range val.(map[string]any) {
		_, ok := index.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraDynamoDBIndex should be type: map[string]any, got: %s %#v", name, index)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range index.(map[string]any) {
			switch k {
			case infraKeyDynamoDBIndexKey, infraKeyDynamoDBIndexNonKey, infraKeyDynamoDBIndexAttr:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraDynamoDBIndex key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraDynamoDBIndex key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraDynamoDB key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateDynamoDB(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraDynamoDB should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, dynamodbVal := range val.(map[string]any) {
		_, ok := dynamodbVal.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraDynamoDB should be type: map[string]any, got: %s %#v", name, dynamodbVal)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range dynamodbVal.(map[string]any) {
			switch k {
			case infraKeyDynamoDBGlobalIndex, infraKeyDynamoDBLocalIndex:
				err := infraParseValidateDynamoDBIndex(v)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			case infraKeyDynamoDBKey, infraKeyDynamoDBAttr:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraDynamoDB key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraDynamoDB key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraDynamoDB key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateS3(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraS3 should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, s3Val := range val.(map[string]any) {
		_, ok := s3Val.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraS3 should be type: map[string]any, got: %s %#v", name, s3Val)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range s3Val.(map[string]any) {
			switch k {
			case infraKeyS3Attr:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraS3 key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraS3 key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraS3 key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateTrigger(val any) error {
	_, ok := val.([]any)
	if !ok {
		err := fmt.Errorf("infraTrigger should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for i, trigger := range val.([]any) {
		_, ok := trigger.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraTrigger should be type: map[string]any, got: %d %#v", i, trigger)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range trigger.(map[string]any) {
			switch k {
			case infraKeyTriggerType:
				_, ok := v.(string)
				if !ok {
					err := fmt.Errorf("infraLambda key %s should be type: string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
			case infraKeyTriggerAttr:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraTrigger key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraTrigger key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraTrigger key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateKeypair(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraKeypair should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, keypair := range val.(map[string]any) {
		_, ok := keypair.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraKeypair should be type: map[string]any, got: %s %#v", name, keypair)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range keypair.(map[string]any) {
			switch k {
			case infraKeyKeypairPubkeyContent:
				_, ok := v.(string)
				if !ok {
					err := fmt.Errorf("infraLambda key %s should be type: string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
			default:
				err := fmt.Errorf("unknown infraKeypair key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateSQS(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraSQS should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, sqsVal := range val.(map[string]any) {
		_, ok := sqsVal.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraSQS should be type: map[string]any, got: %s %#v", name, sqsVal)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range sqsVal.(map[string]any) {
			switch k {
			case infraKeySQSAttr:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraSQS key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraSQS key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraSQS key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateInstanceProfile(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraInstanceProfile should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, instanceProfile := range val.(map[string]any) {
		_, ok := instanceProfile.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraInstanceProfile should be type: map[string]any, got: %s %#v", name, instanceProfile)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range instanceProfile.(map[string]any) {
			switch k {
			case infraKeyInstanceProfileAllow, infraKeyInstanceProfilePolicy:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraInstanceProfile key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraInstanceProfile key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraInstanceProfile key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateSecurityGroup(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraSecurityGroup should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for sgName, infraSg := range val.(map[string]any) {
		_, ok := infraSg.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraSecurityGroup should be type: map[string]any, got: %s %#v", sgName, infraSg)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range infraSg.(map[string]any) {
			switch k {
			case infraKeySecurityGroupRule:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraSecurityGroup key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraSecurityGroup key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraVpc key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateVpc(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraVpc should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, vpcVal := range val.(map[string]any) {
		_, ok := vpcVal.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraVpc should be type: map[string]any, got: %s %#v", name, vpcVal)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range vpcVal.(map[string]any) {
			switch k {
			case infraKeyVpcEC2:
				err := fmt.Errorf("infraVpc will list, but cannot declare ec2 instances. instead manage ec2 with a lambda in the infraset: %#v", v)
				Logger.Println("error:", err)
				return err
			case infraKeyVpcSecurityGroup:
				err := infraParseValidateSecurityGroup(v)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			default:
				err := fmt.Errorf("unknown infraVpc key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func infraParseValidateLambda(val any) error {
	_, ok := val.(map[string]any)
	if !ok {
		err := fmt.Errorf("infraLambda should be type: map[string]any, got: %#v", val)
		Logger.Println("error:", err)
		return err
	}
	for name, lambdaVal := range val.(map[string]any) {
		_, ok := lambdaVal.(map[string]any)
		if !ok {
			err := fmt.Errorf("infraLambda should be type: map[string]any, got: %s %#v", name, lambdaVal)
			Logger.Println("error:", err)
			return err
		}
		for k, v := range lambdaVal.(map[string]any) {
			switch k {
			case infraKeyLambdaName:
				_, ok := v.(string)
				if !ok {
					err := fmt.Errorf("infraLambda key %s should be type: string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
			case infraKeyLambdaEntrypoint:
				x, ok := v.(string)
				if !ok {
					err := fmt.Errorf("infraLambda key %s should be type: string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				switch {
				case strings.HasSuffix(x, ".go"):
				case strings.HasSuffix(x, ".py"):
				case strings.Contains(x, ".dkr.ecr."):
				default:
					err := fmt.Errorf("infraLambda key %s should be *.py, *.go, or ecr container uri, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
			case infraKeyLambdaTrigger:
				err := infraParseValidateTrigger(v)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			case infraKeyLambdaPolicy, infraKeyLambdaAllow, infraKeyLambdaInclude, infraKeyLambdaRequire, infraKeyLambdaEnv, infraKeyLambdaAttr:
				xs, ok := v.([]any)
				if !ok {
					err := fmt.Errorf("infraLambda key %s should be type: []string, got: %#v", k, v)
					Logger.Println("error:", err)
					return err
				}
				for _, x := range xs {
					_, ok := x.(string)
					if !ok {
						err := fmt.Errorf("infraLambda key %s should be type: []string, got: %#v", k, v)
						Logger.Println("error:", err)
						return err
					}
				}
			default:
				err := fmt.Errorf("unknown infraLambda key: %s: %v", k, v)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func InfraParse(yamlPath string) (*InfraSet, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	resolved, err := resolveEnvVars(string(data), []string{lambdaEnvVarApiID, lambdaEnvVarWebsocketID})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	data = []byte(resolved)
	val := map[string]any{}
	err = yaml.Unmarshal(data, &val)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for k, v := range val {
		switch k {
		case infraKeyName:
			if v == "" {
				err := fmt.Errorf("infraSet name cannot be empty")
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeyLambda:
			err := infraParseValidateLambda(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeyS3:
			err := infraParseValidateS3(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeyDynamoDB:
			err := infraParseValidateDynamoDB(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeySqs:
			err := infraParseValidateSQS(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeyVpc:
			err := infraParseValidateVpc(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeyInstanceProfile:
			err := infraParseValidateInstanceProfile(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		case infraKeyKeypair:
			err := infraParseValidateKeypair(v)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		default:
			err := fmt.Errorf("unknown infra key: %s: %v", k, v)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	data, err = yaml.Marshal(val)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	infraSet := &InfraSet{}
	err = yaml.Unmarshal(data, &infraSet)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	yamlPath, err = filepath.Abs(yamlPath)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, infraLambda := range infraSet.Lambda {
		infraLambda.infraSetName = infraSet.Name
		infraLambda.dir = path.Dir(yamlPath)
		if infraLambda.Entrypoint == "" {
			err := fmt.Errorf("missing entrypoint, see examples")
			Logger.Println("error:", err)
			return nil, err
		}
		if !strings.Contains(infraLambda.Entrypoint, ".dkr.ecr.") {
			infraLambda.Entrypoint = path.Join(infraLambda.dir, infraLambda.Entrypoint)
		}
		for _, attr := range infraLambda.Attr {
			k, v, err := SplitOnce(attr, "=")
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			validAttrs := []string{lambdaAttrConcurrency, lambdaAttrMemory, lambdaAttrTimeout, lambdaAttrLogsTTLDays}
			if !slices.Contains(validAttrs, k) {
				err := fmt.Errorf("unknown attr: %s", k)
				Logger.Println("error:", err)
				return nil, err
			}
			if !IsDigit(v) {
				err := fmt.Errorf("conf value should be digits: %s %s", k, v)
				Logger.Println("error:", err)
				return nil, err
			}
		}
		for _, trigger := range infraLambda.Trigger {
			validTriggers := []string{lambdaTriggerSQS, lambdaTrigerS3, lambdaTriggerDynamoDB, lambdaTriggerApi, lambdaTriggerEcr, lambdaTriggerSchedule, lambdaTriggerWebsocket, lambdaTriggerSes, lambdaTriggerUrl}
			if !slices.Contains(validTriggers, trigger.Type) {
				err := fmt.Errorf("unknown trigger: %#v", trigger)
				Logger.Println("error:", err)
				return nil, err
			}
		}
	}
	return infraSet, nil
}

func InfraDelete(ctx context.Context, infraSet *InfraSet, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "InfraDelete"}
		d.Start()
		defer d.End()
	}
	for vpcName := range infraSet.Vpc {
		err := VpcRm(ctx, vpcName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	for profileName := range infraSet.InstanceProfile {
		err := IamDeleteInstanceProfile(ctx, profileName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	for keypairName := range infraSet.Keypair {
		err := EC2DeleteKeypair(ctx, keypairName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	for lambdaName, infraLambda := range infraSet.Lambda {
		infraLambda.Name = lambdaName
		infraLambda.Arn, _ = LambdaArn(ctx, lambdaName)
		infraLambda.Trigger = nil
		_, err := LambdaEnsureTriggerApi(ctx, infraLambda, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = LambdaEnsureTriggerSes(ctx, infraLambda, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if infraLambda.Arn != "" {
			_, err := LambdaEnsureTriggerS3(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = LambdaEnsureTriggerSes(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = LambdaEnsureTriggerEcr(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = LambdaEnsureTriggerSchedule(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = LambdaEnsureTriggerDynamoDB(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = LambdaEnsureTriggerSQS(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		err = IamDeleteRole(ctx, lambdaName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaDeleteFunction(ctx, lambdaName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LogsDeleteGroup(ctx, "/aws/lambda/"+lambdaName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	for bucketName := range infraSet.S3 {
		err := S3DeleteBucket(ctx, bucketName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	for tableName := range infraSet.DynamoDB {
		err := DynamoDBDeleteTable(ctx, tableName, preview, false)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	for queueName := range infraSet.SQS {
		err := SQSDeleteQueue(ctx, queueName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}
