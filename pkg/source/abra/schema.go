package abra

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type property struct {
	PropertyName string `json:"propertyName"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	InID         string `json:"inId"`
	IsSortable   string `json:"isSortable"`
	InExpensive  string `json:"inExpensive"`
	Digits       string `json:"digits"`
	Decimal      string `json:"decimal"`
}

type propertiesDoc struct {
	EvidenceName string     `json:"evidenceName"`
	DBName       string     `json:"dbName"`
	Property     []property `json:"property"`
}

func flag(s string) bool { return strings.EqualFold(s, "true") }

func (s *Source) fetchProperties(ctx context.Context, evidence string) (*propertiesDoc, error) {
	path := fmt.Sprintf("/c/%s/%s/properties.json", s.company, evidence)
	resp, err := s.client.R(ctx).Get(path)
	if err != nil {
		return nil, fmt.Errorf("abra: failed to fetch schema for evidence %q: %w", evidence, err)
	}
	if !resp.IsSuccess() {
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
			continue
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

var relationSuffixes = []string{"", "@ref", "@showAs"}

func dataTypeFor(prop property) (schema.DataType, int, int) {
	switch strings.ToLower(strings.TrimSpace(prop.Type)) {
	case "integer":
		return schema.TypeInt64, 0, 0
	case "logic":
		return schema.TypeBoolean, 0, 0
	case "date":
		return schema.TypeDate, 0, 0
	case "datetime":
		return schema.TypeTimestamp, 0, 0
	case "numeric":
		precision, precisionErr := strconv.Atoi(prop.Digits)
		scale, scaleErr := strconv.Atoi(prop.Decimal)
		if precisionErr == nil && scaleErr == nil && precision > 0 && precision <= 38 && scale >= 0 && scale <= precision {
			return schema.TypeDecimal, precision, scale
		}
		return schema.TypeFloat64, 0, 0
	default:
		return schema.TypeString, 0, 0
	}
}

func isRelational(flexiType string) bool {
	switch strings.ToLower(strings.TrimSpace(flexiType)) {
	case "relation", "select":
		return true
	}
	return false
}

type tablePlan struct {
	evidence       string
	columns        []schema.Column
	sourceToColumn map[string]string
	typeOf         map[string]schema.DataType
	hasLastUpdate  bool
	primaryKey     string
	expensiveCount int
}

func buildPlan(evidence string, doc *propertiesDoc, includeExpensive bool) (*tablePlan, error) {
	p := &tablePlan{
		evidence:       evidence,
		sourceToColumn: map[string]string{},
		typeOf:         map[string]schema.DataType{},
	}

	sourceByColumn := map[string]string{}
	addColumn := func(sourceKey string, dt schema.DataType, precision, scale int, isPK bool) error {
		dest := sanitizeColumn(sourceKey)
		if existing, dup := sourceByColumn[dest]; dup {
			if existing != sourceKey {
				return fmt.Errorf("abra: evidence %q has fields %q and %q that both map to column %q", evidence, existing, sourceKey, dest)
			}
			return nil
		}
		sourceByColumn[dest] = sourceKey
		p.sourceToColumn[sourceKey] = dest
		p.typeOf[dest] = dt
		p.columns = append(p.columns, schema.Column{
			Name:         dest,
			DataType:     dt,
			Precision:    precision,
			Scale:        scale,
			Nullable:     !isPK,
			IsPrimaryKey: isPK,
		})
		return nil
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
		dt, precision, scale := dataTypeFor(prop)
		isPK := name == "id"
		if isPK {
			p.primaryKey = "id"
			dt = schema.TypeInt64
			precision = 0
			scale = 0
		}
		if name == lastUpdateField {
			p.hasLastUpdate = true
		}
		if err := addColumn(name, dt, precision, scale, isPK); err != nil {
			return nil, err
		}

		if isRelational(prop.Type) {
			for _, suffix := range relationSuffixes {
				if suffix == "" {
					continue
				}
				if err := addColumn(name+suffix, schema.TypeString, 0, 0, false); err != nil {
					return nil, err
				}
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

	if err := addColumn(externalIDsField, schema.TypeString, 0, 0, false); err != nil {
		return nil, err
	}

	return p, nil
}
