package source

import (
	"regexp"
	"strconv"
)

var sizedStringLengthRegex = regexp.MustCompile(`\((\d+)\)`)

// ParseSizedStringLength extracts the length from a parameterized string type
// (e.g. "varchar(50)" -> 50); returns 0 when unsized. Call only for strings.
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
