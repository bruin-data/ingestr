package iceberg

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestV3AppendRetriesWithOwnedFilesAndPreservesLineage(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestinationWithTableProperties(t, url.Values{"table.format-version": []string{"3"}})
	table := "lake.correctness.v3_append_retry"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	before := allTableParquetFiles(t, dest, table)
	dest.catalog = &failNthCommitCatalog{Catalog: dest.catalog, failAt: 1, err: icebergtable.ErrCommitFailed}

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}))
	require.Len(t, allTableParquetFiles(t, dest, table), len(before)+1)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.True(t, allTasksHaveRowLineage(tasks))
}

func TestTokenizedAppendRetryRejectsRecreatedTargetWithMatchingToken(t *testing.T) {
	for _, tt := range []struct {
		name         string
		metadataOnly bool
	}{
		{name: "data files"},
		{name: "metadata only", metadataOnly: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dest := newHadoopDestination(t)
			table := "lake.correctness.recreated_token_" + strings.ReplaceAll(tt.name, " ", "_")
			tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
			writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
			original, err := dest.loadIcebergTable(ctx, table)
			require.NoError(t, err)
			const token = "recreated-target-token"
			dest.catalog = &recreateWithCommitTokenOnConflictCatalog{
				Catalog: dest.catalog,
				schema:  tableSchema,
				token:   token,
			}

			if tt.metadataOnly {
				_, err = dest.commitTokenizedAppend(ctx, original, iceberggo.Properties{
					snapshotCommitTokenKey: token,
				}, original.Metadata().TableUUID().String(), func(*icebergtable.Transaction) error {
					return nil
				})
			} else {
				err = dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
					Table: table, Schema: tableSchema, CommitToken: token,
					CDCExpectedIncarnation: original.Metadata().TableUUID().String(),
				})
			}
			require.ErrorContains(t, err, "incarnation changed")
			recreated, loadErr := dest.loadIcebergTable(ctx, table)
			require.NoError(t, loadErr)
			require.NotEqual(t, original.Metadata().TableUUID(), recreated.Metadata().TableUUID())
		})
	}
}

func TestCommitReconciliationRejectsRecreatedTargetWithMatchingToken(t *testing.T) {
	for _, tt := range []struct {
		name               string
		metadataOnly       bool
		recreateAfterLoads int
	}{
		{name: "data files", recreateAfterLoads: 2},
		{name: "metadata only", metadataOnly: true, recreateAfterLoads: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dest := newHadoopDestination(t)
			table := "lake.correctness.reconcile_recreated_token_" + strings.ReplaceAll(tt.name, " ", "_")
			tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
			writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
			original, err := dest.loadIcebergTable(ctx, table)
			require.NoError(t, err)
			const token = "reconcile-recreated-target-token"
			dest.catalog = &recreateDuringCommitReconciliationCatalog{
				Catalog:            dest.catalog,
				schema:             tableSchema,
				token:              token,
				recreateAfterLoads: tt.recreateAfterLoads,
			}

			if tt.metadataOnly {
				current, loadErr := dest.loadIcebergTable(ctx, table)
				require.NoError(t, loadErr)
				err = dest.commitMetadataOnly(ctx, current, "checkpoint",
					newCommitMetadata(token, "").withExpectedIncarnation(original.Metadata().TableUUID().String()))
			} else {
				err = dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
					Table: table, Schema: tableSchema, CommitToken: token,
					CDCExpectedIncarnation: original.Metadata().TableUUID().String(),
				})
			}
			require.ErrorContains(t, err, "incarnation changed")
			recreated, loadErr := dest.loadIcebergTable(ctx, table)
			require.NoError(t, loadErr)
			require.NotEqual(t, original.Metadata().TableUUID(), recreated.Metadata().TableUUID())
		})
	}
}

func TestCommitReconciliationRejectsRecreatedTargetWithoutMatchingToken(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.correctness.reconcile_recreated_without_token"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	original, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.NoError(t, recreateTableWithCommitToken(ctx, dest.catalog, original.Identifier(), tableSchema, "other-token"))

	commitErr := errors.New("commit outcome is unknown")
	err = dest.reconcileCommit(ctx, table, "missing-token", original.Metadata().TableUUID().String(), commitErr)
	require.ErrorContains(t, err, "incarnation changed")
	require.NotErrorIs(t, err, commitErr)
}

func TestV3AppendExhaustedConflictsCleanOwnedFiles(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestinationWithTableProperties(t, url.Values{"table.format-version": []string{"3"}})
	table := "lake.correctness.v3_append_cleanup"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	before := allTableParquetFiles(t, dest, table)
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{
		icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed,
		icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed,
	}}

	err := dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "v3-append-cleanup",
	})
	require.ErrorIs(t, err, icebergtable.ErrCommitFailed)
	require.Equal(t, before, allTableParquetFiles(t, dest, table))
}

func TestTokenizedAppendCommitFailureCleansGeneratedFiles(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.correctness.append_cleanup"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	before := allTableParquetFiles(t, dest, table)
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{
		icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed,
		icebergtable.ErrCommitFailed, icebergtable.ErrCommitFailed,
	}}

	err := dest.WriteParallel(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "append-cleanup",
	})
	require.True(t, errors.Is(err, icebergtable.ErrCommitFailed))
	require.Equal(t, before, allTableParquetFiles(t, dest, table))
}

func TestAppendReaderFailureCleansPartiallyGeneratedFiles(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.correctness.append_reader_cleanup"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	readErr := errors.New("source failed after batch")
	err := dest.WriteParallel(ctx, recordBatchesThenError(readErr, int64Batch(t, 1, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	})
	require.ErrorIs(t, err, readErr)
	require.Empty(t, allTableParquetFiles(t, dest, table))
}

func TestWriteParallelTokenlessAppendKeepsIdenticalRunsWithDifferentLoadTimestamps(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.correctness.content_idempotent_loaded_at"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: naming.IngestrLoadedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
	}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	write := func(loadedAt int64) {
		t.Helper()
		batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(1), loadedAt}})
		require.NoError(t, err)
		require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: table, Schema: tableSchema,
		}))
	}

	write(micros(time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)))
	snapshots := icebergSnapshotCount(t, dest, table)
	write(micros(time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, snapshots+1, icebergSnapshotCount(t, dest, table))
}

func TestWriteParallelTokenlessAppendDoesNotDeduplicateIdenticalContent(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.correctness.content_idempotent_append"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	opts := destination.WriteOptions{Table: table, Schema: tableSchema, Parallelism: 2}
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(
		int64Batch(t, 1, 2),
		int64Batch(t, 3, 4),
	), opts))
	firstSnapshotCount := icebergSnapshotCount(t, dest, table)

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(
		int64Batch(t, 4),
		int64Batch(t, 2, 1, 3),
	), opts))
	require.EqualValues(t, 8, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, firstSnapshotCount+1, icebergSnapshotCount(t, dest, table))
	require.Empty(t, latestSnapshotSummary(t, dest, table)[snapshotCommitTokenKey])

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(
		int64Batch(t, 8),
		int64Batch(t, 6, 5, 7),
	), opts))
	require.EqualValues(t, 12, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, firstSnapshotCount+2, icebergSnapshotCount(t, dest, table))
}

func TestCommitTokenLedgerSurvivesSnapshotExpiration(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.correctness.expired_snapshot_token_ledger"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	write := func(token string, id int64) {
		t.Helper()
		require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, id)), destination.WriteOptions{
			Table: table, Schema: tableSchema, CommitToken: token,
		}))
	}

	write("T1", 1)
	before, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	t1SnapshotID := before.CurrentSnapshot().SnapshotID
	time.Sleep(2 * time.Millisecond)
	write("T2", 2)

	result, err := dest.MaintainTable(ctx, table, MaintenanceOptions{
		ExpireSnapshots:    true,
		SnapshotMaxAge:     time.Millisecond,
		MinSnapshotsToKeep: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.SnapshotsAfter)

	afterExpiration, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Nil(t, afterExpiration.SnapshotByID(t1SnapshotID))
	require.True(t, tableHasCommitToken(afterExpiration, commitTokenID("T1")))

	write("T1", 1)
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, 1, icebergSnapshotCount(t, dest, table))
}

type recreateWithCommitTokenOnConflictCatalog struct {
	icebergcatalog.Catalog
	schema    *schema.TableSchema
	token     string
	recreated bool
}

type recreateDuringCommitReconciliationCatalog struct {
	icebergcatalog.Catalog
	schema             *schema.TableSchema
	token              string
	recreateAfterLoads int
	postFailureLoads   int
	commitFailed       bool
}

type recreateOnNthLoadCatalog struct {
	icebergcatalog.Catalog
	schema     *schema.TableSchema
	token      string
	recreateAt int
	loads      int
}

func (c *recreateOnNthLoadCatalog) LoadTable(
	ctx context.Context,
	identifier icebergtable.Identifier,
) (*icebergtable.Table, error) {
	c.loads++
	if c.loads == c.recreateAt {
		if err := recreateTableWithCommitToken(ctx, c.Catalog, identifier, c.schema, c.token); err != nil {
			return nil, err
		}
	}
	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *recreateDuringCommitReconciliationCatalog) LoadTable(
	ctx context.Context,
	identifier icebergtable.Identifier,
) (*icebergtable.Table, error) {
	if c.commitFailed {
		c.postFailureLoads++
		if c.postFailureLoads == c.recreateAfterLoads {
			if err := recreateTableWithCommitToken(ctx, c.Catalog, identifier, c.schema, c.token); err != nil {
				return nil, err
			}
		}
	}
	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *recreateDuringCommitReconciliationCatalog) CommitTable(
	ctx context.Context,
	identifier icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	if c.commitFailed {
		return c.Catalog.CommitTable(ctx, identifier, requirements, updates)
	}
	c.commitFailed = true
	return nil, "", errors.New("commit outcome is unknown")
}

func (c *recreateWithCommitTokenOnConflictCatalog) LoadTable(
	ctx context.Context,
	identifier icebergtable.Identifier,
) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, identifier)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *recreateWithCommitTokenOnConflictCatalog) CommitTable(
	ctx context.Context,
	identifier icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	if c.recreated {
		return c.Catalog.CommitTable(ctx, identifier, requirements, updates)
	}
	c.recreated = true
	if err := recreateTableWithCommitToken(ctx, c.Catalog, identifier, c.schema, c.token); err != nil {
		return nil, "", err
	}
	return nil, "", icebergtable.ErrCommitFailed
}

func recreateTableWithCommitToken(
	ctx context.Context,
	catalog icebergcatalog.Catalog,
	identifier icebergtable.Identifier,
	tableSchema *schema.TableSchema,
	token string,
) error {
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	if err != nil {
		return err
	}
	ledger, err := json.Marshal([]string{commitTokenID(token)})
	if err != nil {
		return err
	}
	if err := catalog.DropTable(ctx, identifier); err != nil {
		return err
	}
	_, err = catalog.CreateTable(ctx, identifier, iceSchema, icebergcatalog.WithProperties(iceberggo.Properties{
		tableCommitTokenLedgerKey: string(ledger),
	}))
	return err
}
