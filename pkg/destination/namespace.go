package destination

import "github.com/bruin-data/ingestr/pkg/tablename"

// namespaceRequiringSchemes lists destinations that reject bare, unqualified
// table names. Multi-table CDC synthesizes names for these destinations from
// dest_schema, so a run against one of them cannot proceed without a namespace.
var namespaceRequiringSchemes = map[string]tablename.Capability{
	"bigquery":   tablename.BigQuery,
	"databricks": tablename.Databricks,
}

// RequiresNamespace reports whether scheme rejects unqualified table names,
// returning the capability describing its accepted name format.
func RequiresNamespace(scheme string) (tablename.Capability, bool) {
	c, ok := namespaceRequiringSchemes[scheme]
	return c, ok
}
