package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type ec2SubnetTestClient struct {
	vpcName    string
	vpcID      string
	zones      []string
	subnets    []ec2types.Subnet
	failAction string
	calls      []string
}

func (client *ec2SubnetTestClient) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	action := r.Form.Get("Action")
	client.calls = append(client.calls, action)
	body, status := "", http.StatusOK
	if action == client.failAction {
		body = `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>denied subnet probe</Message></Error></Errors></Response>`
		status = http.StatusForbidden
	} else {
		switch action {
		case "DescribeInstanceTypeOfferings":
			if r.Form.Get("Filter.1.Name") != "instance-type" || r.Form.Get("Filter.1.Value.1") != "t3.small" {
				return nil, fmt.Errorf("unexpected instance type filter: %v", r.Form)
			}
			body = "<instanceTypeOfferingSet>"
			for _, zone := range client.zones {
				body += "<item><instanceType>t3.small</instanceType><location>" + zone + "</location></item>"
			}
			body += "</instanceTypeOfferingSet>"
		case "DescribeVpcs":
			if r.Form.Get("Filter.1.Name") != "tag:Name" || r.Form.Get("Filter.1.Value.1") != client.vpcName {
				return nil, fmt.Errorf("unexpected VPC name filter: %v", r.Form)
			}
			body = "<vpcSet><item><vpcId>" + client.vpcID + "</vpcId></item></vpcSet>"
		case "DescribeSubnets":
			if r.Form.Get("Filter.1.Name") != "vpc-id" || r.Form.Get("Filter.1.Value.1") != client.vpcID {
				return nil, fmt.Errorf("unexpected VPC ID filter: %v", r.Form)
			}
			body = "<subnetSet>"
			for _, subnet := range client.subnets {
				body += fmt.Sprintf("<item><subnetId>%s</subnetId><availabilityZone>%s</availabilityZone></item>", *subnet.SubnetId, *subnet.AvailabilityZone)
			}
			body += "</subnetSet>"
		default:
			return nil, fmt.Errorf("unexpected EC2 action: %s", action)
		}
		body = "<" + action + "Response>" + body + "</" + action + "Response>"
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func installEC2SubnetTestClient(t *testing.T) *ec2SubnetTestClient {
	t.Helper()
	client := &ec2SubnetTestClient{
		vpcName: "test-vpc",
		vpcID:   "vpc-0123456789abcdef0",
		zones:   []string{"us-east-1a", "us-east-1b"},
		subnets: []ec2types.Subnet{
			{SubnetId: aws.String("subnet-a1"), AvailabilityZone: aws.String("us-east-1a")},
			{SubnetId: aws.String("subnet-a2"), AvailabilityZone: aws.String("us-east-1a")},
			{SubnetId: aws.String("subnet-b"), AvailabilityZone: aws.String("us-east-1b")},
		},
	}
	oldClient := ec2Client
	ec2Client = ec2.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("key", "secret", ""),
		HTTPClient: &http.Client{Transport: client}, RetryMaxAttempts: 1,
	})
	t.Cleanup(func() { ec2Client = oldClient })
	return client
}

func TestEC2SubnetsFromVpcSpotKeepsEverySubnet(t *testing.T) {
	for _, name := range []string{"test-vpc", "vpc-prod", "vpc-12345", "vpc", "vpc-12345678", "vpc-0123456789abcdef0"} {
		t.Run(name, func(t *testing.T) {
			client := installEC2SubnetTestClient(t)
			client.vpcName = name
			isID := name == "vpc-12345678" || name == "vpc-0123456789abcdef0"
			if isID {
				client.vpcID = name
			}
			got, err := EC2SubnetsFromVpc(context.Background(), name, ec2types.InstanceTypeT3Small, true)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"subnet-a1", "subnet-a2", "subnet-b"}
			if !slices.Equal(got, want) {
				t.Fatalf("subnets = %v, want every subnet %v", got, want)
			}
			if lookedUpName := slices.Contains(client.calls, "DescribeVpcs"); lookedUpName == isID {
				t.Fatalf("VPC name lookup = %t for %q", lookedUpName, name)
			}
		})
	}
}

func TestEC2SubnetsFromVpcOnlyUsesOfferedZones(t *testing.T) {
	for _, spot := range []bool{true, false} {
		t.Run(fmt.Sprint("spot=", spot), func(t *testing.T) {
			client := installEC2SubnetTestClient(t)
			// The instance is offered in 1b, but this VPC has no subnet there.
			// Its 1c subnet cannot run this instance type.
			client.subnets = []ec2types.Subnet{
				{SubnetId: aws.String("subnet-a"), AvailabilityZone: aws.String("us-east-1a")},
				{SubnetId: aws.String("subnet-c"), AvailabilityZone: aws.String("us-east-1c")},
			}
			for range 64 {
				got, err := EC2SubnetsFromVpc(context.Background(), client.vpcName, ec2types.InstanceTypeT3Small, spot)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(got, []string{"subnet-a"}) {
					t.Fatalf("subnets = %v, want the only usable subnet", got)
				}
			}
		})
	}
}

func TestEC2SubnetsFromVpcOnDemandSelectsOne(t *testing.T) {
	client := installEC2SubnetTestClient(t)
	for range 64 {
		got, err := EC2SubnetsFromVpc(context.Background(), client.vpcName, ec2types.InstanceTypeT3Small, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !slices.Contains([]string{"subnet-a1", "subnet-a2", "subnet-b"}, got[0]) {
			t.Fatalf("subnets = %v, want one VPC subnet", got)
		}
	}
}

func TestEC2SubnetsFromVpcRejectsUnavailableSubnets(t *testing.T) {
	for _, spot := range []bool{true, false} {
		for _, empty := range []bool{true, false} {
			t.Run(fmt.Sprintf("spot=%t/empty=%t", spot, empty), func(t *testing.T) {
				client := installEC2SubnetTestClient(t)
				if empty {
					client.subnets = nil
				} else {
					client.zones = []string{"us-east-1c"}
				}
				_, err := EC2SubnetsFromVpc(context.Background(), client.vpcName, ec2types.InstanceTypeT3Small, spot)
				if err == nil || !strings.Contains(err.Error(), "no subnet") {
					t.Fatalf("no usable subnets: error = %v", err)
				}
			})
		}
	}
}

func TestEC2SubnetsFromVpcPreservesAWSErrors(t *testing.T) {
	for _, action := range []string{"DescribeInstanceTypeOfferings", "DescribeVpcs", "DescribeSubnets"} {
		t.Run(action, func(t *testing.T) {
			client := installEC2SubnetTestClient(t)
			client.failAction = action
			_, err := EC2SubnetsFromVpc(context.Background(), client.vpcName, ec2types.InstanceTypeT3Small, true)
			if err == nil || !strings.Contains(err.Error(), "UnauthorizedOperation") || !strings.Contains(err.Error(), "denied subnet probe") {
				t.Fatalf("AWS error was lost: %v", err)
			}
			if client.calls[len(client.calls)-1] != action {
				t.Fatalf("continued after AWS error: %v", client.calls)
			}
		})
	}
}

// The CLI example owns the launch and cleanup; this checks the request AWS stored.
func TestEC2SubnetsFromVpcIntegration(t *testing.T) {
	instanceID := os.Getenv("LIBAWS_EC2_SUBNET_TEST_INSTANCE")
	if instanceID == "" {
		t.Skip("run examples/misc/ec2/test.py to provide a live CLI launch")
	}
	requireLiveAWSAccount(t)
	ctx := context.Background()
	expected := strings.Fields(os.Getenv("LIBAWS_EC2_SUBNET_TEST_SUBNETS"))
	vpcID := os.Getenv("LIBAWS_EC2_SUBNET_TEST_VPC")
	if len(expected) == 0 || vpcID == "" {
		t.Fatal("live subnet and VPC expectations are required")
	}
	instances, err := EC2DescribeInstances(ctx, []string{instanceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want one", len(instances))
	}
	lifecycle := os.Getenv("LIBAWS_EC2_SUBNET_TEST_LIFECYCLE")
	if lifecycle != "spot" && lifecycle != "on-demand" {
		t.Fatal("expected lifecycle must be spot or on-demand")
	}
	instance := instances[0]
	wantLifecycle := ec2types.InstanceLifecycleType("")
	if lifecycle == "spot" {
		wantLifecycle = ec2types.InstanceLifecycleTypeSpot
	}
	if aws.ToString(instance.VpcId) != vpcID || instance.InstanceLifecycle != wantLifecycle || !slices.Contains(expected, aws.ToString(instance.SubnetId)) || instance.IamInstanceProfile != nil {
		t.Fatalf("unexpected launch: VPC=%s subnet=%s lifecycle=%s profile=%v", aws.ToString(instance.VpcId), aws.ToString(instance.SubnetId), instance.InstanceLifecycle, instance.IamInstanceProfile)
	}
	if lifecycle == "spot" {
		fleetID := EC2GetTag(instance.Tags, "aws:ec2spot:fleet-request-id", "")
		if fleetID == "" {
			t.Fatal("launched instance has no Spot Fleet request tag")
		}
		fleet, err := EC2DescribeSpotFleet(ctx, &fleetID)
		if err != nil {
			t.Fatal(err)
		}
		var actual []string
		for _, spec := range fleet.SpotFleetRequestConfig.LaunchSpecifications {
			actual = append(actual, aws.ToString(spec.SubnetId))
		}
		slices.Sort(expected)
		slices.Sort(actual)
		if !slices.Equal(actual, expected) {
			t.Fatalf("AWS fleet subnets = %v, want %v", actual, expected)
		}
		t.Logf("verified AWS fleet %s: subnets=%v", fleetID, actual)
	} else {
		t.Logf("verified on-demand instance %s: subnet=%s", instanceID, aws.ToString(instance.SubnetId))
	}
	all, err := EC2SubnetsFromVpc(ctx, vpcID, ec2types.InstanceTypeT3Small, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, subnetID := range expected {
		if !slices.Contains(all, subnetID) {
			t.Fatalf("VPC ID lookup omitted eligible subnet %s: %v", subnetID, all)
		}
	}
}
