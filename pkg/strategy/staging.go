package strategy

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

const DefaultStagingSchema = "_bruin_staging"

// maxStagingTableNameLen caps the unqualified staging table name. MySQL's
// identifier limit is 64; Postgres truncates silently at 63. Stay under both so
// identifiers don't collide via silent truncation and don't fail on MySQL.
const maxStagingTableNameLen = 60

func GenerateStagingTableName(targetTable, suffix, stagingDataset string) string {
	catalog, originSchema, tableName := splitCatalogSchemaTable(targetTable)

	stagingSchema := stagingDataset
	if stagingSchema == "" {
		stagingSchema = DefaultStagingSchema
	}

	embeddedName := tableName
	if originSchema != "" {
		embeddedName = fmt.Sprintf("%s__%s", originSchema, tableName)
	}

	return qualifyCatalog(catalog, buildStagingTableName(stagingSchema, embeddedName, suffix))
}

func managedStagingTableName(dest destination.Destination, targetTable, suffix, stagingDataset string) string {
	if provider, ok := dest.(destination.ManagedStagingPolicyProvider); ok {
		return GenerateReplaceStagingTableName(targetTable, suffix, stagingDataset, provider.ManagedStagingPolicy())
	}
	return GenerateStagingTableName(targetTable, suffix, stagingDataset)
}

func GenerateReplaceStagingTableName(targetTable, suffix, stagingDataset string, policy destination.ReplaceStagingPolicy) string {
	policy = normaliseReplaceStagingPolicy(policy)
	catalog, targetSchema, tableName := splitCatalogSchemaTable(targetTable)

	stagingSchema := stagingDataset
	if stagingSchema == "" {
		switch policy.DefaultPlacement {
		case destination.ReplaceStagingTargetSchema:
			stagingSchema = targetSchema
			if stagingSchema == "" {
				stagingSchema = policy.DefaultTargetSchema
			}
		default:
			stagingSchema = policy.DefaultManagedSchema
		}
	}
	if stagingSchema == "" {
		stagingSchema = policy.DefaultManagedSchema
	}

	embeddedName := tableName
	if targetSchema != "" && stagingSchema != targetSchema {
		embeddedName = fmt.Sprintf("%s__%s", targetSchema, tableName)
	}

	return qualifyCatalog(catalog, buildStagingTableName(stagingSchema, embeddedName, suffix))
}

func defaultReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: DefaultStagingSchema,
	}
}

func normaliseReplaceStagingPolicy(policy destination.ReplaceStagingPolicy) destination.ReplaceStagingPolicy {
	if policy.DefaultPlacement == "" {
		policy.DefaultPlacement = destination.ReplaceStagingManagedSchema
	}
	if policy.DefaultManagedSchema == "" {
		policy.DefaultManagedSchema = DefaultStagingSchema
	}
	return policy
}

// splitCatalogSchemaTable splits a possibly catalog-qualified table name into
// (catalog, schema, table). For a three-part name the leading component(s) are
// the catalog/database/project; for two parts the catalog is empty. Quoting is
// honored via tablename.Split.
func splitCatalogSchemaTable(table string) (catalog, schema, tableName string) {
	parts := tablename.Split(table)
	switch len(parts) {
	case 0:
		return "", "", table
	case 1:
		return "", "", parts[0]
	case 2:
		return "", parts[0], parts[1]
	default:
		return strings.Join(parts[:len(parts)-2], "."), parts[len(parts)-2], parts[len(parts)-1]
	}
}

// qualifyCatalog prepends the catalog component to a staging name when present,
// so a staging table for a three-part target lives in the same catalog.
func qualifyCatalog(catalog, name string) string {
	if catalog == "" {
		return name
	}
	return catalog + "." + name
}

func buildStagingTableName(stagingSchema, embeddedName, suffix string) string {
	nano := fmt.Sprintf("%d", time.Now().UnixNano())
	tail := fmt.Sprintf("_%s_%s", suffix, nano)
	// If the name would exceed the per-engine identifier limit, hash the
	// embedded portion so the suffix and unique timestamp still fit. We keep a
	// readable prefix plus an 8-char hash of the original embedded name.
	if len(embeddedName)+len(tail) > maxStagingTableNameLen {
		sum := sha1.Sum([]byte(embeddedName))
		shortHash := hex.EncodeToString(sum[:])[:8]
		keep := maxStagingTableNameLen - len(tail) - 1 - len(shortHash) // -1 for underscore
		if keep < 1 {
			keep = 1
		}
		if keep > len(embeddedName) {
			keep = len(embeddedName)
		}
		embeddedName = embeddedName[:keep] + "_" + shortHash
	}

	return fmt.Sprintf("%s.%s%s", stagingSchema, embeddedName, tail)
}

// GenerateNormalisedStagingTableName returns a transient table name in the
// TARGET table's own catalog/schema (not the staging schema).
func GenerateNormalisedStagingTableName(targetTable, stagingDataset string) string {
	staged := GenerateStagingTableName(targetTable, "staging_normalised", stagingDataset)
	stagedParts := tablename.Split(staged)
	bare := stagedParts[len(stagedParts)-1]
	// Re-qualify the transient table in the target's own catalog/schema.
	catalog, schema, _ := splitCatalogSchemaTable(targetTable)
	return tablename.TableName{Catalog: catalog, Schema: schema, Table: bare}.String()
}
