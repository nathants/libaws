package lib

import (
	"testing"
)

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
			t.Errorf("got:\n%s\nwant:\n%s\n", output, test.output)
		}
	}
}
