package postgres_cdc

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestSnapshotChunkQueryPreservesNativeCompositeBounds(t *testing.T) {
	query, args := snapshotChunkQuery(
		[]string{`"tenant"`, `"id"`, `"payload"`}, `"public"."items"`,
		[]string{"tenant", "id"}, []string{"text", "bigint"},
		[]string{"acme", "9"}, []string{"zeta", "100"}, 200,
	)
	require.Equal(t, `SELECT "tenant", "id", "payload", __ingestr_snapshot."tenant"::text AS "__ingestr_key_0", __ingestr_snapshot."id"::text AS "__ingestr_key_1" FROM "public"."items" AS __ingestr_snapshot WHERE (__ingestr_snapshot."tenant", __ingestr_snapshot."id") > ($1::text::text, $2::text::bigint) AND (__ingestr_snapshot."tenant", __ingestr_snapshot."id") <= ($3::text::text, $4::text::bigint) ORDER BY __ingestr_snapshot."tenant", __ingestr_snapshot."id" LIMIT $5`, query)
	require.Equal(t, []any{"acme", "9", "zeta", "100", 200}, args)
}

func TestSnapshotChunkQueryFirstChunkAndUntrustedKey(t *testing.T) {
	upper := `'); DROP TABLE items; --`
	query, args := snapshotChunkQuery([]string{`"key"`}, `"public"."items"`, []string{"key"}, []string{"text"}, nil, []string{upper}, 25)
	require.NotContains(t, query, upper)
	require.Contains(t, query, `WHERE (__ingestr_snapshot."key") <= ($1::text::text)`)
	require.NotContains(t, query, " > ")
	require.Contains(t, query, `ORDER BY __ingestr_snapshot."key" LIMIT $2`)
	require.Equal(t, []any{upper, 25}, args)
}

func TestSnapshotHeapChunkQuery(t *testing.T) {
	query, args := snapshotHeapChunkQuery(
		[]string{`"id"`, `"toasted_payload"`}, `"public"."items"`,
		[]string{"(12,3)"}, []string{"(99,7)"}, 200,
	)
	require.Equal(t, `SELECT "id", "toasted_payload", __ingestr_snapshot.ctid::text AS "__ingestr_key_0" FROM "public"."items" AS __ingestr_snapshot WHERE __ingestr_snapshot.ctid > $1::tid AND __ingestr_snapshot.ctid <= $2::tid LIMIT $3`, query)
	require.Equal(t, []any{"(12,3)", "(99,7)", 200}, args)
	require.NotContains(t, query, "ORDER BY")
}

func TestSnapshotHeapChunkQueryOmitsFirstLowerBound(t *testing.T) {
	query, args := snapshotHeapChunkQuery([]string{`"id"`}, `"public"."items"`, nil, []string{"(99,7)"}, 25)
	require.Equal(t, `SELECT "id", __ingestr_snapshot.ctid::text AS "__ingestr_key_0" FROM "public"."items" AS __ingestr_snapshot WHERE __ingestr_snapshot.ctid <= $1::tid LIMIT $2`, query)
	require.Equal(t, []any{"(99,7)", 25}, args)
}

func TestSnapshotCursorSelectionControlsChunkOrder(t *testing.T) {
	started := &chunkTableState{After: []string{"5"}, Upper: []string{"100"}}
	cursor := snapshotCursor(started, "heap")
	query, _, keyColumns := snapshotChunkQueryForCursor(
		cursor, []string{`"id"`}, `"public"."items"`, []string{"id"}, []string{"bigint"},
		started.After, started.Upper, 25,
	)
	require.Equal(t, "key", cursor)
	require.Contains(t, query, `ORDER BY __ingestr_snapshot."id"`)
	require.NotContains(t, query, "ctid")
	require.Equal(t, 1, keyColumns)

	fresh := &chunkTableState{}
	cursor = snapshotCursor(fresh, "heap")
	query, _, keyColumns = snapshotChunkQueryForCursor(
		cursor, []string{`"id"`}, `"public"."new_items"`, []string{"id"}, nil,
		fresh.After, []string{"(9,9)"}, 25,
	)
	require.Equal(t, "heap", cursor)
	require.Contains(t, query, "ctid")
	require.NotContains(t, query, "ORDER BY")
	require.Equal(t, 1, keyColumns)
}

func TestSnapshotHeapUpperQueryDoesNotReadToastedColumns(t *testing.T) {
	query := snapshotHeapUpperQuery("public.items")
	require.Equal(t, `SELECT ctid::text FROM "public"."items" ORDER BY ctid DESC LIMIT 1`, query)
	require.NotContains(t, query, "payload")
}

func TestSnapshotChunkBoundsRejectIncompleteCompositeCursor(t *testing.T) {
	keys := []string{"tenant", "id"}
	require.NoError(t, validateSnapshotChunkBounds(keys, nil, nil))
	require.NoError(t, validateSnapshotChunkBounds(keys, []string{"acme", "1"}, []string{"zeta", "100"}))
	require.Error(t, validateSnapshotChunkBounds(nil, nil, nil))
	require.Error(t, validateSnapshotChunkBounds(keys, []string{"1"}, nil))
	require.Error(t, validateSnapshotChunkBounds(keys, nil, []string{"100"}))
	require.Error(t, validateSnapshotChunkBounds(keys, []string{}, nil))
	require.Error(t, validateSnapshotChunkBounds(keys, nil, []string{}))
}

func TestSnapshotHeapChunkBoundsRequireOneCTID(t *testing.T) {
	require.NoError(t, validateSnapshotChunkCursor("heap", []string{"id"}, nil, nil))
	require.NoError(t, validateSnapshotChunkCursor("heap", []string{"id"}, []string{"(1,2)"}, []string{"(9,9)"}))
	require.Error(t, validateSnapshotChunkCursor("heap", []string{"id"}, []string{}, nil))
	require.Error(t, validateSnapshotChunkCursor("heap", []string{"id"}, []string{"(1,2)", "(1,3)"}, nil))
}

func TestValidateHeapCursorRelationRequiresOnePhysicalHeap(t *testing.T) {
	require.NoError(t, validateHeapCursorRelation("public.items", "r", false))
	require.ErrorContains(t, validateHeapCursorRelation("public.items", "p", true), "partitioned")
	require.ErrorContains(t, validateHeapCursorRelation("public.items", "r", true), "inheritance")
}

func TestValidateChunkedKeys(t *testing.T) {
	newTable := func() source.SourceTableInfo {
		return source.SourceTableInfo{
			Name:        "public.items",
			PrimaryKeys: []string{"tenant", "id"},
			Schema: addCDCColumns(&schema.TableSchema{Columns: []schema.Column{
				{Name: "tenant", DataType: schema.TypeInt64},
				{Name: "id", DataType: schema.TypeUUID},
				{Name: "payload", DataType: schema.TypeString, Nullable: true},
			}}),
		}
	}
	t.Run("composite non-null keys", func(t *testing.T) {
		require.NoError(t, validateChunkedKeys(newTable()))
	})
	t.Run("nullable key", func(t *testing.T) {
		table := newTable()
		table.Schema.Columns[1].Nullable = true
		require.ErrorContains(t, validateChunkedKeys(table), "non-null unique keys")
	})
	t.Run("missing key", func(t *testing.T) {
		table := newTable()
		table.PrimaryKeys = []string{"missing"}
		require.ErrorContains(t, validateChunkedKeys(table), "non-null unique keys")
	})
	t.Run("duplicate key", func(t *testing.T) {
		table := newTable()
		table.PrimaryKeys = []string{"id", "id"}
		require.ErrorContains(t, validateChunkedKeys(table), "duplicate key")
	})
	t.Run("no schema", func(t *testing.T) {
		table := newTable()
		table.Schema = nil
		require.ErrorContains(t, validateChunkedKeys(table), "non-null unique keys")
	})
}

func TestChunkedTableRequiresFullIdentityForToastValues(t *testing.T) {
	for _, typ := range []schema.DataType{schema.TypeString, schema.TypeBinary, schema.TypeJSON, schema.TypeArray, schema.TypeDecimal, schema.TypeUnknown} {
		t.Run(typ.String(), func(t *testing.T) {
			tableSchema := addCDCColumns(&schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "value", DataType: typ},
			}})
			require.True(t, chunkedTableNeedsFullIdentity(tableSchema))
		})
	}
	for _, typ := range []schema.DataType{schema.TypeInt64, schema.TypeFloat64, schema.TypeBoolean, schema.TypeDate, schema.TypeTime, schema.TypeTimestamp, schema.TypeTimestampTZ, schema.TypeInterval, schema.TypeUUID} {
		t.Run(typ.String(), func(t *testing.T) {
			tableSchema := addCDCColumns(&schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: typ}}})
			require.False(t, chunkedTableNeedsFullIdentity(tableSchema), "CDC text metadata must not require FULL identity")
		})
	}
}

func TestReadSnapshotChunkRejectsUnboundedReadBeforeConnecting(t *testing.T) {
	src := &PostgresCDCSource{}
	for _, tc := range []struct {
		name     string
		size     int
		maxBytes int64
		timeout  time.Duration
	}{
		{name: "zero rows", size: 0, timeout: time.Second},
		{name: "negative rows", size: -1, timeout: time.Second},
		{name: "negative bytes", size: 1, maxBytes: -1, timeout: time.Second},
		{name: "no timeout", size: 1},
		{name: "negative timeout", size: 1, timeout: -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunk, err := src.readSnapshotChunk(context.Background(), source.SourceTableInfo{}, "", "key", nil, nil, tc.size, tc.maxBytes, tc.timeout)
			require.ErrorContains(t, err, "must be positive")
			require.Nil(t, chunk.Batch)
		})
	}
}

func TestReadSnapshotHeapChunkRequiresPostgreSQL14BeforeConnecting(t *testing.T) {
	src := &PostgresCDCSource{serverVersion: 130000}
	chunk, err := src.readSnapshotChunk(context.Background(), source.SourceTableInfo{}, "", "heap", nil, nil, 1, 0, time.Second)
	require.ErrorContains(t, err, "PostgreSQL 14 or newer")
	require.ErrorContains(t, err, "130000")
	require.Nil(t, chunk.Batch)
}
