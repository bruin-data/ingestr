package naming

import (
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// Ingestr metadata column prefixes
const (
	IngestrColumnPrefix = "_dlt_"
)

// Common ingestr metadata columns
var IngestrMetadataColumns = []string{
	"_dlt_load_id",
	"_dlt_id",
	"_dlt_parent_id",
	"_dlt_list_idx",
	"_dlt_root_id",
}

// IsIngestrColumn returns true if the column name is an ingestr metadata column.
func IsIngestrColumn(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), IngestrColumnPrefix)
}

// HasIngestrColumns returns true if the schema contains any ingestr metadata columns.
// This indicates the table was likely created by ingestr.
func HasIngestrColumns(s *schema.TableSchema) bool {
	if s == nil {
		return false
	}
	for _, col := range s.Columns {
		if IsIngestrColumn(col.Name) {
			return true
		}
	}
	return false
}

// GetIngestrColumns returns all ingestr metadata columns from the schema.
func GetIngestrColumns(s *schema.TableSchema) []string {
	if s == nil {
		return nil
	}
	var cols []string
	for _, col := range s.Columns {
		if IsIngestrColumn(col.Name) {
			cols = append(cols, col.Name)
		}
	}
	return cols
}

// DetectConvention attempts to detect which naming convention was used for a destination table
// by comparing source column names with destination column names.
// Returns SnakeCase if destination columns appear to be snake_case normalized versions of source columns.
// Returns Direct if columns match exactly or no clear pattern is detected.
// Ingestr metadata columns are ignored during detection.
func DetectConvention(sourceSchema, destSchema *schema.TableSchema) Convention {
	if sourceSchema == nil || destSchema == nil {
		return SnakeCase
	}

	sourceNames := make(map[string]bool)
	for _, col := range sourceSchema.Columns {
		sourceNames[col.Name] = true
	}

	// Build two dest name maps, excluding ingestr columns:
	// - exact: for direct match (preserves original case)
	// - lowered: for snake_case match
	destNamesExact := make(map[string]bool)
	destNamesNormalized := make(map[string]bool)
	for _, col := range destSchema.Columns {
		if !IsIngestrColumn(col.Name) {
			destNamesExact[col.Name] = true
			destNamesNormalized[normalizeForDetection(col.Name)] = true
		}
	}

	// Check if destination columns match snake_case transformation of source columns
	snakeCaseMatches := 0
	directMatches := 0

	snakeConv := &snakeCaseNaming{}

	for _, col := range sourceSchema.Columns {
		sourceName := col.Name
		snakeName := snakeConv.Normalize(sourceName)

		// Columns already in snake_case form (e.g. "id", "user_name") are ambiguous —
		// both conventions produce the same name, so they provide no detection signal.
		if snakeName == sourceName {
			continue
		}

		// Direct match: exact case comparison (source name exists as-is in destination)
		if destNamesExact[sourceName] {
			directMatches++
		}
		// Snake case match: case-insensitive comparison (source name transformed to snake_case exists in destination)
		if destNamesNormalized[normalizeForDetection(snakeName)] {
			snakeCaseMatches++
		}
	}

	// Only multi-word columns are considered: if direct matches clearly dominate, use direct.
	if directMatches > 0 && directMatches > snakeCaseMatches {
		return Direct
	}

	return SnakeCase
}

// normalizeForDetection lowercases the column name for case-insensitive comparison
func normalizeForDetection(name string) string {
	return strings.ToLower(name)
}

// BuildColumnMapping computes how source columns must be transformed to honor
// the naming convention.
//
// Returns:
//   - renames: source column name → target name, for 1-to-1 renames.
//   - merges:  target name → ordered source columns (in source order) that all
//     normalize to the same target.
func BuildColumnMapping(sourceSchema *schema.TableSchema, convention NamingConvention) (renames map[string]string, merges map[string][]string) {
	renames = make(map[string]string)
	merges = make(map[string][]string)
	if sourceSchema == nil {
		return renames, merges
	}

	groups := make(map[string][]string)
	for _, col := range sourceSchema.Columns {
		target := convention.Normalize(col.Name)
		groups[target] = append(groups[target], col.Name)
	}

	for target, names := range groups {
		if len(names) == 1 {
			if names[0] != target {
				renames[names[0]] = target
			}
			continue
		}
		sources := make([]string, len(names))
		copy(sources, names)
		merges[target] = sources
	}

	return renames, merges
}
