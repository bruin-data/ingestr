//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestReadAllReplacesLegacyPartitionedSnapshotResume(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TABLE partition_resume_anchor (id bigint PRIMARY KEY);
		CREATE TABLE partition_resume_items (id bigint PRIMARY KEY) PARTITION BY RANGE (id);
		CREATE TABLE partition_resume_low PARTITION OF partition_resume_items FOR VALUES FROM (MINVALUE) TO (10);
		CREATE TABLE partition_resume_high PARTITION OF partition_resume_items FOR VALUES FROM (10) TO (MAXVALUE);
		INSERT INTO partition_resume_anchor VALUES (1);
		INSERT INTO partition_resume_items VALUES (1), (2), (3), (11), (12);
		CREATE PUBLICATION partition_resume_pub FOR TABLE partition_resume_anchor, partition_resume_items
			WITH (publish_via_partition_root = true);
	`)
	require.NoError(t, err)
	var originalPosition string
	require.NoError(t, pool.QueryRow(ctx, `SELECT lsn::text FROM pg_create_logical_replication_slot('partition_resume_slot', 'pgoutput')`).Scan(&originalPosition))
	originalLSN, err := pglogrepl.ParseLSN(originalPosition)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO partition_resume_items VALUES (4), (13);
		UPDATE partition_resume_anchor SET id = 2;
	`)
	require.NoError(t, err)

	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, strings.Replace(uri, "postgres://", "postgres+cdc://", 1)+"&publication=partition_resume_pub&slot=partition_resume_slot"))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	tables, err := src.GetTables(ctx)
	require.NoError(t, err)
	require.Len(t, tables, 2, "the publication must expose the partition root")
	byName := make(map[string]source.SourceTableInfo, len(tables))
	for _, table := range tables {
		byName[table.Name] = table
	}
	anchorName := publicationTableFullName("public", "partition_resume_anchor")
	itemsName := publicationTableFullName("public", "partition_resume_items")
	require.Contains(t, byName, anchorName)
	require.Contains(t, byName, itemsName)
	anchor, items := byName[anchorName], byName[itemsName]
	require.Equal(t, []string{"id"}, resumableSnapshotKeys(items))
	readCtx, stopRead := context.WithCancel(ctx)
	results, err := src.ReadAll(readCtx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true, Parallelism: 4, PageSize: 1,
		},
		KnownTables:                 []string{anchorName, itemsName},
		CDCResumeLSNs:               map[string]string{anchorName: originalPosition},
		CDCResumeIncarnations:       map[string]string{anchorName: anchor.Incarnation},
		CDCResumeSchemaFingerprints: map[string]string{anchorName: anchor.SchemaFingerprint},
		CDCSnapshotResumes: map[string]source.CDCSnapshotProgress{
			itemsName: {
				Boundary: FormatLSN(originalLSN), Key: `["11"]`,
				Incarnation: items.Incarnation, SchemaFingerprint: items.SchemaFingerprint,
			},
		},
	})
	defer stopRead()
	require.NoError(t, err)
	defer func() {
		stopRead()
		for result := range results {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
	}()

	var keys []string
	var replacementPosition string
	var announced, completed bool
	markers := 0
	for result := range results {
		require.NoError(t, result.Err)
		if result.TableInfo != nil && result.TableName == itemsName {
			announced = true
		}
		if result.Truncate {
			require.Equal(t, itemsName, result.TableName, "the completed anchor must not be replaced")
			require.True(t, announced, "announcement must precede replacement")
			require.False(t, result.CDCSnapshotResumed)
			markers++
			replacementPosition = result.CDCSnapshotPosition
			position, err := pglogrepl.ParseLSN(replacementPosition)
			require.NoError(t, err)
			require.Greater(t, position, originalLSN, "replacement must use the new export boundary")
		}
		if result.Batch != nil {
			func() {
				defer result.Batch.Release()
				require.Equal(t, itemsName, result.TableName)
				require.True(t, result.CDCSnapshot)
				require.NotEmpty(t, replacementPosition, "replacement must precede snapshot rows")
				lsnColumn := result.Batch.Schema().FieldIndices(CDCLSNColumn)[0]
				for row := 0; row < int(result.Batch.NumRows()); row++ {
					keys = append(keys, result.Batch.Column(0).ValueStr(row))
					require.Equal(t, replacementPosition, result.Batch.Column(lsnColumn).ValueStr(row))
				}
			}()
		}
		if token, ok := result.CommitToken.(source.CDCStateCommitToken); ok && token.SnapshotPositions[itemsName] != "" {
			require.Equal(t, map[string]string{itemsName: replacementPosition}, token.SnapshotPositions)
			require.Equal(t, items.Incarnation, token.SnapshotIncarnations[itemsName])
			require.Equal(t, items.SchemaFingerprint, token.SnapshotSchemas[itemsName])
			completed = true
			break
		}
	}
	require.True(t, completed, "replacement must complete before streaming WAL")
	require.Equal(t, 1, markers)
	require.ElementsMatch(t, []string{"1", "2", "3", "4", "11", "12", "13"}, keys,
		"a rejected legacy cursor must reread both partitions, including keys behind its cursor")
}
