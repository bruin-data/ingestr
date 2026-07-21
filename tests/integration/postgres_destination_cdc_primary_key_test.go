//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/schema"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

func TestPostgresDestinationPrepareRequiresMatchingCDCMergePrimaryKey(t *testing.T) {
	if pgDest.uri == "" {
		t.Skip("shared postgres destination container not available")
	}

	ctx := t.Context()
	schemaName := uniqueSchemaName(t, "cdc_pk")
	ensurePostgresSchema(t, ctx, pgDest.uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, context.Background(), pgDest.uri, schemaName) })

	dest := postgresdest.NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, pgDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "part", DataType: schema.TypeString},
		{Name: "payload", DataType: schema.TypeString, Nullable: true},
	}}
	prepare := func(table string, keys []string, requireMatch bool) error {
		return dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:                  schemaName + "." + table,
			Schema:                 tableSchema,
			PrimaryKeys:            keys,
			CDCMode:                requireMatch,
			CDCKeys:                keys,
			RequirePrimaryKeyMatch: requireMatch,
		})
	}
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}

	t.Run("fresh table", func(t *testing.T) {
		require.NoError(t, prepare("fresh", []string{"id", "part"}, true))
		require.Equal(t, []string{"id", "part"}, readPostgresPrimaryKey(t, ctx, db, schemaName, "fresh"))
	})

	t.Run("matching composite key in different order", func(t *testing.T) {
		exec(t, fmt.Sprintf(`CREATE TABLE %s (id bigint, part text, payload text, PRIMARY KEY (part, id))`, pqTable(schemaName, "matching")))
		require.NoError(t, prepare("matching", []string{"id", "part"}, true))
		require.Equal(t, []string{"part", "id"}, readPostgresPrimaryKey(t, ctx, db, schemaName, "matching"))
	})

	t.Run("missing key is added", func(t *testing.T) {
		exec(t, fmt.Sprintf(`CREATE TABLE %s (id bigint, part text, payload text)`, pqTable(schemaName, "missing")))
		exec(t, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'a', 'keep')`, pqTable(schemaName, "missing")))

		require.NoError(t, prepare("missing", []string{"id"}, true))
		require.Equal(t, []string{"id"}, readPostgresPrimaryKey(t, ctx, db, schemaName, "missing"))
		var payload string
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload FROM %s WHERE id = 1`, pqTable(schemaName, "missing"))).Scan(&payload))
		require.Equal(t, "keep", payload)
	})

	t.Run("different key is rejected", func(t *testing.T) {
		exec(t, fmt.Sprintf(`CREATE TABLE %s (id bigint, part text PRIMARY KEY, payload text)`, pqTable(schemaName, "mismatched")))

		err := prepare("mismatched", []string{"id"}, true)
		require.ErrorContains(t, err, "must have primary key [id]; found [part]")
		require.Equal(t, []string{"part"}, readPostgresPrimaryKey(t, ctx, db, schemaName, "mismatched"))
	})

	t.Run("quoted key case is significant", func(t *testing.T) {
		exec(t, fmt.Sprintf(`CREATE TABLE %s ("ID" bigint PRIMARY KEY, part text, payload text)`, pqTable(schemaName, "case_sensitive")))

		err := prepare("case_sensitive", []string{"id"}, true)
		require.ErrorContains(t, err, "must have primary key [id]; found [ID]")
	})

	t.Run("ordinary merge preserves existing behavior", func(t *testing.T) {
		exec(t, fmt.Sprintf(`CREATE TABLE %s (id bigint, part text PRIMARY KEY, payload text)`, pqTable(schemaName, "ordinary_merge")))
		require.NoError(t, prepare("ordinary_merge", []string{"id"}, false))
		require.Equal(t, []string{"part"}, readPostgresPrimaryKey(t, ctx, db, schemaName, "ordinary_merge"))
	})

	t.Run("concurrent missing key repair", func(t *testing.T) {
		const tableName = "concurrent_missing"
		table := pqTable(schemaName, tableName)
		exec(t, fmt.Sprintf(`CREATE TABLE %s (id bigint, part text, payload text)`, table))

		lockTx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = lockTx.Rollback() }()
		_, err = lockTx.ExecContext(ctx, fmt.Sprintf(`LOCK TABLE %s IN ACCESS SHARE MODE`, table))
		require.NoError(t, err)

		destinations := []*postgresdest.PostgresDestination{postgresdest.NewPostgresDestination(), postgresdest.NewPostgresDestination()}
		for _, concurrentDest := range destinations {
			require.NoError(t, concurrentDest.Connect(ctx, pgDest.uri))
			t.Cleanup(func() { _ = concurrentDest.Close(context.Background()) })
		}

		start := make(chan struct{})
		results := make(chan error, len(destinations))
		for _, concurrentDest := range destinations {
			go func() {
				<-start
				results <- concurrentDest.PrepareTable(ctx, destination.PrepareOptions{
					Table:                  schemaName + "." + tableName,
					Schema:                 tableSchema,
					PrimaryKeys:            []string{"id"},
					CDCMode:                true,
					CDCKeys:                []string{"id"},
					RequirePrimaryKeyMatch: true,
				})
			}()
		}
		close(start)

		queued, waitErr := waitForPostgresPendingTableLocks(ctx, db, schemaName, tableName, "AccessExclusiveLock", len(destinations), 5*time.Second)
		require.NoError(t, lockTx.Commit())
		require.NoError(t, waitErr)
		require.Equal(t, len(destinations), queued)
		for range destinations {
			require.NoError(t, <-results)
		}
		require.Equal(t, []string{"id"}, readPostgresPrimaryKey(t, ctx, db, schemaName, tableName))
	})
}

func waitForPostgresPendingTableLocks(
	ctx context.Context,
	db *sql.DB,
	schemaName, tableName, mode string,
	want int,
	timeout time.Duration,
) (int, error) {
	deadline := time.Now().Add(timeout)
	qualifiedTable := pqTable(schemaName, tableName)
	for {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT count(*)
			FROM pg_catalog.pg_locks
			WHERE relation = to_regclass($1)
			  AND mode = $2
			  AND NOT granted
		`, qualifiedTable, mode).Scan(&count)
		if err != nil || count >= want || time.Now().After(deadline) {
			return count, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readPostgresPrimaryKey(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints AS tc
		JOIN information_schema.key_column_usage AS kcu
		  ON kcu.constraint_catalog = tc.constraint_catalog
		 AND kcu.constraint_schema = tc.constraint_schema
		 AND kcu.constraint_name = tc.constraint_name
		 AND kcu.table_catalog = tc.table_catalog
		 AND kcu.table_schema = tc.table_schema
		 AND kcu.table_name = tc.table_name
		WHERE tc.table_schema = $1
		  AND tc.table_name = $2
		  AND tc.constraint_type = 'PRIMARY KEY'
		ORDER BY kcu.ordinal_position
	`, schemaName, tableName)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var key string
		require.NoError(t, rows.Scan(&key))
		keys = append(keys, key)
	}
	require.NoError(t, rows.Err())
	return keys
}
