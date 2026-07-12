package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type atomicTruncateInsertDestination struct {
	*fakeDestination
	calls           []destination.WriteOptions
	receivedIDs     []int64
	receivedColumns [][]string
	err             error
}

type atomicSchemaEvolvingTruncateDestination struct {
	*atomicTruncateInsertDestination
}

type immediateFailAtomicTruncateDestination struct {
	*fakeDestination
}

type boundaryOnlyAtomicTruncateDestination struct {
	*immediateFailAtomicTruncateDestination
}

type ownedAtomicTruncateDestination struct {
	*atomicTruncateInsertDestination
	preparedToken string
	dropTokens    []string
}

type leaseLosingOwnedAtomicDestination struct {
	*ownedAtomicTruncateDestination
	lease *streamingTestLease
}

type recreatingAtomicTruncateDestination struct {
	*cdcStateDestination
	calls []destination.WriteOptions
}

type leaseLosingReadSource struct {
	*fakeSourceTable
	lease *streamingTestLease
}

type leaseLosingOwnedTruncateDestination struct {
	*ownedTruncateDestination
	lease          *streamingTestLease
	loseAfterWrite bool
	loseAfterMerge bool
}

func (s *leaseLosingReadSource) Read(
	ctx context.Context,
	opts source.ReadOptions,
) (<-chan source.RecordBatchResult, error) {
	records, err := s.fakeSourceTable.Read(ctx, opts)
	close(s.lease.done)
	return records, err
}

func (d *leaseLosingOwnedTruncateDestination) WriteParallel(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	err := d.ownedTruncateDestination.WriteParallel(ctx, records, opts)
	if err == nil && d.loseAfterWrite {
		close(d.lease.done)
	}
	return err
}

func (d *leaseLosingOwnedTruncateDestination) MergeTable(
	ctx context.Context,
	opts destination.MergeOptions,
) error {
	err := d.ownedTruncateDestination.MergeTable(ctx, opts)
	if err == nil && d.loseAfterMerge {
		close(d.lease.done)
	}
	return err
}

type conditionalTruncateCall struct {
	table       string
	incarnation string
}

type managedTruncateInsertDestination struct {
	*cdcStateDestination
	dataWrites             []destination.WriteOptions
	dataColumns            [][]string
	merges                 []destination.MergeOptions
	truncates              []string
	conditionalTruncates   []conditionalTruncateCall
	replaceBeforeEvolution bool
}

func (d *managedTruncateInsertDestination) WriteParallel(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.dataWrites = append(d.dataWrites, opts)
	for result := range records {
		if result.Batch != nil {
			columns := make([]string, result.Batch.NumCols())
			for i := range columns {
				columns[i] = result.Batch.ColumnName(i)
			}
			d.dataColumns = append(d.dataColumns, columns)
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func (d *managedTruncateInsertDestination) MergeTable(_ context.Context, opts destination.MergeOptions) error {
	d.merges = append(d.merges, opts)
	return nil
}

func (d *managedTruncateInsertDestination) TruncateTable(_ context.Context, table string) error {
	d.truncates = append(d.truncates, table)
	return nil
}

func (d *managedTruncateInsertDestination) TruncateCDCTableIfIncarnation(
	ctx context.Context,
	table string,
	expected string,
) error {
	d.conditionalTruncates = append(d.conditionalTruncates, conditionalTruncateCall{
		table:       table,
		incarnation: expected,
	})
	return d.cdcStateDestination.TruncateCDCTableIfIncarnation(ctx, table, expected)
}

func (*managedTruncateInsertDestination) SupportsCDCUnchangedCols() bool {
	return true
}

func (d *managedTruncateInsertDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expected string,
) ([]string, error) {
	d.stateMu.Lock()
	if d.replaceBeforeEvolution {
		d.incarnations[table] = "replacement-v2"
		d.replaceBeforeEvolution = false
	}
	current := d.incarnations[table]
	d.stateMu.Unlock()
	if current != expected {
		return nil, errors.New("destination physical incarnation changed before schema evolution")
	}
	return d.ApplySchemaEvolution(ctx, table, comparison)
}

func managedTruncateInsertJob(t *testing.T, withPrimaryKey bool) (*IngestionJob, *fakeSourceTable, *managedTruncateInsertDestination) {
	t.Helper()
	job, src, _ := minimalJob()
	job.Config.FullRefresh = true
	job.Schema.Columns = append(
		job.Schema.Columns,
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
		schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
	)
	job.SourceSchema = job.Schema
	if !withPrimaryKey {
		job.Config.PrimaryKeys = nil
		job.Schema.PrimaryKeys = nil
		src.primaryKeys = nil
	}

	stateDest := newCDCStateDestination()
	stateDest.incarnations[job.Config.DestTable] = "target-v1"
	dest := &managedTruncateInsertDestination{cdcStateDestination: stateDest}
	job.Destination = dest
	manager, err := NewCDCStateManager(dest, "managed-truncate-insert", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(
		t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1", "schema-v1",
	))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.CDCStateManager = manager
	return job, src, dest
}

func managedTruncateInsertRecords(t *testing.T) <-chan source.RecordBatchResult {
	t.Helper()
	first := "one"
	second := "two"
	return mustClosedRecords(
		source.RecordBatchResult{Batch: cdcRenameRecordBatch(t, "name", &first, `["name"]`)},
		source.RecordBatchResult{Truncate: true, CDCWALTruncate: true},
		source.RecordBatchResult{Batch: cdcRenameRecordBatch(t, "name", &second, `[]`)},
	)
}

func TestManagedDirectTruncateInsertFencesWritesAndSourceTruncates(t *testing.T) {
	job, src, dest := managedTruncateInsertJob(t, false)
	src.readCh = managedTruncateInsertRecords(t)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.True(t, src.readOpts.CDCSnapshotReplace)
	require.Equal(t, []conditionalTruncateCall{
		{table: job.Config.DestTable, incarnation: "target-v1"},
		{table: job.Config.DestTable, incarnation: "target-v1"},
	}, dest.conditionalTruncates)
	require.Len(t, dest.dataWrites, 2)
	for _, opts := range dest.dataWrites {
		require.Equal(t, job.Config.DestTable, opts.Table)
		require.Equal(t, "target-v1", opts.CDCExpectedIncarnation)
		require.True(t, opts.SkipCDCResume)
		require.False(t, opts.StagingTable)
	}
	require.Equal(t, [][]string{{"name"}, {"name"}}, dest.dataColumns)
	require.Empty(t, dest.truncates)
}

func TestManagedStagedTruncateInsertPreservesCDCMarkersAndFencesPublication(t *testing.T) {
	job, src, dest := managedTruncateInsertJob(t, true)
	src.readCh = managedTruncateInsertRecords(t)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.True(t, src.readOpts.CDCSnapshotReplace)
	require.Equal(t, []conditionalTruncateCall{
		{table: job.Config.DestTable, incarnation: "target-v1"},
	}, dest.conditionalTruncates)
	require.Len(t, dest.dataWrites, 2)
	stagingTable := dest.dataWrites[0].Table
	require.NotEqual(t, job.Config.DestTable, stagingTable)
	for _, opts := range dest.dataWrites {
		require.Equal(t, stagingTable, opts.Table)
		require.Empty(t, opts.CDCExpectedIncarnation)
		require.True(t, opts.SkipCDCResume)
		require.True(t, opts.StagingTable)
	}
	for _, columns := range dest.dataColumns {
		require.Contains(t, columns, destination.CDCUnchangedColsColumn)
	}
	require.Equal(t, []string{stagingTable}, dest.truncates)
	require.Len(t, dest.merges, 1)
	require.Equal(t, "target-v1", dest.merges[0].CDCExpectedIncarnation)
	require.True(t, dest.merges[0].SkipCDCResume)
	require.Contains(t, dest.merges[0].Columns, destination.CDCUnchangedColsColumn)
}

func TestTruncateInsertLeaseLossCleansOwnedTargetBeforeMutation(t *testing.T) {
	for _, withPrimaryKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "staged"}[withPrimaryKey], func(t *testing.T) {
			job, src, base := minimalJob()
			job.EvolutionPlan = &schemaevolution.EvolutionPlan{
				Table: job.Config.DestTable,
				Comparison: &schemaevolution.SchemaComparison{
					HasChanges: true,
					Changes: []schemaevolution.SchemaChange{{
						Type: schemaevolution.ChangeAddColumn, ColumnName: "must_not_be_added",
					}},
				},
			}
			if !withPrimaryKey {
				job.Config.PrimaryKeys = nil
				job.Schema.PrimaryKeys = nil
				src.primaryKeys = nil
			}
			src.readCh = mustClosedRecords()
			dest := &ownedTruncateDestination{
				truncateCapableDestination: &truncateCapableDestination{fakeDestination: base},
			}
			job.Destination = dest
			lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease connection lost")}
			close(lease.done)

			err := (&TruncateInsertStrategy{}).Execute(guardedStreamingContext(lease), job)
			require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
			require.NotEmpty(t, dest.preparedToken)
			require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			require.Empty(t, base.truncateCalls)
			require.Empty(t, base.execCalls)
		})
	}
}

func TestTruncateInsertAtomicLeaseLossBeforeEvolutionSkipsDDLAndPublication(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable,
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type: schemaevolution.ChangeAddColumn, ColumnName: "must_not_be_added",
			}},
		},
	}
	src.readCh = mustClosedRecords()
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease connection lost")}
	close(lease.done)

	err := (&TruncateInsertStrategy{}).Execute(guardedStreamingContext(lease), job)
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	require.Empty(t, base.execCalls)
	require.Empty(t, atomic.calls)
	require.False(t, src.readCalled)
}

func TestTruncateInsertLeaseLossAfterPublicationCleansOwnedNewTarget(t *testing.T) {
	t.Run("atomic", func(t *testing.T) {
		job, src, base := minimalJob()
		src.readCh = singleBatchRecords(t, 1)
		lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease connection lost")}
		owned := &ownedAtomicTruncateDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{
			fakeDestination: base,
		}}
		dest := &leaseLosingOwnedAtomicDestination{ownedAtomicTruncateDestination: owned, lease: lease}
		job.Destination = dest

		err := (&TruncateInsertStrategy{}).Execute(guardedStreamingContext(lease), job)
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
		require.Equal(t, []string{owned.preparedToken}, owned.dropTokens)
	})

	t.Run("direct", func(t *testing.T) {
		job, src, base := minimalJob()
		job.Config.PrimaryKeys = nil
		job.Schema.PrimaryKeys = nil
		src.primaryKeys = nil
		src.readCh = singleBatchRecords(t, 1)
		lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease connection lost")}
		owned := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
		dest := &leaseLosingOwnedTruncateDestination{
			ownedTruncateDestination: owned, lease: lease, loseAfterWrite: true,
		}
		job.Destination = dest

		err := (&TruncateInsertStrategy{}).Execute(guardedStreamingContext(lease), job)
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
		require.Equal(t, []string{owned.preparedToken}, owned.dropTokens)
	})

	t.Run("staged", func(t *testing.T) {
		job, src, base := minimalJob()
		src.readCh = singleBatchRecords(t, 1)
		lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease connection lost")}
		owned := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
		dest := &leaseLosingOwnedTruncateDestination{
			ownedTruncateDestination: owned, lease: lease, loseAfterMerge: true,
		}
		job.Destination = dest

		err := (&TruncateInsertStrategy{}).Execute(guardedStreamingContext(lease), job)
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
		require.Equal(t, []string{owned.preparedToken}, owned.dropTokens)
	})
}

func TestManagedTruncateInsertFencesSchemaEvolution(t *testing.T) {
	for _, withPrimaryKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "staged"}[withPrimaryKey], func(t *testing.T) {
			job, src, dest := managedTruncateInsertJob(t, withPrimaryKey)
			if withPrimaryKey {
				src.readCh = mustClosedRecords()
			}
			job.EvolutionPlan = &schemaevolution.EvolutionPlan{
				Table: job.Config.DestTable,
				Comparison: &schemaevolution.SchemaComparison{
					HasChanges: true,
					Changes: []schemaevolution.SchemaChange{{
						Type:       schemaevolution.ChangeAddColumn,
						ColumnName: "new_column",
						NewColumn: schema.Column{
							Name: "new_column", DataType: schema.TypeString, Nullable: true,
						},
					}},
				},
			}
			dest.replaceBeforeEvolution = true

			err := (&TruncateInsertStrategy{}).Execute(t.Context(), job)
			require.ErrorContains(t, err, "incarnation changed before schema evolution")
			require.Equal(t, withPrimaryKey, src.readCalled)
			require.Empty(t, dest.execCalls)
			if withPrimaryKey {
				require.Len(t, dest.dataWrites, 1)
				require.True(t, dest.dataWrites[0].StagingTable)
			} else {
				require.Empty(t, dest.dataWrites)
			}
			require.Empty(t, dest.conditionalTruncates)
		})
	}
}

func (d *recreatingAtomicTruncateDestination) TruncateInsertRecords(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.calls = append(d.calls, opts)
	d.stateMu.Lock()
	d.incarnations[opts.Table] = "external-v2"
	d.stateMu.Unlock()
	current, _, _ := d.CDCTargetIncarnation(ctx, opts.Table)
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	if opts.CDCExpectedIncarnation != current {
		return errors.New("destination incarnation changed before atomic truncate+insert")
	}
	return nil
}

func TestManagedAtomicTruncateInsertRejectsTargetRecreationDuringExtraction(t *testing.T) {
	job, src, _ := minimalJob()
	stateDest := newCDCStateDestination()
	stateDest.incarnations[job.Config.DestTable] = "target-v1"
	dest := &recreatingAtomicTruncateDestination{cdcStateDestination: stateDest}
	job.Destination = dest
	manager, err := NewCDCStateManager(dest, "truncate-insert-recreation", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.CDCStateManager = manager
	src.readCh = singleBatchRecords(t, 1)

	err = (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed")
	require.Len(t, dest.calls, 1)
	require.Equal(t, "target-v1", dest.calls[0].CDCExpectedIncarnation)
	require.True(t, dest.calls[0].SkipCDCResume)
}

func (d *ownedAtomicTruncateDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Table != "" && opts.OwnershipToken != "" {
		d.preparedToken = opts.OwnershipToken
	}
	return d.atomicTruncateInsertDestination.PrepareTable(ctx, opts)
}

func (d *ownedAtomicTruncateDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.dropTokens = append(d.dropTokens, token)
	return errors.New("ownership changed")
}

func (d *leaseLosingOwnedAtomicDestination) TruncateInsertRecords(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	err := d.ownedAtomicTruncateDestination.TruncateInsertRecords(ctx, records, opts)
	if err == nil {
		close(d.lease.done)
	}
	return err
}

type ownedTruncateDestination struct {
	*truncateCapableDestination
	preparedToken string
	dropTokens    []string
}

type uncertainStagingTruncateDestination struct {
	*uncertainManagedStagingPrepareDestination
}

func (d *uncertainStagingTruncateDestination) TruncateTable(context.Context, string) error {
	return nil
}

func (d *ownedTruncateDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.OwnershipToken != "" {
		d.preparedToken = opts.OwnershipToken
	}
	return d.truncateCapableDestination.PrepareTable(ctx, opts)
}

func TestTruncateInsertLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	job, _, base := minimalJob()
	job.Config.PrimaryKeys = []string{"id"}
	dest := &uncertainStagingTruncateDestination{uncertainManagedStagingPrepareDestination: &uncertainManagedStagingPrepareDestination{
		contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base},
	}}
	job.Destination = dest
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorContains(t, (&TruncateInsertStrategy{}).Execute(ctx, job), "prepare response lost")
	require.Len(t, base.prepareCalls, 2)
	require.Equal(t, []string{base.prepareCalls[1].Table}, dest.successfulDrops)
}

func (d *ownedTruncateDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.dropTokens = append(d.dropTokens, token)
	return errors.New("ownership changed")
}

func (d *immediateFailAtomicTruncateDestination) TruncateInsertRecords(
	context.Context,
	<-chan source.RecordBatchResult,
	destination.WriteOptions,
) error {
	return errors.New("atomic writer failed")
}

func (d *atomicSchemaEvolvingTruncateDestination) EvolvesTruncateInsertSchemaAtomically() bool {
	return true
}

func (d *atomicTruncateInsertDestination) TruncateInsertRecords(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.calls = append(d.calls, opts)
	for result := range records {
		if result.Truncate {
			d.receivedIDs = nil
		}
		if result.Batch != nil {
			columns := make([]string, result.Batch.NumCols())
			for column := range columns {
				columns[column] = result.Batch.ColumnName(column)
			}
			d.receivedColumns = append(d.receivedColumns, columns)
			if result.Batch.NumCols() > 0 {
				if values, ok := result.Batch.Column(0).(*array.Int64); ok {
					for row := 0; row < values.Len(); row++ {
						if !values.IsNull(row) {
							d.receivedIDs = append(d.receivedIDs, values.Value(row))
						}
					}
				}
			}
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return d.err
}

func (*atomicTruncateInsertDestination) SupportsTruncateInsertBoundaries() bool {
	return true
}

func (*atomicTruncateInsertDestination) SupportsTruncateInsertStagingColumns() bool {
	return true
}

func (*boundaryOnlyAtomicTruncateDestination) SupportsTruncateInsertBoundaries() bool {
	return true
}

func TestTruncateInsertStrategy_PrefersAtomicWriter(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.readCh = singleBatchRecords(t, 1, 1, 2)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	if len(atomic.calls) != 1 {
		t.Fatalf("expected one atomic write, got %d", len(atomic.calls))
	}
	if !atomic.calls[0].DeduplicatePrimaryKeys {
		t.Fatal("atomic truncate+insert did not request primary-key deduplication")
	}
	require.Len(t, base.prepareCalls, 1)
	require.True(t, base.prepareCalls[0].PreserveExistingLayout)
	if len(base.truncateCalls) != 0 || len(base.mergeCalls) != 0 {
		t.Fatalf("atomic path must not truncate or merge separately: truncate=%v merge=%v", base.truncateCalls, base.mergeCalls)
	}
}

func TestTruncateInsertStrategy_AtomicWriterForwardsCDCSlotSuffixes(t *testing.T) {
	job, src, base := minimalJob()
	job.Destination = &atomicTruncateInsertDestination{fakeDestination: base}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	job.Schema.Columns = append(
		job.Schema.Columns,
		schema.Column{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		schema.Column{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
	)
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Equal(t, job.Config.CDCSlotSuffix, src.readOpts.CDCSlotSuffix)
	require.Equal(t, job.Config.CDCLegacySlotSuffix, src.readOpts.CDCLegacySlotSuffix)
	require.True(t, src.readOpts.CDCSnapshotReplace)
}

func TestTruncateInsertStrategy_AtomicWriterDiscardsRowsBeforeCDCTruncateBoundary(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.Schema.Columns = append(
		job.Schema.Columns,
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
	)
	job.SourceSchema = job.Schema
	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
		source.RecordBatchResult{Truncate: true, CDCWALTruncate: true},
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
	)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.True(t, src.readOpts.CDCSnapshotReplace)
	require.Equal(t, []int64{2}, atomic.receivedIDs)
}

func TestTruncateInsertStrategy_RejectsAtomicCDCWriterWithoutBoundarySupport(t *testing.T) {
	job, _, base := minimalJob()
	job.Destination = &immediateFailAtomicTruncateDestination{fakeDestination: base}
	job.Schema.Columns = append(
		job.Schema.Columns,
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
	)

	err := (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "cannot preserve CDC truncate boundaries")
	require.Empty(t, base.prepareCalls)
}

func TestTruncateInsertStrategy_AtomicWriterSeparatesStagingAndTargetSchemas(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.Schema.Columns = append(
		job.Schema.Columns,
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
	)
	job.SourceSchema = job.Schema
	name := "value"
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch: cdcRenameRecordBatch(t, "name", &name, `["name"]`),
	})

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Len(t, atomic.calls, 1)
	require.Contains(t, atomic.calls[0].Schema.ColumnNames(), destination.CDCUnchangedColsColumn)
	require.NotContains(t, atomic.calls[0].TargetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
	require.Len(t, atomic.receivedColumns, 1)
	require.Contains(t, atomic.receivedColumns[0], destination.CDCUnchangedColsColumn)
}

func TestTruncateInsertStrategy_RejectsAtomicWriterWithoutStagingColumnSupport(t *testing.T) {
	job, _, base := minimalJob()
	job.Destination = &boundaryOnlyAtomicTruncateDestination{
		immediateFailAtomicTruncateDestination: &immediateFailAtomicTruncateDestination{fakeDestination: base},
	}
	job.Schema.Columns = append(
		job.Schema.Columns,
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
	)

	err := (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "cannot keep staging-only CDC columns out of the target")
	require.Empty(t, base.prepareCalls)
}

func TestTruncateInsertStrategy_AtomicLeaseLossAfterReadSkipsPublication(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	src.readCh = mustClosedRecords()
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease connection lost")}
	job.Table = &leaseLosingReadSource{fakeSourceTable: src, lease: lease}

	err := (&TruncateInsertStrategy{}).Execute(guardedStreamingContext(lease), job)
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	require.True(t, src.readCalled)
	require.Empty(t, atomic.calls)
}

// Atomic writers without AtomicTruncateInsertSchemaEvolver still rely on the
// strategy to apply the pending evolution plan before writing.
func TestTruncateInsertStrategy_AtomicWriterAppliesEvolution(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	base.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: job.Schema}
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable,
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "new_column",
			}},
		},
	}
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Len(t, base.execCalls, 1)
	require.Contains(t, base.execCalls[0].sql, "ADD COLUMN new_column")
	require.Nil(t, job.EvolutionPlan.Comparison, "the evolution plan must be applied before the atomic write")
}

func TestTruncateInsertStrategy_AtomicSchemaEvolverOwnsEvolution(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicSchemaEvolvingTruncateDestination{
		atomicTruncateInsertDestination: &atomicTruncateInsertDestination{fakeDestination: base},
	}
	job.Destination = atomic
	base.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: job.Schema}
	comparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeAddColumn, ColumnName: "new_column",
		}},
	}
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{Table: job.Config.DestTable, Comparison: comparison}
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Empty(t, base.execCalls)
	require.Same(t, comparison, job.EvolutionPlan.Comparison)
	require.Len(t, atomic.calls, 1)
}

func TestTruncateInsertStrategy_AtomicSchemaEvolverSoftensRemovedRequiredColumn(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicSchemaEvolvingTruncateDestination{
		atomicTruncateInsertDestination: &atomicTruncateInsertDestination{fakeDestination: base},
	}
	job.Destination = atomic
	destinationSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "legacy", DataType: schema.TypeString, Nullable: false},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeRemoveColumn, ColumnName: "legacy",
		OldColumn: &destinationSchema.Columns[1],
	}}}
	job.Schema = schemaevolution.BuildFinalSchema(destinationSchema, comparison)
	job.SourceSchema = &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable, Comparison: comparison, FinalSchema: job.Schema,
	}
	base.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: destinationSchema}
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Len(t, atomic.calls, 1)
	require.Len(t, atomic.calls[0].Schema.Columns, 2)
	require.Equal(t, "legacy", atomic.calls[0].Schema.Columns[1].Name)
	require.True(t, atomic.calls[0].Schema.Columns[1].Nullable)
}

func TestTruncateInsertStrategy_AtomicWriterSkipsMissingOrderingKey(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.IncrementalKey = "missing_ordering_column"
	src.readCh = singleBatchRecords(t, 1, 1, 2)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Len(t, atomic.calls, 1)
	require.Empty(t, atomic.calls[0].IncrementalKey)
}

func TestTruncateInsertStrategy_AtomicFailurePreservesNewTargetForOutcomeReconciliation(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{
		fakeDestination: base,
		err:             errors.New("atomic commit failed"),
	}
	job.Destination = atomic
	src.readCh = mustClosedRecords()

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected atomic truncate+insert to fail")
	}
	require.Empty(t, base.dropCalls, "atomic write errors may be lost success responses and must preserve the target")
}

func TestTruncateInsertStrategy_AtomicFailureDoesNotAttemptCleanupAfterPossibleCommit(t *testing.T) {
	job, src, base := minimalJob()
	dest := &ownedAtomicTruncateDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{
		fakeDestination: base,
		err:             errors.New("atomic commit failed after replacement"),
	}}
	job.Destination = dest
	src.readCh = mustClosedRecords()

	require.Error(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "conditional ownership is insufficient after a possibly successful commit")
	require.Empty(t, base.dropCalls, "unconditional cleanup must not delete a replacement owner")
}

func TestTruncateInsertStrategy_LostPrepareResponseCleansOwnedTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		make   func(*fakeDestination) destination.Destination
		assert func(*testing.T, destination.Destination)
	}{
		{
			name: "atomic",
			make: func(base *fakeDestination) destination.Destination {
				return &ownedAtomicTruncateDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{fakeDestination: base}}
			},
			assert: func(t *testing.T, raw destination.Destination) {
				dest := raw.(*ownedAtomicTruncateDestination)
				require.NotEmpty(t, dest.preparedToken)
				require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			},
		},
		{
			name: "direct",
			make: func(base *fakeDestination) destination.Destination {
				return &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
			},
			assert: func(t *testing.T, raw destination.Destination) {
				dest := raw.(*ownedTruncateDestination)
				require.NotEmpty(t, dest.preparedToken)
				require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			},
		},
		{
			name: "staged",
			make: func(base *fakeDestination) destination.Destination {
				return &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
			},
			assert: func(t *testing.T, raw destination.Destination) {
				dest := raw.(*ownedTruncateDestination)
				require.NotEmpty(t, dest.preparedToken)
				require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job, src, base := minimalJob()
			base.prepareHook = func(destination.PrepareOptions) error { return errors.New("prepare response lost after create") }
			if tc.name == "direct" {
				job.Config.PrimaryKeys = nil
				job.Schema.PrimaryKeys = nil
				src.primaryKeys = nil
			}
			raw := tc.make(base)
			job.Destination = raw

			require.ErrorContains(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job), "prepare response lost")
			tc.assert(t, raw)
			require.Empty(t, base.dropCalls)
		})
	}
}

func TestTruncateInsertStrategy_AtomicWriterFailureBoundsNonclosingSource(t *testing.T) {
	previousTimeout := canceledSourceDrainTimeout
	canceledSourceDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSourceDrainTimeout = previousTimeout })
	job, src, base := minimalJob()
	records := make(chan source.RecordBatchResult)
	src.readCh = records
	job.Destination = &immediateFailAtomicTruncateDestination{fakeDestination: base}

	started := time.Now()
	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("truncate+insert did not return promptly after writer failure: %v", err)
	}
	close(records)
}

func TestTruncateInsertStrategy_Execute_PassesFullRefreshToRead(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.FullRefresh = true
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.readCh = mustClosedRecords()

	strat := &TruncateInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	src.mu.Lock()
	defer src.mu.Unlock()
	if !src.readOpts.FullRefresh {
		t.Fatalf("ReadOptions.FullRefresh = false, want true")
	}
	if src.readOpts.CDCSlotSuffix != job.Config.CDCSlotSuffix {
		t.Fatalf("ReadOptions.CDCSlotSuffix = %q, want leased slot suffix %q", src.readOpts.CDCSlotSuffix, job.Config.CDCSlotSuffix)
	}
	if src.readOpts.CDCLegacySlotSuffix != job.Config.CDCLegacySlotSuffix {
		t.Fatalf("full-refresh legacy slot suffix = %q, want %q",
			src.readOpts.CDCLegacySlotSuffix, job.Config.CDCLegacySlotSuffix)
	}
	if src.readOpts.CDCResumeLSN != "" {
		t.Fatalf("full refresh unexpectedly supplied resume LSN %q, which could select a previous or legacy slot", src.readOpts.CDCResumeLSN)
	}
}

func TestTruncateInsertStrategy_ExecuteWithStaging_FullRefreshUsesLeasedSlotSuffix(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.FullRefresh = true
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.True(t, src.readOpts.FullRefresh)
	require.Equal(t, job.Config.CDCSlotSuffix, src.readOpts.CDCSlotSuffix)
	require.Equal(t, job.Config.CDCLegacySlotSuffix, src.readOpts.CDCLegacySlotSuffix)
	require.Empty(t, src.readOpts.CDCResumeLSN, "full refresh must not select a previous or legacy slot")
}

func TestTruncateInsertStrategy_Execute_SkipsOrderingKeyMissingFromStagingSchema(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.IncrementalKey = "updated_at"
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{Name: "_cdc_unchanged_cols", DataType: schema.TypeString, Nullable: true})
	src.readCh = mustClosedRecords()

	strat := &TruncateInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected 1 MergeTable call, got %d", len(dest.mergeCalls))
	}
	if dest.mergeCalls[0].IncrementalKey != "" {
		t.Fatalf("MergeOptions.IncrementalKey = %q, want empty for missing staging column", dest.mergeCalls[0].IncrementalKey)
	}
	require.NotContains(t, dest.mergeCalls[0].Columns, "_cdc_unchanged_cols")
}

func TestTruncateInsertStrategy_Execute_ReadFailsBeforeTruncateWithPrimaryKeys(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.primaryKeysUnique = true
	src.readErr = errors.New("read failed")

	strat := &TruncateInsertStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get records") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dest.truncateCalls) != 0 {
		t.Fatalf("expected no truncate before source read succeeds, got %v", dest.truncateCalls)
	}
	if len(dest.writeCalls) != 0 {
		t.Fatalf("expected no write when source read setup fails, got %d", len(dest.writeCalls))
	}
	require.Equal(t, []string{dest.prepareCalls[1].Table}, dest.dropCalls)
}

func TestTruncateInsertStrategy_DirectReadSetupFailurePreservesConcurrentUnownedTarget(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readErr = errors.New("read setup failed")
	dest.prepareHook = func(opts destination.PrepareOptions) error {
		if opts.Table == job.Config.DestTable {
			dest.tableSchemas = map[string]*schema.TableSchema{opts.Table: job.Schema}
		}
		return nil
	}

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected read setup failure")
	}
	if len(dest.truncateCalls) != 0 {
		t.Fatalf("target was truncated before source setup succeeded: %v", dest.truncateCalls)
	}
	require.NotNil(t, dest.tableSchemas[job.Config.DestTable])
	require.Empty(t, dest.dropCalls, "an unowned target may have been created by another writer")
}

func TestTruncateInsertStrategy_StagingFailureDropsManagedTable(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.readCh = mustClosedRecords()
	dest.writeErr = errors.New("write failed")
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable,
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type: schemaevolution.ChangeAddColumn, ColumnName: "must_not_be_added",
			}},
		},
	}

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected write failure")
	}
	staging := dest.prepareCalls[1].Table
	require.Equal(t, []string{staging}, dest.dropCalls)
	require.Empty(t, dest.execCalls, "target schema must not evolve before staging succeeds")
}

func TestTruncateInsertStrategy_DirectTruncateFailurePreservesNewTarget(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()
	dest.truncateErr = errors.New("truncate failed")

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	require.Error(t, err)
	require.Empty(t, dest.dropCalls, "truncate errors may be lost responses after a durable truncate")
}

func TestTruncateInsertStrategy_DirectFailurePreservesReplacementOwner(t *testing.T) {
	job, src, base := minimalJob()
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()
	base.writeErr = errors.New("write failed after replacement")
	dest := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
	job.Destination = dest

	require.Error(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "write errors may follow durable truncate/insert work")
	require.Empty(t, base.dropCalls, "unconditional cleanup must not delete a replacement owner")
}

func TestTruncateInsertStrategy_StagedMergeFailurePreservesTargetAndDropsStaging(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords()
	dest.mergeErr = errors.New("merge failed")

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	require.Error(t, err)
	require.NotContains(t, dest.dropCalls, job.Config.DestTable)
	require.Equal(t, []string{dest.prepareCalls[1].Table}, dest.dropCalls)
}

func TestTruncateInsertStrategy_StagedTruncateLostResponsePreservesTargetAndDropsStaging(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	base.truncateErr = errors.New("truncate response lost after commit")
	dest := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
	job.Destination = dest

	require.ErrorContains(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job), "truncate response lost")
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens)
	require.NotContains(t, base.dropCalls, job.Config.DestTable)
	require.Equal(t, []string{base.prepareCalls[1].Table}, base.dropCalls)
}

func TestTruncateInsertStrategy_StagedFailurePreservesReplacementOwner(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	base.mergeErr = errors.New("merge failed after replacement")
	dest := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
	job.Destination = dest

	require.Error(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "merge errors may follow a durable target mutation")
	require.NotContains(t, base.dropCalls, job.Config.DestTable)
	require.Contains(t, base.dropCalls, base.prepareCalls[1].Table, "owned staging table should still be reclaimed")
}
