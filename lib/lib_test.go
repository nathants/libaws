package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

func lambdaZipHashForTest(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func checkAccountS3() {
	account, err := StsAccount(context.Background())
	if err != nil {
		panic(err)
	}
	if os.Getenv("LIBAWS_TEST_ACCOUNT") != account {
		panic(fmt.Sprintf("%s != %s", os.Getenv("LIBAWS_TEST_ACCOUNT"), account))
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
