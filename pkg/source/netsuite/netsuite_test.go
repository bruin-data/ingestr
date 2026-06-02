package netsuite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

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

func TestTBATokenPassword(t *testing.T) {
	// Known-answer test. The expected signature was computed independently with:
	//   printf '%s' '1234567&ck&tid&abc123&1700000000' | \
	//     openssl dgst -sha256 -hmac 'cs&ts' -binary | openssl base64
	setTBAClock(t, "abc123", 1700000000)

	creds := tbaCredentials{
		accountID:      "1234567",
		consumerKey:    "ck",
		consumerSecret: "cs",
		tokenID:        "tid",
		tokenSecret:    "ts",
	}

	pw, err := creds.tokenPassword()
	require.NoError(t, err)
	assert.Equal(t, "1234567&ck&tid&abc123&1700000000&xblpoRO5sCUEbB0LmlwAMMjUZnl15wLPuzbOjch9dC4=&HMAC-SHA256", pw)
}

func TestParseURIWithTBAOverDriver(t *testing.T) {
	cfg, err := parseURI("netsuite://123456_SB1?driver=NetSuite+ODBC+Drivers+64bit&role_id=57&consumer_key=ck&consumer_secret=cs&token_id=tid&token_secret=ts")
	require.NoError(t, err)

	require.NotNil(t, cfg.tba)
	assert.Equal(t, tbaCredentials{
		accountID:      "123456_SB1",
		consumerKey:    "ck",
		consumerSecret: "cs",
		tokenID:        "tid",
		tokenSecret:    "ts",
	}, *cfg.tba)

	// The base connection string carries everything except the credentials;
	// UID=TBA and the per-connection token password are appended at connect time.
	assert.Equal(t, "DRIVER={NetSuite ODBC Drivers 64bit};Host=123456-sb1.connect.api.netsuite.com;Port=1708;Encrypted=1;AllowSinglePacketLogout=1;SDSN=NetSuite2.com;CustomProperties={AccountID=123456_SB1;RoleID=57};", cfg.connString)
	assert.NotContains(t, cfg.connString, "UID=")
	assert.NotContains(t, cfg.connString, "PWD=")
}

func TestParseURIWithTBAOverDSN(t *testing.T) {
	cfg, err := parseURI("netsuite://?dsn=NetSuite&account_id=123456&role_id=57&client_id=ck&client_secret=cs&token=tid&token_secret=ts")
	require.NoError(t, err)

	require.NotNil(t, cfg.tba)
	assert.Equal(t, "DSN=NetSuite;CustomProperties={AccountID=123456;RoleID=57};", cfg.connString)
}

func TestParseURITBAResolvesAccountAndRoleFromDSN(t *testing.T) {
	// DSN supplies AccountID/RoleID (as a configured odbc.ini would); the URI
	// only carries the token values.
	prev := dsnCustomProperties
	dsnCustomProperties = func(dsn string) map[string]string {
		require.Equal(t, "NetSuite", dsn)
		return map[string]string{"AccountID": "123456", "RoleID": "57"}
	}
	t.Cleanup(func() { dsnCustomProperties = prev })

	cfg, err := parseURI("netsuite://?dsn=NetSuite&consumer_key=ck&consumer_secret=cs&token_id=tid&token_secret=ts")
	require.NoError(t, err)

	require.NotNil(t, cfg.tba)
	assert.Equal(t, "123456", cfg.tba.accountID)
	assert.Equal(t, "DSN=NetSuite;CustomProperties={AccountID=123456;RoleID=57};", cfg.connString)
}

func TestParseURITBAURIOverridesDSN(t *testing.T) {
	prev := dsnCustomProperties
	dsnCustomProperties = func(string) map[string]string {
		return map[string]string{"AccountID": "9999999", "RoleID": "3"}
	}
	t.Cleanup(func() { dsnCustomProperties = prev })

	cfg, err := parseURI("netsuite://?dsn=NetSuite&account_id=123456&role_id=57&consumer_key=ck&consumer_secret=cs&token_id=tid&token_secret=ts")
	require.NoError(t, err)
	require.NotNil(t, cfg.tba)
	assert.Equal(t, "123456", cfg.tba.accountID)
	assert.Equal(t, "DSN=NetSuite;CustomProperties={AccountID=123456;RoleID=57};", cfg.connString)
}

func TestParseINICustomProperties(t *testing.T) {
	ini := `[ODBC Data Sources]
NetSuite=NetSuite ODBC Drivers 8.1

[NetSuite]
Driver=/opt/netsuite/odbcclient/lib64/ivoa27.so
Host=123456.connect.api.netsuite.com
CustomProperties=AccountID=123456;RoleID=57

[Other]
CustomProperties=AccountID=1;RoleID=2
`
	props := parseINICustomProperties(ini, "NetSuite")
	assert.Equal(t, "123456", props["AccountID"])
	assert.Equal(t, "57", props["RoleID"])

	assert.Nil(t, parseINICustomProperties(ini, "Missing"))
}

func TestParseURITBAErrors(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{"missing token_secret", "netsuite://123456?driver=NetSuite&role_id=57&consumer_key=ck&consumer_secret=cs&token_id=tid"},
		{"missing consumer_secret", "netsuite://123456?driver=NetSuite&role_id=57&consumer_key=ck&token_id=tid&token_secret=ts"},
		{"missing account", "netsuite://?driver=NetSuite&host=example.connect.api.netsuite.com&role_id=57&consumer_key=ck&consumer_secret=cs&token_id=tid&token_secret=ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseURI(tt.uri)
			require.Error(t, err)
		})
	}
}

func TestTBAConnectorRegeneratesPerConnection(t *testing.T) {
	setTBAClockSeq(t)

	rec := &recordingDriver{}
	c := &tbaConnector{
		drv:  rec,
		base: "DSN=NetSuite;CustomProperties={AccountID=123456;RoleID=57};",
		creds: tbaCredentials{
			accountID:      "123456",
			consumerKey:    "ck",
			consumerSecret: "cs",
			tokenID:        "tid",
			tokenSecret:    "ts",
		},
	}

	_, err := c.Connect(context.Background())
	require.NoError(t, err)
	_, err = c.Connect(context.Background())
	require.NoError(t, err)

	require.Len(t, rec.dsns, 2)
	assert.Contains(t, rec.dsns[0], "DSN=NetSuite;")
	assert.Contains(t, rec.dsns[0], "UID=TBA;PWD=123456&ck&tid&")
	// A fresh nonce per physical connection produces a distinct token password,
	// honouring NetSuite's single-use token password requirement.
	assert.NotEqual(t, rec.dsns[0], rec.dsns[1])
}

// setTBAClock pins the nonce and timestamp used when computing a token password.
func setTBAClock(t *testing.T, nonce string, timestamp int64) {
	t.Helper()
	prevNonce, prevTime := tbaNonceFunc, tbaTimeFunc
	tbaNonceFunc = func() (string, error) { return nonce, nil }
	tbaTimeFunc = func() int64 { return timestamp }
	t.Cleanup(func() {
		tbaNonceFunc, tbaTimeFunc = prevNonce, prevTime
	})
}

// setTBAClockSeq makes each token password deterministic but distinct.
func setTBAClockSeq(t *testing.T) {
	t.Helper()
	prevNonce, prevTime := tbaNonceFunc, tbaTimeFunc
	var n int
	tbaNonceFunc = func() (string, error) {
		n++
		return fmt.Sprintf("nonce%d", n), nil
	}
	tbaTimeFunc = func() int64 { return 1700000000 }
	t.Cleanup(func() {
		tbaNonceFunc, tbaTimeFunc = prevNonce, prevTime
	})
}

type recordingDriver struct {
	dsns []string
}

func (d *recordingDriver) Open(name string) (driver.Conn, error) {
	d.dsns = append(d.dsns, name)
	return &fakeConn{}, nil
}

func TestBuildSuiteAnalyticsQuery(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	end := time.Date(2026, 1, 3, 3, 4, 5, 987654321, time.UTC)

	got := buildSuiteAnalyticsQuery("transaction", nil, source.ReadOptions{
		IncrementalKey: "lastmodifieddate",
		IntervalStart:  &start,
		IntervalEnd:    &end,
	})

	assert.Equal(t, "SELECT * FROM transaction WHERE lastmodifieddate >= TO_TIMESTAMP('2026-01-02 03:04:05.123456789', 'YYYY-MM-DD HH24:MI:SSxFF') AND lastmodifieddate < TO_TIMESTAMP('2026-01-03 03:04:05.987654321', 'YYYY-MM-DD HH24:MI:SSxFF')", got)
}

func TestBuildSuiteAnalyticsQueryWithColumns(t *testing.T) {
	got := buildSuiteAnalyticsQuery("transaction", []string{"id", "trandate", "type"}, source.ReadOptions{})
	assert.Equal(t, "SELECT id, trandate, type FROM transaction", got)

	gotLimited := buildSuiteAnalyticsQuery("customer", []string{"id", "entityid"}, source.ReadOptions{Limit: 5})
	assert.Equal(t, "SELECT TOP 5 id, entityid FROM customer", gotLimited)
}

func TestBuildSuiteAnalyticsQueryWithLimit(t *testing.T) {
	// SuiteAnalytics Connect's SQL engine rejects FETCH FIRST; it uses TOP.
	got := buildSuiteAnalyticsQuery("customer", nil, source.ReadOptions{Limit: 25})

	assert.Equal(t, "SELECT TOP 25 * FROM customer", got)
}

func TestBuildSuiteAnalyticsQueryWithLimitAndInterval(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := buildSuiteAnalyticsQuery("transaction", nil, source.ReadOptions{
		IncrementalKey: "lastmodifieddate",
		IntervalStart:  &start,
		Limit:          10,
	})

	// TOP for the limit, interval in WHERE, and no ORDER BY (it crashes the
	// driver on wide tables and isn't needed for stateless interval loads).
	assert.Equal(t, "SELECT TOP 10 * FROM transaction WHERE lastmodifieddate >= TO_TIMESTAMP('2026-01-02 03:04:05.000000000', 'YYYY-MM-DD HH24:MI:SSxFF')", got)
}

func TestUniqueColumnNamesAvoidsGeneratedNameCollisions(t *testing.T) {
	got := uniqueColumnNames([]string{"name_2", "name", "name"})

	assert.Equal(t, []string{"name_2", "name", "name_3"}, got)
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
	db := openNetSuiteTestDB(t, fakeQueryResult{
		expectedQuery: "SELECT * FROM customer",
		columns:       []string{"id", "name"},
		rows: [][]driver.Value{
			{"1", "Acme"},
			{"2", "Globex"},
			{"3", "Umbrella"},
		},
	})
	defer func() {
		require.NoError(t, db.Close())
	}()

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
}

func TestTableColumns(t *testing.T) {
	// data_type codes: 93=TIMESTAMP, -5=BIGINT, 8=DOUBLE, -9=WVARCHAR, -10=CLOB.
	// memo and custbody_notes have size 4000 (> the wrapper's 1024 bindable
	// limit) so they land on the crashing chunked-fetch path (non-bindable);
	// entityid is a small VARCHAR2 that binds normally.
	newDB := func() *sql.DB {
		return openNetSuiteTestDB(t, fakeQueryResult{
			expectedQuery: "SELECT column_name, type_name, data_type, oa_precision FROM oa_columns WHERE table_name = 'transaction'",
			columns:       []string{"column_name", "type_name", "data_type", "oa_precision"},
			rows: [][]driver.Value{
				{"trandate", "TIMESTAMP", int64(93), int64(0)},
				{"id", "BIGINT", int64(-5), int64(0)},
				{"memo", "VARCHAR2", int64(-9), int64(4000)},
				{"amount", "DOUBLE", int64(8), int64(0)},
				{"custbody_notes", "CLOB", int64(-10), int64(4000)},
				{"entityid", "VARCHAR2", int64(-9), int64(100)},
			},
		})
	}

	// keep (default): bindable columns first, non-bindable (wide/CLOB) last; each
	// group sorted. Schema qualifier is stripped.
	s := &NetSuiteSource{db: newDB()}
	cols, err := s.tableColumns(context.Background(), "MyView.transaction", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"amount", "entityid", "id", "trandate", "custbody_notes", "memo"}, cols)

	// Excluded columns (case-insensitive) are dropped.
	cols, err = s.tableColumns(context.Background(), "transaction", []string{"MEMO"})
	require.NoError(t, err)
	assert.Equal(t, []string{"amount", "entityid", "id", "trandate", "custbody_notes"}, cols)

	// Legacy excludeCLOBColumns skips CLOB-typed columns; the non-CLOB wide
	// column (memo) still remains, which is why it is insufficient on its own.
	sNoCLOB := &NetSuiteSource{db: newDB(), excludeCLOBColumns: true}
	cols, err = sNoCLOB.tableColumns(context.Background(), "transaction", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"amount", "entityid", "id", "trandate", "memo"}, cols)

	// wide_text=exclude drops every non-bindable column (CLOB and wide VARCHAR2).
	sExclude := &NetSuiteSource{db: newDB(), wideTextMode: wideTextExclude}
	cols, err = sExclude.tableColumns(context.Background(), "transaction", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"amount", "entityid", "id", "trandate"}, cols)

	// wide_text=truncate SUBSTR-wraps non-bindable char columns to a bindable
	// width, keeping the data while avoiding the chunked-fetch crash path.
	sTrunc := &NetSuiteSource{db: newDB(), wideTextMode: wideTextTruncate, wideTextMaxChars: 1000}
	cols, err = sTrunc.tableColumns(context.Background(), "transaction", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"amount", "entityid", "id", "trandate",
		"SUBSTR(custbody_notes, 1, 1000) AS custbody_notes",
		"SUBSTR(memo, 1, 1000) AS memo",
	}, cols)
}

// TestTableColumnsFallsBackToTypeNameWhenDataTypeMissing verifies that a catalog
// without a usable data_type still classifies CLOBs as non-bindable.
func TestTableColumnsFallsBackToTypeNameWhenDataTypeMissing(t *testing.T) {
	db := openNetSuiteTestDB(t, fakeQueryResult{
		expectedQuery: "SELECT column_name, type_name, data_type, oa_precision FROM oa_columns WHERE table_name = 'transaction'",
		columns:       []string{"column_name", "type_name", "data_type", "oa_precision"},
		rows: [][]driver.Value{
			{"id", "BIGINT", nil, nil},
			{"custbody_notes", "CLOB", nil, nil},
		},
	})
	s := &NetSuiteSource{db: db, wideTextMode: wideTextExclude}
	cols, err := s.tableColumns(context.Background(), "transaction", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, cols)
}

func TestParseWideTextOptions(t *testing.T) {
	cfg, err := parseURI("netsuite://?dsn=NetSuite&wide_text=truncate&wide_text_max_chars=500")
	require.NoError(t, err)
	assert.Equal(t, wideTextTruncate, cfg.wideTextMode)
	assert.Equal(t, 500, cfg.wideTextMaxChars)

	// Defaults: keep mode, default truncation width.
	cfg, err = parseURI("netsuite://?dsn=NetSuite")
	require.NoError(t, err)
	assert.Equal(t, wideTextKeep, cfg.wideTextMode)
	assert.Equal(t, defaultWideTextMaxChars, cfg.wideTextMaxChars)

	// A width above the bindable limit is clamped so the column still binds.
	cfg, err = parseURI("netsuite://?dsn=NetSuite&wide_text=truncate&wide_text_max_chars=4000")
	require.NoError(t, err)
	assert.Equal(t, bindableCharWidthLimit, cfg.wideTextMaxChars)

	_, err = parseURI("netsuite://?dsn=NetSuite&wide_text=bogus")
	require.Error(t, err)

	_, err = parseURI("netsuite://?dsn=NetSuite&wide_text_max_chars=0")
	require.Error(t, err)
}

func TestUnqualifyTableName(t *testing.T) {
	assert.Equal(t, "transaction", unqualifyTableName("transaction"))
	assert.Equal(t, "transaction", unqualifyTableName("schema.transaction"))
	assert.Equal(t, "transaction", unqualifyTableName(`"transaction"`))
	assert.Equal(t, "transaction", unqualifyTableName("  transaction  "))
}

func TestNetSuiteSourceReadQueryError(t *testing.T) {
	db := openNetSuiteTestDB(t, fakeQueryResult{
		expectedQuery: "SELECT * FROM customer",
		queryErr:      sql.ErrConnDone,
	})
	defer func() {
		require.NoError(t, db.Close())
	}()

	s := &NetSuiteSource{db: db}
	ch, err := s.readTable(context.Background(), "customer", source.ReadOptions{})
	require.NoError(t, err)

	var gotErr error
	for result := range ch {
		gotErr = result.Err
	}
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "failed to query NetSuite SuiteAnalytics Connect")
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

const netSuiteTestDriverName = "netsuite_test"

var (
	registerTestDriver sync.Once
	testDriverMu       sync.Mutex
	testDriverCounter  int
	testDriverResults  = map[string]fakeQueryResult{}
)

type fakeQueryResult struct {
	expectedQuery string
	columns       []string
	rows          [][]driver.Value
	queryErr      error
}

func openNetSuiteTestDB(t *testing.T, result fakeQueryResult) *sql.DB {
	t.Helper()

	registerTestDriver.Do(func() {
		sql.Register(netSuiteTestDriverName, fakeDriver{})
	})

	testDriverMu.Lock()
	testDriverCounter++
	dsn := fmt.Sprintf("%s-%d", t.Name(), testDriverCounter)
	testDriverResults[dsn] = result
	testDriverMu.Unlock()

	t.Cleanup(func() {
		testDriverMu.Lock()
		delete(testDriverResults, dsn)
		testDriverMu.Unlock()
	})

	db, err := sql.Open(netSuiteTestDriverName, dsn)
	require.NoError(t, err)
	return db
}

type fakeDriver struct{}

func (fakeDriver) Open(name string) (driver.Conn, error) {
	testDriverMu.Lock()
	result, ok := testDriverResults[name]
	testDriverMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown fake NetSuite test DSN %q", name)
	}
	return &fakeConn{result: result}, nil
}

type fakeConn struct {
	result fakeQueryResult
}

func (c *fakeConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare is not supported by the fake NetSuite test driver")
}

func (c *fakeConn) Close() error {
	return nil
}

func (c *fakeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("transactions are not supported by the fake NetSuite test driver")
}

func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.result.expectedQuery != "" && query != c.result.expectedQuery {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	if c.result.queryErr != nil {
		return nil, c.result.queryErr
	}
	return &fakeRows{columns: c.result.columns, rows: c.result.rows}, nil
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *fakeRows) Columns() []string {
	return r.columns
}

func (r *fakeRows) Close() error {
	return nil
}

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}

	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

var _ driver.QueryerContext = (*fakeConn)(nil)
