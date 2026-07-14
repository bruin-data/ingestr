// Package tablename parses possibly-qualified table identifiers
// (table, schema.table, catalog.schema.table) into their components.
//
// It is the single source of truth for multi-component name parsing across
// every source and destination, replacing the ad-hoc strings.Split logic that
// used to live in each connector. The design mirrors Bruin CLI's pkg/tablename
// (bruin-data/bruin#2231) so the two tools parse asset names identically.
package tablename

import (
	"fmt"
	"strings"
)

// TableName is a parsed, possibly-qualified table identifier. Empty strings
// represent absent components.
type TableName struct {
	Catalog string // database / catalog / project
	Schema  string // schema / dataset
	Table   string
}

// Defaults supplies fallback values for leading components that are absent
// from a parsed name (typically sourced from the connection URI or config).
type Defaults struct {
	Catalog string
	Schema  string
}

// Capability describes how a platform names tables: how many dot-separated
// components it accepts and what each position is called.
type Capability struct {
	Platform      string
	MinComponents int
	MaxComponents int
	Labels        [3]string // {catalog-label, schema-label, table-label}
	Unbounded     bool      // allow more than MaxComponents (MSSQL linked servers)
	FormatDesc    string    // expected format for error text, e.g. "database.schema.table"
}

// Split breaks name on '.', honoring bracket ([]), double-quote ("") and
// backtick (“) quoting so a quoted identifier containing a dot is kept
// whole. Quotes are stripped from the returned components and doubled quote
// characters are unescaped. Ported from the MSSQL source splitter so quoting
// is handled in exactly one place.
func Split(name string) []string {
	parts := SplitRaw(name)
	for i, p := range parts {
		parts[i] = normalizePart(p)
	}
	return parts
}

// SplitRaw breaks name into the same components as Split while preserving
// identifier delimiters and escaping. It is intended for code that must render
// a derived identifier back into the same catalog or schema.
func SplitRaw(name string) []string {
	name = strings.TrimSpace(name)
	var parts []string
	var cur strings.Builder
	var closer byte // 0 when outside a quoted span; otherwise the closing char

	for i := 0; i < len(name); i++ {
		ch := name[i]
		if closer != 0 {
			cur.WriteByte(ch)
			if ch == closer {
				// A doubled closer ("" / ]] / ``) is an escaped literal.
				if i+1 < len(name) && name[i+1] == closer {
					i++
					cur.WriteByte(name[i])
					continue
				}
				closer = 0
			}
			continue
		}

		switch ch {
		case '[':
			closer = ']'
			cur.WriteByte(ch)
		case '"':
			closer = '"'
			cur.WriteByte(ch)
		case '`':
			closer = '`'
			cur.WriteByte(ch)
		case '.':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	parts = append(parts, cur.String())

	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func normalizePart(part string) string {
	part = strings.TrimSpace(part)
	if len(part) >= 2 {
		first, last := part[0], part[len(part)-1]
		switch {
		case first == '[' && last == ']':
			return strings.ReplaceAll(part[1:len(part)-1], "]]", "]")
		case first == '"' && last == '"':
			return strings.ReplaceAll(part[1:len(part)-1], `""`, `"`)
		case first == '`' && last == '`':
			return strings.ReplaceAll(part[1:len(part)-1], "``", "`")
		}
	}
	return part
}

// CheckName validates that raw has no empty components and a component count
// within the platform's [MinComponents, MaxComponents] range (the upper bound
// is ignored when Unbounded is set).
func (c Capability) CheckName(raw string) error {
	parts := Split(raw)
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return c.formatErr(raw)
		}
	}
	n := len(parts)
	if n < c.MinComponents || (!c.Unbounded && n > c.MaxComponents) {
		return c.formatErr(raw)
	}
	return nil
}

func (c Capability) formatErr(raw string) error {
	return fmt.Errorf("%s table name must be in format %s, %q given", c.Platform, c.FormatDesc, raw)
}

// Parse validates raw and resolves it into a TableName using right-alignment:
// the last component is the table, the next is the schema, and (when the
// platform allows three components) the first is the catalog. Missing leading
// components are filled from d. For unbounded platforms with more than three
// components (MSSQL linked servers) every component before schema.table is
// joined into Catalog; such connectors typically call Split directly instead.
func (c Capability) Parse(raw string, d Defaults) (TableName, error) {
	if err := c.CheckName(raw); err != nil {
		return TableName{}, err
	}
	parts := Split(raw)
	n := len(parts)

	tn := TableName{Catalog: d.Catalog, Schema: d.Schema}
	tn.Table = parts[n-1]
	if n >= 2 {
		tn.Schema = parts[n-2]
	}
	if n >= 3 {
		tn.Catalog = strings.Join(parts[:n-2], ".")
	}
	return tn, nil
}

// Upper returns a copy with all present components upper-cased (Snowflake).
func (t TableName) Upper() TableName {
	return TableName{
		Catalog: strings.ToUpper(t.Catalog),
		Schema:  strings.ToUpper(t.Schema),
		Table:   strings.ToUpper(t.Table),
	}
}

// QualifiedSchema returns the schema, prefixed with the catalog when present.
func (t TableName) QualifiedSchema() string {
	if t.Catalog != "" {
		return t.Catalog + "." + t.Schema
	}
	return t.Schema
}

// String joins the present components with dots.
func (t TableName) String() string {
	parts := make([]string, 0, 3)
	if t.Catalog != "" {
		parts = append(parts, t.Catalog)
	}
	if t.Schema != "" {
		parts = append(parts, t.Schema)
	}
	parts = append(parts, t.Table)
	return strings.Join(parts, ".")
}

// SchemaToCreate returns the schema identifier that should be ensured to exist
// for name, qualified by its catalog/database when name is three-part (so the
// schema is created in the named container rather than the connection default).
// transform (e.g. a quoting function) is applied per component; a nil transform
// is treated as identity. ok is false for single-component or oversized names.
func SchemaToCreate(name string, transform func(string) string) (string, bool) {
	if transform == nil {
		transform = func(s string) string { return s }
	}
	parts := Split(name)
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return "", false
		}
	}
	switch len(parts) {
	case 2:
		return transform(parts[0]), true
	case 3:
		return transform(parts[0]) + "." + transform(parts[1]), true
	default:
		return "", false
	}
}

// ContainerToCreate returns the catalog/database component (the first segment)
// that should be ensured to exist for a three-part name. ok is false for names
// with fewer than three components. transform is applied to the component; a
// nil transform is treated as identity.
func ContainerToCreate(name string, transform func(string) string) (string, bool) {
	if transform == nil {
		transform = func(s string) string { return s }
	}
	parts := Split(name)
	if len(parts) != 3 {
		return "", false
	}
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return "", false
		}
	}
	return transform(parts[0]), true
}
