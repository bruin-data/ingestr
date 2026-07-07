package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTableInfo(name string) source.SourceTableInfo {
	return source.SourceTableInfo{
		Name:        name,
		Schema:      streamTestSchema(),
		PrimaryKeys: []string{"id"},
	}
}

func TestStreaming_NewTableAnnouncementPreparesAndRoutes(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.users": mergeTableState("ds.users")})

	var prepared []string
	loop.prepareNewTable = func(_ context.Context, ti source.SourceTableInfo) (*streamTableState, error) {
		prepared = append(prepared, ti.Name)
		return mergeTableState("ds.products"), nil
	}

	info := newTableInfo("public.products")
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.products", TableInfo: &info}
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil), TableName: "public.products"}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))

	assert.Equal(t, []string{"public.products"}, prepared)
	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, "ds.products_staging", dest.writeCalls[0].Table)
	require.Len(t, dest.mergeCalls, 1)
	assert.Equal(t, "ds.products", dest.mergeCalls[0].TargetTable)
}

func TestStreaming_AnnouncementForKnownTableIgnored(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.users": mergeTableState("ds.users")})

	loop.prepareNewTable = func(_ context.Context, ti source.SourceTableInfo) (*streamTableState, error) {
		t.Fatalf("prepareNewTable called for already-known table %s", ti.Name)
		return nil, nil
	}

	info := newTableInfo("public.users")
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.users", TableInfo: &info}
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.users"}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))
	assert.Equal(t, 1, writeCallCount(dest))
}

func TestStreaming_AnnouncementWithoutPreparerDropsBatches(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.users": mergeTableState("ds.users")})

	info := newTableInfo("public.products")
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.products", TableInfo: &info}
	// The CheckedAllocator cleanup in int64RecordBatch verifies this gets released.
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.products"}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))
	assert.Equal(t, 0, writeCallCount(dest))
}

func TestStreaming_NewTablePrepareFailureAborts(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{})

	loop.prepareNewTable = func(_ context.Context, _ source.SourceTableInfo) (*streamTableState, error) {
		return nil, errors.New("boom")
	}

	info := newTableInfo("public.products")
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: "public.products", TableInfo: &info}
	close(records)

	err := loop.run(context.Background(), records)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "public.products")
}

func TestWithLoadTimestampColumn(t *testing.T) {
	s := streamTestSchema()
	got := withLoadTimestampColumn(s)
	require.Len(t, got.Columns, len(s.Columns)+1)
	assert.Equal(t, naming.IngestrLoadedAtColumn, got.Columns[len(got.Columns)-1].Name)
	assert.Equal(t, schema.TypeTimestampTZ, got.Columns[len(got.Columns)-1].DataType)

	// Idempotent when the column is already present.
	again := withLoadTimestampColumn(got)
	assert.Len(t, again.Columns, len(got.Columns))

	assert.Nil(t, withLoadTimestampColumn(nil))
}

func TestMultiTableDestName(t *testing.T) {
	dest := &fakeDestination{}
	assert.Equal(t, "products", multiTableDestName(dest, source.SourceTableInfo{Name: "products"}))
	assert.Equal(t, "ds.app_orders", multiTableDestName(dest, source.SourceTableInfo{Name: "app.orders", DestSchema: "ds"}))
}
