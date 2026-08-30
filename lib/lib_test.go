package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"testing"
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
	account, err := StsAccount(context.Background())
	if err != nil {
		t.Fatalf("verify AWS account: %v", err)
	}
	if account != expectedAccount {
		t.Fatalf("AWS account = %s, want guarded account %s", account, expectedAccount)
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
