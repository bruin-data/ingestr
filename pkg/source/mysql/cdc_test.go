package mysql

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMySQLCDCURI(t *testing.T) {
	cfg, normalized, connInfo, err := parseMySQLCDCURI("mysql+cdc://user:pass@example:3307/app?charset=utf8mb4&mode=batch&dest_schema=raw&server_id=123&flavor=mysql&xa_buffer_limit=456&xa_buffer_bytes_limit=789&xa_pending_limit=7")
	require.NoError(t, err)

	assert.Equal(t, "raw", cfg.DestSchema)
	assert.Equal(t, uint32(123), cfg.ServerID)
	assert.Equal(t, gomysql.MySQLFlavor, cfg.Flavor)
	assert.Equal(t, uint64(456), cfg.XABufferLimit)
	assert.Equal(t, uint64(7), cfg.PendingXALimit)
	assert.Equal(t, uint64(789), cfg.XABufferBytes)
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
	assert.Empty(t, u.Query().Get("xa_buffer_limit"))
	assert.Empty(t, u.Query().Get("xa_buffer_bytes_limit"))
	assert.Empty(t, u.Query().Get("xa_pending_limit"))
}

func TestParseMySQLCDCURICarriesReplicationConnectionOptions(t *testing.T) {
	_, normalized, connInfo, err := parseMySQLCDCURI("mysql+cdc://user:pass@example:3307/app?charset=utf8mb4,utf8&mode=batch&tls=skip-verify&readTimeout=7s")
	require.NoError(t, err)

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
			AddRow("flags", "bit").
			AddRow("location", "point"))

	err = validateMySQLCDCTableSupported(context.Background(), db, "app", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ENUM, SET, BIT, or spatial (GEOMETRY)")
	assert.Contains(t, err.Error(), "status ENUM")
	assert.Contains(t, err.Error(), "flags BIT")
	assert.Contains(t, err.Error(), "location POINT")
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

func TestMySQLCDCReadAllInitializesFilteredDiscoveryWithoutGetTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("bad").AddRow("good"))
	expectMySQLCDCTableSchema(mock, "app", "good")
	mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
		WithArgs("app", "good").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))

	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("bad").AddRow("good"))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))
	expectMySQLCDCTableSchema(mock, "app", "good")
	mock.ExpectQuery("SELECT .* FROM .*good").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(1), "one"))
	mock.ExpectExec("COMMIT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))

	src := &MySQLCDCSource{db: db, database: "app"}
	results, err := src.ReadAll(t.Context(), source.MultiTableReadOptions{Tables: []string{"good"}})
	require.NoError(t, err)
	var batches int
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			batches++
			assert.Equal(t, "good", result.TableName)
			result.Batch.Release()
		}
	}
	require.Equal(t, 1, batches)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCReadAllInitializesUnfilteredDiscoveryWithoutGetTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("good"))
	expectMySQLCDCTableSchema(mock, "app", "good")
	mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
		WithArgs("app", "good").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))

	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("good"))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))
	expectMySQLCDCTableSchema(mock, "app", "good")
	mock.ExpectQuery("SELECT .* FROM .*good").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(1), "one"))
	mock.ExpectExec("COMMIT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))

	src := &MySQLCDCSource{db: db, database: "app"}
	results, err := src.ReadAll(t.Context(), source.MultiTableReadOptions{})
	require.NoError(t, err)
	var batches int
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			batches++
			assert.Equal(t, "good", result.TableName)
			result.Batch.Release()
		}
	}
	require.Equal(t, 1, batches)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCReadAllRejectsMissingOrEmptyInventoryWithoutGetTables(t *testing.T) {
	t.Run("missing selected table", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
			WithArgs("app").
			WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("other"))
		src := &MySQLCDCSource{db: db, database: "app"}
		_, err = src.ReadAll(t.Context(), source.MultiTableReadOptions{Tables: []string{"missing"}})
		require.ErrorContains(t, err, "no longer available")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty unfiltered inventory", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
			WithArgs("app").
			WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}))
		src := &MySQLCDCSource{db: db, database: "app"}
		_, err = src.ReadAll(t.Context(), source.MultiTableReadOptions{})
		require.ErrorContains(t, err, "no MySQL tables found")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMySQLCDCReadAllValidatesInventoryAtEverySnapshotBoundary(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("second"))
	for _, table := range []string{"first", "second"} {
		expectMySQLCDCTableSchema(mock, "app", table)
	}
	for _, table := range []string{"first", "second"} {
		mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
			WithArgs("app", table).
			WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))
	}

	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("second"))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))
	expectMySQLCDCTableSchema(mock, "app", "first")
	mock.ExpectQuery("SELECT .* FROM .*first").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(1), "one"))
	mock.ExpectExec("COMMIT").WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "400"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("late").AddRow("second"))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))

	src := &MySQLCDCSource{db: db, database: "app"}
	results, err := src.ReadAll(t.Context(), source.MultiTableReadOptions{})
	require.NoError(t, err)
	var readErr error
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			readErr = result.Err
		}
	}
	require.ErrorContains(t, readErr, "appeared after")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCReadAllValidatesInventoryAfterResumedTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("second"))
	for _, table := range []string{"first", "second"} {
		expectMySQLCDCTableSchema(mock, "app", table)
	}
	for _, table := range []string{"first", "second"} {
		mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
			WithArgs("app", table).
			WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))
	}
	mock.ExpectQuery("SHOW BINARY LOGS").
		WillReturnRows(sqlmock.NewRows([]string{"Log_name", "File_size"}).AddRow("mysql-bin.000012", "1000"))
	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "400"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("late").AddRow("second"))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))

	resume := formatStoredMySQLPosition(gomysql.Position{Name: "mysql-bin.000012", Pos: 300}, 0)
	src := &MySQLCDCSource{db: db, database: "app"}
	results, err := src.ReadAll(t.Context(), source.MultiTableReadOptions{
		CDCResumeLSNs: map[string]string{"first": resume},
	})
	require.NoError(t, err)
	var readErr error
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			readErr = result.Err
		}
	}
	require.ErrorContains(t, readErr, "appeared after")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCReadAllIgnoresUnrequestedInventoryChangesAtSnapshotBoundaries(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("ignored").AddRow("second"))
	for _, table := range []string{"first", "second"} {
		expectMySQLCDCTableSchema(mock, "app", table)
	}
	for _, table := range []string{"first", "second"} {
		mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
			WithArgs("app", table).
			WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))
	}

	for i, table := range []string{"first", "second"} {
		mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SHOW BINARY LOG STATUS").
			WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
		mock.ExpectQuery("XA RECOVER").
			WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
		inventory := sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("first").AddRow("ignored")
		if i == 1 {
			inventory.AddRow("late")
		}
		inventory.AddRow("second")
		mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").WithArgs("app").WillReturnRows(inventory)
		mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))
		expectMySQLCDCTableSchema(mock, "app", table)
		mock.ExpectQuery("SELECT .* FROM .*" + table).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(i+1), table))
		mock.ExpectExec("COMMIT").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))

	src := &MySQLCDCSource{db: db, database: "app"}
	results, err := src.ReadAll(t.Context(), source.MultiTableReadOptions{Tables: []string{"first", "second"}})
	require.NoError(t, err)
	var batches int
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			batches++
			result.Batch.Release()
		}
	}
	require.Equal(t, 2, batches)
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

func TestMySQLCDCRejectsNonMergeStrategies(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectMySQLCDCTableSchema(mock, "app", "items")
	mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
		WithArgs("app", "items").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}))

	src := &MySQLCDCSource{db: db, database: "app"}
	_, err = src.GetTable(t.Context(), source.TableRequest{Name: "items", Strategy: config.StrategyAppend})
	require.ErrorContains(t, err, "only supports the merge strategy")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLCDCDoesNotAdvertiseStreaming(t *testing.T) {
	_, ok := any(NewMySQLCDCSource()).(source.StreamingSource)
	require.False(t, ok)
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

func TestStoredMySQLCheckpointCarriesLineage(t *testing.T) {
	checkpoint := mysqlCDCCheckpoint{
		Position: gomysql.Position{Name: "mysql-bin.000012", Pos: 345},
		Identity: "mysql:550e8400-e29b-41d4-a716-446655440000",
		GTIDSet:  "550e8400-e29b-41d4-a716-446655440000:1-42",
	}
	stored := formatStoredMySQLCheckpoint(checkpoint)
	parsed, ok := parseStoredMySQLCheckpoint(stored)
	require.True(t, ok)
	assert.Equal(t, checkpoint, parsed)
	assert.True(t, strings.HasPrefix(stored, formatStoredMySQLPosition(checkpoint.Position, 0)+":l1:"))
}

func TestMySQLCDCResumeRejectsMissingAndMismatchedLineage(t *testing.T) {
	src := &MySQLCDCSource{lineageIdentity: "mysql:current"}

	_, err := src.canResume(t.Context(), mysqlCDCCheckpoint{Position: gomysql.Position{Name: "mysql-bin.000001", Pos: 4}})
	require.ErrorContains(t, err, "no source lineage identity")

	_, err = src.canResume(t.Context(), mysqlCDCCheckpoint{
		Position: gomysql.Position{Name: "mysql-bin.000001", Pos: 4},
		Identity: "mysql:other",
	})
	require.ErrorContains(t, err, "connected server is")
}

func TestMySQLCDCGTIDContainment(t *testing.T) {
	const uuid = "550e8400-e29b-41d4-a716-446655440000"
	contains, err := mysqlCDCGTIDSetContains(gomysql.MySQLFlavor, uuid+":1-10", uuid+":1-5")
	require.NoError(t, err)
	assert.True(t, contains)

	contains, err = mysqlCDCGTIDSetContains(gomysql.MySQLFlavor, uuid+":1-4", uuid+":1-5")
	require.NoError(t, err)
	assert.False(t, contains)
}

func TestMySQLCDCRejectsReservedSourceColumns(t *testing.T) {
	err := validateMySQLCDCSourceColumns(&schema.TableSchema{Columns: []schema.Column{{Name: "_CDC_LSN"}}}, "items")
	require.ErrorContains(t, err, "reserved metadata column")
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

func TestConvertMySQLCDCBinlogValueUnsigned(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		col   schema.Column
		want  interface{}
	}{
		{"tinyint unsigned wraps", int8(-1), schema.Column{DataType: schema.TypeInt16, Unsigned: true}, int16(255)},
		{"smallint unsigned wraps", int16(-1), schema.Column{DataType: schema.TypeInt32, Unsigned: true}, int32(65535)},
		{"mediumint unsigned sign-extended", int32(-1), schema.Column{DataType: schema.TypeInt32, Unsigned: true}, int32(16777215)},
		{"int unsigned wraps", int32(-1), schema.Column{DataType: schema.TypeInt64, Unsigned: true}, int64(4294967295)},
		{"bigint unsigned wraps", int64(-1), schema.Column{DataType: schema.TypeDecimal, Unsigned: true}, "18446744073709551615"},
		{"unsigned positive passthrough", int32(42), schema.Column{DataType: schema.TypeInt64, Unsigned: true}, int64(42)},
		{"signed untouched", int64(-1), schema.Column{DataType: schema.TypeInt64}, int64(-1)},
		{"nil untouched", nil, schema.Column{DataType: schema.TypeInt64, Unsigned: true}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, convertMySQLCDCBinlogValue(tc.value, tc.col))
		})
	}
}

func TestConvertMySQLCDCValueNormalizesZeroDates(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		col   schema.Column
		want  interface{}
	}{
		{"zero date string", "0000-00-00", schema.Column{Name: "d", DataType: schema.TypeDate}, nil},
		{"zero datetime string", "0000-00-00 00:00:00", schema.Column{Name: "dt", DataType: schema.TypeTimestamp}, nil},
		{"zero timestamp bytes", []byte("0000-00-00 00:00:00"), schema.Column{Name: "ts", DataType: schema.TypeTimestampTZ}, nil},
		{"driver zero time", time.Time{}, schema.Column{Name: "dt", DataType: schema.TypeTimestamp}, nil},
		{"valid date string", "2026-07-24", schema.Column{Name: "d", DataType: schema.TypeDate}, "2026-07-24"},
		{"zero-date-looking text column untouched", "0000-00-00", schema.Column{Name: "note", DataType: schema.TypeString}, "0000-00-00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, convertMySQLCDCValue(tc.value, tc.col))
		})
	}
	valid := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	assert.Equal(t, valid, convertMySQLCDCValue(valid, schema.Column{Name: "dt", DataType: schema.TypeTimestamp}))
}

func TestValidateMySQLCDCTimeValueRejectsOutOfRange(t *testing.T) {
	timeCol := schema.Column{Name: "elapsed", DataType: schema.TypeTime}
	require.NoError(t, validateMySQLCDCTimeValue("23:59:59", timeCol))
	require.NoError(t, validateMySQLCDCTimeValue("00:00:00", timeCol))
	require.NoError(t, validateMySQLCDCTimeValue([]byte("12:34:56.123456"), timeCol))
	require.NoError(t, validateMySQLCDCTimeValue(nil, timeCol))
	require.NoError(t, validateMySQLCDCTimeValue("838:59:59", schema.Column{Name: "note", DataType: schema.TypeString}))

	err := validateMySQLCDCTimeValue("838:59:59", timeCol)
	require.Error(t, err, "TIME above 24h must fail loudly instead of silently becoming NULL")
	assert.Contains(t, err.Error(), "elapsed")
	require.Error(t, validateMySQLCDCTimeValue("-12:34:56", timeCol))
	require.Error(t, validateMySQLCDCTimeValue([]byte("24:00:00"), timeCol))
}

func mysqlCDCTestSchema() *schema.TableSchema {
	return addMySQLCDCColumns(&schema.TableSchema{
		Name: "items",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
}

func TestMySQLRowsEventToChanges(t *testing.T) {
	tableSchema := mysqlCDCTestSchema()
	pos := gomysql.Position{Name: "mysql-bin.000001", Pos: 500}

	inserts, err := mysqlRowsEventToChanges(replication.EnumRowsEventTypeInsert, [][]interface{}{
		{int64(1), "a"},
		{int64(2), "b"},
	}, tableSchema, tableSchema, pos)
	require.NoError(t, err)
	require.Len(t, inserts, 2)
	assert.False(t, inserts[0].deleted)
	assert.Equal(t, []interface{}{int64(1), "a"}, inserts[0].values)
	assert.Less(t, inserts[0].lsn, inserts[1].lsn, "row sequence must keep LSNs ordered within an event")

	deletes, err := mysqlRowsEventToChanges(replication.EnumRowsEventTypeDelete, [][]interface{}{
		{int64(1), "a"},
	}, tableSchema, tableSchema, pos)
	require.NoError(t, err)
	require.Len(t, deletes, 1)
	assert.True(t, deletes[0].deleted)

	updates, err := mysqlRowsEventToChanges(replication.EnumRowsEventTypeUpdate, [][]interface{}{
		{int64(1), "a"},
		{int64(1), "a2"},
	}, tableSchema, tableSchema, pos)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.False(t, updates[0].deleted)
	assert.Equal(t, []interface{}{int64(1), "a2"}, updates[0].values)
}

func TestMySQLRowsEventToChangesPKChangeEmitsDeleteAndInsert(t *testing.T) {
	tableSchema := mysqlCDCTestSchema()
	pos := gomysql.Position{Name: "mysql-bin.000001", Pos: 500}

	changes, err := mysqlRowsEventToChanges(replication.EnumRowsEventTypeUpdate, [][]interface{}{
		{int64(1), "a"},
		{int64(9), "a"},
	}, tableSchema, tableSchema, pos)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	assert.True(t, changes[0].deleted, "old primary key must be tombstoned")
	assert.Equal(t, []interface{}{int64(1), "a"}, changes[0].values)
	assert.False(t, changes[1].deleted)
	assert.Equal(t, []interface{}{int64(9), "a"}, changes[1].values)
	assert.Less(t, changes[0].lsn, changes[1].lsn)
}

func TestMySQLRowsEventToChangesKeylessUpdateEmitsRetractPair(t *testing.T) {
	tableSchema := addMySQLCDCColumns(&schema.TableSchema{
		Name: "events",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: true},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	})
	pos := gomysql.Position{Name: "mysql-bin.000001", Pos: 500}

	changes, err := mysqlRowsEventToChanges(replication.EnumRowsEventTypeUpdate, [][]interface{}{
		{int64(1), "a"},
		{int64(1), "a2"},
	}, tableSchema, tableSchema, pos)
	require.NoError(t, err)
	require.Len(t, changes, 2, "keyless updates must land as delete+insert retract pairs")
	assert.True(t, changes[0].deleted)
	assert.Equal(t, []interface{}{int64(1), "a"}, changes[0].values)
	assert.False(t, changes[1].deleted)
	assert.Equal(t, []interface{}{int64(1), "a2"}, changes[1].values)
	assert.Less(t, changes[0].lsn, changes[1].lsn)
}

func TestMySQLRowsEventToChangesRejectsColumnCountMismatch(t *testing.T) {
	tableSchema := mysqlCDCTestSchema()
	pos := gomysql.Position{Name: "mysql-bin.000001", Pos: 500}

	_, err := mysqlRowsEventToChanges(replication.EnumRowsEventTypeInsert, [][]interface{}{
		{int64(1), "a", "extra-column"},
	}, tableSchema, tableSchema, pos)
	require.Error(t, err, "a wider row image means the table gained a column; positional mapping is unsafe")
	assert.Contains(t, err.Error(), "--full-refresh")

	_, err = mysqlRowsEventToChanges(replication.EnumRowsEventTypeInsert, [][]interface{}{
		{int64(1)},
	}, tableSchema, tableSchema, pos)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--full-refresh")
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

func TestValidateMySQLCDCSnapshotSchemaRejectsDiscoveryRace(t *testing.T) {
	expected := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	current := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "added", DataType: schema.TypeString},
	}}
	require.NoError(t, validateMySQLCDCSnapshotSchema(expected, expected, "items"))
	require.ErrorContains(t, validateMySQLCDCSnapshotSchema(expected, current, "items"), "--full-refresh")
}

func TestValidateMySQLCDCDiscoveredSchemasRejectsMultiTableRace(t *testing.T) {
	expected := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	src := &MySQLCDCSource{discoveredSchemas: map[string]*schema.TableSchema{"items": expected}}
	require.NoError(t, src.validateMySQLCDCDiscoveredSchemas([]source.SourceTableInfo{{Name: "items", Schema: cloneMySQLCDCTableSchema(expected)}}, nil))

	changed := cloneMySQLCDCTableSchema(expected)
	changed.Columns = append(changed.Columns, schema.Column{Name: "added", DataType: schema.TypeString})
	require.ErrorContains(t, src.validateMySQLCDCDiscoveredSchemas([]source.SourceTableInfo{{Name: "items", Schema: changed}}, nil), "--full-refresh")
	require.ErrorContains(t, src.validateMySQLCDCDiscoveredSchemas([]source.SourceTableInfo{
		{Name: "items", Schema: expected},
		{Name: "late", Schema: expected},
	}, nil), "appeared after")

	src.discoveredSchemas["ignored"] = cloneMySQLCDCTableSchema(expected)
	require.NoError(t, src.validateMySQLCDCDiscoveredSchemas(
		[]source.SourceTableInfo{{Name: "items", Schema: cloneMySQLCDCTableSchema(expected)}},
		[]string{"items"},
	))
	require.ErrorContains(t, src.validateMySQLCDCDiscoveredSchemas(nil, []string{"items"}), "no longer available")
}

func TestBeginMySQLConsistentSnapshotLocksAroundPosition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("SET time_zone = '\\+00:00'").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
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

func TestBeginMySQLConsistentSnapshotValidatesInventoryBeforeUnlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}))
	mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("items").AddRow("late"))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	src := &MySQLCDCSource{
		database:          "app",
		discoveredSchemas: map[string]*schema.TableSchema{"items": {}},
	}

	_, err = beginMySQLConsistentSnapshotWithValidation(ctx, conn, func(validationCtx context.Context, q mysqlCDCPositionQueryer) error {
		return src.validateMySQLCDCInventory(validationCtx, q, nil)
	})
	require.ErrorContains(t, err, "appeared after")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBeginMySQLConsistentSnapshotUnlocksOnPositionError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("SET time_zone = '\\+00:00'").
		WillReturnResult(sqlmock.NewResult(0, 0))
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

func TestBeginMySQLConsistentSnapshotRejectsPreparedXA(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("SET time_zone = '\\+00:00'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("FLUSH TABLES WITH READ LOCK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("START TRANSACTION WITH CONSISTENT SNAPSHOT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW BINARY LOG STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"File", "Position"}).AddRow("mysql-bin.000012", "345"))
	mock.ExpectQuery("XA RECOVER").
		WillReturnRows(sqlmock.NewRows([]string{"formatID", "gtrid_length", "bqual_length", "data"}).AddRow(1, 3, 0, []byte("xid")))
	mock.ExpectExec("UNLOCK TABLES").WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = beginMySQLConsistentSnapshot(ctx, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepared XA")
	require.NoError(t, mock.ExpectationsWereMet())
}

type mysqlCDCTestStreamer func(context.Context) (*replication.BinlogEvent, error)

func (f mysqlCDCTestStreamer) GetEvent(ctx context.Context) (*replication.BinlogEvent, error) {
	return f(ctx)
}

func TestReadMySQLCDCEventClassifiesOnlyActualPollDeadlines(t *testing.T) {
	readerErr := errors.New("replication stream closed")
	_, err, eventCtxErr := readMySQLCDCEvent(context.Background(), mysqlCDCTestStreamer(func(context.Context) (*replication.BinlogEvent, error) {
		return nil, readerErr
	}))
	require.ErrorIs(t, err, readerErr)
	require.NoError(t, eventCtxErr)
	assert.False(t, isMySQLCDCReadPollTimeout(context.Background(), err, eventCtxErr))

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	_, err, eventCtxErr = readMySQLCDCEvent(parent, mysqlCDCTestStreamer(func(ctx context.Context) (*replication.BinlogEvent, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, isMySQLCDCReadPollTimeout(parent, err, eventCtxErr))

	_, err, eventCtxErr = readMySQLCDCEvent(context.Background(), mysqlCDCTestStreamer(func(ctx context.Context) (*replication.BinlogEvent, error) {
		<-ctx.Done()
		return nil, readerErr
	}))
	require.ErrorIs(t, err, readerErr)
	require.ErrorIs(t, eventCtxErr, context.DeadlineExceeded)
	assert.False(t, isMySQLCDCReadPollTimeout(context.Background(), err, eventCtxErr))

	assert.True(t, isMySQLCDCReadPollTimeout(context.Background(), context.DeadlineExceeded, context.DeadlineExceeded))
}

func TestMySQLBinlogEventsPreservesCompressedTransactionOrder(t *testing.T) {
	start := &replication.BinlogEvent{Event: &replication.QueryEvent{Query: []byte("XA START X'41',X'',1")}}
	rows := &replication.BinlogEvent{Event: &replication.RowsEvent{Rows: [][]interface{}{{int64(1)}}}}
	prepare := mysqlCDCTestXAPrepareEvent(t, "A", "", 1, true)
	payload := &replication.BinlogEvent{Event: &replication.TransactionPayloadEvent{Events: []*replication.BinlogEvent{start, rows, prepare}}}

	events, err := mysqlBinlogEvents(payload)
	require.NoError(t, err)
	require.Equal(t, []*replication.BinlogEvent{start, rows, prepare}, events)
}

func TestValidateMySQLCDCFullRowImageDistinguishesNullFromOmitted(t *testing.T) {
	full := &replication.RowsEvent{
		ColumnCount:    3,
		Rows:           [][]interface{}{{int64(1), nil, "value"}},
		SkippedColumns: [][]int{{}},
	}
	require.NoError(t, validateMySQLCDCFullRowImage(full, 3), "an explicit SQL NULL in a full image is valid")

	omitted := &replication.RowsEvent{
		ColumnCount:    3,
		Rows:           [][]interface{}{{int64(1), nil, "value"}},
		SkippedColumns: [][]int{{1}},
	}
	err := validateMySQLCDCFullRowImage(omitted, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binlog_row_image=FULL")
}

func TestMySQLCDCXIDCanonicalizationAndPrepare(t *testing.T) {
	action, quotedID, continuation, err := mysqlCDCXAQuery("XA START 'CaseSensitive', X'6272616E6368', 17 RESUME")
	require.NoError(t, err)
	assert.Equal(t, mysqlCDCXAActionStart, action)
	assert.True(t, continuation)

	action, hexID, onePhase, err := mysqlCDCXAQuery("XA COMMIT X'4361736553656e736974697665',X'6272616e6368',17 ONE PHASE")
	require.NoError(t, err)
	assert.Equal(t, mysqlCDCXAActionCommit, action)
	assert.True(t, onePhase)
	assert.Equal(t, quotedID, hexID)

	prepare := mysqlCDCTestXAPrepareEvent(t, "CaseSensitive", "branch", 17, true)
	prepareID, prepareOnePhase, err := mysqlCDCXAPrepare(prepare)
	require.NoError(t, err)
	assert.True(t, prepareOnePhase)
	assert.Equal(t, quotedID, prepareID)

	_, lowerID, _, err := mysqlCDCXAQuery("XA START 'casesensitive','branch',17")
	require.NoError(t, err)
	assert.NotEqual(t, quotedID, lowerID)

	for _, formatID := range []uint32{1 << 31, 1<<32 - 1} {
		query := fmt.Sprintf("XA START 'boundary','',%d", formatID)
		_, queryID, _, err := mysqlCDCXAQuery(query)
		require.NoError(t, err)
		prepareID, _, err := mysqlCDCXAPrepare(mysqlCDCTestXAPrepareEvent(t, "boundary", "", formatID, false))
		require.NoError(t, err)
		assert.Equal(t, queryID, prepareID)
	}
}

func TestMySQLCDCXAQueryAcceptsWhitespaceAndComments(t *testing.T) {
	action, xaID, _, err := mysqlCDCXAQuery("/* leading */ XA\t/* inter-token */ START\n/* before XID */ 'spaced-xid'")
	require.NoError(t, err)
	require.Equal(t, mysqlCDCXAActionStart, action)
	require.NotEmpty(t, xaID)

	action, committedID, onePhase, err := mysqlCDCXAQuery("XA  COMMIT /* before XID */ 'spaced-xid'   ONE PHASE;")
	require.NoError(t, err)
	require.Equal(t, mysqlCDCXAActionCommit, action)
	require.Equal(t, xaID, committedID)
	require.True(t, onePhase)

	action, committedID, onePhase, err = mysqlCDCXAQuery("XA COMMIT 'spaced-xid' /* after XID */ ONE /* between modifier */ PHASE /* trailing */;")
	require.NoError(t, err)
	require.Equal(t, mysqlCDCXAActionCommit, action)
	require.Equal(t, xaID, committedID)
	require.True(t, onePhase)
}

func TestMySQLCDCChangesSizeAccountsForLargePayloads(t *testing.T) {
	require.Greater(t, mysqlCDCValueSize("x"), uint64(1))
	require.Greater(t, mysqlCDCValueSize([]byte{1}), uint64(1))
	require.Equal(t, ^uint64(0), saturatingMySQLCDCSizeMul(^uint64(0), 2))

	small := mysqlCDCChangesSize([]mysqlCDCChange{{values: []interface{}{int64(1), "small"}, lsn: "lsn"}})
	large := mysqlCDCChangesSize([]mysqlCDCChange{{values: []interface{}{int64(1), strings.Repeat("x", 1024*1024)}, lsn: "lsn"}})
	require.Greater(t, large, small+uint64(1024*1024-16))

	smallDecimal := decimal.RequireFromString("12345.67")
	largeDecimal := decimal.RequireFromString(strings.Repeat("9", 65))
	small = mysqlCDCChangesSize([]mysqlCDCChange{{values: []interface{}{smallDecimal}, lsn: "lsn"}})
	large = mysqlCDCChangesSize([]mysqlCDCChange{{values: []interface{}{largeDecimal}, lsn: "lsn"}})
	require.Equal(t, small, large)
	require.Equal(t, uint64(maxMySQLCDCDecodedDecimalSize), mysqlCDCValueSize(smallDecimal))

	spareCapacity := make([]mysqlCDCChange, 1, 1024)
	require.Greater(
		t,
		mysqlCDCXAChunkSize(spareCapacity),
		uint64(cap(spareCapacity))*uint64(unsafe.Sizeof(mysqlCDCChange{})),
	)
	require.Greater(t, mysqlCDCXATransactionSize("xid"), uint64(len("xid")))
	require.Greater(t, mysqlCDCXATableSize("table"), uint64(len("table")))
}

func TestConvertMySQLCDCValueDetachesDecodedStrings(t *testing.T) {
	backing := strings.Repeat("x", 1024*1024)
	decoded := backing[:5]
	converted := convertMySQLCDCValue(decoded, schema.Column{DataType: schema.TypeString}).(string)
	require.Equal(t, decoded, converted)
	require.NotEqual(t, uintptr(unsafe.Pointer(unsafe.StringData(decoded))), uintptr(unsafe.Pointer(unsafe.StringData(converted))))
}

func TestProjectMySQLCDCRowRejectsPartialJSON(t *testing.T) {
	columns := []schema.Column{{Name: "payload", DataType: schema.TypeJSON}}
	_, err := projectMySQLCDCRow(
		[]interface{}{&replication.JsonDiff{Path: "$.large", Value: strings.Repeat("x", 1024)}},
		columns,
		columns,
		[]int{0},
	)
	require.ErrorContains(t, err, "partial JSON")
}

func mysqlCDCTestXAPrepareEvent(t *testing.T, gtrid, bqual string, formatID uint32, onePhase bool) *replication.BinlogEvent {
	t.Helper()
	data := make([]byte, 13+len(gtrid)+len(bqual))
	if onePhase {
		data[0] = 1
	}
	binary.LittleEndian.PutUint32(data[1:5], uint32(formatID))
	binary.LittleEndian.PutUint32(data[5:9], uint32(len(gtrid)))
	binary.LittleEndian.PutUint32(data[9:13], uint32(len(bqual)))
	copy(data[13:], gtrid)
	copy(data[13+len(gtrid):], bqual)
	return &replication.BinlogEvent{
		Header: &replication.EventHeader{EventType: replication.XA_PREPARE_LOG_EVENT},
		Event:  &replication.GenericEvent{Data: data},
	}
}

func TestMySQLCDCQueryDDLScoping(t *testing.T) {
	meta := mysqlCDCTableMetadata{Name: "items", SourceSchema: "app", SourceName: "items"}
	exchange := mysqlCDCTableMetadata{Name: "exchange_items", SourceSchema: "app", SourceName: "exchange_items"}
	aliases := map[string]mysqlCDCTableMetadata{
		"items":              meta,
		"app.items":          meta,
		"exchange_items":     exchange,
		"app.exchange_items": exchange,
	}

	truncated, err := mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("/* audit */ TRUNCATE TABLE `app`.`items`")}, "app", aliases)
	require.NoError(t, err)
	require.Len(t, truncated, 1)
	assert.Equal(t, "items", truncated[0].Name)

	truncated, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("/* audit */ /*!80000 TRUNCATE TABLE `app`.`items` */")}, "app", aliases)
	require.NoError(t, err)
	require.Len(t, truncated, 1)
	assert.Equal(t, "items", truncated[0].Name)

	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("ALTER TABLE other EXCHANGE PARTITION p0 WITH TABLE exchange_items")}, "app", aliases)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full-refresh")

	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Query: []byte("CREATE DATABASE unrelated")}, "app", aliases)
	require.NoError(t, err)
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Query: []byte("CREATE EVENT unrelated.cleanup ON SCHEDULE EVERY 1 DAY DO DELETE FROM unrelated.logs")}, "app", aliases)
	require.NoError(t, err)
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("CREATE DEFINER = 'svc'@'%' TRIGGER items BEFORE INSERT ON items FOR EACH ROW SET NEW.id = NEW.id")}, "app", aliases)
	require.NoError(t, err, "routine DDL must not be mistaken for captured table DDL when its object name collides")
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("CREATE OR REPLACE DEFINER='svc'@'%' VIEW items AS SELECT 1 AS id")}, "app", aliases)
	require.NoError(t, err)
	require.False(t, mysqlCDCIsKnownNonTableDDL("CREATE SPATIAL INDEX idx ON items (id)"))
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("ALTER /* VIEW */ TABLE items ADD COLUMN added INT")}, "app", aliases)
	require.ErrorContains(t, err, "full-refresh", "comments must not disguise captured table DDL")
	require.True(t, mysqlCDCIsKnownNonTableDDL(normalizeMySQLCDCQuery("CREATE /*!80000 OR REPLACE */ /* object */ VIEW items AS SELECT 1")))
	truncated, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("/*M!100100 TRUNCATE TABLE items */")}, "app", aliases)
	require.NoError(t, err)
	require.Len(t, truncated, 1)
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("CREATE TABLE newly_discovered (id BIGINT NOT NULL PRIMARY KEY)")}, "app", aliases)
	require.NoError(t, err)
	newMeta := mysqlCDCTableMetadata{Name: "newly_discovered", SourceSchema: "app", SourceName: "newly_discovered"}
	aliases["newly_discovered"] = newMeta
	aliases["app.newly_discovered"] = newMeta
	createEvent := &replication.QueryEvent{Schema: []byte("app"), Query: []byte("CREATE TABLE newly_discovered (id BIGINT NOT NULL PRIMARY KEY)")}
	_, err = mysqlCDCQueryTruncatesAfter(
		createEvent, "app", aliases,
		gomysql.Position{Name: "mysql-bin.000001", Pos: 100},
		map[string]gomysql.Position{"newly_discovered": {Name: "mysql-bin.000001", Pos: 200}},
	)
	require.NoError(t, err, "DDL covered by the table's initial snapshot must be ignored")
	_, err = mysqlCDCQueryTruncatesAfter(
		createEvent, "app", aliases,
		gomysql.Position{Name: "mysql-bin.000001", Pos: 300},
		map[string]gomysql.Position{"newly_discovered": {Name: "mysql-bin.000001", Pos: 200}},
	)
	require.ErrorContains(t, err, "full-refresh", "DDL after the snapshot boundary must fail closed")

	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Query: []byte("DROP DATABASE app")}, "app", aliases)
	require.Error(t, err)
}

func TestMySQLCDCQueryUnparseableDDLScoping(t *testing.T) {
	meta := mysqlCDCTableMetadata{Name: "items", SourceSchema: "app", SourceName: "items"}
	aliases := map[string]mysqlCDCTableMetadata{
		"items":     meta,
		"app.items": meta,
	}

	// Valid server DDL the vendored parser rejects must be skipped when it
	// cannot involve captured tables, instead of wedging the stream.
	_, err := mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("report"), Query: []byte("CREATE TABLE tmp (id SERIAL PRIMARY KEY)")}, "app", aliases)
	require.NoError(t, err)
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("CREATE SEQUENCE seq_orders START WITH 1")}, "app", aliases)
	require.NoError(t, err)

	// Unparseable DDL naming a captured table or the captured database still
	// fails closed.
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("ALTER TABLE items SECONDARY_LOAD")}, "app", aliases)
	require.ErrorContains(t, err, "failed to parse MySQL DDL event")
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("report"), Query: []byte("CREATE TABLE app.tmp (id SERIAL PRIMARY KEY)")}, "app", aliases)
	require.ErrorContains(t, err, "failed to parse MySQL DDL event")
	_, err = mysqlCDCQueryTruncates(&replication.QueryEvent{Schema: []byte("app"), Query: []byte("CREATE TABLE `items_archive` (id SERIAL)")}, "app", aliases)
	require.NoError(t, err, "identifiers merely containing a captured name must not fail closed")
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
