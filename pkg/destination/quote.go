package destination

import (
	"fmt"
	"strings"
)

func QuoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

// QuoteTableName quotes a two-level table name: "schema.table" becomes
// "schema"."table" and "table" becomes "table". It intentionally splits only on
// the first dot so a two-level destination receiving a multi-part name (e.g. a
// multi-table CDC destination "destschema.sourceschema.table") keeps everything
// after the first dot as a single quoted table name. Three-level destinations
// (DuckDB, etc.) do their own catalog-aware quoting.
func QuoteTableName(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return QuoteIdentifier(parts[0]) + "." + QuoteIdentifier(parts[1])
	}
	return QuoteIdentifier(table)
}
