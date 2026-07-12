package postgres_cdc

import "strings"

// arrayElement is one element of a parsed Postgres array literal. A NULL element
// (the unquoted token NULL) is distinguished from the string "NULL", which
// Postgres always emits quoted.
type arrayElement struct {
	value  string
	isNull bool
}

// parsePostgresArrayLiteral parses the text output of a one-dimensional Postgres
// array (array_out format) into its elements: e.g. `{a,b}`, `{"x,y",NULL}`, or
// `{}` for an empty array. It returns ok=false when the input is not a
// brace-delimited array literal.
//
// Logical replication delivers array columns (jsonb[], text[], int[], ...) in
// this format, not as JSON arrays, so the streaming decoder must parse it to
// recover the elements. Quoted elements are unescaped (\" -> ", \\ -> \), so a
// jsonb[] element is returned as standalone JSON ready for per-element typing.
func parsePostgresArrayLiteral(s string) ([]arrayElement, bool) {
	return parsePostgresArrayLiteralWithDelimiter(s, ',')
}

func parsePostgresArrayLiteralWithDelimiter(s string, delimiter byte) ([]arrayElement, bool) {
	if delimiter == 0 {
		delimiter = ','
	}
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, false
	}

	inner := s[1 : len(s)-1]
	elems := make([]arrayElement, 0)
	if strings.TrimSpace(inner) == "" {
		return elems, true
	}

	i, n := 0, len(inner)
	for i < n {
		for i < n && (inner[i] == ' ' || inner[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}

		if inner[i] == '"' {
			i++
			var b strings.Builder
			for i < n {
				c := inner[i]
				if c == '\\' && i+1 < n {
					b.WriteByte(inner[i+1])
					i += 2
					continue
				}
				if c == '"' {
					i++
					break
				}
				b.WriteByte(c)
				i++
			}
			elems = append(elems, arrayElement{value: b.String()})
			for i < n && inner[i] != delimiter {
				i++
			}
			if i < n {
				i++
			}
			continue
		}

		start := i
		// A bare '{' signals a nested/multidimensional array, which this
		// one-dimensional parser cannot handle. Return ok=false so the caller
		// falls back to nil instead of producing garbled elements.
		if inner[i] == '{' {
			return nil, false
		}
		for i < n && inner[i] != delimiter {
			i++
		}
		raw := strings.TrimSpace(inner[start:i])
		if i < n {
			i++
		}
		if strings.EqualFold(raw, "NULL") {
			elems = append(elems, arrayElement{isNull: true})
		} else {
			elems = append(elems, arrayElement{value: raw})
		}
	}

	return elems, true
}
