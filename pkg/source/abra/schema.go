package abra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// ── Flexi's schema endpoint ──────────────────────────────────────────────────
//
// GET /c/<company>/<evidence>/properties.json returns:
//
//	{"properties":{"@version":"1.0","dbName":"aFaktVyd",
//	  "property":[{"propertyName":"id","type":"integer","inId":"true",...}, ...]}}
//
// ⚠️ FLEXI WRAPS ITS THREE ENDPOINTS THREE DIFFERENT WAYS, and the wrapper name
// matches neither the URL nor the inner key. Measured against the live API on
// 2026-08-14, after two probe runs died assuming otherwise:
//
//	/<evidence>.json        {"winstrom":  {"<evidence-path>": [...]}}
//	/evidence-list.json     {"evidences": {"evidence":        [...]}}
//	/<ev>/properties.json   {"properties":{"property":        [...]}}
//
// Beware second-hand descriptions of these shapes: one such description calls
// evidence-list and properties "plain", but that describes the payload AFTER its own
// render step, not the wire format. findArray therefore looks for the inner key at
// the top level OR one level down, rather than hard-coding any single wrapper.
//
// Every flag arrives as the STRING "true"/"false", never a JSON boolean, which is
// why flag() exists instead of a bool field.

type property struct {
	PropertyName string `json:"propertyName"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	InID         string `json:"inId"`
	IsSortable   string `json:"isSortable"`
	InExpensive  string `json:"inExpensive"`
}

type propertiesDoc struct {
	EvidenceName string     `json:"evidenceName"`
	DBName       string     `json:"dbName"`
	Property     []property `json:"property"`
}

func flag(s string) bool { return strings.EqualFold(s, "true") }

// fetchProperties reads and decodes one evidence's schema document.
func (s *Source) fetchProperties(ctx context.Context, evidence string) (*propertiesDoc, error) {
	path := fmt.Sprintf("/c/%s/%s/properties.json", s.company, evidence)
	resp, err := s.client.R(ctx).Get(path)
	if err != nil {
		return nil, fmt.Errorf("abra: failed to fetch schema for evidence %q: %w", evidence, err)
	}
	if !resp.IsSuccess() {
		// 404 here is the normal signal for "no such evidence", and it is worth
		// distinguishing because a typo'd evidence name is the most likely error a
		// human makes when adding a table to a CronJob.
		if resp.StatusCode() == 404 {
			return nil, fmt.Errorf("abra: evidence %q does not exist in company %q", evidence, s.company)
		}
		return nil, fmt.Errorf("abra: schema request for %q returned HTTP %d: %s",
			evidence, resp.StatusCode(), truncate(resp.String(), 300))
	}
	raw, err := findArray([]byte(resp.String()), "property")
	if err != nil {
		return nil, fmt.Errorf("abra: failed to locate the property list for %q: %w", evidence, err)
	}
	var doc propertiesDoc
	if err := json.Unmarshal(raw, &doc.Property); err != nil {
		return nil, fmt.Errorf("abra: failed to parse schema for %q: %w", evidence, err)
	}
	if len(doc.Property) == 0 {
		return nil, fmt.Errorf("abra: evidence %q reported no properties", evidence)
	}
	return &doc, nil
}

// findArray locates a named array inside a Flexi response, checking the top level
// first and then one level down through any object-valued field.
//
// This exists because of the three-different-wrappers problem documented above.
// One level of nesting is all Flexi uses, and bounding the search there keeps the
// lookup unambiguous — an unbounded walk could match a same-named key nested
// somewhere unrelated.
func findArray(body []byte, key string) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("response was not a JSON object: %w", err)
	}
	if v, ok := top[key]; ok {
		return v, nil
	}
	for _, v := range top {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(v, &inner); err != nil {
			continue // not an object; skip
		}
		if got, ok := inner[key]; ok {
			return got, nil
		}
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	return nil, fmt.Errorf("no %q array at the top level or one level below; keys were %v", key, sortedKeys(toSet(keys)))
}

func toSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

// ── Column naming ────────────────────────────────────────────────────────────

// sanitizeColumn makes a Flexi property name safe to use as a column name.
//
// ⚠️ THIS IS THE ONE PLACE THIS SOURCE DEVIATES FROM "raw is the vendor's shape,
// verbatim". Flexi emits column names that most destinations can only address
// with quoting, and that many downstream tools cannot reference at all:
//
//	mena@ref      -> mena_ref
//	mena@showAs   -> mena_showAs
//	external-ids  -> external_ids
//
// The substitution is deliberately MINIMAL — every character outside
// [A-Za-z0-9_] becomes '_' and nothing else changes. Casing is preserved
// (`showAs` does NOT become `show_as`) precisely so the name still reads as
// Flexi's, and so the mapping stays mechanical and reversible rather than
// becoming a judgement call that drifts per table.
func sanitizeColumn(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// relationSuffixes are the extra columns Flexi materialises alongside a
// `relation` or `select` property.
//
// A relation returns three columns and a select returns two:
//
//	mena             = "code:CHF"                                  (the value)
//	mena@ref         = "/c/acme_s_r_o__kopie/mena/6.json"         (link, relation only)
//	mena@showAs      = "CHF: Švýcarský frank"                       (human label)
//
// We declare all three for BOTH kinds. An extra always-NULL column is harmless;
// a MISSING column silently loses data, which is not. Do not "optimise" this by
// branching on the type — the two kinds are not reliably distinguishable from
// properties.json alone, and the failure mode is asymmetric.
var relationSuffixes = []string{"", "@ref", "@showAs"}

// ── Type mapping ─────────────────────────────────────────────────────────────

// dataTypeFor maps a Flexi property type onto an ingestr/Arrow type.
//
// ⚠️ `numeric` DELIBERATELY BECOMES A STRING, NOT A DECIMAL OR FLOAT.
//
// Three reasons, in order of how much they hurt:
//
//  1. Float64 silently loses cents. This is an accounting ledger; a rounding error
//     here is not a rounding error, it is a wrong number in a financial report.
//  2. Flexi returns numerics as JSON numbers of varying scale. Carrying the exact
//     decimal TEXT and casting once, explicitly, downstream is both exact and
//     auditable — the scale becomes a documented modelling decision rather than an
//     accident of the first row the type-inferrer happened to see.
//
// Dates and datetimes are parsed in Go (see coerce) rather than handed to the
// Arrow builder as strings, because that builder AppendNull()s anything it cannot
// parse — a silent, per-row data loss that no error surface would report.
func dataTypeFor(flexiType string) schema.DataType {
	switch strings.ToLower(strings.TrimSpace(flexiType)) {
	case "integer":
		return schema.TypeInt64
	case "logic":
		return schema.TypeBoolean
	case "date":
		return schema.TypeDate
	case "datetime":
		return schema.TypeTimestamp
	case "numeric":
		// See the block comment above. This is not an oversight.
		return schema.TypeString
	default:
		// string, select, relation, and anything Flexi adds later.
		return schema.TypeString
	}
}

// isRelational reports whether a property materialises the @ref / @showAs
// companions.
func isRelational(flexiType string) bool {
	switch strings.ToLower(strings.TrimSpace(flexiType)) {
	case "relation", "select":
		return true
	}
	return false
}

// ── Table plan ───────────────────────────────────────────────────────────────

// tablePlan is everything the reader needs about one evidence, derived once from
// properties.json so that neither the schema nor the projection can drift
// mid-run.
type tablePlan struct {
	evidence string
	columns  []schema.Column
	// sourceToColumn maps the key as it arrives in Flexi's JSON to the sanitized
	// destination column name.
	sourceToColumn map[string]string
	// typeOf is keyed by DESTINATION column name.
	typeOf         map[string]schema.DataType
	hasLastUpdate  bool
	primaryKey     string
	expensiveCount int
}

// buildPlan turns a properties document into a table plan.
//
// ⚠️ FAIL-CLOSED ON A MISSING PRIMARY KEY. Some Flexi "evidences" are derived
// views rather than tables — `ucetni-denik` (the accounting journal) is the one
// we hit first: it reports 47,189 rows, every one of them with id = -1 and an
// EMPTY lastUpdate. Such an evidence cannot be merged (no key to dedup on) and
// cannot be incrementally read (no cursor). Loading it under `merge` would append
// the full result set on every run, forever, and `count() FINAL` would not show
// it. The only safe alternative would be `replace`, which on a Replicated target
// desynchronises replicas. So we refuse, loudly, at plan time.
func buildPlan(evidence string, doc *propertiesDoc, includeExpensive bool) (*tablePlan, error) {
	p := &tablePlan{
		evidence:       evidence,
		sourceToColumn: map[string]string{},
		typeOf:         map[string]schema.DataType{},
	}

	seen := map[string]struct{}{}
	addColumn := func(sourceKey string, dt schema.DataType, isPK bool) {
		dest := sanitizeColumn(sourceKey)
		if _, dup := seen[dest]; dup {
			return
		}
		seen[dest] = struct{}{}
		p.sourceToColumn[sourceKey] = dest
		p.typeOf[dest] = dt
		p.columns = append(p.columns, schema.Column{
			Name:         dest,
			DataType:     dt,
			Nullable:     !isPK,
			IsPrimaryKey: isPK,
		})
	}

	for _, prop := range doc.Property {
		name := prop.PropertyName
		if name == "" {
			continue
		}
		if flag(prop.InExpensive) {
			p.expensiveCount++
			if !includeExpensive {
				continue
			}
		}
		dt := dataTypeFor(prop.Type)
		isPK := name == "id"
		if isPK {
			p.primaryKey = "id"
			// Flexi declares id as `integer`; keep it that way and never nullable.
			dt = schema.TypeInt64
		}
		if name == lastUpdateField {
			p.hasLastUpdate = true
		}
		addColumn(name, dt, isPK)

		if isRelational(prop.Type) {
			for _, suffix := range relationSuffixes {
				if suffix == "" {
					continue
				}
				addColumn(name+suffix, schema.TypeString, false)
			}
		}
	}

	if p.primaryKey == "" {
		return nil, fmt.Errorf(
			"abra: evidence %q has no `id` property and cannot be ingested — it is a derived view, "+
				"not a table (the accounting journal `ucetni-denik` behaves this way). Ingesting it "+
				"under --incremental-strategy merge would append every row on every run with nothing "+
				"to deduplicate on", evidence)
	}

	// Flexi attaches this to most evidences without declaring it in properties.json.
	// Declared explicitly so it lands in the table instead of being logged as drift.
	addColumn(externalIDsField, schema.TypeString, false)

	// ⚠️ DO NOT ADD A LOAD-TIMESTAMP COLUMN HERE. ingestr's strategy layer adds (or
	// replaces) `_ingestr_loaded_at` itself with one timestamp for the whole job,
	// and the promote script uses exactly that column as the
	// ReplicatedReplacingMergeTree version. Declaring our own would either be
	// clobbered or, worse, collide with the version column the promote depends on.

	return p, nil
}
