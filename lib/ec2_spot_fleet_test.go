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
			installEC2SpotFleetTestClient(t, func(_ context.Context, action string, _ url.Values) (string, int) {
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
			installEC2SpotFleetTestClient(t, func(_ context.Context, action string, _ url.Values) (string, int) {
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

func TestEC2NewInstancesRequiresIMDSv2(t *testing.T) {
	launched := false
	installEC2SpotFleetTestClient(t, func(_ context.Context, action string, form url.Values) (string, int) {
		if action != "RunInstances" {
			return "", 0
		}
		launched = true
		for key, want := range map[string]string{
			"MetadataOptions.HttpTokens":              "required",
			"MetadataOptions.HttpEndpoint":            "enabled",
			"MetadataOptions.HttpPutResponseHopLimit": "1",
		} {
			if form.Get(key) != want {
				t.Errorf("RunInstances %s = %q, want %q", key, form.Get(key), want)
			}
		}
		return `<RunInstancesResponse><instancesSet><item><instanceId>` + spotFleetTestInstanceID + `</instanceId></item></instancesSet></RunInstancesResponse>`, 200
	})
	instances, err := EC2NewInstances(context.Background(), &EC2Config{
		NumInstances: 1, Name: "test-ec2", UserName: "admin", Key: "test-keypair", SgID: "sg-test", AmiID: "ami-test",
		InstanceType: ec2types.InstanceTypeT3Small, SubnetIds: []string{"subnet-a"}, Gigs: 8,
	})
	if err != nil || !launched || len(instances) != 1 {
		t.Fatalf("launch failed: launched=%t instances=%v err=%v", launched, instances, err)
	}
}

func TestEC2RequestSpotFleetCleansEveryPostCreateFailure(t *testing.T) {
	const templateID = "lt-0123456789abcdef0"
	notFound := `<Response><Errors><Error><Code>InvalidLaunchTemplateName.NotFoundException</Code><Message>template not found</Message></Error></Errors></Response>`
	for _, failure := range []string{"template-lost", "template-rejected", "request", "wait", "finalize", "finalize-result", "fleet-instances", "instances", "empty", "cleanup", "template-delete", "template-visibility", "template-absent", "none"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls, cancellations []string
			fleetState := "active"
			instanceState := "" // the fake fleet launches its instance on request
			instanceReads := 0
			failed := false
			templateName, templateState := "", ""
			deletes, finalizedAt := 0, -1
			installEC2SpotFleetTestClient(t, func(callCtx context.Context, action string, form url.Values) (string, int) {
				calls = append(calls, action)
				if !failed && ((failure == "fleet-instances" && action == "DescribeSpotFleetInstances" && instanceReads == 1) || (failure == "instances" && action == "DescribeInstances")) {
					failed = true
					cancel()
					return "", 0
				}
				switch action {
				case "GetRole":
					return `<GetRoleResponse><GetRoleResult><Role><Arn>arn:aws:iam::123456789012:role/fleet</Arn></Role></GetRoleResult></GetRoleResponse>`, 200
				case "CreateLaunchTemplate":
					templateName = form.Get("LaunchTemplateName")
					for key, want := range map[string]string{
						"LaunchTemplateData.MetadataOptions.HttpTokens":              "required",
						"LaunchTemplateData.MetadataOptions.HttpEndpoint":            "enabled",
						"LaunchTemplateData.MetadataOptions.HttpPutResponseHopLimit": "1",
						"LaunchTemplateData.KeyName":                                 "test-keypair",
						"LaunchTemplateData.TagSpecification.1.ResourceType":         "instance",
					} {
						if form.Get(key) != want {
							t.Errorf("launch template %s = %q, want %q", key, form.Get(key), want)
						}
					}
					if form.Get("LaunchTemplateData.UserData") == "" {
						t.Error("launch template has no user data")
					}
					if failure == "template-rejected" {
						return `<Response><Errors><Error><Code>InvalidParameterValue</Code><Message>template rejected</Message></Error></Errors></Response>`, 400
					}
					templateState = "exists"
					if failure == "template-lost" {
						// AWS created the template, but the caller never sees the response.
						cancel()
						return "", 0
					}
					return `<CreateLaunchTemplateResponse><launchTemplate><launchTemplateId>` + templateID + `</launchTemplateId></launchTemplate></CreateLaunchTemplateResponse>`, 200
				case "DeleteLaunchTemplate":
					deletes++
					deadline, bounded := callCtx.Deadline()
					if !bounded || time.Until(deadline) > 2*time.Minute || callCtx.Err() != nil {
						t.Error("template deletion needs its own bounded, uncanceled context")
					}
					if templateName == "" || form.Get("LaunchTemplateName") != templateName {
						t.Errorf("deleted the wrong template: %q, created %q", form.Get("LaunchTemplateName"), templateName)
					}
					if failure == "template-delete" {
						return `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>template deletion rejected</Message></Error></Errors></Response>`, 403
					}
					if failure == "template-absent" {
						// An earlier delete succeeded, but its response was lost.
						templateState = "deleted"
					}
					if templateState != "exists" || (failure == "template-visibility" && deletes == 1) {
						return notFound, 400
					}
					templateState = "deleted"
					return `<DeleteLaunchTemplateResponse/>`, 200
				case "RequestSpotFleet":
					config := "SpotFleetRequestConfig.LaunchTemplateConfigs.1."
					if templateState != "exists" || form.Get(config+"LaunchTemplateSpecification.LaunchTemplateId") != templateID || form.Get(config+"Overrides.1.SubnetId") != "subnet-a" || form.Get(config+"Overrides.2.SubnetId") != "subnet-b" {
						t.Errorf("fleet must launch from the created template in every subnet: %v", form)
					}
					if failure == "request" {
						return `<Response><Errors><Error><Code>InvalidSpotFleetRequestConfig</Code><Message>fleet rejected</Message></Error></Errors></Response>`, 400
					}
					instanceState = "running"
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
						finalizedAt = len(calls) - 1
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
			started := time.Now()
			instances, err := EC2RequestSpotFleet(ctx, ec2types.AllocationStrategyLowestPrice, &EC2Config{
				NumInstances: 1, Name: "test-ec2", UserName: "admin", Key: "test-keypair", SgID: "sg-test", AmiID: "ami-test",
				InstanceType: ec2types.InstanceTypeT3Small, SubnetIds: []string{"subnet-a", "subnet-b"}, Gigs: 8,
			})
			elapsed := time.Since(started)
			// The template may go only once the fleet can no longer launch from it:
			// after its finalization, otherwise after its teardown.
			deletedAt := slices.Index(calls, "DeleteLaunchTemplate")
			if failure != "template-delete" && (templateState == "exists" || deletedAt < 0 || (finalizedAt >= 0 && deletedAt < finalizedAt) || (finalizedAt < 0 && calls[len(calls)-1] != "DeleteLaunchTemplate")) {
				t.Fatalf("template survived or was deleted while the fleet could launch: state=%s calls=%v", templateState, calls)
			}
			if failure == "none" || failure == "template-visibility" || failure == "template-absent" {
				if err != nil || len(instances) != 1 || aws.ToString(instances[0].InstanceId) != spotFleetTestInstanceID || !slices.Equal(cancellations, []string{"false"}) || instanceState != "running" {
					t.Fatalf("successful launch was not retained: instances=%v err=%v cancellations=%v state=%s", instances, err, cancellations, instanceState)
				}
				// Absence is retried only within the visibility window.
				if failure == "template-absent" && (deletes < 2 || elapsed > ec2LaunchTemplateVisibilityTimeout+5*time.Second) {
					t.Fatalf("persistent absence was not accepted after a bounded retry: deletes=%d elapsed=%s", deletes, elapsed)
				}
				return
			}
			if err == nil || len(instances) != 0 {
				t.Fatalf("failure was not returned: err=%v instances=%v", err, instances)
			}
			if failure == "template-lost" || failure == "template-rejected" || failure == "request" {
				if instanceState != "" || len(cancellations) != 0 {
					t.Fatalf("failure before the fleet existed touched compute: state=%s cancellations=%v", instanceState, cancellations)
				}
			} else if instanceState != "terminated" {
				t.Fatalf("post-create failure abandoned instances: err=%v state=%s cancellations=%v", err, instanceState, cancellations)
			}
			// A template that cannot be deleted fails the launch, so its
			// instances are torn down rather than returned.
			if failure == "template-delete" && (!strings.Contains(err.Error(), "template deletion rejected") || deletedAt < finalizedAt || deletedAt > slices.Index(calls, "TerminateInstances")) {
				t.Fatalf("template deletion failure was not a launch failure: err=%v calls=%v", err, calls)
			}
			if failure == "template-rejected" && strings.Contains(err.Error(), "delete launch template") {
				t.Fatalf("an absent template after a rejected create is not a cleanup failure: %v", err)
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
	installEC2SpotFleetTestClient(t, func(_ context.Context, action string, form url.Values) (string, int) {
		if form.Get("NextToken") != "" {
			return `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>second page denied</Message></Error></Errors></Response>`, 403
		}
		switch action {
		case "DescribeLaunchTemplateVersions":
			if form.Get("LaunchTemplateVersion.1") != "$Latest" {
				t.Fatalf("template discovery must read every latest version: %v", form)
			}
			version := func(id, name, keyName string) string {
				return "<item><launchTemplateId>" + id + "</launchTemplateId><launchTemplateName>" + name + "</launchTemplateName><launchTemplateData><keyName>" + keyName + "</keyName></launchTemplateData></item>"
			}
			return `<DescribeLaunchTemplateVersionsResponse><nextToken>more</nextToken><launchTemplateVersionSet>` + version("lt-owned", "owned", key) + version("lt-named", "named", key) + version("lt-foreign", "foreign", "foreign") + `</launchTemplateVersionSet></DescribeLaunchTemplateVersionsResponse>`, 200
		case "DescribeSpotFleetRequests":
			item := func(id string, refs ...string) string {
				var configs string
				for _, ref := range refs {
					field := "launchTemplateId"
					if !strings.HasPrefix(ref, "lt-") {
						field = "launchTemplateName"
					}
					configs += "<item><launchTemplateSpecification><" + field + ">" + ref + "</" + field + "></launchTemplateSpecification></item>"
				}
				return "<item><spotFleetRequestId>" + id + "</spotFleetRequestId><spotFleetRequestConfig><launchTemplateConfigs>" + configs + "</launchTemplateConfigs></spotFleetRequestConfig></item>"
			}
			return `<DescribeSpotFleetRequestsResponse><nextToken>more</nextToken><spotFleetRequestConfigSet>` + item(spotFleetTestID, "lt-owned") + item("sfr-named", "named") + item("sfr-mixed", "lt-owned", "lt-foreign") + item("sfr-empty") + item("sfr-foreign", "lt-foreign") + item("sfr-unknown", "lt-unknown") + `</spotFleetRequestConfigSet></DescribeSpotFleetRequestsResponse>`, 200
		case "DescribeInstances":
			if form.Get("Filter.1.Name") != "key-name" || form.Get("Filter.1.Value.1") != key {
				t.Fatalf("instance discovery escaped its fixture: %v", form)
			}
			return strings.Replace(spotFleetTestDescribeInstance("running"), "<reservationSet>", "<nextToken>more</nextToken><reservationSet>", 1), 200
		}
		return "", 0
	})
	fleets, err := ec2ExampleOwnedFleets(context.Background(), key)
	if err == nil || strings.Count(err.Error(), "second page denied") != 2 || len(fleets) != 2 || aws.ToString(fleets[0].SpotFleetRequestId) != spotFleetTestID || aws.ToString(fleets[1].SpotFleetRequestId) != "sfr-named" {
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
	installEC2SpotFleetTestClient(t, func(_ context.Context, action string, _ url.Values) (string, int) {
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
