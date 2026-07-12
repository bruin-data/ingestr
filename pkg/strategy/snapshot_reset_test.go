package strategy

import (
	"context"
	"errors"
	"fmt"
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

type leaseLossAfterAtomicWriteDestination struct {
	*atomicSnapshotDestination
	loseLease func()
}

type failNthPublishAtomicDestination struct {
	*atomicSnapshotDestination
	failAt int
	calls  int
}

type ambiguousPublishAtomicDestination struct {
	*atomicSnapshotDestination
	publishCalls int
}

type incarnationFencedAtomicDestination struct {
	*incarnationTrackingAtomicSnapshotDestination
}

type externallyReplacedAfterAtomicPublishDestination struct {
	*incarnationTrackingAtomicSnapshotDestination
	externalIncarnation string
}

type ambiguousManagedAtomicSnapshotDestination struct {
	*incarnationTrackingAtomicSnapshotDestination
	publishMu       sync.Mutex
	attempts        []string
	options         []destination.AtomicSnapshotOptions
	lostResponseFor string
}

type beforeReadSourceTable struct {
	source.SourceTable
	beforeRead func()
}

type beforeReadAllSnapshotSource struct {
	*snapshotResetMultiSource
	beforeRead func()
}

type trackingMultiTableSource struct {
	*announcingMultiTableSource
	readCalled bool
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

func (d *failNthPublishAtomicDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	d.calls++
	if d.calls == d.failAt {
		return "", errors.New("injected multi-table publish failure")
	}
	return d.atomicSnapshotDestination.PublishAtomicSnapshot(context.Background(), opts)
}

func (d *leaseLossAfterAtomicWriteDestination) WriteAtomicSnapshot(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	if err := d.atomicSnapshotDestination.WriteAtomicSnapshot(ctx, records, opts); err != nil {
		return err
	}
	d.loseLease()
	return nil
}

func (d *ambiguousPublishAtomicDestination) PublishAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	incarnation, err := d.atomicSnapshotDestination.PublishAtomicSnapshot(ctx, opts)
	if err != nil {
		return "", err
	}
	d.atomicMu.Lock()
	d.publishCalls++
	call := d.publishCalls
	d.atomicMu.Unlock()
	if call == 1 {
		return "", errors.New("injected lost publish response")
	}
	return incarnation, nil
}

func (d *incarnationFencedAtomicDestination) MergeRecords(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	_ destination.WriteOptions,
	opts destination.MergeOptions,
) error {
	drainAndRelease(records)
	d.stateMu.Lock()
	current := d.incarnations[opts.TargetTable]
	d.stateMu.Unlock()
	d.mu.Lock()
	d.mergeCalls = append(d.mergeCalls, opts)
	d.mu.Unlock()
	if opts.CDCExpectedIncarnation == "" || current != opts.CDCExpectedIncarnation {
		return fmt.Errorf("destination table %q physical incarnation changed", opts.TargetTable)
	}
	return nil
}

func (s *beforeReadSourceTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	s.beforeRead()
	return s.SourceTable.Read(ctx, opts)
}

func (s *beforeReadAllSnapshotSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	s.beforeRead()
	return s.snapshotResetMultiSource.ReadAll(ctx, opts)
}

func (s *trackingMultiTableSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	s.readCalled = true
	return s.announcingMultiTableSource.ReadAll(ctx, opts)
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

func (*unownedAtomicSnapshotDestination) PublishAtomicSnapshot(context.Context, destination.AtomicSnapshotOptions) (string, error) {
	return "", nil
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

func (d *snapshotReplacementTrackingDestination) PublishAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	incarnation, err := d.atomicSnapshotDestination.PublishAtomicSnapshot(ctx, opts)
	if err != nil {
		return "", err
	}
	d.rowsMu.Lock()
	d.visible = make(map[int64]struct{}, len(d.staged[opts.AttemptID]))
	for _, id := range d.staged[opts.AttemptID] {
		d.visible[id] = struct{}{}
	}
	d.rowsMu.Unlock()
	return incarnation, nil
}

type incarnationTrackingAtomicSnapshotDestination struct {
	*cdcStateDestination
	publishedIncarnation string
	incarnationMu        sync.Mutex
	incarnationReads     int
	begunOpts            []destination.AtomicSnapshotOptions
	evolvedOpts          []destination.AtomicSnapshotOptions
	writtenOpts          []destination.WriteOptions
	publishedOpts        []destination.AtomicSnapshotOptions
	abortedOpts          []destination.AtomicSnapshotOptions
	discardedOpts        []destination.AtomicSnapshotOptions
}

type idempotentIncarnationTrackingAtomicSnapshotDestination struct {
	*incarnationTrackingAtomicSnapshotDestination
	tokenMu         sync.Mutex
	committedTokens map[source.DurableID]struct{}
}

func (*idempotentIncarnationTrackingAtomicSnapshotDestination) SupportsIdempotentCommitTokenWrites() bool {
	return true
}

func (d *idempotentIncarnationTrackingAtomicSnapshotDestination) WriteParallel(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.tokenMu.Lock()
	defer d.tokenMu.Unlock()
	if d.committedTokens == nil {
		d.committedTokens = make(map[source.DurableID]struct{})
	}
	_, duplicate := d.committedTokens[opts.CommitToken]
	if opts.CommitToken != "" && duplicate {
		drainAndRelease(records)
		return nil
	}
	if err := d.cdcStateDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if opts.CommitToken != "" {
		d.committedTokens[opts.CommitToken] = struct{}{}
	}
	return nil
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

func (d *incarnationTrackingAtomicSnapshotDestination) AbortAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.incarnationMu.Lock()
	d.abortedOpts = append(d.abortedOpts, opts)
	d.incarnationMu.Unlock()
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) DiscardAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.incarnationMu.Lock()
	d.discardedOpts = append(d.discardedOpts, opts)
	d.incarnationMu.Unlock()
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) EvolveAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) error {
	d.incarnationMu.Lock()
	d.evolvedOpts = append(d.evolvedOpts, opts)
	d.incarnationMu.Unlock()
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	incarnation, exists, err := d.cdcStateDestination.CDCTargetIncarnation(ctx, table)
	d.incarnationMu.Lock()
	d.incarnationReads++
	d.incarnationMu.Unlock()
	return incarnation, exists, err
}

func (d *incarnationTrackingAtomicSnapshotDestination) WriteAtomicSnapshot(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	d.incarnationMu.Lock()
	d.writtenOpts = append(d.writtenOpts, opts)
	d.incarnationMu.Unlock()
	return nil
}

func (d *incarnationTrackingAtomicSnapshotDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	d.stateMu.Lock()
	current := d.incarnations[opts.Table]
	if current == "" {
		current = "incarnation:" + opts.Table
	}
	if opts.CDCExpectedIncarnation != "" && current != opts.CDCExpectedIncarnation {
		d.stateMu.Unlock()
		return "", fmt.Errorf("destination table %q physical incarnation changed before atomic publication", opts.Table)
	}
	d.incarnations[opts.Table] = d.publishedIncarnation
	d.stateMu.Unlock()
	d.incarnationMu.Lock()
	d.publishedOpts = append(d.publishedOpts, opts)
	d.incarnationMu.Unlock()
	return d.publishedIncarnation, nil
}

func (d *externallyReplacedAfterAtomicPublishDestination) PublishAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	published, err := d.incarnationTrackingAtomicSnapshotDestination.PublishAtomicSnapshot(ctx, opts)
	if err != nil {
		return "", err
	}
	d.stateMu.Lock()
	d.incarnations[opts.Table] = d.externalIncarnation
	d.stateMu.Unlock()
	return published, nil
}

func (d *ambiguousManagedAtomicSnapshotDestination) PublishAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	d.publishMu.Lock()
	d.attempts = append(d.attempts, opts.AttemptID)
	d.options = append(d.options, opts)
	first := d.lostResponseFor == ""
	if first {
		d.lostResponseFor = opts.AttemptID
	}
	retryingLost := opts.AttemptID == d.lostResponseFor
	d.publishMu.Unlock()

	if first {
		if _, err := d.incarnationTrackingAtomicSnapshotDestination.PublishAtomicSnapshot(ctx, opts); err != nil {
			return "", err
		}
		return "", errors.New("injected lost atomic publication response")
	}
	if retryingLost {
		return d.publishedIncarnation, nil
	}
	return d.incarnationTrackingAtomicSnapshotDestination.PublishAtomicSnapshot(ctx, opts)
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
	require.Equal(t, atomicSnapshotCommitID(dest.publishes[0].AttemptID), dest.publishes[0].CommitToken)
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
					dest: dest, incarnation: dest, connectorID: "test", runID: "00000000000000000000000000000001",
					started: true, generation: 1, runs: map[string]struct{}{}, states: map[cdcStateKey]reducedCDCState{},
					destTables:          map[string]string{job.Config.SourceTable: job.Config.DestTable},
					currentIncarnations: map[string]string{}, currentSchemas: map[string]string{},
					boundDestinations: map[string]string{}, boundDestinationRaw: map[string]string{},
					atomicAttempts: map[string]atomicSnapshotJournalEntry{}, atomicSequences: map[string]uint64{},
					snapshotEpochs: map[string]uint64{},
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
		publishedIncarnation: "destination-after-publication",
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
	require.Len(t, dest.writtenOpts, 1)
	require.Equal(t, "destination-before-publication", dest.writtenOpts[0].CDCExpectedIncarnation)
	require.Len(t, dest.publishedOpts, 1)
	require.True(t, dest.publishedOpts[0].SkipCDCResume)
	dest.incarnationMu.Unlock()
	require.Equal(t, readsBeforeExecute+2, readsAfterExecute, "atomic batch snapshot must fence before publication and rebind after it")
}

func TestBatchAtomicSnapshotRejectsExternalReplacementAfterPublication(t *testing.T) {
	stateDest := newCDCStateDestination()
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "destination-published",
	}
	dest := &externallyReplacedAfterAtomicPublishDestination{
		incarnationTrackingAtomicSnapshotDestination: base,
		externalIncarnation:                          "destination-external",
	}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before"
	manager, err := NewCDCStateManager(dest, "batch-publish-race", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job.CDCStateManager = manager
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:empty", DurableCommitPosition: "0/900"},
	)

	err = (&MergeStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "changed after its fenced replacement was published")
	require.Equal(t, cdcDestinationIncarnationDigest("destination-before"), manager.boundDestinations[job.Config.SourceTable])
}

func TestIncarnationTrackingAtomicPublicationRejectsReplacementBeforeCommit(t *testing.T) {
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "external-replacement"
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "snapshot-publication",
	}

	_, err := dest.PublishAtomicSnapshot(t.Context(), destination.AtomicSnapshotOptions{
		Table: "lake.events", AttemptID: "attempt-1", CDCExpectedIncarnation: "original-target",
	})
	require.ErrorContains(t, err, "physical incarnation changed before atomic publication")
	require.Equal(t, "external-replacement", stateDest.incarnations["lake.events"])
	require.Empty(t, dest.publishedOpts)
}

func TestBatchAtomicSnapshotReusesDurableAttemptAfterAmbiguousPublicationRestart(t *testing.T) {
	ctx := t.Context()
	stateDest := newCDCStateDestination()
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "destination-published",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before"

	first, err := NewCDCStateManager(dest, "batch-ambiguous-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, first.BeginRun(ctx, false))
	job.CDCStateManager = first
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:empty", DurableCommitPosition: "0/900"},
	)
	err = (&MergeStrategy{}).Execute(ctx, job)
	require.ErrorContains(t, err, "lost atomic publication response")

	restarted, err := NewCDCStateManager(dest, "batch-ambiguous-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, restarted.BeginRun(ctx, false))
	job.CDCStateManager = restarted
	src.mu.Lock()
	src.readCalled = false
	src.readErr = errors.New("source must not be reread while recovering sealed snapshot")
	src.mu.Unlock()
	require.NoError(t, (&MergeStrategy{}).Execute(ctx, job))

	dest.publishMu.Lock()
	require.Len(t, dest.attempts, 2)
	require.Equal(t, dest.attempts[0], dest.attempts[1])
	require.Equal(t, "0/900", dest.options[1].CDCResumeLSN)
	require.Equal(t, "destination-before", dest.options[1].CDCExpectedIncarnation)
	dest.publishMu.Unlock()
	src.mu.Lock()
	require.False(t, src.readCalled)
	src.mu.Unlock()
	require.Empty(t, restarted.atomicAttempts)
	verified, err := NewCDCStateManager(dest, "batch-ambiguous-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, verified.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	position, err := verified.ResumePosition(ctx, job.Config.SourceTable)
	require.NoError(t, err)
	require.Equal(t, "0/900", position)
}

func TestBatchAtomicSnapshotDiscardsSealedAttemptWhenSourceMetadataChanges(t *testing.T) {
	for _, tc := range []struct {
		name              string
		sourceIncarnation string
		schemaFingerprint string
		legacyJournal     bool
	}{
		{name: "source incarnation", sourceIncarnation: "source-v2", schemaFingerprint: "schema-v1"},
		{name: "schema fingerprint", sourceIncarnation: "source-v1", schemaFingerprint: "schema-v2"},
		{name: "legacy journal", sourceIncarnation: "source-v1", schemaFingerprint: "schema-v1", legacyJournal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertBatchAtomicSnapshotDiscardsSealedAttemptWhenSourceMetadataChanges(
				t, tc.sourceIncarnation, tc.schemaFingerprint, tc.legacyJournal,
			)
		})
	}
}

func TestBatchAtomicSnapshotFullRefreshDiscardsSealedAttemptAndReadsFreshSnapshot(t *testing.T) {
	ctx := t.Context()
	stateDest := newCDCStateDestination()
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "destination-published",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before"

	first, err := NewCDCStateManager(dest, "batch-full-refresh-sealed-attempt", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableState(
		ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1", "schema-v1",
	))
	require.NoError(t, first.BeginRun(ctx, false))
	job.CDCStateManager = first
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:v1", DurableCommitPosition: "0/900"},
	)
	require.ErrorContains(t, (&MergeStrategy{}).Execute(ctx, job), "lost atomic publication response")

	restarted, err := NewCDCStateManager(dest, "batch-full-refresh-sealed-attempt", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableState(
		ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1", "schema-v1",
	))
	require.NoError(t, restarted.BeginRun(ctx, true))
	job.CDCStateManager = restarted
	job.Config.FullRefresh = true
	src.mu.Lock()
	src.readCalled = false
	src.readErr = nil
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:v2", DurableCommitPosition: "0/A00"},
	)
	src.mu.Unlock()
	require.NoError(t, (&MergeStrategy{}).Execute(ctx, job))

	dest.publishMu.Lock()
	require.Len(t, dest.attempts, 2)
	require.NotEqual(t, dest.attempts[0], dest.attempts[1])
	require.Equal(t, "0/A00", dest.options[1].CDCResumeLSN)
	dest.publishMu.Unlock()
	dest.incarnationMu.Lock()
	require.Empty(t, dest.abortedOpts)
	require.Len(t, dest.discardedOpts, 1)
	require.Equal(t, dest.attempts[0], dest.discardedOpts[0].AttemptID)
	dest.incarnationMu.Unlock()
	src.mu.Lock()
	require.True(t, src.readCalled)
	src.mu.Unlock()
	require.Empty(t, restarted.atomicAttempts)
}

func assertBatchAtomicSnapshotDiscardsSealedAttemptWhenSourceMetadataChanges(
	t *testing.T,
	sourceIncarnation, schemaFingerprint string,
	legacyJournal bool,
) {
	ctx := t.Context()
	stateDest := newCDCStateDestination()
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "destination-published",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before"

	first, err := NewCDCStateManager(dest, "batch-stale-sealed-attempt", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableState(
		ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1", "schema-v1",
	))
	require.NoError(t, first.BeginRun(ctx, false))
	job.CDCStateManager = first
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:v1", DurableCommitPosition: "0/900"},
	)
	err = (&MergeStrategy{}).Execute(ctx, job)
	require.ErrorContains(t, err, "lost atomic publication response")
	if legacyJournal {
		legacyFound := false
		stateDest.stateMu.Lock()
		for table, states := range stateDest.states {
			for i := range states {
				entry := &states[i].entry
				if entry.StateKind != cdcStateKindAtomicAttempt || entry.Status != cdcStateStatusReady {
					continue
				}
				attempt, valid := decodeAtomicSnapshotAttempt(entry.Position)
				if !valid {
					continue
				}
				attempt.sourceMetadataKnown = false
				attempt.sourceIncarnation = ""
				attempt.sourceSchemaFingerprint = ""
				entry.Position = encodeAtomicSnapshotAttempt(attempt)
				legacyFound = true
			}
			stateDest.states[table] = states
		}
		stateDest.stateMu.Unlock()
		require.True(t, legacyFound)
	}

	restarted, err := NewCDCStateManager(dest, "batch-stale-sealed-attempt", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableState(
		ctx, job.Config.SourceTable, job.Config.DestTable, sourceIncarnation, schemaFingerprint,
	))
	require.NoError(t, restarted.BeginRun(ctx, false))
	job.CDCStateManager = restarted
	src.mu.Lock()
	src.readCalled = false
	src.readErr = nil
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:v2", DurableCommitPosition: "0/A00"},
	)
	src.mu.Unlock()
	require.NoError(t, (&MergeStrategy{}).Execute(ctx, job))

	dest.publishMu.Lock()
	require.Len(t, dest.attempts, 2)
	require.NotEqual(t, dest.attempts[0], dest.attempts[1])
	require.Equal(t, "0/A00", dest.options[1].CDCResumeLSN)
	dest.publishMu.Unlock()
	dest.incarnationMu.Lock()
	require.Empty(t, dest.abortedOpts)
	require.NotEmpty(t, dest.discardedOpts)
	require.Equal(t, dest.attempts[0], dest.discardedOpts[0].AttemptID)
	dest.incarnationMu.Unlock()
	src.mu.Lock()
	require.True(t, src.readCalled)
	src.mu.Unlock()
}

func TestBatchAtomicSnapshotAbortsOpenAttemptBeforeCreatingReplacement(t *testing.T) {
	ctx := t.Context()
	stateDest := newCDCStateDestination()
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "destination-published",
	}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before"

	first, err := NewCDCStateManager(dest, "batch-open-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, first.BeginRun(ctx, false))
	openAttempt, err := first.AtomicSnapshotAttempt(ctx, job.Config.SourceTable, job.Config.DestTable)
	require.NoError(t, err)

	restarted, err := NewCDCStateManager(dest, "batch-open-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, restarted.BeginRun(ctx, false))
	job.CDCStateManager = restarted
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{DurableCommitID: "snapshot:empty", DurableCommitPosition: "0/A00"},
	)
	require.NoError(t, (&MergeStrategy{}).Execute(ctx, job))

	dest.incarnationMu.Lock()
	require.Len(t, dest.abortedOpts, 1)
	require.Equal(t, openAttempt, dest.abortedOpts[0].AttemptID)
	require.Len(t, dest.begunOpts, 1)
	require.NotEqual(t, openAttempt, dest.begunOpts[0].AttemptID)
	dest.incarnationMu.Unlock()

	next, err := NewCDCStateManager(dest, "batch-open-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, next.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, next.BeginRun(ctx, false))
	_, pending, err := next.PendingAtomicSnapshotAttempt(job.Config.SourceTable, job.Config.DestTable)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestManagedAppendSnapshotPublishesAtomically(t *testing.T) {
	stateDest := newCDCStateDestination()
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "target-v2",
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
	require.Equal(t, atomicSnapshotCommitID(dest.publishedOpts[0].AttemptID), dest.publishedOpts[0].CommitToken)
	require.Equal(t, "0/900", dest.publishedOpts[0].CDCResumeLSN)
	require.True(t, dest.publishedOpts[0].SkipCDCResume)
	require.Equal(t, "target-v1", dest.publishedOpts[0].CDCExpectedIncarnation)
	dest.incarnationMu.Unlock()
	require.Equal(t, cdcDestinationIncarnationDigest("target-v2"), manager.boundDestinations[job.Config.SourceTable])
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
	dest := &incarnationTrackingAtomicSnapshotDestination{cdcStateDestination: stateDest, publishedIncarnation: "target-v2"}
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
	require.Equal(t, atomicSnapshotCommitID(dest.publishedOpts[0].AttemptID), dest.publishedOpts[0].CommitToken)
	require.Empty(t, dest.publishedOpts[0].PrimaryKeys, "append snapshots must not acquire merge semantics")
	require.Equal(t, "target-v1", dest.publishedOpts[0].CDCExpectedIncarnation)
	require.Contains(t, dest.publishedOpts[0].TargetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
	dest.incarnationMu.Unlock()
	require.Equal(t, cdcDestinationIncarnationDigest("target-v2"), manager.boundDestinations[table.Name])
}

func TestStreamingDynamicManagedKeylessSnapshotPublishesAtomically(t *testing.T) {
	startup := newTableInfo("public.users")
	startup.DestSchema = "landing"
	late := source.SourceTableInfo{
		Name: "public.events", Schema: keylessCDCSchema(), DestSchema: "landing",
		Incarnation: "source-events-v1", SchemaFingerprint: "schema-events-v1",
	}
	stateDest := newCDCStateDestination()
	dest := &idempotentIncarnationTrackingAtomicSnapshotDestination{
		incarnationTrackingAtomicSnapshotDestination: &incarnationTrackingAtomicSnapshotDestination{
			cdcStateDestination: stateDest, publishedIncarnation: "target-events-v2",
		},
	}
	startupTarget := multiTableDestName(dest, startup)
	lateTarget := multiTableDestName(dest, late)
	stateDest.incarnations[startupTarget] = "target-users-v1"
	stateDest.incarnations[lateTarget] = "target-events-v1"
	manager, err := NewCDCStateManager(dest, "dynamic-keyless-atomic", startupTarget, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), startup.Name, startupTarget, "source-users-v1"))
	require.NoError(t, manager.ClaimTarget(t.Context(), startup.Name, startupTarget))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	records := mustClosedRecords(
		source.RecordBatchResult{TableName: late.Name, TableInfo: &late},
		source.RecordBatchResult{TableName: late.Name, Truncate: true},
		source.RecordBatchResult{
			TableName: late.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			CommitToken: snapshotStateToken(late.Name, "0/50"),
		},
	)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{startup}, records: records}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{
			IncrementalStrategy: config.StrategyMerge, FullRefresh: true, NoLoadTimestamp: true,
			FlushInterval: time.Hour, FlushRecords: 1,
		},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{startup},
		TableDestNames: map[string]string{startup.Name: startupTarget}, CDCStateManager: manager,
	}

	require.NoError(t, NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, FlushInterval: time.Hour, FlushRecords: 1,
	}).ExecuteMultiTable(t.Context(), job))
	require.True(t, src.readOpts.CDCStableDataBatches)
	dest.incarnationMu.Lock()
	require.Len(t, dest.begunOpts, 1)
	require.Equal(t, lateTarget, dest.begunOpts[0].Table)
	require.Equal(t, "target-events-v1", dest.begunOpts[0].CDCExpectedIncarnation)
	require.Len(t, dest.writtenOpts, 1)
	require.Equal(t, lateTarget, dest.writtenOpts[0].Table)
	require.Equal(t, "target-events-v1", dest.writtenOpts[0].CDCExpectedIncarnation)
	require.Len(t, dest.publishedOpts, 1)
	require.Equal(t, lateTarget, dest.publishedOpts[0].Table)
	dest.incarnationMu.Unlock()
	require.Equal(t, cdcDestinationIncarnationDigest("target-events-v2"), manager.boundDestinations[late.Name])
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
		records: mustClosedRecords(
			source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
			source.RecordBatchResult{TableName: table.Name, DurableCommitID: "snapshot:public.events", DurableCommitPosition: "0/100"},
		),
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

func TestManagedMultiTableRecoversPublishedTableAndRestartsOnlyOpenAttempt(t *testing.T) {
	ctx := t.Context()
	tableSchema := testCDCSchema(streamTestSchema())
	firstTable := source.SourceTableInfo{Name: "public.first", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "source-first"}
	secondTable := source.SourceTableInfo{Name: "public.second", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "source-second"}
	tables := []source.SourceTableInfo{firstTable, secondTable}
	tableDestNames := map[string]string{firstTable.Name: "lake.first", secondTable.Name: "lake.second"}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.first"] = "first-before"
	stateDest.incarnations["lake.second"] = "second-before"
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "published-after",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	firstSource := &announcingMultiTableSource{
		tables: tables,
		records: mustClosedRecords(
			source.RecordBatchResult{TableName: firstTable.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
			source.RecordBatchResult{TableName: firstTable.Name, DurableCommitID: "snapshot:first", DurableCommitPosition: "0/100"},
			source.RecordBatchResult{TableName: secondTable.Name, Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
			source.RecordBatchResult{TableName: secondTable.Name, DurableCommitID: "snapshot:second", DurableCommitPosition: "0/200"},
		),
	}
	manager, err := NewCDCStateManager(dest, "multi-ambiguous-restart", "lake.first", "")
	require.NoError(t, err)
	for _, table := range tables {
		require.NoError(t, manager.RegisterTableIncarnation(ctx, table.Name, tableDestNames[table.Name], table.Incarnation))
	}
	require.NoError(t, manager.BeginRun(ctx, false))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: firstSource, Destination: dest, Tables: tables, TableDestNames: tableDestNames,
		CDCStateManager: manager,
	}
	err = (&MergeStrategy{}).ExecuteMultiTable(ctx, job)
	require.ErrorContains(t, err, "lost atomic publication response")

	restarted, err := NewCDCStateManager(dest, "multi-ambiguous-restart", "lake.first", "")
	require.NoError(t, err)
	for _, table := range tables {
		require.NoError(t, restarted.RegisterTableIncarnation(ctx, table.Name, tableDestNames[table.Name], table.Incarnation))
	}
	require.NoError(t, restarted.BeginRun(ctx, false))
	secondSource := &announcingMultiTableSource{
		tables: tables,
		records: mustClosedRecords(
			source.RecordBatchResult{TableName: firstTable.Name, Batch: int64RecordBatch(t, "id", []int64{3}, nil)},
			source.RecordBatchResult{TableName: secondTable.Name, Batch: int64RecordBatch(t, "id", []int64{4}, nil)},
			source.RecordBatchResult{TableName: secondTable.Name, DurableCommitID: "snapshot:second-retry", DurableCommitPosition: "0/300"},
		),
	}
	job.Source = secondSource
	job.CDCStateManager = restarted
	job.CDCResumeLSNs = nil
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, job))

	require.Equal(t, "0/100", secondSource.readOpts.CDCResumeLSNs[firstTable.Name])
	require.Empty(t, secondSource.readOpts.CDCResumeLSNs[secondTable.Name])
	dest.publishMu.Lock()
	require.GreaterOrEqual(t, len(dest.options), 3)
	require.Equal(t, dest.options[0].AttemptID, dest.options[1].AttemptID)
	dest.publishMu.Unlock()
	dest.incarnationMu.Lock()
	require.NotEmpty(t, dest.abortedOpts)
	for _, aborted := range dest.abortedOpts {
		require.Equal(t, "lake.second", aborted.Table)
		require.Equal(t, dest.abortedOpts[0].AttemptID, aborted.AttemptID)
	}
	dest.incarnationMu.Unlock()
}

func TestManagedMultiTableAppendRecoveryRejectsUnsafeResumedWritesBeforeRead(t *testing.T) {
	ctx := t.Context()
	table := source.SourceTableInfo{
		Name: "public.events", Schema: keylessCDCSchema(),
		Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-before"
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "target-published",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	manager, err := NewCDCStateManager(dest, "multi-append-recovery-gate", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, table.Name, "lake.events", table.Incarnation, table.SchemaFingerprint))
	require.NoError(t, manager.BeginRun(ctx, false))
	firstSource := &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(
			source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
			source.RecordBatchResult{TableName: table.Name, DurableCommitID: "snapshot:events", DurableCommitPosition: "0/100"},
		),
	}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyAppend, NoLoadTimestamp: true},
		Source: firstSource, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"}, CDCStateManager: manager,
	}
	require.ErrorContains(t, (&AppendStrategy{}).ExecuteMultiTable(ctx, job), "lost atomic publication response")

	restarted, err := NewCDCStateManager(dest, "multi-append-recovery-gate", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableState(ctx, table.Name, "lake.events", table.Incarnation, table.SchemaFingerprint))
	require.NoError(t, restarted.BeginRun(ctx, false))
	secondSource := &trackingMultiTableSource{announcingMultiTableSource: &announcingMultiTableSource{
		tables:  []source.SourceTableInfo{table},
		records: mustClosedRecords(),
	}}
	job.Source = secondSource
	job.CDCStateManager = restarted
	job.CDCResumeLSNs = nil
	stateDest.mu.Lock()
	prepareCallsBefore := len(stateDest.prepareCalls)
	stateDest.mu.Unlock()

	err = (&AppendStrategy{}).ExecuteMultiTable(ctx, job)
	require.ErrorContains(t, err, "managed multi-table CDC append requires destination support for idempotent commit-token writes")
	require.False(t, secondSource.readCalled, "unsafe resumed append read from the source")
	stateDest.mu.Lock()
	require.Len(t, stateDest.prepareCalls, prepareCallsBefore, "unsafe resumed append prepared a destination table")
	stateDest.mu.Unlock()
}

func TestManagedMultiTableFullRefreshDiscardsSealedAttemptAndReadsFreshSnapshot(t *testing.T) {
	ctx := t.Context()
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{
		Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"},
		Incarnation: "source-v1", SchemaFingerprint: "schema-v1",
	}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-before"
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "target-published",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	firstSource := &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(
			source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
			source.RecordBatchResult{TableName: table.Name, DurableCommitID: "snapshot:v1", DurableCommitPosition: "0/100"},
		),
	}
	manager, err := NewCDCStateManager(dest, "multi-full-refresh-sealed-attempt", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(ctx, table.Name, "lake.events", table.Incarnation, table.SchemaFingerprint))
	require.NoError(t, manager.BeginRun(ctx, false))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: firstSource, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"}, CDCStateManager: manager,
	}
	require.ErrorContains(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, job), "lost atomic publication response")

	restarted, err := NewCDCStateManager(dest, "multi-full-refresh-sealed-attempt", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableState(ctx, table.Name, "lake.events", table.Incarnation, table.SchemaFingerprint))
	require.NoError(t, restarted.BeginRun(ctx, true))
	secondSource := &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(
			source.RecordBatchResult{TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
			source.RecordBatchResult{TableName: table.Name, DurableCommitID: "snapshot:v2", DurableCommitPosition: "0/200"},
		),
	}
	job.Config.FullRefresh = true
	job.Source = secondSource
	job.CDCStateManager = restarted
	job.CDCResumeLSNs = nil
	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(ctx, job))

	require.Empty(t, secondSource.readOpts.CDCResumeLSNs[table.Name])
	dest.publishMu.Lock()
	require.Len(t, dest.attempts, 2)
	require.NotEqual(t, dest.attempts[0], dest.attempts[1])
	require.Equal(t, "0/200", dest.options[1].CDCResumeLSN)
	dest.publishMu.Unlock()
	dest.incarnationMu.Lock()
	require.Len(t, dest.discardedOpts, 1)
	require.Equal(t, dest.attempts[0], dest.discardedOpts[0].AttemptID)
	dest.incarnationMu.Unlock()
	require.Empty(t, restarted.atomicAttempts)
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
				Type: schemaevolution.ChangeWidenType, ColumnName: "value",
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

func TestAtomicCapableSingleTableSnapshotAppliesDiscardRowContract(t *testing.T) {
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
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
	records := mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{
			Batch: batch,
			CommitToken: source.CDCStateCommitToken{SnapshotPositions: map[string]string{
				"public.events": "0/20",
			}},
		},
	)
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	job := &IngestionJob{
		Config: &config.IngestConfig{
			SourceTable: "public.events", DestTable: "lake.events", SchemaContract: "discard_row", NoLoadTimestamp: true,
		},
		Table:             &snapshotResetSourceTable{fakeSourceTable: &fakeSourceTable{readCh: records}},
		Destination:       dest,
		Schema:            sourceSchema,
		SourceSchema:      sourceSchema,
		SchemaComparison:  comparison,
		DestinationSchema: finalSchema,
		EvolutionPlan: &schemaevolution.EvolutionPlan{
			TransformComparison: comparison,
			FinalSchema:         finalSchema,
		},
	}

	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 100})
	require.NoError(t, executor.Execute(context.Background(), job))
	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Empty(t, dest.writeRows)
	require.Len(t, dest.publishes, 1)
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
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
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

func TestAtomicMultiTableBatchLeaseLossDuringSpoolCleansPreparedTargetBeforePublication(t *testing.T) {
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
	dest.atomicMu.Lock()
	require.Len(t, dest.preparedOwnershipTokens, 1)
	require.NotEmpty(t, dest.preparedOwnershipTokens[0])
	require.Equal(t, dest.preparedOwnershipTokens, dest.ownedDropTokens)
	require.Empty(t, dest.begins)
	require.Empty(t, dest.publishes)
	dest.atomicMu.Unlock()
}

func TestAtomicMultiTableBatchLeaseLossAfterStagingAbortsUnpublishedAttempt(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.snapshot", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost after atomic staging")}
	base := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	var once sync.Once
	dest := &leaseLossAfterAtomicWriteDestination{
		atomicSnapshotDestination: base,
		loseLease: func() {
			once.Do(func() { close(lease.done) })
		},
	}
	records := mustClosedRecords(source.RecordBatchResult{
		TableName: table.Name,
		Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
	})
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
			tables: []source.SourceTableInfo{table}, records: records,
		}},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.snapshot"},
	}

	err := (&MergeStrategy{}).ExecuteMultiTable(guardedStreamingContext(lease), job)
	require.ErrorContains(t, err, "lease lost after atomic staging")
	base.atomicMu.Lock()
	require.Empty(t, base.publishes)
	require.Len(t, base.aborts, 1)
	require.Equal(t, base.begins[0].AttemptID, base.aborts[0].AttemptID)
	base.atomicMu.Unlock()
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
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{0}, nil),
		},
		source.RecordBatchResult{TableName: table.Name, Truncate: true, CDCWALTruncate: true},
		source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "snapshot:metadata-v2", DurableCommitPosition: "0/20",
		},
	)
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.snapshot"] = "target-v1"
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "target-v2",
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
	require.Equal(t, atomicSnapshotCommitID(dest.publishedOpts[0].AttemptID), dest.publishedOpts[0].CommitToken)
	dest.incarnationMu.Unlock()
	require.Equal(t, cdcDestinationIncarnationDigest("target-v2"), manager.boundDestinations[table.Name])
}

func TestAtomicMultiTablePreflightRequiresOrderedSnapshotInvalidationBoundary(t *testing.T) {
	const tableName = "public.snapshot"
	states := map[string]*atomicMultiTableBatchState{tableName: {}}
	invalidation := func() source.RecordBatchResult {
		return source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
			TableName: tableName, Incarnation: "source-v2", SchemaFingerprint: "schema-v2",
		}}
	}
	for _, tc := range []struct {
		name    string
		records func(*testing.T) <-chan source.RecordBatchResult
		wantErr string
	}{
		{
			name: "boundary before invalidation",
			records: func(*testing.T) <-chan source.RecordBatchResult {
				return mustClosedRecords(
					source.RecordBatchResult{TableName: tableName, Truncate: true},
					invalidation(),
				)
			},
			wantErr: "was not followed by a replacement boundary",
		},
		{
			name: "duplicate invalidation",
			records: func(*testing.T) <-chan source.RecordBatchResult {
				return mustClosedRecords(invalidation(), invalidation())
			},
			wantErr: "repeated snapshot invalidation",
		},
		{
			name: "data before boundary",
			records: func(t *testing.T) <-chan source.RecordBatchResult {
				return mustClosedRecords(
					invalidation(),
					source.RecordBatchResult{TableName: tableName, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
				)
			},
			wantErr: "was not followed by a replacement boundary",
		},
		{
			name: "WAL truncate is not a replacement boundary",
			records: func(*testing.T) <-chan source.RecordBatchResult {
				return mustClosedRecords(
					invalidation(),
					source.RecordBatchResult{TableName: tableName, Truncate: true, CDCWALTruncate: true},
				)
			},
			wantErr: "was not followed by a replacement boundary",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spool, _, err := spoolMultiTableRecords(t.Context(), tc.records(t), states)
			if spool != nil {
				require.NoError(t, spool.Close())
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestAtomicMultiTablePreflightAllowsPerTableInterleavingAndAnnouncementBeforeBoundary(t *testing.T) {
	const tableName = "public.snapshot"
	const otherTable = "public.other"
	states := map[string]*atomicMultiTableBatchState{tableName: {}, otherTable: {}}
	tableInfo := source.SourceTableInfo{
		Name: tableName, Schema: streamTestSchema(), Incarnation: "source-v2", SchemaFingerprint: "schema-v2",
	}
	records := mustClosedRecords(
		source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
			TableName: tableName,
		}},
		source.RecordBatchResult{TableName: otherTable, Batch: int64RecordBatch(t, "id", []int64{7}, nil)},
		source.RecordBatchResult{TableName: tableName, TableInfo: &tableInfo},
		source.RecordBatchResult{TableName: tableName, Truncate: true},
	)

	spool, resets, err := spoolMultiTableRecords(t.Context(), records, states)
	require.NoError(t, err)
	require.NotNil(t, spool)
	require.NoError(t, spool.Close())
	require.Len(t, spool.invalidations, 1)
	require.Equal(t, "source-v2", spool.invalidations[0].Incarnation)
	require.Equal(t, "schema-v2", spool.invalidations[0].SchemaFingerprint)
	require.Contains(t, resets, tableName)
}

func TestAtomicMultiTablePreflightRejectsConflictingAnnouncementMetadata(t *testing.T) {
	const tableName = "public.snapshot"
	states := map[string]*atomicMultiTableBatchState{tableName: {}}
	tableInfo := source.SourceTableInfo{
		Name: tableName, Schema: streamTestSchema(), Incarnation: "source-v2", SchemaFingerprint: "schema-v3",
	}
	records := mustClosedRecords(
		source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
			TableName: tableName, Incarnation: "source-v2", SchemaFingerprint: "schema-v2",
		}},
		source.RecordBatchResult{TableName: tableName, TableInfo: &tableInfo},
	)

	spool, _, err := spoolMultiTableRecords(t.Context(), records, states)
	if spool != nil {
		require.NoError(t, spool.Close())
	}
	require.ErrorContains(t, err, "conflicting schema metadata")
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
	records := make(chan source.RecordBatchResult)
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
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, IncrementalPredicate: "id >= 100"}, Source: src, Destination: dest, Tables: tables,
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
	require.Equal(t, "id >= 100", dest.mergeCalls[0].IncrementalPredicate)
}

func TestManagedStreamingBindsDestinationBeforeSourceRead(t *testing.T) {
	stateDest := newCDCStateDestination()
	stateDest.incarnations["ds.tbl"] = "target-v1"
	dest := &incarnationFencedAtomicDestination{incarnationTrackingAtomicSnapshotDestination: &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest,
	}}
	manager, err := NewCDCStateManager(dest, "stream-bind-before-read", "ds.tbl", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "src_table", "ds.tbl", "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))

	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.SourceSchema = job.Schema
	job.Config.CDCResumeLSN = "0/10"
	job.Config.FlushRecords = 1
	job.Config.FlushInterval = time.Hour
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch:       int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: source.CDCStateCommitToken{Position: "0/11"},
	})
	job.Table = &beforeReadSourceTable{SourceTable: src, beforeRead: func() {
		require.Equal(t, "target-v1", manager.BoundDestinationIncarnation(job.Config.SourceTable))
		stateDest.stateMu.Lock()
		stateDest.incarnations[job.Config.DestTable] = "target-v2"
		stateDest.stateMu.Unlock()
	}}

	err = NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, FlushRecords: 1, FlushInterval: time.Hour, StateManager: manager,
	}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "physical incarnation changed")
	stateDest.mu.Lock()
	require.Len(t, stateDest.mergeCalls, 1)
	require.Equal(t, "target-v1", stateDest.mergeCalls[0].CDCExpectedIncarnation)
	stateDest.mu.Unlock()
}

func TestManagedMultiTableStreamingBindsDestinationsBeforeSourceRead(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "source-v1"}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-v1"
	dest := &incarnationFencedAtomicDestination{incarnationTrackingAtomicSnapshotDestination: &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest,
	}}
	manager, err := NewCDCStateManager(dest, "multi-stream-bind-before-read", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, "lake.events", table.Incarnation))
	require.NoError(t, manager.BeginRun(t.Context(), false))

	baseSource := &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			CommitToken: source.CDCStateCommitToken{Position: "0/11"},
		}),
	}}
	src := &beforeReadAllSnapshotSource{snapshotResetMultiSource: baseSource, beforeRead: func() {
		require.Equal(t, "target-v1", manager.BoundDestinationIncarnation(table.Name))
		stateDest.stateMu.Lock()
		stateDest.incarnations["lake.events"] = "target-v2"
		stateDest.stateMu.Unlock()
	}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{
			IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true,
			FlushRecords: 1, FlushInterval: time.Hour,
		},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"},
		CDCResumeLSNs:  map[string]string{table.Name: "0/10"}, CDCStateManager: manager,
	}

	err = NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, FlushRecords: 1, FlushInterval: time.Hour, StateManager: manager,
	}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "physical incarnation changed")
	stateDest.mu.Lock()
	require.Len(t, stateDest.mergeCalls, 1)
	require.Equal(t, "target-v1", stateDest.mergeCalls[0].CDCExpectedIncarnation)
	stateDest.mu.Unlock()
}

func TestManagedAtomicMultiTableBatchBindsDestinationsBeforeSourceRead(t *testing.T) {
	tableSchema := testCDCSchema(streamTestSchema())
	table := source.SourceTableInfo{Name: "public.events", Schema: tableSchema, PrimaryKeys: []string{"id"}, Incarnation: "source-v1"}
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-v1"
	dest := &incarnationFencedAtomicDestination{incarnationTrackingAtomicSnapshotDestination: &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest,
	}}
	manager, err := NewCDCStateManager(dest, "multi-batch-bind-before-read", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, "lake.events", table.Incarnation))
	require.NoError(t, manager.BeginRun(t.Context(), false))

	baseSource := &snapshotResetMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
		tables: []source.SourceTableInfo{table},
		records: mustClosedRecords(source.RecordBatchResult{
			TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "wal:resume", DurableCommitPosition: "0/11",
		}),
	}}
	src := &beforeReadAllSnapshotSource{snapshotResetMultiSource: baseSource, beforeRead: func() {
		require.Equal(t, "target-v1", manager.BoundDestinationIncarnation(table.Name))
		stateDest.stateMu.Lock()
		stateDest.incarnations["lake.events"] = "target-v2"
		stateDest.stateMu.Unlock()
	}}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge, NoLoadTimestamp: true},
		Source: src, Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"},
		CDCResumeLSNs:  map[string]string{table.Name: "0/10"}, CDCStateManager: manager,
	}

	err = (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "physical incarnation changed")
	stateDest.mu.Lock()
	require.Len(t, stateDest.mergeCalls, 1)
	require.Equal(t, "target-v1", stateDest.mergeCalls[0].CDCExpectedIncarnation)
	stateDest.mu.Unlock()
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

func (d *atomicSnapshotDestination) PublishAtomicSnapshot(_ context.Context, opts destination.AtomicSnapshotOptions) (string, error) {
	d.atomicMu.Lock()
	defer d.atomicMu.Unlock()
	d.publishes = append(d.publishes, opts)
	return "target-v1", nil
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
	require.Equal(t, atomicSnapshotCommitID(dest.begins[0].AttemptID), dest.publishes[0].CommitToken)
	require.Equal(t, "0/100", dest.publishes[0].CDCResumeLSN)
	require.False(t, st.atomicSnapshot)
}

func TestStreamingAtomicSnapshotCombinedPagesDoNotUseFinalPageIdentity(t *testing.T) {
	dest := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	require.NoError(t, loop.handleSnapshotReset(t.Context(), "public.events"))
	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID: "snapshot-page-a",
	})
	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
		DurableCommitID: "snapshot-page-b", DurableCommitPosition: "0/100",
		CommitToken: snapshotStateToken("public.events", "0/100"),
	})
	require.NoError(t, loop.flush(t.Context()))

	dest.atomicMu.Lock()
	defer dest.atomicMu.Unlock()
	require.Len(t, dest.writes, 1)
	require.Empty(t, dest.writes[0].CommitToken, "two staged pages cannot use only the final page identity")
	require.Len(t, dest.publishes, 1)
	require.Equal(t, atomicSnapshotCommitID(dest.begins[0].AttemptID), dest.publishes[0].CommitToken)
	require.Equal(t, "0/100", dest.publishes[0].CDCResumeLSN)
}

func TestStreamingAtomicSnapshotRetriesAmbiguousPublishWithSameIdentity(t *testing.T) {
	base := &atomicSnapshotDestination{fakeDestination: &fakeDestination{}}
	dest := &ambiguousPublishAtomicDestination{atomicSnapshotDestination: base}
	committer := &fakeCommitter{}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge, Committer: committer,
	}, map[string]*streamTableState{"public.events": st})

	require.NoError(t, loop.handleSnapshotReset(t.Context(), "public.events"))
	attemptID := st.snapshotAttemptID
	loop.buffer(source.RecordBatchResult{
		TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID: "snapshot-page-final", DurableCommitPosition: "0/100",
		CommitToken: snapshotStateToken("public.events", "0/100"),
	})

	err := loop.flush(t.Context())
	require.ErrorContains(t, err, "lost publish response")
	require.True(t, st.atomicSnapshot)
	require.True(t, st.atomicPublishAttempted)
	require.Equal(t, attemptID, st.snapshotAttemptID)
	require.Empty(t, committer.committed())
	loop.cleanup(t.Context())
	base.atomicMu.Lock()
	require.Empty(t, base.aborts, "an attempted publication has an ambiguous outcome and must never be aborted")
	base.atomicMu.Unlock()

	require.NoError(t, loop.flush(t.Context()))
	require.False(t, st.atomicSnapshot)
	require.False(t, st.atomicPublishAttempted)
	require.Equal(t, []any{snapshotStateToken("public.events", "0/100")}, committer.committed())
	base.atomicMu.Lock()
	defer base.atomicMu.Unlock()
	require.Len(t, base.writes, 1, "retrying publication must not restage pages")
	require.Len(t, base.publishes, 2)
	for _, publish := range base.publishes {
		require.Equal(t, attemptID, publish.AttemptID)
		require.Equal(t, atomicSnapshotCommitID(attemptID), publish.CommitToken)
	}
}

func TestStreamingManagedAtomicSnapshotRecoversSealedPublicationBeforeResumeRead(t *testing.T) {
	ctx := t.Context()
	stateDest := newCDCStateDestination()
	base := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination:  stateDest,
		publishedIncarnation: "destination-published",
	}
	dest := &ambiguousManagedAtomicSnapshotDestination{incarnationTrackingAtomicSnapshotDestination: base}
	job, src, _ := minimalJob()
	job.Destination = dest
	job.Schema = testCDCSchema(job.Schema)
	job.SourceSchema = job.Schema
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.FlushInterval = time.Hour
	job.Config.FlushRecords = 100
	job.Table = &snapshotResetSourceTable{fakeSourceTable: src}
	stateDest.incarnations[job.Config.DestTable] = "destination-before"

	first, err := NewCDCStateManager(dest, "stream-ambiguous-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, first.BeginRun(ctx, false))
	job.CDCStateManager = first
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Truncate: true},
		source.RecordBatchResult{
			Batch:                 int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID:       "snapshot-page-final",
			DurableCommitPosition: "0/900",
			CommitToken:           snapshotStateToken(job.Config.SourceTable, "0/900"),
		},
	)
	executor := NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		StateManager:  first,
	})
	err = executor.Execute(ctx, job)
	require.ErrorContains(t, err, "lost atomic publication response")

	restarted, err := NewCDCStateManager(dest, "stream-ambiguous-restart", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, restarted.BeginRun(ctx, false))
	job.CDCStateManager = restarted
	job.Config.CDCResumeLSN = ""
	src.mu.Lock()
	src.readCalled = false
	src.readErr = nil
	src.readCh = mustClosedRecords()
	src.mu.Unlock()
	executor = NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		StateManager:  restarted,
	})
	require.NoError(t, executor.Execute(ctx, job))

	src.mu.Lock()
	require.True(t, src.readCalled)
	require.Equal(t, "0/900", src.readOpts.CDCResumeLSN)
	src.mu.Unlock()
	dest.publishMu.Lock()
	require.Len(t, dest.options, 2)
	require.Equal(t, dest.options[0].AttemptID, dest.options[1].AttemptID)
	dest.publishMu.Unlock()
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
	require.Equal(t, atomicSnapshotCommitID(dest.begins[0].AttemptID), dest.publishes[0].CommitToken)
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
	require.Equal(t, atomicSnapshotCommitID(dest.begins[0].AttemptID), dest.publishes[0].CommitToken)
	require.Equal(t, "0/100", dest.publishes[0].CDCResumeLSN)
	dest.atomicMu.Unlock()
	dest.mu.Lock()
	require.Len(t, dest.writeCalls, 1, "following WAL batch uses the normal merge staging path")
	require.Empty(t, dest.writeCalls[0].CommitToken)
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
	stateDest := newCDCStateDestination()
	stateDest.incarnations["lake.events"] = "target-v1"
	dest := &incarnationTrackingAtomicSnapshotDestination{
		cdcStateDestination: stateDest, publishedIncarnation: "target-v2",
	}
	manager, err := NewCDCStateManager(dest, "streaming-schema-refresh-fence", "lake.events", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(t.Context(), "public.events", "lake.events", "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		StateManager:  manager,
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

	dest.incarnationMu.Lock()
	defer dest.incarnationMu.Unlock()
	require.Len(t, dest.writtenOpts, 1, "old-shape pages are durably consumed before restarting")
	require.Equal(t, "target-v1", dest.writtenOpts[0].CDCExpectedIncarnation)
	require.Len(t, dest.begunOpts, 1)
	require.Equal(t, "target-v1", dest.begunOpts[0].CDCExpectedIncarnation)
	require.Len(t, dest.evolvedOpts, 1, "schema refresh must evolve hidden staging without changing the target")
	require.Equal(t, "target-v1", dest.evolvedOpts[0].CDCExpectedIncarnation)
	require.Empty(t, dest.publishedOpts)
	require.Equal(t, "added", dest.evolvedOpts[0].Schema.Columns[len(dest.evolvedOpts[0].Schema.Columns)-1].Name)
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
	require.Equal(t, atomicSnapshotCommitID(dest.begins[0].AttemptID), dest.publishes[0].CommitToken)
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
