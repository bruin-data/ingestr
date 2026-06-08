package strategy

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const DefaultStagingSchema = "_bruin_staging"

// maxStagingTableNameLen caps the unqualified staging table name. MySQL's
// identifier limit is 64; Postgres truncates silently at 63. Stay under both so
// identifiers don't collide via silent truncation and don't fail on MySQL.
const maxStagingTableNameLen = 60

func GenerateStagingTableName(targetTable, suffix, stagingDataset string) string {
	parts := strings.SplitN(targetTable, ".", 2)
	originSchema := ""
	tableName := targetTable

	if len(parts) == 2 {
		originSchema = parts[0]
		tableName = parts[1]
	}

	stagingSchema := stagingDataset
	if stagingSchema == "" {
		stagingSchema = DefaultStagingSchema
	}

	embeddedName := tableName
	if originSchema != "" {
		embeddedName = fmt.Sprintf("%s__%s", originSchema, tableName)
	}

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
// TARGET table's own schema (not the staging schema).
func GenerateNormalisedStagingTableName(targetTable, stagingDataset string) string {
	staged := GenerateStagingTableName(targetTable, "staging_normalised", stagingDataset)
	name := staged
	if i := strings.Index(staged, "."); i >= 0 {
		name = staged[i+1:]
	}
	if parts := strings.SplitN(targetTable, ".", 2); len(parts) == 2 {
		return parts[0] + "." + name
	}
	return name
}
