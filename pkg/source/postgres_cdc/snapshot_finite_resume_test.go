package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestInitialSnapshotResumeWithoutCompletedTable(t *testing.T) {
	tables := []source.SourceTableInfo{{Name: "public.first"}, {Name: "public.second"}, {Name: "public.new"}}
	reader := NewMultiTableCDCReader(nil, tables, CDCConfig{}, nil, "")
	reader.snapshotResumes = map[string]source.CDCSnapshotProgress{
		"public.first": {
			Boundary: "0/30", Attempt: "first-attempt",
			Ranges: []source.CDCSnapshotRangeProgress{{ID: "0", Key: []string{"900"}}},
		},
		"public.second": {
			Boundary: "0/20", Attempt: "second-attempt",
			Ranges: []source.CDCSnapshotRangeProgress{{ID: "0", Complete: true}},
		},
	}
	needsSnapshot, start := reader.determineResumeStrategy()
	require.False(t, needsSnapshot)
	require.Equal(t, pglogrepl.LSN(0x20), start, "repair must start at the oldest interrupted snapshot")
	require.Empty(t, reader.processedLSNs, "range completion must not certify a complete table")
	require.Equal(t, tables, reader.tablesWithoutResumeState())
	reader.setSnapshotBoundary("public.first", 0x30)
	require.False(t, reader.ShouldFilterChange("public.first", 0x30))
	require.False(t, reader.ShouldFilterChange("public.first", 0x40), "changes behind the saved cursor must repair copied rows")
}

func TestInitialSnapshotResumeInvalidBoundaryRequiresReplacement(t *testing.T) {
	for _, boundary := range []string{"", "invalid", "0/0"} {
		t.Run(boundary, func(t *testing.T) {
			reader := NewMultiTableCDCReader(nil, nil, CDCConfig{}, nil, "")
			reader.snapshotResumes = map[string]source.CDCSnapshotProgress{
				"public.items": {Boundary: boundary, Ranges: []source.CDCSnapshotRangeProgress{{ID: "0", Key: []string{"1"}}}},
			}
			needsSnapshot, start := reader.determineResumeStrategy()
			require.True(t, needsSnapshot)
			require.Zero(t, start)
			require.Empty(t, reader.processedLSNs)
		})
	}
}
