package mysql

import (
	"context"
	"crypto/tls"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMySQLCDCURI(t *testing.T) {
	cfg, normalized, connInfo, err := parseMySQLCDCURI("mysql+cdc://user:pass@example:3307/app?charset=utf8mb4&mode=batch&dest_schema=raw&server_id=123&flavor=mysql")
	require.NoError(t, err)

	assert.Equal(t, MySQLCDCModeBatch, cfg.Mode)
	assert.Equal(t, "raw", cfg.DestSchema)
	assert.Equal(t, uint32(123), cfg.ServerID)
	assert.Equal(t, gomysql.MySQLFlavor, cfg.Flavor)
	assert.Equal(t, "example", connInfo.Host)
	assert.Equal(t, uint16(3307), connInfo.Port)
	assert.Equal(t, "user", connInfo.User)
	assert.Equal(t, "pass", connInfo.Password)
	assert.Equal(t, "app", connInfo.Database)

	u, err := url.Parse(normalized)
	require.NoError(t, err)
	assert.Equal(t, "mysql", u.Scheme)
	assert.Equal(t, "utf8mb4", u.Query().Get("charset"))
	assert.Empty(t, u.Query().Get("mode"))
	assert.Empty(t, u.Query().Get("dest_schema"))
	assert.Empty(t, u.Query().Get("server_id"))
	assert.Empty(t, u.Query().Get("flavor"))
}

func TestParseMySQLCDCURICarriesReplicationConnectionOptions(t *testing.T) {
	cfg, normalized, connInfo, err := parseMySQLCDCURI("mysql+cdc://user:pass@example:3307/app?charset=utf8mb4,utf8&mode=batch&tls=skip-verify&readTimeout=7s")
	require.NoError(t, err)

	assert.Equal(t, MySQLCDCModeBatch, cfg.Mode)
	assert.Equal(t, "example", connInfo.Host)
	assert.Equal(t, uint16(3307), connInfo.Port)
	assert.Equal(t, "user", connInfo.User)
	assert.Equal(t, "pass", connInfo.Password)
	assert.Equal(t, "app", connInfo.Database)
	require.NotNil(t, connInfo.TLSConfig)
	assert.True(t, connInfo.TLSConfig.InsecureSkipVerify)
	assert.Equal(t, "utf8mb4", connInfo.Charset)
	assert.Equal(t, 7*time.Second, connInfo.ReadTimeout)

	u, err := url.Parse(normalized)
	require.NoError(t, err)
	assert.Equal(t, "skip-verify", u.Query().Get("tls"))
	assert.Equal(t, "7s", u.Query().Get("readTimeout"))
}

func TestParseMySQLCDCURIRejectsInvalidMode(t *testing.T) {
	_, _, _, err := parseMySQLCDCURI("mysql+cdc://user:pass@example/app?mode=once")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mode")
}

func TestParseMySQLCDCURIRejectsStreamMode(t *testing.T) {
	_, _, _, err := parseMySQLCDCURI("mysql+cdc://user:pass@example/app?mode=stream")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream mode is not supported")
}

func TestParseMariaDBCDCURIUsesMariaDBFlavor(t *testing.T) {
	cfg, normalized, connInfo, err := parseMySQLCDCURI("mariadb+cdc://user:pass@example/app")
	require.NoError(t, err)

	assert.Equal(t, gomysql.MariaDBFlavor, cfg.Flavor)
	assert.Equal(t, uint16(3306), connInfo.Port)
	u, err := url.Parse(normalized)
	require.NoError(t, err)
	assert.Equal(t, "mariadb", u.Scheme)
}

func TestMySQLCDCBinlogSyncerConfigCarriesConnectionOptions(t *testing.T) {
	tlsConfig := &tls.Config{ServerName: "example"}
	source := &MySQLCDCSource{
		cdcConfig: MySQLCDCConfig{
			ServerID: 99,
			Flavor:   gomysql.MySQLFlavor,
		},
		connInfo: mysqlCDCConnInfo{
			Host:        "example",
			Port:        3307,
			User:        "user",
			Password:    "pass",
			TLSConfig:   tlsConfig,
			Charset:     "utf8mb4",
			ReadTimeout: 7 * time.Second,
		},
	}

	cfg := source.binlogSyncerConfig()

	assert.Equal(t, uint32(99), cfg.ServerID)
	assert.Equal(t, "example", cfg.Host)
	assert.Equal(t, uint16(3307), cfg.Port)
	assert.Equal(t, "user", cfg.User)
	assert.Equal(t, "pass", cfg.Password)
	assert.Equal(t, "utf8mb4", cfg.Charset)
	assert.Same(t, tlsConfig, cfg.TLSConfig)
	assert.Equal(t, 7*time.Second, cfg.ReadTimeout)
}

func TestCheckMySQLBinlogSettingsRejectsPartialJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SHOW VARIABLES").
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("log_bin", "ON").
			AddRow("binlog_format", "ROW").
			AddRow("binlog_row_image", "FULL").
			AddRow("binlog_row_value_options", "PARTIAL_JSON"))

	err = checkMySQLBinlogSettings(context.Background(), db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PARTIAL_JSON")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestValidateMySQLCDCTableSupportedRejectsNativeBinlogTypes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.COLUMNS").
		WithArgs("app", "items").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).
			AddRow("status", "enum").
			AddRow("flags", "bit"))

	err = validateMySQLCDCTableSupported(context.Background(), db, "app", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ENUM, SET, or BIT")
	assert.Contains(t, err.Error(), "status ENUM")
	assert.Contains(t, err.Error(), "flags BIT")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCReadAllValidatesOnlySelectedTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).
			AddRow("bad").
			AddRow("good"))
	expectMySQLCDCTableSchema(mock, "app", "bad")
	expectMySQLCDCTableSchema(mock, "app", "good")
	mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
		WithArgs("app", "good").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))

	src := &MySQLCDCSource{db: db, database: "app"}
	selected, err := src.getSelectedTables(context.Background(), source.MultiTableReadOptions{Tables: []string{"good"}})
	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "good", selected[0].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectMySQLCDCTableSchema(mock sqlmock.Sqlmock, database, table string) {
	mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+COLUMN_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
		WithArgs(database, table).
		WillReturnRows(sqlmock.NewRows([]string{
			"COLUMN_NAME",
			"COLUMN_TYPE",
			"IS_NULLABLE",
			"NUMERIC_PRECISION",
			"NUMERIC_SCALE",
			"CHARACTER_MAXIMUM_LENGTH",
		}).
			AddRow("id", "bigint", "NO", nil, nil, nil).
			AddRow("name", "varchar(255)", "YES", nil, nil, int64(255)))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.KEY_COLUMN_USAGE").
		WithArgs(database, table).
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("id"))
}

func TestStoredMySQLPositionHelpers(t *testing.T) {
	pos := gomysql.Position{Name: "mysql-bin.000012", Pos: 345}
	stored := formatStoredMySQLPosition(pos, 7)

	assert.Equal(t, "00000000000000000012:mysql-bin.000012:00000000000000000345:00000000000000000007", stored)

	parsed, ok := parseStoredMySQLPosition(stored)
	require.True(t, ok)
	assert.Equal(t, pos, parsed)

	legacy, ok := parseStoredMySQLPosition("mysql-bin.000012:00000000000000000345:000007")
	require.True(t, ok)
	assert.Equal(t, pos, legacy)

	wideStored := formatStoredMySQLPosition(pos, 1000000)
	parsed, ok = parseStoredMySQLPosition(wideStored)
	require.True(t, ok)
	assert.Equal(t, pos, parsed)
	assert.True(
		t,
		formatStoredMySQLPosition(pos, 999999) < wideStored,
		"stored positions should sort lexically by row sequence",
	)

	_, ok = parseStoredMySQLPosition("00000000/00000123")
	assert.False(t, ok)
}

func TestAddMySQLCDCColumns(t *testing.T) {
	original := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	got := addMySQLCDCColumns(original)

	require.Len(t, got.Columns, 5)
	assert.Equal(t, destination.CDCLSNColumn, got.Columns[2].Name)
	assert.Equal(t, destination.CDCDeletedColumn, got.Columns[3].Name)
	assert.Equal(t, destination.CDCSyncedAtColumn, got.Columns[4].Name)
	assert.Len(t, original.Columns, 2, "addMySQLCDCColumns must not mutate the input schema")
}

func TestMySQLCDCChangesToBatch(t *testing.T) {
	tableSchema := addMySQLCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})

	record, err := mysqlCDCChangesToBatch([]mysqlCDCChange{
		{values: []interface{}{int64(1), "item1"}, lsn: "00000000000000000001:mysql-bin.000001:00000000000000000100:000000", deleted: false},
		{values: []interface{}{int64(2), "item2"}, lsn: "00000000000000000001:mysql-bin.000001:00000000000000000120:000000", deleted: true},
	}, tableSchema)
	require.NoError(t, err)
	defer record.Release()

	require.EqualValues(t, 2, record.NumRows())
	assert.Equal(t, int64(1), record.Column(0).(*array.Int64).Value(0))
	assert.Equal(t, "item2", record.Column(1).(*array.String).Value(1))
	assert.False(t, record.Column(3).(*array.Boolean).Value(0))
	assert.True(t, record.Column(3).(*array.Boolean).Value(1))
}

func TestRowsToMySQLCDCSnapshotBatchesReturnsIteratorErrorForEmptyBatch(t *testing.T) {
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

	tableSchema := addMySQLCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	results := make(chan source.RecordBatchResult, 1)

	err = rowsToMySQLCDCSnapshotBatches(rows, tableSchema, source.ReadOptions{}, gomysql.Position{Name: "mysql-bin.000001", Pos: 100}, results, "items")
	require.ErrorIs(t, err, iterErr)
	assertNoCDCResult(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAppendMySQLCDCBufferedChangesFlushesAtBatchSize(t *testing.T) {
	tableSchema := addMySQLCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	results := make(chan source.RecordBatchResult, 2)
	buffers := map[string]*mysqlCDCChangeBuffer{}

	err := appendMySQLCDCBufferedChanges(buffers, "items", tableSchema, "items", []mysqlCDCChange{
		{values: []interface{}{int64(1), "item1"}, lsn: formatStoredMySQLPosition(gomysql.Position{Name: "mysql-bin.000001", Pos: 100}, 0), deleted: false},
	}, 2, results)
	require.NoError(t, err)
	assertNoCDCResult(t, results)

	err = appendMySQLCDCBufferedChanges(buffers, "items", tableSchema, "items", []mysqlCDCChange{
		{values: []interface{}{int64(2), "item2"}, lsn: formatStoredMySQLPosition(gomysql.Position{Name: "mysql-bin.000001", Pos: 120}, 0), deleted: false},
	}, 2, results)
	require.NoError(t, err)

	result := <-results
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	assert.Equal(t, "items", result.TableName)
	assert.EqualValues(t, 2, result.Batch.NumRows())
	result.Batch.Release()

	err = appendMySQLCDCBufferedChanges(buffers, "items", tableSchema, "items", []mysqlCDCChange{
		{values: []interface{}{int64(3), "item3"}, lsn: formatStoredMySQLPosition(gomysql.Position{Name: "mysql-bin.000001", Pos: 140}, 0), deleted: false},
	}, 2, results)
	require.NoError(t, err)
	assertNoCDCResult(t, results)

	require.NoError(t, flushMySQLCDCChangeBuffers(buffers, results))
	result = <-results
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	assert.EqualValues(t, 1, result.Batch.NumRows())
	result.Batch.Release()
}

func assertNoCDCResult(t *testing.T, results <-chan source.RecordBatchResult) {
	t.Helper()
	select {
	case result := <-results:
		if result.Batch != nil {
			result.Batch.Release()
		}
		require.Fail(t, "unexpected CDC result")
	default:
	}
}

func TestPrimaryKeyChanged(t *testing.T) {
	assert.False(t, primaryKeyChanged([]interface{}{int64(1), "a"}, []interface{}{int64(1), "b"}, []int{0}))
	assert.True(t, primaryKeyChanged([]interface{}{int64(1), "a"}, []interface{}{int64(2), "a"}, []int{0}))

	utcPK := time.Date(2026, 6, 16, 12, 30, 45, 123000, time.UTC)
	offsetPK := utcPK.In(time.FixedZone("offset", 3*60*60))
	assert.False(t, primaryKeyChanged([]interface{}{utcPK}, []interface{}{offsetPK}, []int{0}))
	assert.True(t, primaryKeyChanged([]interface{}{utcPK}, []interface{}{utcPK.Add(time.Microsecond)}, []int{0}))
}

func TestMySQLCDCResultTableName(t *testing.T) {
	assert.Empty(t, mysqlCDCResultTableName("items", 1, false), "single-table Read should not tag batches")
	assert.Equal(t, "items", mysqlCDCResultTableName("items", 1, true), "ReadAll must tag even one selected table")
	assert.Equal(t, "items", mysqlCDCResultTableName("items", 2, false), "multiple-table stream batches must be tagged")
}

func TestBeginMySQLConsistentSnapshotLocksAroundPosition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectExec("UNLOCK TABLES").
		WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	pos, err := beginMySQLConsistentSnapshot(ctx, conn)
	require.NoError(t, err)

	assert.Equal(t, gomysql.Position{Name: "mysql-bin.000012", Pos: 345}, pos)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBeginMySQLConsistentSnapshotUnlocksOnPositionError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnError(errors.New("status failed"))
	mock.ExpectQuery("SHOW MASTER STATUS").
		WillReturnError(errors.New("master failed"))
	mock.ExpectExec("UNLOCK TABLES").
		WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = beginMySQLConsistentSnapshot(ctx, conn)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCTableReadErrorsWhenResumePositionExpired(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	resume := gomysql.Position{Name: "mysql-bin.000012", Pos: 345}
	stored := formatStoredMySQLPosition(resume, 0)

	mock.ExpectQuery("SHOW BINARY LOGS").
		WillReturnRows(sqlmock.NewRows([]string{"Log_name", "File_size"}).AddRow("mysql-bin.000013", "1000"))

	table := &MySQLCDCTable{
		source:    &MySQLCDCSource{db: db},
		tableName: "items",
	}

	records, err := table.Read(context.Background(), source.ReadOptions{CDCResumeLSN: stored})
	require.NoError(t, err)

	result, ok := <-records
	require.True(t, ok)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "resume position")
	assert.Contains(t, result.Err.Error(), "--full-refresh")

	_, ok = <-records
	assert.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCTableReadErrorsWhenResumePositionInvalid(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	table := &MySQLCDCTable{
		source:    &MySQLCDCSource{db: db},
		tableName: "items",
	}

	records, err := table.Read(context.Background(), source.ReadOptions{CDCResumeLSN: "not-a-mysql-lsn"})
	require.NoError(t, err)

	result, ok := <-records
	require.True(t, ok)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "invalid")
	assert.Contains(t, result.Err.Error(), "--full-refresh")

	_, ok = <-records
	assert.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}
