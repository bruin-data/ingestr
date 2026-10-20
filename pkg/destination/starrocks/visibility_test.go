package starrocks

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

const insertTransactionQuery = "SELECT get_json_int(RUNTIME_DETAILS, '$.txn_id') FROM information_schema.loads WHERE DB_NAME = ? AND LABEL = ?"

func newVisibilityTestDestination(t *testing.T, matchers ...sqlmock.QueryMatcher) (*StarRocksDestination, sqlmock.Sqlmock) {
	t.Helper()
	matcher := sqlmock.QueryMatcherRegexp
	if len(matchers) > 0 {
		matcher = matchers[0]
	}
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	return &StarRocksDestination{db: db, database: "testdb"}, mock
}

func expectTransactionDatabase(mock sqlmock.Sqlmock, database string) {
	mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions'")).
		WillReturnRows(sqlmock.NewRows([]string{"DbName", "Extra", "DbId"}).
			AddRow(database+"_other", "ignored", "999").
			AddRow(database, "ignored", "123")).
		RowsWillBeClosed()
}

func TestInsertTransactionIDModern(t *testing.T) {
	permission := &mysql.MySQLError{Number: 1142, Message: "SELECT command denied"}
	unrelated := &mysql.MySQLError{Number: 1064, Message: "Column 'LABEL' cannot be resolved"}
	wrongCode := &mysql.MySQLError{Number: 1142, Message: "Access denied to DB_NAME"}
	for _, tc := range []struct {
		name  string
		value driver.Value
		err   error
	}{
		{name: "valid", value: int64(42)},
		{name: "null"},
		{name: "zero", value: int64(0)},
		{name: "negative", value: int64(-1)},
		{name: "malformed", value: "invalid"},
		{name: "no rows", err: sql.ErrNoRows},
		{name: "permission denied", err: permission},
		{name: "unrelated missing column", err: unrelated},
		{name: "wrong error code", err: wrongCode},
		{name: "non mysql error", err: errors.New("Column 'DB_NAME' cannot be resolved")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			query := mock.ExpectQuery(regexp.QuoteMeta(insertTransactionQuery)).WithArgs("testdb", "insert_events")
			if tc.err != nil {
				query.WillReturnError(tc.err)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"txn_id"}).AddRow(tc.value))
			}
			id, err := d.insertTransactionID(t.Context(), "testdb", "insert_events")
			if tc.name == "valid" {
				require.NoError(t, err)
				require.Equal(t, int64(42), id)
			} else {
				require.Error(t, err)
				if tc.err != nil {
					require.ErrorIs(t, err, tc.err)
				}
			}
			require.False(t, d.legacyLoadMetadata.Load())
		})
	}
}

func TestInsertTransactionIDLegacy(t *testing.T) {
	for _, tc := range []struct {
		name, database, location string
		missing                  error
	}{
		{"running", "testdb", "running", &mysql.MySQLError{Number: 1064, Message: "Getting analyzing error. Detail message: Column 'DB_NAME' cannot be resolved."}},
		{"finished", "testdb", "finished", &mysql.MySQLError{Number: 1054, Message: "Unknown column 'RUNTIME_DETAILS' in 'field list'"}},
		{"numeric database name", "456", "running", &mysql.MySQLError{Number: 1054, Message: "Unknown column 'DB_NAME' in 'where clause'"}},
		{"wrapped mysql error", "testdb", "running", fmt.Errorf("query metadata: %w", &mysql.MySQLError{Number: 1064, Message: "Column 'DB_NAME' cannot be resolved"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			mock.ExpectQuery(regexp.QuoteMeta(insertTransactionQuery)).
				WithArgs(tc.database, "insert_events").WillReturnError(tc.missing)
			expectTransactionDatabase(mock, tc.database)
			rows := sqlmock.NewRows([]string{"Extra", "lAbEl", "tRaNsAcTiOnId"}).
				AddRow("ignored", "INSERT_EVENTS", "500").
				AddRow("ignored", "insert_events_suffix", "600")
			running := mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions/123/running'"))
			if tc.location == "finished" {
				running.WillReturnRows(rows).RowsWillBeClosed()
				rows = sqlmock.NewRows([]string{"Extra", "lAbEl", "tRaNsAcTiOnId"})
				mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions/123/finished'")).
					WillReturnRows(rows.AddRow("ignored", "insert_events", "42")).RowsWillBeClosed()
			} else {
				running.WillReturnRows(rows.AddRow("ignored", "insert_events", "42")).RowsWillBeClosed()
			}
			id, err := d.insertTransactionID(t.Context(), tc.database, "insert_events")
			require.NoError(t, err)
			require.Equal(t, int64(42), id)
			require.True(t, d.legacyLoadMetadata.Load())
		})
	}
}

func TestInsertTransactionIDCachesLegacyMetadata(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	mock.ExpectQuery(regexp.QuoteMeta(insertTransactionQuery)).WithArgs("testdb", "first").
		WillReturnError(&mysql.MySQLError{Number: 1064, Message: "Column 'DB_NAME' cannot be resolved"})
	for _, label := range []string{"first", "second"} {
		expectTransactionDatabase(mock, "testdb")
		mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions/123/running'")).
			WillReturnRows(sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow("42", label))
		id, err := d.insertTransactionID(t.Context(), "testdb", label)
		require.NoError(t, err)
		require.Equal(t, int64(42), id)
	}
}

func TestInsertTransactionIDLegacyFailsClosed(t *testing.T) {
	permission := &mysql.MySQLError{Number: 1227, Message: "Access denied; ADMIN privilege required"}
	readFailure := errors.New("connection lost while reading transaction history")
	for _, tc := range []struct {
		name string
		rows *sqlmock.Rows
		err  error
	}{
		{name: "permission denied", err: permission},
		{name: "missing transaction id", rows: sqlmock.NewRows([]string{"Label"}).AddRow("insert_events")},
		{name: "missing label", rows: sqlmock.NewRows([]string{"TransactionId"}).AddRow("42")},
		{name: "null transaction id", rows: sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow(nil, "insert_events")},
		{name: "zero transaction id", rows: sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow("0", "insert_events")},
		{name: "negative transaction id", rows: sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow("-1", "insert_events")},
		{name: "invalid transaction id", rows: sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow("invalid", "insert_events")},
		{name: "row read error", rows: sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow("42", "insert_events").RowError(0, readFailure)},
		{name: "no matching label", rows: sqlmock.NewRows([]string{"TransactionId", "Label"}).AddRow("42", "another_insert")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			d.legacyLoadMetadata.Store(true)
			expectTransactionDatabase(mock, "testdb")
			query := mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions/123/running'"))
			if tc.err != nil {
				query.WillReturnError(tc.err)
			} else {
				query.WillReturnRows(tc.rows).RowsWillBeClosed()
			}
			if tc.name == "no matching label" {
				mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions/123/finished'")).
					WillReturnRows(sqlmock.NewRows([]string{"TransactionId", "Label"}))
			}
			_, err := d.insertTransactionID(t.Context(), "testdb", "insert_events")
			require.Error(t, err)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			if tc.name == "no matching label" {
				require.ErrorIs(t, err, sql.ErrNoRows)
			}
		})
	}
}

func TestInsertTransactionIDLegacyMissingDatabase(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	d.legacyLoadMetadata.Store(true)
	mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions'")).
		WillReturnRows(sqlmock.NewRows([]string{"DbId", "DbName"}).AddRow("123", "testdb_other"))
	_, err := d.insertTransactionID(t.Context(), "testdb", "insert_events")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestInsertVisibleWaitsForPublication(t *testing.T) {
	labelPattern := regexp.MustCompile("WITH LABEL `([^`]+)`")
	for _, tc := range []struct {
		name   string
		states []string
		legacy bool
	}{
		{"modern committed then visible", []string{"COMMITTED", "VISIBLE"}, false},
		{"modern aborted", []string{"ABORTED"}, false},
		{"legacy committed then visible", []string{"COMMITTED", "VISIBLE"}, true},
		{"legacy aborted", []string{"ABORTED"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transactions := sqlmock.NewRows([]string{"TransactionId", "Label"})
			matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				if err := sqlmock.QueryMatcherRegexp.Match(expected, actual); err != nil {
					return err
				}
				if match := labelPattern.FindStringSubmatch(actual); match != nil {
					transactions.AddRow("42", match[1])
				}
				return nil
			})
			d, mock := newVisibilityTestDestination(t, matcher)
			d.legacyLoadMetadata.Store(tc.legacy)
			mock.ExpectExec("INSERT INTO `testdb`.`events` WITH LABEL `ingestr_events_[a-z0-9]+_[0-9]+_[0-9]+` SELECT 1").
				WillReturnResult(sqlmock.NewResult(0, 1))
			if tc.legacy {
				expectTransactionDatabase(mock, "testdb")
				mock.ExpectQuery(regexp.QuoteMeta("SHOW PROC '/transactions/123/running'")).
					WillReturnRows(transactions)
			} else {
				mock.ExpectQuery(regexp.QuoteMeta(insertTransactionQuery)).WithArgs("testdb", sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"txn_id"}).AddRow(int64(42)))
			}
			for _, state := range tc.states {
				mock.ExpectQuery(regexp.QuoteMeta("SHOW TRANSACTION FROM `testdb` WHERE ID = 42")).
					WillReturnRows(sqlmock.NewRows([]string{"TransactionStatus"}).AddRow(state))
			}
			err := d.insertVisible(t.Context(), "testdb.events", "SELECT 1")
			if tc.states[len(tc.states)-1] == "ABORTED" {
				require.ErrorContains(t, err, "aborted")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
