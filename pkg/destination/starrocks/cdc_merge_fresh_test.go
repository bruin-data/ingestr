package starrocks

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestCDCFilterLiteral(t *testing.T) {
	for _, tc := range []struct {
		dataType schema.DataType
		value    string
		want     string
		ok       bool
	}{
		{schema.TypeInt64, "-42", "-42", true},
		{schema.TypeInt64, "18446744073709551615", "18446744073709551615", true},
		{schema.TypeInt32, "1 OR 1=1", "", false},
		{schema.TypeString, `it's\`, `'it''s\\'`, true},
		{schema.TypeDate, "2026-01-02", "'2026-01-02'", true},
		{schema.TypeTimestamp, "2026-01-02 00:00:00", "", false},
		{schema.TypeFloat64, "1.5", "", false},
	} {
		got, ok := cdcFilterLiteral(tc.dataType, tc.value)
		require.Equal(t, tc.ok, ok, tc.value)
		require.Equal(t, tc.want, got, tc.value)
	}
}

func TestCDCCarryForwardCondition(t *testing.T) {
	opts := destination.MergeOptions{Columns: []string{"id", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCUnchangedColsColumn}}
	unchanged := "COALESCE(array_length(CAST(parse_json(__ingestr_k.`_cdc_unchanged_cols`) AS ARRAY<VARCHAR>)), 0) > 0"
	require.Equal(t, "COALESCE(__ingestr_k.`_cdc_deleted`, FALSE) OR "+unchanged, cdcCarryForwardCondition(opts, false))
	require.Equal(t, "NOT COALESCE(__ingestr_k.`_cdc_deleted`, FALSE) AND "+unchanged, cdcCarryForwardCondition(opts, true))

	opts.Columns = opts.Columns[:3]
	require.Equal(t, "NOT COALESCE(__ingestr_k.`_cdc_deleted`, FALSE) AND FALSE", cdcCarryForwardCondition(opts, true))
}

func TestCDCHardDeleteCanSkipCarry(t *testing.T) {
	opts := destination.MergeOptions{
		TargetTable:   "bench.t",
		PrimaryKeys:   []string{"id"},
		CDCHardDelete: true,
	}
	query := regexp.QuoteMeta(`SELECT COLUMN_NAME, IS_NULLABLE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &StarRocksDestination{db: db, database: "bench"}

	mock.ExpectQuery(query).WithArgs("bench", "t").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "IS_NULLABLE"}).
			AddRow("id", "NO").
			AddRow("v", "YES").
			AddRow(destination.CDCLSNColumn, "NO").
			AddRow(destination.CDCDeletedColumn, "NO").
			AddRow(cdcOrderColumn, "NO"),
	)
	canSkip, err := d.cdcHardDeleteCanSkipCarry(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, canSkip)

	mock.ExpectQuery(query).WithArgs("bench", "t").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "IS_NULLABLE"}).
			AddRow("id", "NO").
			AddRow("v", "NO"),
	)
	canSkip, err = d.cdcHardDeleteCanSkipCarry(context.Background(), opts)
	require.NoError(t, err)
	require.False(t, canSkip)

	opts.CDCHardDelete = false
	canSkip, err = d.cdcHardDeleteCanSkipCarry(context.Background(), opts)
	require.NoError(t, err)
	require.False(t, canSkip)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCDCPartialPlanOmitsUnresolvedColumns(t *testing.T) {
	d := &StarRocksDestination{database: "bench"}
	opts := destination.MergeOptions{
		StagingTable: "_bruin_staging.s", TargetTable: "bench.t", PrimaryKeys: []string{"id"},
		Columns: []string{"id", "v", "w", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCUnchangedColsColumn},
	}
	plan := d.cdcPartialPlan(opts, false)
	require.Equal(t, []string{"v", "w"}, plan.payload)
	groups := plan.groupsSQL()
	require.True(t, strings.HasPrefix(groups, "SELECT "+cdcMergeHint+" * FROM (WITH staged AS (SELECT"), groups)
	require.Contains(t, groups, "MAX(IF(COALESCE(__ingestr_k.`_cdc_deleted`, FALSE) OR ")
	require.Contains(t, groups, "CONCAT(IF(__ingestr_value_1.`id` IS NULL, '1', '0'), IF(__ingestr_value_2.`id` IS NULL, '1', '0')) AS `__ingestr_partial`")
	require.Contains(t, groups, "UNION SELECT '-' FROM staged WHERE `__ingestr_rank` = 1 AND `__ingestr_carry` = 0")
	require.NotContains(t, groups, "`bench`.`t`")

	simple := plan.upsertSQL(cdcPartialSimpleGroup)
	require.True(t, strings.HasSuffix(simple, "SELECT `id`, `v`, `w`, `_cdc_lsn`, `_cdc_deleted` FROM staged WHERE `__ingestr_rank` = 1 AND `__ingestr_carry` = 0"), simple)

	upsert := plan.upsertSQL("01")
	require.True(t, strings.HasPrefix(upsert, "(`id`, `v`, `_cdc_lsn`, `_cdc_deleted`) WITH staged AS"), upsert)
	require.True(t, strings.HasSuffix(upsert, "SELECT `id`, `v`, `_cdc_lsn`, `_cdc_deleted` FROM resolved WHERE `__ingestr_partial` = '01'"), upsert)

	require.Contains(t, d.cdcPartialPlan(opts, true).groupsSQL(), "IF(COALESCE(__ingestr_l.`_cdc_deleted`, FALSE), '00', CONCAT(")
}

func TestCDCFreshMergeSQLReadsOnlyCarriedTargetRows(t *testing.T) {
	d := &StarRocksDestination{database: "bench"}
	opts := destination.MergeOptions{
		StagingTable: "_bruin_staging.s", TargetTable: "bench.t", PrimaryKeys: []string{"id"},
		Columns: []string{"id", "v", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCUnchangedColsColumn},
	}
	filter := cdcKeyFilter{keys: []string{"id"}, values: [][]string{{"7"}}}
	query := d.buildCDCFreshMergeSQL(opts, filter, false)
	require.Equal(t, 1, strings.Count(query, "`bench`.`t`"), query)
	require.Contains(t, query, "__ingestr_tgt AS (SELECT __ingestr_f.`id`, __ingestr_f.`v`, __ingestr_f.`_cdc_lsn`, __ingestr_f.`_cdc_deleted` FROM `bench`.`t` __ingestr_f WHERE __ingestr_f.`id` IN (7) AND EXISTS (")
	require.Contains(t, query, "FROM staged __ingestr_s WHERE __ingestr_s.`__ingestr_carry` = 1")
	require.True(t, strings.HasSuffix(query, "UNION ALL\nSELECT __ingestr_k.`id`, __ingestr_k.`v`, __ingestr_k.`_cdc_lsn`, __ingestr_k.`_cdc_deleted` FROM staged __ingestr_k WHERE __ingestr_k.`__ingestr_rank` = 1 AND __ingestr_k.`__ingestr_carry` = 0"), query)
	require.NotContains(t, query, "changes AS")
}

type lostTestLease struct {
	done chan struct{}
	err  error
}

func (l *lostTestLease) Done() <-chan struct{} { return l.done }
func (l *lostTestLease) Err() error            { return l.err }
func (l *lostTestLease) Release() error        { return nil }

func TestCDCMergeChecksConnectorLeaseBeforeWrites(t *testing.T) {
	lease := &lostTestLease{done: make(chan struct{}), err: errors.New("expired")}
	ctx := source.WithConnectorLeaseGuard(context.Background(), source.NewConnectorLeaseGuard(lease))

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &StarRocksDestination{db: db, database: "bench", insertPartialChecked: true, insertPartialUpdates: true}

	opts := destination.MergeOptions{
		StagingTable: "_bruin_staging.s", TargetTable: "bench.t", PrimaryKeys: []string{"id"},
		Columns: []string{"id", "v", destination.CDCLSNColumn, destination.CDCDeletedColumn},
	}

	// Lease lost after target eligibility and grouping queries, before the first partial upsert.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT PRIMARY_KEY, SORT_KEY FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`)).
		WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"PRIMARY_KEY", "SORT_KEY"}).AddRow("`id`", "`id`"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COLUMN_NAME, GENERATION_EXPRESSION FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`)).
		WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "GENERATION_EXPRESSION"}).AddRow("id", nil).AddRow("v", ""))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + cdcMergeHint)).WillReturnRows(
		sqlmock.NewRows([]string{"g"}).AddRow("-"),
	)
	close(lease.done)

	upserts, err := d.mergeCDCPartial(ctx, opts, false)
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	require.Zero(t, upserts)

	err = d.runCDCMerge(ctx, opts, "SELECT 1")
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)

	err = d.deleteVisible(ctx, opts.TargetTable, "DELETE FROM `bench`.`t`")
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSupportsInsertPartialUpdates(t *testing.T) {
	ctx := context.Background()

	// StarRocks < 3.3.1: disabled without probing enable_insert_partial_update.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &StarRocksDestination{db: db}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT current_version()")).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("3.3.0-abc"))
	ok, hint, err := d.supportsInsertPartialUpdates(ctx)
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, hint)
	require.NoError(t, mock.ExpectationsWereMet())

	// StarRocks 3.3.1-3.3.19 (no enable_insert_partial_update variable): enabled with base hint.
	db2, mock2, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()
	d2 := &StarRocksDestination{db: db2}
	mock2.ExpectQuery(regexp.QuoteMeta("SELECT current_version()")).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("3.3.10-abc"))
	mock2.ExpectQuery(regexp.QuoteMeta("SHOW VARIABLES LIKE 'enable_insert_partial_update'")).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}))
	ok, hint, err = d2.supportsInsertPartialUpdates(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cdcMergeHint, hint)
	require.NoError(t, mock2.ExpectationsWereMet())

	// StarRocks 3.3.20+ (exposes enable_insert_partial_update, even if false): enabled with SET_VAR override.
	db3, mock3, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db3.Close() }()
	d3 := &StarRocksDestination{db: db3}
	mock3.ExpectQuery(regexp.QuoteMeta("SELECT current_version()")).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("3.3.22-753696f"))
	mock3.ExpectQuery(regexp.QuoteMeta("SHOW VARIABLES LIKE 'enable_insert_partial_update'")).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).AddRow("enable_insert_partial_update", "false"))
	ok, hint, err = d3.supportsInsertPartialUpdates(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cdcPartialMergeHint, hint)
	require.NoError(t, mock3.ExpectationsWereMet())
}

func TestTargetSupportsCDCPartialMerge(t *testing.T) {
	ctx := context.Background()
	sortQuery := regexp.QuoteMeta(`SELECT PRIMARY_KEY, SORT_KEY FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`)
	genQuery := regexp.QuoteMeta(`SELECT COLUMN_NAME, GENERATION_EXPRESSION FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &StarRocksDestination{db: db, database: "bench"}
	opts := destination.MergeOptions{TargetTable: "bench.t", PrimaryKeys: []string{"id"}}

	// Standard PK table with __ingestr_cdc_order generated column: eligible.
	mock.ExpectQuery(sortQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"PRIMARY_KEY", "SORT_KEY"}).AddRow("`id`", "`id`"))
	mock.ExpectQuery(genQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "GENERATION_EXPRESSION"}).
			AddRow("id", nil).
			AddRow("v", "").
			AddRow(cdcOrderColumn, "CAST(1 AS LARGEINT)"))
	eligible, err := d.targetSupportsCDCPartialMerge(ctx, opts)
	require.NoError(t, err)
	require.True(t, eligible)

	// Decoupled ORDER BY including non-PK column: ineligible.
	mock.ExpectQuery(sortQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"PRIMARY_KEY", "SORT_KEY"}).AddRow("`id`", "`v`, `id`"))
	eligible, err = d.targetSupportsCDCPartialMerge(ctx, opts)
	require.NoError(t, err)
	require.False(t, eligible)

	// User-defined generated column: ineligible.
	mock.ExpectQuery(sortQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"PRIMARY_KEY", "SORT_KEY"}).AddRow("`id`", nil))
	mock.ExpectQuery(genQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "GENERATION_EXPRESSION"}).
			AddRow("id", nil).
			AddRow("v", nil).
			AddRow("g", "`v` + 1"))
	eligible, err = d.targetSupportsCDCPartialMerge(ctx, opts)
	require.NoError(t, err)
	require.False(t, eligible)

	// Reordered composite PK sort key: eligible.
	opts.PrimaryKeys = []string{"a", "b"}
	mock.ExpectQuery(sortQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"PRIMARY_KEY", "SORT_KEY"}).AddRow("`a`, `b`", "`b`, `a`"))
	mock.ExpectQuery(genQuery).WithArgs("bench", "t").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME", "GENERATION_EXPRESSION"}).AddRow("a", nil).AddRow("b", nil))
	eligible, err = d.targetSupportsCDCPartialMerge(ctx, opts)
	require.NoError(t, err)
	require.True(t, eligible)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCDCFilterStarRocksType(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want schema.DataType
		ok   bool
	}{
		{"tinyint", schema.TypeInt8, true},
		{"SMALLINT", schema.TypeInt16, true},
		{"int", schema.TypeInt32, true},
		{"integer", schema.TypeInt32, true},
		{"bigint", schema.TypeInt64, true},
		{"varchar", schema.TypeString, true},
		{"string", schema.TypeString, true},
		{"date", schema.TypeDate, true},
		{"largeint", schema.TypeString, false},
		{"char", schema.TypeString, false},
		{"decimal", schema.TypeString, false},
		{"datetime", schema.TypeString, false},
		{"double", schema.TypeString, false},
		{"", schema.TypeString, false},
	} {
		got, ok := cdcFilterStarRocksType(tc.raw)
		require.Equal(t, tc.ok, ok, tc.raw)
		if tc.ok {
			require.Equal(t, tc.want, got, tc.raw)
		}
	}
}

func TestStagedKeyFilterChecksActualStarRocksTypes(t *testing.T) {
	typeQuery := regexp.QuoteMeta(`SELECT COLUMN_NAME, DATA_TYPE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &StarRocksDestination{db: db, database: "bench"}

	// Target LARGEINT vs staging VARCHAR must leave the key unbounded.
	opts := destination.MergeOptions{
		StagingTable: "_bruin_staging.s",
		TargetTable:  "bench.t",
		PrimaryKeys:  []string{"id"},
	}
	mock.ExpectQuery(typeQuery).WithArgs("bench", "t").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).AddRow("id", "largeint"),
	)
	mock.ExpectQuery(typeQuery).WithArgs("_bruin_staging", "s").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).AddRow("id", "varchar"),
	)
	filter, err := d.stagedKeyFilter(context.Background(), opts, "TRUE")
	require.NoError(t, err)
	require.Empty(t, filter.predicate("t"))

	// Target BIGINT vs staging VARCHAR must also leave the key unbounded.
	mock.ExpectQuery(typeQuery).WithArgs("bench", "t").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).AddRow("id", "bigint"),
	)
	mock.ExpectQuery(typeQuery).WithArgs("_bruin_staging", "s").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).AddRow("id", "varchar"),
	)
	filter, err = d.stagedKeyFilter(context.Background(), opts, "TRUE")
	require.NoError(t, err)
	require.Empty(t, filter.predicate("t"))

	// Composite key: matching BIGINT column is bounded, LARGEINT column stays unbounded.
	opts.PrimaryKeys = []string{"id", "big_id"}
	mock.ExpectQuery(typeQuery).WithArgs("bench", "t").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).
			AddRow("id", "bigint").
			AddRow("big_id", "largeint"),
	)
	mock.ExpectQuery(typeQuery).WithArgs("_bruin_staging", "s").WillReturnRows(
		sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).
			AddRow("id", "bigint").
			AddRow("big_id", "varchar"),
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT CAST(__ingestr_k.`id` AS VARCHAR) FROM `_bruin_staging`.`s` __ingestr_k WHERE TRUE LIMIT 4097")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("10").AddRow("20"))
	filter, err = d.stagedKeyFilter(context.Background(), opts, "TRUE")
	require.NoError(t, err)
	require.Equal(t, "t.`id` IN (10, 20)", filter.predicate("t"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCDCKeyFilterPredicate(t *testing.T) {
	inList := cdcKeyFilter{keys: []string{"tenant", "code"}, values: [][]string{{"1", "2"}, {`'a''b\\c'`}}}
	require.Equal(t, "t.`tenant` IN (1, 2) AND t.`code` IN ('a''b"+`\\`+"c')", inList.predicate("t"))
	require.Equal(t, "FALSE", cdcKeyFilter{keys: []string{"id"}, values: [][]string{{}}}.predicate("t"))
	bounded := cdcKeyFilter{keys: []string{"id", "note"}, lower: []string{"5", ""}, upper: []string{"9", ""}}
	require.Equal(t, "t.`id` BETWEEN 5 AND 9", bounded.predicate("t"))
	require.Empty(t, cdcKeyFilter{}.predicate("t"))
}

func TestCDCSingleReadMergeSQLReadsTargetOnce(t *testing.T) {
	d := &StarRocksDestination{database: "bench"}
	opts := destination.MergeOptions{
		StagingTable: "_bruin_staging.s", TargetTable: "bench.t", PrimaryKeys: []string{"id"},
		Columns: []string{"id", "v", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCUnchangedColsColumn},
	}
	require.Equal(t, 2, strings.Count(d.buildCDCMergeSQL(opts), "`bench`.`t`"))
	query := d.buildCDCSingleReadMergeSQL(opts)
	require.Equal(t, 1, strings.Count(query, "`bench`.`t`"), query)
	require.Contains(t, query, "WITH __ingestr_tgt AS (SELECT __ingestr_f.`id`, __ingestr_f.`v`, __ingestr_f.`_cdc_lsn`, __ingestr_f.`_cdc_deleted` FROM `bench`.`t` __ingestr_f WHERE EXISTS (SELECT 1 FROM `_bruin_staging`.`s` __ingestr_k WHERE __ingestr_k.`id` = __ingestr_f.`id`))")
	require.Contains(t, query, "LEFT JOIN __ingestr_tgt __ingestr_t ON")
	require.Contains(t, query, "FROM __ingestr_tgt __ingestr_t WHERE EXISTS (SELECT 1 FROM changes")

	opts.CDCSnapshot = true
	require.Equal(t, d.buildCDCMergeSQL(opts), d.buildCDCSingleReadMergeSQL(opts))
}

func TestCDCMergeModeFromEnv(t *testing.T) {
	for value, want := range map[string]cdcMergeMode{"": cdcMergeAuto, "auto": cdcMergeAuto, "atomic": cdcMergeAtomic, " Legacy ": cdcMergeLegacy, "bogus": cdcMergeAuto} {
		t.Setenv("INGESTR_STARROCKS_CDC_MERGE", value)
		require.Equal(t, want, cdcMergeModeFromEnv(), value)
	}
}

func TestVersionAtLeast(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]bool{
		"3.3.1-abc1234": true,
		"3.3.5":         true,
		"3.4.0":         true,
		"4.0.0-rc01":    true,
		"3.3.0-a1b2c3":  false,
		"3.2.11":        false,
		"2.5.22":        false,
		"3.3":           false,
		"":              false,
		"x.y.z":         false,
	} {
		require.Equal(t, want, versionAtLeast(version, 3, 3, 1), version)
	}
}
