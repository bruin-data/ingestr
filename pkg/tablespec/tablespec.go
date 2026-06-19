// Package tablespec parses ingestr table strings that carry URL-style query
// parameters layered on top of a base path or object name, for example:
//
//	Reports/q1.xlsx?sheet=Sheet1&skip=2
//	items?board_ids=12345&board_ids=67890
//
// The query form gives connectors a single, consistent way to express per-table
// options that mirrors the URI concept, and lets tools such as Bruin compile a
// structured parameter list down to the table string generically — a YAML list
// becomes a repeated query key, which url.Values represents as a slice.
//
// Connectors adopt it incrementally: Split reports whether a query component was
// present, so a connector can keep its existing (legacy) table-string parsing as
// the authority whenever the table string carries no parameter block.
package tablespec

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// paramKeyToken matches a single "key" or "key=value" query token. The key must
// be a bare identifier, which is what distinguishes a real parameter block from a
// path that merely contains "?" — a file extension (".xlsx") or glob literal does
// not look like "identifier[=value]".
var paramKeyToken = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(=.*)?$`)

// Split separates a raw table string into its base path/object name and any
// URL-style query parameters. hasQuery reports whether a parameter block was
// found; when false, params is empty and the connector should fall back to its
// legacy table-string parsing, leaving "?" as part of the path.
//
// The split is on the LAST "?", and only when the text after it looks like a
// parameter block (see looksLikeParams). This keeps a "?" used as a glob wildcard
// — e.g. "Reports/q?.xlsx" — or as a URL's own query delimiter out of harm's way.
// Query values are URL-decoded; the path is returned verbatim (it may contain
// spaces and "&"). A literal "?" that must sit in the path alongside parameters
// should be percent-encoded.
func Split(raw string) (path string, params url.Values, hasQuery bool, err error) {
	i := strings.LastIndexByte(raw, '?')
	if i < 0 || !looksLikeParams(raw[i+1:]) {
		return raw, url.Values{}, false, nil
	}
	params, err = url.ParseQuery(raw[i+1:])
	if err != nil {
		return raw[:i], nil, true, fmt.Errorf("invalid table parameters: %w", err)
	}
	return raw[:i], params, true, nil
}

// looksLikeParams reports whether s (the text after the last "?") has the shape
// of a query parameter block: it must contain at least one "=" and every
// non-empty "&"-separated token must be an identifier key (optionally "=value").
// Requiring "=" means a bare glob fragment such as "x" or ".xlsx" is treated as
// path, not parameters; a query of only bare flags must therefore be written with
// an explicit value (e.g. "raw=true").
func looksLikeParams(s string) bool {
	if !strings.Contains(s, "=") {
		return false
	}
	for _, tok := range strings.Split(s, "&") {
		if tok == "" {
			continue
		}
		if !paramKeyToken.MatchString(tok) {
			return false
		}
	}
	return true
}

// ValidateKeys returns an error if params contains any key absent from known.
// It lets connectors reject typos loudly rather than silently ignoring them.
// Matching is case-sensitive; unknown keys are reported sorted for a stable
// message.
func ValidateKeys(params url.Values, known ...string) error {
	allowed := make(map[string]struct{}, len(known))
	for _, k := range known {
		allowed[k] = struct{}{}
	}

	var unknown []string
	for k := range params {
		if _, ok := allowed[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}

	sort.Strings(unknown)
	return fmt.Errorf("unknown table parameter(s): %s (supported: %s)",
		strings.Join(unknown, ", "), strings.Join(known, ", "))
}
