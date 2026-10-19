package postgres

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// ArrayElement is one element of a parsed Postgres array literal. A NULL element
// (the unquoted token NULL) is distinguished from the string "NULL", which
// Postgres always emits quoted.
type ArrayElement struct {
	Value  string
	IsNull bool
}

// ParseArrayLiteral parses the text output of a Postgres array (array_out
// format) into its elements: e.g. `{a,b}`, `{"x,y",NULL}`, or `{}` for an empty
// array. Multidimensional arrays are flattened in row-major order and an
// explicit bounds prefix (`[0:1]={x,y}`) is ignored, matching how pgx decodes
// binary arrays. It returns ok=false when the input is not an array literal.
//
// Logical replication delivers array columns (jsonb[], text[], int[], ...) in
// this format, not as JSON arrays, so the streaming decoder must parse it to
// recover the elements. Quoted elements are unescaped (\" -> ", \\ -> \), so a
// jsonb[] element is returned as standalone JSON ready for per-element typing.
func ParseArrayLiteral(s string) ([]ArrayElement, bool) {
	return ParseArrayLiteralDelim(s, ',')
}

// ArrayDelimiter returns the element delimiter PostgreSQL uses in the text
// output of an array type: box[] separates elements with ';' because each box
// contains commas.
func ArrayDelimiter(arrayOID uint32) byte {
	if arrayOID == pgtype.BoxArrayOID {
		return ';'
	}
	return ','
}

// ParseArrayLiteralDelim is ParseArrayLiteral with an explicit element delimiter.
func ParseArrayLiteralDelim(s string, delim byte) ([]ArrayElement, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		eq := strings.IndexByte(s, '=')
		if eq < 0 || strings.Trim(s[:eq], "[]:-0123456789") != "" {
			return nil, false
		}
		s = strings.TrimSpace(s[eq+1:])
	}
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, false
	}

	elems := make([]ArrayElement, 0)
	depth := 0
	for i, n := 0, len(s); i < n; {
		switch c := s[i]; {
		case c == '{':
			if depth == 0 && i > 0 {
				return nil, false
			}
			depth++
			i++
		case c == '}':
			depth--
			if depth < 0 {
				return nil, false
			}
			i++
		case c == delim || c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case depth == 0:
			return nil, false
		case c == '"':
			i++
			var b strings.Builder
			for i < n && s[i] != '"' {
				if s[i] == '\\' && i+1 < n {
					i++
				}
				b.WriteByte(s[i])
				i++
			}
			if i >= n {
				return nil, false
			}
			i++
			elems = append(elems, ArrayElement{Value: b.String()})
		default:
			start := i
			for i < n && s[i] != delim && s[i] != '}' {
				i++
			}
			raw := strings.TrimSpace(s[start:i])
			if strings.EqualFold(raw, "NULL") {
				elems = append(elems, ArrayElement{IsNull: true})
			} else {
				elems = append(elems, ArrayElement{Value: raw})
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	return elems, true
}
