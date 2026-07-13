package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeLSNOrdersEventsWithinTransactionAndParsesForResume(t *testing.T) {
	lsn := pglogrepl.LSN(0x2a)
	deleted := FormatChangeLSN(lsn, 2)
	reinserted := FormatChangeLSN(lsn, 4)
	require.Less(t, deleted, reinserted)

	parsed, err := parseStoredPostgresLSN(reinserted)
	require.NoError(t, err)
	require.Equal(t, lsn, parsed)
}

func TestDecoderAssignsStableTransactionLocalSequenceAcrossDrains(t *testing.T) {
	d := NewDecoder(&schema.TableSchema{}, "public", "items")
	require.NoError(t, d.appendChange(Change{Operation: "DELETE", LSN: 42}))
	require.NoError(t, d.appendChange(Change{Operation: "INSERT", LSN: 42}))
	require.NoError(t, d.pendingChanges.Seal())
	d.committed = d.pendingChanges

	first, err := d.DrainCommitted(1)
	require.NoError(t, err)
	second, err := d.DrainCommitted(1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), first[0].Sequence)
	require.Equal(t, uint64(4), second[0].Sequence)
}

func TestBatchAccumulatorFlushesOnByteLimitBelowRowThreshold(t *testing.T) {
	tableSchema := keyedTestSchema()
	a := newBatchAccumulator(25_000, map[string]*schema.TableSchema{"items": tableSchema})
	a.byteLimit = 1024
	a.add("items", []Change{{Operation: "INSERT", LSN: 1, Sequence: 2, Values: []interface{}{int32(1), strings.Repeat("x", 2048)}}}, 1)

	results := make(chan source.RecordBatchResult, 1)
	require.NoError(t, a.flushReadyContext(context.Background(), results, nil))
	result := <-results
	require.NotNil(t, result.Batch)
	result.Batch.Release()
	assert.Empty(t, a.changes)
	assert.Zero(t, a.totalBytes)
}

func TestBatchAccumulatorByteLimitIsGlobalAcrossTables(t *testing.T) {
	tables := []string{"one", "two", "three", "four", "five"}
	schemas := make(map[string]*schema.TableSchema, len(tables))
	for _, table := range tables {
		schemas[table] = keyedTestSchema()
	}
	a := newBatchAccumulator(25_000, schemas)
	a.byteLimit = 4096
	for i, table := range tables {
		a.add(table, []Change{{
			Operation: "INSERT",
			LSN:       pglogrepl.LSN(i + 1),
			Sequence:  2,
			Values:    []interface{}{int32(i + 1), strings.Repeat("x", 240)},
		}}, pglogrepl.LSN(i+1))
		require.Less(t, a.bytes[table], a.byteLimit, "each table must remain below the per-table limit")
	}
	require.GreaterOrEqual(t, a.totalBytes, a.byteLimit)

	results := make(chan source.RecordBatchResult, len(tables))
	require.NoError(t, a.flushReadyContext(context.Background(), results, nil))
	require.Less(t, a.totalBytes, a.byteLimit)

	var accounted int64
	for _, size := range a.bytes {
		accounted += size
	}
	assert.Equal(t, accounted, a.totalBytes)
	close(results)
	flushed := 0
	for result := range results {
		flushed++
		result.Batch.Release()
	}
	assert.Positive(t, flushed)
}

func TestBatchAccumulatorAccountsForManyTableContainerMemory(t *testing.T) {
	const tableCount = 64
	schemas := make(map[string]*schema.TableSchema, tableCount)
	a := newBatchAccumulator(25_000, schemas)
	a.byteLimit = 32 << 10
	for tableIndex := 0; tableIndex < tableCount; tableIndex++ {
		table := fmt.Sprintf("table_%03d", tableIndex)
		schemas[table] = keyedTestSchema()
		changes := make([]Change, 16)
		for row := range changes {
			changes[row] = Change{Operation: "INSERT", LSN: pglogrepl.LSN(tableIndex + 1), Values: []interface{}{int32(row), "x"}}
		}
		a.add(table, changes, pglogrepl.LSN(tableIndex+1))
	}

	results := make(chan source.RecordBatchResult, tableCount)
	require.NoError(t, a.flushReadyContext(context.Background(), results, nil))
	require.Less(t, a.totalBytes, a.byteLimit)

	var retainedLowerBound int64
	for table, changes := range a.changes {
		retainedLowerBound += int64(cap(changes)) * changeStructBytes
		for i := range changes {
			retainedLowerBound += int64(cap(changes[i].Values)+cap(changes[i].OldValues)) * interfaceBytes
		}
		require.GreaterOrEqual(t, a.bytes[table], int64(cap(changes))*changeStructBytes)
	}
	require.GreaterOrEqual(t, a.totalBytes, retainedLowerBound)

	close(results)
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	a.discard()
	require.Zero(t, a.totalBytes)
	require.Empty(t, a.bytes)
	require.Empty(t, a.changes)
}

func TestBatchAccumulatorGlobalByteFlushKeepsPendingLowWater(t *testing.T) {
	tables := []string{"old", "middle", "largest"}
	schemas := make(map[string]*schema.TableSchema, len(tables))
	for _, table := range tables {
		schemas[table] = keyedTestSchema()
	}
	a := newBatchAccumulator(25_000, schemas)
	a.byteLimit = 4096
	a.add("old", []Change{{Operation: "INSERT", LSN: 100, Values: []interface{}{int32(1), strings.Repeat("a", 220)}}}, 100)
	a.add("middle", []Change{{Operation: "INSERT", LSN: 200, Values: []interface{}{int32(2), strings.Repeat("b", 220)}}}, 200)
	a.add("largest", []Change{{Operation: "INSERT", LSN: 300, Values: []interface{}{int32(3), strings.Repeat("c", 600)}}}, 300)
	require.GreaterOrEqual(t, a.totalBytes, a.byteLimit)

	results := make(chan source.RecordBatchResult, len(tables))
	repl := &fakeReplicator{lsn: 300}
	require.NoError(t, a.flushReadyContext(context.Background(), results, func() any {
		return safeCommitLSN(repl, a)
	}))
	close(results)

	result := <-results
	require.Equal(t, "largest", result.TableName)
	require.Equal(t, pglogrepl.LSN(99), result.CommitToken)
	result.Batch.Release()
	assert.Contains(t, a.changes, "old")
	for result := range results {
		require.Equal(t, pglogrepl.LSN(99), result.CommitToken)
		result.Batch.Release()
	}
}

func TestPublicationCoverageGapForcesReplacementOnSameOIDReturn(t *testing.T) {
	current := []source.SourceTableInfo{{Name: "public.items", Incarnation: "42"}}
	missing := make(map[string]struct{})
	assert.Empty(t, updateCoverageGaps(current, map[string]string{}, missing))
	require.Contains(t, missing, "public.items")
	assert.Equal(t, []string{"public.items"}, updateCoverageGaps(current, map[string]string{"public.items": "42"}, missing))
}

func TestReplicatorDetectsCatalogSchemaChangeWithoutRelationMessage(t *testing.T) {
	repl, err := NewReplicator(NewPostgresCDCSource(), "public.items", &schema.TableSchema{}, CDCConfig{DiscoverInterval: time.Nanosecond}, 0, true, "")
	require.NoError(t, err)
	require.NoError(t, repl.ExpectTableIncarnation("42"))
	repl.ExpectTableSchemaFingerprint("old")
	repl.incarnationLookup = func(context.Context) (string, error) { return "42", nil }
	repl.schemaFingerprintLookup = func(context.Context) (string, error) { return "new", nil }

	err = repl.checkTableIncarnation(context.Background())
	var changed *SchemaChangedError
	require.ErrorAs(t, err, &changed)
}

func TestResumeMetadataRejectsStateAuthorizedForPreviousTable(t *testing.T) {
	assert.True(t, resumeMetadataChanged("100", "schema-a", "200", "schema-a"))
	assert.True(t, resumeMetadataChanged("100", "schema-a", "100", "schema-b"))
	assert.False(t, resumeMetadataChanged("100", "schema-a", "100", "schema-a"))
}

func TestBatchSnapshotStopsBeforePostSnapshotWALMerge(t *testing.T) {
	assert.True(t, stopAfterBatchSnapshot(ModeBatch, false))
	assert.False(t, stopAfterBatchSnapshot(ModeBatch, true))
	assert.False(t, stopAfterBatchSnapshot(ModeStream, false))
}
