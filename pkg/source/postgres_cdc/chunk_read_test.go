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
			chunk, err := src.readSnapshotChunk(context.Background(), source.SourceTableInfo{}, "", nil, nil, tc.size, tc.maxBytes, tc.timeout)
			require.ErrorContains(t, err, "must be positive")
			require.Nil(t, chunk.Batch)
		})
	}
}
