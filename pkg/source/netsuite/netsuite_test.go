package netsuite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURIWithRawODBCConnectionString(t *testing.T) {
	cfg, err := parseURI("netsuite+odbc://?odbc_connect_string=DSN%3DNetSuite2%3BUID%3Duser%3BPWD%3Dsecret%3B")
	require.NoError(t, err)

	assert.Equal(t, "DSN=NetSuite2;UID=user;PWD=secret;", cfg.connString)
}

func TestParseURIWithDSN(t *testing.T) {
	cfg, err := parseURI("netsuite://?dsn=NetSuite2&username=user@example.com&password=secret&account_id=123456_SB1&role_id=57")
	require.NoError(t, err)

	assert.Equal(t, "DSN=NetSuite2;UID=user@example.com;PWD=secret;CustomProperties={AccountID=123456_SB1;RoleID=57};", cfg.connString)
}

func TestParseURIWithDriverConnectionString(t *testing.T) {
	cfg, err := parseURI("netsuite://123456_SB1?driver=NetSuite+ODBC+Drivers+64bit&role_id=57&username=user@example.com&password=secret&static_schema=true&custom_properties=ApplicationName%3Dingestr")
	require.NoError(t, err)

	assert.Equal(t, "DRIVER={NetSuite ODBC Drivers 64bit};Host=123456-sb1.connect.api.netsuite.com;Port=1708;Encrypted=1;AllowSinglePacketLogout=1;SDSN=NetSuite2.com;UID=user@example.com;PWD=secret;CustomProperties={AccountID=123456_SB1;RoleID=57;StaticSchema=1;ApplicationName=ingestr};", cfg.connString)
}

func TestParseURIWithHostOverrideAndUserInfo(t *testing.T) {
	cfg, err := parseURI("netsuite+odbc://user:secret@example.connect.api.netsuite.com:1709?driver=%7BNetSuite+ODBC+Drivers+64bit%7D&server_data_source=NetSuite.com&account_id=123456&role_id=3&encrypted=ssl")
	require.NoError(t, err)

	assert.Equal(t, "DRIVER={NetSuite ODBC Drivers 64bit};Host=example.connect.api.netsuite.com;Port=1709;Encrypted=ssl;AllowSinglePacketLogout=1;SDSN=NetSuite.com;UID=user;PWD=secret;CustomProperties={AccountID=123456;RoleID=3};", cfg.connString)
}

func TestParseURIErrors(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{"wrong scheme", "https://123456?username=u&password=p&dsn=x"},
		{"missing dsn or driver", "netsuite://123456?role_id=57&username=u&password=p"},
		{"missing account or host", "netsuite://?driver=NetSuite&role_id=57&username=u&password=p"},
		{"missing role", "netsuite://123456?driver=NetSuite&username=u&password=p"},
		{"bad port", "netsuite://123456?driver=NetSuite&role_id=57&username=u&password=p&port=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseURI(tt.uri)
			require.Error(t, err)
		})
	}
}

func TestBuildSuiteAnalyticsQuery(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	end := time.Date(2026, 1, 3, 3, 4, 5, 987654321, time.UTC)

	got := buildSuiteAnalyticsQuery("transaction", source.ReadOptions{
		IncrementalKey: "lastmodifieddate",
		IntervalStart:  &start,
		IntervalEnd:    &end,
	})

	assert.Equal(t, "SELECT * FROM transaction WHERE lastmodifieddate >= TO_TIMESTAMP('2026-01-02 03:04:05.123456789', 'YYYY-MM-DD HH24:MI:SSxFF') AND lastmodifieddate < TO_TIMESTAMP('2026-01-03 03:04:05.987654321', 'YYYY-MM-DD HH24:MI:SSxFF') ORDER BY lastmodifieddate ASC", got)
}

func TestNetSuiteSourceGetTable(t *testing.T) {
	s := NewNetSuiteSource()

	table, err := s.GetTable(context.Background(), source.TableRequest{
		Name:           "customer",
		IncrementalKey: "lastmodifieddate",
		Strategy:       config.StrategyMerge,
	})
	require.NoError(t, err)

	assert.Equal(t, "customer", table.Name())
	assert.Equal(t, []string{"id"}, table.PrimaryKeys())
	assert.Equal(t, "lastmodifieddate", table.IncrementalKey())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
	assert.False(t, table.HasKnownSchema())
}

func TestNetSuiteSourceReadWithODBCDB(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mockRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow("1", "Acme").
		AddRow("2", "Globex").
		AddRow("3", "Umbrella")
	mock.ExpectQuery("SELECT * FROM customer").WillReturnRows(mockRows)

	s := &NetSuiteSource{db: db}
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "customer"})
	require.NoError(t, err)

	ch, err := table.Read(context.Background(), source.ReadOptions{PageSize: 2})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 2)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
	assert.True(t, hasColumn(batches[0], "id"))
	assert.True(t, hasColumn(batches[0], "name"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNetSuiteSourceReadQueryError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT * FROM customer").WillReturnError(sql.ErrConnDone)

	s := &NetSuiteSource{db: db}
	ch, err := s.readTable(context.Background(), "customer", source.ReadOptions{})
	require.NoError(t, err)

	var gotErr error
	for result := range ch {
		gotErr = result.Err
	}
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "failed to query NetSuite SuiteAnalytics Connect")
	require.NoError(t, mock.ExpectationsWereMet())
}

func collectResults(t *testing.T, ch <-chan source.RecordBatchResult) []source.RecordBatchResult {
	t.Helper()

	var results []source.RecordBatchResult
	for result := range ch {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			results = append(results, result)
		}
	}
	return results
}

func hasColumn(result source.RecordBatchResult, name string) bool {
	for i := 0; i < int(result.Batch.NumCols()); i++ {
		if result.Batch.ColumnName(i) == name {
			return true
		}
	}
	return false
}
