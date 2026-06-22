// Package tablespec parses ingestr table strings that carry URL-style query
// parameters on top of a base path or object name, e.g.
// "Reports/q1.xlsx?sheet=Sheet1&skip=2". Parse is the single entry point a
// connector uses: it returns the base path and decodes any parameters onto a
// struct via mapstructure tags.
package tablespec

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
)

// paramKeyToken matches a "key" or "key=value" query token: a bare identifier,
// optionally dotted for nesting (a.b). It distinguishes a parameter block from a
// path that merely contains "?".
var paramKeyToken = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*(=.*)?$`)

// Split separates a raw table string into its base path/object name and any
// URL-style query parameters. hasQuery reports whether a parameter block was
// found; when false params is empty and the path is returned verbatim, so a
// connector can fall back to its legacy table-string parsing.
func Split(raw string) (path string, params url.Values, hasQuery bool, err error) {
	// Split on the LAST "?" so a glob wildcard ("Reports/q?.xlsx") stays in the path.
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

// looksLikeParams reports whether s has the shape of a query parameter block.
// It must contain "=" so a bare glob fragment (".xlsx") is treated as path, not
// parameters; consequently a bare flag must be written with a value ("raw=true").
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
// Matching is case-sensitive; unknown keys are reported sorted.
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

// ParseOption configures Parse.
type ParseOption func(*decodeConfig)

type decodeConfig struct {
	listSep string
}

// WithListSeparator sets the separator used to split a single string value into a
// slice field. Defaults to ",". A repeated query key forms a slice regardless.
func WithListSeparator(sep string) ParseOption {
	return func(c *decodeConfig) { c.listSep = sep }
}

// Parse is the single entry point for the URL-style table form. It splits raw into
// the base path and, when a query block is present, decodes the parameters onto
// out (a struct with mapstructure tags) and returns hasParams=true. When no query
// block is present, out is untouched and hasParams is false, so the caller runs
// its legacy table-string parser on the returned path. path is always returned.
//
// String values are coerced to each field's type: a repeated or separator-joined
// value fills a []string, a bare flag (?x with no value) sets a bool to true, and
// dotted keys nest ("a.b=c"). A parameter with no matching field is rejected, so
// the struct defines what the connector accepts.
func Parse(raw string, out any, opts ...ParseOption) (path string, hasParams bool, err error) {
	cfg := decodeConfig{listSep: ","}
	for _, fn := range opts {
		fn(&cfg)
	}

	var params url.Values
	path, params, hasParams, err = Split(raw)
	if err != nil || !hasParams {
		return path, hasParams, err
	}
	if err = decode(params, out, cfg); err != nil {
		return path, hasParams, err
	}
	return path, hasParams, nil
}

// decode maps the query parameters onto out via mapstructure, applying the list
// and bool conventions and rejecting any key without a matching field.
func decode(params url.Values, out any, cfg decodeConfig) error {
	var md mapstructure.Metadata
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           out,
		Metadata:         &md,
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			stringToBoolHook(),
			stringToSliceHook(cfg.listSep),
		),
	})
	if err != nil {
		return fmt.Errorf("invalid table parameters: %w", err)
	}
	if err := dec.Decode(valuesToNestedMap(params)); err != nil {
		return fmt.Errorf("invalid table parameters: %w", err)
	}
	if len(md.Unused) > 0 {
		sort.Strings(md.Unused)
		return fmt.Errorf("unknown table parameter(s): %s", strings.Join(md.Unused, ", "))
	}
	return nil
}

// valuesToNestedMap turns a single value into a string, a repeated key into a
// []string, and a dotted key into nested maps ("a.b" -> {"a": {"b": ...}}).
func valuesToNestedMap(params url.Values) map[string]any {
	root := map[string]any{}
	for key, vals := range params {
		var v any = vals
		if len(vals) == 1 {
			v = vals[0]
		}
		assignNested(root, strings.Split(key, "."), v)
	}
	return root
}

func assignNested(m map[string]any, path []string, val any) {
	for _, seg := range path[:len(path)-1] {
		next, ok := m[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[seg] = next
		}
		m = next
	}
	m[path[len(path)-1]] = val
}

// stringToBoolHook decodes a string into a bool: an empty value (a bare flag) is
// true, otherwise it must parse as a Go boolean.
func stringToBoolHook() mapstructure.DecodeHookFunc {
	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to.Kind() != reflect.Bool {
			return data, nil
		}
		s := strings.TrimSpace(data.(string))
		if s == "" {
			return true, nil
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("expected a boolean (true/false), got %q", s)
		}
		return b, nil
	}
}

// stringToSliceHook splits a single string into a []string field on sep, trimming
// each element and dropping empties. Repeated keys (already a slice) bypass it.
func stringToSliceHook(sep string) mapstructure.DecodeHookFunc {
	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to.Kind() != reflect.Slice || to.Elem().Kind() != reflect.String {
			return data, nil
		}
		out := []string{}
		for _, part := range strings.Split(data.(string), sep) {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	}
}
