package naming

import (
	"strings"
	"testing"
)

// TestPartitionColumnNamingMatrix documents and locks the contract the pipeline
// relies on: partition_by/cluster_by are normalized with the SAME convention as
// the destination columns, so they resolve to the same destination column name.
//
// destColumn = Normalize(sourceColumn)   (what applyColumnMapping produces)
// partition  = Normalize(partitionBy)    (what the pipeline passes to the dest)
// BigQuery resolves the partition field against columns case-insensitively.
func TestPartitionColumnNamingMatrix(t *testing.T) {
	tests := []struct {
		name          string
		partitionBy   string
		sourceColumn  string
		convention    Convention
		wantPartition string // normalized value handed to the destination
		wantMatch     bool   // whether it matches the destination column (success)
	}{
		{"camel/camel/snake", "updatedAt", "updatedAt", SnakeCase, "updated_at", true},
		{"camel/camel/direct", "updatedAt", "updatedAt", Direct, "updatedAt", true},
		{"snake/snake/snake", "updated_at", "updated_at", SnakeCase, "updated_at", true},
		{"snake/snake/direct", "updated_at", "updated_at", Direct, "updated_at", true},
		{"camel/snake/snake", "updatedAt", "updated_at", SnakeCase, "updated_at", true},
		{"camel/snake/direct", "updatedAt", "updated_at", Direct, "updatedAt", false},
		{"snake/camel/snake", "updated_at", "updatedAt", SnakeCase, "updated_at", true},
		{"snake/camel/direct", "updated_at", "updatedAt", Direct, "updated_at", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := Get(tt.convention)
			gotPartition := conv.Normalize(tt.partitionBy)
			if gotPartition != tt.wantPartition {
				t.Fatalf("Normalize(%q) under %s = %q, want %q",
					tt.partitionBy, tt.convention, gotPartition, tt.wantPartition)
			}
			destColumn := conv.Normalize(tt.sourceColumn)
			gotMatch := strings.EqualFold(gotPartition, destColumn)
			if gotMatch != tt.wantMatch {
				t.Fatalf("match(partition=%q, destColumn=%q) = %v, want %v",
					gotPartition, destColumn, gotMatch, tt.wantMatch)
			}
		})
	}
}
