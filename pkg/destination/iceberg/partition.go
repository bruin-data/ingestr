package iceberg

import (
	"fmt"
	"regexp"
	"strings"

	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/schema"
)

var partitionNameSanitizer = regexp.MustCompile(`[^A-Za-z0-9_]+`)

type partitionTerm struct {
	source    string
	name      string
	transform iceberggo.Transform
}

func validatePreparedLayoutColumns(tableSchema *schema.TableSchema, partitionBy string, clusterBy []string) error {
	available := make(map[string]struct{}, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		available[col.Name] = struct{}{}
	}
	terms, err := parsePartitionExpression(partitionBy)
	if err != nil {
		return err
	}
	for _, term := range terms {
		if _, ok := available[term.source]; !ok {
			return fmt.Errorf("iceberg: partition column %q not found", term.source)
		}
	}
	for _, column := range clusterBy {
		if _, ok := available[column]; !ok {
			return fmt.Errorf("iceberg: cluster column %q not found", column)
		}
	}
	return nil
}

func preparedLayoutColumnsExist(tableSchema *schema.TableSchema, partitionBy string, clusterBy []string) bool {
	return validatePreparedLayoutColumns(tableSchema, partitionBy, clusterBy) == nil
}

func layoutColumnsExist(iceSchema *iceberggo.Schema, partitionBy string, clusterBy []string) bool {
	terms, err := parsePartitionExpression(partitionBy)
	if err != nil {
		return false
	}
	for _, term := range terms {
		if _, ok := iceSchema.FindFieldByName(term.source); !ok {
			return false
		}
	}
	for _, column := range clusterBy {
		if _, ok := iceSchema.FindFieldByName(column); !ok {
			return false
		}
	}
	return true
}

func parsePartitionExpression(expression string) ([]partitionTerm, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, nil
	}

	parts := strings.Split(expression, ",")
	terms := make([]partitionTerm, 0, len(parts))
	seenNames := make(map[string]struct{}, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, fmt.Errorf("iceberg: invalid partition specification %q: empty field", expression)
		}

		transformName := "identity"
		source := raw
		if open := strings.IndexByte(raw, '('); open >= 0 {
			if !strings.HasSuffix(raw, ")") || open == 0 {
				return nil, fmt.Errorf("iceberg: invalid partition field %q", raw)
			}
			transformName = strings.TrimSpace(raw[:open])
			source = strings.TrimSpace(raw[open+1 : len(raw)-1])
			if strings.Contains(source, "(") || strings.Contains(source, ")") {
				return nil, fmt.Errorf("iceberg: invalid partition field %q", raw)
			}
		}
		if source == "" {
			return nil, fmt.Errorf("iceberg: invalid partition field %q: source column is empty", raw)
		}

		transform, err := iceberggo.ParseTransform(strings.ToLower(transformName))
		if err != nil {
			return nil, fmt.Errorf("iceberg: invalid partition transform in %q: %w", raw, err)
		}
		name := source
		if _, identity := transform.(iceberggo.IdentityTransform); !identity {
			suffix := partitionNameSanitizer.ReplaceAllString(strings.ToLower(transform.String()), "_")
			suffix = strings.Trim(suffix, "_")
			name = source + "_" + suffix
		}
		if _, duplicate := seenNames[name]; duplicate {
			return nil, fmt.Errorf("iceberg: partition field name %q is duplicated", name)
		}
		seenNames[name] = struct{}{}
		terms = append(terms, partitionTerm{source: source, name: name, transform: transform})
	}
	return terms, nil
}

func buildPartitionSpec(iceSchema *iceberggo.Schema, expression string) (iceberggo.PartitionSpec, error) {
	terms, err := parsePartitionExpression(expression)
	if err != nil {
		return iceberggo.PartitionSpec{}, err
	}
	opts := make([]iceberggo.PartitionOption, 0, len(terms))
	for _, term := range terms {
		opts = append(opts, iceberggo.AddPartitionFieldByName(term.source, term.name, term.transform, iceSchema, nil))
	}
	spec, err := iceberggo.NewPartitionSpecOpts(opts...)
	if err != nil {
		return iceberggo.PartitionSpec{}, fmt.Errorf("iceberg: invalid partition specification %q: %w", expression, err)
	}
	return spec, nil
}

func partitionExpressionMatches(tbl *icebergtable.Table, expression string) bool {
	terms, err := parsePartitionExpression(expression)
	if err != nil {
		return false
	}
	spec := tbl.Metadata().PartitionSpec()
	i := 0
	for _, field := range spec.Fields() {
		if i >= len(terms) {
			return false
		}
		source, ok := tbl.Schema().FindColumnName(field.SourceID())
		if !ok || source != terms[i].source || field.Name != terms[i].name || !field.Transform.Equals(terms[i].transform) {
			return false
		}
		i++
	}
	return i == len(terms)
}
