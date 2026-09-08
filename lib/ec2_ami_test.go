package lib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type ec2AmiTestTransport func(*http.Request) (*http.Response, error)

func (fn ec2AmiTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

const amiTestID = "ami-00000000000000001"
const amiTestSnapshot = "snap-00000000000000001"
const amiTestShared = "snap-00000000000000002"
const amiTestDecoy = "snap-00000000000000003"

func amiTestImage(id string, snapshots ...string) string {
	body := "<item><imageId>" + id + "</imageId><imageOwnerId>123456789012</imageOwnerId><imageState>available</imageState><blockDeviceMapping>"
	for _, snapshot := range snapshots {
		body += "<item><ebs><snapshotId>" + snapshot + "</snapshotId></ebs></item>"
	}
	return body + "</blockDeviceMapping></item>"
}

func amiTestResult(id, code string) string {
	return "<item><snapshotId>" + id + "</snapshotId><returnCode>" + code + "</returnCode></item>"
}

func installEC2AmiTestClient(t *testing.T, respond func(string, url.Values) (string, int)) *[]string {
	t.Helper()
	var calls []string
	oldClient := ec2Client
	ec2Client = ec2.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""), RetryMaxAttempts: 1,
		HTTPClient: &http.Client{Transport: ec2AmiTestTransport(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			action := r.Form.Get("Action")
			calls = append(calls, action)
			body, status := respond(action, r.Form)
			if status == 0 {
				return nil, fmt.Errorf("unexpected API call: %s %v", action, r.Form)
			}
			if status == http.StatusOK {
				body = "<" + action + "Response>" + body + "</" + action + "Response>"
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})},
	})
	t.Cleanup(func() { ec2Client = oldClient })
	return &calls
}

func TestEC2DeleteAmiUsesProviderAssociations(t *testing.T) {
	for _, preview := range []bool{true, false} {
		t.Run(fmt.Sprint(preview), func(t *testing.T) {
			var logs bytes.Buffer
			oldLogger := Logger
			Logger = &LoggerStruct{Print: func(args ...any) { _, _ = fmt.Fprint(&logs, args...) }, Flush: func() {}}
			t.Cleanup(func() { Logger = oldLogger })
			calls := installEC2AmiTestClient(t, func(action string, params url.Values) (string, int) {
				switch action {
				case "DescribeImages":
					if params.Get("ImageId.1") != amiTestID || params.Get("Owner.1") != "self" || params.Get("IncludeDisabled") != "true" || params.Get("IncludeDeprecated") != "true" || params.Has("Filter.1.Name") {
						t.Errorf("unscoped image lookup: %v", params)
					}
					// Repeated device mappings are one physical snapshot.
					return "<imagesSet>" + amiTestImage(amiTestID, amiTestSnapshot, amiTestShared, amiTestSnapshot) + "</imagesSet>", 200
				case "DeregisterImage":
					if preview || params.Get("ImageId") != amiTestID || params.Get("DeleteAssociatedSnapshots") != "true" {
						t.Errorf("unsafe deregistration: %v", params)
					}
					return "<return>true</return><deleteSnapshotResultSet>" + amiTestResult(amiTestSnapshot, "success") + amiTestResult(amiTestShared, "skipped") + "</deleteSnapshotResultSet>", 200
				default:
					return "", 0
				}
			})
			if err := EC2DeleteAmi(context.Background(), amiTestID, preview); err != nil {
				t.Fatal(err)
			}
			expected := []string{"DescribeImages"}
			if !preview {
				expected = append(expected, "DeregisterImage")
				if !strings.Contains(logs.String(), "deleted backing snapshot: "+amiTestSnapshot) || !strings.Contains(logs.String(), "preserved shared snapshot: "+amiTestShared) {
					t.Fatalf("lost per-snapshot outcome: %s", &logs)
				}
			} else if !strings.Contains(logs.String(), "preview: deregister: "+amiTestID) || strings.Count(logs.String(), amiTestSnapshot) != 1 {
				t.Fatalf("incorrect read-only preview: %s", &logs)
			}
			if !slices.Equal(*calls, expected) || strings.Contains(logs.String(), amiTestDecoy) {
				t.Fatalf("description-based scan or manual deletion reached provider: %v %s", *calls, &logs)
			}
		})
	}
}

func TestEC2DeleteAmiReportsEverySnapshotFailure(t *testing.T) {
	for name, body := range map[string]string{
		"permission": amiTestResult(amiTestSnapshot, "missing-permissions"),
		"internal":   amiTestResult(amiTestSnapshot, "internal-error"),
		"client":     amiTestResult(amiTestSnapshot, "client-error"),
		"unknown":    amiTestResult(amiTestSnapshot, "future-error"),
		"missing":    "", "unrelated": amiTestResult(amiTestDecoy, "success"),
		"duplicate": amiTestResult(amiTestSnapshot, "success") + amiTestResult(amiTestSnapshot, "success"),
	} {
		t.Run(name, func(t *testing.T) {
			installEC2AmiTestClient(t, func(action string, _ url.Values) (string, int) {
				switch action {
				case "DescribeImages":
					return "<imagesSet>" + amiTestImage(amiTestID, amiTestSnapshot, amiTestShared) + "</imagesSet>", 200
				case "DeregisterImage":
					return "<return>true</return><deleteSnapshotResultSet>" + body + amiTestResult(amiTestShared, "skipped") + "</deleteSnapshotResultSet>", 200
				default:
					return "", 0
				}
			})
			if err := EC2DeleteAmi(context.Background(), amiTestID, false); err == nil || !strings.Contains(err.Error(), amiTestID) {
				t.Fatalf("unconfirmed snapshot deletion must fail with AMI identity: %v", err)
			}
		})
	}
}

func TestEC2DeleteAmiRefusesInvalidMetadata(t *testing.T) {
	for name, body := range map[string]string{
		"wrong-id":        "<imagesSet>" + amiTestImage("ami-00000000000000002", amiTestSnapshot) + "</imagesSet>",
		"duplicate":       "<imagesSet>" + amiTestImage(amiTestID) + amiTestImage(amiTestID) + "</imagesSet>",
		"truncated":       "<nextToken>remaining</nextToken>",
		"missing-owner":   "<imagesSet><item><imageId>" + amiTestID + "</imageId></item></imagesSet>",
		"bad-snapshot-id": "<imagesSet>" + amiTestImage(amiTestID, "invalid") + "</imagesSet>",
	} {
		t.Run(name, func(t *testing.T) {
			calls := installEC2AmiTestClient(t, func(action string, _ url.Values) (string, int) {
				if action != "DescribeImages" {
					return "", 0
				}
				return body, 200
			})
			if err := EC2DeleteAmi(context.Background(), amiTestID, false); err == nil || !slices.Equal(*calls, []string{"DescribeImages"}) {
				t.Fatalf("invalid metadata allowed a write: error=%v calls=%v", err, *calls)
			}
		})
	}
}

func TestEC2DeleteAmiValidatesBeforeReadsAndPropagatesDenials(t *testing.T) {
	calls := installEC2AmiTestClient(t, func(action string, _ url.Values) (string, int) {
		return `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>fixture denied</Message></Error></Errors></Response>`, 403
	})
	for _, id := range []string{"", " ", "*", "ami-*", "ami-foo", "i-00000000000000001"} {
		if err := EC2DeleteAmi(context.Background(), id, false); err == nil || len(*calls) != 0 {
			t.Fatalf("invalid ID reached provider: %q error=%v calls=%v", id, err, *calls)
		}
	}
	for _, preview := range []bool{true, false} {
		if err := EC2DeleteAmi(context.Background(), amiTestID, preview); err == nil || !strings.Contains(err.Error(), "UnauthorizedOperation") {
			t.Fatalf("denial became absence: %v", err)
		}
	}
}

func TestEC2DeleteAmiWithoutSnapshotsStillChecksReturn(t *testing.T) {
	for _, result := range []string{"true", "false", ""} {
		t.Run(result, func(t *testing.T) {
			installEC2AmiTestClient(t, func(action string, _ url.Values) (string, int) {
				switch action {
				case "DescribeImages":
					return "<imagesSet>" + amiTestImage(amiTestID) + "</imagesSet>", 200
				case "DeregisterImage":
					if result == "" {
						return "", 200
					}
					return "<return>" + result + "</return>", 200
				default:
					return "", 0
				}
			})
			if err := EC2DeleteAmi(context.Background(), amiTestID, false); (err == nil) != (result == "true") {
				t.Fatalf("unconfirmed deregistration result %q: %v", result, err)
			}
		})
	}
}

func TestEC2DeleteAmiAbsentAndCanceled(t *testing.T) {
	for name, body := range map[string]string{
		"empty":        "<imagesSet/>",
		"deregistered": strings.ReplaceAll("<imagesSet>"+amiTestImage(amiTestID)+"</imagesSet>", "available", "deregistered"),
		"not-found":    `<Response><Errors><Error><Code>InvalidAMIID.NotFound</Code><Message>fixture missing</Message></Error></Errors></Response>`,
	} {
		t.Run(name, func(t *testing.T) {
			calls := installEC2AmiTestClient(t, func(action string, _ url.Values) (string, int) {
				if action != "DescribeImages" {
					return "", 0
				}
				if name == "not-found" {
					return body, 400
				}
				return body, 200
			})
			for _, preview := range []bool{true, false} {
				if err := EC2DeleteAmi(context.Background(), amiTestID, preview); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(*calls, []string{"DescribeImages", "DescribeImages"}) {
				t.Fatalf("absent AMI caused mutation: %v", *calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := installEC2AmiTestClient(t, func(action string, _ url.Values) (string, int) {
		if action != "DescribeImages" {
			return "", 0
		}
		cancel()
		return "<imagesSet>" + amiTestImage(amiTestID, amiTestSnapshot) + "</imagesSet>", 200
	})
	if err := EC2DeleteAmi(ctx, amiTestID, false); !errors.Is(err, context.Canceled) || !slices.Equal(*calls, []string{"DescribeImages"}) {
		t.Fatalf("cancellation must prevent deregistration: error=%v calls=%v", err, *calls)
	}
}
