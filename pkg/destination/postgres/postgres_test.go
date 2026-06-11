package postgres

import "testing"

func TestBuildDeleteInsertLockSQL(t *testing.T) {
	got := buildDeleteInsertLockSQL(`"public"."orders"`)
	want := `LOCK TABLE "public"."orders" IN EXCLUSIVE MODE`
	if got != want {
		t.Fatalf("buildDeleteInsertLockSQL() = %q, want %q", got, want)
	}
}
