package postgres

import (
	"reflect"
	"testing"
)

func TestParsePostgresArrayLiteral(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []ArrayElement
		ok    bool
	}{
		{"empty array", "{}", []ArrayElement{}, true},
		{"single text", "{a}", []ArrayElement{{Value: "a"}}, true},
		{"two text", "{snap-a,snap-b}", []ArrayElement{{Value: "snap-a"}, {Value: "snap-b"}}, true},
		{
			"quoted with embedded comma",
			`{x,"has,comma"}`,
			[]ArrayElement{{Value: "x"}, {Value: "has,comma"}},
			true,
		},
		{
			"sql null vs quoted NULL string",
			`{NULL,"NULL"}`,
			[]ArrayElement{{IsNull: true}, {Value: "NULL"}},
			true,
		},
		{
			"jsonb elements are unescaped",
			`{"{\"error\": \"boom\"}","{\"a\": 1}"}`,
			[]ArrayElement{{Value: `{"error": "boom"}`}, {Value: `{"a": 1}`}},
			true,
		},
		{
			"escaped quote and backslash",
			`{"a\"b","c\\d"}`,
			[]ArrayElement{{Value: `a"b`}, {Value: `c\d`}},
			true,
		},
		{"multidimensional array is flattened", `{{a,"b,c"},{NULL,d}}`, []ArrayElement{{Value: "a"}, {Value: "b,c"}, {IsNull: true}, {Value: "d"}}, true},
		{"nested empty arrays", "{{},{}}", []ArrayElement{}, true},
		{"explicit bounds", "[0:1]={x,y}", []ArrayElement{{Value: "x"}, {Value: "y"}}, true},
		{"explicit multidimensional bounds", "[1:1][-1:0]={{1,2}}", []ArrayElement{{Value: "1"}, {Value: "2"}}, true},
		{"unbalanced braces", "{{a}", nil, false},
		{"two top-level arrays", "{a}{b}", nil, false},
		{"unterminated quote", `{"a}`, nil, false},
		{"not an array literal", "boom", nil, false},
		{"empty string", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseArrayLiteral(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseArrayLiteralBoxDelimiter(t *testing.T) {
	got, ok := ParseArrayLiteralDelim("{(1,1),(0,0);(3,3),(2,2)}", ArrayDelimiter(1020))
	if !ok || !reflect.DeepEqual(got, []ArrayElement{{Value: "(1,1),(0,0)"}, {Value: "(3,3),(2,2)"}}) {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}
