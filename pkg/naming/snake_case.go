package naming

import (
	"regexp"
	"strings"
	"unicode"
)

type snakeCaseNaming struct{}

func (s *snakeCaseNaming) Normalize(name string) string {
	return ToSnakeCase(name)
}

func (s *snakeCaseNaming) Name() string {
	return "snake_case"
}

var (
	camelCaseRegex1 = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	camelCaseRegex2 = regexp.MustCompile(`([a-z\d])([A-Z])`)
	multiUnderscore = regexp.MustCompile(`_+`)
)

// ToSnakeCase converts a string to snake_case.
// The string is split on "__",
// empty segments are dropped, each segment is normalized independently, then
// segments are rejoined with "__". Trailing underscores on the whole string
// are converted to x first, so "foo___" becomes "fooxxx" instead of being
// lost to the split.
//
// Per-segment normalization:
//   - `+` and `*` become `x`, `-` becomes `_`, `@` becomes `a`, `|` becomes `l`
//   - other non-alphanumeric characters become `_`
//   - camelCase is split
//   - lowercased
//   - leading digits get a `_` prefix
//   - underscore runs collapse to a single `_`
//   - each trailing `_` becomes `x`
func ToSnakeCase(name string) string {
	if name == "" {
		return name
	}

	name = replaceTrailingUnderscores(name)

	parts := strings.Split(name, "__")
	normalized := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		normalized = append(normalized, normalizeSegment(p))
	}
	result := strings.Join(normalized, "__")

	if len(result) > 0 && unicode.IsDigit(rune(result[0])) {
		result = "_" + result
	}
	return result
}

func replaceTrailingUnderscores(s string) string {
	trimmed := strings.TrimRight(s, "_")
	return trimmed + strings.Repeat("x", len(s)-len(trimmed))
}

func normalizeSegment(s string) string {
	result := make([]rune, 0, len(s)*2)
	for _, r := range s {
		switch r {
		case '+', '*':
			result = append(result, 'x')
		case '-':
			result = append(result, '_')
		case '@':
			result = append(result, 'a')
		case '|':
			result = append(result, 'l')
		default:
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				result = append(result, r)
			} else {
				result = append(result, '_')
			}
		}
	}

	s = string(result)
	s = camelCaseRegex1.ReplaceAllString(s, "${1}_${2}")
	s = camelCaseRegex2.ReplaceAllString(s, "${1}_${2}")
	s = strings.ToLower(s)

	s = multiUnderscore.ReplaceAllString(s, "_")

	return replaceTrailingUnderscores(s)
}
