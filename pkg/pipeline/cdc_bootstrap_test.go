package pipeline

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

type generatedBaselineDestination struct {
	mockManagedCDCStateDestination
	generated []string
}

func (d *generatedBaselineDestination) GeneratedColumns(context.Context, string) ([]string, error) {
	return d.generated, nil
}

type noSnapshotBaselineSource struct {
	source.Source
	positions map[string]string
}

func (s *noSnapshotBaselineSource) ConfigureNoSnapshot(_ context.Context, positions map[string]string) error {
	s.positions = positions
	return nil
}

func TestPrepareCDCNoSnapshotGeneratedColumns(t *testing.T) {
	for _, generated := range []bool{true, false} {
		name := "missing ordinary column"
		if generated {
			name = "destination generated column"
		}
		t.Run(name, func(t *testing.T) {
			dest := &generatedBaselineDestination{}
			dest.tableSchema = &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "a", DataType: schema.TypeInt64},
			}, PrimaryKeys: []string{"id"}}
			if generated {
				dest.generated = []string{"total"}
			}
			src := &noSnapshotBaselineSource{}
			p := &Pipeline{
				config: &config.IngestConfig{CDCSnapshotMode: "never", CDCStartLSN: "0/100"},
				src:    src,
				dest:   dest,
			}
			manager, err := strategy.NewCDCStateManager(dest, "connector", "raw.items", "")
			require.NoError(t, err)
			sourceSchema := &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "a", DataType: schema.TypeInt64},
				{Name: "total", DataType: schema.TypeInt64},
			}, PrimaryKeys: []string{"id"}}
			positions := make(map[string]string)
			bootstrap, err := p.prepareCDCNoSnapshot(t.Context(), manager,
				[]source.SourceTableInfo{{Name: "public.items", Schema: sourceSchema, PrimaryKeys: []string{"id"}}},
				map[string]string{"public.items": "raw.items"}, positions, config.StrategyMerge)
			if !generated {
				require.ErrorContains(t, err, "column total differs from the source")
				require.False(t, bootstrap)
				require.Nil(t, src.positions)
				return
			}
			require.NoError(t, err)
			require.True(t, bootstrap)
			require.Equal(t, map[string]string{"public.items": "0/100"}, src.positions)
			require.Len(t, sourceSchema.Columns, 3)
		})
	}
}

func TestPrepareCDCNoSnapshotRepeatedBoundaryResumesCheckpoints(t *testing.T) {
	for _, tableNames := range [][]string{{"public.orders"}, {"public.orders", "public.items"}} {
		t.Run(tableNames[len(tableNames)-1], func(t *testing.T) {
			dest := &mockLegacyBootstrapDestination{}
			dest.tableSchema = &schema.TableSchema{
				Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"id"},
			}
			newManager := func() *strategy.CDCStateManager {
				manager, err := strategy.NewCDCStateManager(dest, "connector", "raw.orders", "")
				require.NoError(t, err)
				for _, table := range tableNames {
					require.NoError(t, manager.RegisterTableState(t.Context(), table, "raw."+table, "source:"+table, "schema:"+table))
				}
				return manager
			}
			manager := newManager()
			_, err := manager.ResolveSnapshotMode(t.Context(), "never")
			require.NoError(t, err)
			baseline := make(map[string]string)
			destinations := make(map[string]string)
			var tables []source.SourceTableInfo
			for _, table := range tableNames {
				baseline[table] = "0/100"
				destinations[table] = "raw." + table
				require.NoError(t, manager.BindDestinationIncarnation(t.Context(), table, destinations[table]))
				tables = append(tables, source.SourceTableInfo{Name: table, Schema: dest.tableSchema, PrimaryKeys: []string{"id"}})
			}
			require.NoError(t, manager.BeginBaseline(t.Context(), baseline))
			require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{Position: "0/200"}))

			for _, boundary := range []string{"0/100", "00000000/00000100", ""} {
				t.Run("boundary="+boundary, func(t *testing.T) {
					restarted := newManager()
					positions := make(map[string]string)
					for _, table := range tableNames {
						positions[table], err = restarted.ResumePositionForKeyedMerge(t.Context(), table)
						require.NoError(t, err)
						require.Equal(t, "0/200", positions[table])
					}
					src := &noSnapshotBaselineSource{}
					p := &Pipeline{config: &config.IngestConfig{CDCSnapshotMode: "never", CDCStartLSN: boundary}, src: src, dest: dest}
					stateCount := len(dest.entries)
					bootstrap, err := p.prepareCDCNoSnapshot(t.Context(), restarted, tables, destinations, positions, config.StrategyMerge)
					require.NoError(t, err)
					require.False(t, bootstrap, "completed adoption must not call BeginBaseline again")
					for _, table := range tableNames {
						require.Equal(t, "0/200", src.positions[table], "must stream from the checkpoint, not the original boundary")
					}
					require.Len(t, dest.entries, stateCount)
				})
			}
		})
	}
}

func TestPrepareCDCNoSnapshotRepeatedBoundaryCannotResetState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		external   bool
		boundary   string
		invalidate bool
		wantErr    string
	}{
		{name: "different boundary", external: true, boundary: "0/101", wantErr: "cannot change the boundary"},
		{name: "ordinary CDC state", boundary: "0/100", wantErr: "cannot reset existing CDC state"},
		{name: "missing checkpoint", external: true, boundary: "0/100", invalidate: true, wantErr: "no valid baseline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := &mockLegacyBootstrapDestination{}
			dest.tableSchema = &schema.TableSchema{
				Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"id"},
			}
			newManager := func() *strategy.CDCStateManager {
				manager, err := strategy.NewCDCStateManager(dest, "connector", "raw.orders", "")
				require.NoError(t, err)
				require.NoError(t, manager.RegisterTableState(t.Context(), "public.orders", "raw.orders", "12345", "schema"))
				return manager
			}
			manager := newManager()
			_, err := manager.ResolveSnapshotMode(t.Context(), "never")
			require.NoError(t, err)
			require.NoError(t, manager.BindDestinationIncarnation(t.Context(), "public.orders", "raw.orders"))
			baseline := map[string]string{"public.orders": "0/100"}
			if tc.external {
				require.NoError(t, manager.BeginBaseline(t.Context(), baseline))
			} else {
				require.NoError(t, manager.BeginRun(t.Context(), false))
				require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: baseline}))
			}
			if tc.invalidate {
				kept := make([]destination.CDCStateEntry, 0, len(dest.entries))
				for _, entry := range dest.entries {
					if entry.StateKind == "run" || entry.StateKind == "external_baseline" {
						kept = append(kept, entry)
					}
				}
				dest.entries = kept
			}
			restarted := newManager()
			position, err := restarted.ResumePositionForKeyedMerge(t.Context(), "public.orders")
			require.NoError(t, err)
			src := &noSnapshotBaselineSource{}
			p := &Pipeline{config: &config.IngestConfig{CDCSnapshotMode: "never", CDCStartLSN: tc.boundary}, src: src, dest: dest}
			stateCount := len(dest.entries)
			bootstrap, err := p.prepareCDCNoSnapshot(t.Context(), restarted,
				[]source.SourceTableInfo{{Name: "public.orders", Schema: dest.tableSchema, PrimaryKeys: []string{"id"}}},
				map[string]string{"public.orders": "raw.orders"}, map[string]string{"public.orders": position}, config.StrategyMerge)
			require.ErrorContains(t, err, tc.wantErr)
			require.False(t, bootstrap)
			require.Nil(t, src.positions)
			require.Len(t, dest.entries, stateCount)
		})
	}
}
