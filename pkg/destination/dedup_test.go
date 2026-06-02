package destination

import "testing"

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
		want := `SELECT "id", "name", "ts" FROM (SELECT "id", "name", "ts", ROW_NUMBER() OVER (PARTITION BY "id" ORDER BY "ts" DESC) AS __bruin_dedup_rn FROM "staging") AS _numbered WHERE __bruin_dedup_rn = 1`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("composite primary key", func(t *testing.T) {
		got := DedupStagingSelect(cols, `"a", "b"`, `"staging"`, `"ts"`)
		want := `SELECT "id", "name", "ts" FROM (SELECT "id", "name", "ts", ROW_NUMBER() OVER (PARTITION BY "a", "b" ORDER BY "ts" DESC) AS __bruin_dedup_rn FROM "staging") AS _numbered WHERE __bruin_dedup_rn = 1`
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
