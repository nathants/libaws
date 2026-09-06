package lib

import (
	"strings"
	"testing"
)

func TestLambdaEnvironmentVariablesEnforcesAWSSizeLimit(t *testing.T) {
	// Each value occupies 4087 bytes in AWS's JSON representation; {"AA":""}
	// accounts for the remaining 9. The request-size limit is checked separately.
	for _, test := range []struct{ name, value string }{
		{"ASCII", strings.Repeat("x", 4087)},
		{"HTML", strings.Repeat("<>&", 1362) + "x"},
		{"UTF8", strings.Repeat("é", 2043) + "x"},
		{"line separator", "\u2028" + strings.Repeat("x", 4084)},
		{"paragraph separator", "\u2029" + strings.Repeat("x", 4084)},
		{"quotes", strings.Repeat("\"", 2043) + "x"},
		{"backslashes", strings.Repeat("\\", 2043) + "x"},
		{"literal Unicode escape", strings.Repeat(`\u2028`, 583) + "xxxxxx"},
		{"newline", strings.Repeat("\n", 2043) + "x"},
		{"backspace", strings.Repeat("\b", 2043) + "x"},
		{"form feed", strings.Repeat("\f", 2043) + "x"},
		{"long control escape", strings.Repeat("\x01", 681) + "x"},
	} {
		t.Run(test.name, func(t *testing.T) {
			atLimit, err := lambdaEnvironmentVariables([]string{"AA=" + test.value})
			if err != nil {
				t.Fatalf("environment at AWS limit was rejected: %v", err)
			}
			if len(atLimit) != 1 || atLimit["AA"] != test.value {
				t.Fatal("environment value was not preserved exactly")
			}
			_, err = lambdaEnvironmentVariables([]string{"AA=" + test.value + "x"})
			if err == nil || !strings.Contains(err.Error(), "size 4097 bytes exceeds AWS Lambda limit 4096 bytes") {
				t.Fatalf("oversized Lambda environment error=%v", err)
			}
		})
	}
}

func TestLambdaEnvironmentVariablesCountsMultipleEntries(t *testing.T) {
	// {"AA":"","BB":""} has 17 bytes of syntax and names.
	value := strings.Repeat("x", 4079)
	variables, err := lambdaEnvironmentVariables([]string{"AA=" + value, "BB="})
	if err != nil {
		t.Fatal(err)
	}
	if got, exists := variables["BB"]; !exists || got != "" || variables["AA"] != value {
		t.Fatal("environment entries were not preserved exactly")
	}
	_, err = lambdaEnvironmentVariables([]string{"AA=" + value, "BB=x"})
	if err == nil || !strings.Contains(err.Error(), "size 4097 bytes") {
		t.Fatalf("oversized multi-entry environment error=%v", err)
	}
}

func TestLambdaEnvironmentVariablesRejectsDuplicateNames(t *testing.T) {
	_, err := lambdaEnvironmentVariables([]string{"AA=first", "AA=second"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate Lambda environment error=%v", err)
	}
}

func TestLambdaEnvironmentVariablesRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "A", "_A", "A-B"} {
		_, err := lambdaEnvironmentVariables([]string{name + "=value"})
		if err == nil || !strings.Contains(err.Error(), "names must match") {
			t.Fatalf("invalid Lambda environment name %q error=%v", name, err)
		}
	}
}
