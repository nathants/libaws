package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

type ec2SpotFleetTestTransport func(*http.Request) (*http.Response, error)

func (fn ec2SpotFleetTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func installEC2SpotFleetTestClient(t *testing.T, respond func(context.Context, string, url.Values) (string, int)) {
	t.Helper()
	oldEC2, oldIAM := ec2Client, iamClient
	cfg := aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		HTTPClient: &http.Client{Transport: ec2SpotFleetTestTransport(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			action := r.Form.Get("Action")
			body, status := respond(r.Context(), action, r.Form)
			if body == "" {
				return nil, fmt.Errorf("unexpected API call: %s", action)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}, RetryMaxAttempts: 1,
	}
	ec2Client = ec2.NewFromConfig(cfg)
	iamClient = iam.NewFromConfig(cfg)
	t.Cleanup(func() { ec2Client, iamClient = oldEC2, oldIAM })
}

const spotFleetTestID = "sfr-01234567-89ab-cdef-0123-456789abcdef"
const spotFleetTestInstanceID = "i-0123456789abcdef0"
const spotFleetTestInstances = `<DescribeSpotFleetInstancesResponse><activeInstanceSet><item><instanceId>` + spotFleetTestInstanceID + `</instanceId><instanceType>t3.small</instanceType></item></activeInstanceSet></DescribeSpotFleetInstancesResponse>`
const spotFleetTestEmptyInstances = `<DescribeSpotFleetInstancesResponse><activeInstanceSet/></DescribeSpotFleetInstancesResponse>`

func spotFleetTestState(state string) string {
	return `<DescribeSpotFleetRequestsResponse><spotFleetRequestConfigSet><item><spotFleetRequestId>` + spotFleetTestID + `</spotFleetRequestId><spotFleetRequestState>` + state + `</spotFleetRequestState></item></spotFleetRequestConfigSet></DescribeSpotFleetRequestsResponse>`
}

func spotFleetTestCancelResult(id, state string) string {
	return `<CancelSpotFleetRequestsResponse><successfulFleetRequestSet><item><spotFleetRequestId>` + id + `</spotFleetRequestId><currentSpotFleetRequestState>` + state + `</currentSpotFleetRequestState></item></successfulFleetRequestSet></CancelSpotFleetRequestsResponse>`
}

func spotFleetTestCancelFailure(code, message string) string {
	return `<CancelSpotFleetRequestsResponse><unsuccessfulFleetRequestSet><item><spotFleetRequestId>` + spotFleetTestID + `</spotFleetRequestId><error><code>` + code + `</code><message>` + message + `</message></error></item></unsuccessfulFleetRequestSet></CancelSpotFleetRequestsResponse>`
}

func spotFleetTestDescribeInstance(state string) string {
	return `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>` + spotFleetTestInstanceID + `</instanceId><instanceState><name>` + state + `</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`
}

func TestEC2TeardownSpotFleetChecksCancellationResult(t *testing.T) {
	for name, response := range map[string]string{
		"rejected":    spotFleetTestCancelFailure("unexpectedError", "cancel rejected by EC2"),
		"missing":     `<CancelSpotFleetRequestsResponse/>`,
		"wrong-id":    spotFleetTestCancelResult("sfr-unrelated", "cancelled"),
		"wrong-state": spotFleetTestCancelResult(spotFleetTestID, "active"),
	} {
		t.Run(name, func(t *testing.T) {
			installEC2SpotFleetTestClient(t, func(ctx context.Context, action string, form url.Values) (string, int) {
				switch action {
				case "CancelSpotFleetRequests":
					return response, 200
				case "DescribeSpotFleetRequests":
					return spotFleetTestState("active"), 200
				case "DescribeSpotFleetInstances":
					return spotFleetTestEmptyInstances, 200
				}
				return "", 0
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := EC2TeardownSpotFleet(ctx, aws.String(spotFleetTestID))
			if err == nil || !strings.Contains(err.Error(), spotFleetTestID) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unconfirmed cancellation must return the fleet error: %v", err)
			}
			if name == "rejected" && !strings.Contains(err.Error(), "cancel rejected by EC2") {
				t.Fatalf("provider rejection was lost: %v", err)
			}
		})
	}
}

func TestEC2TeardownSpotFleetWaitsForTermination(t *testing.T) {
	for _, initial := range []string{"active", "cancelled_running", "cancelled_terminating"} {
		t.Run(initial, func(t *testing.T) {
			state := initial
			cancellations := 0
			installEC2SpotFleetTestClient(t, func(ctx context.Context, action string, form url.Values) (string, int) {
				switch action {
				case "DescribeSpotFleetRequests":
					if initial == "cancelled_terminating" {
						// AWS can retain this bookkeeping state long after all its
						// instances have terminated and its active list is empty.
						return spotFleetTestState(initial), 200
					}
					return spotFleetTestState(state), 200
				case "CancelSpotFleetRequests":
					cancellations++
					if state == "cancelled_running" {
						return spotFleetTestCancelFailure("fleetRequestNotInCancellableState", "already finalized"), 200
					}
					state = "cancelled_terminating"
					return spotFleetTestCancelResult(spotFleetTestID, state), 200
				case "DescribeSpotFleetInstances":
					return spotFleetTestInstances, 200
				case "TerminateInstances":
					state = "cancelled"
					return `<TerminateInstancesResponse/>`, 200
				case "DescribeInstances":
					// Cancellation completed between listing active fleet instances
					// and describing the instance itself. It can never run again.
					state = "cancelled"
					return spotFleetTestDescribeInstance("terminated"), 200
				}
				return "", 0
			})
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if err := EC2TeardownSpotFleet(ctx, aws.String(spotFleetTestID)); err != nil {
				t.Fatalf("already terminated instance caused teardown failure: %v", err)
			}
			if initial == "cancelled_running" && cancellations != 0 {
				t.Fatalf("attempted to recancel a finalized fleet %d times", cancellations)
			}
		})
	}
}

func TestEC2RequestSpotFleetCleansEveryPostCreateFailure(t *testing.T) {
	for _, failure := range []string{"wait", "finalize", "finalize-result", "fleet-instances", "instances", "empty", "cleanup", "none"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var cancellations []string
			fleetState := "active"
			instanceState := "running"
			instanceReads := 0
			failed := false
			installEC2SpotFleetTestClient(t, func(callCtx context.Context, action string, form url.Values) (string, int) {
				if !failed && ((failure == "fleet-instances" && action == "DescribeSpotFleetInstances" && instanceReads == 1) || (failure == "instances" && action == "DescribeInstances")) {
					failed = true
					cancel()
					return "", 0
				}
				switch action {
				case "GetRole":
					return `<GetRoleResponse><GetRoleResult><Role><Arn>arn:aws:iam::123456789012:role/fleet</Arn></Role></GetRoleResult></GetRoleResponse>`, 200
				case "RequestSpotFleet":
					return `<RequestSpotFleetResponse><spotFleetRequestId>` + spotFleetTestID + `</spotFleetRequestId></RequestSpotFleetResponse>`, 200
				case "DescribeSpotFleetRequests":
					if failure == "wait" && fleetState == "active" {
						return spotFleetTestState("failed"), 200
					}
					return spotFleetTestState(fleetState), 200
				case "DescribeSpotFleetRequestHistory":
					return `<DescribeSpotFleetRequestHistoryResponse><historyRecordSet/></DescribeSpotFleetRequestHistoryResponse>`, 200
				case "DescribeSpotFleetInstances":
					instanceReads++
					if failure == "empty" && instanceReads == 2 {
						return spotFleetTestEmptyInstances, 200
					}
					return spotFleetTestInstances, 200
				case "CancelSpotFleetRequests":
					terminate := form.Get("TerminateInstances")
					cancellations = append(cancellations, terminate)
					if terminate == "false" && (failure == "finalize" || failure == "cleanup") {
						return `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>finalization rejected</Message></Error></Errors></Response>`, 403
					}
					if terminate == "false" && failure == "finalize-result" {
						return spotFleetTestCancelFailure("unexpectedError", "finalization rejected"), 200
					}
					if terminate == "true" {
						deadline, bounded := callCtx.Deadline()
						if !bounded || time.Until(deadline) > 6*time.Minute || callCtx.Err() != nil {
							t.Error("cleanup needs its own bounded, uncanceled context")
						}
						if failure == "cleanup" {
							return spotFleetTestCancelFailure("unexpectedError", "cleanup rejected"), 200
						}
						fleetState = "cancelled_terminating"
					} else {
						fleetState = "cancelled_running"
					}
					return spotFleetTestCancelResult(spotFleetTestID, fleetState), 200
				case "TerminateInstances":
					instanceState, fleetState = "terminated", "cancelled"
					return `<TerminateInstancesResponse/>`, 200
				case "DescribeInstances":
					if form.Get("InstanceId.1") == "" {
						t.Error("launch must never describe unrelated account instances")
					}
					return spotFleetTestDescribeInstance(instanceState), 200
				}
				return "", 0
			})
			instances, err := EC2RequestSpotFleet(ctx, ec2types.AllocationStrategyLowestPrice, &EC2Config{
				NumInstances: 1, Name: "test-ec2", UserName: "admin", Key: "test-keypair", SgID: "sg-test", AmiID: "ami-test",
				InstanceType: ec2types.InstanceTypeT3Small, SubnetIds: []string{"subnet-test"}, Gigs: 8,
			})
			if failure == "none" {
				if err != nil || len(instances) != 1 || aws.ToString(instances[0].InstanceId) != spotFleetTestInstanceID || !slices.Equal(cancellations, []string{"false"}) || instanceState != "running" {
					t.Fatalf("successful launch was not retained: instances=%v err=%v cancellations=%v state=%s", instances, err, cancellations, instanceState)
				}
				return
			}
			if err == nil || len(instances) != 0 || instanceState != "terminated" {
				t.Fatalf("post-create failure abandoned instances: err=%v instances=%v state=%s cancellations=%v", err, instances, instanceState, cancellations)
			}
			if failure == "cleanup" && (!strings.Contains(err.Error(), "finalization rejected") || !strings.Contains(err.Error(), "cleanup rejected")) {
				t.Fatalf("launch or cleanup error was lost: %v", err)
			}
			if (failure == "fleet-instances" || failure == "instances") && !errors.Is(err, context.Canceled) {
				t.Fatalf("caller cancellation was lost: %v", err)
			}
		})
	}
}

func TestEC2ExampleDiscoveryPreservesOwnedPages(t *testing.T) {
	key := "test-keypair-abcdef123456"
	installEC2SpotFleetTestClient(t, func(ctx context.Context, action string, form url.Values) (string, int) {
		if form.Get("NextToken") != "" {
			return `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>second page denied</Message></Error></Errors></Response>`, 403
		}
		switch action {
		case "DescribeSpotFleetRequests":
			item := func(id string, keys ...string) string {
				var specs string
				for _, name := range keys {
					specs += "<item><keyName>" + name + "</keyName></item>"
				}
				return "<item><spotFleetRequestId>" + id + "</spotFleetRequestId><spotFleetRequestConfig><launchSpecifications>" + specs + "</launchSpecifications></spotFleetRequestConfig></item>"
			}
			return `<DescribeSpotFleetRequestsResponse><nextToken>more</nextToken><spotFleetRequestConfigSet>` + item(spotFleetTestID, key) + item("sfr-mixed", key, "foreign") + item("sfr-empty") + item("sfr-foreign", "foreign") + `</spotFleetRequestConfigSet></DescribeSpotFleetRequestsResponse>`, 200
		case "DescribeInstances":
			if form.Get("Filter.1.Name") != "key-name" || form.Get("Filter.1.Value.1") != key {
				t.Fatalf("instance discovery escaped its fixture: %v", form)
			}
			return strings.Replace(spotFleetTestDescribeInstance("running"), "<reservationSet>", "<nextToken>more</nextToken><reservationSet>", 1), 200
		}
		return "", 0
	})
	fleets, err := ec2ExampleOwnedFleets(context.Background(), key)
	if err == nil || !strings.Contains(err.Error(), "second page denied") || len(fleets) != 1 || aws.ToString(fleets[0].SpotFleetRequestId) != spotFleetTestID {
		t.Fatalf("partial owned fleets or error lost: %v %v", fleets, err)
	}
	instances, err := ec2ExampleOwnedInstances(context.Background(), key)
	if err == nil || !strings.Contains(err.Error(), "second page denied") || len(instances) != 1 || aws.ToString(instances[0].InstanceId) != spotFleetTestInstanceID {
		t.Fatalf("partial owned instances or error lost: %v %v", instances, err)
	}
}

func TestEC2TeardownSpotFleetWaitsForLateInstances(t *testing.T) {
	state := "active"
	reads := 0
	terminated := false
	installEC2SpotFleetTestClient(t, func(ctx context.Context, action string, form url.Values) (string, int) {
		switch action {
		case "DescribeSpotFleetRequests":
			return spotFleetTestState(state), 200
		case "CancelSpotFleetRequests":
			state = "cancelled_terminating"
			return spotFleetTestCancelResult(spotFleetTestID, state), 200
		case "DescribeSpotFleetInstances":
			reads++
			if reads == 1 {
				return spotFleetTestEmptyInstances, 200
			}
			return spotFleetTestInstances, 200
		case "TerminateInstances":
			terminated = true
			state = "cancelled"
			return `<TerminateInstancesResponse/>`, 200
		case "DescribeInstances":
			return spotFleetTestDescribeInstance("terminated"), 200
		}
		return "", 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := EC2TeardownSpotFleet(ctx, aws.String(spotFleetTestID)); err != nil || !terminated || reads < 2 {
		t.Fatalf("late instance escaped teardown: err=%v terminated=%t reads=%d", err, terminated, reads)
	}
}
