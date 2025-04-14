package lib

import (
	"reflect"
	"slices"
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
			t.Errorf("\ngot:\n%s\nwant:\n%s\n", output, test.output)
		}
	}
}

func TestChunk(t *testing.T) {
	type test struct {
		input  []string
		output [][]string
	}
	tests := []test{
		{[]string{"a", "b", "c", "d"}, [][]string{{"a", "b", "c"}, {"d"}}},
	}
	for _, test := range tests {
		output := slices.Chunk(test.input, 3)
		if !reflect.DeepEqual(output, test.output) {
			t.Errorf("\ngot:\n%v\nwant:\n%v\n", output, test.output)
		}
	}
}
