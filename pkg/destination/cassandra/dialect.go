package cassandra

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/cassandrautil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type Dialect struct{}

func (d *Dialect) Name() string {
	return "Cassandra"
}

func (d *Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf(
		"ALTER TABLE %s ADD %s %s",
		quoteTableLiteral(table),
		cassandrautil.QuoteIdentifier(col.Name),
		MapDataTypeToCassandra(col),
	)
}

func (d *Dialect) AlterColumnTypeSQL(_, _ string, _ schema.Column) string {
	return ""
}

func (d *Dialect) SupportsAlterType() bool {
	return false
}

func (d *Dialect) TypeName(col schema.Column) string {
	return MapDataTypeToCassandra(col)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return cassandrautil.QuoteIdentifier(name)
}

func quoteTableLiteral(table string) string {
	parts := splitTableLiteral(table)
	if len(parts) == 2 {
		return cassandrautil.QuoteIdentifier(normalizeTableLiteralSegment(parts[0])) + "." + cassandrautil.QuoteIdentifier(normalizeTableLiteralSegment(parts[1]))
	}
	return cassandrautil.QuoteIdentifier(normalizeTableLiteralSegment(table))
}

func splitTableLiteral(table string) []string {
	for i, r := range table {
		if r == '.' {
			return []string{table[:i], table[i+1:]}
		}
	}
	return []string{table}
}

func normalizeTableLiteralSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if len(segment) >= 2 && strings.HasPrefix(segment, `"`) && strings.HasSuffix(segment, `"`) {
		return strings.ReplaceAll(segment[1:len(segment)-1], `""`, `"`)
	}
	return strings.ToLower(segment)
}
