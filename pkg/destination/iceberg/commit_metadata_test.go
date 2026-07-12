package iceberg

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"testing"

	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestTruncateResetsCommitTokenScope(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "commit_metadata.truncate_token_scope"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "snapshot-page-1"}
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), opts))
	before, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.True(t, tableHasCommitToken(before, commitTokenID(opts.CommitToken)))

	require.NoError(t, dest.TruncateTable(ctx, table))
	truncated, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.False(t, tableHasCommitToken(truncated, commitTokenID(opts.CommitToken)))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), opts))
	require.Equal(t, [][]any{{int64(2)}}, readTableRows(t, dest, table).Rows)
}

func TestCommitTokenIDIsStableAndTypeAware(t *testing.T) {
	left := map[string]any{"partition": 7, "offset": 42}
	right := map[string]any{"offset": 42, "partition": 7}

	require.Equal(t, commitTokenID(left), commitTokenID(right))
	require.NotEqual(t, commitTokenID(int64(42)), commitTokenID("42"))
	require.NotEqual(t, commitTokenID("first"), commitTokenID("second"))
	require.Empty(t, commitTokenID(nil))
}

func TestWriteParallelCommitTokenMakesAppendIdempotent(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.idempotent_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: map[string]int{"offset": 10}}
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), opts))
	firstSnapshotCount := icebergSnapshotCount(t, dest, table)

	// A retry can carry the same token but a fresh copy of the records. It must
	// consume/release that channel without appending the rows again.
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), opts))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, firstSnapshotCount, icebergSnapshotCount(t, dest, table))
	require.Equal(t, commitTokenID(opts.CommitToken), latestSnapshotSummary(t, dest, table)[snapshotCommitTokenKey])

	opts.CommitToken = map[string]int{"offset": 11}
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), opts))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))
}

func TestWriteParallelConcurrentSameTokenCommitsRowsOnce(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.concurrent_idempotent_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	barrier := &commitBarrierCatalog{
		Catalog: dest.catalog,
		ready:   make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	dest.catalog = barrier
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "shared-flush-token", Parallelism: 2}

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			errs <- dest.WriteParallel(ctx, recordBatches(int64Batch(t, 42)), opts)
		}()
	}
	<-barrier.ready
	<-barrier.ready
	close(barrier.release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, commitTokenID(opts.CommitToken), latestSnapshotSummary(t, dest, table)[snapshotCommitTokenKey])
}

func TestWriteParallelV3ConcurrentDifferentTokensReplayRowsAfterConflict(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table.format-version": []string{"3"},
	})
	ctx := context.Background()
	table := "lake.correctness.concurrent_v3_replay"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	conflict := &conflictFirstOfTwoCatalog{
		Catalog: dest.catalog, firstReady: make(chan struct{}), secondCommitted: make(chan struct{}),
	}
	dest.catalog = conflict
	inputs := []<-chan source.RecordBatchResult{
		recordBatches(int64Batch(t, 1)),
		recordBatches(int64Batch(t, 2)),
	}
	errs := make(chan error, 2)
	for i := range 2 {
		go func(index int) {
			errs <- dest.WriteParallel(ctx, inputs[index], destination.WriteOptions{
				Table: table, Schema: tableSchema, CommitToken: fmt.Sprintf("v3-token-%d", index),
			})
		}(i)
	}
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))
}

func TestWriteParallelReconcilesCommitWhoseOutcomeWasUnknown(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.unknown_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	unknown := errors.New("simulated response loss after catalog commit")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, afterCommitErrs: []error{unknown}}
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "flush-17"}

	// The catalog applied the commit and then returned an error. Reloading the
	// snapshot lineage finds the token, so the destination reports success.
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 17)), opts))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))

	// A process-level retry is also a no-op.
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 17)), opts))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
}

func TestLineageCommitReconciliationIgnoresAnOlderLedgerToken(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.lineage_reconcile"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "reused-token",
	}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "interleaved-append",
	}))

	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	baseSnapshotID := tbl.CurrentSnapshot().SnapshotID
	require.True(t, tableHasCommitToken(tbl, commitTokenID("reused-token")))
	require.False(t, lineageHasCommitTokenAfterSnapshot(tbl, commitTokenID("reused-token"), baseSnapshotID))
	require.False(t, lineageHasCommitTokenAfterSnapshot(tbl, commitTokenID("interleaved-append"), baseSnapshotID))
}

func TestWriteParallelReconcilesUnknownCommitAfterCallerContextCancellation(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx, cancel := context.WithCancel(context.Background())
	table := "lake.correctness.cancelled_unknown_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	unknown := errors.New("simulated timeout after catalog commit")
	dest.catalog = &commitOutcomeCatalog{
		Catalog:         dest.catalog,
		afterCommitErrs: []error{unknown},
		afterCommitHook: cancel,
	}
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "flush-cancelled-1"}

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 19)), opts))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.EqualValues(t, 1, icebergRowCount(context.Background(), t, dest, table))
}

func TestWriteParallelPollsUntilUnknownCommitBecomesVisible(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.eventually_visible_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	eventual := &eventuallyVisibleCommitCatalog{
		Catalog:          dest.catalog,
		unknown:          errors.New("simulated response loss before catalog replicas converged"),
		invisibleLoads:   3,
		invisibleLoadErr: errors.New("simulated stale catalog replica"),
	}
	dest.catalog = eventual
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 23)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "eventual-flush-23",
	}))
	require.GreaterOrEqual(t, eventual.failedLoadCount(), 3)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
}

func TestTokenlessReplaceReconcilesUnknownCommitOutcome(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.unknown_tokenless_replace"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true,
	}))

	unknown := errors.New("simulated response loss after tokenless replace")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, afterCommitErrs: []error{unknown}}
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}))

	rows := readTableRows(t, dest, table)
	require.Equal(t, [][]any{{int64(2)}}, rows.Rows)
	require.NotEmpty(t, latestSnapshotSummary(t, dest, table)[snapshotCommitTokenKey])
}

func TestWriteParallelDoesNotReconcileDefinitelyFailedCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.failed_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	failed := errors.New("simulated catalog rejection")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{failed}}
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "flush-18"}

	err := dest.WriteParallel(ctx, recordBatches(int64Batch(t, 18)), opts)
	require.ErrorIs(t, err, failed)
	require.EqualValues(t, 0, icebergRowCount(ctx, t, dest, table))

	// The token was not committed, so a retry performs the append.
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 18)), opts))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
}

func TestRowDeltaMergeReconcilesUnknownCommitAndRetry(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.correctness.unknown_merge_target"
	staging := "lake.correctness.unknown_merge_staging"
	tableSchema := mergeTestSchema()

	writeTableRows(t, dest, target, tableSchema, false, [][]any{
		{int64(1), "before", 1.0, int64(1)},
	})
	writeTableRows(t, dest, staging, tableSchema, true, [][]any{
		{int64(1), "after", 2.0, int64(2)},
		{int64(2), "new", 3.0, int64(2)},
	})

	unknown := errors.New("simulated response loss after row-delta commit")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, afterCommitErrs: []error{unknown}}
	opts := destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      tableSchema.ColumnNames(),
		CommitToken:  "merge-flush-1",
	}
	require.NoError(t, dest.MergeTable(ctx, opts))
	firstSnapshotCount := icebergSnapshotCount(t, dest, target)
	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 2)
	require.Equal(t, "after", rows[int64(1)][1])

	require.NoError(t, dest.MergeTable(ctx, opts))
	require.Equal(t, firstSnapshotCount, icebergSnapshotCount(t, dest, target))
	rows = singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 2)
}

func TestCommitWriteTokenReconcilesUnknownCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.unknown_checkpoint"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	unknown := errors.New("simulated response loss after checkpoint commit")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, afterCommitErrs: []error{unknown}}
	require.NoError(t, dest.CommitWriteToken(ctx, table, "idle-flush-1", "00000000/00000060"))
	firstSnapshotCount := icebergSnapshotCount(t, dest, table)

	require.NoError(t, dest.CommitWriteToken(ctx, table, "idle-flush-1", "00000000/00000060"))
	require.Equal(t, firstSnapshotCount, icebergSnapshotCount(t, dest, table))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "00000000/00000060", resume)
}

func TestGetMaxCDCLSNUsesSnapshotLineage(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.cdc_append"
	tableSchema := cdcSchema(false)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	first, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "one", "payload", "00000000/00000010", false, int64(10)},
	})
	require.NoError(t, err)
	second, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(2), "two", "payload", "00000000/00000020", false, int64(20)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(first[0], second[0]), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}))

	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "00000000/00000020", resume)

	// A later non-CDC metadata checkpoint must not hide the last CDC cursor.
	require.NoError(t, dest.CommitWriteToken(ctx, table, "broker-checkpoint", ""))
	resume, err = dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "00000000/00000020", resume)

	// A source checkpoint can advance beyond the last data row.
	require.NoError(t, dest.CommitWriteToken(ctx, table, "cdc-keepalive", "00000000/00000030"))
	resume, err = dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Equal(t, "00000000/00000030", resume)

	missing, err := dest.GetMaxCDCLSN(ctx, "lake.correctness.missing")
	require.NoError(t, err)
	require.Empty(t, missing)
}

func TestCDCMergePersistsCursorAndDeduplicatesDerivedToken(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.correctness.cdc_target"
	staging := "lake.correctness.cdc_staging"
	targetSchema := cdcSchema(false)
	stagingSchema := cdcSchema(true)

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: targetSchema}))
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), "one", "payload", "00000000/00000040", false, int64(40), nil},
		{int64(2), "two", "payload", "00000000/00000050", false, int64(50), nil},
	})
	opts := destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      stagingSchema.ColumnNames(),
	}
	require.NoError(t, dest.MergeTable(ctx, opts))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, target))
	firstSnapshotCount := icebergSnapshotCount(t, dest, target)

	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Equal(t, "00000000/00000050", resume)

	// Batch CDC does not provide a streaming CommitToken. The destination
	// derives one from the max durable LSN, making a retried merge a no-op.
	require.NoError(t, dest.MergeTable(ctx, opts))
	require.Equal(t, firstSnapshotCount, icebergSnapshotCount(t, dest, target))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, target))
}

func icebergSnapshotCount(t *testing.T, dest *Destination, table string) int {
	t.Helper()
	tbl, err := dest.loadIcebergTable(context.Background(), table)
	require.NoError(t, err)
	return len(tbl.Metadata().Snapshots())
}

// commitOutcomeCatalog injects failures immediately before or after the real
// catalog commit. LoadTable rebinds returned tables to this wrapper so their
// transactions exercise the injected CommitTable behavior too.
type commitOutcomeCatalog struct {
	icebergcatalog.Catalog

	mu               sync.Mutex
	beforeCommitErrs []error
	afterCommitErrs  []error
	afterCommitHook  func()
	injectIdentifier icebergtable.Identifier
}

type commitBarrierCatalog struct {
	icebergcatalog.Catalog
	ready   chan struct{}
	release chan struct{}
}

type conflictFirstOfTwoCatalog struct {
	icebergcatalog.Catalog
	mu              sync.Mutex
	calls           int
	firstReady      chan struct{}
	secondCommitted chan struct{}
}

type eventuallyVisibleCommitCatalog struct {
	icebergcatalog.Catalog

	mu               sync.Mutex
	unknown          error
	invisibleLoads   int
	invisibleLoadErr error
	remainingLoads   int
	failedLoads      int
}

func (c *eventuallyVisibleCommitCatalog) LoadTable(ctx context.Context, identifier icebergtable.Identifier) (*icebergtable.Table, error) {
	c.mu.Lock()
	if c.remainingLoads > 0 {
		c.remainingLoads--
		c.failedLoads++
		err := c.invisibleLoadErr
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *eventuallyVisibleCommitCatalog) CommitTable(ctx context.Context, identifier icebergtable.Identifier, requirements []icebergtable.Requirement, updates []icebergtable.Update) (icebergtable.Metadata, string, error) {
	metadata, location, err := c.Catalog.CommitTable(ctx, identifier, requirements, updates)
	if err != nil {
		return metadata, location, err
	}
	c.mu.Lock()
	c.remainingLoads = c.invisibleLoads
	c.mu.Unlock()
	return nil, "", c.unknown
}

func (c *eventuallyVisibleCommitCatalog) failedLoadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failedLoads
}

func (c *commitBarrierCatalog) LoadTable(ctx context.Context, identifier icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *commitBarrierCatalog) CommitTable(ctx context.Context, identifier icebergtable.Identifier, requirements []icebergtable.Requirement, updates []icebergtable.Update) (icebergtable.Metadata, string, error) {
	c.ready <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-c.release:
		return c.Catalog.CommitTable(ctx, identifier, requirements, updates)
	}
}

func (c *conflictFirstOfTwoCatalog) LoadTable(ctx context.Context, identifier icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *conflictFirstOfTwoCatalog) CommitTable(
	ctx context.Context,
	identifier icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.mu.Unlock()
	switch call {
	case 1:
		close(c.firstReady)
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-c.secondCommitted:
			return nil, "", icebergtable.ErrCommitFailed
		}
	case 2:
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-c.firstReady:
		}
		metadata, location, err := c.Catalog.CommitTable(ctx, identifier, requirements, updates)
		close(c.secondCommitted)
		return metadata, location, err
	default:
		return c.Catalog.CommitTable(ctx, identifier, requirements, updates)
	}
}

func (c *commitOutcomeCatalog) LoadTable(ctx context.Context, identifier icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *commitOutcomeCatalog) CommitTable(ctx context.Context, identifier icebergtable.Identifier, requirements []icebergtable.Requirement, updates []icebergtable.Update) (icebergtable.Metadata, string, error) {
	c.mu.Lock()
	if len(c.beforeCommitErrs) > 0 && (len(c.injectIdentifier) == 0 || slices.Equal(c.injectIdentifier, identifier)) {
		err := c.beforeCommitErrs[0]
		c.beforeCommitErrs = c.beforeCommitErrs[1:]
		c.mu.Unlock()
		return nil, "", err
	}
	c.mu.Unlock()

	metadata, location, err := c.Catalog.CommitTable(ctx, identifier, requirements, updates)
	if err != nil {
		return metadata, location, err
	}
	if c.afterCommitHook != nil && (len(c.injectIdentifier) == 0 || slices.Equal(c.injectIdentifier, identifier)) {
		c.afterCommitHook()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.afterCommitErrs) > 0 && (len(c.injectIdentifier) == 0 || slices.Equal(c.injectIdentifier, identifier)) {
		err := c.afterCommitErrs[0]
		c.afterCommitErrs = c.afterCommitErrs[1:]
		return nil, "", err
	}
	return metadata, location, nil
}

var (
	_ icebergcatalog.Catalog               = (*commitOutcomeCatalog)(nil)
	_ icebergcatalog.Catalog               = (*commitBarrierCatalog)(nil)
	_ icebergcatalog.Catalog               = (*eventuallyVisibleCommitCatalog)(nil)
	_ destination.CDCResumeProvider        = (*Destination)(nil)
	_ destination.DurableCommitTokenWriter = (*Destination)(nil)
)
