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

// "schema.table" becomes "schema"."table", "table" becomes "table".
func QuoteTableName(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return QuoteIdentifier(parts[0]) + "." + QuoteIdentifier(parts[1])
	}
	return QuoteIdentifier(table)
}
