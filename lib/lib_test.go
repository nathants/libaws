package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

func lambdaZipHashForTest(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func requireLiveAWSAccount(t *testing.T) {
	t.Helper()
	expectedAccount := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expectedAccount == "" {
		t.Skip("set LIBAWS_TEST_ACCOUNT to run live AWS tests")
	}
	if !liveAWSCredentialsConfigured() {
		t.Skip("LIBAWS_TEST_ACCOUNT is set but no AWS credentials are configured")
	}
	account, err := StsAccount(context.Background())
	if err != nil {
		t.Fatalf("verify AWS account: %v", err)
	}
	if account != expectedAccount {
		t.Fatalf("AWS account = %s, want guarded account %s", account, expectedAccount)
	}
}

// liveAWSCredentialsConfigured reports whether a local source exists for the
// default AWS credential chain. Without one, an opted-in live test would stall
// resolving IMDS instead of running or failing meaningfully. A configured
// source that turns out invalid still fails loudly.
func liveAWSCredentialsConfigured() bool {
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}
	if os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" && os.Getenv("AWS_ROLE_ARN") != "" {
		return true
	}
	if os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" || os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}
	path := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		path = filepath.Join(home, ".aws", "credentials")
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// S3 can alternate new and old configuration after a write. Wait for stable
// fixture reads before asserting behavior once; never retry failed assertions.
func waitLiveAWSFixtureStable(ctx context.Context, ready func() (bool, error)) error {
	var stableSince time.Time
	for {
		ok, err := ready()
		if err != nil {
			return err
		}
		if !ok {
			stableSince = time.Time{}
		} else if stableSince.IsZero() {
			stableSince = time.Now()
		} else if time.Since(stableSince) >= 10*time.Second {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func TestDropLinesWithAny(t *testing.T) {
	type test struct {
		input  string
		output string
		tokens []string
	}
	tests := []test{
		{"a\nb\nc\n", "a\nb\nc\n", []string{"foo"}},
		{"a\nb\nc\n", "b\nc\n", []string{"a"}},
		{"a\nb\nc\n", "b\n", []string{"a", "c"}},
	}
	for _, test := range tests {
		output := DropLinesWithAny(test.input, test.tokens...)
		if output != test.output {
			t.Errorf("\ngot:\n%s\nwant:\n%s\n", output, test.output)
		}
	}
}

func ec2ExampleUID(t *testing.T) string {
	t.Helper()
	uid := os.Getenv("LIBAWS_EC2_EXAMPLE_UID")
	if uid == "" {
		t.Skip("run through examples/misc/ec2 or its live failure-cleanup test")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("EC2 example requires a unique 12-hex uid")
	}
	requireLiveAWSAccount(t)
	return uid
}

// Fleet requests have no infrastructure-set membership, and libaws fleets
// launch from templates. The unique fixture key in a template proves ownership
// even before instances exist or after membership tags are lost.
func ec2ExampleOwnedLaunchTemplates(ctx context.Context, key string) ([]ec2types.LaunchTemplateVersion, error) {
	var owned []ec2types.LaunchTemplateVersion
	pages := ec2.NewDescribeLaunchTemplateVersionsPaginator(EC2Client(), &ec2.DescribeLaunchTemplateVersionsInput{Versions: []string{"$Latest"}})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return owned, err
		}
		for _, version := range page.LaunchTemplateVersions {
			if version.LaunchTemplateData != nil && aws.ToString(version.LaunchTemplateData.KeyName) == key {
				owned = append(owned, version)
			}
		}
	}
	return owned, nil
}

// A fleet is owned when every template it launches from is owned. Once its
// template is gone, a fleet is no longer attributable; its instances are still
// found by key.
func ec2ExampleOwnedFleets(ctx context.Context, key string) ([]ec2types.SpotFleetRequestConfig, error) {
	templates, err := ec2ExampleOwnedLaunchTemplates(ctx, key)
	ownedTemplates := map[string]bool{}
	for _, template := range templates {
		for _, ref := range []string{aws.ToString(template.LaunchTemplateId), aws.ToString(template.LaunchTemplateName)} {
			if ref != "" {
				ownedTemplates[ref] = true
			}
		}
	}
	var owned []ec2types.SpotFleetRequestConfig
	pages := ec2.NewDescribeSpotFleetRequestsPaginator(EC2Client(), &ec2.DescribeSpotFleetRequestsInput{})
	for pages.HasMorePages() {
		page, pageErr := pages.NextPage(ctx)
		if pageErr != nil {
			return owned, errors.Join(err, pageErr)
		}
		for _, fleet := range page.SpotFleetRequestConfigs {
			if fleet.SpotFleetRequestConfig == nil {
				continue
			}
			configs := fleet.SpotFleetRequestConfig.LaunchTemplateConfigs
			matches := len(configs) > 0
			for _, config := range configs {
				spec := config.LaunchTemplateSpecification
				matches = matches && spec != nil && (ownedTemplates[aws.ToString(spec.LaunchTemplateId)] || ownedTemplates[aws.ToString(spec.LaunchTemplateName)])
			}
			if matches {
				owned = append(owned, fleet)
			}
		}
	}
	return owned, err
}

func ec2ExampleOwnedInstances(ctx context.Context, key string) ([]ec2types.Instance, error) {
	var instances []ec2types.Instance
	pages := ec2.NewDescribeInstancesPaginator(EC2Client(), &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{{Name: aws.String("key-name"), Values: []string{key}}},
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return instances, err
		}
		for _, reservation := range page.Reservations {
			instances = append(instances, reservation.Instances...)
		}
	}
	return instances, nil
}

func assertEC2ExampleComputeAbsent(t *testing.T, ctx context.Context, key string) {
	t.Helper()
	fleets, err := ec2ExampleOwnedFleets(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, fleet := range fleets {
		// All canceled states prevent new launches. The suffix describes slow
		// bookkeeping; actual instance termination is checked independently below.
		if !slices.Contains(ec2FailedStates, fleet.SpotFleetRequestState) {
			t.Errorf("owned fleet %s can still launch: %s", aws.ToString(fleet.SpotFleetRequestId), fleet.SpotFleetRequestState)
		}
	}
	instances, err := ec2ExampleOwnedInstances(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, instance := range instances {
		if instance.State.Name != ec2types.InstanceStateNameTerminated {
			t.Errorf("owned instance %s remains %s", aws.ToString(instance.InstanceId), instance.State.Name)
		}
	}
	// A successful deletion is not immediate absence proof. Wait boundedly for
	// deleted templates to disappear, then report any that remain.
	templates, err := ec2ExampleOwnedLaunchTemplates(ctx, key)
	for deadline := time.Now().Add(30 * time.Second); err == nil && len(templates) > 0 && time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(2 * time.Second):
			templates, err = ec2ExampleOwnedLaunchTemplates(ctx, key)
		}
	}
	if err != nil {
		t.Error(err)
	}
	for _, template := range templates {
		t.Errorf("owned launch template %s remains", aws.ToString(template.LaunchTemplateId))
	}
}

// waitEC2ExampleLaunchTemplateDeleted fails unless the template is gone,
// allowing a bounded wait for a successful deletion to become visible.
func waitEC2ExampleLaunchTemplateDeleted(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	if !strings.HasPrefix(id, "lt-") {
		t.Fatalf("invalid launch template ID %q", id)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		out, err := EC2Client().DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{LaunchTemplateIds: []string{id}})
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidLaunchTemplateId.NotFound" {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(out.LaunchTemplates) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("launch template %s survived its launch", id)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// assertEC2ExampleLaunch checks what every libaws launch guarantees: the guest
// requires IMDSv2 with a hop limit of one, and a Spot Fleet launch leaves no
// launch template behind. It returns the instance's fleet, if any.
func assertEC2ExampleLaunch(t *testing.T, ctx context.Context, instance ec2types.Instance) *ec2types.SpotFleetRequestConfig {
	t.Helper()
	id := aws.ToString(instance.InstanceId)
	metadata := instance.MetadataOptions
	if metadata == nil || metadata.HttpTokens != ec2types.HttpTokensStateRequired || metadata.HttpEndpoint != ec2types.InstanceMetadataEndpointStateEnabled || aws.ToInt32(metadata.HttpPutResponseHopLimit) != 1 {
		t.Fatalf("instance %s does not require IMDSv2 with hop limit 1: %s", id, Pformat(metadata))
	}
	fleetID := EC2GetTag(instance.Tags, "aws:ec2spot:fleet-request-id", "")
	if fleetID == "" {
		t.Logf("verified IMDSv2 on %s", id)
		return nil
	}
	fleet, err := EC2DescribeSpotFleet(ctx, &fleetID)
	if err != nil {
		t.Fatal(err)
	}
	config := fleet.SpotFleetRequestConfig
	if config == nil || len(config.LaunchSpecifications) != 0 || len(config.LaunchTemplateConfigs) != 1 || config.LaunchTemplateConfigs[0].LaunchTemplateSpecification == nil {
		t.Fatalf("fleet %s does not launch from exactly one template: %s", fleetID, Pformat(config))
	}
	template := aws.ToString(config.LaunchTemplateConfigs[0].LaunchTemplateSpecification.LaunchTemplateId)
	// EC2 tags the guest with its template; listings must hide that tag.
	if EC2GetTag(instance.Tags, "aws:ec2launchtemplate:id", "") != template {
		t.Fatalf("instance %s is not tagged with its template %s: %v", id, template, instance.Tags)
	}
	waitEC2ExampleLaunchTemplateDeleted(t, ctx, template)
	t.Logf("verified IMDSv2 on %s; fleet %s template %s is deleted", id, fleetID, template)
	return fleet
}

func cleanupEC2ExampleCompute(t *testing.T, ctx context.Context, key string) {
	t.Helper()
	if !regexp.MustCompile(`^test-keypair-[0-9a-f]{12}$`).MatchString(key) {
		t.Fatal("compute cleanup requires a unique example keypair")
	}
	fleets, err := ec2ExampleOwnedFleets(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, fleet := range fleets {
		if fleet.SpotFleetRequestState == ec2types.BatchStateCancelled {
			continue
		}
		if err := EC2TeardownSpotFleet(ctx, fleet.SpotFleetRequestId); err != nil {
			t.Error(err)
		}
	}
	// Independent discovery also covers on-demand instances and any fleet whose
	// cancellation/listing failed. Attempt it even when another cleanup failed.
	instances, err := ec2ExampleOwnedInstances(ctx, key)
	if err != nil {
		t.Error(err)
	}
	var terminate, wait []string
	for _, instance := range instances {
		if instance.State.Name == ec2types.InstanceStateNameTerminated {
			continue
		}
		id := aws.ToString(instance.InstanceId)
		wait = append(wait, id)
		if instance.State.Name != ec2types.InstanceStateNameShuttingDown {
			terminate = append(terminate, id)
		}
	}
	if len(terminate) > 0 {
		if _, err := EC2Client().TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: terminate}); err != nil {
			t.Error(err)
		}
	}
	if len(wait) > 0 {
		if err := EC2WaitState(ctx, wait, ec2types.InstanceStateNameTerminated); err != nil {
			t.Error(err)
		}
	}
	// Templates go only after their fleets can no longer launch from them.
	templates, err := ec2ExampleOwnedLaunchTemplates(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, template := range templates {
		if err := ec2DeleteLaunchTemplate(ctx, aws.ToString(template.LaunchTemplateName), false); err != nil {
			t.Error(err)
		}
	}
	assertEC2ExampleComputeAbsent(t, ctx, key)
}

func TestLiveAWSCredentialsConfigured(t *testing.T) {
	for _, name := range []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_SHARED_CREDENTIALS_FILE",
	} {
		t.Setenv(name, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if liveAWSCredentialsConfigured() {
		t.Fatal("credentials reported without any configured source")
	}
	credentials := filepath.Join(home, ".aws", "credentials")
	if err := os.MkdirAll(filepath.Dir(credentials), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if liveAWSCredentialsConfigured() {
		t.Fatal("empty shared credentials file reported as configured")
	}
	if err := os.WriteFile(credentials, []byte("[default]\naws_access_key_id = fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !liveAWSCredentialsConfigured() {
		t.Fatal("shared credentials file was not detected")
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(home, "missing"))
	if liveAWSCredentialsConfigured() {
		t.Fatal("missing shared credentials file reported as configured")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-secret")
	if !liveAWSCredentialsConfigured() {
		t.Fatal("environment credentials were not detected")
	}
}
