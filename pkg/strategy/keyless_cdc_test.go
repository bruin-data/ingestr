package strategy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keylessCDCSchema mimics what the Postgres CDC source emits for a table with
// no primary key and no replica identity index: source columns plus the CDC
// metadata columns, and an empty PrimaryKeys list.
func keylessCDCSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "payload", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
	}
}

func schemaHasColumn(s *schema.TableSchema, name string) bool {
	for _, col := range s.Columns {
		if col.Name == name {
			return true
		}
	}
	return false
}

type idempotentCommitTokenDestination struct {
	*fakeDestination
	tokenMu         sync.Mutex
	committedTokens map[source.DurableID]struct{}
}

func (*idempotentCommitTokenDestination) SupportsIdempotentCommitTokenWrites() bool { return true }

func (d *idempotentCommitTokenDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.tokenMu.Lock()
	defer d.tokenMu.Unlock()
	if d.committedTokens == nil {
		d.committedTokens = make(map[source.DurableID]struct{})
	}
	if _, duplicate := d.committedTokens[opts.CommitToken]; opts.CommitToken != "" && duplicate {
		drainAndRelease(records)
		return nil
	}
	if err := d.fakeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if opts.CommitToken != "" {
		d.committedTokens[opts.CommitToken] = struct{}{}
	}
	return nil
}

type managedKeylessDestination struct {
	*cdcStateDestination
	dataMu             sync.Mutex
	dataWrites         []destination.WriteOptions
	committedData      map[source.DurableID]struct{}
	visibleRows        int64
	truncates          int
	failStateAfterData bool
}

type emptyIncarnationManagedKeylessDestination struct {
	*managedKeylessDestination
}

func (*emptyIncarnationManagedKeylessDestination) CDCTargetIncarnation(context.Context, string) (string, bool, error) {
	return "", true, nil
}

type replacingManagedKeylessDestination struct {
	*managedKeylessDestination
	writes int
}

type recreateDuringInvalidationManagedKeylessDestination struct {
	*managedKeylessDestination
	recreateMu sync.Mutex
	armed      bool
}

type managedAtomicKeylessDestination struct {
	*managedKeylessDestination
}

func (d *recreateDuringInvalidationManagedKeylessDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := d.managedKeylessDestination.WriteCDCState(ctx, records, opts); err != nil {
		return err
	}
	d.recreateMu.Lock()
	armed := d.armed
	d.armed = false
	d.recreateMu.Unlock()
	if armed {
		d.stateMu.Lock()
		d.incarnations["raw.events"] = "replacement-incarnation"
		d.stateMu.Unlock()
	}
	return nil
}

func (d *replacingManagedKeylessDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	current, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
	if err != nil {
		drainAndRelease(records)
		return err
	}
	if !exists || opts.CDCExpectedIncarnation == "" || opts.CDCExpectedIncarnation != current {
		drainAndRelease(records)
		return errors.New("destination was replaced before managed keyless write")
	}
	if err := d.managedKeylessDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	d.writes++
	if d.writes == 1 {
		d.stateMu.Lock()
		d.incarnations[opts.Table] = "replacement-incarnation"
		d.stateMu.Unlock()
	}
	return nil
}

func (*managedAtomicKeylessDestination) BeginAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) error {
	return nil
}

func (*managedAtomicKeylessDestination) EvolveAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) error {
	return nil
}

func (*managedAtomicKeylessDestination) WriteAtomicSnapshot(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
	drainAndRelease(records)
	return nil
}

func (*managedAtomicKeylessDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	return opts.CDCExpectedIncarnation, nil
}

func (*managedAtomicKeylessDestination) MergeRecords(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions, _ destination.MergeOptions) error {
	drainAndRelease(records)
	return nil
}

func (*managedKeylessDestination) SupportsIdempotentCommitTokenWrites() bool { return true }

func (d *managedKeylessDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expected string) error {
	if err := d.cdcStateDestination.TruncateCDCTableIfIncarnation(ctx, table, expected); err != nil {
		return err
	}
	d.dataMu.Lock()
	d.visibleRows = 0
	d.truncates++
	d.dataMu.Unlock()
	return nil
}

func (d *managedKeylessDestination) WriteParallel(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	var rows int64
	for result := range records {
		if result.Batch != nil {
			rows += result.Batch.NumRows()
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	d.dataMu.Lock()
	d.dataWrites = append(d.dataWrites, opts)
	if token := opts.CommitToken; token != "" {
		if d.committedData == nil {
			d.committedData = make(map[source.DurableID]struct{})
		}
		if _, exists := d.committedData[token]; !exists {
			d.committedData[token] = struct{}{}
			d.visibleRows += rows
		}
	} else {
		d.visibleRows += rows
	}
	d.dataMu.Unlock()
	return nil
}

func (d *managedKeylessDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.dataMu.Lock()
	fail := d.failStateAfterData && len(d.dataWrites) > 0
	if fail {
		d.failStateAfterData = false
	}
	d.dataMu.Unlock()
	if fail {
		drainAndRelease(records)
		return errors.New("injected state persistence failure")
	}
	return d.cdcStateDestination.WriteCDCState(ctx, records, opts)
}

func TestMergeStrategy_MultiTableKeylessCDCLandsAppendOnly(t *testing.T) {
	dest := &fakeDestination{}
	table := source.SourceTableInfo{Name: "public.events", Schema: keylessCDCSchema()}

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.events",
		DurableCommitID: "wal:public.events:0/10", DurableCommitPosition: "0/10",
	}
	close(records)

	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{"public.events": "events"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))

	dest.mu.Lock()
	defer dest.mu.Unlock()

	// One PrepareTable call: the final table, keyless, CDC-relaxed, and keeping
	// the staging-only _cdc_unchanged_cols column since batches carry it.
	require.Len(t, dest.prepareCalls, 1)
	prep := dest.prepareCalls[0]
	assert.Equal(t, "events", prep.Table)
	assert.Empty(t, prep.PrimaryKeys)
	assert.True(t, prep.CDCMode)
	assert.False(t, prep.DropFirst)
	assert.True(t, schemaHasColumn(prep.Schema, destination.CDCUnchangedColsColumn))

	// Rows land in the final table directly; nothing is merged or dropped.
	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, "events", dest.writeCalls[0].Table)
	assert.Empty(t, dest.mergeCalls)
	assert.Empty(t, dest.dropCalls)
}

func TestMergeStrategy_MultiTableMixedKeyedAndKeyless(t *testing.T) {
	dest := &fakeDestination{}
	keyed := source.SourceTableInfo{Name: "public.users", Schema: streamTestSchema(), PrimaryKeys: []string{"id"}}
	keyless := source.SourceTableInfo{Name: "public.events", Schema: keylessCDCSchema()}

	records := make(chan source.RecordBatchResult)
	close(records)

	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{keyed, keyless}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{"public.users": "users", "public.events": "events"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))

	dest.mu.Lock()
	defer dest.mu.Unlock()

	// The keyed table merges via staging; the keyless one never reaches MergeTable.
	require.Len(t, dest.mergeCalls, 1)
	assert.Equal(t, "users", dest.mergeCalls[0].TargetTable)
}

func TestStreaming_PrepareTableKeylessCDCSkipsStaging(t *testing.T) {
	dest := &idempotentCommitTokenDestination{fakeDestination: &fakeDestination{}}
	exec := NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	})

	st := &streamTableState{
		destTable: "events",
		schema:    keylessCDCSchema(),
		isCDC:     true,
	}
	require.NoError(t, exec.prepareTable(context.Background(), dest, &config.IngestConfig{}, st))

	assert.Empty(t, st.stagingTable)

	dest.mu.Lock()
	require.Len(t, dest.prepareCalls, 1)
	prep := dest.prepareCalls[0]
	dest.mu.Unlock()
	assert.Equal(t, "events", prep.Table)
	assert.True(t, prep.CDCMode)
	assert.True(t, schemaHasColumn(prep.Schema, destination.CDCUnchangedColsColumn))

	// A flush for this table writes directly to the destination table and
	// performs no merge.
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.events",
		DurableCommitID: "wal:public.events:0/10", DurableCommitPosition: "0/10",
	}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, "events", dest.writeCalls[0].Table)
	assert.False(t, dest.writeCalls[0].StagingTable)
	assert.Empty(t, dest.mergeCalls)
}

func TestIdempotentCommitTokenDestinationSuppressesReplay(t *testing.T) {
	base := &fakeDestination{}
	dest := &idempotentCommitTokenDestination{fakeDestination: base}
	opts := destination.WriteOptions{Table: "events", CommitToken: "wal:0/10"}
	records := func() <-chan source.RecordBatchResult {
		return mustClosedRecords(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)})
	}

	require.NoError(t, dest.WriteParallel(t.Context(), records(), opts))
	require.NoError(t, dest.WriteParallel(t.Context(), records(), opts))
	require.Len(t, base.writeCalls, 1)
}

func TestManagedCDCDataWriteTokenIsStableAndTableScoped(t *testing.T) {
	token := func(table, batchID string) source.DurableID {
		return managedCDCDataWriteID(table, source.DurableID(batchID))
	}
	require.Equal(t, token("public.events", "tx-1"), token("public.events", "tx-1"))
	require.NotEqual(t, token("public.events", "tx-1"), token("public.audit", "tx-1"))
	require.NotEqual(t, token("public.events", "tx-1"), token("public.events", "tx-2"))
}

func TestStreamingManagedKeylessMergeRequestsStableDataBatches(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "stream-keyless-contract", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "source-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))

	job, src, _ := minimalJob()
	job.Config.SourceTable = "public.events"
	job.Config.DestTable = "raw.events"
	job.Config.PrimaryKeys = nil
	job.Config.NoLoadTimestamp = true
	job.Schema = keylessCDCSchema()
	job.SourceSchema = job.Schema
	job.Destination = dest
	job.CDCStateManager = manager
	src.readCh = mustClosedRecords()

	require.NoError(t, NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, FlushInterval: time.Hour,
	}).Execute(ctx, job))
	require.True(t, src.readOpts.CDCStableDataBatches)
}

func TestStreamingMultiTableManagedKeylessMergeRequestsStableDataBatches(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "stream-multi-keyless-contract", "raw.events", "")
	require.NoError(t, err)
	keyedSchema := keylessCDCSchema()
	keyedSchema.PrimaryKeys = []string{"id"}
	tables := []source.SourceTableInfo{
		{Name: "public.users", Schema: keyedSchema, PrimaryKeys: []string{"id"}, Incarnation: "users-v1"},
		{Name: "public.events", Schema: keylessCDCSchema(), Incarnation: "events-v1"},
	}
	for _, table := range tables {
		require.NoError(t, manager.RegisterTableIncarnation(ctx, table.Name, "raw."+table.Name[len("public."):], table.Incarnation))
	}
	require.NoError(t, manager.BeginRun(ctx, false))
	src := &announcingMultiTableSource{tables: tables, records: mustClosedRecords()}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: src, Destination: dest, Tables: tables,
		TableDestNames:  map[string]string{"public.users": "raw.users", "public.events": "raw.events"},
		CDCStateManager: manager,
	}

	require.NoError(t, NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, FlushInterval: time.Hour,
	}).ExecuteMultiTable(ctx, job))
	require.True(t, src.readOpts.CDCStableDataBatches)
}

func TestStreamingManagedFullRefreshRequestsStableDataBatchesForLateKeylessTable(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "stream-late-keyless-contract", "raw.users", "")
	require.NoError(t, err)
	keyedSchema := keylessCDCSchema()
	keyedSchema.PrimaryKeys = []string{"id"}
	table := source.SourceTableInfo{
		Name: "public.users", Schema: keyedSchema, PrimaryKeys: []string{"id"}, Incarnation: "users-v1",
	}
	require.NoError(t, manager.RegisterTableIncarnation(ctx, table.Name, "raw.users", table.Incarnation))
	require.NoError(t, manager.BeginRun(ctx, false))
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: mustClosedRecords()}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{
			IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true, FullRefresh: true,
		},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "raw.users"}, CDCStateManager: manager,
	}

	require.NoError(t, NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, FlushInterval: time.Hour,
	}).ExecuteMultiTable(ctx, job))
	require.True(t, src.readOpts.CDCStableDataBatches)
}

func TestStreamingManagedKeylessCDCReplayUsesDataTokenWithoutResumeAuthority(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{
		cdcStateDestination: newCDCStateDestination(),
		failStateAfterData:  true,
	}
	manager, err := NewCDCStateManager(dest, "managed-keyless-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	require.Equal(t, "incarnation:raw.events", manager.BoundDestinationIncarnation("public.events"))

	newLoop := func(flushRecords int64) *flushLoop {
		st := &streamTableState{
			destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true,
			incarnation: "100", schemaFingerprint: "schema-v1",
		}
		cfg := config.DefaultConfig()
		cfg.NoLoadTimestamp = true
		return newFlushLoop(dest, cfg, StreamingOptions{
			FlushInterval: time.Hour,
			FlushRecords:  flushRecords,
			Strategy:      config.StrategyMerge,
			StateManager:  manager,
		}, map[string]*streamTableState{"public.events": st})
	}
	newRecords := func(position string) <-chan source.RecordBatchResult {
		records := make(chan source.RecordBatchResult, 2)
		records <- source.RecordBatchResult{
			TableName: "public.events",
			Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
			CommitToken: source.CDCStateCommitToken{
				Position:    position,
				DataBatchID: "public.events:1:0/110/2:0/110/2",
			},
		}
		records <- source.RecordBatchResult{
			TableName: "public.events",
			Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
			CommitToken: source.CDCStateCommitToken{
				Position:    position,
				DataBatchID: "public.events:1:0/120/2:0/120/2",
			},
		}
		close(records)
		return records
	}

	err = newLoop(100).run(ctx, newRecords("0/100"))
	require.ErrorContains(t, err, "injected state persistence failure")
	require.NoError(t, newLoop(1).run(ctx, newRecords("0/105")))

	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 4)
	require.EqualValues(t, 2, dest.visibleRows)
	first, second := dest.dataWrites[0], dest.dataWrites[1]
	replayFirst, replaySecond := dest.dataWrites[2], dest.dataWrites[3]
	dest.dataMu.Unlock()
	firstToken := first.CommitToken
	secondToken := second.CommitToken
	require.Equal(t, managedCDCDataWriteID("public.events", "public.events:1:0/110/2:0/110/2"), firstToken)
	require.Equal(t, managedCDCDataWriteID("public.events", "public.events:1:0/120/2:0/120/2"), secondToken)
	require.NotEqual(t, firstToken, secondToken, "duplicate-content batches at the same globally safe position must not collide")
	require.Equal(t, first.CommitToken, replayFirst.CommitToken, "global safe position changes must not perturb a source batch's data-write identity")
	require.Equal(t, second.CommitToken, replaySecond.CommitToken, "global safe position changes must not perturb a source batch's data-write identity")
	require.Equal(t, "incarnation:raw.events", first.CDCExpectedIncarnation)
	require.Equal(t, first.CDCExpectedIncarnation, second.CDCExpectedIncarnation)
	require.Equal(t, first.CDCExpectedIncarnation, replayFirst.CDCExpectedIncarnation)
	require.Equal(t, first.CDCExpectedIncarnation, replaySecond.CDCExpectedIncarnation)
	require.True(t, first.SkipCDCResume)
	require.True(t, second.SkipCDCResume)
	require.True(t, replayFirst.SkipCDCResume)
	require.True(t, replaySecond.SkipCDCResume)
	require.Empty(t, first.CDCResumeLSN)
	require.Empty(t, second.CDCResumeLSN)
	require.Empty(t, replayFirst.CDCResumeLSN)
	require.Empty(t, replaySecond.CDCResumeLSN)
}

func TestStreamingStandaloneKeylessCDCWithoutDurableIdentityUsesLegacyBatchWrite(t *testing.T) {
	dest := &fakeDestination{}
	st := &streamTableState{
		destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true,
	}
	cfg := config.DefaultConfig()
	cfg.NoLoadTimestamp = true
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: "public.events",
		Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
	}
	close(records)

	require.NoError(t, loop.run(t.Context(), records))
	dest.mu.Lock()
	require.Len(t, dest.writeCalls, 1)
	require.Empty(t, dest.writeCalls[0].CommitToken)
	dest.mu.Unlock()
}

func TestStreamingManagedKeyedAppendReplayUsesDataTokenWithoutResumeAuthority(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{
		cdcStateDestination: newCDCStateDestination(),
		failStateAfterData:  true,
	}
	manager, err := NewCDCStateManager(dest, "managed-keyed-append-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))

	tableSchema := keylessCDCSchema()
	tableSchema.PrimaryKeys = []string{"id"}
	newLoop := func() *flushLoop {
		st := &streamTableState{
			destTable: "raw.events", schema: tableSchema, primaryKeys: []string{"id"}, isCDC: true,
			incarnation: "100", schemaFingerprint: "schema-v1",
		}
		cfg := config.DefaultConfig()
		cfg.NoLoadTimestamp = true
		return newFlushLoop(dest, cfg, StreamingOptions{
			FlushInterval: time.Hour,
			FlushRecords:  100,
			Strategy:      config.StrategyAppend,
			StateManager:  manager,
		}, map[string]*streamTableState{"public.events": st})
	}
	newRecords := func(position string) <-chan source.RecordBatchResult {
		records := make(chan source.RecordBatchResult, 2)
		for i, batchID := range []string{"public.events:tx-1", "public.events:tx-2"} {
			records <- source.RecordBatchResult{
				TableName: "public.events",
				Batch:     int64RecordBatch(t, "id", []int64{int64(i + 1)}, nil),
				CommitToken: source.CDCStateCommitToken{
					Position: position, DataBatchID: source.DurableID(batchID),
				},
			}
		}
		close(records)
		return records
	}

	require.ErrorContains(t, newLoop().run(ctx, newRecords("0/100")), "injected state persistence failure")
	require.NoError(t, newLoop().run(ctx, newRecords("0/105")))

	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 4)
	require.EqualValues(t, 2, dest.visibleRows)
	require.Equal(t, dest.dataWrites[0].CommitToken, dest.dataWrites[2].CommitToken)
	require.Equal(t, dest.dataWrites[1].CommitToken, dest.dataWrites[3].CommitToken)
	for _, writeOpts := range dest.dataWrites {
		require.True(t, writeOpts.SkipCDCResume)
		require.Empty(t, writeOpts.CDCResumeLSN)
		require.Equal(t, "incarnation:raw.events", writeOpts.CDCExpectedIncarnation)
	}
	dest.dataMu.Unlock()
}

func TestBatchAppendManagedCDCReplayUsesStableDataToken(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{
		cdcStateDestination: newCDCStateDestination(),
		failStateAfterData:  true,
	}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "batch-append-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, "public.events", "raw.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))

	job, src, _ := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyAppend
	job.Config.SourceTable = "public.events"
	job.Config.DestTable = "raw.events"
	job.Config.CDCResumeLSN = "0/10"
	job.Schema = keylessCDCSchema()
	job.Schema.PrimaryKeys = []string{"id"}
	job.SourceSchema = job.Schema
	job.Destination = dest
	job.CDCStateManager = manager
	newRecords := func(position string) <-chan source.RecordBatchResult {
		records := make(chan source.RecordBatchResult, 2)
		for i, batchID := range []string{"public.events:tx-1", "public.events:tx-2"} {
			records <- source.RecordBatchResult{
				Batch: int64RecordBatch(t, "id", []int64{int64(i + 1)}, nil),
				CommitToken: source.CDCStateCommitToken{
					Position: position, DataBatchID: source.DurableID(batchID),
				},
			}
		}
		close(records)
		return records
	}

	src.readCh = newRecords("0/20")
	require.NoError(t, (&AppendStrategy{}).Execute(ctx, job))
	require.True(t, src.readOpts.CDCStableDataBatches)
	require.ErrorContains(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/20"}), "injected state persistence failure")
	src.readCh = newRecords("0/25")
	require.NoError(t, (&AppendStrategy{}).Execute(ctx, job))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/25"}))

	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 4)
	require.EqualValues(t, 2, dest.visibleRows)
	require.Equal(t, dest.dataWrites[0].CommitToken, dest.dataWrites[2].CommitToken)
	require.Equal(t, dest.dataWrites[1].CommitToken, dest.dataWrites[3].CommitToken)
	dest.dataMu.Unlock()
}

func TestManagedCDCAppendRejectsNonIdempotentDestinationBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "non-idempotent-append", "raw.events", "")
	require.NoError(t, err)
	job, src, _ := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyAppend
	job.Config.SourceTable = "public.events"
	job.Config.DestTable = "raw.events"
	job.Schema = keylessCDCSchema()
	job.SourceSchema = job.Schema
	job.Destination = dest
	job.CDCStateManager = manager

	err = (&AppendStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "managed CDC append requires destination support for idempotent commit-token writes")
	require.Empty(t, dest.prepareCalls)
	require.Empty(t, dest.writeCalls)
	require.False(t, src.readCalled)
}

func TestMultiTableManagedCDCAppendRejectsNonIdempotentDestinationBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "non-idempotent-multi-append", "raw.events", "")
	require.NoError(t, err)
	table := source.SourceTableInfo{Name: "public.events", Schema: keylessCDCSchema()}
	job := &MultiTableIngestionJob{
		Config:          &config.IngestConfig{IncrementalStrategy: config.StrategyAppend},
		Source:          &announcingMultiTableSource{tables: []source.SourceTableInfo{table}},
		Destination:     dest,
		Tables:          []source.SourceTableInfo{table},
		TableDestNames:  map[string]string{table.Name: "raw.events"},
		CDCStateManager: manager,
	}

	err = (&AppendStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "managed multi-table CDC append requires destination support for idempotent commit-token writes")
	require.Empty(t, dest.prepareCalls)
	require.Empty(t, dest.writeCalls)
}

func TestMultiTableManagedKeylessCDCMergeRejectsNonIdempotentDestinationBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "non-idempotent-keyless-merge", "raw.events", "")
	require.NoError(t, err)
	table := source.SourceTableInfo{Name: "public.events", Schema: keylessCDCSchema()}
	job := &MultiTableIngestionJob{
		Config:          &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source:          &announcingMultiTableSource{tables: []source.SourceTableInfo{table}},
		Destination:     dest,
		Tables:          []source.SourceTableInfo{table},
		TableDestNames:  map[string]string{table.Name: "raw.events"},
		CDCStateManager: manager,
	}

	err = (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "managed keyless CDC merge requires destination support for idempotent commit-token writes")
	require.Empty(t, dest.prepareCalls)
	require.Empty(t, dest.writeCalls)
}

func TestManagedStreamingAppendRejectsNonIdempotentDestinationBeforeMutation(t *testing.T) {
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "non-idempotent-streaming-append", "raw.events", "")
	require.NoError(t, err)
	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyAppend, StateManager: manager})
	st := &streamTableState{destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true}

	err = executor.prepareTable(t.Context(), dest, &config.IngestConfig{}, st)
	require.ErrorContains(t, err, "managed streaming CDC append requires destination support for idempotent commit-token writes")
	require.Empty(t, dest.prepareCalls)
	require.Empty(t, dest.writeCalls)
}

func TestDurableKeylessCDCWriteRejectsNonIdempotentDestinationBeforeConsuming(t *testing.T) {
	dest := &fakeDestination{}
	records := mustClosedRecords(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil), DurableCommitID: "wal:0/20",
	})

	err := writeDurableKeylessCDCRecords(t.Context(), dest, records, destination.WriteOptions{Table: "raw.events"})
	require.ErrorContains(t, err, "durable keyless CDC writes requires destination support for idempotent commit-token writes")
	require.Empty(t, dest.writeCalls)
	drainAndRelease(records)
}

func TestMultiTableBatchAppendManagedCDCReplayUsesStableDataToken(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{
		cdcStateDestination: newCDCStateDestination(),
		failStateAfterData:  true,
	}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "multi-batch-append-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, "public.events", "raw.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))
	table := source.SourceTableInfo{
		Name: "public.events", Schema: keylessCDCSchema(), Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	table.Schema.PrimaryKeys = []string{"id"}
	table.PrimaryKeys = []string{"id"}
	newRecords := func(position string) <-chan source.RecordBatchResult {
		records := make(chan source.RecordBatchResult, 2)
		for i, batchID := range []string{"public.events:tx-1", "public.events:tx-2"} {
			records <- source.RecordBatchResult{
				TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{int64(i + 1)}, nil),
				CommitToken: source.CDCStateCommitToken{
					Position: position, DataBatchID: source.DurableID(batchID),
				},
			}
		}
		close(records)
		return records
	}
	newJob := func(position string) *MultiTableIngestionJob {
		return &MultiTableIngestionJob{
			Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyAppend, NoLoadTimestamp: true},
			Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: newRecords(position)},
			Destination: dest, Tables: []source.SourceTableInfo{table},
			TableDestNames:  map[string]string{table.Name: "raw.events"},
			CDCResumeLSNs:   map[string]string{table.Name: "0/10"},
			CDCStateManager: manager,
		}
	}

	firstJob := newJob("0/20")
	require.NoError(t, (&AppendStrategy{}).ExecuteMultiTable(ctx, firstJob))
	require.True(t, firstJob.Source.(*announcingMultiTableSource).readOpts.CDCStableDataBatches)
	require.ErrorContains(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/20"}), "injected state persistence failure")
	require.NoError(t, (&AppendStrategy{}).ExecuteMultiTable(ctx, newJob("0/25")))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/25"}))

	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 4)
	require.EqualValues(t, 2, dest.visibleRows)
	require.Equal(t, dest.dataWrites[0].CommitToken, dest.dataWrites[2].CommitToken)
	require.Equal(t, dest.dataWrites[1].CommitToken, dest.dataWrites[3].CommitToken)
	dest.dataMu.Unlock()
}

func TestMultiTableBatchMergeManagedKeylessCDCReplayUsesStableDataToken(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{
		cdcStateDestination: newCDCStateDestination(),
		failStateAfterData:  true,
	}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "multi-batch-merge-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, "public.events", "raw.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))
	table := source.SourceTableInfo{
		Name: "public.events", Schema: keylessCDCSchema(), Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	newRecords := func(position string) <-chan source.RecordBatchResult {
		records := make(chan source.RecordBatchResult, 2)
		for i, batchID := range []string{"public.events:tx-1", "public.events:tx-2"} {
			records <- source.RecordBatchResult{
				TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{int64(i + 1)}, nil),
				CommitToken: source.CDCStateCommitToken{
					Position: position, DataBatchID: source.DurableID(batchID),
				},
			}
		}
		close(records)
		return records
	}
	newJob := func(position string) *MultiTableIngestionJob {
		return &MultiTableIngestionJob{
			Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
			Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: newRecords(position)},
			Destination: dest, Tables: []source.SourceTableInfo{table},
			TableDestNames:  map[string]string{table.Name: "raw.events"},
			CDCResumeLSNs:   map[string]string{table.Name: "0/10"},
			CDCStateManager: manager,
		}
	}

	firstJob := newJob("0/20")
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, firstJob))
	require.True(t, firstJob.Source.(*announcingMultiTableSource).readOpts.CDCStableDataBatches)
	require.ErrorContains(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/20"}), "injected state persistence failure")
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, newJob("0/25")))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/25"}))

	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 4)
	require.EqualValues(t, 2, dest.visibleRows)
	require.Equal(t, dest.dataWrites[0].CommitToken, dest.dataWrites[2].CommitToken)
	require.Equal(t, dest.dataWrites[1].CommitToken, dest.dataWrites[3].CommitToken)
	require.False(t, dest.dataWrites[0].StagingTable)
	dest.dataMu.Unlock()
}

func TestAtomicMultiTableBatchMergeManagedKeylessCDCRequestsStableDataBatches(t *testing.T) {
	ctx := t.Context()
	base := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	base.incarnations["raw.events"] = "target-v1"
	dest := &managedAtomicKeylessDestination{managedKeylessDestination: base}
	manager, err := NewCDCStateManager(dest, "atomic-keyless-merge-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, "public.events", "raw.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))
	table := source.SourceTableInfo{
		Name: "public.events", Schema: keylessCDCSchema(), Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	newJob := func() *MultiTableIngestionJob {
		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{
			TableName: table.Name,
			Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
			CommitToken: source.CDCStateCommitToken{
				Position: "0/20", DataBatchID: "public.events:tx-1",
			},
		}
		close(records)
		return &MultiTableIngestionJob{
			Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
			Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
			Destination: dest, Tables: []source.SourceTableInfo{table},
			TableDestNames:  map[string]string{table.Name: "raw.events"},
			CDCResumeLSNs:   map[string]string{table.Name: "0/10"},
			CDCStateManager: manager,
		}
	}

	firstJob := newJob()
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, firstJob))
	require.True(t, firstJob.Source.(*announcingMultiTableSource).readOpts.CDCStableDataBatches)
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, newJob()))

	base.dataMu.Lock()
	require.Len(t, base.dataWrites, 2)
	require.EqualValues(t, 1, base.visibleRows)
	require.Equal(t, base.dataWrites[0].CommitToken, base.dataWrites[1].CommitToken)
	base.dataMu.Unlock()
}

func TestManagedAtomicAppendRejectsPublisherWithoutAbortBeforeMutation(t *testing.T) {
	base := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest := &managedAtomicKeylessDestination{managedKeylessDestination: base}
	manager, err := NewCDCStateManager(dest, "non-abortable-atomic-append", "raw.events", "")
	require.NoError(t, err)
	job, src, _ := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyAppend
	job.Config.SourceTable = "public.events"
	job.Config.DestTable = "raw.events"
	job.Schema = keylessCDCSchema()
	job.SourceSchema = job.Schema
	job.Destination = dest
	job.CDCStateManager = manager

	err = (&AppendStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "cannot safely recover an open managed atomic snapshot attempt")
	require.False(t, src.readCalled)
	require.Empty(t, base.prepareCalls)
	require.Empty(t, base.dataWrites)
}

func TestBatchAppendManagedCDCRejectsNonAtomicSnapshotBeforeTruncate(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest.incarnations["raw.events"] = "target-v1"
	opts := destination.WriteOptions{
		Table: "raw.events", SkipCDCResume: true, CDCExpectedIncarnation: "target-v1",
	}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true}
	records <- source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	}
	close(records)

	truncated, err := writeDurableCDCAppendRecords(ctx, dest, records, opts, "public.events", nil)
	require.False(t, truncated)
	require.ErrorContains(t, err, "require atomic snapshot publication")
	drainAndRelease(records)

	dest.dataMu.Lock()
	require.Empty(t, dest.dataWrites)
	require.Zero(t, dest.visibleRows)
	dest.dataMu.Unlock()
}

func TestAppendStrategyManagedCDCRejectsNonAtomicSnapshot(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "non-atomic-append", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, "public.events", "raw.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))

	job, src, _ := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyAppend
	job.Config.SourceTable = "public.events"
	job.Config.DestTable = "raw.events"
	job.Schema = keylessCDCSchema()
	job.Schema.PrimaryKeys = []string{"id"}
	job.SourceSchema = job.Schema
	job.Destination = dest
	job.CDCStateManager = manager
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Truncate: true}
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	src.readCh = records

	require.ErrorContains(t, (&AppendStrategy{}).Execute(ctx, job), "require atomic snapshot publication")
	require.True(t, src.readOpts.CDCStableDataBatches)
	dest.dataMu.Lock()
	require.Empty(t, dest.dataWrites)
	dest.dataMu.Unlock()
}

func TestMultiTableAppendStrategyManagedCDCRejectsNonAtomicSnapshot(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "non-atomic-multi-append", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, "public.events", "raw.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))
	table := source.SourceTableInfo{
		Name: "public.events", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"},
		Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	table.Schema.PrimaryKeys = []string{"id"}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
	records <- source.RecordBatchResult{
		TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	}
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyAppend, NoLoadTimestamp: true},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "raw.events"}, CDCStateManager: manager,
	}

	require.ErrorContains(t, (&AppendStrategy{}).ExecuteMultiTable(ctx, job), "require atomic snapshot publication")
	require.True(t, src.readOpts.CDCStableDataBatches)
	dest.dataMu.Lock()
	require.Empty(t, dest.dataWrites)
	dest.dataMu.Unlock()
}

func TestBatchAppendManagedCDCWALTruncateStillRequiresTypedSourcePosition(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "wal-truncate-missing-id", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "source-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true, CDCWALTruncate: true}
	records <- source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	}
	close(records)

	truncated, err := writeDurableCDCAppendRecords(ctx, dest, records, destination.WriteOptions{
		Table: "raw.events", SkipCDCResume: true, CDCExpectedIncarnation: "target-v1",
	}, "public.events", manager)
	require.True(t, truncated)
	require.ErrorContains(t, err, "has no typed source position")
}

func TestBatchAppendManagedCDCWALTruncateReplayReappliesPostTruncateRows(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination(), visibleRows: 7}
	dest.incarnations["raw.events"] = "target-v1"
	manager, err := NewCDCStateManager(dest, "wal-truncate-replay", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "source-v1"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))

	newRecords := func() <-chan source.RecordBatchResult {
		records := make(chan source.RecordBatchResult, 2)
		records <- source.RecordBatchResult{
			TableName: "public.events", Truncate: true, CDCWALTruncate: true,
		}
		records <- source.RecordBatchResult{
			TableName: "public.events",
			Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
			CommitToken: source.CDCStateCommitToken{
				Position: "0/20", DataBatchID: "public.events:0/20",
			},
		}
		close(records)
		return records
	}
	opts := destination.WriteOptions{
		Table: "raw.events", SkipCDCResume: true, CDCExpectedIncarnation: "target-v1",
	}

	truncated, err := writeDurableCDCAppendRecords(ctx, dest, newRecords(), opts, "public.events", manager)
	require.NoError(t, err)
	require.True(t, truncated)
	truncated, err = writeDurableCDCAppendRecords(ctx, dest, newRecords(), opts, "public.events", manager)
	require.NoError(t, err)
	require.True(t, truncated)

	dest.dataMu.Lock()
	require.Equal(t, 2, dest.truncates)
	require.EqualValues(t, 1, dest.visibleRows)
	require.Len(t, dest.dataWrites, 2)
	require.Empty(t, dest.dataWrites[0].CommitToken)
	require.Empty(t, dest.dataWrites[1].CommitToken)
	dest.dataMu.Unlock()
}

func TestBatchAppendManagedCDCWALTruncateCompletesNewSnapshotBoundary(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest.incarnations["raw.events"] = "target-v1"

	first, err := NewCDCStateManager(dest, "wal-truncate-completion", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableIncarnation(ctx, "public.events", "raw.events", "source-v1"))
	require.NoError(t, first.BeginRun(ctx, false))
	require.NoError(t, first.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	require.NoError(t, first.Persist(ctx, source.CDCStateCommitToken{
		Position:             "0/10",
		SnapshotPositions:    map[string]string{"public.events": "0/10"},
		SnapshotIncarnations: map[string]string{"public.events": "source-v1"},
	}))

	manager, err := NewCDCStateManager(dest, "wal-truncate-completion", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "source-v1"))
	position, err := manager.ResumePosition(ctx, "public.events")
	require.NoError(t, err)
	require.Equal(t, "0/10", position)
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{
		TableName: "public.events", Truncate: true, CDCWALTruncate: true,
	}
	records <- source.RecordBatchResult{
		TableName: "public.events",
		Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: source.CDCStateCommitToken{
			Position: "0/20", DataBatchID: "public.events:0/20",
		},
	}
	close(records)
	truncated, err := writeDurableCDCAppendRecords(ctx, dest, records, destination.WriteOptions{
		Table: "raw.events", SkipCDCResume: true, CDCExpectedIncarnation: "target-v1",
	}, "public.events", manager)
	require.NoError(t, err)
	require.True(t, truncated)
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{Position: "0/20"}))

	restarted, err := NewCDCStateManager(dest, "wal-truncate-completion", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, "public.events", "raw.events", "source-v1"))
	position, err = restarted.ResumePosition(ctx, "public.events")
	require.NoError(t, err)
	require.Equal(t, "0/20", position)
}

func TestStreamingManagedKeylessCDCRejectsEmptyDestinationIncarnation(t *testing.T) {
	ctx := t.Context()
	base := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest := &emptyIncarnationManagedKeylessDestination{managedKeylessDestination: base}
	manager, err := NewCDCStateManager(dest, "managed-keyless-empty-incarnation", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	st := &streamTableState{
		destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true,
		incarnation: "100", schemaFingerprint: "schema-v1",
	}
	cfg := config.DefaultConfig()
	cfg.NoLoadTimestamp = true
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge, StateManager: manager,
	}, map[string]*streamTableState{"public.events": st})
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: "public.events",
		Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: source.CDCStateCommitToken{
			Position: "0/100", DataBatchID: "public.events:1:0/110/2:0/110/2",
		},
	}
	close(records)

	require.ErrorContains(t, loop.run(ctx, records), "has no previously verified physical incarnation")
	base.dataMu.Lock()
	require.Empty(t, base.dataWrites)
	base.dataMu.Unlock()
}

func TestStreamingManagedKeylessCDCRejectsTargetRecreatedBetweenBoundaries(t *testing.T) {
	ctx := t.Context()
	base := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest := &replacingManagedKeylessDestination{managedKeylessDestination: base}
	manager, err := NewCDCStateManager(dest, "managed-keyless-recreated", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	st := &streamTableState{
		destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true,
		incarnation: "100", schemaFingerprint: "schema-v1",
	}
	cfg := config.DefaultConfig()
	cfg.NoLoadTimestamp = true
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge, StateManager: manager,
	}, map[string]*streamTableState{"public.events": st})
	records := make(chan source.RecordBatchResult, 2)
	for i, batchID := range []string{"public.events:1:0/110/2:0/110/2", "public.events:1:0/120/2:0/120/2"} {
		records <- source.RecordBatchResult{
			TableName: "public.events",
			Batch:     int64RecordBatch(t, "id", []int64{int64(i + 1)}, nil),
			CommitToken: source.CDCStateCommitToken{
				Position: "0/100", DataBatchID: source.DurableID(batchID),
			},
		}
	}
	close(records)

	require.ErrorContains(t, loop.run(ctx, records), "destination was replaced before managed keyless write")
	base.dataMu.Lock()
	require.Len(t, base.dataWrites, 1)
	require.Equal(t, "incarnation:raw.events", base.dataWrites[0].CDCExpectedIncarnation)
	require.EqualValues(t, 1, base.visibleRows)
	base.dataMu.Unlock()
}

func TestStreamingManagedKeylessCDCRejectsTargetRecreatedBetweenFlushes(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "managed-keyless-between-flushes", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	newLoop := func() *flushLoop {
		cfg := config.DefaultConfig()
		cfg.NoLoadTimestamp = true
		return newFlushLoop(dest, cfg, StreamingOptions{
			FlushInterval: time.Hour, FlushRecords: 1, Strategy: config.StrategyMerge, StateManager: manager,
		}, map[string]*streamTableState{"public.events": {
			destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true,
			incarnation: "100", schemaFingerprint: "schema-v1",
		}})
	}
	records := func(batchID string, value int64) <-chan source.RecordBatchResult {
		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{
			TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{value}, nil),
			CommitToken: source.CDCStateCommitToken{Position: "0/100", DataBatchID: source.DurableID(batchID)},
		}
		close(ch)
		return ch
	}
	require.NoError(t, newLoop().run(ctx, records("public.events:0/110", 1)))
	dest.stateMu.Lock()
	dest.incarnations["raw.events"] = "replacement-incarnation"
	dest.stateMu.Unlock()

	require.ErrorContains(t, newLoop().run(ctx, records("public.events:0/120", 2)), "was replaced after its prior snapshot boundary")
	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 1)
	require.EqualValues(t, 1, dest.visibleRows)
	dest.dataMu.Unlock()
}

func TestStreamingManagedKeylessCDCInvalidationRaceReleasesAllMovedBatchesOnce(t *testing.T) {
	ctx := t.Context()
	base := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	dest := &recreateDuringInvalidationManagedKeylessDestination{managedKeylessDestination: base}
	manager, err := NewCDCStateManager(dest, "managed-keyless-invalidation-race", "raw.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.events", "raw.events", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.events", "raw.events"))
	dest.recreateMu.Lock()
	dest.armed = true
	dest.recreateMu.Unlock()
	cfg := config.DefaultConfig()
	cfg.NoLoadTimestamp = true
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge, StateManager: manager,
	}, map[string]*streamTableState{"public.events": {
		destTable: "raw.events", schema: keylessCDCSchema(), isCDC: true,
		incarnation: "100", schemaFingerprint: "schema-v1",
	}})
	batch := &singleReleaseCountingRecordBatch{RecordBatch: int64RecordBatch(t, "id", []int64{1}, nil)}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: "public.events", Batch: batch,
		CommitToken: source.CDCStateCommitToken{Position: "0/100", DataBatchID: "public.events:0/110"},
	}
	close(records)

	require.ErrorContains(t, loop.run(ctx, records), "changed while invalidating its prior snapshot boundary")
	require.EqualValues(t, 1, batch.releases.Load())
	base.dataMu.Lock()
	require.Empty(t, base.dataWrites)
	base.dataMu.Unlock()
}

func TestBatchManagedCDCStagingSkipsDestinationResumeAuthority(t *testing.T) {
	ctx := t.Context()
	dest := &managedKeylessDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "managed-staging", "raw.events", "")
	require.NoError(t, err)
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.CDCResumeLSN = "0/100"
	job.CDCStateManager = manager
	require.NoError(t, manager.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	})

	require.NoError(t, (&MergeStrategy{}).Execute(ctx, job))
	dest.dataMu.Lock()
	require.Len(t, dest.dataWrites, 1)
	require.True(t, dest.dataWrites[0].SkipCDCResume)
	dest.dataMu.Unlock()
	dest.mu.Lock()
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].SkipCDCResume)
	dest.mu.Unlock()
}

func TestStreaming_PrepareTableRejectsUnsafeKeylessCDCDestination(t *testing.T) {
	dest := &fakeDestination{}
	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge})
	st := &streamTableState{
		destTable: "events",
		schema:    keylessCDCSchema(),
		isCDC:     true,
	}

	err := exec.prepareTable(context.Background(), dest, &config.IngestConfig{}, st)
	require.ErrorContains(t, err, "requires destination support for idempotent commit-token writes")

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Empty(t, dest.prepareCalls, "unsafe destination must fail before creating a table")
}

func TestStreamingSchemaRefreshRejectsPrimaryKeyModeChangeWithoutSnapshot(t *testing.T) {
	base := &fakeDestination{}
	dest := &idempotentCommitTokenDestination{fakeDestination: base}
	st := &streamTableState{
		destTable: "events", stagingTable: "events_staging", schema: keylessCDCSchema(),
		primaryKeys: []string{"id"}, isCDC: true,
	}
	st.schema.PrimaryKeys = []string{"id"}
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})
	loop.cfg.NoLoadTimestamp = true
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error { return nil }
	refreshed := *st.schema
	refreshed.PrimaryKeys = nil

	err := loop.refreshTableSchema(context.Background(), source.SourceTableInfo{
		Name: "public.events", Schema: &refreshed, PrimaryKeys: nil,
	}, st)
	require.ErrorContains(t, err, "requires a new snapshot")
	require.Equal(t, []string{"id"}, st.primaryKeys)
	require.Equal(t, "events_staging", st.stagingTable)

	base.mu.Lock()
	defer base.mu.Unlock()
	require.Empty(t, base.dropCalls)
	require.Empty(t, base.prepareCalls)
}
