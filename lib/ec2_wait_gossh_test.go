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
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type ec2WaitGoSshTestTransport func(*http.Request) (*http.Response, error)

func (fn ec2WaitGoSshTestTransport) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func installEC2WaitGoSshTestClient(t *testing.T, respond func(url.Values) (string, error)) {
	t.Helper()
	old := ec2Client
	t.Cleanup(func() { ec2Client = old })
	ec2Client = ec2.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
		HTTPClient: ec2WaitGoSshTestTransport(func(request *http.Request) (*http.Response, error) {
			if err := request.ParseForm(); err != nil {
				return nil, err
			}
			body, err := respond(request.Form)
			if err != nil {
				return nil, err
			}
			return &http.Response{Request: request, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	})
}

func ec2WaitGoSshTestInstances(ids ...string) string {
	body := "<DescribeInstancesResponse><reservationSet><item><instancesSet>"
	for i, id := range ids {
		body += fmt.Sprintf("<item><instanceId>%s</instanceId><instanceState><name>running</name></instanceState><dnsName>host%d.example.invalid</dnsName><launchTime>2026-01-01T00:00:00Z</launchTime></item>", id, i)
	}
	return body + "</instancesSet></item></reservationSet></DescribeInstancesResponse>"
}

func TestEC2WaitGoSshConfigurationFailureNeverTerminates(t *testing.T) {
	_, privateKey, err := SshKeygenEd25519()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing-user", "missing-key", "invalid-key", "invalid-rsa", "negative-concurrency", "negative-wait", "no-selectors", "empty-selector"} {
		t.Run(name, func(t *testing.T) {
			// No callback or real SSH executable may run in this regression.
			t.Setenv("PATH", t.TempDir())
			selected := "i-11111111111111111"
			unrelated := "i-22222222222222222"
			var terminated []string
			reads := 0
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				switch form.Get("Action") {
				case "DescribeInstances":
					reads++
					if reads == 1 {
						// Expire max-wait before the defective cleanup branch.
						time.Sleep(1100 * time.Millisecond)
						return ec2WaitGoSshTestInstances(selected), nil
					}
					if form.Get("Filter.1.Name") == "instance-state-name" && form.Get("Filter.2.Name") == "" {
						return ec2WaitGoSshTestInstances(selected, unrelated), nil
					}
					return ec2WaitGoSshTestInstances(selected), nil
				case "TerminateInstances":
					for key, values := range form {
						if strings.HasPrefix(key, "InstanceId.") {
							terminated = append(terminated, values...)
						}
					}
					return "<TerminateInstancesResponse/>", nil
				default:
					return "", fmt.Errorf("unexpected API call: %s", form.Get("Action"))
				}
			})
			input := &EC2WaitGoSshInput{
				Selectors: []string{selected}, MaxWaitSeconds: 1,
				User: "fixture", Ed25519PrivKey: privateKey,
			}
			switch name {
			case "missing-user":
				input.User = ""
			case "missing-key":
				input.Ed25519PrivKey = ""
			case "invalid-key":
				input.Ed25519PrivKey = "not a private key"
			case "invalid-rsa":
				input.RsaPrivKey = "not a private key"
			case "negative-concurrency":
				input.MaxConcurrency = -1
			case "negative-wait":
				input.MaxWaitSeconds = -1
			case "no-selectors":
				input.Selectors = nil
			case "empty-selector":
				input.Selectors = []string{selected, ""}
			default:
				t.Fatalf("unknown configuration failure: %s", name)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ready, err := EC2WaitGoSsh(ctx, input)
			if err == nil || len(ready) != 0 || len(terminated) != 0 || reads != 0 {
				t.Fatalf("configuration failure must stop before provider calls: ready=%v error=%v reads=%d terminated=%v unrelated=%t", ready, err, reads, terminated, slices.Contains(terminated, unrelated))
			}
		})
	}
}

func ec2WaitGoSshTestInput(t *testing.T, selectors ...string) *EC2WaitGoSshInput {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	_, key, err := SshKeygenEd25519()
	if err != nil {
		t.Fatal(err)
	}
	return &EC2WaitGoSshInput{Selectors: selectors, User: "fixture", Ed25519PrivKey: key, MaxWaitSeconds: 1}
}

func ec2WaitGoSshTestValues(form url.Values, prefix string) []string {
	var result []string
	for key, values := range form {
		if strings.HasPrefix(key, prefix) {
			result = append(result, values...)
		}
	}
	slices.Sort(result)
	return result
}

func TestEC2WaitGoSshResults(t *testing.T) {
	first, second := "i-11111111111111111", "i-22222222222222222"
	good := &ec2GoSshResult{TargetAddr: "host0.example.invalid"}
	otherGood := &ec2GoSshResult{TargetAddr: "host1.example.invalid"}
	unready := errors.New("fixture SSH connection failed")
	bad := &ec2GoSshResult{TargetAddr: good.TargetAddr, Err: unready}
	otherBad := &ec2GoSshResult{TargetAddr: otherGood.TargetAddr, Err: unready}
	globalError := errors.New("fixture SSH setup failed")
	terminateError := errors.New("fixture termination failed")
	for _, test := range []struct {
		name          string
		results       []*ec2GoSshResult
		sshError      error
		ready         []string
		terminate     []string
		wantError     bool
		failTerminate bool
	}{
		{name: "all-ready", results: []*ec2GoSshResult{otherGood, good}, ready: []string{first, second}},
		{name: "partial-timeout", results: []*ec2GoSshResult{otherBad, good}, sshError: unready, ready: []string{first}, terminate: []string{second}},
		{name: "all-timeout", results: []*ec2GoSshResult{otherBad, bad}, sshError: unready, terminate: []string{first, second}, wantError: true},
		{name: "termination-error", results: []*ec2GoSshResult{otherBad, good}, sshError: unready, terminate: []string{second}, wantError: true, failTerminate: true},
		{name: "no-results-error", sshError: globalError, wantError: true},
		{name: "no-results-success", wantError: true},
		{name: "partial-results", results: []*ec2GoSshResult{bad}, sshError: unready, wantError: true},
		{name: "extra-result", results: []*ec2GoSshResult{good, otherBad, bad}, sshError: unready, wantError: true},
		{name: "nil-result", results: []*ec2GoSshResult{nil, otherBad}, sshError: unready, wantError: true},
		{name: "unknown-result", results: []*ec2GoSshResult{good, {TargetAddr: "unselected.example.invalid", Err: unready}}, sshError: unready, wantError: true},
		{name: "duplicate-result", results: []*ec2GoSshResult{bad, bad}, sshError: unready, wantError: true},
		{name: "global-error-with-successes", results: []*ec2GoSshResult{good, otherGood}, sshError: globalError, wantError: true},
		{name: "failure-without-top-level-error", results: []*ec2GoSshResult{good, otherBad}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := ec2WaitGoSshTestInput(t, first, second)
			reads, terminations, sshCalls := 0, 0, 0
			var terminated []string
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				switch form.Get("Action") {
				case "DescribeInstances":
					reads++
					if form.Get("Filter.1.Name") != "instance-id" || !slices.Equal(ec2WaitGoSshTestValues(form, "Filter.1.Value."), []string{first, second}) {
						t.Fatalf("unexpected selection or destructive re-lookup: %v", form)
					}
					return ec2WaitGoSshTestInstances(first, second), nil
				case "TerminateInstances":
					terminations++
					terminated = ec2WaitGoSshTestValues(form, "InstanceId.")
					if test.failTerminate {
						return "", terminateError
					}
					return "<TerminateInstancesResponse/>", nil
				default:
					t.Fatalf("unexpected request: %v", form)
					return "", nil
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			ready, err := ec2WaitGoSsh(ctx, input, func(callCtx context.Context, sshInput *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				sshCalls++
				if callCtx != ctx || !slices.Equal(sshInput.TargetAddrs, []string{good.TargetAddr, otherGood.TargetAddr}) {
					t.Fatalf("wrong SSH context or targets: %v", sshInput.TargetAddrs)
				}
				// Every result shape is checked after the destructive timeout, not
				// just on a fast path before cleanup could become eligible.
				time.Sleep(1100 * time.Millisecond)
				return test.results, test.sshError
			})
			slices.Sort(ready)
			if (err != nil) != test.wantError || !slices.Equal(ready, test.ready) || !slices.Equal(terminated, test.terminate) {
				t.Fatalf("ready=%v error=%v terminated=%v; want ready=%v error=%t terminated=%v", ready, err, terminated, test.ready, test.wantError, test.terminate)
			}
			wantTerminations := 0
			if len(test.terminate) > 0 {
				wantTerminations = 1
			}
			if reads != 1 || sshCalls != 1 || terminations != wantTerminations {
				t.Fatalf("reads=%d SSH calls=%d terminations=%d", reads, sshCalls, terminations)
			}
			if test.sshError == globalError && !errors.Is(err, globalError) {
				t.Fatalf("lost global SSH error: %v", err)
			}
			if test.failTerminate && !errors.Is(err, terminateError) {
				t.Fatalf("lost termination error: %v", err)
			}
		})
	}
}

func TestEC2WaitGoSshPinsSelectionAcrossPolls(t *testing.T) {
	first, second, unrelated := "i-11111111111111111", "i-22222222222222222", "i-33333333333333333"
	for _, phase := range []string{"timeout", "disappeared", "replaced"} {
		t.Run(phase, func(t *testing.T) {
			input := ec2WaitGoSshTestInput(t, "fixture")
			reads, sshCalls := 0, 0
			var terminated []string
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				switch form.Get("Action") {
				case "DescribeInstances":
					reads++
					if reads == 1 {
						if form.Get("Filter.1.Name") != "tag:Name" || form.Get("Filter.1.Value.1") != "fixture" {
							t.Fatalf("wrong initial selection: %v", form)
						}
					} else {
						if form.Get("Filter.1.Name") != "instance-id" || !slices.Equal(ec2WaitGoSshTestValues(form, "Filter.1.Value."), []string{first, second}) {
							t.Fatalf("selection was not pinned: %v", form)
						}
						if phase == "disappeared" {
							return ec2WaitGoSshTestInstances(first), nil
						}
						if phase == "replaced" {
							return ec2WaitGoSshTestInstances(first, unrelated), nil
						}
					}
					return ec2WaitGoSshTestInstances(first, second), nil
				case "TerminateInstances":
					terminated = ec2WaitGoSshTestValues(form, "InstanceId.")
					return "<TerminateInstancesResponse/>", nil
				default:
					t.Fatalf("unexpected request: %v", form)
					return "", nil
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ready, err := ec2WaitGoSsh(ctx, input, func(_ context.Context, sshInput *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				sshCalls++
				if !slices.Equal(sshInput.TargetAddrs, []string{"host0.example.invalid", "host1.example.invalid"}) {
					t.Fatalf("unexpected SSH targets: %v", sshInput.TargetAddrs)
				}
				unready := errors.New("fixture SSH connection failed")
				return []*ec2GoSshResult{{TargetAddr: sshInput.TargetAddrs[0]}, {TargetAddr: sshInput.TargetAddrs[1], Err: unready}}, unready
			})
			if reads != 2 || !slices.Equal(input.Selectors, []string{"fixture"}) {
				t.Fatalf("reads=%d caller selectors=%v", reads, input.Selectors)
			}
			if phase == "timeout" {
				if err != nil || !slices.Equal(ready, []string{first}) || !slices.Equal(terminated, []string{second}) || sshCalls != 2 {
					t.Fatalf("ready=%v error=%v terminated=%v SSH calls=%d", ready, err, terminated, sshCalls)
				}
			} else if err == nil || len(ready) != 0 || len(terminated) != 0 || sshCalls != 1 {
				t.Fatalf("lookup failure allowed pruning: ready=%v error=%v terminated=%v SSH calls=%d", ready, err, terminated, sshCalls)
			}
		})
	}
}

func TestEC2WaitGoSshLookupFailureNeverTerminates(t *testing.T) {
	first, second := "i-11111111111111111", "i-22222222222222222"
	one := ec2WaitGoSshTestInstances(first)
	for _, test := range []struct{ name, response string }{
		{"absent", ec2WaitGoSshTestInstances()},
		{"stopped", strings.ReplaceAll(one, "running", "stopped")},
		{"pending", strings.ReplaceAll(one, "running", "pending")},
		{"missing-address", strings.ReplaceAll(one, "host0.example.invalid", "")},
		{"duplicate-address", strings.ReplaceAll(ec2WaitGoSshTestInstances(first, second), "host1.example.invalid", "host0.example.invalid")},
		{"missing-id", ec2WaitGoSshTestInstances("")},
		{"duplicate-id", ec2WaitGoSshTestInstances(first, first)},
		{"unexpected-id", ec2WaitGoSshTestInstances(second)},
		{"missing-state", strings.ReplaceAll(one, "<instanceState><name>running</name></instanceState>", "")},
		{"provider-error", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := ec2WaitGoSshTestInput(t, first)
			if test.name == "duplicate-address" {
				input.Selectors = append(input.Selectors, second)
			}
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				if form.Get("Action") != "DescribeInstances" {
					t.Fatalf("unexpected mutation after lookup failure: %v", form)
				}
				if test.name == "provider-error" {
					return "", errors.New("fixture EC2 read failed")
				}
				return test.response, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			ready, err := ec2WaitGoSsh(ctx, input, func(context.Context, *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				t.Fatal("SSH started without usable selected instances")
				return nil, nil
			})
			if err == nil || len(ready) != 0 {
				t.Fatalf("ready=%v error=%v", ready, err)
			}
		})
	}
}

func TestEC2WaitGoSshCancellationNeverTerminates(t *testing.T) {
	for _, phase := range []string{"before-lookup", "during-ssh", "between-polls"} {
		t.Run(phase, func(t *testing.T) {
			id := "i-11111111111111111"
			input := ec2WaitGoSshTestInput(t, id)
			reads, sshCalls := 0, 0
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				if form.Get("Action") != "DescribeInstances" {
					t.Fatalf("unexpected mutation after cancellation: %v", form)
				}
				reads++
				return ec2WaitGoSshTestInstances(id), nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before-lookup" {
				cancel()
			}
			ready, err := ec2WaitGoSsh(ctx, input, func(callCtx context.Context, _ *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				sshCalls++
				if callCtx != ctx {
					t.Fatal("caller cancellation not passed to SSH")
				}
				if phase == "during-ssh" {
					time.Sleep(1100 * time.Millisecond)
					cancel()
				} else {
					time.AfterFunc(20*time.Millisecond, cancel)
				}
				return []*ec2GoSshResult{{TargetAddr: "host0.example.invalid", Err: context.Canceled}}, context.Canceled
			})
			wantCalls := 1
			if phase == "before-lookup" {
				wantCalls = 0
			}
			if !errors.Is(err, context.Canceled) || len(ready) != 0 || reads != wantCalls || sshCalls != wantCalls {
				t.Fatalf("ready=%v error=%v reads=%d SSH calls=%d", ready, err, reads, sshCalls)
			}
		})
	}
}

func TestEC2GoSshRejectsInvalidConfiguration(t *testing.T) {
	_, key, err := SshKeygenEd25519()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*EC2GoSshInput)
	}{
		{"missing-user", func(input *EC2GoSshInput) { input.User = "" }},
		{"missing-key", func(input *EC2GoSshInput) { input.Ed25519PrivKey = "" }},
		{"invalid-key", func(input *EC2GoSshInput) { input.Ed25519PrivKey = "invalid" }},
		{"invalid-rsa", func(input *EC2GoSshInput) { input.RsaPrivKey = "invalid" }},
		{"negative-concurrency", func(input *EC2GoSshInput) { input.MaxConcurrency = -1 }},
		{"negative-timeout", func(input *EC2GoSshInput) { input.TimeoutSeconds = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := &EC2GoSshInput{TargetAddrs: []string{"host.example.invalid"}, User: "fixture", Ed25519PrivKey: key}
			test.edit(input)
			results, err := EC2GoSsh(context.Background(), input)
			if err == nil || len(results) != 0 {
				t.Fatalf("invalid config reached per-host workers: results=%v error=%v", results, err)
			}
		})
	}
}

func TestEC2WaitGoSshKeepsSurvivorsAfterTerminalState(t *testing.T) {
	for _, state := range []string{"shutting-down", "terminated"} {
		t.Run(state, func(t *testing.T) {
			good, interrupted := "i-11111111111111111", "i-22222222222222222"
			input := ec2WaitGoSshTestInput(t, good, interrupted)
			reads, probes := 0, 0
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				if form.Get("Action") != "DescribeInstances" {
					t.Fatalf("unexpected request: %v", form)
				}
				reads++
				current := "pending"
				if reads > 1 {
					current = state
				}
				body := ec2WaitGoSshTestInstances(good, interrupted)
				prefix := fmt.Sprintf("<instanceId>%s</instanceId><instanceState><name>", interrupted)
				return strings.Replace(body, prefix+"running", prefix+current, 1), nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ready, err := ec2WaitGoSsh(ctx, input, func(_ context.Context, sshInput *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				probes++
				if !slices.Equal(sshInput.TargetAddrs, []string{"host0.example.invalid"}) {
					t.Fatalf("unexpected SSH targets: %v", sshInput.TargetAddrs)
				}
				return []*ec2GoSshResult{{TargetAddr: "host0.example.invalid"}}, nil
			})
			if err != nil || !slices.Equal(ready, []string{good}) || reads != 2 || probes != 2 {
				t.Fatalf("healthy fleet survivor was not returned: ready=%v error=%v reads=%d successful-SSH-probes=%d", ready, err, reads, probes)
			}
		})
	}
}

func TestEC2WaitGoSshRetainsInitiallyInvisibleExplicitID(t *testing.T) {
	for _, state := range []string{"pending", "running"} {
		t.Run(state, func(t *testing.T) {
			first, second := "i-11111111111111111", "i-22222222222222222"
			input := ec2WaitGoSshTestInput(t, first, second)
			reads := 0
			var lastFilter []string
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				if form.Get("Action") != "DescribeInstances" || form.Get("Filter.1.Name") != "instance-id" {
					t.Fatalf("unexpected request: %v", form)
				}
				reads++
				lastFilter = ec2WaitGoSshTestValues(form, "Filter.1.Value.")
				if reads == 1 {
					return strings.ReplaceAll(ec2WaitGoSshTestInstances(first), "running", state), nil
				}
				// Both guests are now visible and running. Return only those queried.
				return ec2WaitGoSshTestInstances(lastFilter...), nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ready, err := ec2WaitGoSsh(ctx, input, func(_ context.Context, sshInput *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				if reads != 2 || len(sshInput.TargetAddrs) != 2 {
					t.Fatalf("SSH started without the complete explicit selection: reads=%d targets=%v", reads, sshInput.TargetAddrs)
				}
				var results []*ec2GoSshResult
				for _, addr := range sshInput.TargetAddrs {
					results = append(results, &ec2GoSshResult{TargetAddr: addr})
				}
				return results, nil
			})
			slices.Sort(ready)
			if err != nil || !slices.Equal(ready, []string{first, second}) || !slices.Equal(input.Selectors, []string{first, second}) {
				t.Fatalf("explicitly selected instance was forgotten: ready=%v error=%v reads=%d final-ID-filter=%v", ready, err, reads, lastFilter)
			}
		})
	}
}

func TestEC2WaitGoSshIncompleteExplicitIDsHaveBoundedDiscovery(t *testing.T) {
	first, missing := "i-11111111111111111", "i-22222222222222222"
	input := ec2WaitGoSshTestInput(t, first, missing)
	reads := 0
	installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
		if form.Get("Action") != "DescribeInstances" || !slices.Equal(ec2WaitGoSshTestValues(form, "Filter.1.Value."), []string{first, missing}) {
			t.Fatalf("missing metadata changed the selection or allowed a mutation: %v", form)
		}
		reads++
		return ec2WaitGoSshTestInstances(first), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ready, err := ec2WaitGoSsh(ctx, input, func(context.Context, *EC2GoSshInput) ([]*ec2GoSshResult, error) {
		t.Fatal("incomplete explicit selection reached SSH")
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete selected instance lookup") || len(ready) != 0 || reads < 2 || reads > 17 || ctx.Err() != nil {
		t.Fatalf("discovery did not fail within its own bound: ready=%v error=%v reads=%d context=%v", ready, err, reads, ctx.Err())
	}
}

func TestEC2WaitGoSshDoesNotRequeryTerminalIDs(t *testing.T) {
	first, terminal := "i-11111111111111111", "i-22222222222222222"
	for _, allTerminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("all-terminal=%t", allTerminal), func(t *testing.T) {
			input := ec2WaitGoSshTestInput(t, first, terminal)
			reads := 0
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				if form.Get("Action") != "DescribeInstances" {
					t.Fatalf("terminal instances caused a mutation: %v", form)
				}
				reads++
				if reads == 1 {
					if allTerminal {
						return strings.ReplaceAll(ec2WaitGoSshTestInstances(first, terminal), "running", "terminated"), nil
					}
					body := strings.ReplaceAll(ec2WaitGoSshTestInstances(first, terminal), "running", "pending")
					prefix := fmt.Sprintf("<instanceId>%s</instanceId><instanceState><name>", terminal)
					return strings.Replace(body, prefix+"pending", prefix+"shutting-down", 1), nil
				}
				if allTerminal || !slices.Equal(ec2WaitGoSshTestValues(form, "Filter.1.Value."), []string{first}) {
					t.Fatalf("terminal IDs were requeried or selection expanded: %v", form)
				}
				return ec2WaitGoSshTestInstances(first), nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ready, err := ec2WaitGoSsh(ctx, input, func(_ context.Context, sshInput *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				if allTerminal || !slices.Equal(sshInput.TargetAddrs, []string{"host0.example.invalid"}) {
					t.Fatalf("invalid SSH targets: %v", sshInput.TargetAddrs)
				}
				return []*ec2GoSshResult{{TargetAddr: "host0.example.invalid"}}, nil
			})
			if allTerminal {
				if err == nil || err.Error() != "no instances became ready" || len(ready) != 0 || reads != 1 {
					t.Fatalf("all-terminal selection: ready=%v error=%v reads=%d", ready, err, reads)
				}
			} else if err != nil || !slices.Equal(ready, []string{first}) || reads != 2 {
				t.Fatalf("lost remaining pending guest: ready=%v error=%v reads=%d", ready, err, reads)
			}
		})
	}
}

func TestEC2WaitGoSshLiteralIDsAndSelectorExpressions(t *testing.T) {
	first, second := "i-11111111", "i-22222222222222222"
	for _, test := range []struct {
		name      string
		selectors []string
		filter    string
	}{
		{"literal-IDs", []string{first, second, first}, "instance-id"},
		{"ID-pattern", []string{"i-*"}, "instance-id"},
		{"DNS-name", []string{"i-fixture.amazonaws.com"}, "dns-name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := ec2WaitGoSshTestInput(t, slices.Clone(test.selectors)...)
			installEC2WaitGoSshTestClient(t, func(form url.Values) (string, error) {
				if form.Get("Action") != "DescribeInstances" || form.Get("Filter.1.Name") != test.filter {
					t.Fatalf("wrong selector interpretation: %v", form)
				}
				return ec2WaitGoSshTestInstances(first, second), nil
			})
			ready, err := ec2WaitGoSsh(context.Background(), input, func(_ context.Context, sshInput *EC2GoSshInput) ([]*ec2GoSshResult, error) {
				return []*ec2GoSshResult{{TargetAddr: sshInput.TargetAddrs[0]}, {TargetAddr: sshInput.TargetAddrs[1]}}, nil
			})
			slices.Sort(ready)
			if err != nil || !slices.Equal(ready, []string{first, second}) || !slices.Equal(input.Selectors, test.selectors) {
				t.Fatalf("ready=%v error=%v caller selectors=%v", ready, err, input.Selectors)
			}
		})
	}
}
