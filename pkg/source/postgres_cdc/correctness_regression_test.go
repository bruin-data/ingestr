package postgres_cdc

import (
	"context"
	"fmt"
	"os"
	"sort"
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

func TestBatchAccumulatorEmitsStableUniqueDataBatchIdentity(t *testing.T) {
	for schemaName, tableSchema := range map[string]*schema.TableSchema{
		"keyless": keylessTestSchema(),
		"keyed":   keyedTestSchema(),
	} {
		for _, tableName := range []string{"", "public.events"} {
			t.Run(schemaName+"/"+tableName, func(t *testing.T) {
				flushWindows := func(windows [][]Change) []source.CDCStateCommitToken {
					a := newBatchAccumulator(100, map[string]*schema.TableSchema{tableName: tableSchema})
					a.stableAll = true
					results := make(chan source.RecordBatchResult, 16)
					for _, window := range windows {
						a.add(tableName, window, window[0].LSN)
						require.NoError(t, a.flushStableKeylessContext(t.Context(), results, func() any {
							return source.CDCStateCommitToken{Position: "00000000/00000100"}
						}, 0, false))
						require.NoError(t, a.flushAllContext(t.Context(), results, func() any {
							return source.CDCStateCommitToken{Position: "00000000/00000100"}
						}))
					}
					close(results)
					var tokens []source.CDCStateCommitToken
					for result := range results {
						require.NotNil(t, result.Batch)
						result.Batch.Release()
						token, ok := result.CommitToken.(source.CDCStateCommitToken)
						require.True(t, ok)
						tokens = append(tokens, token)
					}
					return tokens
				}

				c1 := Change{Operation: "INSERT", LSN: 0x110, Sequence: 2, Values: []interface{}{int32(1), "same"}}
				c2 := Change{Operation: "INSERT", LSN: 0x120, Sequence: 2, Values: []interface{}{int32(1), "same"}}
				c3 := Change{Operation: "INSERT", LSN: 0x130, Sequence: 2, Values: []interface{}{int32(1), "same"}}
				firstGrouping := flushWindows([][]Change{{c1, c2}, {c3}})
				retryGrouping := flushWindows([][]Change{{c1, c2, c3}})

				require.Len(t, firstGrouping, 3)
				require.Equal(t, firstGrouping, retryGrouping, "accumulator window regrouping must not change transaction write identities")
				require.NotEqual(t, firstGrouping[0].DataBatchID, firstGrouping[1].DataBatchID, "duplicate payloads from distinct transactions need distinct identities")
				require.NotEqual(t, firstGrouping[1].DataBatchID, firstGrouping[2].DataBatchID)
			})
		}
	}
}

func TestDataBatchIdentityIsTableScoped(t *testing.T) {
	changes := []Change{{LSN: 0x110, Sequence: 2}}
	require.NotEqual(t, transactionDataBatchID("public.events", changes), transactionDataBatchID("public.audit", changes))
	require.True(t, strings.HasPrefix(string(transactionDataBatchID("public.events", changes)), "pgcdc-keyless-v1:"))
}

func TestIncompleteKeylessSequenceWindowIsNotFlushedOnErrorPath(t *testing.T) {
	tableSchema := keylessTestSchema()
	accum := newBatchAccumulator(1, map[string]*schema.TableSchema{"public.events": tableSchema})
	accum.stableAll = true
	accum.add("public.events", []Change{{
		Operation: "INSERT", LSN: 0x900, Sequence: 2, Values: []interface{}{int32(1), "same"},
	}}, 0x900)
	results := make(chan source.RecordBatchResult, 1)
	token := func() any { return source.CDCStateCommitToken{Position: "00000000/000008FF"} }

	require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, token, 0x900, true))
	require.NoError(t, accum.flushReadyContext(t.Context(), results, token))
	require.NoError(t, accum.flushAllContext(t.Context(), results, token))
	require.Empty(t, results, "a partial deterministic window must be replayed, not emitted with a drain-dependent identity")

	require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, token, 0, false))
	result := <-results
	require.NotNil(t, result.Batch)
	result.Batch.Release()
}

func TestKeylessDataBatchWindowIsByteBoundedAndCapacityIndependent(t *testing.T) {
	compactValues := []interface{}{strings.Repeat("x", 128)}
	spareValues := make([]interface{}, 1, 64)
	spareValues[0] = compactValues[0]
	compact := Change{Operation: "INSERT", Values: compactValues}
	spare := Change{Operation: "INSERT", Values: spareValues}
	require.Equal(t, stableChangeBytes(compact), stableChangeBytes(spare))

	large := Change{Operation: "INSERT", Values: []interface{}{strings.Repeat("x", int(keylessDataBatchWindowBytes/2)+1)}}
	state := keylessBatchWindowState{}
	state.assign(&large)
	require.Equal(t, uint64(0), large.DataBatchWindow)
	second := large
	state.assign(&second)
	require.Equal(t, uint64(1), second.DataBatchWindow)
}

func TestExpandedKeylessUpdateRetainsDataBatchWindow(t *testing.T) {
	changes := expandUpdates([]Change{{
		Operation: "UPDATE", LSN: 0x900, Sequence: 8, DataBatchWindow: 3,
		Values: []interface{}{int32(2)}, OldValues: []interface{}{int32(1)},
	}}, keylessTestSchema())
	require.Len(t, changes, 2)
	require.Equal(t, uint64(3), changes[0].DataBatchWindow)
	require.Equal(t, uint64(3), changes[1].DataBatchWindow)
}

func TestLargeKeylessTransactionFragmentsHaveStableUniqueIdentities(t *testing.T) {
	const changeCount = defaultCommittedDrainChanges*2 + 2
	run := func(drainLimit int) ([]source.DurableID, int64) {
		tableSchema := keylessTestSchema()
		decoder := NewDecoder(tableSchema, "public", "events")
		decoder.currentTxLSN = 0x900
		for i := 0; i < changeCount; i++ {
			require.NoError(t, decoder.appendChange(Change{
				Operation: "INSERT", LSN: decoder.currentTxLSN, Values: []interface{}{int32(i), "same"},
			}))
		}
		require.NoError(t, decoder.pendingChanges.Seal())
		decoder.committed = decoder.pendingChanges
		decoder.pendingChanges = newChangeSpoolWithBudget[Change](defaultTransactionMemoryBytes, decoder.memoryBudget, nil)
		accum := newBatchAccumulator(changeCount+1, map[string]*schema.TableSchema{"": tableSchema})
		accum.stableAll = true
		results := make(chan source.RecordBatchResult, 8)
		flush := func(changes []Change) {
			accum.add("", changes, decoder.currentTxLSN)
			pendingLow, pending := decoder.CommittedLowWater()
			require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, func() any {
				return source.CDCStateCommitToken{Position: "00000000/00000800"}
			}, pendingLow, pending))
		}
		for decoder.HasCommitted() {
			fragment, err := decoder.DrainCommitted(drainLimit)
			require.NoError(t, err)
			flush(fragment)
		}
		require.NoError(t, decoder.Close())
		close(results)
		var ids []source.DurableID
		var rows int64
		for result := range results {
			token := result.CommitToken.(source.CDCStateCommitToken)
			ids = append(ids, token.DataBatchID)
			rows += result.Batch.NumRows()
			result.Batch.Release()
		}
		return ids, rows
	}

	firstIDs, firstRows := run(257)
	replayIDs, replayRows := run(defaultCommittedDrainChanges + 333)
	require.EqualValues(t, changeCount, firstRows)
	require.Equal(t, firstRows, replayRows)
	require.Len(t, firstIDs, 3)
	require.Equal(t, firstIDs, replayIDs)
	require.Len(t, map[source.DurableID]struct{}{firstIDs[0]: {}, firstIDs[1]: {}, firstIDs[2]: {}}, 3)
}

func TestLargeMultiTableKeylessTransactionFragmentsHaveStableUniqueIdentities(t *testing.T) {
	const changesPerTable = defaultCommittedDrainChanges*2 + 2
	run := func(drainLimit int, blockedInterleaving bool) ([]string, int64) {
		tableSchema := keylessTestSchema()
		tables := []source.SourceTableInfo{{Name: "public.events", Schema: tableSchema}, {Name: "public.audit", Schema: tableSchema}}
		decoder := NewMultiTableDecoder(tables)
		decoder.currentTxLSN = 0x900
		appendTableChange := func(table string, value int) {
			require.NoError(t, decoder.appendChange(TableChange{TableName: table, Change: Change{
				Operation: "INSERT", Values: []interface{}{int32(value), "same"},
			}}))
		}
		if blockedInterleaving {
			for _, table := range tables {
				for i := 0; i < changesPerTable; i++ {
					appendTableChange(table.Name, i)
				}
			}
		} else {
			for i := 0; i < changesPerTable; i++ {
				for _, table := range tables {
					appendTableChange(table.Name, i)
				}
			}
		}
		require.NoError(t, decoder.pendingChanges.Seal())
		decoder.committed = decoder.pendingChanges
		decoder.committedLSN = decoder.currentTxLSN
		decoder.committedTableSequences = make(map[string]uint64)
		decoder.pendingChanges = newChangeSpoolWithBudget[streamedChange](defaultTransactionMemoryBytes, decoder.memoryBudget, streamedChangeXID)
		schemas := map[string]*schema.TableSchema{tables[0].Name: tableSchema, tables[1].Name: tableSchema}
		accum := newBatchAccumulator(changesPerTable*len(tables)+1, schemas)
		accum.stableAll = true
		accum.byteLimit = 12 << 10
		defer accum.discard()
		results := make(chan source.RecordBatchResult, 16)
		flush := func(groups []DecodedChanges) {
			for _, group := range groups {
				accum.add(group.TableName, group.Changes, group.LSN)
			}
			pendingLow, pending := decoder.CommittedLowWater()
			require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, func() any {
				return source.CDCStateCommitToken{Position: "00000000/00000800"}
			}, pendingLow, pending))
			require.NoError(t, accum.flushReadyContext(t.Context(), results, func() any {
				return source.CDCStateCommitToken{Position: "00000000/00000800"}
			}))
		}
		for decoder.HasCommitted() {
			groups, err := decoder.DrainCommitted(drainLimit)
			require.NoError(t, err)
			flush(groups)
		}
		require.NoError(t, decoder.Close())
		close(results)
		var ids []string
		var rows int64
		for result := range results {
			token := result.CommitToken.(source.CDCStateCommitToken)
			ids = append(ids, string(token.DataBatchID))
			rows += result.Batch.NumRows()
			result.Batch.Release()
		}
		sort.Strings(ids)
		return ids, rows
	}

	firstIDs, firstRows := run(317, false)
	replayIDs, replayRows := run(defaultCommittedDrainChanges+471, true)
	require.EqualValues(t, changesPerTable*2, firstRows)
	require.Equal(t, firstRows, replayRows)
	require.Len(t, firstIDs, 6)
	require.Equal(t, firstIDs, replayIDs)
	unique := make(map[string]struct{}, len(firstIDs))
	for _, id := range firstIDs {
		unique[id] = struct{}{}
	}
	require.Len(t, unique, len(firstIDs))
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

func TestStableBatchAccumulatorSpillsBlockedWindowsUnderGlobalMemoryPressure(t *testing.T) {
	tables := map[string]*schema.TableSchema{
		"public.keyed_a":   keyedTestSchema(),
		"public.keyless_a": keylessTestSchema(),
		"public.keyed_b":   keyedTestSchema(),
		"public.keyless_b": keylessTestSchema(),
	}
	accum := newBatchAccumulator(25_000, tables)
	accum.byteLimit = 6 << 10
	accum.stableAll = true
	t.Cleanup(accum.discard)
	for tableName := range tables {
		accum.add(tableName, []Change{{
			Operation: "INSERT", LSN: 0x900, Sequence: 2, DataBatchWindow: 0,
			Values: []interface{}{int32(1), strings.Repeat(tableName, 80)},
		}}, 0x900)
	}
	require.GreaterOrEqual(t, accum.totalBytes, accum.byteLimit)

	results := make(chan source.RecordBatchResult, 32)
	token := func() any { return source.CDCStateCommitToken{Position: "0/8FF"} }
	require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, token, 0x900, true))
	require.NoError(t, accum.flushReadyContext(t.Context(), results, token))
	require.Less(t, accum.totalBytes, accum.byteLimit)
	require.NotEmpty(t, accum.stableSpills)
	require.Empty(t, results, "incomplete durable windows must spill, not flush")

	spillPaths := make([]string, 0, len(accum.stableSpills))
	for _, spill := range accum.stableSpills {
		require.NotNil(t, spill.spool.file)
		spillPaths = append(spillPaths, spill.spool.file.Name())
	}
	for tableName := range tables {
		accum.add(tableName, []Change{{
			Operation: "INSERT", LSN: 0x900, Sequence: 4, DataBatchWindow: 0,
			Values: []interface{}{int32(2), tableName},
		}}, 0x900)
	}
	require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, token, 0, false))
	require.NoError(t, accum.flushAllContext(t.Context(), results, token))
	require.Empty(t, accum.stableSpills)
	close(results)

	seen := make(map[string]source.CDCStateCommitToken, len(tables))
	for result := range results {
		require.NotNil(t, result.Batch)
		require.EqualValues(t, 2, result.Batch.NumRows())
		commit, ok := result.CommitToken.(source.CDCStateCommitToken)
		require.True(t, ok)
		seen[result.TableName] = commit
		result.Batch.Release()
	}
	require.Len(t, seen, len(tables))
	for tableName, commit := range seen {
		require.Equal(t, transactionDataBatchID(tableName, []Change{{LSN: 0x900}}), commit.DataBatchID)
	}
	for _, path := range spillPaths {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestStableBatchAccumulatorDiscardRemovesOversizedWindowSpill(t *testing.T) {
	const tableName = "public.oversized"
	accum := newBatchAccumulator(25_000, map[string]*schema.TableSchema{tableName: keylessTestSchema()})
	accum.byteLimit = 1024
	accum.stableAll = true
	accum.add(tableName, []Change{{
		Operation: "INSERT", LSN: 0x900, Sequence: 2,
		Values: []interface{}{int32(1), strings.Repeat("x", 16<<10)},
	}}, 0x900)
	results := make(chan source.RecordBatchResult, 1)
	require.NoError(t, accum.flushStableKeylessContext(t.Context(), results, nil, 0x900, true))
	require.NoError(t, accum.flushReadyContext(t.Context(), results, nil))
	require.Zero(t, accum.totalBytes)
	require.Empty(t, results)
	spill := accum.stableSpills[tableName]
	require.NotNil(t, spill)
	path := spill.spool.file.Name()

	accum.discard()
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
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
