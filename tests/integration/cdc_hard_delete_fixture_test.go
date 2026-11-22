//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

type cdcHardDeleteChange struct {
	id        int64
	payload   string
	lsn       string
	deleted   bool
	synced    string
	unchanged []string
}

type cdcHardDeleteRow struct {
	payload string
	lsn     string
	deleted bool
}

// cdcHardDeleteDialect renders fixture values as SQL literals. Empty payload
// and LSN strings render as NULL.
type cdcHardDeleteDialect struct {
	boolean   func(bool) string
	timestamp func(string) string
	unchanged func([]string) string
}

func (d cdcHardDeleteDialect) values(rows []cdcHardDeleteChange, staging bool) string {
	nullable := func(v string) string {
		if v == "" {
			return "NULL"
		}
		return "'" + v + "'"
	}
	tuples := make([]string, len(rows))
	for i, r := range rows {
		parts := []string{fmt.Sprint(r.id), nullable(r.payload), nullable(r.lsn), d.boolean(r.deleted), d.timestamp(r.synced)}
		if staging {
			parts = append(parts, d.unchanged(r.unchanged))
		}
		tuples[i] = "(" + strings.Join(parts, ", ") + ")"
	}
	return strings.Join(tuples, ", ")
}

// cdcHardDeleteHarness drives one destination through the shared hard-delete
// scenario. Target and staging tables must already exist with columns
// (id, payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at[, _cdc_unchanged_cols]).
type cdcHardDeleteHarness struct {
	dest         destination.Destination
	targetTable  string
	stagingTable string
	dialect      cdcHardDeleteDialect
	// insert appends rendered VALUES tuples to a table.
	insert func(t *testing.T, table, values string)
	// truncate empties a table.
	truncate func(t *testing.T, table string)
	readRows func(t *testing.T) map[int64]cdcHardDeleteRow
	// predicate is an incremental predicate excluding id 13, or empty when the
	// destination does not support incremental predicates for CDC merges.
	predicate string
}

var cdcHardDeleteTargetRows = []cdcHardDeleteChange{
	{1, "newer-active", "00000000/00000030", false, "2026-01-03", nil},
	{2, "newer-deleted", "00000000/00000030", true, "2026-01-03", nil},
	{3, "legacy", "", false, "2026-01-01", nil},
	{6, "same-active", "00000000/00000010", false, "2026-01-01", nil},
	{7, "same-deleted", "00000000/00000010", true, "2026-01-01", nil},
	{8, "tie-delete", "00000000/00000010", false, "2026-01-01", nil},
	{9, "toast-newer", "00000000/00000030", false, "2026-01-03", nil},
	{10, "older-row-image", "00000000/00000010", false, "2026-01-01", nil},
	{12, "legacy-delete", "", false, "2026-01-01", nil},
	{13, "untouched", "00000000/00000005", false, "2026-01-01", nil},
}

var cdcHardDeleteStagingRows = []cdcHardDeleteChange{
	{1, "stale-active", "00000000/00000020", false, "2026-01-02", nil},
	{1, "", "00000000/00000025", true, "2026-01-02", nil},
	{2, "stale-resurrection", "00000000/00000020", false, "2026-01-02", nil},
	{3, "first-cdc-update", "00000000/00000010", false, "2026-01-02", nil},
	{4, "first-insert", "00000000/00000010", false, "2026-01-02", nil},
	{5, "insert-then-delete", "00000000/00000010", false, "2026-01-02", nil},
	{5, "", "00000000/00000010", true, "2026-01-02", nil},
	{6, "same-replay", "00000000/00000010", false, "2026-01-02", nil},
	{7, "same-resurrection", "00000000/00000010", false, "2026-01-02", nil},
	{8, "", "00000000/00000010", true, "2026-01-02", nil},
	{9, "", "00000000/00000020", false, "2026-01-02", []string{"payload"}},
	{10, "latest-row-image", "00000000/00000010", false, "2026-01-02", nil},
	{10, "", "00000000/00000010", true, "2026-01-02", nil},
	{11, "", "00000000/00000010", true, "2026-01-02", nil},
	{12, "", "00000000/00000010", true, "2026-01-02", nil},
	{14, "deleted-then-reinserted", "00000000/00000012", false, "2026-01-02", nil},
	{14, "", "00000000/00000011", true, "2026-01-02", nil},
}

func runCDCHardDeleteScenario(t *testing.T, ctx context.Context, h cdcHardDeleteHarness) {
	t.Helper()
	h.insert(t, h.targetTable, h.dialect.values(cdcHardDeleteTargetRows, false))
	h.insert(t, h.stagingTable, h.dialect.values(cdcHardDeleteStagingRows, true))

	opts := destination.MergeOptions{
		TargetTable:   h.targetTable,
		StagingTable:  h.stagingTable,
		PrimaryKeys:   []string{"id"},
		Columns:       []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
		CDCHardDelete: true,
	}
	require.NoError(t, h.dest.MergeTable(ctx, opts))

	// Tombstones written by earlier soft-delete runs (2, 7) stay put unless a
	// newer change arrives; stale changes never resurrect or delete rows.
	expected := map[int64]cdcHardDeleteRow{
		1:  {"newer-active", "00000000/00000030", false},
		2:  {"newer-deleted", "00000000/00000030", true},
		3:  {"first-cdc-update", "00000000/00000010", false},
		4:  {"first-insert", "00000000/00000010", false},
		6:  {"same-active", "00000000/00000010", false},
		7:  {"same-deleted", "00000000/00000010", true},
		9:  {"toast-newer", "00000000/00000030", false},
		13: {"untouched", "00000000/00000005", false},
		14: {"deleted-then-reinserted", "00000000/00000012", false},
	}
	require.Equal(t, expected, h.readRows(t))

	require.NoError(t, h.dest.MergeTable(ctx, opts))
	require.Equal(t, expected, h.readRows(t), "replaying the same changes must be idempotent")

	h.truncate(t, h.stagingTable)
	h.insert(t, h.stagingTable, h.dialect.values([]cdcHardDeleteChange{
		{1, "", "00000000/00000040", true, "2026-01-04", nil},
		{2, "reinserted", "00000000/00000040", false, "2026-01-04", nil},
	}, true))
	require.NoError(t, h.dest.MergeTable(ctx, opts))
	got := h.readRows(t)
	require.NotContains(t, got, int64(1))
	require.Equal(t, cdcHardDeleteRow{"reinserted", "00000000/00000040", false}, got[2])

	if h.predicate == "" {
		return
	}
	h.truncate(t, h.stagingTable)
	h.insert(t, h.stagingTable, h.dialect.values([]cdcHardDeleteChange{
		{13, "", "00000000/00000050", true, "2026-01-05", nil},
	}, true))
	opts.IncrementalPredicate = h.predicate
	require.NoError(t, h.dest.MergeTable(ctx, opts))
	require.Contains(t, h.readRows(t), int64(13), "rows outside the incremental predicate must not be deleted")
}

func readCDCHardDeleteRows(t *testing.T, db *sql.DB, query string) map[int64]cdcHardDeleteRow {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	got := map[int64]cdcHardDeleteRow{}
	for rows.Next() {
		var id int64
		var r cdcHardDeleteRow
		require.NoError(t, rows.Scan(&id, &r.payload, &r.lsn, &r.deleted))
		got[id] = r
	}
	require.NoError(t, rows.Err())
	return got
}
