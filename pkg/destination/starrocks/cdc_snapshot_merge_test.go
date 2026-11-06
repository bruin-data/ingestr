package starrocks

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestSnapshotCDCMergePreservesNewerRowsAndTombstones(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE target (id INTEGER PRIMARY KEY, payload TEXT, _cdc_lsn TEXT, _cdc_deleted BOOLEAN);
		CREATE TABLE staging (id INTEGER, payload TEXT, _cdc_lsn TEXT, _cdc_deleted BOOLEAN);
		INSERT INTO target VALUES
		(0, 'outside-lower', '30', FALSE),
		(1, 'newer', '30', FALSE),
		(2, 'same', '20', FALSE),
		(3, 'same-tombstone', '20', TRUE),
		(4, 'old', '10', FALSE),
		(6, 'unstamped', NULL, NULL),
		(7, 'newer-tombstone', '30', TRUE),
		(8, 'outside-upper', '30', FALSE);
		INSERT INTO staging VALUES
		(1, 'snapshot', '20', FALSE),
		(2, 'snapshot', '20', FALSE),
		(3, 'snapshot', '20', FALSE),
		(4, NULL, '20', FALSE),
		(5, 'older-snapshot', '10', FALSE),
		(5, 'new-row', '20', FALSE),
		(6, 'snapshot', '20', FALSE),
		(7, 'snapshot', '20', FALSE)`)
	require.NoError(t, err)
	d := &StarRocksDestination{}
	opts := snapshotMergeOptions()
	opts.CDCSnapshotRange = &destination.CDCSnapshotRange{After: []string{"0"}, Last: []string{"7"}}
	query := d.buildCDCMergeSQL(opts)
	require.Equal(t, 1, strings.Count(query, "ROW_NUMBER()"))
	require.NotContains(t, query, "history AS")
	require.Contains(t, query, "FROM `target` WHERE (`id` > 0) AND (`id` <= 7)")

	for range 2 {
		_, err = db.Exec("INSERT OR REPLACE INTO target " + query)
		require.NoError(t, err)
		rows, err := db.Query("SELECT id, COALESCE(payload, '<null>'), COALESCE(_cdc_lsn, ''), _cdc_deleted FROM target ORDER BY id")
		require.NoError(t, err)
		type row struct {
			ID      int
			Payload string
			LSN     string
			Deleted bool
		}
		var got []row
		for rows.Next() {
			var r row
			require.NoError(t, rows.Scan(&r.ID, &r.Payload, &r.LSN, &r.Deleted))
			got = append(got, r)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Equal(t, []row{
			{0, "outside-lower", "30", false},
			{1, "newer", "30", false},
			{2, "same", "20", false},
			{3, "same-tombstone", "20", true},
			{4, "<null>", "20", false},
			{5, "new-row", "20", false},
			{6, "snapshot", "20", false},
			{7, "newer-tombstone", "30", true},
			{8, "outside-upper", "30", false},
		}, got)
	}
}

func TestSnapshotCDCMergeCompositeRangeUsesLexicographicOrder(t *testing.T) {
	opts := snapshotMergeOptions()
	opts.PrimaryKeys = []string{"tenant", "id"}
	opts.Schema.Columns = append(opts.Schema.Columns, schema.Column{Name: "tenant", DataType: schema.TypeInt32})
	opts.CDCSnapshotRange = &destination.CDCSnapshotRange{After: []string{"1", "9"}, Last: []string{"2", "0"}}
	predicate := snapshotKeyRange(opts)
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE target (tenant INTEGER, id INTEGER);
		INSERT INTO target VALUES (1, 8), (1, 9), (1, 10), (2, -10), (2, 0), (2, 1), (3, -20)`)
	require.NoError(t, err)
	rows, err := db.Query("SELECT tenant, id FROM target WHERE " + predicate + " ORDER BY tenant, id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got [][2]int
	for rows.Next() {
		var row [2]int
		require.NoError(t, rows.Scan(&row[0], &row[1]))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, [][2]int{{1, 10}, {2, -10}, {2, 0}}, got)
}

func TestSnapshotCDCMergeRangeFallback(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(*destination.MergeOptions)
	}{
		{"WAL batch", func(o *destination.MergeOptions) { o.CDCSnapshot = false }},
		{"missing schema", func(o *destination.MergeOptions) { o.Schema = nil }},
		{"missing key column", func(o *destination.MergeOptions) { o.Schema.Columns = nil }},
		{"no bounds", func(o *destination.MergeOptions) { o.CDCSnapshotRange = nil }},
		{"no keys", func(o *destination.MergeOptions) { o.PrimaryKeys = nil }},
		{"no last key", func(o *destination.MergeOptions) { o.CDCSnapshotRange.Last = nil }},
		{"bad after arity", func(o *destination.MergeOptions) { o.CDCSnapshotRange.After = []string{} }},
		{"equal bounds", func(o *destination.MergeOptions) { o.CDCSnapshotRange.After = []string{"20"} }},
		{"reversed bounds", func(o *destination.MergeOptions) { o.CDCSnapshotRange.After = []string{"21"} }},
		{"noninteger lower", func(o *destination.MergeOptions) { o.CDCSnapshotRange.After = []string{"0 OR TRUE"} }},
		{"noninteger upper", func(o *destination.MergeOptions) { o.CDCSnapshotRange.Last = []string{"20 OR TRUE"} }},
		{"overflow", func(o *destination.MergeOptions) { o.CDCSnapshotRange.Last = []string{"9223372036854775808"} }},
		{"narrow overflow", func(o *destination.MergeOptions) {
			o.Schema.Columns[0].DataType = schema.TypeInt8
			o.CDCSnapshotRange.Last = []string{"128"}
		}},
		{"text collation", func(o *destination.MergeOptions) { o.Schema.Columns[0].DataType = schema.TypeString }},
		{"timestamp conversion", func(o *destination.MergeOptions) { o.Schema.Columns[0].DataType = schema.TypeTimestampTZ }},
		{"unsigned key", func(o *destination.MergeOptions) { o.Schema.Columns[0].Unsigned = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := snapshotMergeOptions()
			tt.adjust(&opts)
			require.Empty(t, snapshotKeyRange(opts))
		})
	}
}

func TestSnapshotCDCMergeUnboundedFirstChunk(t *testing.T) {
	opts := snapshotMergeOptions()
	opts.CDCSnapshotRange.After = nil
	opts.CDCSnapshotRange.Last = []string{"+00020"}
	require.Equal(t, "(`id` <= 20)", snapshotKeyRange(opts))
	opts.CDCSnapshotRange.Last = []string{"-9223372036854775808"}
	require.Equal(t, "(`id` <= -9223372036854775808)", snapshotKeyRange(opts))
}

func TestSnapshotCDCMergeTextKeysKeepFullTargetLookup(t *testing.T) {
	opts := snapshotMergeOptions()
	opts.Schema.Columns[0].DataType = schema.TypeString
	opts.CDCSnapshotRange = &destination.CDCSnapshotRange{After: []string{"A"}, Last: []string{"z"}}
	query := (&StarRocksDestination{}).buildCDCMergeSQL(opts)
	require.Contains(t, query, "LEFT JOIN `target` t ON")
	require.Equal(t, 1, strings.Count(query, "ROW_NUMBER()"))
	require.NotContains(t, query, "history AS")
}

func snapshotMergeOptions() destination.MergeOptions {
	return destination.MergeOptions{
		StagingTable: "staging",
		TargetTable:  "target",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn},
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "payload", DataType: schema.TypeString},
		}},
		CDCSnapshot:      true,
		CDCSnapshotRange: &destination.CDCSnapshotRange{After: []string{"10"}, Last: []string{"20"}},
	}
}
