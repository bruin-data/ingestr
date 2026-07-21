package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

func TestManagedStagingSchemaUsesLegacyCompatibleFilename(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "events.db")
	legacyPath := filepath.Join(filepath.Dir(targetPath), "events__bruin_staging.db")
	legacyDB, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyDB.ExecContext(t.Context(), `CREATE TABLE cdc_state (connector_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}

	d := NewSQLiteDestination()
	if err := d.Connect(t.Context(), "sqlite://"+targetPath); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()
	if err := d.ensureSchemaAttached(t.Context(), "_bruin_staging"); err != nil {
		t.Fatal(err)
	}

	if got := d.attachedSchemas["_bruin_staging"]; got != legacyPath {
		t.Fatalf("attached path = %q, want legacy-compatible %q", got, legacyPath)
	}
	var count int
	if err := d.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM "_bruin_staging".cdc_state`).Scan(&count); err != nil {
		t.Fatalf("legacy managed state table is not visible: %v", err)
	}
}

func TestCDCTargetIncarnationStableAcrossDMLAndChangesOnRecreate(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "incarnation.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()
	if err := d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
		t.Fatal(err)
	}

	first, exists, err := d.EnsureCDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists {
		t.Fatalf("initial incarnation = %q, exists=%v, err=%v", first, exists, err)
	}
	if err := d.Exec(t.Context(), `INSERT INTO events VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	stable, exists, err := d.CDCTargetIncarnation(t.Context(), "main.events")
	if err != nil || !exists || stable != first {
		t.Fatalf("DML changed incarnation: first=%q current=%q exists=%v err=%v", first, stable, exists, err)
	}
	if err := d.TruncateTable(t.Context(), "events"); err != nil {
		t.Fatal(err)
	}
	stable, exists, err = d.CDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists || stable != first {
		t.Fatalf("source truncate changed incarnation: first=%q current=%q exists=%v err=%v", first, stable, exists, err)
	}
	if err := d.Exec(t.Context(), `CREATE TABLE unrelated (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	stable, exists, err = d.CDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists || stable != first {
		t.Fatalf("unrelated DDL changed incarnation: first=%q current=%q exists=%v err=%v", first, stable, exists, err)
	}
	if err := d.Exec(t.Context(), `DROP TABLE events`); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := d.CDCTargetIncarnation(t.Context(), "events"); err != nil || exists {
		t.Fatalf("dropped table exists=%v err=%v", exists, err)
	}
	if err := d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	recreated, exists, err := d.CDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists || recreated != "" {
		t.Fatalf("recreated incarnation=%q first=%q exists=%v err=%v", recreated, first, exists, err)
	}
	recreated, exists, err = d.EnsureCDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists || recreated == first {
		t.Fatalf("initialized recreated incarnation=%q first=%q exists=%v err=%v", recreated, first, exists, err)
	}
}

func TestCDCMergePreservesUnchangedPayloadAndAppliesExplicitNull(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "cdc-toast.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()

	if err := d.Exec(t.Context(), `CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		payload TEXT,
		note TEXT,
		_cdc_lsn TEXT,
		_cdc_deleted INTEGER,
		_cdc_synced_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if err := d.Exec(t.Context(), `CREATE TABLE items_staging (
		id INTEGER,
		payload TEXT,
		note TEXT,
		_cdc_lsn TEXT,
		_cdc_deleted INTEGER,
		_cdc_synced_at TEXT,
		_cdc_unchanged_cols TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	largePayload := strings.Repeat("payload-", 10_000)
	if err := d.Exec(t.Context(), `INSERT INTO items VALUES (?, ?, 'before', '00000000/00000001/0000000000000002', 0, '2026-01-01')`, 1, largePayload); err != nil {
		t.Fatal(err)
	}

	columns := []string{"id", "payload", "note", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}
	merge := func() {
		t.Helper()
		if err := d.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:  "items",
			StagingTable: "items_staging",
			PrimaryKeys:  []string{"id"},
			Columns:      columns,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := d.Exec(t.Context(), `INSERT INTO items_staging VALUES (1, NULL, 'after', '00000000/00000002/0000000000000002', 0, '2026-01-02', '["payload"]')`); err != nil {
		t.Fatal(err)
	}
	merge()
	var payload sql.NullString
	var note string
	if err := d.db.QueryRowContext(t.Context(), `SELECT payload, note FROM items WHERE id = 1`).Scan(&payload, &note); err != nil {
		t.Fatal(err)
	}
	if !payload.Valid || payload.String != largePayload || note != "after" {
		t.Fatalf("partial update = payload(valid=%v,len=%d), note=%q", payload.Valid, len(payload.String), note)
	}

	if err := d.Exec(t.Context(), `DELETE FROM items_staging`); err != nil {
		t.Fatal(err)
	}
	if err := d.Exec(t.Context(), `INSERT INTO items_staging VALUES (1, NULL, 'explicit-null', '00000000/00000003/0000000000000002', 0, '2026-01-03', '[]')`); err != nil {
		t.Fatal(err)
	}
	merge()
	if err := d.db.QueryRowContext(t.Context(), `SELECT payload, note FROM items WHERE id = 1`).Scan(&payload, &note); err != nil {
		t.Fatal(err)
	}
	if payload.Valid || note != "explicit-null" {
		t.Fatalf("explicit NULL update = payload(valid=%v,value=%q), note=%q", payload.Valid, payload.String, note)
	}
	if !d.SupportsCDCUnchangedCols() {
		t.Fatal("SQLite destination must advertise unchanged-column support")
	}
}

func TestCDCMergeWithoutUnchangedColsMarkerUpdatesNormally(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "cdc-without-unchanged-cols.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()

	for _, statement := range []string{
		`CREATE TABLE items (id INTEGER PRIMARY KEY, payload TEXT, _cdc_lsn TEXT, _cdc_deleted INTEGER, _cdc_synced_at TEXT)`,
		`CREATE TABLE items_staging (id INTEGER, payload TEXT, _cdc_lsn TEXT, _cdc_deleted INTEGER, _cdc_synced_at TEXT)`,
		`INSERT INTO items VALUES (1, 'before', '00000000/00000001/0000000000000002', 0, '2026-01-01')`,
		`INSERT INTO items_staging VALUES (1, 'after', '00000000/00000002/0000000000000002', 0, '2026-01-02')`,
	} {
		if err := d.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}

	err := d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:  "items",
		StagingTable: "items_staging",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload string
	if err := d.db.QueryRowContext(t.Context(), `SELECT payload FROM items WHERE id = 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != "after" {
		t.Fatalf("payload = %q, want after", payload)
	}
}

func TestCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "cdc-lsn-fence.db")
	requireNoError(t, d.Connect(t.Context(), "sqlite://"+path))
	t.Cleanup(func() { _ = d.Close(t.Context()) })

	for _, statement := range []string{
		`CREATE TABLE items (id INTEGER PRIMARY KEY, payload TEXT, _cdc_lsn TEXT, _cdc_deleted INTEGER, _cdc_synced_at TEXT)`,
		`CREATE TABLE items_staging (id INTEGER, payload TEXT, _cdc_lsn TEXT, _cdc_deleted INTEGER, _cdc_synced_at TEXT, _cdc_unchanged_cols TEXT)`,
		`INSERT INTO items VALUES
			(1, 'newer-active', '00000000/00000030/0000000000000002', 0, '2026-01-03'),
			(2, 'newer-deleted', '00000000/00000030/0000000000000002', 1, '2026-01-03'),
			(3, 'legacy', NULL, 0, '2026-01-01'),
			(6, 'same-active', '00000000/00000010/0000000000000002', 0, '2026-01-01'),
			(7, 'same-deleted', '00000000/00000010/0000000000000002', 1, '2026-01-01'),
			(8, 'tie-delete', '00000000/00000010/0000000000000002', 0, '2026-01-01'),
			(9, 'toast-newer', '00000000/00000030/0000000000000002', 0, '2026-01-03'),
			(10, 'older-row-image', '00000000/00000010/0000000000000002', 0, '2026-01-01')`,
		`INSERT INTO items_staging VALUES
			(1, 'stale-active', '00000000/00000020/0000000000000002', 0, '2026-01-02', '[]'),
			(1, NULL, '00000000/00000025/0000000000000002', 1, '2026-01-02', '[]'),
			(2, 'stale-resurrection', '00000000/00000020/0000000000000002', 0, '2026-01-02', '[]'),
			(3, 'first-cdc-update', '00000000/00000010/0000000000000002', 0, '2026-01-02', '[]'),
			(4, 'first-insert', '00000000/00000010/0000000000000002', 0, '2026-01-02', '[]'),
			(5, NULL, '00000000/00000010/0000000000000002', 1, '2026-01-02', '[]'),
			(6, 'same-replay', '00000000/00000010/0000000000000002', 0, '2026-01-02', '[]'),
			(7, 'same-resurrection', '00000000/00000010/0000000000000002', 0, '2026-01-02', '[]'),
			(8, NULL, '00000000/00000010/0000000000000002', 1, '2026-01-02', '[]'),
			(9, NULL, '00000000/00000020/0000000000000002', 0, '2026-01-02', '["payload"]'),
			(10, 'latest-row-image', '00000000/00000010/0000000000000002', 0, '2026-01-02 01:00:00', '[]'),
			(10, NULL, '00000000/00000010/0000000000000002', 1, '2026-01-02 02:00:00', '[]'),
			(11, 'insert-then-delete', '00000000/00000010/0000000000000002', 0, '2026-01-02 01:00:00', '[]'),
			(11, NULL, '00000000/00000010/0000000000000002', 1, '2026-01-02 02:00:00', '[]'),
			(12, NULL, '00000000/00000010/0000000000000002', 1, '2026-01-01', '[]'),
			(12, 'newer-active-image', '00000000/00000020/0000000000000002', 0, '2026-01-02', '[]'),
			(13, 'older-active-image', '00000000/00000010/0000000000000002', 0, '2026-01-01', '[]'),
			(13, NULL, '00000000/00000020/0000000000000002', 1, '2026-01-02', '[]')`,
	} {
		requireNoError(t, d.Exec(t.Context(), statement))
	}

	opts := destination.MergeOptions{
		TargetTable:  "items",
		StagingTable: "items_staging",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}
	requireNoError(t, d.MergeTable(t.Context(), opts))

	expected := map[int64]struct {
		payload string
		lsn     string
		deleted int
		synced  string
	}{
		1:  {"newer-active", "00000000/00000030/0000000000000002", 0, "2026-01-03"},
		2:  {"newer-deleted", "00000000/00000030/0000000000000002", 1, "2026-01-03"},
		3:  {"first-cdc-update", "00000000/00000010/0000000000000002", 0, "2026-01-02"},
		4:  {"first-insert", "00000000/00000010/0000000000000002", 0, "2026-01-02"},
		5:  {"<null>", "00000000/00000010/0000000000000002", 1, "2026-01-02"},
		6:  {"same-active", "00000000/00000010/0000000000000002", 0, "2026-01-01"},
		7:  {"same-deleted", "00000000/00000010/0000000000000002", 1, "2026-01-01"},
		8:  {"tie-delete", "00000000/00000010/0000000000000002", 1, "2026-01-02"},
		9:  {"toast-newer", "00000000/00000030/0000000000000002", 0, "2026-01-03"},
		10: {"latest-row-image", "00000000/00000010/0000000000000002", 1, "2026-01-02 02:00:00"},
		11: {"insert-then-delete", "00000000/00000010/0000000000000002", 1, "2026-01-02 02:00:00"},
		12: {"newer-active-image", "00000000/00000020/0000000000000002", 0, "2026-01-02"},
		13: {"older-active-image", "00000000/00000020/0000000000000002", 1, "2026-01-02"},
	}
	for id, want := range expected {
		var payload, lsn, synced string
		var deleted int
		requireNoError(t, d.db.QueryRowContext(t.Context(), `
			SELECT COALESCE(payload, '<null>'), COALESCE(_cdc_lsn, ''), _cdc_deleted, _cdc_synced_at
			FROM items WHERE id = ?
		`, id).Scan(&payload, &lsn, &deleted, &synced))
		if payload != want.payload || lsn != want.lsn || deleted != want.deleted || synced != want.synced {
			t.Fatalf("id %d = (%q, %q, %d, %q), want (%q, %q, %d, %q)", id, payload, lsn, deleted, synced, want.payload, want.lsn, want.deleted, want.synced)
		}
	}

	requireNoError(t, d.Exec(t.Context(), `CREATE TABLE cdc_replay_audit (target_id INTEGER)`))
	requireNoError(t, d.Exec(t.Context(), `CREATE TRIGGER cdc_replay_audit_trigger AFTER UPDATE ON items BEGIN INSERT INTO cdc_replay_audit VALUES (NEW.id); END`))
	requireNoError(t, d.MergeTable(t.Context(), opts))
	var replayUpdates int
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM cdc_replay_audit`).Scan(&replayUpdates))
	if replayUpdates != 0 {
		t.Fatalf("replay update count = %d, want 0", replayUpdates)
	}
	requireNoError(t, d.Exec(t.Context(), `DROP TRIGGER cdc_replay_audit_trigger`))

	requireNoError(t, d.Exec(t.Context(), `DELETE FROM items_staging`))
	requireNoError(t, d.Exec(t.Context(), `INSERT INTO items_staging VALUES
		(14, 'predicate-excluded-image', '00000000/00000030/0000000000000002', 0, '2026-01-03 01:00:00', '[]'),
		(14, NULL, '00000000/00000040/0000000000000002', 1, '2026-01-03 02:00:00', '[]')`))
	opts.IncrementalPredicate = "target.id > 100"
	requireNoError(t, d.MergeTable(t.Context(), opts))
	var predicatePayload, predicateLSN, predicateSynced string
	var predicateDeleted int
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at FROM items WHERE id = 14`).Scan(&predicatePayload, &predicateLSN, &predicateDeleted, &predicateSynced))
	if predicatePayload != "predicate-excluded-image" || predicateLSN != "00000000/00000040/0000000000000002" || predicateDeleted != 1 || predicateSynced != "2026-01-03 02:00:00" {
		t.Fatalf("predicate-excluded row = (%q, %q, %d, %q)", predicatePayload, predicateLSN, predicateDeleted, predicateSynced)
	}
	opts.IncrementalPredicate = ""

	requireNoError(t, d.Exec(t.Context(), `DELETE FROM items_staging`))
	requireNoError(t, d.Exec(t.Context(), `INSERT INTO items_staging VALUES (1, 'newest', '00000000/00000040/0000000000000002', 0, '2026-01-04', '[]')`))
	requireNoError(t, d.MergeTable(t.Context(), opts))
	assertSQLiteCDCRow(t, d.db, "newest", "00000000/00000040/0000000000000002", 0)

	requireNoError(t, d.Exec(t.Context(), `DELETE FROM items_staging`))
	requireNoError(t, d.Exec(t.Context(), `INSERT INTO items_staging VALUES (1, NULL, '00000000/00000040/0000000000000002', 1, '2026-01-04', '[]')`))
	requireNoError(t, d.MergeTable(t.Context(), opts))
	assertSQLiteCDCRow(t, d.db, "newest", "00000000/00000040/0000000000000002", 1)

	requireNoError(t, d.Exec(t.Context(), `DELETE FROM items_staging`))
	requireNoError(t, d.Exec(t.Context(), `INSERT INTO items_staging VALUES (1, 'stale-outside-predicate', '00000000/00000020/0000000000000002', 0, '2026-01-02', '[]')`))
	opts.IncrementalPredicate = "target.id > 100"
	requireNoError(t, d.MergeTable(t.Context(), opts))
	assertSQLiteCDCRow(t, d.db, "newest", "00000000/00000040/0000000000000002", 1)
	var count int
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM items WHERE id = 1`).Scan(&count))
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}
}

func TestCDCMergeInternalAliasesDoNotCollide(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "cdc-alias-collision.db")
	requireNoError(t, d.Connect(t.Context(), "sqlite://"+path))
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	collisionColumns := []string{
		"__BRUIN_DEDUP_RN",
		"__bruin_ACTIVE_RN",
		"__BRUIN_IMAGE_RN",
		"__INGESTR_HAS_EQUAL_LSN_DELETE",
		"__ingestr_LATEST_LSN",
		"__INGESTR_LATEST_DELETED",
		"__ingestr_LATEST_SYNCED_AT",
	}
	requireNoError(t, d.Exec(t.Context(), `
		CREATE TABLE target_aliases (
			id INTEGER PRIMARY KEY,
			payload TEXT,
			_cdc_lsn TEXT,
			_cdc_deleted INTEGER,
			_cdc_synced_at TEXT,
			__BRUIN_DEDUP_RN TEXT,
			__bruin_ACTIVE_RN TEXT,
			__BRUIN_IMAGE_RN TEXT,
			__INGESTR_HAS_EQUAL_LSN_DELETE TEXT,
			__ingestr_LATEST_LSN TEXT,
			__INGESTR_LATEST_DELETED TEXT,
			__ingestr_LATEST_SYNCED_AT TEXT
		);
		CREATE TABLE staging_aliases AS SELECT * FROM target_aliases WHERE false;
		INSERT INTO target_aliases VALUES
			(2, 'before-update', '00000000/00000005/0000000000000002', 0, '2026-01-01 00:00:00', 'old-dedup', 'old-active', 'old-image', 'old-equal-delete', 'old-lsn', 'old-deleted', 'old-synced');
		INSERT INTO staging_aliases VALUES
			(1, 'active-image', '00000000/00000010/0000000000000002', 0, '2026-01-01 01:00:00', 'user-dedup', 'user-active', 'user-image', 'user-equal-delete', 'user-lsn', 'user-deleted', 'user-synced'),
			(1, NULL, '00000000/00000020/0000000000000002', 1, '2026-01-01 02:00:00', NULL, NULL, NULL, NULL, NULL, NULL, NULL),
			(2, 'tied-active-image', '00000000/00000030/0000000000000002', 0, '2026-01-01 03:00:00', 'tied-dedup', 'tied-active', 'tied-image', 'tied-equal-delete', 'tied-lsn', 'tied-deleted', 'tied-synced'),
			(2, NULL, '00000000/00000030/0000000000000002', 1, '2026-01-01 03:01:00', NULL, NULL, NULL, NULL, NULL, NULL, NULL)
	`))

	columns := append([]string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}, collisionColumns...)
	requireNoError(t, d.MergeTable(t.Context(), destination.MergeOptions{
		StagingTable: "staging_aliases",
		TargetTable:  "target_aliases",
		PrimaryKeys:  []string{"id"},
		Columns:      columns,
	}))
	var payload, lsn, syncedAt string
	var deleted int
	values := make([]string, len(collisionColumns))
	requireNoError(t, d.db.QueryRowContext(t.Context(), `
		SELECT payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at,
			__bruin_dedup_rn, __bruin_active_rn, __bruin_image_rn, __ingestr_has_equal_lsn_delete,
			__ingestr_latest_lsn, __ingestr_latest_deleted, __ingestr_latest_synced_at
		FROM target_aliases WHERE id = 1
	`).Scan(&payload, &lsn, &deleted, &syncedAt, &values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6]))
	if payload != "active-image" || lsn != "00000000/00000020/0000000000000002" || deleted != 1 || syncedAt != "2026-01-01 02:00:00" {
		t.Fatalf("row = (%q, %q, %d, %q)", payload, lsn, deleted, syncedAt)
	}
	wantValues := []string{"user-dedup", "user-active", "user-image", "user-equal-delete", "user-lsn", "user-deleted", "user-synced"}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("collision values = %v, want %v", values, wantValues)
	}

	values = make([]string, len(collisionColumns))
	requireNoError(t, d.db.QueryRowContext(t.Context(), `
		SELECT payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at,
			__BRUIN_DEDUP_RN, __bruin_ACTIVE_RN, __BRUIN_IMAGE_RN, __INGESTR_HAS_EQUAL_LSN_DELETE,
			__ingestr_LATEST_LSN, __INGESTR_LATEST_DELETED, __ingestr_LATEST_SYNCED_AT
		FROM target_aliases WHERE id = 2
	`).Scan(&payload, &lsn, &deleted, &syncedAt, &values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6]))
	if payload != "tied-active-image" || lsn != "00000000/00000030/0000000000000002" || deleted != 1 || syncedAt != "2026-01-01 03:01:00" {
		t.Fatalf("tied row = (%q, %q, %d, %q)", payload, lsn, deleted, syncedAt)
	}
	wantValues = []string{"tied-dedup", "tied-active", "tied-image", "tied-equal-delete", "tied-lsn", "tied-deleted", "tied-synced"}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("tied collision values = %v, want %v", values, wantValues)
	}
}

func assertSQLiteCDCRow(t *testing.T, db *sql.DB, wantPayload, wantLSN string, wantDeleted int) {
	t.Helper()
	var payload, lsn string
	var deleted int
	requireNoError(t, db.QueryRowContext(t.Context(), `SELECT payload, _cdc_lsn, _cdc_deleted FROM items WHERE id = 1`).Scan(&payload, &lsn, &deleted))
	if payload != wantPayload || lsn != wantLSN || deleted != wantDeleted {
		t.Fatalf("row = (%q, %q, %d), want (%q, %q, %d)", payload, lsn, deleted, wantPayload, wantLSN, wantDeleted)
	}
}

func TestCDCMergeWithIncrementalPredicateInsertsBeforeUpdate(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "cdc-predicate-order.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()

	for _, statement := range []string{
		`CREATE TABLE items (id INTEGER PRIMARY KEY, payload TEXT, _cdc_lsn TEXT, _cdc_deleted INTEGER, _cdc_synced_at TEXT)`,
		`CREATE TABLE items_staging (id INTEGER, payload TEXT, _cdc_lsn TEXT, _cdc_deleted INTEGER, _cdc_synced_at TEXT)`,
		`INSERT INTO items VALUES (1, 'before', '00000000/00000001/0000000000000002', 0, '2026-01-01')`,
		`INSERT INTO items_staging VALUES (1, 'after', '00000000/00000002/0000000000000002', 0, '2026-01-02')`,
	} {
		if err := d.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}

	err := d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:          "items",
		StagingTable:         "items_staging",
		PrimaryKeys:          []string{"id"},
		Columns:              []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		IncrementalPredicate: "target.payload = 'before'",
	})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	var payload string
	if err := d.db.QueryRowContext(t.Context(), `SELECT COUNT(*), MAX(payload) FROM items WHERE id = 1`).Scan(&count, &payload); err != nil {
		t.Fatal(err)
	}
	if count != 1 || payload != "after" {
		t.Fatalf("row count = %d, payload = %q; want 1, after", count, payload)
	}
}

func TestBuildCDCUpdateSetSQLiteUsesExactMarkerNames(t *testing.T) {
	got := buildCDCUpdateSetSQLite([]string{"Foo", "foo"}, "target", "source", `source."_cdc_unchanged_cols"`)
	if !strings.Contains(got, "value = 'Foo' COLLATE BINARY") || !strings.Contains(got, "value = 'foo' COLLATE BINARY") {
		t.Fatalf("exact marker comparisons missing: %s", got)
	}
	if strings.Contains(got, "LOWER(") {
		t.Fatalf("case-folded marker membership found: %s", got)
	}
}

func TestSQLiteCDCMergePreservesRenamedUnchangedTOASTColumn(t *testing.T) {
	d := NewSQLiteDestination()
	requireNoError(t, d.Connect(t.Context(), "sqlite://"+filepath.Join(t.TempDir(), "renamed-toast.db")))
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	requireNoError(t, d.Exec(t.Context(), `CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		config_data TEXT,
		note TEXT,
		_cdc_lsn TEXT,
		_cdc_deleted INTEGER,
		_cdc_synced_at TEXT
	)`))
	requireNoError(t, d.Exec(t.Context(), `CREATE TABLE items_staging (
		id INTEGER,
		config_data TEXT,
		note TEXT,
		_cdc_lsn TEXT,
		_cdc_deleted INTEGER,
		_cdc_synced_at TEXT,
		_cdc_unchanged_cols TEXT
	)`))
	largeValue := strings.Repeat("toast-value-", 10_000)
	requireNoError(t, d.Exec(t.Context(), `INSERT INTO items VALUES (1, ?, 'before', '00000000/00000001/0000000000000002', 0, '2026-01-01')`, largeValue))

	input := renamedToastRecord(t)
	renamed, err := transformer.NewColumnRenamer(map[string]string{"configData": "config_data"}).Transform(input)
	requireNoError(t, err)
	input.Release()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: renamed}
	close(records)
	requireNoError(t, d.WriteParallel(t.Context(), records, destination.WriteOptions{Table: "items_staging"}))
	requireNoError(t, d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:  "items",
		StagingTable: "items_staging",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "config_data", "note", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}))

	var configData, note string
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT config_data, note FROM items WHERE id = 1`).Scan(&configData, &note))
	if configData != largeValue || note != "after" {
		t.Fatalf("renamed partial update = config length %d, note %q", len(configData), note)
	}
}

func renamedToastRecord(t *testing.T) arrow.RecordBatch {
	t.Helper()
	fields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "configData", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "note", Type: arrow.BinaryTypes.String},
		{Name: destination.CDCLSNColumn, Type: arrow.BinaryTypes.String},
		{Name: destination.CDCDeletedColumn, Type: arrow.FixedWidthTypes.Boolean},
		{Name: destination.CDCSyncedAtColumn, Type: arrow.BinaryTypes.String},
		{Name: destination.CDCUnchangedColsColumn, Type: arrow.BinaryTypes.String},
	}
	id := array.NewInt64Builder(memory.DefaultAllocator)
	id.Append(1)
	configData := array.NewStringBuilder(memory.DefaultAllocator)
	configData.AppendNull()
	note := array.NewStringBuilder(memory.DefaultAllocator)
	note.Append("after")
	lsn := array.NewStringBuilder(memory.DefaultAllocator)
	lsn.Append("00000000/00000002/0000000000000002")
	deleted := array.NewBooleanBuilder(memory.DefaultAllocator)
	deleted.Append(false)
	syncedAt := array.NewStringBuilder(memory.DefaultAllocator)
	syncedAt.Append("2026-01-02")
	unchanged := array.NewStringBuilder(memory.DefaultAllocator)
	unchanged.Append(`["configData"]`)
	builders := []array.Builder{id, configData, note, lsn, deleted, syncedAt, unchanged}
	columns := make([]arrow.Array, len(builders))
	for i, builder := range builders {
		columns[i] = builder.NewArray()
		builder.Release()
	}
	record := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, 1)
	for _, column := range columns {
		column.Release()
	}
	return record
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCDCTargetIncarnationDetectsUnobservedRecreateAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart-incarnation.db")
	open := func() *SQLiteDestination {
		d := NewSQLiteDestination()
		if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
			t.Fatal(err)
		}
		return d
	}

	firstDestination := open()
	if err := firstDestination.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	first, exists, err := firstDestination.EnsureCDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists {
		t.Fatalf("initial incarnation = %q, exists=%v, err=%v", first, exists, err)
	}
	if err := firstDestination.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	secondDestination := open()
	if err := secondDestination.Exec(t.Context(), `DROP TABLE events`); err != nil {
		t.Fatal(err)
	}
	if err := secondDestination.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := secondDestination.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	thirdDestination := open()
	defer func() { _ = thirdDestination.Close(t.Context()) }()
	recreated, exists, err := thirdDestination.CDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists || recreated != "" {
		t.Fatalf("recreated incarnation=%q first=%q exists=%v err=%v", recreated, first, exists, err)
	}
}

func TestCDCTargetIncarnationDetectsRenameAndReplacementWhileOldTableRemains(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "rename-replacement.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()
	if err := d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	first, exists, err := d.EnsureCDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists {
		t.Fatalf("initial incarnation = %q, exists=%v, err=%v", first, exists, err)
	}
	if err := d.Exec(t.Context(), `ALTER TABLE events RENAME TO events_old`); err != nil {
		t.Fatal(err)
	}
	if err := d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	recreated, exists, err := d.CDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists || recreated != "" {
		t.Fatalf("replacement incarnation=%q first=%q exists=%v err=%v", recreated, first, exists, err)
	}
}

func TestCDCTargetIncarnationProbeDoesNotMutateUnclaimedTable(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "readonly-incarnation.db")
	requireNoError(t, d.Connect(t.Context(), "sqlite://"+path))
	defer func() { _ = d.Close(t.Context()) }()
	requireNoError(t, d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`))

	var before string
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT group_concat(type || ':' || name || ':' || COALESCE(sql, ''), '|') FROM sqlite_schema`).Scan(&before))
	incarnation, exists, err := d.CDCTargetIncarnation(t.Context(), "events")
	requireNoError(t, err)
	if !exists || incarnation != "" {
		t.Fatalf("unclaimed probe incarnation=%q exists=%v", incarnation, exists)
	}
	var after string
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT group_concat(type || ':' || name || ':' || COALESCE(sql, ''), '|') FROM sqlite_schema`).Scan(&after))
	if after != before {
		t.Fatalf("read-only incarnation probe changed SQLite schema: before=%q after=%q", before, after)
	}
}

func TestConditionalCDCTruncateRejectsRecreatedSQLiteTargetWithoutMutation(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "conditional-truncate.db")
	requireNoError(t, d.Connect(t.Context(), "sqlite://"+path))
	defer func() { _ = d.Close(t.Context()) }()
	requireNoError(t, d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`))
	expected, exists, err := d.EnsureCDCTargetIncarnation(t.Context(), "events")
	requireNoError(t, err)
	if !exists || expected == "" {
		t.Fatalf("initialized incarnation=%q exists=%v", expected, exists)
	}
	requireNoError(t, d.Exec(t.Context(), `DROP TABLE events`))
	requireNoError(t, d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`))
	requireNoError(t, d.Exec(t.Context(), `INSERT INTO events VALUES (7)`))

	err = d.TruncateCDCTableIfIncarnation(t.Context(), "events", expected)
	if err == nil || !strings.Contains(err.Error(), "physical incarnation changed") {
		t.Fatalf("conditional truncate error=%v", err)
	}
	var count int
	requireNoError(t, d.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM events`).Scan(&count))
	if count != 1 {
		t.Fatalf("conditional truncate mutated recreated target: row count=%d", count)
	}
}

type releaseCountingBatch struct {
	arrow.RecordBatch
	releases int
}

func (b *releaseCountingBatch) Release() {
	b.releases++
	b.RecordBatch.Release()
}

func newReleaseCountingBatch(t *testing.T) *releaseCountingBatch {
	t.Helper()
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(1)
	values := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{values}, 1)
	values.Release()
	return &releaseCountingBatch{RecordBatch: record}
}

func TestWriteParallelReleasesBatchExactlyOnce(t *testing.T) {
	tests := []struct {
		name      string
		prepare   bool
		wantError string
	}{
		{name: "success", prepare: true},
		{name: "write error", wantError: "no such table"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewSQLiteDestination()
			path := filepath.Join(t.TempDir(), "release.db")
			if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = d.Close(t.Context()) }()
			if tt.prepare {
				if err := d.Exec(t.Context(), `CREATE TABLE events (id INTEGER)`); err != nil {
					t.Fatal(err)
				}
			}

			batch := newReleaseCountingBatch(t)
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: batch}
			close(records)
			err := d.WriteParallel(t.Context(), records, destination.WriteOptions{Table: "events"})
			if tt.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("WriteParallel() error = %v, want %q", err, tt.wantError)
			}
			if batch.releases != 1 {
				t.Fatalf("batch releases = %d, want exactly 1", batch.releases)
			}
		})
	}
}

func TestClaimCDCTargetIsAtomicIdempotentAndSchemaAware(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.db")
	destinations := []*SQLiteDestination{NewSQLiteDestination(), NewSQLiteDestination()}
	for _, d := range destinations {
		if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
			t.Fatal(err)
		}
		defer func(d *SQLiteDestination) { _ = d.Close(context.Background()) }(d)
	}

	claimSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "destination_table", DataType: schema.TypeString, Nullable: false},
		{Name: "connector_id", DataType: schema.TypeString, Nullable: false},
		{Name: "claimed_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}}
	for _, d := range destinations {
		if err := d.PrepareTable(t.Context(), destination.PrepareOptions{
			Table:       "_bruin_staging.cdc_targets",
			Schema:      claimSchema,
			PrimaryKeys: []string{"destination_table"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	errs := make([]error, len(destinations))
	var wg sync.WaitGroup
	for i, d := range destinations {
		wg.Add(1)
		go func(i int, d *SQLiteDestination) {
			defer wg.Done()
			<-start
			errs[i] = d.ClaimCDCTarget(context.Background(), "_bruin_staging.cdc_targets", destination.CDCTargetClaim{DestinationTable: "main.orders", ConnectorID: []string{"connector-a", "connector-b"}[i], SourceTable: "public.orders"})
		}(i, d)
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner != -1 {
				t.Fatalf("both connectors claimed one target: errors=%v", errs)
			}
			winner = i
		}
	}
	if winner == -1 {
		t.Fatalf("no connector claimed target: errors=%v", errs)
	}
	winnerID := []string{"connector-a", "connector-b"}[winner]
	loserID := []string{"connector-a", "connector-b"}[1-winner]
	claim := func(table, connector string) destination.CDCTargetClaim {
		return destination.CDCTargetClaim{DestinationTable: table, ConnectorID: connector, SourceTable: "public.orders"}
	}
	if err := destinations[winner].ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim("orders", winnerID)); err != nil {
		t.Fatalf("same connector could not reclaim target: %v", err)
	}
	if err := destinations[1-winner].ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim("orders", loserID)); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("conflicting claim error = %v", err)
	}
	if err := destinations[1-winner].ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim(`"main"."orders"`, loserID)); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("quoted alias claim error = %v", err)
	}
	if err := destinations[1-winner].ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim("archive.orders", loserID)); err != nil {
		t.Fatalf("distinct schema target was rejected: %v", err)
	}
}

func TestAttachedSchemaUsesSameFullDurabilityAsTarget(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "durability.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()
	if err := d.ensureSchemaAttached(t.Context(), "_bruin_staging"); err != nil {
		t.Fatal(err)
	}

	for _, database := range []string{"main", "_bruin_staging"} {
		var synchronous int
		if err := d.db.QueryRowContext(t.Context(), `PRAGMA "`+database+`".synchronous`).Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		if synchronous != 2 {
			t.Fatalf("%s synchronous = %d, want FULL (2)", database, synchronous)
		}
	}
}

func TestGetTableSchemaHonorsAttachedSchemaQualifier(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "schema.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()
	if err := d.ensureSchemaAttached(t.Context(), "_bruin_staging"); err != nil {
		t.Fatal(err)
	}
	if err := d.Exec(t.Context(), `CREATE TABLE main.events (main_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := d.Exec(t.Context(), `CREATE TABLE "_bruin_staging".events (staging_value TEXT)`); err != nil {
		t.Fatal(err)
	}

	got, err := d.GetTableSchema(t.Context(), "_bruin_staging.events")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Columns) != 1 || got.Columns[0].Name != "staging_value" {
		t.Fatalf("GetTableSchema() = %#v, want attached-schema table", got)
	}
}

func TestLoadCDCStateFenceReturnsLatestRunSentinels(t *testing.T) {
	d := NewSQLiteDestination()
	path := filepath.Join(t.TempDir(), "fence.db")
	if err := d.Connect(t.Context(), "sqlite://"+path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()

	if err := d.Exec(t.Context(), `CREATE TABLE "cdc_state" ("event_id" TEXT, "connector_id" TEXT, "state_kind" TEXT, "state_generation" INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		eventID    string
		connector  string
		kind       string
		generation int64
	}{
		{eventID: "old-run", connector: "connector-a", kind: "run", generation: 4},
		{eventID: "run-b", connector: "connector-a", kind: "run", generation: 5},
		{eventID: "snapshot", connector: "connector-a", kind: "snapshot", generation: 5},
		{eventID: "run-a", connector: "connector-a", kind: "run", generation: 5},
		{eventID: "run-a", connector: "connector-a", kind: "run", generation: 5},
		{eventID: "other", connector: "connector-b", kind: "run", generation: 9},
	} {
		if err := d.Exec(t.Context(), `INSERT INTO "cdc_state" VALUES (?, ?, ?, ?)`, row.eventID, row.connector, row.kind, row.generation); err != nil {
			t.Fatal(err)
		}
	}

	fence, err := d.LoadCDCStateFence(t.Context(), "cdc_state", "connector-a")
	if err != nil {
		t.Fatal(err)
	}
	if fence.Generation != 5 || !reflect.DeepEqual(fence.RunEventIDs, []string{"run-a", "run-b"}) {
		t.Fatalf("LoadCDCStateFence() = %#v", fence)
	}

	missing, err := d.LoadCDCStateFence(t.Context(), "missing_state", "connector-a")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Generation != 0 || len(missing.RunEventIDs) != 0 {
		t.Fatalf("missing fence = %#v", missing)
	}
}
