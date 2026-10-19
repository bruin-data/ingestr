package starrocks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func TestBuildCDCRetireSQLTombstonesOnlyStaleLiveRows(t *testing.T) {
	// SQLite's INSERT OR REPLACE stands in for the PRIMARY KEY upsert.
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE target (id INTEGER PRIMARY KEY, payload TEXT, _cdc_lsn TEXT, _cdc_deleted BOOLEAN, _cdc_synced_at TEXT);
		INSERT INTO target VALUES
		(1, 'stale', '00000001/00000010/0000000000000002', FALSE, 'old'),
		(2, 'snapshot', '00000001/00000020', FALSE, 'old'),
		(3, 'tombstone', '00000001/00000005', TRUE, 'old'),
		(4, 'unstamped', NULL, NULL, 'old'),
		(5, 'newer', '00000001/00000021', FALSE, 'old')`)
	require.NoError(t, err)

	body, err := buildCDCRetireSQL("target", []string{"id", "payload", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}, destination.CDCRetireOptions{
		Table:       "target",
		PrimaryKeys: []string{"id"},
		Boundary:    "00000001/00000020",
		SyncedAt:    time.Date(2026, 10, 17, 1, 2, 3, 456000, time.UTC),
	})
	require.NoError(t, err)
	_, err = db.Exec("INSERT OR REPLACE INTO target " + body)
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, payload, COALESCE(_cdc_lsn, ''), COALESCE(_cdc_deleted, FALSE), _cdc_synced_at FROM target ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	type row struct {
		id       int
		payload  string
		lsn      string
		deleted  bool
		syncedAt string
	}
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.id, &r.payload, &r.lsn, &r.deleted, &r.syncedAt))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	retired := "2026-10-17 01:02:03.000456"
	require.Equal(t, []row{
		{1, "stale", "00000001/00000020", true, retired},
		{2, "snapshot", "00000001/00000020", false, "old"},
		{3, "tombstone", "00000001/00000005", true, "old"},
		{4, "unstamped", "00000001/00000020", true, retired},
		{5, "newer", "00000001/00000021", false, "old"},
	}, got)
}

func TestBuildCDCRetireSQLRequiresCDCColumns(t *testing.T) {
	_, err := buildCDCRetireSQL("target", []string{"id", "_cdc_lsn"}, destination.CDCRetireOptions{Boundary: "00000001/00000020"})
	require.Error(t, err)
}
