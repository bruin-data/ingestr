package destination

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestDedupStagingSelectAliasCollision(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE staging (id INTEGER, __BRUIN_DEDUP_RN INTEGER, __bruin_dedup_rn_1 INTEGER);
		INSERT INTO staging VALUES (7, 40, 3), (7, 80, 9)`)
	require.NoError(t, err)
	query := DedupStagingSelect(`"id", "__BRUIN_DEDUP_RN", "__bruin_dedup_rn_1"`, `"id"`, `"staging"`, `"__bruin_dedup_rn_1"`)
	rows, err := db.Query(query)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next(), "must not filter on the user column")
	var id, value, version int
	require.NoError(t, rows.Scan(&id, &value, &version))
	require.Equal(t, 7, id)
	require.Equal(t, 80, value)
	require.Equal(t, 9, version)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestDedupStagingSelect(t *testing.T) {
	cols := `"id", "name", "ts"`

	t.Run("no primary keys returns plain select", func(t *testing.T) {
		got := DedupStagingSelect(cols, "", `"staging"`, `"ts"`)
		want := `SELECT "id", "name", "ts" FROM "staging"`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("orders by incremental key DESC so latest wins", func(t *testing.T) {
		got := DedupStagingSelect(cols, `"id"`, `"staging"`, `"ts"`)
		want := `SELECT "id", "name", "ts" FROM (SELECT "id", "name", "ts", ROW_NUMBER() OVER (PARTITION BY "id" ORDER BY CASE WHEN "ts" IS NULL THEN 1 ELSE 0 END ASC, "ts" DESC) AS __bruin_dedup_rn FROM "staging") AS _numbered WHERE __bruin_dedup_rn = 1`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("composite primary key", func(t *testing.T) {
		got := DedupStagingSelect(cols, `"a", "b"`, `"staging"`, `"ts"`)
		want := `SELECT "id", "name", "ts" FROM (SELECT "id", "name", "ts", ROW_NUMBER() OVER (PARTITION BY "a", "b" ORDER BY CASE WHEN "ts" IS NULL THEN 1 ELSE 0 END ASC, "ts" DESC) AS __bruin_dedup_rn FROM "staging") AS _numbered WHERE __bruin_dedup_rn = 1`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("empty order column falls back to no-op order", func(t *testing.T) {
		got := DedupStagingSelect(cols, `"id"`, `"staging"`, "")
		want := `SELECT "id", "name", "ts" FROM (SELECT "id", "name", "ts", ROW_NUMBER() OVER (PARTITION BY "id" ORDER BY (SELECT NULL)) AS __bruin_dedup_rn FROM "staging") AS _numbered WHERE __bruin_dedup_rn = 1`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
