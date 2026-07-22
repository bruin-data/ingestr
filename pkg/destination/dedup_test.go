package destination

import (
	"strings"
	"testing"
)

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

func TestDedupStagingSelectWithCollisionSafeAlias(t *testing.T) {
	alias := UniqueInternalColumnName([]string{"id", "__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2"}, "__bruin_dedup_rn")
	if alias != "__bruin_dedup_rn_3" {
		t.Fatalf("alias = %q, want __bruin_dedup_rn_3", alias)
	}
	quotedAlias := `"` + alias + `"`
	got := DedupStagingSelectWithRowNumberAlias(`"id", "__BRUIN_DEDUP_RN"`, `"id"`, `"staging"`, "", quotedAlias)
	if !strings.Contains(got, `AS "__bruin_dedup_rn_3"`) || !strings.Contains(got, `WHERE "__bruin_dedup_rn_3" = 1`) {
		t.Fatalf("dedup SQL does not use collision-safe alias: %s", got)
	}
}
