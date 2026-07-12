package strategy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type atomicSnapshotDestination struct {
	*fakeDestination
	atomicMu                sync.Mutex
	begins                  []destination.AtomicSnapshotOptions
	writes                  []destination.WriteOptions
	publishes               []destination.AtomicSnapshotOptions
	evolves                 []destination.AtomicSnapshotOptions
	aborts                  []destination.AtomicSnapshotOptions
	beginErr                error
	writeErr                error
	preparedOwnershipTokens []string
	ownedDropTokens         []string
	ownedDropErr            error
	mergeErr                error
	mergeRows               []int64
	mergeNulls              []int
	writeTypes              []arrow.DataType
	writeNulls              []int
	writeRows               []int64
}

type unownedAtomicSnapshotDestination struct {
	*fakeDestination
	beginErr error
}

type immediateFailAtomicMultiDestination struct{ *atomicSnapshotDestination }

type failNthPublishAtomicDestination struct {
	*atomicSnapshotDestination
	failAt int
	calls  int
}

type lateDiscoveryFilteringSource struct {
	*announcingMultiTableSource
	late         source.SourceTableInfo
	includedLate bool
}

func (s *lateDiscoveryFilteringSource) ReadAll(_ context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	s.readOpts = opts
	known := make(map[string]struct{}, len(opts.KnownTables))
	for _, table := range opts.KnownTables {
		known[table] = struct{}{}
	}
	records := make(chan source.RecordBatchResult, 2)
	for _, table := range s.tables {
		records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
	}
	if _, frozen := known[s.late.Name]; !frozen && len(opts.KnownTables) == 0 {
		s.includedLate = true
		records <- source.RecordBatchResult{TableName: s.late.Name, Truncate: true}
	}
	close(records)
	return records, nil
}

func (d *failNthPublishAtomicDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.calls++
	if d.calls == d.failAt {
		return errors.New("injected multi-table publish failure")
	}
	return d.atomicSnapshotDestination.PublishAtomicSnapshot(context.Background(), opts)
}

func (*immediateFailAtomicMultiDestination) MergeRecords(
	context.Context,
	<-chan source.RecordBatchResult,
	destination.WriteOptions,
	destination.MergeOptions,
) error {
	return errors.New("direct worker failed before draining")
}

func (d *unownedAtomicSnapshotDestination) BeginAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) error {
	return d.beginErr
}

func (*unownedAtomicSnapshotDestination) EvolveAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) error {
	return nil
}

func (*unownedAtomicSnapshotDestination) WriteAtomicSnapshot(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
	drainAndRelease(records)
	return nil
}

func (*unownedAtomicSnapshotDestination) PublishAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) error {
	return nil
}

func (*unownedAtomicSnapshotDestination) MergeRecords(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions, _ destination.MergeOptions) error {
	drainAndRelease(records)
	return nil
}

type snapshotReplacementTrackingDestination struct {
	*atomicSnapshotDestination
	rowsMu  sync.Mutex
	visible map[int64]struct{}
	staged  map[string][]int64
}

func (d *snapshotReplacementTrackingDestination) WriteAtomicSnapshot(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	var rows []int64
	for result := range records {
		if result.Batch != nil {
			values := result.Batch.Column(0).(*array.Int64)
			for i := 0; i < values.Len(); i++ {
				if !values.IsNull(i) {
					rows = append(rows, values.Value(i))
				}
			}
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	d.rowsMu.Lock()
	d.staged[opts.AtomicSnapshotAttemptID] = rows
	d.rowsMu.Unlock()
	return nil
}

func (d *snapshotReplacementTrackingDestination) PublishAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) error {
	if err := d.atomicSnapshotDestination.PublishAtomicSnapshot(ctx, opts); err != nil {
		return err
	}
	d.rowsMu.Lock()
	d.visible = make(map[int64]struct{}, len(d.staged[opts.AttemptID]))
	for _, id := range d.staged[opts.AttemptID] {
		d.visible[id] = struct{}{}
	}
	d.rowsMu.Unlock()
	return nil
}

type incarnationTrackingAtomicSnapshotDestination struct {
	*cdcStateDestination
	publishedIncarnation string
	incarnationMu        sync.Mutex
	incarnationReads     int
	begunOpts            []destination.AtomicSnapshotOptions
	publishedOpts        []destination.AtomicSnapshotOptions
}

func (*incarnationTrackingAtomicSnapshotDestination) MergeRecords(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	_ destination.WriteOptions,
	_ destination.MergeOptions,
) error {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) BeginAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.incarnationMu.Lock()
	d.begunOpts = append(d.begunOpts, opts)
	d.incarnationMu.Unlock()
	return nil
}

func (*incarnationTrackingAtomicSnapshotDestination) EvolveAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) error {
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	incarnation, exists, err := d.cdcStateDestination.CDCTargetIncarnation(ctx, table)
	d.incarnationMu.Lock()
	d.incarnationReads++
	d.incarnationMu.Unlock()
	return incarnation, exists, err
}

func (*incarnationTrackingAtomicSnapshotDestination) WriteAtomicSnapshot(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	_ destination.WriteOptions,
) error {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.incarnationMu.Lock()
	d.publishedOpts = append(d.publishedOpts, opts)
	d.incarnationMu.Unlock()
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.incarnations[opts.Table] = d.publishedIncarnation
	return nil
}

func (d *atomicSnapshotDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.atomicMu.Lock()
	d.preparedOwnershipTokens = append(d.preparedOwnershipTokens, opts.OwnershipToken)
	d.atomicMu.Unlock()
	return d.fakeDestination.PrepareTable(ctx, opts)
}

func (d *atomicSnapshotDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.ownedDropTokens = append(d.ownedDropTokens, token)
	return d.ownedDropErr
}

func (*atomicSnapshotDestination) CDCTargetIncarnation(context.Context, string) (string, bool, error) {
	return "target-v1", true, nil
}

func (d *atomicSnapshotDestination) AbortAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.aborts = append(d.aborts, opts)
	return nil
}

func (d *atomicSnapshotDestination) MergeRecords(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	_ destination.WriteOptions,
	opts destination.MergeOptions,
) error {
	var rows int64
	var nulls int
	for result := range records {
		if result.Batch != nil {
			rows += result.Batch.NumRows()
			if result.Batch.NumCols() > 1 {
				nulls += result.Batch.Column(1).NullN()
			}
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	d.mu.Lock()
	d.mergeCalls = append(d.mergeCalls, opts)
	d.mu.Unlock()
	d.atomicMu.Lock()
	d.mergeRows = append(d.mergeRows, rows)
	d.mergeNulls = append(d.mergeNulls, nulls)
	d.atomicMu.Unlock()
	return d.mergeErr
}

type snapshotResetSourceTable struct{ *fakeSourceTable }

func (*snapshotResetSourceTable) EmitsSnapshotResets() bool { return true }

type snapshotResetMultiSource struct{ *announcingMultiTableSource }

func (*snapshotResetMultiSource) EmitsSnapshotResets() bool { return true }

type truncateCapableFakeDestination struct{ *fakeDestination }

func (d *truncateCapableFakeDestination) TruncateTable(_ context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.truncateCalls = append(d.truncateCalls, table)
	return d.truncateErr
}

func snapshotStateToken(table, position string) source.CDCStateCommitToken {
	return source.CDCStateCommitToken{SnapshotPositions: map[string]string{table: position}}
}

func testCDCSchema(base *schema.TableSchema) *schema.TableSchema {
	result := *base
	result.Columns = append([]schema.Column(nil), base.Columns...)
	result.PrimaryKeys = append([]string(nil), base.PrimaryKeys...)
	result.Columns = append(
		result.Columns,
		schema.Column{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		schema.Column{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
	)
	return &result
}

func (l *flushLoop) handleSnapshotReset(ctx context.Context, table string) error {
	return l.handleResult(ctx, source.RecordBatchResult{TableName: table, Truncate: true})
}

func (d *atomicSnapshotDestination) BeginAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.begins = append(d.begins, opts)
	return d.beginErr
}

func TestStreamingAtomicSnapshotBeginFailureAbortsOwnedAttempt(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}, beginErr: errors.New("begin failed after stage creation")}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})

	err := loop.handleSnapshotReset(context.Background(), "public.events")
	require.Error(t, err)
	loop.cleanup(context.Background())
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.begins, 1)
	require.Len(t, dest.aborts, 1)
	require.Equal(t, dest.begins[0].AttemptID, dest.aborts[0].AttemptID)
}

func TestStreamingAppendAtomicPreparationDoesNotMutateExistingTarget(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"lake.events": tableSchema},
	}}
	st := &streamTableState{destTable: "lake.events", schema: tableSchema, isCDC: true}
	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyAppend})

	require.NoError(t, executor.prepareTable(t.Context(), dest, &config.IngestConfig{}, st))
	require.Empty(t, dest.prepareCalls)
	require.False(t, st.targetCreatedByRun)
}

func TestStreamingAppendAtomicPreparationCleansOwnedNewTargetOnFailure(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := &streamTableState{destTable: "lake.events", schema: tableSchema, isCDC: true}
	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyAppend})

	require.NoError(t, executor.prepareTable(t.Context(), dest, &config.IngestConfig{}, st))
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyAppend}, map[string]*streamTableState{"public.events": st})
	loop.cleanup(t.Context())

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 1)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
}

func TestStreamingRepeatedSnapshotResetAbortsPreviousAttempt(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})

	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))
	firstAttempt := st.snapshotAttemptID
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))
	require.NotEqual(t, firstAttempt, st.snapshotAttemptID)
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.aborts, 1)
	require.Equal(t, firstAttempt, dest.aborts[0].AttemptID)
	require.Len(t, dest.begins, 2)
}

func TestStreamingAtomicRefreshRebuildsDiscardValueTransformer(t *testing.T) {
	current := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"lake.events": current},
	}}
	st := &streamTableState{destTable: "lake.events", schema: current, primaryKeys: []string{"id"}, isCDC: true}
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})
	loop.cfg.NoLoadTimestamp = true
	loop.cfg.SchemaContract = "discard_value"
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error { return nil }
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))

	refreshed := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	require.NoError(t, loop.refreshTableSchema(context.Background(), source.SourceTableInfo{
		Name: "public.events", Schema: refreshed, PrimaryKeys: []string{"id"},
	}, st))
	require.NotNil(t, st.schemaAligner)
	require.True(t, arrow.TypeEqual(arrow.PrimitiveTypes.Int64, st.schemaAligner.OutputSchema(nil).Field(1).Type))
	id := array.NewInt64Builder(memory.DefaultAllocator)
	value := array.NewStringBuilder(memory.DefaultAllocator)
	id.Append(1)
	value.Append("not-an-integer")
	idArray, valueArray := id.NewArray(), value.NewArray()
	id.Release()
	value.Release()
	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "value", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil), []arrow.Array{idArray, valueArray}, 1)
	idArray.Release()
	valueArray.Release()
	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: batch,
		CommitToken: snapshotStateToken("public.events", "0/100"),
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.writeTypes, 1)
	require.Truef(t, arrow.TypeEqual(arrow.PrimitiveTypes.Int64, dest.writeTypes[0]), "got type %s", dest.writeTypes[0])
	require.Equal(t, []int{1}, dest.writeNulls)
	require.Equal(t, schema.TypeInt64, dest.writes[0].Schema.Columns[1].DataType)
}

func TestBatchSnapshotMergePreservesTargetOnSourceFailure(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	snapshotSource := &snapshotResetSourceTable{fakeSourceTable: src}
	job.Table = snapshotSource
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Truncate: true}
	records <- source.RecordBatchResult{Err: errors.New("snapshot extraction failed")}
	close(records)
	src.readCh = records

	err := (&MergeStrategy{}).Execute(context.Background(), job)
	require.Error(t, err)
	require.Empty(t, dest.truncateCalls)
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Empty(t, dest.publishes)
	require.Len(t, dest.aborts, 1)
}

func TestBatchSnapshotFailureConditionallyRemovesOnlyOwnedNewTarget(t *testing.T) {
	dest := &atomicSnapshotDestination{
		fakeDestination: &fakeDestination{},
		writeErr:        errors.New("snapshot staging failed after replacement owner appeared"),
		ownedDropErr:    errors.New("ownership changed"),
	}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
	)

	err := (&MergeStrategy{}).Execute(context.Background(), job)
	require.Error(t, err)
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 1)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
	require.Empty(t, dest.dropCalls, "unconditional cleanup must not delete a replacement owner")
	require.Empty(t, dest.publishes)
	require.Len(t, dest.aborts, 1)
}

func TestBatchSnapshotLostPrepareResponseCleansOwnedTarget(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		prepareHook: func(destination.PrepareOptions) error { return errors.New("prepare response lost after create") },
	}}
	job, _, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: job.Table.(*fakeSourceTable)}

	require.ErrorContains(t, (&MergeStrategy{}).Execute(context.Background(), job), "prepare response lost")
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 1)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
}

func TestBatchEmptySnapshotPublishesBatchlessDurablePosition(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Truncate: true}
	records <- source.RecordBatchResult{DurableCommitID: "snapshot:empty", DurableCommitPosition: "0/900"}
	close(records)
	src.readCh = records

	require.NoError(t, (&MergeStrategy{}).Execute(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "snapshot:empty", dest.publishes[0].CommitToken)
	require.Equal(t, "0/900", dest.publishes[0].CDCResumeLSN)
}

func TestAtomicSnapshotBeginFailureDrainsUnreadArrowBatches(t *testing.T) {
	for _, strategyName := range []string{"merge", "append"} {
		t.Run(strategyName, func(t *testing.T) {
			pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
			previousAllocator := memory.DefaultAllocator
			memory.DefaultAllocator = pool
			t.Cleanup(func() {
				memory.DefaultAllocator = previousAllocator
				pool.AssertSize(t, 0)
			})

			dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}, beginErr: errors.New("begin failed")}
			job, src, _ := minimalJob()
			job.Destination = dest
			job.Schema = testCDCSchema(job.Schema)
			job.SourceSchema = job.Schema
			job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
			src.readCh = mustClosedRecords(
				source.RecordBatchResult{Truncate: true},
				source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
			)

			var err error
			if strategyName == "merge" {
				err = (&MergeStrategy{}).Execute(t.Context(), job)
			} else {
				job.CDCStateManager = &CDCStateManager{
					dest: dest, incarnation: dest,
					destTables:          map[string]string{job.Config.SourceTable: job.Config.DestTable},
					currentIncarnations: map[string]string{}, currentSchemas: map[string]string{},
					boundDestinations: map[string]string{}, boundDestinationRaw: map[string]string{},
				}
				err = (&AppendStrategy{}).executeAtomicSnapshot(t.Context(), job, dest, destination.DestinationTableSchema(job.Schema))
			}
			require.ErrorContains(t, err, "failed to begin")
			if strategyName == "append" {
				dest.atomicMu.Lock()
				require.Len(t, dest.preparedOwnershipTokens, 1)
				require.NotEmpty(t, dest.preparedOwnershipTokens[0])
				require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
				dest.atomicMu.Unlock()
			}
		})
	}
}

func TestBatchAtomicSnapshotBindsPublishedDestinationIncarnation(t *testing.T) {
	stateDest := newCDCStateDestination()
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "destination-before-publication",
	}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before-publication"
	manager, err := NewCDCStateManager(dest, "batch-published-incarnation", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-incarnation"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job.CDCStateManager = manager
	dest.incarnationMu.Lock()
	readsBeforeExecute := dest.incarnationReads
	dest.incarnationMu.Unlock()
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:empty", DurableCommitPosition: "0/900"},
	)

	require.NoError(t, (&MergeStrategy{}).Execute(t.Context(), job))
	require.Equal(
		t,
		cdcDestinationIncarnationDigest(dest.publishedIncarnation),
		manager.boundDestinations[job.Config.SourceTable],
	)
	dest.incarnationMu.Lock()
	readsAfterExecute := dest.incarnationReads
	require.Len(t, dest.publishedOpts, 1)
	require.True(t, dest.publishedOpts[0].SkipCDCResume)
	dest.incarnationMu.Unlock()
	require.Equal(t, readsBeforeExecute+2, readsAfterExecute, "atomic batch snapshot must fence before publication and rebind after it")
}

func TestManagedAppendSnapshotPublishesAtomically(t *testing.T) {
	stateDest := newCDCStateDestination()
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "target-v1",
	}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.SourceSchema = job.Schema
	stateDest.incarnations[job.Config.DestTable] = "target-v1"
	manager, err := NewCDCStateManager(dest, "managed-append-atomic", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job.CDCStateManager = manager
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{
			Batch:           int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "snapshot:append", DurableCommitPosition: "0/900",
		},
	)

	require.NoError(t, (&AppendStrategy{}).Execute(t.Context(), job))
	require.True(t, src.readOpts.CDCSnapshotReplace)
	for _, prepare := range stateDest.prepareCalls {
		require.NotEqual(t, job.Config.DestTable, prepare.Table, "existing atomic append target must not be prepared before publication")
	}
	dest.incarnationMu.Lock()
	require.Len(t, dest.publishedOpts, 1)
	require.Equal(t, "snapshot:append", dest.publishedOpts[0].CommitToken)
	require.Equal(t, "0/900", dest.publishedOpts[0].CDCResumeLSN)
	require.True(t, dest.publishedOpts[0].SkipCDCResume)
	require.Equal(t, "target-v1", dest.publishedOpts[0].CDCExpectedIncarnation)
	dest.incarnationMu.Unlock()
}

func TestManagedMultiTableAppendSnapshotPublishesAtomically(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	src := &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "snapshot:multi-append", DurableCommitPosition: "0/901",
		}),
	}}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-v1"
	dest := &incarnationTrackingAtomicSnapshotDestination{cdcStateDestination: stateDest, publishedIncarnation: "target-v1"}
	manager, err := NewCDCStateManager(dest, "managed-multi-append-atomic", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, "lake.events", "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyAppend, NoLoadTimestamp: true},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"}, CDCStateManager: manager,
	}

	require.NoError(t, (&AppendStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.True(t, src.readOpts.CDCStableDataBatches)
	stateDest.mu.Lock()
	for _, prepare := range stateDest.prepareCalls {
		require.NotEqual(t, "lake.events", prepare.Table, "existing atomic multi-table append target must not be prepared before publication")
	}
	stateDest.mu.Unlock()
	dest.incarnationMu.Lock()
	require.Len(t, dest.begunOpts, 1)
	require.Contains(t, dest.begunOpts[0].TargetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
	require.Len(t, dest.publishedOpts, 1)
	require.Equal(t, "snapshot:multi-append", dest.publishedOpts[0].CommitToken)
	require.Empty(t, dest.publishedOpts[0].PrimaryKeys, "append snapshots must not acquire merge semantics")
	require.Equal(t, "target-v1", dest.publishedOpts[0].CDCExpectedIncarnation)
	require.Contains(t, dest.publishedOpts[0].TargetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
	dest.incarnationMu.Unlock()
}

func TestBatchCDCResumeWithoutResetUsesDirectMerge(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.CDCResumeLSN = "0/900"
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch:           int64RecordBatch(t, "id", []int64{4}, nil),
		DurableCommitID: "wal:resume", DurableCommitPosition: "0/901",
	})

	require.NoError(t, (&MergeStrategy{}).Execute(context.Background(), job))
	dest.atomicMu.Lock()
	require.Empty(t, dest.begins)
	require.Empty(t, dest.publishes)
	dest.atomicMu.Unlock()
	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.mergeCalls, 1)
}

func TestBatchCDCResumeLostMergeResponsePreservesOwnedTarget(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{mergeErr: errors.New("merge response lost after commit")}}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.CDCResumeLSN = "0/900"
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{4}, nil), DurableCommitID: "wal:resume", DurableCommitPosition: "0/901",
	})

	require.ErrorContains(t, (&MergeStrategy{}).Execute(context.Background(), job), "merge response lost")
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.mergeCalls, 1)
	require.Empty(t, dest.ownedDropTokens, "a merge error may be a lost response after a durable commit")
}

func TestAtomicMultiTableInitialCDCSnapshotWithoutBoundaryReplacesStaleRows(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	dest := &snapshotReplacementTrackingDestination{
		atomicSnapshotDestination: &atomicSnapshotDestination{fakeDestination: &fakeDestination{
			tableSchemas: map[string]*schema.TableSchema{"lake.events": destination.DestinationTableSchema(tableSchema)},
		}},
		visible: map[int64]struct{}{99: {}},
		staged:  make(map[string][]int64),
	}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest,
		Tables:      []source.SourceTableInfo{table},
		TableDestNames: map[string]string{
			table.Name: "lake.events",
		},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job))
	dest.rowsMu.Lock()
	require.Equal(t, map[int64]struct{}{1: {}}, dest.visible)
	dest.rowsMu.Unlock()
	dest.atomicMu.Lock()
	require.Len(t, dest.begins, 1)
	require.Len(t, dest.publishes, 1)
	require.Empty(t, dest.mergeRows)
	dest.atomicMu.Unlock()
}

func TestAtomicMultiTableManagedSnapshotAndReplaySkipDestinationCursor(t *testing.T) {
	t.Run("snapshot", func(t *testing.T) {
		tableSchema := testCDCSchema(streamTestSchema())
		table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}}
		records := mustClosedRecords(source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		})
		dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
		job := &MultiTableIngestionJob{
			Config: &config.IngestConfig{NoLoadTimestamp: true}, Source: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
			Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "lake.events"},
			CDCStateManager: &CDCStateManager{},
		}

		require.NoError(t, (&MergeStrategy{}).executeAtomicMultiTableBatch(t.Context(), job, dest, dest))
		dest.atomicMu.Lock()
		require.Len(t, dest.writes, 1)
		require.True(t, dest.writes[0].SkipCDCResume)
		require.Len(t, dest.publishes, 1)
		require.True(t, dest.publishes[0].SkipCDCResume)
		dest.atomicMu.Unlock()
	})

	t.Run("incremental_replay", func(t *testing.T) {
		tableSchema := testCDCSchema(streamTestSchema())
		table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}}
		records := mustClosedRecords(source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		})
		dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
		job := &MultiTableIngestionJob{
			Config: &config.IngestConfig{NoLoadTimestamp: true}, Source: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
			Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "lake.events"},
			CDCResumeLSNs: map[string]string{table.Name: "0/100"}, CDCStateManager: &CDCStateManager{},
		}

		require.NoError(t, (&MergeStrategy{}).executeAtomicMultiTableBatch(t.Context(), job, dest, dest))
		dest.mu.Lock()
		require.Len(t, dest.mergeCalls, 1)
		require.True(t, dest.mergeCalls[0].SkipCDCResume)
		dest.mu.Unlock()
	})
}

func TestManagedMultiTableInitialSnapshotUsesAtomicPublicationAndLegacySlot(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	src := &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		}),
	}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-v1"
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "target-v1",
	}
	manager, err := NewCDCStateManager(dest, "managed-multi-atomic", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, "lake.events", "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{
			IncrementalStrategy: config.StrategyMerge,
			NoLoadTimestamp:     true,
			CDCSlotSuffix:       "current-slot",
			CDCLegacySlotSuffix: "legacy-slot",
		},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames:  map[string]string{table.Name: "lake.events"},
		CDCStateManager: manager,
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.Equal(t, "current-slot", src.readOpts.CDCSlotSuffix)
	require.Equal(t, "legacy-slot", src.readOpts.CDCLegacySlotSuffix)
	dest.incarnationMu.Lock()
	require.Len(t, dest.publishedOpts, 1)
	require.True(t, dest.publishedOpts[0].SkipCDCResume)
	require.Equal(t, "target-v1", dest.publishedOpts[0].CDCExpectedIncarnation)
	dest.incarnationMu.Unlock()
	require.Equal(t, "target-v1", manager.BoundDestinationIncarnation(table.Name))
}

func TestAtomicCapableMultiTableResumeAppliesSchemaContract(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		wantRows  int64
		wantNulls int
	}{
		{mode: "discard_value", wantRows: 1, wantNulls: 1},
		{mode: "discard_row", wantRows: 0, wantNulls: 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			sourceSchema := &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "value", DataType: schema.TypeString, Nullable: true},
			}, PrimaryKeys: []string{"id"}}
			finalSchema := &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "value", DataType: schema.TypeInt64, Nullable: true},
			}, PrimaryKeys: []string{"id"}}
			comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
				Type: schemaevolution.ChangeWidenType, ColumnName: "value", ColumnPath: []string{"value"},
			}}}
			ids := array.NewInt64Builder(memory.DefaultAllocator)
			values := array.NewStringBuilder(memory.DefaultAllocator)
			ids.Append(1)
			values.Append("invalid")
			idArray, valueArray := ids.NewArray(), values.NewArray()
			ids.Release()
			values.Release()
			batch := array.NewRecordBatch(sourceSchema.ToArrowSchema(), []arrow.Array{idArray, valueArray}, 1)
			idArray.Release()
			valueArray.Release()
			table := source.SourceTableInfo{Name: "public.events", Schema: sourceSchema, PrimaryKeys: []string{"id"}}
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{TableName: table.Name, Batch: batch}
			close(records)
			dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
			job := &MultiTableIngestionJob{
				Config:      &config.IngestConfig{SchemaContract: tc.mode, NoLoadTimestamp: true},
				Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
				Destination: dest, Tables: []source.SourceTableInfo{table},
				TableDestNames: map[string]string{table.Name: "lake.events"},
				CDCResumeLSNs:  map[string]string{table.Name: "0/10"},
				EvolutionPlans: map[string]*schemaevolution.EvolutionPlan{
					table.Name: {TransformComparison: comparison, FinalSchema: finalSchema},
				},
			}

			executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 100})
			require.NoError(t, executor.ExecuteMultiTable(context.Background(), job))
			dest.atomicMu.Lock()
			defer dest.atomicMu.Unlock()
			if tc.wantRows == 0 {
				require.Empty(t, dest.mergeRows)
				require.Empty(t, dest.mergeNulls)
			} else {
				require.Equal(t, []int64{tc.wantRows}, dest.mergeRows)
				require.Equal(t, []int{tc.wantNulls}, dest.mergeNulls)
			}
			require.Empty(t, dest.publishes)
		})
	}
}

func TestAtomicCapableMultiTableSnapshotAppliesDiscardRowContract(t *testing.T) {
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value", ColumnPath: []string{"value"},
	}}}
	ids := array.NewInt64Builder(memory.DefaultAllocator)
	values := array.NewStringBuilder(memory.DefaultAllocator)
	ids.Append(1)
	values.Append("invalid")
	idArray, valueArray := ids.NewArray(), values.NewArray()
	ids.Release()
	values.Release()
	batch := array.NewRecordBatch(sourceSchema.ToArrowSchema(), []arrow.Array{idArray, valueArray}, 1)
	idArray.Release()
	valueArray.Release()
	table := source.SourceTableInfo{Name: "public.events", Schema: testCDCSchema(sourceSchema), PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
	records <- source.RecordBatchResult{
		TableName: table.Name,
		Batch:     batch,
		CommitToken: source.CDCStateCommitToken{SnapshotPositions: map[string]string{
			table.Name: "0/20",
		}},
	}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{SchemaContract: "discard_row", NoLoadTimestamp: true},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"},
		EvolutionPlans: map[string]*schemaevolution.EvolutionPlan{
			table.Name: {TransformComparison: comparison, FinalSchema: finalSchema},
		},
	}

	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 100})
	require.NoError(t, executor.ExecuteMultiTable(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Empty(t, dest.writeRows)
	require.Len(t, dest.publishes, 1)
}

func TestMultiTableBatchPublishesMultipleResetSnapshotsAfterAllStagesSucceed(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	tables := []source.SourceTableInfo{
		{Name: "public.first", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "first-v1", SchemaFingerprint: "first-schema-v1"},
		{Name: "public.second", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "second-v1", SchemaFingerprint: "second-schema-v1"},
	}
	records := make(chan source.RecordBatchResult, 2)
	for _, table := range tables {
		records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
	}
	close(records)
	src := &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{tables: tables, records: records}}
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge}, Source: src, Destination: dest, Tables: tables,
		TableDestNames: map[string]string{"public.first": "lake.first", "public.second": "lake.second"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))
	require.ElementsMatch(t, []string{"public.first", "public.second"}, src.readOpts.KnownTables)
	require.True(t, src.readOpts.CDCSnapshotReplace)
	require.Equal(t, "first-v1", src.readOpts.CDCResumeIncarnations["public.first"])
	require.Equal(t, "second-schema-v1", src.readOpts.CDCResumeSchemaFingerprints["public.second"])
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.publishes, 2)
	require.Equal(t, "lake.first", dest.publishes[0].Table)
	require.Equal(t, "lake.second", dest.publishes[1].Table)
	require.Len(t, dest.begins, 2)
	require.Empty(t, dest.aborts)
	require.Empty(t, dest.truncateCalls)
}

func TestAtomicMultiTableReadFreezesTablesDiscoveredBeforeSourceRead(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	known := source.SourceTableInfo{Name: "public.known", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "known-v1", SchemaFingerprint: "known-schema-v1"}
	late := source.SourceTableInfo{Name: "public.late", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	src := &lateDiscoveryFilteringSource{
		announcingMultiTableSource: &announcingMultiTableSource{tables: []source.SourceTableInfo{known}},
		late:                       late,
	}
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source:         src,
		Destination:    dest,
		Tables:         []source.SourceTableInfo{known},
		TableDestNames: map[string]string{known.Name: "lake.known"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.Equal(t, []string{known.Name}, src.readOpts.KnownTables)
	require.False(t, src.includedLate, "a table discovered after the pipeline froze its target set must wait for the next run")
	require.Equal(t, "known-v1", src.readOpts.CDCResumeIncarnations[known.Name])
	require.Equal(t, "known-schema-v1", src.readOpts.CDCResumeSchemaFingerprints[known.Name])
	require.True(t, src.readOpts.CDCSnapshotReplace)
}

func TestMultiTableBatchPartialPublicationIsReplayableWithoutCheckpoint(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	tables := []source.SourceTableInfo{
		{Name: "public.first", Schema: tableSchema, PrimaryKeys: []string{"id"}},
		{Name: "public.second", Schema: tableSchema, PrimaryKeys: []string{"id"}},
	}
	dest := &failNthPublishAtomicDestination{
		atomicSnapshotDestination: &atomicSnapshotDestination{fakeDestination: &fakeDestination{}},
		failAt:                    2,
	}
	newJob := func() *MultiTableIngestionJob {
		records := make(chan source.RecordBatchResult, 4)
		for i, table := range tables {
			records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
			records <- source.RecordBatchResult{
				TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{int64(i + 1)}, nil),
				CommitToken: source.CDCStateCommitToken{SnapshotPositions: map[string]string{table.Name: "0/20"}},
			}
		}
		close(records)
		return &MultiTableIngestionJob{
			Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
			Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
				tables: tables, records: records,
			}},
			Destination: dest,
			Tables:      tables,
			TableDestNames: map[string]string{
				tables[0].Name: "lake.first", tables[1].Name: "lake.second",
			},
		}
	}

	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), newJob()), "injected multi-table publish failure")
	dest.atomicMu.Lock()
	require.Len(t, dest.begins, 2, "every replacement must be staged before publication begins")
	require.Len(t, dest.publishes, 1, "the first per-table commit may be visible, but no source state is acknowledged")
	dest.atomicMu.Unlock()

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), newJob()))
	dest.atomicMu.Lock()
	require.Len(t, dest.publishes, 3, "replay must republish the complete target set after an incomplete generation")
	require.Equal(t, "lake.first", dest.publishes[1].Table)
	require.Equal(t, "lake.second", dest.publishes[2].Table)
	dest.atomicMu.Unlock()
}

func TestMultiTableBatchSpoolKeepsOnlyRowsAfterEachReset(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	first := source.SourceTableInfo{Name: "public.first", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	second := source.SourceTableInfo{Name: "public.second", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 8)
	records <- source.RecordBatchResult{TableName: first.Name, Batch: int64RecordBatch(t, "id", []int64{91}, nil)}
	records <- source.RecordBatchResult{TableName: second.Name, Batch: int64RecordBatch(t, "id", []int64{92}, nil)}
	records <- source.RecordBatchResult{TableName: first.Name, Truncate: true}
	records <- source.RecordBatchResult{TableName: first.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	records <- source.RecordBatchResult{TableName: second.Name, Truncate: true}
	records <- source.RecordBatchResult{TableName: second.Name, Batch: int64RecordBatch(t, "id", []int64{2}, nil)}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{first, second}, records: records,
		}},
		Destination: dest,
		Tables:      []source.SourceTableInfo{first, second},
		TableDestNames: map[string]string{
			first.Name: "lake.first", second.Name: "lake.second",
		},
		CDCResumeLSNs: map[string]string{first.Name: "0/10", second.Name: "0/10"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.begins, 2)
	require.Len(t, dest.publishes, 2)
	require.ElementsMatch(t, []int64{1, 1}, dest.writeRows)
}

func TestMultiTableBatchWALTruncateUsesFencedAtomicReplacement(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	truncated := source.SourceTableInfo{Name: "public.truncated", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	unchanged := source.SourceTableInfo{Name: "public.unchanged", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 5)
	records <- source.RecordBatchResult{TableName: truncated.Name, Batch: int64RecordBatch(t, "id", []int64{98}, nil)}
	records <- source.RecordBatchResult{TableName: truncated.Name, Truncate: true, CDCWALTruncate: true}
	records <- source.RecordBatchResult{
		TableName: truncated.Name, Batch: int64RecordBatch(t, "id", []int64{7}, nil),
		CommitToken: source.CDCStateCommitToken{Position: "0/20"},
	}
	records <- source.RecordBatchResult{TableName: unchanged.Name, Batch: int64RecordBatch(t, "id", []int64{8}, nil)}
	close(records)
	dest := &snapshotReplacementTrackingDestination{
		atomicSnapshotDestination: &atomicSnapshotDestination{fakeDestination: &fakeDestination{
			tableSchemas: map[string]*schema.TableSchema{
				"lake.truncated": destination.DestinationTableSchema(tableSchema),
				"lake.unchanged": destination.DestinationTableSchema(tableSchema),
			},
		}},
		visible: map[int64]struct{}{99: {}},
		staged:  make(map[string][]int64),
	}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{truncated, unchanged}, records: records,
		}},
		Destination: dest,
		Tables:      []source.SourceTableInfo{truncated, unchanged},
		TableDestNames: map[string]string{
			truncated.Name: "lake.truncated", unchanged.Name: "lake.unchanged",
		},
		CDCResumeLSNs: map[string]string{truncated.Name: "0/10", unchanged.Name: "0/10"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job))
	dest.rowsMu.Lock()
	require.Equal(t, map[int64]struct{}{7: {}}, dest.visible)
	dest.rowsMu.Unlock()
	dest.atomicMu.Lock()
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "lake.truncated", dest.publishes[0].Table)
	require.Equal(t, "0/20", dest.publishes[0].CDCResumeLSN)
	dest.atomicMu.Unlock()
	dest.mu.Lock()
	require.Len(t, dest.mergeCalls, 1)
	require.Equal(t, "lake.unchanged", dest.mergeCalls[0].TargetTable)
	dest.mu.Unlock()
}

func TestMultiTableBatchFailureConditionallyRemovesOwnedUnpublishedSnapshotTarget(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
	records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	src := &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}}
	dest := &atomicSnapshotDestination{
		fakeDestination: &fakeDestination{}, writeErr: errors.New("snapshot staging failed"), ownedDropErr: errors.New("ownership changed"),
	}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge}, Source: src, Destination: dest,
		Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "lake.snapshot"},
	}

	require.Error(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 1)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
	require.Len(t, dest.aborts, 1)
	require.Empty(t, dest.publishes)
	require.Empty(t, dest.dropCalls, "unconditional cleanup must not delete a replacement owner")
}

func TestAtomicMultiTableBatchLeaseLossDuringSpoolPreventsDestinationMutation(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during atomic preflight spool")}
	records := make(chan source.RecordBatchResult)
	go func() {
		records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
		close(lease.done)
		close(records)
	}()
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.snapshot"},
	}

	err := (&MergeStrategy{}).ExecuteMultiTable(guardedStreamingContext(lease), job)
	require.ErrorContains(t, err, "lease lost during atomic preflight spool")
	require.Empty(t, dest.prepareCalls)
	dest.atomicMu.Lock()
	require.Empty(t, dest.begins)
	require.Empty(t, dest.publishes)
	dest.atomicMu.Unlock()
}

func TestAtomicMultiTableFailureNeverDropsUnownedPreparedTarget(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	dest := &unownedAtomicSnapshotDestination{
		fakeDestination: &fakeDestination{},
		beginErr:        errors.New("atomic begin failed after external replacement"),
	}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: mustClosedRecords(),
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.snapshot"},
	}

	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job), "atomic begin failed")
	require.Empty(t, dest.dropCalls, "without destination-enforced ownership, cleanup must preserve a possible external replacement")
}

func TestAtomicMultiTableDirectWorkerFailureReleasesDispatchedReplayBatches(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	previousAllocator := memory.DefaultAllocator
	memory.DefaultAllocator = pool
	t.Cleanup(func() {
		memory.DefaultAllocator = previousAllocator
		pool.AssertSize(t, 0)
	})

	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.resume", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 16)
	for i := int64(0); i < 16; i++ {
		records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{i}, nil)}
	}
	close(records)
	dest := &immediateFailAtomicMultiDestination{atomicSnapshotDestination: &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"lake.resume": destination.DestinationTableSchema(tableSchema)},
	}}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.resume"},
		CDCResumeLSNs:  map[string]string{table.Name: "0/10"},
	}

	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job), "direct worker failed")
}

func TestAtomicMultiTablePreflightFailureDrainsUnreadArrowBatches(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	previousAllocator := memory.DefaultAllocator
	memory.DefaultAllocator = pool
	t.Cleanup(func() {
		memory.DefaultAllocator = previousAllocator
		pool.AssertSize(t, 0)
	})

	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 18)
	records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	records <- source.RecordBatchResult{
		TableName: table.Name,
		Batch:     int64RecordBatch(t, "id", []int64{2}, nil),
		Err:       errors.New("source failed during atomic preflight"),
	}
	for i := int64(3); i < 19; i++ {
		records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{i}, nil)}
	}
	close(records)

	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.snapshot"},
	}

	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job), "source failed during atomic preflight")
}

func TestAtomicMultiTablePreflightAppliesSnapshotInvalidationBeforePublication(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{
		Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"},
		Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	records := mustClosedRecords(
		source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
			TableName: table.Name, Incarnation: "source-v2", SchemaFingerprint: "schema-v2",
		}},
		source.RecordBatchResult{TableName: table.Name, Truncate: true},
		source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "snapshot:metadata-v2", DurableCommitPosition: "0/20",
		},
	)
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.snapshot"] = "target-v1"
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "target-v1",
	}
	manager, err := NewCDCStateManager(dest, "metadata-change-atomic", "lake.snapshot", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(t.Context(), table.Name, "lake.snapshot", table.Incarnation, table.SchemaFingerprint))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames:  map[string]string{table.Name: "lake.snapshot"},
		CDCResumeLSNs:   map[string]string{table.Name: "0/10"},
		CDCStateManager: manager,
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{
		Position:             "0/20",
		SnapshotPositions:    map[string]string{table.Name: "0/20"},
		SnapshotIncarnations: map[string]string{table.Name: "source-v2"},
		SnapshotSchemas:      map[string]string{table.Name: "schema-v2"},
	}))
	restarted, err := NewCDCStateManager(dest, "metadata-change-atomic", "lake.snapshot", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableState(t.Context(), table.Name, "lake.snapshot", "source-v2", "schema-v2"))
	position, err := restarted.ResumePosition(t.Context(), table.Name)
	require.NoError(t, err)
	require.Equal(t, "0/20", position)
	dest.incarnationMu.Lock()
	require.Len(t, dest.publishedOpts, 1)
	require.Equal(t, "snapshot:metadata-v2", dest.publishedOpts[0].CommitToken)
	dest.incarnationMu.Unlock()
}

func TestMultiTableBatchLaterPrepareFailureCleansEarlierOwnedSnapshotTarget(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	snapshotTable := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	resumeTable := source.SourceTableInfo{Name: "public.resume", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: snapshotTable.Name, Truncate: true}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		prepareErrByTable: map[string]error{"lake.resume": errors.New("prepare failed")},
	}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{snapshotTable, resumeTable}, records: records,
		}},
		Destination: dest,
		Tables:      []source.SourceTableInfo{snapshotTable, resumeTable},
		TableDestNames: map[string]string{
			snapshotTable.Name: "lake.snapshot", resumeTable.Name: "lake.resume",
		},
		CDCResumeLSNs: map[string]string{resumeTable.Name: "0/19"},
	}

	require.Error(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 2)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.ElementsMatch(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
	require.Empty(t, dest.begins)
	require.Empty(t, dest.publishes)
}

func TestMultiTableBatchLostPrepareResponseCleansItsOwnedTarget(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: table.Name, Truncate: true}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		prepareErrByTable: map[string]error{"lake.snapshot": errors.New("prepare response lost after create")},
	}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.snapshot"},
	}

	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job), "prepare response lost")
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 1)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
}

func TestMultiTableBatchLaterPrepareFailureCleansNeverWrittenResumedTarget(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	first := source.SourceTableInfo{Name: "public.first", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	second := source.SourceTableInfo{Name: "public.second", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: first.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		prepareErrByTable: map[string]error{"lake.second": errors.New("prepare failed")},
	}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{first, second}, records: records,
		}},
		Destination: dest,
		Tables:      []source.SourceTableInfo{first, second},
		TableDestNames: map[string]string{
			first.Name: "lake.first", second.Name: "lake.second",
		},
		CDCResumeLSNs: map[string]string{first.Name: "0/10", second.Name: "0/10"},
	}

	require.Error(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.preparedOwnershipTokens, 2)
	require.ElementsMatch(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
	require.Empty(t, dest.mergeRows)
}

func TestMultiTableBatchPreservesResumedTargetAfterDirectWriteError(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}, mergeErr: errors.New("lost direct-write response")}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"},
		CDCResumeLSNs:  map[string]string{table.Name: "0/10"},
	}

	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job), "lost direct-write response")
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.mergeRows, 1)
	require.Empty(t, dest.ownedDropTokens, "a direct-write error may be a lost success response")
}

func TestMultiTableBatchRoutesResetToPublishAndResumeToMerge(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	resetTable := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	resumeTable := source.SourceTableInfo{Name: "public.resume", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	tables := []source.SourceTableInfo{resetTable, resumeTable}
	records := make(chan source.RecordBatchResult, 4)
	records <- source.RecordBatchResult{TableName: resetTable.Name, Truncate: true}
	records <- source.RecordBatchResult{
		TableName: resetTable.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID: "snapshot:final", DurableCommitPosition: "0/20",
	}
	records <- source.RecordBatchResult{
		TableName: resumeTable.Name, Batch: int64RecordBatch(t, "id", []int64{2}, nil),
		DurableCommitID: "wal:resume", DurableCommitPosition: "0/21",
	}
	close(records)
	src := &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{tables: tables, records: records}}
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge}, Source: src, Destination: dest, Tables: tables,
		TableDestNames: map[string]string{resetTable.Name: "lake.snapshot", resumeTable.Name: "lake.resume"},
		CDCResumeLSNs:  map[string]string{resumeTable.Name: "0/19"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.atomicMu.Lock()
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "lake.snapshot", dest.publishes[0].Table)
	dest.atomicMu.Unlock()
	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.mergeCalls, 1)
	require.Equal(t, "lake.resume", dest.mergeCalls[0].TargetTable)
}

func (d *atomicSnapshotDestination) EvolveAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.evolves = append(d.evolves, opts)
	return nil
}

func (d *atomicSnapshotDestination) WriteAtomicSnapshot(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	var types []arrow.DataType
	var nulls []int
	var rows int64
	for result := range records {
		if result.Batch != nil {
			rows += result.Batch.NumRows()
			if result.Batch.NumCols() > 1 {
				types = append(types, result.Batch.Column(1).DataType())
				nulls = append(nulls, result.Batch.Column(1).NullN())
			}
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.writes = append(d.writes, opts)
	d.writeTypes = append(d.writeTypes, types...)
	d.writeNulls = append(d.writeNulls, nulls...)
	d.writeRows = append(d.writeRows, rows)
	return d.writeErr
}

func (d *atomicSnapshotDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.publishes = append(d.publishes, opts)
	return nil
}

func TestStreamingGetRecordsLeavesSnapshotResetForFlushLoop(t *testing.T) {
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true}
	close(records)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &IngestionJob{
		Config:      &config.IngestConfig{Stream: true, DestTable: "lake.events"},
		Table:       &snapshotResetSourceTable{fakeSourceTable: &fakeSourceTable{readCh: records}},
		Destination: dest,
		Schema:      streamTestSchema(),
	}

	out, err := job.GetRecords(context.Background(), source.ReadOptions{Streaming: true})
	require.NoError(t, err)
	result, ok := <-out
	require.True(t, ok)
	require.True(t, result.Truncate)
	require.Empty(t, dest.begins)
	require.Empty(t, dest.prepareCalls)
}

func TestStreamingHandlesDynamicSnapshotResetBeforeRows(t *testing.T) {
	dest := &truncateCapableFakeDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true}
	records <- source.RecordBatchResult{
		TableName:                 "public.events",
		Batch:                     int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID:           "snapshot:final",
		DurableCommitPosition:     "0/100",
		DurableCheckpointID:       "checkpoint:0/100",
		DurableCheckpointPosition: "0/100",
		DurableCheckpointTable:    "public.events",
	}
	close(records)
	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Contains(t, dest.truncateCalls, "lake.events")
	require.Len(t, dest.writeCalls, 1)
	require.Len(t, dest.mergeCalls, 1)
	require.Equal(t, "0/100", dest.mergeCalls[0].CDCResumeLSN)
}

func TestStreamingUnknownTableBatchIsNotAcknowledged(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{
		"public.known": mergeTableState("lake.known"),
	})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName:       "public.missing",
		Batch:           int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken:     "must-not-commit",
		DurableCommitID: "must-not-commit",
	}
	close(records)
	err := loop.run(context.Background(), records)
	require.ErrorContains(t, err, `unknown table "public.missing"`)
	require.Empty(t, committer.committed())
	require.Empty(t, dest.writeCalls)
}

func TestStreamingShutdownProcessesSnapshotResetBeforeFinalRows(t *testing.T) {
	dest := &truncateCapableFakeDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true}
	records <- source.RecordBatchResult{
		TableName:             "public.events",
		Batch:                 int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID:       "snapshot:page-1",
		DurableCommitPosition: "",
	}
	close(records)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, loop.shutdown(ctx, records))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Contains(t, dest.truncateCalls, "lake.events")
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].SkipCDCResume)
}

func TestStreamingShutdownAbandonsActiveAtomicSnapshot(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))

	records := make(chan source.RecordBatchResult, 1)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(records)
		for i := 0; i < 32; i++ {
			records <- source.RecordBatchResult{
				TableName:       "public.events",
				Batch:           int64RecordBatch(t, "id", []int64{int64(i)}, nil),
				DurableCommitID: "snapshot:unpublished-page",
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	require.ErrorIs(t, loop.shutdown(ctx, records), context.Canceled)
	loop.cleanup(ctx)
	require.Less(t, time.Since(started), time.Second)
	require.Eventually(t, func() bool {
		select {
		case <-producerDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.begins, 1)
	require.Empty(t, dest.writes)
	require.Empty(t, dest.publishes)
	require.Len(t, dest.aborts, 1)
	require.Equal(t, dest.begins[0].AttemptID, dest.aborts[0].AttemptID)
}

func TestStreamingCleanupDoesNotAbortAfterPublicationAttempt(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.atomicSnapshot = true
	st.snapshotAttemptID = "unknown-publication"
	st.atomicPublishAttempted = true
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})

	loop.cleanup(context.Background())
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Empty(t, dest.aborts)
}

func TestStreamingCanceledClosedChannelDoesNotPublishActiveAtomicSnapshot(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))
	loop.buffer(source.RecordBatchResult{
		TableName:   "public.events",
		Batch:       int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: snapshotStateToken("public.events", "0/123"),
	})

	records := make(chan source.RecordBatchResult)
	close(records)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, loop.run(ctx, records), context.Canceled)

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.begins, 1)
	require.Empty(t, dest.writes)
	require.Empty(t, dest.publishes)
}

func TestStreamingAtomicSnapshotStagesPagesUntilFinalBoundary(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))
	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID: "snapshot:page-1",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.atomicMu.Lock()
	require.Len(t, dest.begins, 1)
	require.Len(t, dest.writes, 1)
	require.Empty(t, dest.publishes)
	dest.atomicMu.Unlock()
	require.Empty(t, dest.prepareCalls, "atomic reset must not truncate or recreate the visible target")

	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
		CommitToken: snapshotStateToken("public.events", "0/100"),
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.writes, 2)
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "0/100", dest.publishes[0].CommitToken)
	require.Equal(t, "0/100", dest.publishes[0].CDCResumeLSN)
	require.False(t, st.atomicSnapshot)
}

func TestStreamingAtomicSnapshotPublishesEmptySnapshotImmediately(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.empty_events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true}
	records <- source.RecordBatchResult{
		TableName: "public.events", CommitToken: snapshotStateToken("public.events", "0/200"),
	}
	close(records)
	require.NoError(t, loop.run(context.Background(), records))

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.begins, 1)
	require.Empty(t, dest.writes)
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "0/200", dest.publishes[0].CommitToken)
}

func TestStreamingAtomicSnapshotPublishesSmallFinalBatchBeforeFollowingWAL(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(context.Background(), records) }()
	records <- source.RecordBatchResult{TableName: "public.events", Truncate: true}
	records <- source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: snapshotStateToken("public.events", "0/100"),
	}
	require.Eventually(t, func() bool {
		dest.atomicMu.Lock()
		defer dest.atomicMu.Unlock()
		return len(dest.publishes) == 1
	}, time.Second, time.Millisecond, "final snapshot rows must publish before waiting for another result")

	records <- source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
		CommitToken: source.CDCStateCommitToken{Position: "0/101"},
	}
	close(records)
	require.NoError(t, <-done)

	dest.atomicMu.Lock()
	require.Len(t, dest.writes, 1, "only the snapshot batch is written through hidden atomic staging")
	require.Equal(t, "0/100", dest.publishes[0].CommitToken)
	require.Equal(t, "0/100", dest.publishes[0].CDCResumeLSN)
	dest.atomicMu.Unlock()
	dest.mu.Lock()
	require.Len(t, dest.writeCalls, 1, "following WAL batch uses the normal merge staging path")
	require.Nil(t, dest.writeCalls[0].CommitToken)
	dest.mu.Unlock()
}

func TestStreamingAtomicSnapshotsPublishIndependentlyPerTable(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	first := mergeTableState("lake.first")
	first.isCDC = true
	second := mergeTableState("lake.second")
	second.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.first": first, "public.second": second})

	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.first"))
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.second"))
	loop.buffer(source.RecordBatchResult{
		TableName: "public.first", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: snapshotStateToken("public.first", "0/300"),
	})
	loop.buffer(source.RecordBatchResult{
		TableName: "public.second", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
		DurableCommitID: "second:page-1",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.atomicMu.Lock()
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "lake.first", dest.publishes[0].Table)
	dest.atomicMu.Unlock()
	require.False(t, first.atomicSnapshot)
	require.True(t, second.atomicSnapshot)
}

func TestStreamingAtomicSnapshotSchemaRefreshRestartsHiddenSnapshot(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})
	loop.cfg.NoLoadTimestamp = true
	evolveCalls := 0
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error {
		evolveCalls++
		return nil
	}

	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))
	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID: "old-shape-page",
	})
	newSchema := *st.schema
	newSchema.Columns = append(append([]schema.Column{}, st.schema.Columns...), schema.Column{
		Name: "added", DataType: schema.TypeString, Nullable: true,
	})
	require.NoError(t, loop.refreshTableSchema(context.Background(), source.SourceTableInfo{
		Name: "public.events", Schema: &newSchema, PrimaryKeys: []string{"id"},
	}, st))

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.writes, 1, "old-shape pages are durably consumed before restarting")
	require.Len(t, dest.begins, 1)
	require.Len(t, dest.evolves, 1, "schema refresh must evolve hidden staging without changing the target")
	require.Empty(t, dest.publishes)
	require.Equal(t, "added", dest.evolves[0].Schema.Columns[len(dest.evolves[0].Schema.Columns)-1].Name)
	require.Zero(t, evolveCalls, "visible target schema must change with final snapshot publication")
	require.True(t, st.atomicSnapshot)
}

func TestStreamingAtomicSnapshotSchemaRefreshHonorsFreezeContract(t *testing.T) {
	current := streamTestSchema()
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"lake.events": current},
	}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})
	loop.cfg.NoLoadTimestamp = true
	loop.cfg.SchemaContract = "freeze"
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error { return nil }
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))

	newSchema := *current
	newSchema.Columns = append(append([]schema.Column{}, current.Columns...), schema.Column{
		Name: "forbidden", DataType: schema.TypeString, Nullable: true,
	})
	err := loop.refreshTableSchema(context.Background(), source.SourceTableInfo{
		Name: "public.events", Schema: &newSchema, PrimaryKeys: []string{"id"},
	}, st)
	require.ErrorContains(t, err, "schema contract violation")
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Empty(t, dest.evolves)
	require.NotContains(t, st.schema.ColumnNames(), "forbidden")
}

func TestStreamingAtomicSnapshotRejectsDiscardRowSchemaRefresh(t *testing.T) {
	current := streamTestSchema()
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"lake.events": current},
	}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.events": st})
	loop.cfg.NoLoadTimestamp = true
	loop.cfg.SchemaContract = "discard_row"
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error { return nil }
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))

	newSchema := *current
	newSchema.Columns = append(append([]schema.Column{}, current.Columns...), schema.Column{
		Name: "violating", DataType: schema.TypeString, Nullable: true,
	})
	err := loop.refreshTableSchema(context.Background(), source.SourceTableInfo{
		Name: "public.events", Schema: &newSchema, PrimaryKeys: []string{"id"},
	}, st)
	require.ErrorContains(t, err, "unsupported with discard_row")
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Empty(t, dest.evolves)
	require.NotContains(t, st.schema.ColumnNames(), "violating")
}

func TestStreamingAtomicAppendSnapshotKeepsCDCChangelogColumns(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := &streamTableState{destTable: "lake.events", schema: keylessCDCSchema(), isCDC: true}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyAppend,
	}, map[string]*streamTableState{"public.events": st})

	require.NoError(t, loop.handleSnapshotReset(context.Background(), "public.events"))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.begins, 1)
	require.Contains(t, dest.begins[0].TargetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
}

func TestStreamingAtomicSnapshotDefersSharedSourceAckUntilAllTablesPublish(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	committer := &fakeCommitter{}
	first := mergeTableState("lake.first")
	second := mergeTableState("lake.second")
	first.isCDC, second.isCDC = true, true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 1, Strategy: config.StrategyMerge, Committer: committer,
	}, map[string]*streamTableState{"first": first, "second": second})
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "first"))
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "second"))

	loop.buffer(source.RecordBatchResult{
		TableName: "first", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: snapshotStateToken("first", "0/500"),
	})
	require.NoError(t, loop.flush(context.Background()))
	require.Empty(t, committer.committed(), "shared source cursor cannot advance while another snapshot table is hidden")

	loop.buffer(source.RecordBatchResult{
		TableName: "second", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
		CommitToken: snapshotStateToken("second", "0/500"),
	})
	require.NoError(t, loop.flush(context.Background()))
	require.Equal(t, []any{snapshotStateToken("second", "0/500")}, committer.committed())
}

func TestStreamingFinalFlushPublishesPendingEmptyAtomicSnapshot(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.empty")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge}, map[string]*streamTableState{"events": st})
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "events"))
	require.NoError(t, loop.bufferResult(source.RecordBatchResult{
		TableName: "events", CommitToken: snapshotStateToken("events", "0/600"),
	}))
	require.NoError(t, loop.finalFlush(context.Background()))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.publishes, 1)
	require.Equal(t, "0/600", dest.publishes[0].CommitToken)
}

func TestStreamingAtomicSnapshotDefersEvolutionUntilPublication(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	evolutions := 0
	st.deferredEvolution = func(context.Context) error { evolutions++; return nil }
	loop := newTestLoop(dest, StreamingOptions{FlushInterval: time.Hour, FlushRecords: 1, Strategy: config.StrategyMerge}, map[string]*streamTableState{"events": st})
	require.NoError(t, loop.handleSnapshotReset(context.Background(), "events"))
	batch := int64RecordBatch(t, "id", []int64{1}, nil)
	defer batch.Release()
	require.NoError(t, loop.applyDeferredEvolution(context.Background(), source.RecordBatchResult{
		TableName: "events", Batch: batch,
	}))
	require.Zero(t, evolutions)
}

func TestStreamingWithoutSnapshotAppliesDeferredEvolutionBeforeData(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	evolutions := 0
	st.deferredEvolution = func(context.Context) error { evolutions++; return nil }
	loop := newTestLoop(dest, StreamingOptions{FlushInterval: time.Hour, FlushRecords: 1, Strategy: config.StrategyMerge}, map[string]*streamTableState{"events": st})
	batch := int64RecordBatch(t, "id", []int64{1}, nil)
	defer batch.Release()
	require.NoError(t, loop.applyDeferredEvolution(context.Background(), source.RecordBatchResult{TableName: "events", Batch: batch}))
	require.Equal(t, 1, evolutions)
}
