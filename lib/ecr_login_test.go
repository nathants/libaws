package lib

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

func TestEcrLoginCommandValidatesAuthorization(t *testing.T) {
	endpoint := "https://123456789012.dkr.ecr.us-west-2.amazonaws.com"
	valid := ecrtypes.AuthorizationData{
		AuthorizationToken: aws.String(base64.StdEncoding.EncodeToString([]byte("AWS:secret:with:colons"))),
		ProxyEndpoint:      aws.String(endpoint),
	}
	for _, test := range []struct {
		name   string
		output *ecr.GetAuthorizationTokenOutput
	}{
		{"nil output", nil},
		{"no credentials", &ecr.GetAuthorizationTokenOutput{}},
		{"multiple credentials", &ecr.GetAuthorizationTokenOutput{AuthorizationData: []ecrtypes.AuthorizationData{valid, valid}}},
		{"nil token", &ecr.GetAuthorizationTokenOutput{AuthorizationData: []ecrtypes.AuthorizationData{{ProxyEndpoint: valid.ProxyEndpoint}}}},
		{"nil endpoint", &ecr.GetAuthorizationTokenOutput{AuthorizationData: []ecrtypes.AuthorizationData{{AuthorizationToken: valid.AuthorizationToken}}}},
		{"empty endpoint", &ecr.GetAuthorizationTokenOutput{AuthorizationData: []ecrtypes.AuthorizationData{{AuthorizationToken: valid.AuthorizationToken, ProxyEndpoint: aws.String("")}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ecrLoginCommand(context.Background(), test.output); err == nil {
				t.Fatal("accepted invalid ECR authorization")
			}
		})
	}
	for _, token := range []string{"!invalid-base64!", "", "AWS", "AWS:", ":secret"} {
		t.Run("token "+token, func(t *testing.T) {
			authorization := valid
			value := token
			if token != "!invalid-base64!" {
				value = base64.StdEncoding.EncodeToString([]byte(token))
			}
			authorization.AuthorizationToken = aws.String(value)
			if _, err := ecrLoginCommand(context.Background(), &ecr.GetAuthorizationTokenOutput{AuthorizationData: []ecrtypes.AuthorizationData{authorization}}); err == nil {
				t.Fatal("accepted malformed ECR token")
			}
		})
	}
	command, err := ecrLoginCommand(context.Background(), &ecr.GetAuthorizationTokenOutput{AuthorizationData: []ecrtypes.AuthorizationData{valid}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "login", "--username", "AWS", "--password-stdin", endpoint}
	if !slices.Equal(command.Args, want) {
		t.Fatalf("Docker arguments = %v, want %v", command.Args, want)
	}
	password, err := io.ReadAll(command.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if string(password) != "secret:with:colons" {
		t.Fatalf("Docker stdin = %q", password)
	}
}

func TestEcrLoginRunsDockerWithoutExposingPassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Amz-Target") != "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken" {
			http.Error(writer, "unexpected ECR operation", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if _, err := io.WriteString(writer, `{"authorizationData":[{"authorizationToken":"QVdTOnNlY3JldA==","proxyEndpoint":"https://registry.example.invalid"}]}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{
		Region:      "us-west-2",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		HTTPClient:  server.Client(),
	}, func(options *ecr.Options) { options.BaseEndpoint = aws.String(server.URL) })
	ecrClientLock.Lock()
	previous := ecrClient
	ecrClient = client
	ecrClientLock.Unlock()
	t.Cleanup(func() {
		ecrClientLock.Lock()
		ecrClient = previous
		ecrClientLock.Unlock()
	})
	bin := t.TempDir()
	// A process-boundary assertion: the password must arrive only on stdin.
	program := `#!/bin/bash
set -euo pipefail
[[ "$*" == 'login --username AWS --password-stdin https://registry.example.invalid' ]]
[[ $(cat) == secret ]]
echo docker-stdout
echo docker-stderr >&2
exit "${TEST_DOCKER_STATUS:-0}"
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, status := range []string{"0", "23"} {
		t.Setenv("TEST_DOCKER_STATUS", status)
		var stdout, stderr bytes.Buffer
		err := EcrLogin(context.Background(), &stdout, &stderr)
		if (err == nil) != (status == "0") {
			t.Fatalf("Docker status %s: %v", status, err)
		}
		if strings.TrimSpace(stdout.String()) != "docker-stdout" || strings.TrimSpace(stderr.String()) != "docker-stderr" {
			t.Fatalf("Docker output: stdout=%q stderr=%q", &stdout, &stderr)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := EcrLogin(ctx, io.Discard, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled login error = %v", err)
	}
}
