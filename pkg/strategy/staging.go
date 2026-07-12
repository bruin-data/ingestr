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

const (
	encodedStagingIdentifierPrefix       = "ingestr_hex_"
	legacyEncodedStagingIdentifierPrefix = "_ingestr_hex_"
)

func GenerateStagingTableName(targetTable, suffix, stagingDataset string) string {
	catalog, originSchema, tableName := splitCatalogSchemaTable(targetTable)
	catalogRef, _, _ := splitCatalogSchemaTableRaw(targetTable)

	stagingSchema := stagingDataset
	if stagingSchema == "" {
		stagingSchema = DefaultStagingSchema
	}

	embeddedParts := []string{tableName}
	if originSchema != "" {
		embeddedParts = []string{originSchema, tableName}
	}
	embeddedName := syntheticStagingIdentifier(embeddedParts...)

	if catalogRef != "" {
		catalog = catalogRef
	}
	return qualifyCatalog(catalog, buildStagingTableName(stagingSchema, embeddedName, suffix))
}

func managedStagingTableName(dest destination.Destination, targetTable, suffix, stagingDataset string) string {
	if provider, ok := dest.(destination.ManagedStagingPolicyProvider); ok {
		return GenerateReplaceStagingTableName(targetTable, suffix, stagingDataset, provider.ManagedStagingPolicy())
	}
	return GenerateStagingTableName(targetTable, suffix, stagingDataset)
}

func managedCDCStateTableName(dest destination.Destination, stateTable, _ string) string {
	policy := defaultReplaceStagingPolicy()
	if provider, ok := dest.(destination.ManagedStagingPolicyProvider); ok {
		policy = normaliseReplaceStagingPolicy(provider.ManagedStagingPolicy())
	}

	catalog := ""
	if provider, ok := dest.(destination.ManagedCDCStateCatalogProvider); ok {
		catalog = provider.ManagedCDCStateCatalog()
	}
	stateSchema := ""
	switch policy.DefaultPlacement {
	case destination.ReplaceStagingTargetSchema:
		stateSchema = policy.DefaultTargetSchema
	default:
		stateSchema = policy.DefaultManagedSchema
	}
	if stateSchema == "" {
		stateSchema = DefaultStagingSchema
	}

	return qualifyCatalog(catalog, fmt.Sprintf("%s.%s", stateSchema, stateTable))
}

func GenerateReplaceStagingTableName(targetTable, suffix, stagingDataset string, policy destination.ReplaceStagingPolicy) string {
	policy = normaliseReplaceStagingPolicy(policy)
	catalog, targetSchema, tableName := splitCatalogSchemaTable(targetTable)
	catalogRef, targetSchemaRef, _ := splitCatalogSchemaTableRaw(targetTable)
	if catalogRef != "" {
		catalog = catalogRef
	}

	stagingSchema := stagingDataset
	if stagingSchema == "" {
		switch policy.DefaultPlacement {
		case destination.ReplaceStagingTargetSchema:
			stagingSchema = targetSchemaRef
			if stagingSchema == "" {
				stagingSchema = targetSchema
			}
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

	embeddedParts := []string{tableName}
	targetSchemaPlacement := targetSchemaRef
	if targetSchemaPlacement == "" {
		targetSchemaPlacement = targetSchema
	}
	if targetSchema != "" && stagingSchema != targetSchemaPlacement {
		embeddedParts = []string{targetSchema, tableName}
	}
	embeddedName := syntheticStagingIdentifier(embeddedParts...)

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
	return catalogSchemaTable(parts, table)
}

func splitCatalogSchemaTableRaw(table string) (catalog, schema, tableName string) {
	parts := tablename.SplitRaw(table)
	return catalogSchemaTable(parts, table)
}

func catalogSchemaTable(parts []string, fallback string) (catalog, schema, tableName string) {
	switch len(parts) {
	case 0:
		return "", "", fallback
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

func syntheticStagingIdentifier(parts ...string) string {
	legacy := strings.Join(parts, "__")
	if isUnambiguousLegacyStagingIdentifier(legacy, parts) {
		return legacy
	}

	var encoded strings.Builder
	encoded.WriteString(encodedStagingIdentifierPrefix)
	for i, part := range parts {
		if i > 0 {
			encoded.WriteByte('_')
		}
		encoded.WriteString(hex.EncodeToString([]byte(part)))
	}
	return encoded.String()
}

func isUnambiguousLegacyStagingIdentifier(candidate string, parts []string) bool {
	if strings.HasPrefix(candidate, encodedStagingIdentifierPrefix) || strings.HasPrefix(candidate, legacyEncodedStagingIdentifierPrefix) {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Contains(part, "__") {
			return false
		}
		for i := 0; i < len(part); i++ {
			ch := part[i]
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
				continue
			}
			return false
		}
	}
	return true
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
	catalog, schema, _ := splitCatalogSchemaTableRaw(targetTable)
	return tablename.TableName{Catalog: catalog, Schema: schema, Table: bare}.String()
}
