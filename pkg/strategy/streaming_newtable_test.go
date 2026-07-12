package strategy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
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

type announcingMultiTableSource struct {
	tables   []source.SourceTableInfo
	records  <-chan source.RecordBatchResult
	readOpts source.MultiTableReadOptions
}

type caseFoldingClaimDestination struct{ *cdcStateDestination }

type postgresNamingDestination struct{ *fakeDestination }

func (d *postgresNamingDestination) GetScheme() string { return "postgres" }

type lateTableCDCStateDestination struct {
	*cdcStateDestination
	rows                      map[string][]int64
	beforeAtomicCreate        func(destination.CDCTargetClaim)
	beforeTargetClaim         func(destination.CDCTargetClaim)
	beforeConditionalTruncate func(string)
}

func newLateTableCDCStateDestination() *lateTableCDCStateDestination {
	stateDest := newCDCStateDestination()
	stateDest.missing = make(map[string]bool)
	return &lateTableCDCStateDestination{
		cdcStateDestination: stateDest,
		rows:                make(map[string][]int64),
	}
}

func (d *lateTableCDCStateDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := d.cdcStateDestination.PrepareTable(ctx, opts); err != nil {
		return err
	}
	d.stateMu.Lock()
	delete(d.missing, opts.Table)
	d.stateMu.Unlock()
	return nil
}

func (d *lateTableCDCStateDestination) TruncateTable(_ context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.truncateCalls = append(d.truncateCalls, table)
	d.rows[table] = nil
	return nil
}

func (d *lateTableCDCStateDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expected string) error {
	if d.beforeConditionalTruncate != nil {
		d.beforeConditionalTruncate(table)
	}
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.missing[table] {
		return fmt.Errorf("destination table %q is missing", table)
	}
	current := d.incarnations[table]
	if current == "" {
		current = "incarnation:" + table
	}
	if current != expected {
		return fmt.Errorf("destination table %q physical incarnation changed", table)
	}
	d.mu.Lock()
	d.truncateCalls = append(d.truncateCalls, table)
	d.rows[table] = nil
	d.mu.Unlock()
	return nil
}

func (d *lateTableCDCStateDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
	if d.beforeAtomicCreate != nil {
		d.beforeAtomicCreate(claim)
	}
	return d.cdcStateDestination.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, claim, opts)
}

func (d *lateTableCDCStateDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	if d.beforeTargetClaim != nil {
		d.beforeTargetClaim(claim)
	}
	return d.cdcStateDestination.ClaimCDCTarget(ctx, claimTable, claim)
}

func (d *caseFoldingClaimDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	claim.DestinationTable = strings.ToLower(claim.DestinationTable)
	return d.cdcStateDestination.ClaimCDCTarget(ctx, claimTable, claim)
}

func (s *announcingMultiTableSource) Schemes() []string { return nil }

func (s *announcingMultiTableSource) Connect(ctx context.Context, uri string) error { return nil }

func (s *announcingMultiTableSource) Close(ctx context.Context) error { return nil }

func (s *announcingMultiTableSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	return nil, errors.New("not implemented")
}

func (s *announcingMultiTableSource) HandlesIncrementality() bool { return false }

func (s *announcingMultiTableSource) IsMultiTable() bool { return true }

func (s *announcingMultiTableSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	return s.tables, nil
}

func (s *announcingMultiTableSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	s.readOpts = opts
	return s.records, nil
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

func TestStreaming_PassesPreparedTablesToReadAll(t *testing.T) {
	records := make(chan source.RecordBatchResult)
	close(records)
	src := &announcingMultiTableSource{
		tables:  []source.SourceTableInfo{newTableInfo("public.users"), newTableInfo("public.orders")},
		records: records,
	}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    &fakeDestination{},
		Tables:         src.tables,
		TableDestNames: map[string]string{"public.users": "users", "public.orders": "orders"},
	}

	require.NoError(t, NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour}).ExecuteMultiTable(t.Context(), job))
	assert.ElementsMatch(t, []string{"public.users", "public.orders"}, src.readOpts.KnownTables)
}

func TestMultiTableJobFreezesBatchReadToPreparedTables(t *testing.T) {
	records := make(chan source.RecordBatchResult)
	close(records)
	src := &announcingMultiTableSource{records: records}
	job := &MultiTableIngestionJob{
		Source: src,
		Tables: []source.SourceTableInfo{newTableInfo("public.users"), newTableInfo("public.orders")},
	}

	read, err := job.ReadAll(t.Context(), source.MultiTableReadOptions{})
	require.NoError(t, err)
	for range read {
	}
	assert.ElementsMatch(t, []string{"public.users", "public.orders"}, src.readOpts.KnownTables)
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

func TestStreaming_AnnouncementWithoutPreparerFailsClosed(t *testing.T) {
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

	require.EqualError(t, loop.run(context.Background(), records), `streaming received 1 row(s) for unknown table "public.products"`)
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

func TestStreaming_NewTableAnnouncementWithoutSchemaReturnsError(t *testing.T) {
	records := make(chan source.RecordBatchResult, 1)
	info := source.SourceTableInfo{Name: "public.products"}
	records <- source.RecordBatchResult{TableName: "public.products", TableInfo: &info}
	close(records)

	src := &announcingMultiTableSource{
		tables:  []source.SourceTableInfo{newTableInfo("public.users")},
		records: records,
	}
	exec := NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	})
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{FullRefresh: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    &fakeDestination{},
		Tables:         src.tables,
		TableDestNames: map[string]string{"public.users": "users"},
	}

	err := exec.ExecuteMultiTable(context.Background(), job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "public.products")
	assert.Contains(t, err.Error(), "no schema")
}

func TestStreaming_NewTableCollisionFailsBeforePrepare(t *testing.T) {
	startup := newTableInfo("a.b_c")
	startup.DestSchema = "landing"
	late := newTableInfo("a_b.c")
	late.DestSchema = "landing"
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: late.Name, TableInfo: &late}
	close(records)

	dest := &fakeDestination{}
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{startup}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{FullRefresh: true, NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{startup.Name: "landing.a_b_c"},
	}

	err := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "multi-table destination collision")
	require.ErrorContains(t, err, startup.Name)
	require.ErrorContains(t, err, late.Name)
	require.Len(t, dest.prepareCalls, 2, "late collision prepared a target or staging table")
}

func TestStreaming_NewTablePhysicalAliasClaimFailsBeforePrepare(t *testing.T) {
	startup := newTableInfo("public.Orders")
	startup.DestSchema = "landing"
	late := newTableInfo("public.orders")
	late.Incarnation = "100"
	late.SchemaFingerprint = "schema-a"
	late.DestSchema = "landing"
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: late.Name, TableInfo: &late}
	close(records)

	dest := &caseFoldingClaimDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "connector-a", "landing.public_Orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(t.Context(), startup.Name, "landing.public_Orders"))
	require.NoError(t, manager.ClaimTarget(t.Context(), startup.Name, "landing.public_Orders"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	dest.mu.Lock()
	dest.prepareCalls = nil
	dest.mu.Unlock()

	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{startup}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{startup.Name: "landing.public_Orders"},
	}

	err = NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, StateManager: manager}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "requires restarting the stream")
	require.Len(t, dest.prepareCalls, 2, "physical alias prepared a target or staging table after its claim failed")
}

func TestStreaming_NewTableExistingUnprovenTargetFailsBeforeSideEffects(t *testing.T) {
	const (
		connector = "connector-a"
		startup   = "public.users"
		late      = "public.products"
		target    = "landing.public_products"
	)
	dest := newLateTableCDCStateDestination()
	dest.rows[target] = []int64{41, 42}
	manager, err := NewCDCStateManager(dest, connector, "landing.public_users", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.ClaimTarget(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	claimsBefore := make(map[string]string, len(dest.targets))
	for table, owner := range dest.targets {
		claimsBefore[table] = owner
	}
	dest.stateMu.Lock()
	stateWritesBefore := dest.cdcWrites
	dest.stateMu.Unlock()
	dest.mu.Lock()
	rowWritesBefore := len(dest.writeCalls)
	mergesBefore := len(dest.mergeCalls)
	dest.mu.Unlock()

	info := newTableInfo(late)
	info.Incarnation = "100"
	info.SchemaFingerprint = "schema-a"
	info.DestSchema = "landing"
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: late, TableInfo: &info}
	records <- source.RecordBatchResult{TableName: late, Truncate: true}
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{newTableInfo(startup)}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{startup: "landing.public_users"},
	}

	err = NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, FlushInterval: time.Hour, StateManager: manager,
	}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "requires restarting the stream")
	require.Equal(t, []int64{41, 42}, dest.rows[target])
	require.Empty(t, dest.truncateCalls)
	require.Equal(t, claimsBefore, dest.targets, "late discovery changed target claims")
	dest.stateMu.Lock()
	require.Equal(t, stateWritesBefore, dest.cdcWrites, "late discovery wrote CDC state")
	dest.stateMu.Unlock()
	require.Len(t, dest.writeCalls, rowWritesBefore, "late discovery wrote rows")
	require.Len(t, dest.mergeCalls, mergesBefore, "late discovery merged rows")
	for _, call := range dest.prepareCalls {
		require.NotEqual(t, target, call.Table, "unproven target was prepared")
		require.NotContains(t, call.Table, "public_products_stream", "unproven target staging was prepared")
	}
}

func TestStreaming_NewTableCompletedStateAuthorizesReplacement(t *testing.T) {
	const (
		connector = "connector-a"
		startup   = "public.users"
		late      = "public.products"
		target    = "landing.public_products"
	)
	dest := newLateTableCDCStateDestination()
	dest.rows[target] = []int64{41, 42}
	completed, err := NewCDCStateManager(dest, connector, target, "")
	require.NoError(t, err)
	require.NoError(t, completed.RegisterTableState(t.Context(), late, target, "100", "schema-a"))
	require.NoError(t, completed.ClaimTarget(t.Context(), late, target))
	require.NoError(t, completed.BeginRun(t.Context(), false))
	require.NoError(t, completed.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions:    map[string]string{late: "00000000/00000010"},
		SnapshotIncarnations: map[string]string{late: "100"},
		SnapshotSchemas:      map[string]string{late: "schema-a"},
	}))

	manager, err := NewCDCStateManager(dest, connector, "landing.public_users", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.ClaimTarget(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	info := newTableInfo(late)
	info.Incarnation = "100"
	info.SchemaFingerprint = "schema-a"
	info.DestSchema = "landing"
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: late, TableInfo: &info}
	records <- source.RecordBatchResult{TableName: late, Truncate: true}
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{newTableInfo(startup)}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{FullRefresh: true, NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{startup: "landing.public_users"},
	}

	require.NoError(t, NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, FlushInterval: time.Hour, StateManager: manager,
	}).ExecuteMultiTable(t.Context(), job))
	require.Equal(t, []string{target}, dest.truncateCalls)
	require.Empty(t, dest.rows[target])
}

func TestStreaming_NewTableAbsentTargetCanBeClaimedAndPrepared(t *testing.T) {
	const (
		startup = "public.users"
		late    = "public.products"
		target  = "landing.public_products"
	)
	dest := newLateTableCDCStateDestination()
	dest.missing[target] = true
	manager, err := NewCDCStateManager(dest, "connector-a", "landing.public_users", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.ClaimTarget(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.BeginRun(t.Context(), false))

	info := newTableInfo(late)
	info.DestSchema = "landing"
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: late, TableInfo: &info}
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{newTableInfo(startup)}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{FullRefresh: true, NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{startup: "landing.public_users"},
	}

	require.NoError(t, NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, FlushInterval: time.Hour, StateManager: manager,
	}).ExecuteMultiTable(t.Context(), job))
	require.NotEmpty(t, dest.targets[target])
	require.False(t, dest.missing[target])
}

func TestStreaming_NewManagedCDCAppendRejectsNonIdempotentDestinationBeforeClaim(t *testing.T) {
	const (
		startup = "public.users"
		late    = "public.events"
		target  = "landing.public_events"
	)
	dest := newLateTableCDCStateDestination()
	dest.missing[target] = true
	manager, err := NewCDCStateManager(dest, "connector-a", "landing.public_users", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.ClaimTarget(t.Context(), startup, "landing.public_users"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	dest.stateMu.Lock()
	stateWritesBefore := dest.cdcWrites
	dest.stateMu.Unlock()

	info := source.SourceTableInfo{
		Name: late, Schema: keylessCDCSchema(), DestSchema: "landing",
		Incarnation: "100", SchemaFingerprint: "schema-a",
	}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: late, TableInfo: &info}
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{newTableInfo(startup)}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{FullRefresh: true, NoLoadTimestamp: true, FlushInterval: time.Hour, FlushRecords: 1},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{startup: "landing.public_users"},
	}

	err = NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyAppend, FlushInterval: time.Hour, StateManager: manager,
	}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "managed streaming CDC append requires destination support for idempotent commit-token writes")
	require.Empty(t, dest.targets[target], "unsafe late target was claimed")
	require.True(t, dest.missing[target], "unsafe late target was created")
	dest.stateMu.Lock()
	require.Equal(t, stateWritesBefore, dest.cdcWrites, "unsafe late discovery wrote CDC state")
	dest.stateMu.Unlock()
	for _, call := range dest.prepareCalls {
		require.NotEqual(t, target, call.Table, "unsafe late target was prepared")
	}
}

func TestCDCStateLateDiscoveredTargetClaimIsAtomicAcrossConnectors(t *testing.T) {
	const target = "landing.public_products"
	dest := newLateTableCDCStateDestination()
	dest.missing[target] = true
	results := make(chan error, 2)
	for _, connector := range []string{"connector-a", "connector-b"} {
		connector := connector
		go func() {
			manager, err := NewCDCStateManager(dest, connector, target, "")
			if err == nil {
				err = manager.prepareTargetTable(context.Background())
			}
			if err == nil {
				err = manager.BeginRun(context.Background(), false)
			}
			if err == nil {
				err = manager.ClaimLateDiscoveredTarget(context.Background(), "public.products", target, "", "", false, destination.PrepareOptions{Schema: streamTestSchema()})
			}
			results <- err
		}()
	}

	var successes int
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	require.NotEmpty(t, dest.targets[target])
}

func TestCDCStateLateDiscoveredTargetFullRefreshAuthorizesExistingTarget(t *testing.T) {
	const target = "landing.public_products"
	dest := newLateTableCDCStateDestination()
	dest.rows[target] = []int64{41, 42}
	manager, err := NewCDCStateManager(dest, "connector-a", target, "")
	require.NoError(t, err)
	require.NoError(t, manager.prepareTargetTable(t.Context()))
	require.NoError(t, manager.BeginRun(t.Context(), true))

	require.NoError(t, manager.ClaimLateDiscoveredTarget(
		t.Context(), "public.products", target, "", "", true, destination.PrepareOptions{Schema: streamTestSchema()},
	))
	require.NotEmpty(t, dest.targets[target])
	require.Equal(t, []int64{41, 42}, dest.rows[target], "authorization mutated the target")
}

func TestCDCStateLateDiscoveredTargetRejectsExternalCreateAfterAbsentProbe(t *testing.T) {
	const target = "landing.public_products"
	dest := newLateTableCDCStateDestination()
	dest.missing[target] = true
	manager, err := NewCDCStateManager(dest, "connector-a", target, "")
	require.NoError(t, err)
	require.NoError(t, manager.prepareTargetTable(t.Context()))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	dest.beforeAtomicCreate = func(claim destination.CDCTargetClaim) {
		if claim.DestinationTable != target {
			return
		}
		dest.stateMu.Lock()
		delete(dest.missing, target)
		dest.incarnations[target] = "external-create"
		dest.stateMu.Unlock()
		dest.rows[target] = []int64{77}
	}

	err = manager.ClaimLateDiscoveredTarget(t.Context(), "public.products", target, "100", "schema-a", false,
		destination.PrepareOptions{Schema: streamTestSchema(), PrimaryKeys: []string{"id"}})
	require.ErrorContains(t, err, "already exists")
	require.Equal(t, []int64{77}, dest.rows[target])
	require.Empty(t, dest.targets[target])
	require.Empty(t, dest.truncateCalls)
}

func TestCDCStateLateDiscoveredTargetRejectsRecreateAfterAuthorizedProbe(t *testing.T) {
	const (
		connector  = "connector-a"
		sourceName = "public.products"
		target     = "landing.public_products"
	)
	dest := newLateTableCDCStateDestination()
	seedLateCompletedState(t, dest, connector, sourceName, target)
	manager, err := NewCDCStateManager(dest, connector, target, "")
	require.NoError(t, err)
	require.NoError(t, manager.prepareTargetTable(t.Context()))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	dest.beforeTargetClaim = func(claim destination.CDCTargetClaim) {
		if claim.DestinationTable != target {
			return
		}
		dest.stateMu.Lock()
		dest.incarnations[target] = "external-recreate"
		dest.stateMu.Unlock()
		dest.rows[target] = []int64{88}
	}
	require.NoError(t, manager.ClaimLateDiscoveredTarget(t.Context(), sourceName, target, "100", "schema-a", false,
		destination.PrepareOptions{Schema: streamTestSchema(), PrimaryKeys: []string{"id"}}))

	handled, err := manager.ApplyLateSnapshotBoundary(t.Context(), sourceName, target)
	require.True(t, handled)
	require.ErrorContains(t, err, "replaced after its late-target claim")
	require.Equal(t, []int64{88}, dest.rows[target])
	require.Empty(t, dest.truncateCalls)
}

func TestCDCStateLateDiscoveredTargetConditionalTruncateRejectsRecreate(t *testing.T) {
	const (
		connector  = "connector-a"
		sourceName = "public.products"
		target     = "landing.public_products"
	)
	dest := newLateTableCDCStateDestination()
	seedLateCompletedState(t, dest, connector, sourceName, target)
	manager, err := NewCDCStateManager(dest, connector, target, "")
	require.NoError(t, err)
	require.NoError(t, manager.prepareTargetTable(t.Context()))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	require.NoError(t, manager.ClaimLateDiscoveredTarget(t.Context(), sourceName, target, "100", "schema-a", false,
		destination.PrepareOptions{Schema: streamTestSchema(), PrimaryKeys: []string{"id"}}))
	dest.beforeConditionalTruncate = func(table string) {
		dest.stateMu.Lock()
		dest.incarnations[table] = "external-between-bind-and-truncate"
		dest.stateMu.Unlock()
		dest.rows[table] = []int64{99}
	}

	handled, err := manager.ApplyLateSnapshotBoundary(t.Context(), sourceName, target)
	require.True(t, handled)
	require.ErrorContains(t, err, "conditionally truncate")
	require.Equal(t, []int64{99}, dest.rows[target])
	require.Empty(t, dest.truncateCalls)
}

func TestCDCStateLateDiscoveredTargetRejectsEmptySourceProof(t *testing.T) {
	const (
		connector  = "connector-a"
		sourceName = "public.products"
		target     = "landing.public_products"
	)
	dest := newLateTableCDCStateDestination()
	seedLateCompletedState(t, dest, connector, sourceName, target)
	manager, err := NewCDCStateManager(dest, connector, target, "")
	require.NoError(t, err)
	require.NoError(t, manager.prepareTargetTable(t.Context()))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	claimsBefore := make(map[string]string, len(dest.targets))
	for table, owner := range dest.targets {
		claimsBefore[table] = owner
	}

	err = manager.ClaimLateDiscoveredTarget(t.Context(), sourceName, target, "", "", false,
		destination.PrepareOptions{Schema: streamTestSchema(), PrimaryKeys: []string{"id"}})
	require.ErrorContains(t, err, "no complete incarnation and schema proof")
	require.Equal(t, claimsBefore, dest.targets)
	require.Empty(t, dest.truncateCalls)
}

func seedLateCompletedState(t *testing.T, dest *lateTableCDCStateDestination, connector, sourceName, target string) {
	t.Helper()
	dest.rows[target] = []int64{41, 42}
	manager, err := NewCDCStateManager(dest, connector, target, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(t.Context(), sourceName, target, "100", "schema-a"))
	require.NoError(t, manager.ClaimTarget(t.Context(), sourceName, target))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions:    map[string]string{sourceName: "00000000/00000010"},
		SnapshotIncarnations: map[string]string{sourceName: "100"},
		SnapshotSchemas:      map[string]string{sourceName: "schema-a"},
	}))
}

func TestStreamingDynamicTableFreezeRejectsBeforePrepare(t *testing.T) {
	initial := newTableInfo("users")
	dynamicSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeString, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	destinationSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	dynamic := source.SourceTableInfo{Name: "products", Schema: dynamicSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: dynamic.Name, TableInfo: &dynamic}
	close(records)
	dest := &fakeDestination{tableSchemas: map[string]*schema.TableSchema{
		"users": initial.Schema, "products": destinationSchema,
	}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{SchemaContract: "freeze", NoLoadTimestamp: true, FullRefresh: true},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{initial}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{initial}, TableDestNames: map[string]string{"users": "users"},
	}

	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 1})
	err := exec.ExecuteMultiTable(context.Background(), job)
	require.ErrorContains(t, err, "schema contract violation")
	for _, call := range dest.prepareCalls {
		require.NotEqual(t, "products", call.Table)
		require.NotContains(t, call.Table, "products")
	}
}

func TestStreamingDynamicTableDiscardValueUsesFinalSchemaAndTransforms(t *testing.T) {
	initial := newTableInfo("users")
	dynamicSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeString, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	destinationSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	dynamic := source.SourceTableInfo{Name: "products", Schema: dynamicSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: dynamic.Name, TableInfo: &dynamic}
	records <- source.RecordBatchResult{
		TableName: dynamic.Name, Batch: intStringRecordBatch(t, "id", []int64{1}, "value", []string{"invalid"}),
	}
	close(records)
	base := &fakeDestination{tableSchemas: map[string]*schema.TableSchema{
		"users": initial.Schema, "products": destinationSchema,
	}}
	dest := &contractCaptureReplaceDestination{fakeDestination: base}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{SchemaContract: "discard_value", NoLoadTimestamp: true, FullRefresh: true},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{initial}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{initial}, TableDestNames: map[string]string{"users": "users"},
	}

	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 1})
	require.NoError(t, exec.ExecuteMultiTable(context.Background(), job))
	dest.muCapture.Lock()
	defer dest.muCapture.Unlock()
	require.Equal(t, []int64{1}, dest.rows)
	require.Equal(t, []int{1}, dest.nulls)
	require.True(t, arrow.TypeEqual(arrow.PrimitiveTypes.Int64, dest.types[0]))
	require.Equal(t, schema.TypeInt64, dest.schemas[0].Columns[1].DataType)
}

func TestStreamingDynamicTableRejectsUnsupportedDecimalBeforePrepare(t *testing.T) {
	initial := newTableInfo("users")
	dynamic := source.SourceTableInfo{Name: "products", Schema: &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 50, Scale: 2,
	}}}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: dynamic.Name, TableInfo: &dynamic}
	close(records)
	dest := &fakeDestination{}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{NoLoadTimestamp: true, FullRefresh: true},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{initial}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{initial}, TableDestNames: map[string]string{"users": "users"},
	}

	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 1})
	err := exec.ExecuteMultiTable(context.Background(), job)
	require.ErrorContains(t, err, "maximum supported precision is 38")
	require.ErrorContains(t, err, "products")
	for _, call := range dest.prepareCalls {
		require.NotContains(t, call.Table, "products")
	}
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

	longSource := strings.Repeat("s", 40) + "." + strings.Repeat("t", 40)
	longInfo := source.SourceTableInfo{Name: longSource, DestSchema: "landing"}
	postgresDest := &postgresNamingDestination{fakeDestination: &fakeDestination{}}
	lateName := multiTableDestName(postgresDest, longInfo)
	require.Equal(t, lateName, multiTableDestName(postgresDest, longInfo), "late mapping changed across restart")
	parts := strings.Split(lateName, ".")
	require.Len(t, parts, 2)
	require.LessOrEqual(t, len(parts[1]), destination.MaxIdentifierLength("postgres"))
}

func TestMultiTableDestNameShortensMultibyteLateTable(t *testing.T) {
	postgresDest := &postgresNamingDestination{fakeDestination: &fakeDestination{}}
	firstInfo := source.SourceTableInfo{
		Name:       strings.Repeat("é", 20) + "." + strings.Repeat("界", 14) + "一",
		DestSchema: "landing",
	}
	secondInfo := firstInfo
	secondInfo.Name = strings.Repeat("é", 20) + "." + strings.Repeat("界", 14) + "二"

	first := multiTableDestName(postgresDest, firstInfo)
	require.Equal(t, first, multiTableDestName(postgresDest, firstInfo), "late multibyte mapping changed across restart")
	second := multiTableDestName(postgresDest, secondInfo)
	require.NotEqual(t, first, second)
	for _, mapped := range []string{first, second} {
		require.True(t, utf8.ValidString(mapped))
		parts := strings.Split(mapped, ".")
		require.Len(t, parts, 2)
		require.LessOrEqual(t, len(parts[1]), destination.MaxIdentifierLength("postgres"))
	}
}
