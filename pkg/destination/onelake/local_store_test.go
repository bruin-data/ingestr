package onelake

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type localDataLakeClient struct {
	root      string
	downloads int
}

func (c *localDataLakeClient) UploadBufferSkippingPrefix(ctx context.Context, _ string, path string, data []byte, _ int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := c.path(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, data, 0o644)
}

func (c *localDataLakeClient) EnsureDirectoriesSkippingPrefix(ctx context.Context, _ string, path string, _ int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := c.path(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0o755)
}

func (c *localDataLakeClient) DeleteDir(ctx context.Context, _ string, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := c.path(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(fullPath)
}

func (c *localDataLakeClient) ListLogVersions(ctx context.Context, _ string, path string) ([]int64, error) {
	entries, err := c.ListLogEntries(ctx, "", path)
	if err != nil {
		return nil, err
	}
	versions := make([]int64, len(entries))
	for i, entry := range entries {
		versions[i] = entry.Version
	}
	return versions, nil
}

func (c *localDataLakeClient) ListLogEntries(ctx context.Context, _ string, path string) ([]adlsutil.DeltaLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fullPath, err := c.path(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	logEntries := make([]adlsutil.DeltaLogEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) != 25 || !strings.HasSuffix(name, ".json") {
			continue
		}
		version, err := strconv.ParseInt(strings.TrimSuffix(name, ".json"), 10, 64)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fullPath, name))
		if err != nil {
			return nil, err
		}
		logEntries = append(logEntries, adlsutil.DeltaLogEntry{
			Version: version,
			ETag:    fmt.Sprintf("%x", sha256.Sum256(data)),
		})
	}
	sort.Slice(logEntries, func(i, j int) bool { return logEntries[i].Version < logEntries[j].Version })
	return logEntries, nil
}

func (c *localDataLakeClient) Download(ctx context.Context, _ string, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.downloads++
	fullPath, err := c.path(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fullPath)
}

func (c *localDataLakeClient) publishCommit(ctx context.Context, logDir string, version int64, commit []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := c.path(logDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".ingestr-commit-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(commit); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	target := filepath.Join(dir, commitFileName(version))
	if err := os.Link(tempPath, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errDeltaCommitConflict
		}
		return err
	}
	return nil
}

func (c *localDataLakeClient) path(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid local OneLake path %q", path)
	}
	return filepath.Join(c.root, clean), nil
}

func newLocalOneLakeDestination(root string) (*OneLakeDestination, *localDataLakeClient) {
	client := &localDataLakeClient{root: root}
	destination := &OneLakeDestination{
		workspace: "local",
		lakehouse: "test",
		client:    client,
		layout:    defaultLayout,
	}
	destination.publishCommit = client.publishCommit
	return destination, client
}

func writeOneLakeTestBatch(t *testing.T, dest *OneLakeDestination, table string, tableSchema *schema.TableSchema, batch arrow.RecordBatch) {
	t.Helper()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)
	require.NoError(t, dest.WriteParallel(t.Context(), records, destination.WriteOptions{Table: table, Schema: tableSchema}))
}

// TestLocalStoreDeleteInsertAfterAdditiveSchemaEvolution drives a full
// evolve-then-rewrite cycle against a local Delta table. The added column is
// declared NOT NULL at the source, which is the case that forces the target's
// NULL-filled column and the staging column to be reconciled before the
// copy-on-write rewrite.
func TestLocalStoreDeleteInsertAfterAdditiveSchemaEvolution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	targetTable := "analytics.orders"
	stagingTable := "analytics.orders_staging"
	updatedAt := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	initialSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ORDER_ID", DataType: schema.TypeInt64},
		{Name: "LINE_ID", DataType: schema.TypeInt64},
		{Name: "UPDATED_AT", DataType: schema.TypeTimestampTZ},
	}}
	evolvedSchema := &schema.TableSchema{Columns: append(
		append([]schema.Column(nil), initialSchema.Columns...),
		schema.Column{Name: "EXTERNAL_REF_ID", DataType: schema.TypeString},
	)}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: targetTable, Schema: initialSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, targetTable, initialSchema, makeBatch(t, []arrow.Field{
		{Name: "ORDER_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "LINE_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "UPDATED_AT", Type: tsTZType},
	}, [][]any{
		{int64(1), int64(10), updatedAt.Add(-time.Hour)},
		{int64(2), int64(20), updatedAt},
	}))

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: stagingTable, Schema: evolvedSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, stagingTable, evolvedSchema, makeBatch(t, []arrow.Field{
		{Name: "ORDER_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "LINE_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "UPDATED_AT", Type: tsTZType},
		{Name: "EXTERNAL_REF_ID", Type: arrow.BinaryTypes.String},
	}, [][]any{
		{int64(2), int64(20), updatedAt, "external-2"},
		{int64(3), int64(30), updatedAt, "external-3"},
	}))

	currentSchema, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	comparison, err := schemaevolution.Compare(evolvedSchema, currentSchema, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(t.Context(), targetTable, comparison)
	require.NoError(t, err)
	require.NoError(t, dest.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{
		TargetTable:        targetTable,
		StagingTable:       stagingTable,
		IncrementalKey:     "UPDATED_AT",
		IncrementalKeyType: schema.TypeTimestampTZ,
		IntervalStart:      updatedAt,
		IntervalEnd:        updatedAt,
		PrimaryKeys:        []string{"ORDER_ID", "LINE_ID"},
	}))

	finalSchema, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	assert.Equal(t, []string{"ORDER_ID", "LINE_ID", "UPDATED_AT", "EXTERNAL_REF_ID"}, finalSchema.ColumnNames())
	_, snapshot, batches, err := dest.readTable(t.Context(), targetTable, "test")
	require.NoError(t, err)
	defer releaseBatches(batches)
	require.True(t, snapshot.exists)
	require.Len(t, snapshot.activeFiles, 1)

	rows := collectRows(batches)
	require.Len(t, rows, 3)
	byOrderID := make(map[int64]map[string]any, len(rows))
	for _, row := range rows {
		byOrderID[row["ORDER_ID"].(int64)] = row
	}
	assert.Nil(t, byOrderID[1]["EXTERNAL_REF_ID"])
	assert.Equal(t, "external-2", byOrderID[2]["EXTERNAL_REF_ID"])
	assert.Equal(t, "external-3", byOrderID[3]["EXTERNAL_REF_ID"])

	logDir := dest.itemPath() + "/Tables/analytics/orders/_delta_log"
	versions, err := client.ListLogVersions(t.Context(), dest.workspace, logDir)
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1, 2}, versions)
	assert.FileExists(t, filepath.Join(root, filepath.FromSlash(logDir), commitFileName(2)))
}

// TestLocalStoreMergeAfterMidSchemaColumnAdd covers the ordering skew between
// the two sides of a rewrite: schema evolution appends the new column to the
// end of the Delta schema, while the ingest schema keeps it where the source
// put it (SCD2 does the same by always keeping its _scd_* columns last).
func TestLocalStoreMergeAfterMidSchemaColumnAdd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, _ := newLocalOneLakeDestination(root)
	targetTable := "analytics.people"
	stagingTable := "analytics.people_staging"

	initialSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "TAIL", DataType: schema.TypeString},
	}}
	// EXTRA sits before TAIL at the source, but evolution can only append it
	// after TAIL in the Delta schema.
	evolvedSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "EXTRA", DataType: schema.TypeString},
		{Name: "TAIL", DataType: schema.TypeString},
	}}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: targetTable, Schema: initialSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, targetTable, initialSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "TAIL", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(1), "kept"}}))

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: stagingTable, Schema: evolvedSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, stagingTable, evolvedSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "EXTRA", Type: arrow.BinaryTypes.String},
		{Name: "TAIL", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(2), "new-extra", "new-tail"}}))

	current, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	comparison, err := schemaevolution.Compare(evolvedSchema, current, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(t.Context(), targetTable, comparison)
	require.NoError(t, err)
	require.NoError(t, dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: targetTable, StagingTable: stagingTable, PrimaryKeys: []string{"ID"},
	}))

	_, _, batches, err := dest.readTable(t.Context(), targetTable, "test")
	require.NoError(t, err)
	defer releaseBatches(batches)
	rows := collectRows(batches)
	require.Len(t, rows, 2)
	byID := make(map[int64]map[string]any, len(rows))
	for _, row := range rows {
		byID[row["ID"].(int64)] = row
	}
	assert.Equal(t, "kept", byID[1]["TAIL"], "the pre-evolution row must keep its own column values")
	assert.Nil(t, byID[1]["EXTRA"])
	assert.Equal(t, "new-extra", byID[2]["EXTRA"])
	assert.Equal(t, "new-tail", byID[2]["TAIL"])
}

// TestLocalStoreMergeAfterSourceDropsColumn drives the soft-removal path end to
// end: the dropped column stays in the table relaxed to nullable, so the
// rewrite has to NULL-fill it for the rows the staging table no longer carries.
func TestLocalStoreMergeAfterSourceDropsColumn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, _ := newLocalOneLakeDestination(root)
	targetTable := "analytics.invoices"
	stagingTable := "analytics.invoices_staging"

	initialSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "GONE", DataType: schema.TypeString},
	}}
	reducedSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
	}}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: targetTable, Schema: initialSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, targetTable, initialSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "GONE", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(1), "kept"}}))

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: stagingTable, Schema: reducedSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, stagingTable, reducedSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(2)}}))

	current, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	comparison, err := schemaevolution.Compare(reducedSchema, current, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(t.Context(), targetTable, comparison)
	require.NoError(t, err)
	require.NoError(t, dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: targetTable, StagingTable: stagingTable, PrimaryKeys: []string{"ID"},
	}))

	finalSchema, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	assert.Equal(t, []string{"ID", "GONE"}, finalSchema.ColumnNames(), "the dropped column must be retained")

	_, _, batches, err := dest.readTable(t.Context(), targetTable, "test")
	require.NoError(t, err)
	defer releaseBatches(batches)
	rows := collectRows(batches)
	require.Len(t, rows, 2)
	byID := make(map[int64]map[string]any, len(rows))
	for _, row := range rows {
		byID[row["ID"].(int64)] = row
	}
	assert.Equal(t, "kept", byID[1]["GONE"], "rows written before the drop must keep their value")
	assert.Nil(t, byID[2]["GONE"])
}

// TestLocalStoreMergeRejectsUndeclaredStagingColumn covers a rewrite whose
// staging table carries a column the target never declared, which is what a
// skipped schema evolution looks like from here. A rewrite commit carries no
// metaData action, so writing the column would hide it from every Delta reader
// — including ingestr's own next run.
func TestLocalStoreMergeRejectsUndeclaredStagingColumn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, _ := newLocalOneLakeDestination(root)
	targetTable := "analytics.tickets"
	stagingTable := "analytics.tickets_staging"

	targetSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}
	stagingSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "NEWCOL", DataType: schema.TypeString},
	}}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: targetTable, Schema: targetSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, targetTable, targetSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(1)}}))

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: stagingTable, Schema: stagingSchema, DropFirst: true,
	}))
	// The staging row replaces every target row, so nothing from the target
	// survives the merge to expose the skew further down the write path.
	writeOneLakeTestBatch(t, dest, stagingTable, stagingSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "NEWCOL", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(1), "new"}}))

	err := dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: targetTable, StagingTable: stagingTable, PrimaryKeys: []string{"ID"},
	})
	require.ErrorContains(t, err, `column "NEWCOL" is not declared in its Delta schema`)

	// The target's own data files are what the check falls back to, so empty the
	// table out and confirm the Delta schema alone still rejects the column.
	dir, snapshot, batches, err := dest.readTable(t.Context(), targetTable, "test")
	require.NoError(t, err)
	releaseBatches(batches)
	require.NoError(t, dest.commitRewrite(t.Context(), dir, snapshot, &rewriteData{}, "DELETE"))

	err = dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: targetTable, StagingTable: stagingTable, PrimaryKeys: []string{"ID"},
	})
	require.ErrorContains(t, err, `column "NEWCOL" is not declared in its Delta schema`)
}

// TestLocalStoreSchemaEvolutionRetriesCommitConflict covers the case where
// another writer takes the next Delta version while evolution is preparing its
// commit: the retry has to re-read the table so it does not lose that writer's
// changes or add the column twice.
func TestLocalStoreSchemaEvolutionRetriesCommitConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.contested"
	initialSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}
	evolvedSchema := &schema.TableSchema{Columns: append(
		append([]schema.Column(nil), initialSchema.Columns...),
		schema.Column{Name: "LABEL", DataType: schema.TypeString},
	)}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: table, Schema: initialSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, table, initialSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(1)}}))

	// Steal version 1 the first time evolution tries to publish it.
	stolen := false
	dest.publishCommit = func(ctx context.Context, logDir string, version int64, commit []byte) error {
		if !stolen {
			stolen = true
			require.NoError(t, client.publishCommit(ctx, logDir, version, appendOnlyCommit(t)))
			return errDeltaCommitConflict
		}
		return client.publishCommit(ctx, logDir, version, commit)
	}

	current, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	comparison, err := schemaevolution.Compare(evolvedSchema, current, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(t.Context(), table, comparison)
	require.NoError(t, err)
	assert.True(t, stolen)

	final, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	assert.Equal(t, []string{"ID", "LABEL"}, final.ColumnNames(), "the retry must add the column exactly once")

	versions, err := client.ListLogVersions(t.Context(), dest.workspace, dest.itemPath()+"/Tables/analytics/contested/_delta_log")
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1, 2}, versions)
}

func TestLocalStoreSchemaEvolutionRejectsConcurrentMetadataChange(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.schema_conflict"
	initialSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}
	desiredSchema := &schema.TableSchema{Columns: append(
		append([]schema.Column(nil), initialSchema.Columns...),
		schema.Column{Name: "LABEL", DataType: schema.TypeString},
	)}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: table, Schema: initialSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, table, initialSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(1)}}))
	current, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	comparison, err := schemaevolution.Compare(desiredSchema, current, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)

	tableDir, err := dest.tableDirForTables(table, "test")
	require.NoError(t, err)
	snapshot, err := dest.readTableMetadata(t.Context(), tableDir)
	require.NoError(t, err)
	concurrentSchema, err := deltaSchemaFromMetadata(snapshot.metadata)
	require.NoError(t, err)
	concurrentSchema.Fields[0].Type = "string"
	concurrentMetadata, err := deltaMetadataWithSchema(snapshot.metadata, concurrentSchema)
	require.NoError(t, err)
	concurrentCommit, err := buildSchemaEvolutionCommit(concurrentMetadata, 2)
	require.NoError(t, err)

	stolen := false
	dest.publishCommit = func(ctx context.Context, logDir string, version int64, commit []byte) error {
		if !stolen {
			stolen = true
			require.NoError(t, client.publishCommit(ctx, logDir, version, concurrentCommit))
			return errDeltaCommitConflict
		}
		return client.publishCommit(ctx, logDir, version, commit)
	}

	_, err = dest.ApplySchemaEvolution(t.Context(), table, comparison)
	require.ErrorContains(t, err, "metadata changed after its schema evolution plan was built")
	versions, err := client.ListLogVersions(t.Context(), dest.workspace, tableDir+"/_delta_log")
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1}, versions, "stale evolution must not publish another metadata commit")
}

func appendOnlyCommit(t *testing.T) []byte {
	t.Helper()
	commit, err := buildAppendCommit([]deltaAddFile{{Path: "part-other.parquet", Size: 1}}, 1)
	require.NoError(t, err)
	return commit
}

// TestLocalStoreMergeAcrossLossyTypeGenerations covers the schema the pipeline
// feeds back into the next run. Delta has no TIME type, so once GetTableSchema
// reports the table the ingest schema carries the column as an int64 while the
// data already written for it is a time64. Both encodings describe the same
// Delta "long" column and must still merge.
func TestLocalStoreMergeAcrossLossyTypeGenerations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, _ := newLocalOneLakeDestination(root)
	targetTable := "analytics.sessions"
	stagingTable := "analytics.sessions_staging"

	firstRunSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "DURATION", DataType: schema.TypeTime},
	}}
	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: targetTable, Schema: firstRunSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, targetTable, firstRunSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "DURATION", Type: arrow.FixedWidthTypes.Time64us},
	}, [][]any{{int64(1), int64(3_600_000_000)}}))

	// The second run's ingest schema comes from the destination, where the
	// column reads back as a plain long.
	secondRunSchema, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	require.Equal(t, schema.TypeInt64, secondRunSchema.Columns[1].DataType)
	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: stagingTable, Schema: secondRunSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, stagingTable, secondRunSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "DURATION", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(2), int64(7_200_000_000)}}))

	require.NoError(t, dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: targetTable, StagingTable: stagingTable, PrimaryKeys: []string{"ID"},
	}))

	_, _, batches, err := dest.readTable(t.Context(), targetTable, "test")
	require.NoError(t, err)
	defer releaseBatches(batches)
	rows := collectRows(batches)
	require.Len(t, rows, 2)
	byID := make(map[int64]map[string]any, len(rows))
	for _, row := range rows {
		byID[row["ID"].(int64)] = row
	}
	assert.Equal(t, int64(3_600_000_000), byID[1]["DURATION"])
	assert.Equal(t, int64(7_200_000_000), byID[2]["DURATION"])
}

// TestLocalStoreRewriteRejectsPartitionedTable guards the copy-on-write read
// path: a partitioned table keeps its partition values in the Delta log rather
// than in the Parquet files, so rewriting one would blank those columns.
func TestLocalStoreRewriteRejectsPartitionedTable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.partitioned"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "REGION", DataType: schema.TypeString},
	}}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, table, tableSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "REGION", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(1), "eu"}}))

	dir := rewriteDeltaMetadata(t, dest, client, table, func(metadata deltaMetadata) {
		metadata["partitionColumns"] = json.RawMessage(`["REGION"]`)
	})
	assert.NotEmpty(t, dir)

	_, _, _, err := dest.readTable(t.Context(), table, "merge")
	require.ErrorContains(t, err, "partitioned Delta table")
}

// TestLocalStoreGetTableSchemaStopsAtTheNewestMetadata pins the backward scan:
// the pipeline asks for the schema on every run, so the lookup must stop at the
// most recent metaData action rather than replaying the whole commit history.
func TestLocalStoreGetTableSchemaStopsAtTheNewestMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.events"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}

	for i := range 4 {
		require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table: table, Schema: tableSchema, DropFirst: i == 0,
		}))
		writeOneLakeTestBatch(t, dest, table, tableSchema, makeBatch(t, []arrow.Field{
			{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		}, [][]any{{int64(i)}}))
	}
	evolvedSchema := &schema.TableSchema{Columns: append(
		append([]schema.Column(nil), tableSchema.Columns...),
		schema.Column{Name: "LABEL", DataType: schema.TypeString},
	)}
	current, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	comparison, err := schemaevolution.Compare(evolvedSchema, current, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(t.Context(), table, comparison)
	require.NoError(t, err)

	client.downloads = 0
	got, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"ID", "LABEL"}, got.ColumnNames())
	assert.Equal(t, 2, client.downloads,
		"the backward scan may only read the newest metaData commit plus the version-0 commit the protocol anchors to")
}

// TestLocalStoreGetTableSchemaReusesMetadataAcrossAppends pins the cost of the
// schema lookup on an append-only table, where ingestr's only metaData action
// sits in the version-0 commit and the backward scan therefore reaches the
// bottom of the log. The pipeline asks for the schema several times per run, so
// only the first lookup may pay for the history.
func TestLocalStoreGetTableSchemaReusesMetadataAcrossAppends(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.appends"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}

	appendRow := func(i int, dropFirst bool) {
		require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table: table, Schema: tableSchema, DropFirst: dropFirst,
		}))
		writeOneLakeTestBatch(t, dest, table, tableSchema, makeBatch(t, []arrow.Field{
			{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		}, [][]any{{int64(i)}}))
	}
	for i := range 4 {
		appendRow(i, i == 0)
	}

	client.downloads = 0
	got, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 4, client.downloads, "the first lookup scans back to the version-0 metaData")

	client.downloads = 0
	_, err = dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	assert.Equal(t, 1, client.downloads, "an unchanged log must only re-read the commit the metaData came from")

	appendRow(4, false)
	client.downloads = 0
	_, err = dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	assert.Equal(t, 2, client.downloads, "only the appended commit and the metaData commit may be read")

	// Recreating the table restarts the log, so the cached metaData must go.
	appendRow(5, true)
	client.downloads = 0
	recreated, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	assert.Equal(t, []string{"ID"}, recreated.ColumnNames())
	assert.Equal(t, 1, client.downloads)
}

// TestLocalStoreGetTableSchemaDetectsRecreatedTable covers the one way the
// metaData cache can go stale: another writer drops and recreates the table, so
// its log restarts at version 0 and the versions the cache treats as already
// scanned belong to a table that no longer exists.
func TestLocalStoreGetTableSchemaDetectsRecreatedTable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, _ := newLocalOneLakeDestination(root)
	other, _ := newLocalOneLakeDestination(root)
	table := "analytics.recreated"

	oldSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "OLD_ONLY", DataType: schema.TypeString},
	}}
	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "BRAND_NEW", DataType: schema.TypeString},
	}}
	writeRows := func(d *OneLakeDestination, tableSchema *schema.TableSchema, field arrow.Field, value any, dropFirst bool) {
		require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{
			Table: table, Schema: tableSchema, DropFirst: dropFirst,
		}))
		writeOneLakeTestBatch(t, d, table, tableSchema, makeBatch(t, []arrow.Field{
			{Name: "ID", Type: arrow.PrimitiveTypes.Int64}, field,
		}, [][]any{{int64(1), value}}))
	}

	oldField := arrow.Field{Name: "OLD_ONLY", Type: arrow.BinaryTypes.String}
	writeRows(dest, oldSchema, oldField, "old", true)
	writeRows(dest, oldSchema, oldField, "old", false)
	current, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	require.Equal(t, []string{"ID", "OLD_ONLY"}, current.ColumnNames())

	// The recreated log is rebuilt to the same length, so the cached versions
	// all still exist — only their contents changed.
	newField := arrow.Field{Name: "BRAND_NEW", Type: arrow.BinaryTypes.String}
	writeRows(other, newSchema, newField, "new", true)
	writeRows(other, newSchema, newField, "new", false)

	got, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	assert.Equal(t, []string{"ID", "BRAND_NEW"}, got.ColumnNames(),
		"a recreated table must not be reported with the previous table's schema")
}

func TestLocalStoreSchemaEvolutionDetectsRecreatedTableWithChangedProtocol(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.protocol_recreated"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}

	for i := range 2 {
		require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table: table, Schema: tableSchema, DropFirst: i == 0,
		}))
		writeOneLakeTestBatch(t, dest, table, tableSchema, makeBatch(t, []arrow.Field{
			{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		}, [][]any{{int64(i)}}))
	}

	current, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	tableDir, err := dest.tableDirForTables(table, "test")
	require.NoError(t, err)
	logDir := tableDir + "/_delta_log"
	versionZero, err := client.Download(t.Context(), dest.workspace, logDir+"/"+commitFileName(0))
	require.NoError(t, err)
	fullLogDir, err := client.path(logDir)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(fullLogDir))
	require.NoError(t, client.UploadBufferSkippingPrefix(
		t.Context(), dest.workspace, logDir+"/"+commitFileName(0), versionZero, 0,
	))
	unsupportedProtocol := []byte(`{"protocol":{"minReaderVersion":3,"minWriterVersion":7,"readerFeatures":["deletionVectors"],"writerFeatures":["deletionVectors","rowTracking"]}}` + "\n")
	require.NoError(t, client.UploadBufferSkippingPrefix(
		t.Context(), dest.workspace, logDir+"/"+commitFileName(1), unsupportedProtocol, 0,
	))

	desired := &schema.TableSchema{Columns: append(
		append([]schema.Column(nil), current.Columns...),
		schema.Column{Name: "EXTERNAL_REF_ID", DataType: schema.TypeString},
	)}
	comparison, err := schemaevolution.Compare(desired, current, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	_, err = dest.ApplySchemaEvolution(t.Context(), table, comparison)
	require.ErrorContains(t, err, `writer feature "rowTracking"`)

	versions, err := client.ListLogVersions(t.Context(), dest.workspace, logDir)
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1}, versions, "schema evolution must not commit against the recreated table")
}

// TestLocalStoreGetTableSchemaDetectsRecreatedTableAfterAlter covers the same
// staleness with the cached metaData coming from a commit other than version 0,
// which is what an ALTER TABLE by another engine leaves behind. The recreated
// log carries no metaData at that version at all, so re-reading it is the only
// thing that can tell the two tables apart.
func TestLocalStoreGetTableSchemaDetectsRecreatedTableAfterAlter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	other, _ := newLocalOneLakeDestination(root)
	table := "analytics.altered"

	oldSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "OLD_ONLY", DataType: schema.TypeString},
	}}
	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "BRAND_NEW", DataType: schema.TypeString},
	}}
	writeRows := func(d *OneLakeDestination, tableSchema *schema.TableSchema, field arrow.Field, value any, dropFirst bool) {
		require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{
			Table: table, Schema: tableSchema, DropFirst: dropFirst,
		}))
		writeOneLakeTestBatch(t, d, table, tableSchema, makeBatch(t, []arrow.Field{
			{Name: "ID", Type: arrow.PrimitiveTypes.Int64}, field,
		}, [][]any{{int64(1), value}}))
	}

	oldField := arrow.Field{Name: "OLD_ONLY", Type: arrow.BinaryTypes.String}
	writeRows(dest, oldSchema, oldField, "old", true)
	writeRows(dest, oldSchema, oldField, "old", false)
	// An ALTER TABLE-style commit at version 2, so the cached metaData no longer
	// comes from version 0.
	rewriteDeltaMetadata(t, dest, client, table, func(deltaMetadata) {})
	current, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	require.Equal(t, []string{"ID", "OLD_ONLY"}, current.ColumnNames())

	newField := arrow.Field{Name: "BRAND_NEW", Type: arrow.BinaryTypes.String}
	writeRows(other, newSchema, newField, "new", true)
	writeRows(other, newSchema, newField, "new", false)
	writeRows(other, newSchema, newField, "new", false)

	got, err := dest.GetTableSchema(t.Context(), table)
	require.NoError(t, err)
	require.NotNil(t, got, "the recreated table must not read back as schema-less")
	assert.Equal(t, []string{"ID", "BRAND_NEW"}, got.ColumnNames())
}

// TestLocalStoreRewriteRejectsColumnMappedTable guards the copy-on-write read
// path: Parquet files of a column-mapped table carry physical column names, so
// projecting them onto the logical Delta schema would null out every value.
func TestLocalStoreRewriteRejectsColumnMappedTable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	table := "analytics.mapped"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, table, tableSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(7)}}))

	rewriteDeltaMetadata(t, dest, client, table, func(metadata deltaMetadata) {
		metadata["configuration"] = json.RawMessage(`{"delta.columnMapping.mode":"name"}`)
	})

	_, _, _, err := dest.readTable(t.Context(), table, "merge")
	require.ErrorContains(t, err, "column mapping mode")
}

// TestLocalStoreMergeSkipsDeclaredUnmappableColumnWithoutData covers a
// Fabric-style metadata-only ALTER TABLE ADD COLUMNS of a struct column: the
// column is declared in the Delta schema but present in no data file. The
// rewrite must skip it — the metaData action stays untouched, so readers keep
// NULL-filling it — rather than fail trying to materialize a type ingestr
// cannot map.
func TestLocalStoreMergeSkipsDeclaredUnmappableColumnWithoutData(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dest, client := newLocalOneLakeDestination(root)
	targetTable := "analytics.profiles"
	stagingTable := "analytics.profiles_staging"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: targetTable, Schema: tableSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, targetTable, tableSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(1)}}))

	rewriteDeltaMetadata(t, dest, client, targetTable, func(metadata deltaMetadata) {
		raw, err := json.Marshal(`{"type":"struct","fields":[` +
			`{"name":"ID","type":"long","nullable":true,"metadata":{}},` +
			`{"name":"PROFILE","type":{"type":"struct","fields":[]},"nullable":true,"metadata":{}}]}`)
		require.NoError(t, err)
		metadata["schemaString"] = raw
	})

	current, err := dest.GetTableSchema(t.Context(), targetTable)
	require.NoError(t, err)
	assert.Equal(t, []string{"ID"}, current.ColumnNames(), "the unmappable column must stay out of the write schema")

	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: stagingTable, Schema: tableSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, stagingTable, tableSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(2)}}))
	require.NoError(t, dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: targetTable, StagingTable: stagingTable, PrimaryKeys: []string{"ID"},
	}))

	_, snapshot, batches, err := dest.readTable(t.Context(), targetTable, "test")
	require.NoError(t, err)
	defer releaseBatches(batches)
	rows := collectRows(batches)
	require.Len(t, rows, 2)
	for _, row := range rows {
		_, hasProfile := row["PROFILE"]
		assert.False(t, hasProfile, "rewritten files must omit the column so readers NULL-fill it")
	}

	deltaSchema, err := deltaSchemaFromMetadata(snapshot.metadata)
	require.NoError(t, err)
	require.GreaterOrEqual(t, deltaFieldIndex(deltaSchema.Fields, "PROFILE"), 0,
		"the rewrite must keep the column declared in the Delta metadata")
}

func newLocalOneLakeTableWithRow(t *testing.T, table string) (*OneLakeDestination, *localDataLakeClient) {
	t.Helper()
	dest, client := newLocalOneLakeDestination(t.TempDir())
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true,
	}))
	writeOneLakeTestBatch(t, dest, table, tableSchema, makeBatch(t, []arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(1)}}))
	return dest, client
}

// TestLocalStoreRewriteRejectsDeletionVectorFiles guards the copy-on-write read
// path against tables whose engine deletes rows through deletion vectors: the
// Parquet file still contains those rows, so rewriting it would commit them
// back as live data.
func TestLocalStoreRewriteRejectsDeletionVectorFiles(t *testing.T) {
	t.Parallel()
	table := "analytics.dv"
	dest, client := newLocalOneLakeTableWithRow(t, table)

	commit := `{"add":{"path":"part-dv.parquet","partitionValues":{},"size":10,"modificationTime":1,"dataChange":true,"deletionVector":{"storageType":"u","pathOrInlineDv":"x","offset":1,"sizeInBytes":36,"cardinality":1}}}
{"commitInfo":{"operation":"DELETE"}}
`
	logDir := dest.itemPath() + "/Tables/analytics/dv/_delta_log"
	require.NoError(t, client.UploadBufferSkippingPrefix(
		t.Context(), dest.workspace, logDir+"/"+commitFileName(1), []byte(commit), 0,
	))

	_, _, _, err := dest.readTable(t.Context(), table, "merge")
	require.ErrorContains(t, err, "deletion vector")
	require.ErrorContains(t, err, "part-dv.parquet")
}

// TestLocalStoreRejectsUnsupportedProtocol covers tables whose protocol action
// demands writer capabilities ingestr does not have. Both the copy-on-write
// read path and the schema evolution commit have to refuse such tables instead
// of writing to them.
func TestLocalStoreRejectsUnsupportedProtocol(t *testing.T) {
	t.Parallel()
	table := "analytics.upgraded"
	dest, client := newLocalOneLakeTableWithRow(t, table)

	commit := `{"protocol":{"minReaderVersion":3,"minWriterVersion":7,"readerFeatures":["deletionVectors"],"writerFeatures":["deletionVectors","rowTracking"]}}
{"commitInfo":{"operation":"UPGRADE PROTOCOL"}}
`
	logDir := dest.itemPath() + "/Tables/analytics/upgraded/_delta_log"
	require.NoError(t, client.UploadBufferSkippingPrefix(
		t.Context(), dest.workspace, logDir+"/"+commitFileName(1), []byte(commit), 0,
	))

	_, _, _, err := dest.readTable(t.Context(), table, "merge")
	require.ErrorContains(t, err, `writer feature "rowTracking"`)

	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeAddColumn,
		ColumnName: "LABEL",
		NewColumn:  schema.Column{Name: "LABEL", DataType: schema.TypeString},
	}}}
	_, err = dest.ApplySchemaEvolution(t.Context(), table, comparison)
	require.ErrorContains(t, err, `writer feature "rowTracking"`)
}

// TestLocalStoreRewriteRejectsTruncatedLog covers a checkpointed table whose
// old JSON commits were removed by log cleanup while a recent commit (a
// SET TBLPROPERTIES-style one) still carries both a protocol and a metaData
// action. Every per-action gate passes on such a table, but the JSON-only
// replay cannot see the data files recorded in the checkpoint, so a rewrite
// would leave them live next to its own output.
func TestLocalStoreRewriteRejectsTruncatedLog(t *testing.T) {
	t.Parallel()
	table := "analytics.truncated"
	dest, client := newLocalOneLakeTableWithRow(t, table)

	_, snapshot, batches, err := dest.readTable(t.Context(), table, "test")
	require.NoError(t, err)
	releaseBatches(batches)
	metadataRaw, err := json.Marshal(snapshot.metadata)
	require.NoError(t, err)

	commit := `{"protocol":{"minReaderVersion":1,"minWriterVersion":2}}` + "\n" +
		`{"metaData":` + string(metadataRaw) + "}\n" +
		`{"commitInfo":{"operation":"SET TBLPROPERTIES"}}` + "\n"
	logDir := dest.itemPath() + "/Tables/analytics/truncated/_delta_log"
	require.NoError(t, client.UploadBufferSkippingPrefix(
		t.Context(), dest.workspace, logDir+"/"+commitFileName(1), []byte(commit), 0,
	))
	fullPath, err := client.path(logDir + "/" + commitFileName(0))
	require.NoError(t, err)
	require.NoError(t, os.Remove(fullPath))

	_, _, _, err = dest.readTable(t.Context(), table, "merge")
	require.ErrorContains(t, err, "truncated")
	require.ErrorContains(t, err, "oldest commit is 1")
}

// TestLocalStoreRewriteRejectsTableFeaturesInUse covers the table properties a
// plain remove-and-add rewrite commit would silently violate even though the
// feature names themselves are tolerated in the protocol action.
func TestLocalStoreRewriteRejectsTableFeaturesInUse(t *testing.T) {
	t.Parallel()

	setFieldMetadata := func(t *testing.T, metadata deltaMetadata, key string, value any) {
		t.Helper()
		deltaSchema, err := deltaSchemaFromMetadata(metadata)
		require.NoError(t, err)
		deltaSchema.Fields[0].Metadata[key] = value
		updated, err := deltaMetadataWithSchema(metadata, deltaSchema)
		require.NoError(t, err)
		metadata["schemaString"] = updated["schemaString"]
	}

	cases := []struct {
		name    string
		mutate  func(t *testing.T, metadata deltaMetadata)
		wantErr string
	}{
		{
			"append only",
			func(t *testing.T, metadata deltaMetadata) {
				t.Helper()
				metadata["configuration"] = json.RawMessage(`{"delta.appendOnly":"true"}`)
			},
			"append-only Delta table",
		},
		{
			"change data feed",
			func(t *testing.T, metadata deltaMetadata) {
				t.Helper()
				metadata["configuration"] = json.RawMessage(`{"delta.enableChangeDataFeed":"true"}`)
			},
			"change data files",
		},
		{
			"check constraint",
			func(t *testing.T, metadata deltaMetadata) {
				t.Helper()
				metadata["configuration"] = json.RawMessage(`{"delta.constraints.positive_id":"ID > 0"}`)
			},
			`CHECK constraint "delta.constraints.positive_id"`,
		},
		{
			"invariant",
			func(t *testing.T, metadata deltaMetadata) {
				t.Helper()
				setFieldMetadata(t, metadata, "delta.invariants", `{"expression":{"expression":"ID > 0"}}`)
			},
			`invariant on column "ID"`,
		},
		{
			"generated column",
			func(t *testing.T, metadata deltaMetadata) {
				t.Helper()
				setFieldMetadata(t, metadata, "delta.generationExpression", "ID + 1")
			},
			`generated column "ID"`,
		},
		{
			"identity column",
			func(t *testing.T, metadata deltaMetadata) {
				t.Helper()
				setFieldMetadata(t, metadata, "delta.identity.start", float64(1))
			},
			`identity column "ID"`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table := "analytics.features"
			dest, client := newLocalOneLakeTableWithRow(t, table)
			rewriteDeltaMetadata(t, dest, client, table, func(metadata deltaMetadata) {
				tt.mutate(t, metadata)
			})

			_, _, _, err := dest.readTable(t.Context(), table, "merge")
			require.ErrorContains(t, err, tt.wantErr)
			require.ErrorContains(t, err, "merge")
		})
	}
}

// rewriteDeltaMetadata commits a new metaData action for an existing table so a
// test can give it properties ingestr itself never writes. It returns the
// table directory.
func rewriteDeltaMetadata(t *testing.T, dest *OneLakeDestination, client *localDataLakeClient, table string, mutate func(deltaMetadata)) string {
	t.Helper()
	dir, snapshot, batches, err := dest.readTable(t.Context(), table, "test")
	require.NoError(t, err)
	releaseBatches(batches)

	deltaSchema, err := deltaSchemaFromMetadata(snapshot.metadata)
	require.NoError(t, err)
	metadata, err := deltaMetadataWithSchema(snapshot.metadata, deltaSchema)
	require.NoError(t, err)
	mutate(metadata)
	commit, err := buildSchemaEvolutionCommit(metadata, 5)
	require.NoError(t, err)
	logDir := dir + "/_delta_log"
	require.NoError(t, client.UploadBufferSkippingPrefix(
		t.Context(), dest.workspace, logDir+"/"+commitFileName(snapshot.version+1), commit, 0,
	))
	return dir
}
