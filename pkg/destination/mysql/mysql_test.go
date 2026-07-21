package mysql

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedCDCRunLeaseAcquiresAndReleasesAdvisoryLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, 0)")).
		WithArgs("ingestr_cdc_connector").
		WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs("ingestr_cdc_connector").
		WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

	lease, err := dest.AcquireManagedCDCRunLease(t.Context(), "connector")
	require.NoError(t, err)
	require.NoError(t, lease.Release())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagedCDCRunLeaseRejectsConcurrentOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, 0)")).
		WithArgs("ingestr_cdc_connector").
		WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(sql.NullInt64{Int64: 0, Valid: true}))

	_, err = dest.AcquireManagedCDCRunLease(t.Context(), "connector")
	require.ErrorContains(t, err, "already owns connector")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimAndPrepareEmptyCDCTargetUsesAtomicRename(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db, database: "app"}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")).
		WithArgs("app", "events").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT IGNORE INTO `_bruin_staging`.`cdc_targets` (`destination_table`, `connector_id`, `claimed_at`) VALUES (?, ?, CURRENT_TIMESTAMP(6))")).
		WithArgs(destination.CDCTargetKey("app", "events"), destination.CDCTargetOwnerID("connector", "source.events")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT `connector_id` FROM `_bruin_staging`.`cdc_targets` WHERE `destination_table` = ?")).
		WithArgs(destination.CDCTargetKey("app", "events")).
		WillReturnRows(sqlmock.NewRows([]string{"connector_id"}).AddRow(destination.CDCTargetOwnerID("connector", "source.events")))
	mock.ExpectCommit()
	mock.ExpectExec("CREATE TABLE `app`\\.`events_ingestr_claim_[0-9]+`").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(uint64(42)))
	mock.ExpectExec("RENAME TABLE `app`\\.`events_ingestr_claim_[0-9]+` TO `app`\\.`events`").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")).
		WithArgs("app", "events").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE"}).AddRow("InnoDB"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?")).
		WithArgs("app/events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(uint64(42)))

	incarnation, err := dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "events",
		ConnectorID:      "connector",
		SourceTable:      "source.events",
	}, destination.PrepareOptions{
		Table:       "events",
		Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"id"}},
		PrimaryKeys: []string{"id"},
		CDCMode:     true,
	})
	require.NoError(t, err)
	require.Equal(t, mysqlTableIncarnation("app", "events", 42), incarnation)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConditionalCDCSwapRejectsReplacedTargetBeforeRename(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db, database: "app"}

	mock.ExpectExec(regexp.QuoteMeta("LOCK TABLES `app`.`events` WRITE, `app`.`events_staging` WRITE")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")).
		WithArgs("app", "events").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE"}).AddRow("InnoDB"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?")).
		WithArgs("app/events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(uint64(43)))
	mock.ExpectExec(regexp.QuoteMeta("UNLOCK TABLES")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = dest.SwapTable(t.Context(), destination.SwapOptions{
		TargetTable:                   "events",
		StagingTable:                  "events_staging",
		CDCExpectedIncarnation:        mysqlTableIncarnation("app", "events", 42),
		CDCExpectedStagingIncarnation: mysqlTableIncarnation("app", "events_staging", 44),
		CDCExpectedResultIncarnation:  mysqlTableIncarnation("app", "events", 44),
	})
	require.ErrorContains(t, err, "replaced before conditional swap")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCanonicalMySQLTargetHonorsLowerCaseTableNames(t *testing.T) {
	if got := canonicalMySQLTarget("Sales", "Orders", 0); got != destination.CDCTargetKey("Sales", "Orders") {
		t.Fatalf("case-sensitive target = %q", got)
	}
	for _, mode := range []int{1, 2} {
		if got := canonicalMySQLTarget("Sales", "Orders", mode); got != destination.CDCTargetKey("sales", "orders") {
			t.Fatalf("mode %d target = %q", mode, got)
		}
	}
	database, table := splitDatabaseTable("`Sales`.`Orders`")
	if database != "Sales" || table != "Orders" {
		t.Fatalf("quoted target = %q.%q", database, table)
	}
}

func TestMySQLSiblingReferencePreservesDottedIdentifierBoundary(t *testing.T) {
	database, table := splitDatabaseTable("`Sales`.`order.events`")
	assert.Equal(t, "Sales", database)
	assert.Equal(t, "order.events", table)
	assert.Equal(t, "`Sales`.`order.events_old_123`", quoteMySQLTable(database, table+"_old_123"))
}

func TestMySQLSwapRejectsOverQualifiedNames(t *testing.T) {
	dest := &MySQLDestination{}
	err := dest.SwapTable(t.Context(), destination.SwapOptions{StagingTable: "app.staging", TargetTable: "server.app.orders"})
	assert.ErrorContains(t, err, "mysql table name")
}

func TestMySQLSwapQuotedDotTargetKeepsBackupInOneComponent(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db, database: "Sales"}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'Sales' AND table_name = 'order.events'")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec("RENAME TABLE `Sales`\\.`order\\.events` TO `Sales`\\.`order\\.events_old_[0-9]+`, `Sales`\\.`staging\\.events` TO `Sales`\\.`order\\.events`").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP TABLE IF EXISTS `Sales`\\.`order\\.events_old_[0-9]+`").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	assert.NoError(t, dest.SwapTable(t.Context(), destination.SwapOptions{
		StagingTable: "`Sales`.`staging.events`",
		TargetTable:  "`Sales`.`order.events`",
	}))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLTableIncarnationUsesInnoDBTableID(t *testing.T) {
	first := mysqlTableIncarnation("app", "events", 41)
	assert.Equal(t, first, mysqlTableIncarnation("app", "events", 41))
	assert.NotEqual(t, first, mysqlTableIncarnation("app", "events", 42))
}

func TestCDCTargetIncarnationReadsInnoDBDictionaryID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db, database: "app"}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")).
		WithArgs("app", "events").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE"}).AddRow("InnoDB"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?")).
		WithArgs("app/events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(uint64(42)))

	got, exists, err := dest.CDCTargetIncarnation(t.Context(), "events")
	if err != nil || !exists {
		t.Fatalf("CDCTargetIncarnation() = %q, exists=%v, err=%v", got, exists, err)
	}
	if want := mysqlTableIncarnation("app", "events", 42); got != want {
		t.Fatalf("CDCTargetIncarnation() = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCDCTruncatePreservesInnoDBPhysicalIdentityWithoutChangingBatchTruncate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db}
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM `app`.`events`")).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta("TRUNCATE TABLE `app`.`events`")).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := dest.TruncateCDCTable(t.Context(), "app.events"); err != nil {
		t.Fatal(err)
	}
	if err := dest.TruncateTable(t.Context(), "app.events"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConditionalCDCTruncateRejectsReplacedTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db, database: "app"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM `app`.`events` LIMIT 1 FOR UPDATE")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")).
		WithArgs("app", "events").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE"}).AddRow("InnoDB"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?")).
		WithArgs("app/events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(uint64(43)))
	mock.ExpectRollback()

	err = dest.TruncateCDCTableIfIncarnation(t.Context(), "app.events", mysqlTableIncarnation("app", "events", 42))
	require.ErrorContains(t, err, "was replaced before mutation")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConditionalCDCTruncateMutatesBoundTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &MySQLDestination{db: db, database: "app"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM `app`.`events` LIMIT 1 FOR UPDATE")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")).
		WithArgs("app", "events").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE"}).AddRow("InnoDB"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?")).
		WithArgs("app/events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(uint64(42)))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM `app`.`events`")).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	require.NoError(t, dest.TruncateCDCTableIfIncarnation(t.Context(), "app.events", mysqlTableIncarnation("app", "events", 42)))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVitessManagedCDCStateFailsWithoutDurableIncarnation(t *testing.T) {
	dest := NewVitessCompatibleDestination("vitess")
	assert.ErrorContains(t, dest.ValidateManagedCDCState(), "do not expose a durable physical table incarnation")
}

func TestCDCTargetClaimTableUsesBinaryCollation(t *testing.T) {
	columns := []schema.Column{
		{Name: "destination_table", DataType: schema.TypeString, MaxLength: 512},
		{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64},
		{Name: "claimed_at", DataType: schema.TypeTimestampTZ},
	}
	sql := buildCreateTableSQL("cdc_targets", columns, []string{"destination_table"})
	if !strings.Contains(sql, "`destination_table` VARCHAR(512) CHARACTER SET ascii COLLATE ascii_bin") {
		t.Fatalf("CDC target DDL lacks binary claim key:\n%s", sql)
	}
}

func TestUriToDSN(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantDSN      string
		wantDatabase string
		wantErr      bool
	}{
		{
			name:         "basic mysql uri",
			uri:          "mysql://user:pass@localhost:3306/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mysql uri with default port",
			uri:          "mysql://user:pass@localhost/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mysql uri without password",
			uri:          "mysql://user@localhost:3306/testdb",
			wantDSN:      "user@tcp(localhost:3306)/testdb?parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mariadb scheme",
			uri:          "mariadb://user:pass@localhost:3306/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mysql+pymysql scheme",
			uri:          "mysql+pymysql://user:pass@localhost:3306/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "uri with query parameters",
			uri:          "mysql://user:pass@localhost:3306/testdb?charset=utf8mb4",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "ps_mysql scheme enables tls",
			uri:          "ps_mysql://user:pass@aws.connect.psdb.cloud/mydb",
			wantDSN:      "user:pass@tcp(aws.connect.psdb.cloud:3306)/mydb?parseTime=true&tls=true",
			wantDatabase: "mydb",
			wantErr:      false,
		},
		{
			name:         "ps_mysql tls override wins",
			uri:          "ps_mysql://user:pass@localhost:3306/mydb?tls=skip-verify",
			wantDSN:      "user:pass@tcp(localhost:3306)/mydb?parseTime=true&tls=skip-verify",
			wantDatabase: "mydb",
			wantErr:      false,
		},
		{
			name:    "invalid scheme",
			uri:     "postgres://user:pass@localhost:5432/testdb",
			wantErr: true,
		},
		{
			name:    "invalid uri format",
			uri:     "not-a-valid-uri",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDSN, gotDatabase, err := uriToDSN(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantDSN, gotDSN)
			assert.Equal(t, tt.wantDatabase, gotDatabase)
		})
	}
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "simple table name",
			table: "users",
			want:  "`users`",
		},
		{
			name:  "schema qualified table",
			table: "mydb.users",
			want:  "`mydb`.`users`",
		},
		{
			name:  "table with special chars",
			table: "my-table",
			want:  "`my-table`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteTable(tt.table)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQuoteColumns(t *testing.T) {
	tests := []struct {
		name    string
		columns []string
		want    []string
	}{
		{
			name:    "single column",
			columns: []string{"id"},
			want:    []string{"`id`"},
		},
		{
			name:    "multiple columns",
			columns: []string{"id", "name", "email"},
			want:    []string{"`id`", "`name`", "`email`"},
		},
		{
			name:    "empty list",
			columns: []string{},
			want:    []string{},
		},
		{
			name:    "columns with special chars",
			columns: []string{"user-id", "user_name"},
			want:    []string{"`user-id`", "`user_name`"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteColumns(tt.columns)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterColumns(t *testing.T) {
	tests := []struct {
		name    string
		columns []string
		exclude []string
		want    []string
	}{
		{
			name:    "exclude one column",
			columns: []string{"id", "name", "email"},
			exclude: []string{"id"},
			want:    []string{"name", "email"},
		},
		{
			name:    "exclude multiple columns",
			columns: []string{"id", "name", "email", "age"},
			exclude: []string{"id", "age"},
			want:    []string{"name", "email"},
		},
		{
			name:    "exclude nothing",
			columns: []string{"id", "name", "email"},
			exclude: []string{},
			want:    []string{"id", "name", "email"},
		},
		{
			name:    "exclude non-existent column",
			columns: []string{"id", "name", "email"},
			exclude: []string{"age"},
			want:    []string{"id", "name", "email"},
		},
		{
			name:    "case insensitive exclusion",
			columns: []string{"ID", "Name", "Email"},
			exclude: []string{"id", "email"},
			want:    []string{"Name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterColumns(tt.columns, tt.exclude)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildJoinCondition(t *testing.T) {
	tests := []struct {
		name        string
		keys        []string
		targetAlias string
		sourceAlias string
		want        string
	}{
		{
			name:        "single primary key",
			keys:        []string{"id"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`id` = source.`id`",
		},
		{
			name:        "composite primary key",
			keys:        []string{"user_id", "post_id"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`user_id` = source.`user_id` AND target.`post_id` = source.`post_id`",
		},
		{
			name:        "different aliases",
			keys:        []string{"id"},
			targetAlias: "t",
			sourceAlias: "s",
			want:        "t.`id` = s.`id`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildJoinCondition(tt.keys, tt.targetAlias, tt.sourceAlias)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildUpdateSet(t *testing.T) {
	tests := []struct {
		name        string
		columns     []string
		targetAlias string
		sourceAlias string
		want        string
	}{
		{
			name:        "single column",
			columns:     []string{"name"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`name` = source.`name`",
		},
		{
			name:        "multiple columns",
			columns:     []string{"name", "email", "age"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`name` = source.`name`, target.`email` = source.`email`, target.`age` = source.`age`",
		},
		{
			name:        "different aliases",
			columns:     []string{"name"},
			targetAlias: "t",
			sourceAlias: "s",
			want:        "t.`name` = s.`name`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildUpdateSet(tt.columns, tt.targetAlias, tt.sourceAlias)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildCDCUpdateSetPreservesOnlyMarkedColumns(t *testing.T) {
	got := buildCDCUpdateSet(
		[]string{"payload", "note", destination.CDCLSNColumn},
		"target",
		"source",
		"source."+quoteColumn(destination.CDCUnchangedColsColumn),
	)
	assert.Contains(t, got, "target.`payload` = CASE WHEN JSON_CONTAINS(COALESCE(source.`_cdc_unchanged_cols`, '[]'), JSON_QUOTE(CONVERT(0x7061796c6f6164 USING utf8mb4))) THEN target.`payload` ELSE source.`payload` END")
	assert.Contains(t, got, "target.`note` = CASE WHEN JSON_CONTAINS(COALESCE(source.`_cdc_unchanged_cols`, '[]'), JSON_QUOTE(CONVERT(0x6e6f7465 USING utf8mb4))) THEN target.`note` ELSE source.`note` END")
	assert.Contains(t, got, "target.`_cdc_lsn` = source.`_cdc_lsn`")
	assert.NotContains(t, got, "LOWER(")
	assert.True(t, NewMySQLDestination().SupportsCDCUnchangedCols())
}

func TestBuildCDCUpdateSetUsesExactMarkerNames(t *testing.T) {
	got := buildCDCUpdateSet([]string{"Foo", "foo"}, "target", "source", "source."+quoteColumn(destination.CDCUnchangedColsColumn))
	assert.Contains(t, got, "JSON_QUOTE(CONVERT(0x466f6f USING utf8mb4))")
	assert.Contains(t, got, "JSON_QUOTE(CONVERT(0x666f6f USING utf8mb4))")
	assert.NotContains(t, got, "LOWER(")
}

func TestBuildCDCUpdateSetUsesSQLModeIndependentIdentifierEncoding(t *testing.T) {
	columns := []string{`a\b`, `quote's`, "line\nbreak", `literal\nsequence`}
	got := buildCDCUpdateSet(columns, "target", "source", "source."+quoteColumn(destination.CDCUnchangedColsColumn))

	for _, col := range columns {
		assert.Contains(t, got, "JSON_QUOTE("+mysqlUTF8Expression(col)+")")
	}
	assert.NotContains(t, got, "JSON_QUOTE('")
	assert.NotContains(t, got, `JSON_QUOTE('a\b')`)
}

func TestCDCMergeWithoutUnchangedColsMarkerUsesNormalUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	dest := &MySQLDestination{db: db}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("ON target.`id` = source.`id` AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn` OR (source.`_cdc_lsn` = target.`_cdc_lsn` AND COALESCE(target.`_cdc_deleted`, 0) = 0 AND source.`__ingestr_has_equal_lsn_delete` = 1)) SET target.`payload` = source.`payload`")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("WHERE NOT EXISTS (SELECT 1 FROM `items` AS target WHERE target.`id` = source.`id`)")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("WHERE source.`_cdc_deleted` = 1 AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn` OR (source.`_cdc_lsn` = target.`_cdc_lsn` AND COALESCE(target.`_cdc_deleted`, 0) = 0))")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("WHERE source.`_cdc_deleted` = 1 AND NOT EXISTS (SELECT 1 FROM `items` AS target WHERE target.`id` = source.`id`)")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:  "items",
		StagingTable: "items_staging",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
	})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCDCMergeWithIncrementalPredicateInsertsBeforeUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	dest := &MySQLDestination{db: db}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("WHERE NOT EXISTS (SELECT 1 FROM `items` AS target WHERE target.`id` = source.`id`)")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("ON target.`id` = source.`id` AND (target.`id` >= 1) AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn` OR (source.`_cdc_lsn` = target.`_cdc_lsn` AND COALESCE(target.`_cdc_deleted`, 0) = 0 AND source.`__ingestr_has_equal_lsn_delete` = 1)) SET target.`payload` = source.`payload`")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("WHERE source.`_cdc_deleted` = 1 AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn` OR (source.`_cdc_lsn` = target.`_cdc_lsn` AND COALESCE(target.`_cdc_deleted`, 0) = 0))")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("WHERE source.`_cdc_deleted` = 1 AND NOT EXISTS (SELECT 1 FROM `items` AS target WHERE target.`id` = source.`id`)")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = dest.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:          "items",
		StagingTable:         "items_staging",
		PrimaryKeys:          []string{"id"},
		Columns:              []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		IncrementalPredicate: "target.`id` >= 1",
	})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildMultiRowInsertSQL(t *testing.T) {
	got := buildMultiRowInsertSQL("analytics.users", []string{"`id`", "`name`"}, 3)
	want := "INSERT INTO `analytics`.`users` (`id`, `name`) VALUES (?, ?), (?, ?), (?, ?)"
	assert.Equal(t, want, got)
}

func TestBuildLoadDataSQL(t *testing.T) {
	got := buildLoadDataSQL("analytics.users", []string{"`id`", "`name`"}, "ingestr_load_1")
	want := "LOAD DATA LOCAL INFILE 'Reader::ingestr_load_1' INTO TABLE `analytics`.`users` FIELDS TERMINATED BY '\\t' ESCAPED BY '\\\\' LINES TERMINATED BY '\\n' (`id`, `name`)"
	assert.Equal(t, want, got)
}

func TestMapDataTypeToMySQLStringLength(t *testing.T) {
	assert.Equal(t, "VARCHAR(16383)", MapDataTypeToMySQL(schema.Column{DataType: schema.TypeString, MaxLength: 16383}))
	assert.Equal(t, "TEXT", MapDataTypeToMySQL(schema.Column{DataType: schema.TypeString, MaxLength: 16384}))
	assert.Equal(t, "TEXT", MapDataTypeToMySQL(schema.Column{DataType: schema.TypeString, MaxLength: 65535}))
}

func TestWriteLoadDataFieldEscaping(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{name: "null", value: nil, want: `\N`},
		{name: "string escapes", value: "a\tb\nc\rd\\e\x00\x1a", want: `a\tb\nc\rd\\e\0\Z`},
		{name: "bytes escapes", value: []byte("bytes\tvalue"), want: `bytes\tvalue`},
		{name: "integer", value: int64(42), want: "42"},
		{name: "float", value: 12.5, want: "12.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got strings.Builder
			assert.NoError(t, writeLoadDataField(&got, tt.value))
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestIsLoadDataLocalDisabledError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"mysql 3948", &mysqldriver.MySQLError{Number: 3948, Message: "Loading local data is disabled"}, true},
		{"mysql 1148", &mysqldriver.MySQLError{Number: 1148, Message: "The used command is not allowed with this MySQL version"}, true},
		{"text disabled", errors.New("loading local data is disabled; enable local_infile"), true},
		{"text not allowed", errors.New("The used command is not allowed with this MySQL version"), true},
		{"other mysql error", &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry"}, false},
		{"unrelated error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLoadDataLocalDisabledError(tt.err))
		})
	}
}

// isMySQLMissingTableError must recognize both plain MySQL ("doesn't exist",
// errno 1146) and vtgate (VT05004/VT05005 "does not exist", errno 1146/1051)
// forms, so a first CDC run against a Vitess/PlanetScale destination is treated
// as "no cursor yet" rather than an error.
func TestIsMySQLMissingTableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"mysql errno 1146", &mysqldriver.MySQLError{Number: 1146, Message: "Table 'db.t' doesn't exist"}, true},
		{"mysql errno 1049", &mysqldriver.MySQLError{Number: 1049, Message: "Unknown database 'db'"}, true},
		{"vtgate errno 1051", &mysqldriver.MySQLError{Number: 1051, Message: "VT05004: table 't' does not exist"}, true},
		{"vtgate text without errno", errors.New("target: db.0.primary: vttablet: table 't' does not exist in keyspace 'db'"), true},
		{"plain mysql text without errno", errors.New("Error 1146: Table 'db.t' doesn't exist"), true},
		{"other mysql error", &mysqldriver.MySQLError{Number: 1045, Message: "Access denied"}, false},
		{"unrelated error", errors.New("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isMySQLMissingTableError(tt.err))
		})
	}
}

// The information_schema filter must honor the table's database qualifier:
// destination tables can live outside the connection's default database (e.g.
// multi-table CDC with dest_schema).
func TestMySQLSchemaFilter(t *testing.T) {
	assert.Equal(t, "?", mysqlSchemaFilterExpr("otherdb"))
	assert.Equal(t, []interface{}{"otherdb", "users"}, mysqlSchemaFilterArgs("otherdb", "users"))

	assert.Equal(t, "DATABASE()", mysqlSchemaFilterExpr(""))
	assert.Equal(t, []interface{}{"users"}, mysqlSchemaFilterArgs("", "users"))
}

func TestExtractTableName(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "simple table name",
			table: "users",
			want:  "users",
		},
		{
			name:  "schema qualified table",
			table: "mydb.users",
			want:  "users",
		},
		{
			name:  "multiple dots",
			table: "catalog.schema.table",
			want:  "table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTableName(tt.table)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	tests := []struct {
		name        string
		table       string
		columns     []schema.Column
		primaryKeys []string
		want        string
	}{
		{
			name:  "simple table without primary key",
			table: "users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
			},
			primaryKeys: nil,
			want:        "CREATE TABLE IF NOT EXISTS `users` (\n  `id` BIGINT,\n  `name` TEXT\n)",
		},
		{
			name:  "table with primary key",
			table: "users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
			},
			primaryKeys: []string{"id"},
			want:        "CREATE TABLE IF NOT EXISTS `users` (\n  `id` BIGINT,\n  `name` TEXT,\n  PRIMARY KEY (`id`)\n)",
		},
		{
			name:  "table with composite primary key",
			table: "user_posts",
			columns: []schema.Column{
				{Name: "user_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "post_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "created_at", DataType: schema.TypeTimestamp, Nullable: true},
			},
			primaryKeys: []string{"user_id", "post_id"},
			want:        "CREATE TABLE IF NOT EXISTS `user_posts` (\n  `user_id` BIGINT,\n  `post_id` BIGINT,\n  `created_at` DATETIME(6),\n  PRIMARY KEY (`user_id`, `post_id`)\n)",
		},
		{
			name:  "schema qualified table",
			table: "mydb.users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			},
			primaryKeys: nil,
			want:        "CREATE TABLE IF NOT EXISTS `mydb`.`users` (\n  `id` BIGINT\n)",
		},
		{
			name:  "table with various types",
			table: "test_table",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
				{Name: "active", DataType: schema.TypeBoolean, Nullable: false},
				{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
				{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2, Nullable: true},
				{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
			},
			primaryKeys: []string{"id"},
			want:        "CREATE TABLE IF NOT EXISTS `test_table` (\n  `id` BIGINT,\n  `name` TEXT,\n  `active` BOOLEAN,\n  `score` DOUBLE,\n  `amount` DECIMAL(10,2),\n  `created_at` TIMESTAMP(6),\n  PRIMARY KEY (`id`)\n)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCreateTableSQL(tt.table, tt.columns, tt.primaryKeys)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMySQLDestination_Schemes(t *testing.T) {
	dest := NewMySQLDestination()
	schemes := dest.Schemes()
	expected := []string{"mysql", "mysql+pymysql", "mariadb"}
	assert.Equal(t, expected, schemes)
}

func TestMySQLDestination_StrategySupport(t *testing.T) {
	dest := NewMySQLDestination()

	assert.True(t, dest.SupportsReplaceStrategy())
	assert.True(t, dest.SupportsAppendStrategy())
	assert.True(t, dest.SupportsMergeStrategy())
	assert.True(t, dest.SupportsDeleteInsertStrategy())
	assert.True(t, dest.SupportsAtomicSwap())
}

func TestDeleteInsertLockName(t *testing.T) {
	first := deleteInsertLockName("analytics.orders")
	second := deleteInsertLockName("analytics.orders")
	other := deleteInsertLockName("analytics.customers")

	assert.Equal(t, first, second)
	assert.NotEqual(t, first, other)
	assert.LessOrEqual(t, len(first), 64)
	assert.Contains(t, first, "ingestr_di_")
}

func TestMySQLVersionAllowsRenameUnderLock(t *testing.T) {
	assert.True(t, mysqlVersionAllowsRenameUnderLock("8.0.13"))
	assert.True(t, mysqlVersionAllowsRenameUnderLock("8.0.34-log"))
	assert.True(t, mysqlVersionAllowsRenameUnderLock("8.4.0"))
	assert.True(t, mysqlVersionAllowsRenameUnderLock("9.1.0"))

	assert.False(t, mysqlVersionAllowsRenameUnderLock("8.0.12"))
	assert.False(t, mysqlVersionAllowsRenameUnderLock("5.7.44-log"))
	assert.False(t, mysqlVersionAllowsRenameUnderLock("10.11.6-MariaDB-1:10.11.6+maria~ubu2204"))
	assert.False(t, mysqlVersionAllowsRenameUnderLock("11.4.2-MariaDB"))
	assert.False(t, mysqlVersionAllowsRenameUnderLock("garbage"))
}
