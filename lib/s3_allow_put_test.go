package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gofrs/uuid"
)

type s3AllowPutTransport func(*http.Request) (*http.Response, error)

func (transport s3AllowPutTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestS3EnsureScopesAllowPutToSourceAccount(t *testing.T) {
	const account = "123456789012"
	deny := `{"Sid":"DenyInsecureTransport","Effect":"Deny","Principal":"*","Action":"s3:*","Resource":["arn:aws:s3:::fixture","arn:aws:s3:::fixture/*"],"Condition":{"Bool":{"aws:SecureTransport":"false"}}}`
	allowPut := `{"Sid":"allow put from ses.amazonaws.com","Effect":"Allow","Principal":{"Service":"ses.amazonaws.com"},"Action":"s3:PutObject","Resource":"arn:aws:s3:::fixture/*"`
	unscoped := `{"Version":"2012-10-17","Statement":[` + deny + `,` + allowPut + `}]}`
	scoped := `{"Version":"2012-10-17","Statement":[` + deny + `,` + allowPut + `,"Condition":{"StringEquals":{"aws:SourceAccount":"123456789012"}}}]}`
	for _, scenario := range []struct {
		name    string
		stored  string
		preview bool
		rewrite bool
	}{
		{"unscoped preview", unscoped, true, true},
		{"unscoped", unscoped, false, true},
		{"scoped", scoped, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			oldClient, oldAccount, oldPrint := s3Client, stsAccount, Logger.Print
			t.Cleanup(func() { s3Client, stsAccount, Logger.Print = oldClient, oldAccount, oldPrint })
			stsAccount = aws.String(account)
			var logs strings.Builder
			Logger.Print = func(args ...any) {
				_, _ = fmt.Fprint(&logs, args...)
			}
			var puts []string
			s3Client = s3.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
				HTTPClient: s3AllowPutTransport(func(request *http.Request) (*http.Response, error) {
					status, response := 200, ""
					query := request.URL.Query()
					switch {
					case request.Method == http.MethodHead:
					case request.Method == http.MethodPut && query.Has("policy"):
						body, err := io.ReadAll(request.Body)
						if err != nil {
							return nil, err
						}
						puts = append(puts, string(body))
						status = 204
					case request.Method != http.MethodGet:
						return nil, fmt.Errorf("unexpected mutation: %s %s", request.Method, request.URL)
					case query.Has("tagging"):
						response = `<Tagging><TagSet><Tag><Key>libaws.infraset</Key><Value>wanted</Value></Tag></TagSet></Tagging>`
					case query.Has("publicAccessBlock"):
						response = `<PublicAccessBlockConfiguration><BlockPublicAcls>true</BlockPublicAcls><IgnorePublicAcls>true</IgnorePublicAcls><BlockPublicPolicy>true</BlockPublicPolicy><RestrictPublicBuckets>true</RestrictPublicBuckets></PublicAccessBlockConfiguration>`
					case query.Has("policy"):
						response = scenario.stored
					case query.Has("cors"):
						status, response = 404, `<Error><Code>NoSuchCORSConfiguration</Code></Error>`
					case query.Has("versioning"):
						response = `<VersioningConfiguration/>`
					case query.Has("encryption"):
						response = `<ServerSideEncryptionConfiguration><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault><BlockedEncryptionTypes><EncryptionType>SSE-C</EncryptionType></BlockedEncryptionTypes><BucketKeyEnabled>false</BucketKeyEnabled></Rule></ServerSideEncryptionConfiguration>`
					case query.Has("metrics"):
						status, response = 404, `<Error><Code>NoSuchConfiguration</Code></Error>`
					case query.Has("lifecycle"):
						status, response = 404, `<Error><Code>NoSuchLifecycleConfiguration</Code></Error>`
					default:
						return nil, fmt.Errorf("unexpected read: %s %s", request.Method, request.URL)
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
				}),
			})
			input, err := S3EnsureInput("wanted", "fixture", []string{"allow_put=ses.amazonaws.com"})
			if err != nil {
				t.Fatal(err)
			}
			if err := S3Ensure(context.Background(), input, scenario.preview); err != nil {
				t.Fatal(err)
			}
			reported := strings.Contains(logs.String(), PreviewString(scenario.preview)+"updated bucket policy: fixture")
			if reported != scenario.rewrite {
				t.Fatalf("policy rewrite reported = %v, want %v: %s", reported, scenario.rewrite, logs.String())
			}
			if scenario.preview || !scenario.rewrite {
				if len(puts) != 0 {
					t.Fatalf("unexpected bucket policy writes: %q", puts)
				}
				return
			}
			if len(puts) != 1 {
				t.Fatalf("bucket policy writes = %q, want one", puts)
			}
			equal, err := iamPolicyEqual(puts[0], scoped)
			if err != nil {
				t.Fatal(err)
			}
			if !equal {
				t.Fatalf("written bucket policy:\ngot:  %s\nwant: %s", puts[0], scoped)
			}
		})
	}
}

func TestS3EnsureAllowPutSourceAccount(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx := context.Background()
	account, err := StsAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.Must(uuid.NewV4()).String()
	bucket := "libaws-allow-put-test-" + id
	infraSet := "allow-put-test-" + id
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := S3DeleteBucket(cleanupCtx, bucket, false); err != nil {
			t.Errorf("delete bucket %s: %v", bucket, err)
		}
	})
	input, err := S3EnsureInput(infraSet, bucket, []string{"allow_put=ses.amazonaws.com"})
	if err != nil {
		t.Fatal(err)
	}
	desired, err := s3DesiredPolicy(input, account)
	if err != nil {
		t.Fatal(err)
	}
	managedAttrs := []string{"allow_put=ses.amazonaws.com", "acl=private"}

	if err := S3Ensure(ctx, input, false); err != nil {
		t.Fatal(err)
	}
	client, err := S3ClientBucketRegion(ctx, bucket)
	if err != nil {
		t.Fatal(err)
	}
	waitStable := func(ready func(context.Context) (bool, error)) error {
		waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return waitLiveAWSFixtureStable(waitCtx, func() (bool, error) { return ready(waitCtx) })
	}
	waitPolicy := func(want string) string {
		t.Helper()
		var stored string
		err := waitStable(func(waitCtx context.Context) (bool, error) {
			out, err := client.GetBucketPolicy(waitCtx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
			if err != nil {
				if strings.Contains(err.Error(), s3ErrCodeNoSuchBucketPolicy) {
					stored = ""
					return false, nil
				}
				return false, err
			}
			stored = aws.ToString(out.Policy)
			return iamPolicyEqual(stored, want)
		})
		if err != nil {
			t.Fatalf("bucket policy did not settle: %v\ngot:  %s\nwant: %s", err, stored, want)
		}
		return stored
	}
	preview := func() string {
		t.Helper()
		var logs strings.Builder
		originalPrint := Logger.Print
		Logger.Print = func(args ...any) {
			_, _ = fmt.Fprint(&logs, args...)
		}
		err := S3Ensure(ctx, input, true)
		Logger.Print = originalPrint
		if err != nil {
			t.Fatal(err)
		}
		return logs.String()
	}
	inventory := func() []string {
		t.Helper()
		buckets, err := (infraListScope{setName: infraSet}).listS3(ctx, make(chan *InfraTrigger, 1))
		if err != nil {
			t.Fatal(err)
		}
		listed, ok := buckets[bucket]
		if !ok {
			t.Fatalf("inventory omitted bucket %s", bucket)
		}
		return listed.Attr
	}

	err = waitStable(func(waitCtx context.Context) (bool, error) {
		out, err := client.GetBucketTagging(waitCtx, &s3.GetBucketTaggingInput{Bucket: aws.String(bucket)})
		if err != nil {
			return false, err
		}
		for _, tag := range out.TagSet {
			if aws.ToString(tag.Key) == infraSetTagName && aws.ToString(tag.Value) == infraSet {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("infrastructure set tag did not settle: %v", err)
	}
	stored := waitPolicy(desired)
	storedPolicy := IamPolicyDocument{}
	if err := json.Unmarshal([]byte(stored), &storedPolicy); err != nil {
		t.Fatal(err)
	}
	conditioned := IamStatementEntry{}
	if err := json.Unmarshal([]byte(fmt.Sprintf(
		`{"Sid":"allow put from ses.amazonaws.com","Effect":"Allow","Principal":{"Service":"ses.amazonaws.com"},"Action":"s3:PutObject","Resource":"arn:aws:s3:::%s/*","Condition":{"StringEquals":{"aws:SourceAccount":%q}}}`,
		bucket, account,
	)), &conditioned); err != nil {
		t.Fatal(err)
	}
	if !s3PolicyHasStatement(storedPolicy, conditioned) {
		t.Fatalf("stored bucket policy lacks the source-account allow_put statement: %s", stored)
	}
	if logs := preview(); strings.Contains(logs, "bucket policy") {
		t.Fatalf("converged bucket still reports policy drift: %s", logs)
	}
	if attrs := inventory(); !reflect.DeepEqual(attrs, managedAttrs) {
		t.Fatalf("inventory attrs = %q, want %q", attrs, managedAttrs)
	}

	otherAccount := "111122223333"
	if account == otherAccount {
		otherAccount = "444455556666"
	}
	for _, scenario := range []struct {
		name      string
		statement IamStatementEntry
	}{
		{"unconditioned", IamStatementEntry{
			Sid:       "allow put from ses.amazonaws.com",
			Effect:    "Allow",
			Principal: map[string]string{"Service": "ses.amazonaws.com"},
			Action:    "s3:PutObject",
			Resource:  "arn:aws:s3:::" + bucket + "/*",
		}},
		{"other account", s3AllowPutStatement(bucket, "ses.amazonaws.com", otherAccount)},
	} {
		policy := s3BasePolicy(bucket)
		policy.Statement = append(policy.Statement, scenario.statement)
		data, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
			Bucket: aws.String(bucket),
			Policy: aws.String(string(data)),
		}); err != nil {
			t.Fatalf("%s: put fixture policy: %v", scenario.name, err)
		}
		waitPolicy(string(data))
		if attrs := inventory(); !reflect.DeepEqual(attrs, []string{"acl=custom"}) {
			t.Fatalf("%s: inventory attrs = %q, want unmanaged acl=custom", scenario.name, attrs)
		}
		if logs := preview(); !strings.Contains(logs, "preview: updated bucket policy: "+bucket) {
			t.Fatalf("%s: preview did not report the policy rewrite: %s", scenario.name, logs)
		}
		if err := S3Ensure(ctx, input, false); err != nil {
			t.Fatalf("%s: ensure: %v", scenario.name, err)
		}
		waitPolicy(desired)
		if attrs := inventory(); !reflect.DeepEqual(attrs, managedAttrs) {
			t.Fatalf("%s: inventory attrs after ensure = %q, want %q", scenario.name, attrs, managedAttrs)
		}
	}
}
