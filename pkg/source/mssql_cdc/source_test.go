package mssql_cdc

import (
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURIConfig(t *testing.T) {
	cfg, normalized, err := parseURIConfig("mssql+cdc://sa:pass@example:1433/app?encrypt=disable&mode=stream&dest_schema=raw&capture_instance=dbo_users&poll_interval=250ms")
	require.NoError(t, err)

	assert.Equal(t, "raw", cfg.DestSchema)
	assert.Equal(t, "dbo_users", cfg.CaptureInstance)
	assert.Equal(t, 250*time.Millisecond, cfg.PollInterval)

	u, err := url.Parse(normalized)
	require.NoError(t, err)
	assert.Equal(t, "mssql", u.Scheme)
	assert.Equal(t, "disable", u.Query().Get("encrypt"))
	assert.Empty(t, u.Query().Get("mode"))
	assert.Empty(t, u.Query().Get("dest_schema"))
	assert.Empty(t, u.Query().Get("capture_instance"))
	assert.Empty(t, u.Query().Get("poll_interval"))
}

func TestStoredLSNHelpers(t *testing.T) {
	assert.Equal(t, "0000002F0000010D0002", startLSNFromStored("0000002f0000010d0002:0000002f0000010d0003:04"))
	assert.Equal(t, "0000002F0000010D0002", startLSNFromStored("0x0000002f0000010d0002"))
	assert.Empty(t, startLSNFromStored("00000000/00000123"))

	assert.Equal(
		t,
		"0000002F0000010D0002:0000002F0000010D0003:04",
		formatStoredLSN("0000002f0000010d0002", "0000002f0000010d0003", 4),
	)

	assert.Less(t, compareLSNHex("00000000000000000001", "00000000000000000002"), 0)
	assert.Greater(t, compareLSNHex("00000000000000000002", "00000000000000000001"), 0)
	assert.True(t, isZeroLSN("00000000000000000000"))
}

func TestAddCDCColumns(t *testing.T) {
	original := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	got := addCDCColumns(original)

	require.Len(t, got.Columns, 5)
	assert.Equal(t, destination.CDCLSNColumn, got.Columns[2].Name)
	assert.Equal(t, destination.CDCDeletedColumn, got.Columns[3].Name)
	assert.Equal(t, destination.CDCSyncedAtColumn, got.Columns[4].Name)
	assert.Len(t, original.Columns, 2, "addCDCColumns must not mutate the input schema")
}

func TestSourceColumnsWithoutCDC(t *testing.T) {
	tableSchema := addCDCColumns(&schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	})

	got := sourceColumnsWithoutCDC(tableSchema)

	require.Len(t, got, 2)
	assert.Equal(t, "id", got[0].Name)
	assert.Equal(t, "name", got[1].Name)
}

func TestBuildSnapshotQueryUsesNullForDroppedCapturedColumns(t *testing.T) {
	meta := tableMetadata{
		SourceSchema:   "dbo",
		SourceName:     "users",
		CurrentColumns: map[string]bool{"id": true},
	}
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "old_name", DataType: schema.TypeString},
	}

	got := buildSnapshotQuery(meta, columns, true)

	assert.Contains(t, got, "SELECT [id], NULL AS [old_name]")
	assert.Contains(t, got, "FROM [dbo].[users] WITH (HOLDLOCK)")
}

func TestBuildChangesQueryWindowBounds(t *testing.T) {
	meta := tableMetadata{CaptureInstance: "dbo_users"}
	columns := []schema.Column{{Name: "id", DataType: schema.TypeInt64}}

	exclusive := buildChangesQuery(meta, columns, false)
	assert.Contains(t, exclusive, "DECLARE @from_lsn binary(10) = sys.fn_cdc_increment_lsn(CONVERT(binary(10), @p1, 2))")
	assert.Contains(t, exclusive, "WHERE __$operation IN (1, 2, 3, 4)")
	assert.Contains(t, exclusive, "ORDER BY __$start_lsn, __$seqval, __$operation")

	inclusive := buildChangesQuery(meta, columns, true)
	assert.Contains(t, inclusive, "DECLARE @from_lsn binary(10) = CONVERT(binary(10), @p1, 2)")
	assert.NotContains(t, inclusive, "fn_cdc_increment_lsn")
}

func TestPlanChangeWindowNeverRegressesCursor(t *testing.T) {
	const (
		before = "00000000000000000001"
		at     = "00000000000000000002"
		after  = "00000000000000000003"
	)

	start, read := planChangeWindow("", at, false)
	assert.Equal(t, at, start)
	assert.False(t, read)

	start, read = planChangeWindow(after, at, false)
	assert.Equal(t, after, start, "a capture-instance minimum ahead of the global watermark must not regress")
	assert.False(t, read)

	start, read = planChangeWindow(at, at, false)
	assert.Equal(t, at, start)
	assert.False(t, read)

	start, read = planChangeWindow(at, at, true)
	assert.Equal(t, at, start)
	assert.True(t, read, "a resume cursor at the watermark must re-read its boundary once")

	start, read = planChangeWindow(before, at, false)
	assert.Equal(t, before, start)
	assert.True(t, read)
}

func TestSnapshotStampLifecycle(t *testing.T) {
	const start = "0000002F0000010D0002"
	incomplete := snapshotIncompleteLSN(start)
	complete := snapshotCompleteLSN(start)

	assert.True(t, isIncompleteSnapshotStamp(incomplete))
	assert.False(t, isIncompleteSnapshotStamp(complete))
	assert.True(t, isSnapshotStamp(incomplete))
	assert.True(t, isSnapshotStamp(complete))
	assert.False(t, isSnapshotStamp(formatStoredLSN(start, "0000002F0000010D0003", 4)))
	assert.False(t, isIncompleteSnapshotStamp(""))

	// The destination resumes from MAX(_cdc_lsn) as a string, so the stamps
	// and change rows must order: incomplete < complete < any change row.
	changeRow := formatStoredLSN(start, "0000002F0000010D0003", 1)
	assert.Less(t, incomplete, complete)
	assert.Less(t, complete, changeRow)
}

func TestRowsToSnapshotBatchesMarksOnlyFinalBatchComplete(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mockRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(int64(1), "a").
		AddRow(int64(2), "b").
		AddRow(int64(3), "c")
	mock.ExpectQuery("SELECT").WillReturnRows(mockRows)

	rows, err := db.Query("SELECT")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	tableSchema := addCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	results := make(chan source.RecordBatchResult, 4)
	s := &MSSQLCDCSource{}

	const snapshotLSN = "0000002F0000010D0002"
	err = s.rowsToSnapshotBatches(rows, tableSchema, source.ReadOptions{PageSize: 1}, snapshotLSN, results, "items")
	require.NoError(t, err)
	close(results)

	var stamps []string
	var ids []int64
	for res := range results {
		require.NoError(t, res.Err)
		lsnCol, ok := res.Batch.Column(2).(*array.String)
		require.True(t, ok)
		require.EqualValues(t, 1, res.Batch.NumRows())
		stamps = append(stamps, lsnCol.Value(0))
		ids = append(ids, res.Batch.Column(0).(*array.Int64).Value(0))
		res.Batch.Release()
	}

	require.Len(t, stamps, 3)
	assert.Equal(t, []int64{1, 2, 3}, ids, "restamping the final batch must preserve its data columns")
	assert.Equal(t, snapshotIncompleteLSN(snapshotLSN), stamps[0])
	assert.Equal(t, snapshotIncompleteLSN(snapshotLSN), stamps[1])
	assert.Equal(t, snapshotCompleteLSN(snapshotLSN), stamps[2], "only the final batch may carry the complete stamp")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRowsToSnapshotBatchesReturnsIteratorErrorForEmptyBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	iterErr := errors.New("connection reset")
	mockRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(int64(1), "item1").
		RowError(0, iterErr)
	mock.ExpectQuery("SELECT").WillReturnRows(mockRows)

	rows, err := db.Query("SELECT")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	tableSchema := addCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	results := make(chan source.RecordBatchResult, 1)
	s := &MSSQLCDCSource{}

	err = s.rowsToSnapshotBatches(rows, tableSchema, source.ReadOptions{}, "00000000000000000001", results, "items")
	require.ErrorIs(t, err, iterErr)
	assert.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRowsToSnapshotBatchesNormalizesUUIDColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Wire-order bytes for FF19966F-868B-11D0-B42D-00C04FC964FF: the first
	// three fields arrive little-endian from the driver.
	wire := []byte{
		0x6F, 0x96, 0x19, 0xFF,
		0x8B, 0x86,
		0xD0, 0x11,
		0xB4, 0x2D, 0x00, 0xC0, 0x4F, 0xC9, 0x64, 0xFF,
	}
	mockRows := sqlmock.NewRows([]string{"id", "guid"}).AddRow(int64(1), wire)
	mock.ExpectQuery("SELECT").WillReturnRows(mockRows)

	rows, err := db.Query("SELECT")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	tableSchema := addCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "guid", DataType: schema.TypeUUID, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	results := make(chan source.RecordBatchResult, 2)
	s := &MSSQLCDCSource{}

	err = s.rowsToSnapshotBatches(rows, tableSchema, source.ReadOptions{}, "00000000000000000001", results, "items")
	require.NoError(t, err)

	res := <-results
	require.NoError(t, res.Err)
	defer res.Batch.Release()

	guidCol, ok := res.Batch.Column(1).(*array.String)
	require.True(t, ok, "UUID column must be an Arrow string column")
	assert.Equal(t, "FF19966F-868B-11D0-B42D-00C04FC964FF", guidCol.Value(0))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizeSourceValueLeavesNonUUIDColumnsAlone(t *testing.T) {
	raw := []byte{0x01, 0x02}
	got, err := normalizeSourceValue(schema.Column{Name: "blob", DataType: schema.TypeBinary}, raw, false)
	require.NoError(t, err)
	assert.Equal(t, raw, got)

	_, err = normalizeSourceValue(schema.Column{Name: "guid", DataType: schema.TypeUUID}, raw, false)
	assert.ErrorContains(t, err, `uniqueidentifier column "guid"`)
}

func TestSelectTables(t *testing.T) {
	all := []source.SourceTableInfo{
		{Name: "dbo.users"},
		{Name: "sales.orders"},
	}
	skipped := []skippedTable{{name: "dbo.audit_log", reason: "it has no primary key"}}

	selected, err := selectTables(all, skipped, nil)
	require.NoError(t, err)
	assert.Len(t, selected, 2, "no filter selects every ingestible table")

	selected, err = selectTables(all, skipped, []string{"DBO.Users"})
	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "dbo.users", selected[0].Name)

	_, err = selectTables(all, skipped, []string{"dbo.users", "dbo.missing"})
	assert.ErrorContains(t, err, "dbo.missing")
	assert.ErrorContains(t, err, "schema-qualified")

	_, err = selectTables(all, skipped, []string{"dbo.audit_log"})
	assert.ErrorContains(t, err, "no primary key")
}

func TestValidateCapturedPrimaryKeys(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "users",
		Schema: "dbo",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}

	assert.NoError(t, validateCapturedPrimaryKeys(tableSchema, []string{"id"}))
	assert.NoError(t, validateCapturedPrimaryKeys(tableSchema, []string{"ID"}))
	assert.ErrorContains(t, validateCapturedPrimaryKeys(tableSchema, []string{"ssn"}), `primary key column "ssn"`)
	assert.NoError(t, validateCapturedPrimaryKeys(tableSchema, nil))
}

func pairerForTest(t *testing.T, pks ...string) *updatePairer {
	t.Helper()
	tableSchema := &schema.TableSchema{
		Name:        "items",
		PrimaryKeys: pks,
	}
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: "value", DataType: schema.TypeInt64},
	}
	p, err := newUpdatePairer(tableSchema, columns)
	require.NoError(t, err)
	return p
}

func TestUpdatePairerDropsBeforeImageWhenKeyUnchanged(t *testing.T) {
	p := pairerForTest(t, "id")

	assert.Nil(t, p.push([]any{int64(1), "old", int64(100)}, "0000002F0000010D0002:0000002F0000010D0003:03", 3))

	out := p.push([]any{int64(1), "new", int64(150)}, "0000002F0000010D0002:0000002F0000010D0003:04", 4)
	require.Len(t, out, 1)
	assert.False(t, out[0].deleted)
	assert.Equal(t, "0000002F0000010D0002:0000002F0000010D0003:04", out[0].lsn)
	assert.Equal(t, []any{int64(1), "new", int64(150)}, out[0].values)
}

func TestUpdatePairerEmitsDeleteForOldKeyWhenKeyMoved(t *testing.T) {
	p := pairerForTest(t, "id")

	assert.Nil(t, p.push([]any{int64(1), "moved", int64(100)}, "0000002F0000010D0002:0000002F0000010D0003:03", 3))

	out := p.push([]any{int64(100), "moved", int64(100)}, "0000002F0000010D0002:0000002F0000010D0003:04", 4)
	require.Len(t, out, 2)

	assert.True(t, out[0].deleted, "before-image must be replayed as a delete of the old key")
	assert.Equal(t, []any{int64(1), "moved", int64(100)}, out[0].values)
	assert.Equal(t, "0000002F0000010D0002:0000002F0000010D0003:03", out[0].lsn, "delete keeps the before-image LSN, which orders before the upsert")

	assert.False(t, out[1].deleted)
	assert.Equal(t, []any{int64(100), "moved", int64(100)}, out[1].values)
}

func TestUpdatePairerCompositeKey(t *testing.T) {
	p := pairerForTest(t, "id", "name")

	assert.Nil(t, p.push([]any{int64(1), "a", int64(100)}, "X0000000000000000001:X0000000000000000002:03", 3))
	out := p.push([]any{int64(1), "a", int64(150)}, "X0000000000000000001:X0000000000000000002:04", 4)
	assert.Len(t, out, 1, "no key column moved")

	assert.Nil(t, p.push([]any{int64(1), "a", int64(100)}, "X0000000000000000001:X0000000000000000005:03", 3))
	out = p.push([]any{int64(1), "b", int64(100)}, "X0000000000000000001:X0000000000000000005:04", 4)
	assert.Len(t, out, 2, "one key column moved")
}

func TestUpdatePairerFlushesPendingBeforeNonUpdateRow(t *testing.T) {
	p := pairerForTest(t, "id")

	assert.Nil(t, p.push([]any{int64(1), "gone", int64(100)}, "0000002F0000010D0002:0000002F0000010D0003:03", 3))

	out := p.push([]any{int64(2), "other", int64(200)}, "0000002F0000010D0002:0000002F0000010D0009:01", 1)
	require.Len(t, out, 2, "unpaired before-image is treated as an identity move")
	assert.True(t, out[0].deleted)
	assert.True(t, out[1].deleted)
}

func TestUpdatePairerFlushesPendingBeforeConsecutiveBeforeImage(t *testing.T) {
	p := pairerForTest(t, "id")

	assert.Nil(t, p.push([]any{int64(1), "first", int64(100)}, "0000002F0000010D0002:0000002F0000010D0003:03", 3))

	out := p.push([]any{int64(2), "second", int64(200)}, "0000002F0000010D0002:0000002F0000010D0007:03", 3)
	require.Len(t, out, 1, "unpaired before-image must not be silently replaced")
	assert.True(t, out[0].deleted)
	assert.Equal(t, []any{int64(1), "first", int64(100)}, out[0].values)

	out = p.push([]any{int64(2), "second", int64(250)}, "0000002F0000010D0002:0000002F0000010D0007:04", 4)
	assert.Len(t, out, 1, "the replacement pending still pairs with its after-image")
}

func TestUpdatePairerFlushEmitsTrailingBeforeImage(t *testing.T) {
	p := pairerForTest(t, "id")

	assert.Nil(t, p.flush())

	assert.Nil(t, p.push([]any{int64(1), "tail", int64(100)}, "0000002F0000010D0002:0000002F0000010D0003:03", 3))

	out := p.flush()
	require.Len(t, out, 1, "a before-image left at end of stream is an identity move")
	assert.True(t, out[0].deleted)
	assert.Equal(t, []any{int64(1), "tail", int64(100)}, out[0].values)
	assert.Nil(t, p.flush(), "flush must clear the pending state")
}

func TestUpdatePairerByteAndNilKeys(t *testing.T) {
	tableSchema := &schema.TableSchema{Name: "files", PrimaryKeys: []string{"hash"}}
	columns := []schema.Column{
		{Name: "hash", DataType: schema.TypeBinary},
		{Name: "note", DataType: schema.TypeString},
	}
	p, err := newUpdatePairer(tableSchema, columns)
	require.NoError(t, err)

	assert.Nil(t, p.push([]any{[]byte{0xDE, 0xAD}, "x"}, "A0000000000000000001:A0000000000000000002:03", 3))
	out := p.push([]any{[]byte{0xDE, 0xAD}, "y"}, "A0000000000000000001:A0000000000000000002:04", 4)
	assert.Len(t, out, 1, "equal []byte keys must not count as moved")

	assert.Nil(t, p.push([]any{nil, "x"}, "A0000000000000000001:A0000000000000000009:03", 3))
	out = p.push([]any{[]byte{0x01}, "x"}, "A0000000000000000001:A0000000000000000009:04", 4)
	assert.Len(t, out, 2, "nil to non-nil key counts as moved")
}

func TestNewUpdatePairerRequiresCapturedKeys(t *testing.T) {
	tableSchema := &schema.TableSchema{Name: "items", PrimaryKeys: []string{"ssn"}}
	columns := []schema.Column{{Name: "id", DataType: schema.TypeInt64}}

	_, err := newUpdatePairer(tableSchema, columns)
	assert.ErrorContains(t, err, `primary key column "ssn"`)
}

func TestErrorClassifiers(t *testing.T) {
	assert.True(t, isInvalidLSNRangeError(mssqldb.Error{Number: 313}))
	assert.True(t, isInvalidLSNRangeError(fmt.Errorf("query: %w", mssqldb.Error{Number: 313})))
	assert.False(t, isInvalidLSNRangeError(mssqldb.Error{Number: 1205}))
	assert.False(t, isInvalidLSNRangeError(errors.New("boom")))

	assert.True(t, isSnapshotIsolationUnavailableError(mssqldb.Error{Number: 3952}))
	assert.False(t, isSnapshotIsolationUnavailableError(mssqldb.Error{Number: 1205}))
	assert.False(t, isSnapshotIsolationUnavailableError(nil))
}

func TestIsTransientMSSQLError(t *testing.T) {
	deadlock := mssqldb.Error{Number: 1205, Message: "deadlock victim"}
	assert.True(t, isTransientMSSQLError(deadlock))
	assert.True(t, isTransientMSSQLError(fmt.Errorf("failed to query CDC changes: %w", deadlock)))
	assert.False(t, isTransientMSSQLError(mssqldb.Error{Number: 208, Message: "invalid object"}))
	assert.False(t, isTransientMSSQLError(errors.New("connection reset")))
	assert.False(t, isTransientMSSQLError(nil))
}

func TestLSNIdentity(t *testing.T) {
	assert.Equal(
		t,
		"0000002F0000010D0002:0000002F0000010D0003",
		lsnIdentity("0000002F0000010D0002:0000002F0000010D0003:04"),
	)
	assert.Equal(t, "garbage", lsnIdentity("garbage"))
}
