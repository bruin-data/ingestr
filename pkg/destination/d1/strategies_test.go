package d1

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

func TestStrategiesUseD1StagingAndPrimaryKeys(t *testing.T) {
	d, db := newTestDestination(t)
	sch := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeInt64},
	}, PrimaryKeys: []string{"id"}}
	run := func(name config.IncrementalStrategy, rows [][2]int64) {
		t.Helper()
		builder := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "value", Type: arrow.PrimitiveTypes.Int64},
		}, nil))
		for _, row := range rows {
			builder.Field(0).(*array.Int64Builder).Append(row[0])
			builder.Field(1).(*array.Int64Builder).Append(row[1])
		}
		record := builder.NewRecordBatch()
		builder.Release()
		cfg := &config.IngestConfig{DestTable: "items", PrimaryKeys: []string{"id"}, IncrementalKey: "value", IncrementalStrategy: name, RunID: "d1test"}
		job := &strategy.IngestionJob{Config: cfg, Destination: d, Schema: sch, SourceSchema: sch, BufferedRecords: writeTestRecords(record)}
		s, err := strategy.Get(name)
		require.NoError(t, err)
		require.NoError(t, s.Execute(t.Context(), job))
	}
	run(config.StrategyReplace, [][2]int64{{1, 10}, {1, 20}, {2, 30}})
	run(config.StrategyMerge, [][2]int64{{1, 40}, {3, 50}})
	run(config.StrategyAppend, [][2]int64{{4, 60}})
	var count, total int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), SUM(value) FROM items`).Scan(&count, &total))
	require.Equal(t, 4, count)
	require.Equal(t, 180, total)
	run(config.StrategyDeleteInsert, [][2]int64{{5, 30}, {6, 50}})
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), SUM(value) FROM items`).Scan(&count, &total))
	require.Equal(t, 3, count)
	require.Equal(t, 140, total)
	run(config.StrategyReplace, nil)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count))
	require.Zero(t, count)
	var leftovers int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name <> 'items'`).Scan(&leftovers))
	require.Zero(t, leftovers)
}
