package postgres_cdc

import (
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestSingleTableSnapshotConfigPreservesSlotIdentity(t *testing.T) {
	const table, publication = "public.items", "items_pub"
	opts := source.ReadOptions{CDCSlotSuffix: "current", CDCLegacySlotSuffix: "abc123"}
	for _, explicit := range []string{"", "selected_slot"} {
		t.Run("slot_"+explicit, func(t *testing.T) {
			original := CDCConfig{Publication: publication, SlotName: explicit, DiscoverInterval: time.Second}
			cfg, legacy := singleTableSnapshotConfig(original, table, opts)
			require.Zero(t, cfg.DiscoverInterval, "a selected table must not discover other publication tables")
			require.Equal(t, time.Second, original.DiscoverInterval)
			if explicit != "" {
				require.Equal(t, explicit, cfg.SlotName)
				require.Empty(t, legacy)
				return
			}
			require.Equal(t, generateSlotName(table, publication, opts.CDCSlotSuffix), cfg.SlotName)
			require.NotEqual(t, generateMultiTableSlotName(publication, opts.CDCSlotSuffix), cfg.SlotName)
			require.Equal(t, []string{generateLegacySlotName(table, publication, opts.CDCLegacySlotSuffix)}, legacy)
		})
	}
	_, legacy := singleTableSnapshotConfig(CDCConfig{Publication: publication}, strings.Repeat("long_table", 20), opts)
	require.Empty(t, legacy, "a truncated legacy slot that lost its suffix cannot authorize resume")
}
