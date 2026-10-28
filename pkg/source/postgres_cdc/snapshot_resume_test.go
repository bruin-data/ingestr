package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestKeyedSnapshotQuery(t *testing.T) {
	columns := []string{`"tenant"`, `"id"`, `"payload"`}
	query, args := keyedSnapshotQuery(columns, `"public"."orders"`, []string{"tenant", "id"}, []string{"text", "bigint"}, nil)
	require.Equal(t, `SELECT "tenant", "id", "payload", __ingestr_snapshot."tenant"::text AS "__ingestr_key_0", __ingestr_snapshot."id"::text AS "__ingestr_key_1" FROM "public"."orders" AS __ingestr_snapshot ORDER BY __ingestr_snapshot."tenant", __ingestr_snapshot."id"`, query)
	require.Empty(t, args)

	query, args = keyedSnapshotQuery(columns, `"public"."orders"`, []string{"tenant", "id"}, []string{"text", "bigint"}, []string{"acme", "41"})
	require.Equal(t, `SELECT "tenant", "id", "payload", __ingestr_snapshot."tenant"::text AS "__ingestr_key_0", __ingestr_snapshot."id"::text AS "__ingestr_key_1" FROM "public"."orders" AS __ingestr_snapshot WHERE (__ingestr_snapshot."tenant", __ingestr_snapshot."id") > ($1::text::text, $2::text::bigint) ORDER BY __ingestr_snapshot."tenant", __ingestr_snapshot."id"`, query)
	require.Equal(t, []any{"acme", "41"}, args)
}

func TestResumableSnapshotKeys(t *testing.T) {
	table := source.SourceTableInfo{
		PrimaryKeys: []string{"id"},
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "payload", DataType: schema.TypeString, Nullable: true},
		}},
	}
	require.Equal(t, []string{"id"}, resumableSnapshotKeys(table))

	table.PrimaryKeys = []string{"payload"}
	require.Nil(t, resumableSnapshotKeys(table), "nullable keys cannot be ordered and compared reliably")

	table.PrimaryKeys = nil
	require.Nil(t, resumableSnapshotKeys(table))
}
