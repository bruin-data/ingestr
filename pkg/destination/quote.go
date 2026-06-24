package destination

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/tablename"
)

func QuoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

// QuoteTableName quotes every dot-separated component of a possibly-qualified
// table name: "schema.table" becomes "schema"."table" and
// "catalog.schema.table" becomes "catalog"."schema"."table".
func QuoteTableName(table string) string {
	parts := tablename.Split(table)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = QuoteIdentifier(p)
	}
	return strings.Join(quoted, ".")
}
