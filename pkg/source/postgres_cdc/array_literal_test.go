package postgres_cdc

import (
	"reflect"
	"testing"
)

func TestParsePostgresArrayLiteral(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []arrayElement
		ok    bool
	}{
		{"empty array", "{}", []arrayElement{}, true},
		{"single text", "{a}", []arrayElement{{value: "a"}}, true},
		{"two text", "{snap-a,snap-b}", []arrayElement{{value: "snap-a"}, {value: "snap-b"}}, true},
		{
			"quoted with embedded comma",
			`{x,"has,comma"}`,
			[]arrayElement{{value: "x"}, {value: "has,comma"}},
			true,
		},
		{
			"sql null vs quoted NULL string",
			`{NULL,"NULL"}`,
			[]arrayElement{{isNull: true}, {value: "NULL"}},
			true,
		},
		{
			"jsonb elements are unescaped",
			`{"{\"error\": \"boom\"}","{\"a\": 1}"}`,
			[]arrayElement{{value: `{"error": "boom"}`}, {value: `{"a": 1}`}},
			true,
		},
		{
			"escaped quote and backslash",
			`{"a\"b","c\\d"}`,
			[]arrayElement{{value: `a"b`}, {value: `c\d`}},
			true,
		},
		{"multidimensional array unsupported", "{{1,2},{3,4}}", nil, false},
		{"not an array literal", "boom", nil, false},
		{"empty string", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePostgresArrayLiteral(tt.input)
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
