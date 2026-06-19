// Package tablespec parses ingestr table strings that carry URL-style query
// parameters on top of a base path or object name, e.g.
// "Reports/q1.xlsx?sheet=Sheet1&skip=2". Split separates the two parts and
// Decode maps the parameters onto a struct via mapstructure tags.
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

// DecodeOption configures Decode.
type DecodeOption func(*decodeConfig)

type decodeConfig struct {
	listSep string
}

// WithListSeparator sets the separator used to split a single string value into a
// slice field. Defaults to ",". A repeated query key forms a slice regardless.
func WithListSeparator(sep string) DecodeOption {
	return func(c *decodeConfig) { c.listSep = sep }
}

// Decode maps a table path and the query parameters from Split onto out, a struct
// with "table" and "parameters" mapstructure fields. String values are coerced to
// each field's type: a repeated/separator-joined value fills a []string, a bare
// flag sets a bool to true, and dotted keys nest ("a.b=c"). Unknown parameters are
// rejected, so the struct defines what the connector accepts.
func Decode(table string, params url.Values, out any, opts ...DecodeOption) error {
	cfg := decodeConfig{listSep: ","}
	for _, fn := range opts {
		fn(&cfg)
	}

	input := map[string]any{
		"table":      table,
		"parameters": valuesToNestedMap(params),
	}

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
	if err := dec.Decode(input); err != nil {
		return fmt.Errorf("invalid table parameters: %w", err)
	}
	if unknown := unusedParamKeys(md.Unused); len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown table parameter(s): %s", strings.Join(unknown, ", "))
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

// unusedParamKeys strips the "parameters." prefix mapstructure prepends so error
// messages read in the user's terms.
func unusedParamKeys(unused []string) []string {
	out := make([]string, 0, len(unused))
	for _, k := range unused {
		out = append(out, strings.TrimPrefix(k, "parameters."))
	}
	return out
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
