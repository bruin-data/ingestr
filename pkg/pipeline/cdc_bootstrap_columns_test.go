package pipeline

import (
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

func TestPrepareCDCNoSnapshotColumnOverrides(t *testing.T) {
	for _, tt := range []struct {
		name     string
		columns  string
		destType schema.DataType
		wantErr  string
	}{
		{name: "override matches baseline", columns: "value:bigint", destType: schema.TypeInt64},
		{name: "baseline requires override", destType: schema.TypeInt64, wantErr: "column value differs from the source"},
		{name: "override differs from baseline", columns: "value:bigint", destType: schema.TypeString, wantErr: "column value differs from the source"},
		{name: "invalid override", columns: "value:invalid", destType: schema.TypeInt64, wantErr: "failed to parse column overrides"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dest := &mockManagedCDCStateDestination{}
			dest.tableSchema = &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "value", DataType: tt.destType},
			}, PrimaryKeys: []string{"id"}}
			src := &noSnapshotBaselineSource{}
			p := &Pipeline{
				config: &config.IngestConfig{CDCSnapshotMode: "never", CDCStartLSN: "0/100", Columns: tt.columns},
				src:    src,
				dest:   dest,
			}
			manager, err := strategy.NewCDCStateManager(dest, "connector", "raw.items", "")
			require.NoError(t, err)
			tables := make([]source.SourceTableInfo, 0, 2)
			destinations := make(map[string]string)
			for _, name := range []string{"items", "orders"} {
				tables = append(tables, source.SourceTableInfo{
					Name: "public." + name,
					Schema: &schema.TableSchema{Columns: []schema.Column{
						{Name: "id", DataType: schema.TypeInt64},
						{Name: "value", DataType: schema.TypeString},
					}, PrimaryKeys: []string{"id"}},
					PrimaryKeys: []string{"id"},
				})
				destinations["public."+name] = "raw." + name
			}
			bootstrap, err := p.prepareCDCNoSnapshot(t.Context(), manager, tables, destinations, make(map[string]string), config.StrategyMerge)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.False(t, bootstrap)
				require.Nil(t, src.positions)
				return
			}
			require.NoError(t, err)
			require.True(t, bootstrap)
			require.Equal(t, map[string]string{"public.items": "0/100", "public.orders": "0/100"}, src.positions)
			for _, table := range tables {
				require.Equal(t, schema.TypeString, table.Schema.Columns[1].DataType)
			}
		})
	}
}
