package lib

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type lambdaEventSourceMappingClientStub struct {
	create func(context.Context, *lambda.CreateEventSourceMappingInput) (*lambda.CreateEventSourceMappingOutput, error)
	delete func(context.Context, *lambda.DeleteEventSourceMappingInput) (*lambda.DeleteEventSourceMappingOutput, error)
	get    func(context.Context, *lambda.GetEventSourceMappingInput) (*lambda.GetEventSourceMappingOutput, error)
	list   func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error)
	update func(context.Context, *lambda.UpdateEventSourceMappingInput) (*lambda.UpdateEventSourceMappingOutput, error)
}

func (stub *lambdaEventSourceMappingClientStub) CreateEventSourceMapping(ctx context.Context, input *lambda.CreateEventSourceMappingInput, _ ...func(*lambda.Options)) (*lambda.CreateEventSourceMappingOutput, error) {
	if stub.create == nil {
		return nil, errors.New("unexpected CreateEventSourceMapping call")
	}
	return stub.create(ctx, input)
}

func (stub *lambdaEventSourceMappingClientStub) DeleteEventSourceMapping(ctx context.Context, input *lambda.DeleteEventSourceMappingInput, _ ...func(*lambda.Options)) (*lambda.DeleteEventSourceMappingOutput, error) {
	if stub.delete == nil {
		return nil, errors.New("unexpected DeleteEventSourceMapping call")
	}
	return stub.delete(ctx, input)
}

func (stub *lambdaEventSourceMappingClientStub) GetEventSourceMapping(ctx context.Context, input *lambda.GetEventSourceMappingInput, _ ...func(*lambda.Options)) (*lambda.GetEventSourceMappingOutput, error) {
	if stub.get == nil {
		return nil, errors.New("unexpected GetEventSourceMapping call")
	}
	return stub.get(ctx, input)
}

func (stub *lambdaEventSourceMappingClientStub) ListEventSourceMappings(ctx context.Context, input *lambda.ListEventSourceMappingsInput, _ ...func(*lambda.Options)) (*lambda.ListEventSourceMappingsOutput, error) {
	if stub.list == nil {
		return nil, errors.New("unexpected ListEventSourceMappings call")
	}
	return stub.list(ctx, input)
}

func (stub *lambdaEventSourceMappingClientStub) UpdateEventSourceMapping(ctx context.Context, input *lambda.UpdateEventSourceMappingInput, _ ...func(*lambda.Options)) (*lambda.UpdateEventSourceMappingOutput, error) {
	if stub.update == nil {
		return nil, errors.New("unexpected UpdateEventSourceMapping call")
	}
	return stub.update(ctx, input)
}

func testLambdaDynamoDBInfra(table, functionARN string) *InfraLambda {
	return &InfraLambda{
		Name: "function",
		Arn:  functionARN,
		Trigger: []*InfraTrigger{{
			Type: lambdaTriggerDynamoDB,
			Attr: []string{table, "start=trim_horizon"},
		}},
	}
}

func testLambdaDynamoDBMapping(uuid, streamARN, functionARN, state string) lambdatypes.EventSourceMappingConfiguration {
	return lambdatypes.EventSourceMappingConfiguration{
		UUID:                           aws.String(uuid),
		EventSourceArn:                 aws.String(streamARN),
		FunctionArn:                    aws.String(functionARN),
		State:                          aws.String(state),
		BatchSize:                      aws.Int32(100),
		MaximumBatchingWindowInSeconds: aws.Int32(0),
		MaximumRetryAttempts:           aws.Int32(-1),
		ParallelizationFactor:          aws.Int32(1),
		StartingPosition:               lambdatypes.EventSourcePositionTrimHorizon,
	}
}

func testLambdaDynamoDBGetOutput(mapping lambdatypes.EventSourceMappingConfiguration) *lambda.GetEventSourceMappingOutput {
	return &lambda.GetEventSourceMappingOutput{
		UUID:                           mapping.UUID,
		EventSourceArn:                 mapping.EventSourceArn,
		FunctionArn:                    mapping.FunctionArn,
		State:                          mapping.State,
		BatchSize:                      mapping.BatchSize,
		MaximumBatchingWindowInSeconds: mapping.MaximumBatchingWindowInSeconds,
		MaximumRetryAttempts:           mapping.MaximumRetryAttempts,
		ParallelizationFactor:          mapping.ParallelizationFactor,
		StartingPosition:               mapping.StartingPosition,
	}
}

func TestDynamoDBLatestStreamARNRejectsStaleARNWhenStreamDisabled(t *testing.T) {
	streamARN := aws.String("arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/stale")
	for _, test := range []struct {
		name                string
		streamSpecification *ddbtypes.StreamSpecification
	}{
		{name: "missing stream specification"},
		{
			name: "explicitly disabled stream",
			streamSpecification: &ddbtypes.StreamSpecification{
				StreamEnabled: aws.Bool(false),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := dynamoDBLatestStreamARN("table", &ddbtypes.TableDescription{
				LatestStreamArn:     streamARN,
				StreamSpecification: test.streamSpecification,
			})
			if err == nil {
				t.Fatal("stale LatestStreamArn was accepted while streaming was disabled")
			}
		})
	}
}

func TestLambdaEnsureTriggerDynamoDBPreviewMissingTableMakesNoLambdaCalls(t *testing.T) {
	calls := 0
	unexpected := errors.New("unexpected Lambda call")
	client := &lambdaEventSourceMappingClientStub{
		create: func(context.Context, *lambda.CreateEventSourceMappingInput) (*lambda.CreateEventSourceMappingOutput, error) {
			calls++
			return nil, unexpected
		},
		delete: func(context.Context, *lambda.DeleteEventSourceMappingInput) (*lambda.DeleteEventSourceMappingOutput, error) {
			calls++
			return nil, unexpected
		},
		get: func(context.Context, *lambda.GetEventSourceMappingInput) (*lambda.GetEventSourceMappingOutput, error) {
			calls++
			return nil, unexpected
		},
		list: func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error) {
			calls++
			return nil, unexpected
		},
		update: func(context.Context, *lambda.UpdateEventSourceMappingInput) (*lambda.UpdateEventSourceMappingOutput, error) {
			calls++
			return nil, unexpected
		},
	}
	missing := &ddbtypes.ResourceNotFoundException{Message: aws.String("missing table")}
	err := lambdaEnsureTriggerDynamoDB(context.Background(), client, func(context.Context, string) (string, error) {
		return "", fmt.Errorf("describe table: %w", missing)
	}, testLambdaDynamoDBInfra("missing", "arn:aws:lambda:us-west-2:123456789012:function:function"), true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("preview made %d Lambda calls for an unresolved table", calls)
	}
}

func TestLambdaEnsureTriggerDynamoDBPreviewMissingFunctionTreatsMappingsAsEmpty(t *testing.T) {
	listCalls := 0
	client := &lambdaEventSourceMappingClientStub{
		list: func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error) {
			listCalls++
			return nil, &lambdatypes.ResourceNotFoundException{Message: aws.String("missing function")}
		},
	}
	infraLambda := &InfraLambda{Name: "missing-function"}
	err := lambdaEnsureTriggerDynamoDB(context.Background(), client, func(context.Context, string) (string, error) {
		t.Fatal("resolved a stream without a DynamoDB trigger")
		return "", nil
	}, infraLambda, true)
	if err != nil {
		t.Fatal(err)
	}
	if listCalls != 1 {
		t.Fatalf("ListEventSourceMappings calls = %d, want 1", listCalls)
	}
}

func TestLambdaEnsureTriggerDynamoDBResolvesEveryStreamBeforeLambdaCalls(t *testing.T) {
	calls := 0
	client := &lambdaEventSourceMappingClientStub{
		create: func(context.Context, *lambda.CreateEventSourceMappingInput) (*lambda.CreateEventSourceMappingOutput, error) {
			calls++
			return &lambda.CreateEventSourceMappingOutput{UUID: aws.String("unexpected")}, nil
		},
		list: func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error) {
			calls++
			return &lambda.ListEventSourceMappingsOutput{}, nil
		},
	}
	infraLambda := testLambdaDynamoDBInfra("resolved", "arn:aws:lambda:us-west-2:123456789012:function:function")
	infraLambda.Trigger = append(infraLambda.Trigger, &InfraTrigger{
		Type: lambdaTriggerDynamoDB,
		Attr: []string{"missing", "start=trim_horizon"},
	})
	var resolvedTables []string
	resolveErr := errors.New("Requested resource not found")
	err := lambdaEnsureTriggerDynamoDB(context.Background(), client, func(_ context.Context, tableName string) (string, error) {
		resolvedTables = append(resolvedTables, tableName)
		if tableName == "resolved" {
			return "arn:aws:dynamodb:us-west-2:123456789012:table/resolved/stream/current", nil
		}
		return "", resolveErr
	}, infraLambda, false)
	if !errors.Is(err, resolveErr) {
		t.Fatalf("error = %v, want stream resolution error", err)
	}
	if len(resolvedTables) != 2 || resolvedTables[0] != "resolved" || resolvedTables[1] != "missing" {
		t.Fatalf("resolved tables = %v, want [resolved missing]", resolvedTables)
	}
	if calls != 0 {
		t.Fatalf("made %d Lambda calls before all streams resolved", calls)
	}
}

func TestLambdaEnsureTriggerDynamoDBReenablesDisabledMapping(t *testing.T) {
	streamARN := "arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/current"
	functionARN := "arn:aws:lambda:us-west-2:123456789012:function:function"
	mapping := testLambdaDynamoDBMapping("current", streamARN, functionARN, "Disabled")
	mapping.BatchSize = aws.Int32(50)
	updated := false
	enabledObserved := false
	getCalls := 0
	client := &lambdaEventSourceMappingClientStub{
		list: func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error) {
			return &lambda.ListEventSourceMappingsOutput{EventSourceMappings: []lambdatypes.EventSourceMappingConfiguration{mapping}}, nil
		},
		get: func(context.Context, *lambda.GetEventSourceMappingInput) (*lambda.GetEventSourceMappingOutput, error) {
			getCalls++
			current := mapping
			if updated {
				enabledObserved = true
				current.State = aws.String("Enabled")
				current.BatchSize = aws.Int32(100)
			}
			return testLambdaDynamoDBGetOutput(current), nil
		},
		update: func(_ context.Context, input *lambda.UpdateEventSourceMappingInput) (*lambda.UpdateEventSourceMappingOutput, error) {
			if input.Enabled == nil || !*input.Enabled {
				t.Fatalf("update Enabled = %v, want true", input.Enabled)
			}
			if aws.ToInt32(input.BatchSize) != 100 {
				t.Fatalf("update BatchSize = %d, want 100", aws.ToInt32(input.BatchSize))
			}
			updated = true
			return &lambda.UpdateEventSourceMappingOutput{UUID: input.UUID, State: aws.String("Enabling")}, nil
		},
	}
	err := lambdaEnsureTriggerDynamoDB(context.Background(), client, func(context.Context, string) (string, error) {
		return streamARN, nil
	}, testLambdaDynamoDBInfra("table", functionARN), false)
	if err != nil {
		t.Fatal(err)
	}
	if !updated || !enabledObserved || getCalls < 2 {
		t.Fatalf("disabled mapping reconciliation: updated=%t enabled observed=%t get calls=%d", updated, enabledObserved, getCalls)
	}
}

func TestLambdaEnsureTriggerDynamoDBWaitsForReplacementBeforeDeletingStale(t *testing.T) {
	currentARN := "arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/current"
	staleARN := "arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/stale"
	functionARN := "arn:aws:lambda:us-west-2:123456789012:function:function"
	stale := testLambdaDynamoDBMapping("stale", staleARN, functionARN, "Enabled")
	unrelated := testLambdaDynamoDBMapping("sqs", "arn:aws:sqs:us-west-2:123456789012:queue", functionARN, "Enabled")
	unrelated.StartingPosition = ""
	replacementEnabled := false
	deletedStale := false
	staleDeletionObserved := false
	client := &lambdaEventSourceMappingClientStub{
		list: func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error) {
			return &lambda.ListEventSourceMappingsOutput{EventSourceMappings: []lambdatypes.EventSourceMappingConfiguration{stale, unrelated}}, nil
		},
		create: func(_ context.Context, input *lambda.CreateEventSourceMappingInput) (*lambda.CreateEventSourceMappingOutput, error) {
			if input.EventSourceArn == nil || *input.EventSourceArn != currentARN {
				t.Fatalf("created stream = %v, want %s", input.EventSourceArn, currentARN)
			}
			return &lambda.CreateEventSourceMappingOutput{UUID: aws.String("current"), State: aws.String("Creating")}, nil
		},
		get: func(_ context.Context, input *lambda.GetEventSourceMappingInput) (*lambda.GetEventSourceMappingOutput, error) {
			switch aws.ToString(input.UUID) {
			case "current":
				replacementEnabled = true
				current := testLambdaDynamoDBMapping("current", currentARN, functionARN, "Enabled")
				return testLambdaDynamoDBGetOutput(current), nil
			case "stale":
				if deletedStale {
					staleDeletionObserved = true
					return nil, &lambdatypes.ResourceNotFoundException{Message: aws.String("gone")}
				}
				return testLambdaDynamoDBGetOutput(stale), nil
			default:
				return nil, errors.New("unexpected mapping UUID")
			}
		},
		delete: func(_ context.Context, input *lambda.DeleteEventSourceMappingInput) (*lambda.DeleteEventSourceMappingOutput, error) {
			if !replacementEnabled {
				t.Fatal("deleted stale mapping before replacement reached Enabled")
			}
			if aws.ToString(input.UUID) != "stale" {
				t.Fatalf("deleted UUID = %q, want stale", aws.ToString(input.UUID))
			}
			deletedStale = true
			return &lambda.DeleteEventSourceMappingOutput{UUID: input.UUID, State: aws.String("Deleting")}, nil
		},
	}
	err := lambdaEnsureTriggerDynamoDB(context.Background(), client, func(context.Context, string) (string, error) {
		return currentARN, nil
	}, testLambdaDynamoDBInfra("table", functionARN), false)
	if err != nil {
		t.Fatal(err)
	}
	if !replacementEnabled || !deletedStale || !staleDeletionObserved {
		t.Fatalf("reconciliation incomplete: replacement enabled=%t stale deleted=%t deletion observed=%t", replacementEnabled, deletedStale, staleDeletionObserved)
	}
}

func TestLambdaEnsureTriggerDynamoDBPreservesStaleMappingWhenReplacementFails(t *testing.T) {
	currentARN := "arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/current"
	staleARN := "arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/stale"
	functionARN := "arn:aws:lambda:us-west-2:123456789012:function:function"
	stale := testLambdaDynamoDBMapping("stale", staleARN, functionARN, "Enabled")
	deleted := false
	client := &lambdaEventSourceMappingClientStub{
		list: func(context.Context, *lambda.ListEventSourceMappingsInput) (*lambda.ListEventSourceMappingsOutput, error) {
			return &lambda.ListEventSourceMappingsOutput{EventSourceMappings: []lambdatypes.EventSourceMappingConfiguration{stale}}, nil
		},
		create: func(context.Context, *lambda.CreateEventSourceMappingInput) (*lambda.CreateEventSourceMappingOutput, error) {
			return &lambda.CreateEventSourceMappingOutput{UUID: aws.String("current"), State: aws.String("Creating")}, nil
		},
		get: func(context.Context, *lambda.GetEventSourceMappingInput) (*lambda.GetEventSourceMappingOutput, error) {
			failed := testLambdaDynamoDBMapping("current", currentARN, functionARN, "Disabled")
			out := testLambdaDynamoDBGetOutput(failed)
			out.StateTransitionReason = aws.String("creation failed")
			return out, nil
		},
		delete: func(context.Context, *lambda.DeleteEventSourceMappingInput) (*lambda.DeleteEventSourceMappingOutput, error) {
			deleted = true
			return &lambda.DeleteEventSourceMappingOutput{}, nil
		},
	}
	err := lambdaEnsureTriggerDynamoDB(context.Background(), client, func(context.Context, string) (string, error) {
		return currentARN, nil
	}, testLambdaDynamoDBInfra("table", functionARN), false)
	if err == nil {
		t.Fatal("replacement failure was accepted")
	}
	if deleted {
		t.Fatal("stale mapping was deleted after replacement failure")
	}
}

func TestLambdaDynamoDBStreamMappingIntegration(t *testing.T) {
	tableName := os.Getenv("LIBAWS_LAMBDA_DYNAMODB_TEST_TABLE")
	outputTableName := os.Getenv("LIBAWS_LAMBDA_DYNAMODB_TEST_OUTPUT_TABLE")
	functionName := os.Getenv("LIBAWS_LAMBDA_DYNAMODB_TEST_FUNCTION")
	if tableName == "" && outputTableName == "" && functionName == "" {
		t.Skip("set LIBAWS_LAMBDA_DYNAMODB_TEST_TABLE, LIBAWS_LAMBDA_DYNAMODB_TEST_OUTPUT_TABLE, and LIBAWS_LAMBDA_DYNAMODB_TEST_FUNCTION")
	}
	if tableName == "" || outputTableName == "" || functionName == "" {
		t.Fatal("all Lambda DynamoDB integration resource names are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	account, err := StsAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if account != os.Getenv("LIBAWS_TEST_ACCOUNT") {
		t.Fatalf("AWS account = %s, want guarded account %s", account, os.Getenv("LIBAWS_TEST_ACCOUNT"))
	}

	initialARN, err := DynamoDBStreamArn(ctx, tableName)
	if err != nil {
		t.Fatal(err)
	}
	initial := requireSingleEnabledDynamoDBMapping(t, ctx, functionName, initialARN)
	functionARN := aws.ToString(initial.FunctionArn)
	infraLambda := &InfraLambda{
		Name: functionName,
		Arn:  functionARN,
		Trigger: []*InfraTrigger{{
			Type: lambdaTriggerDynamoDB,
			Attr: []string{tableName, "start=trim_horizon", "batch=1", "parallel=10", "retry=0", "window=1"},
		}},
	}

	if _, err := LambdaClient().UpdateEventSourceMapping(ctx, &lambda.UpdateEventSourceMappingInput{UUID: initial.UUID, Enabled: aws.Bool(false)}); err != nil {
		t.Fatal(err)
	}
	disabled, err := lambdaWaitEventSourceMappingSettled(ctx, LambdaClient(), aws.ToString(initial.UUID))
	if err != nil {
		t.Fatal(err)
	}
	if disabled == nil || aws.ToString(disabled.state) != "Disabled" {
		t.Fatalf("disabled mapping state = %v, want Disabled", disabled)
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, infraLambda, false); err != nil {
		t.Fatal(err)
	}
	reenabled := requireSingleEnabledDynamoDBMapping(t, ctx, functionName, initialARN)
	if aws.ToString(reenabled.UUID) != aws.ToString(initial.UUID) {
		t.Fatalf("reenable replaced mapping UUID %s with %s", aws.ToString(initial.UUID), aws.ToString(reenabled.UUID))
	}

	if _, err := DynamoDBClient().UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName: tableNamePointer(tableName),
		StreamSpecification: &ddbtypes.StreamSpecification{
			StreamEnabled: aws.Bool(false),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DynamoDBWaitForReady(ctx, tableName); err != nil {
		t.Fatal(err)
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, infraLambda, false); err == nil {
		t.Fatal("missing configured stream was accepted")
	}
	if _, err := LambdaClient().GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{UUID: initial.UUID}); err != nil {
		t.Fatalf("old mapping was removed after stream-resolution failure: %v", err)
	}

	if _, err := DynamoDBClient().UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName: tableNamePointer(tableName),
		StreamSpecification: &ddbtypes.StreamSpecification{
			StreamEnabled:  aws.Bool(true),
			StreamViewType: ddbtypes.StreamViewTypeNewAndOldImages,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DynamoDBWaitForReady(ctx, tableName); err != nil {
		t.Fatal(err)
	}
	currentARN, err := DynamoDBStreamArn(ctx, tableName)
	if err != nil {
		t.Fatal(err)
	}
	if currentARN == initialARN {
		t.Fatal("stream rotation retained the old ARN")
	}
	if err := LambdaEnsureTriggerDynamoDB(ctx, infraLambda, false); err != nil {
		t.Fatal(err)
	}
	current := requireSingleEnabledDynamoDBMapping(t, ctx, functionName, currentARN)
	if _, err := LambdaClient().GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{UUID: initial.UUID}); !lambdaEventSourceMappingNotFound(err) {
		t.Fatalf("stale mapping still exists: %v", err)
	}

	if current.LastModified == nil {
		t.Fatal("current mapping lacks LastModified")
	}
	modified := *current.LastModified
	currentUUID := aws.ToString(current.UUID)
	if err := LambdaEnsureTriggerDynamoDB(ctx, infraLambda, false); err != nil {
		t.Fatal(err)
	}
	converged := requireSingleEnabledDynamoDBMapping(t, ctx, functionName, currentARN)
	if aws.ToString(converged.UUID) != currentUUID || converged.LastModified == nil || !converged.LastModified.Equal(modified) {
		t.Fatalf("second ensure changed converged mapping: before=%s %s after=%s %v", currentUUID, modified, aws.ToString(converged.UUID), converged.LastModified)
	}

	marker := fmt.Sprintf("rotation-%d", time.Now().UnixNano())
	if _, err := DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
		TableName: tableNamePointer(tableName),
		Item: map[string]ddbtypes.AttributeValue{
			"userid":  &ddbtypes.AttributeValueMemberS{Value: marker},
			"version": &ddbtypes.AttributeValueMemberN{Value: "1"},
			"data":    &ddbtypes.AttributeValueMemberS{Value: marker},
		},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		output, err := DynamoDBClient().GetItem(ctx, &dynamodb.GetItemInput{
			TableName: tableNamePointer(outputTableName),
			Key: map[string]ddbtypes.AttributeValue{
				"userid": &ddbtypes.AttributeValueMemberS{Value: marker},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if data, ok := output.Item["data"].(*ddbtypes.AttributeValueMemberS); ok && data.Value == marker {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rotated enabled mapping did not process a DynamoDB stream record")
		}
		time.Sleep(time.Second)
	}
}

func requireSingleEnabledDynamoDBMapping(t *testing.T, ctx context.Context, functionName, streamARN string) lambdatypes.EventSourceMappingConfiguration {
	t.Helper()
	mappings, err := lambdaListEventSourceMappings(ctx, functionName)
	if err != nil {
		t.Fatal(err)
	}
	var dynamoDBMappings []lambdatypes.EventSourceMappingConfiguration
	for _, mapping := range mappings {
		if mapping.EventSourceArn != nil && lambdaDynamoDBStreamARN(*mapping.EventSourceArn) {
			dynamoDBMappings = append(dynamoDBMappings, mapping)
		}
	}
	if len(dynamoDBMappings) != 1 {
		t.Fatalf("DynamoDB mapping count = %d, want 1: %#v", len(dynamoDBMappings), dynamoDBMappings)
	}
	mapping := dynamoDBMappings[0]
	if aws.ToString(mapping.EventSourceArn) != streamARN || aws.ToString(mapping.State) != "Enabled" {
		t.Fatalf("mapping stream/state = %s/%s, want %s/Enabled", aws.ToString(mapping.EventSourceArn), aws.ToString(mapping.State), streamARN)
	}
	return mapping
}

func tableNamePointer(name string) *string {
	return aws.String(name)
}
