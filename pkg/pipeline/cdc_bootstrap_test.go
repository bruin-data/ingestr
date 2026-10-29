package pipeline

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
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
