package postgres_cdc

import (
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
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

func TestConvertTextValueRejectsUnsupportedArrayShapes(t *testing.T) {
	col := schema.Column{Name: "items", DataType: schema.TypeArray, ArrayType: schema.TypeString}
	for _, literal := range []string{"[2:3]={a,b}", "{{a,b},{c,d}}"} {
		t.Run(literal, func(t *testing.T) {
			value, err := convertTextValue(literal, col)
			require.Nil(t, value)
			require.ErrorContains(t, err, "only one-dimensional arrays with default bounds are supported")
		})
	}
}

func TestConvertTextValueUsesArrayElementMetadata(t *testing.T) {
	col := schema.Column{
		Name:      "items",
		DataType:  schema.TypeArray,
		ArrayType: schema.TypeString,
		Element:   &schema.Column{DataType: schema.TypeInt32},
	}

	value, err := convertTextValue("{1,2}", col)
	require.NoError(t, err)
	require.Equal(t, []interface{}{int32(1), int32(2)}, value)
}

func TestConvertTextValueUsesPostgresArrayDelimiter(t *testing.T) {
	col := schema.Column{
		Name: "boxes", DataType: schema.TypeArray, ArrayType: schema.TypeString, ArrayDelimiter: ";",
	}

	value, err := convertTextValue("{(1,1),(0,0);(2,2),(1,1)}", col)
	require.NoError(t, err)
	require.Equal(t, []interface{}{"(1,1),(0,0)", "(2,2),(1,1)"}, value)
}
