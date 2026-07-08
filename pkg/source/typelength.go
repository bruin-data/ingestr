package source

import (
	"regexp"
	"strconv"
)

var sizedStringLengthRegex = regexp.MustCompile(`\((\d+)\)`)

// ParseSizedStringLength extracts the declared length from a parameterized
// string type such as "varchar(50)" or "char(10)", returning 0 when the type
// carries no length (e.g. "varchar", "string", "text").
//
// It should only be called for columns already identified as string types so
// that parenthesized parameters of other types (e.g. decimal precision) are
// never misread as a length.
func ParseSizedStringLength(typeStr string) int {
	matches := sizedStringLengthRegex.FindStringSubmatch(typeStr)
	if len(matches) < 2 {
		return 0
	}
	length, err := strconv.Atoi(matches[1])
	if err != nil || length <= 0 {
		return 0
	}
	return length
}
