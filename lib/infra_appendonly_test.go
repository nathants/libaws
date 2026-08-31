package lib

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
)

func TestS3AppendOnlyPolicy(t *testing.T) {
	bucket := "backup-bucket"
	want := `{
		"Version":"2012-10-17",
		"Statement":[
			{"Sid":"DenyInsecureTransport","Effect":"Deny","Principal":"*","Action":"s3:*","Resource":["arn:aws:s3:::backup-bucket","arn:aws:s3:::backup-bucket/*"],"Condition":{"Bool":{"aws:SecureTransport":"false"}}},
			{"Sid":"DenyPutWithoutExactCreateCondition","Effect":"Deny","Principal":"*","Action":"s3:PutObject","Resource":"arn:aws:s3:::backup-bucket/*","Condition":{"Bool":{"s3:ObjectCreationOperation":"true"},"StringNotEquals":{"s3:if-none-match":"*"}}},
			{"Sid":"DenyObjectDeletion","Effect":"Deny","Principal":"*","Action":["s3:DeleteObject","s3:DeleteObjectVersion"],"Resource":"arn:aws:s3:::backup-bucket/*"}
		]
	}`
	input := s3EnsureInputDefault()
	input.name = bucket
	input.appendOnly = true
	got, err := s3DesiredPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	equal, err := iamPolicyEqual(got, want)
	if err != nil {
		t.Fatal(err)
	}
	if !equal {
		t.Fatalf("append-only policy mismatch:\ngot:  %s\nwant: %s", got, want)
	}
	if !s3AppendOnlyPolicyMatches(bucket, got) {
		t.Fatal("generated append-only policy was not recognized")
	}
	changed := strings.Replace(got, `"s3:if-none-match":"*"`, `"s3:if-none-match":"changed"`, 1)
	if s3AppendOnlyPolicyMatches(bucket, changed) {
		t.Fatal("security-critical condition drift was accepted")
	}
}

func TestS3EnsureInputAppendOnly(t *testing.T) {
	input, err := S3EnsureInput("backup", "backup-bucket", []string{
		"appendonly=true",
		"acl=public",
		"allow_put=lambda.amazonaws.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !input.appendOnly || input.acl != "public" || input.versioning {
		t.Fatalf("append-only input not configured independently: %#v", input)
	}
	if input.CustomPolicy == nil {
		t.Fatal("append-only input lost the independent allow_put policy")
	}
	desired, err := s3DesiredPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if !s3AppendOnlyPolicyMatches("backup-bucket", desired) {
		t.Fatalf("composed policy lost append-only enforcement: %s", desired)
	}
	policy := IamPolicyDocument{}
	if err := json.Unmarshal([]byte(desired), &policy); err != nil {
		t.Fatal(err)
	}
	if !s3PolicyHasStatement(policy, s3PublicReadStatement("backup-bucket")) {
		t.Fatal("composed policy lost public-read configuration")
	}
	allowPut := false
	for _, statement := range policy.Statement {
		if principal, ok := s3AllowPutPrincipal("backup-bucket", statement); ok && principal == "lambda.amazonaws.com" {
			allowPut = true
		}
	}
	if !allowPut {
		t.Fatal("composed policy lost allow_put configuration")
	}
	if strings.Contains(desired, "s3:x-amz-server-side-encryption") {
		t.Fatalf("append-only policy requires an unrelated encryption request header: %s", desired)
	}
}

func TestS3EnsureInputAppendOnlyRejectsExpiration(t *testing.T) {
	attrs := []string{"appendonly=true", "ttldays=1"}
	if _, err := S3EnsureInput("backup", "backup-bucket", attrs); err == nil {
		t.Fatalf("expiring append-only configuration accepted: %v", attrs)
	}
}

func TestS3PublicPolicyAlwaysDeniesInsecureTransport(t *testing.T) {
	input, err := S3EnsureInput("public", "backup-bucket", []string{"acl=public"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := s3DesiredPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, `"Sid":"DenyInsecureTransport"`) {
		t.Fatalf("public bucket policy allows unspecified insecure transport: %s", data)
	}
}

func TestS3AllowPutPrincipalRejectsConditionedStatement(t *testing.T) {
	bucket := "backup-bucket"
	statement := IamStatementEntry{
		Sid:       "allow put from lambda.amazonaws.com",
		Effect:    "Allow",
		Principal: map[string]any{"Service": "lambda.amazonaws.com"},
		Action:    "s3:PutObject",
		Resource:  "arn:aws:s3:::" + bucket + "/*",
		Condition: map[string]any{"StringEquals": map[string]string{"aws:SourceAccount": "123456789012"}},
	}
	if principal, ok := s3AllowPutPrincipal(bucket, statement); ok {
		t.Fatalf("conditioned custom statement was classified as managed allow_put=%q", principal)
	}
}

func TestS3DesiredPolicyPreservesCustomStatementFields(t *testing.T) {
	input, err := S3EnsureInput("backup", "backup-bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	custom := `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","NotAction":"s3:GetObject","NotResource":"arn:aws:s3:::other/*","NotPrincipal":{"AWS":"arn:aws:iam::123456789012:root"}}]}`
	input.CustomPolicy = &custom
	desired, err := s3DesiredPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"NotAction", "NotResource", "NotPrincipal"} {
		if !strings.Contains(desired, `"`+field+`"`) {
			t.Fatalf("composed bucket policy dropped %s: %s", field, desired)
		}
	}
}

func TestIamAllowsRejectConditionalPolicy(t *testing.T) {
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:PutObject","Resource":"arn:aws:s3:::backup-bucket/*","Condition":{"StringEquals":{"s3:if-none-match":"*"}}}]}`
	if _, err := iamAllowsFromPolicyDocument(policy); err == nil {
		t.Fatal("conditional policy was silently represented as an unconditional allow")
	}
}

func TestIamEnsureUserAllowsRejectsPolicyNameCollisionBeforeAWS(t *testing.T) {
	err := IamEnsureUserAllows(context.Background(), "unused", []string{
		"s3:GetObject arn:aws:s3:::backup-bucket/a/b",
		"s3:GetObject arn:aws:s3:::backup-bucket/a__b",
	}, false)
	if err == nil || !strings.Contains(err.Error(), "same inline policy name") {
		t.Fatalf("policy-name collision error = %v", err)
	}
}

func TestIamEnsureUserPoliciesPreviewRejectsMissingPolicy(t *testing.T) {
	requireLiveAWSAccount(t)
	suffix := uuid.Must(uuid.NewV4()).String()
	policyName := "libaws-missing-policy-" + suffix
	err := IamEnsureUserPolicies(
		context.Background(),
		"libaws-missing-user-"+suffix,
		[]string{policyName},
		true,
	)
	if err == nil || !strings.Contains(err.Error(), policyName) {
		t.Fatalf("preview missing-policy error = %v, want policy name %q", err, policyName)
	}
}

func TestInfraParseUsersAndAppendOnlyBucket(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "infra.yaml")
	data := []byte(`name: backup
s3:
  backup-bucket:
    attr:
      - acl=private
      - versioning=true
      - appendonly=true
user:
  backup-writer:
    allow:
      - s3:PutObject arn:aws:s3:::backup-bucket/*
  backup-reader:
    allow:
      - s3:GetObject arn:aws:s3:::backup-bucket/*
      - s3:GetObjectVersion arn:aws:s3:::backup-bucket/*
      - s3:ListBucket arn:aws:s3:::backup-bucket
    policy:
      - ExistingPolicy
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	infra, err := InfraParse(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]*InfraUser{
		"backup-writer": {Allow: []string{"s3:PutObject arn:aws:s3:::backup-bucket/*"}},
		"backup-reader": {
			Allow: []string{
				"s3:GetObject arn:aws:s3:::backup-bucket/*",
				"s3:GetObjectVersion arn:aws:s3:::backup-bucket/*",
				"s3:ListBucket arn:aws:s3:::backup-bucket",
			},
			Policy: []string{"ExistingPolicy"},
		},
	}
	if !reflect.DeepEqual(infra.User, want) {
		got, _ := json.Marshal(infra.User)
		t.Fatalf("users mismatch: %s", got)
	}
	input, err := S3EnsureInput(infra.Name, "backup-bucket", infra.S3["backup-bucket"].Attr)
	if err != nil {
		t.Fatal(err)
	}
	if !input.appendOnly {
		t.Fatal("parsed bucket is not append-only")
	}
}

func TestInfraParseRejectsInvalidUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte("name: backup\nuser:\n  bad:\n    unknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InfraParse(path); err == nil {
		t.Fatal("invalid user configuration accepted")
	}
}
