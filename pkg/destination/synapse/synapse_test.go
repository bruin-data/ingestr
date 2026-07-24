package synapse

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
)

func TestTruncateInsertFromStagingRollsBackInsertFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &SynapseDestination{db: db}
	opts := destination.TruncateInsertFromStagingOptions{
		StagingTable:             "stage.events",
		TargetTable:              "dbo.events",
		PrimaryKeys:              []string{"id"},
		StagingPrimaryKeysUnique: true,
		Columns:                  []string{"id", "value"},
	}
	truncateSQL, insertSQL, err := buildTruncateInsertFromStagingSQL(opts)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(truncateSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertSQL)).WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err = dest.TruncateInsertFromStaging(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "failed to insert from staging") {
		t.Fatalf("TruncateInsertFromStaging() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTruncateInsertFromStagingSQLDeduplicatesUncertainKeys(t *testing.T) {
	truncateSQL, insertSQL, err := buildTruncateInsertFromStagingSQL(destination.TruncateInsertFromStagingOptions{
		StagingTable:   "stage.events",
		TargetTable:    "dbo.events",
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "updated_at", "value"},
		IncrementalKey: "updated_at",
	})
	if err != nil {
		t.Fatal(err)
	}
	if truncateSQL != "TRUNCATE TABLE [dbo].[events]" {
		t.Fatalf("truncateSQL = %q", truncateSQL)
	}
	if !strings.Contains(insertSQL, "ROW_NUMBER() OVER (PARTITION BY [id] ORDER BY [updated_at] DESC)") {
		t.Fatalf("insertSQL does not deduplicate staging rows:\n%s", insertSQL)
	}
}

func TestBuildDeleteInsertDeleteSQLUsesTableLock(t *testing.T) {
	sql := buildDeleteInsertDeleteSQL("dbo.events", "updated_at")

	if !strings.Contains(sql, "DELETE FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)") {
		t.Fatalf("delete SQL missing table lock: %s", sql)
	}
	if !strings.Contains(sql, "[updated_at] >= @p1") || !strings.Contains(sql, "[updated_at] <= @p2") {
		t.Fatalf("delete SQL missing interval predicate: %s", sql)
	}
}

func TestBuildMergeSQLWithIncrementalPredicate(t *testing.T) {
	sql := buildMergeSQLWithPredicate(
		"dbo.events",
		"stage.events",
		[]string{"id"},
		[]string{"[id]", "[event_date]"},
		[]string{"event_date"},
		"",
		"target.[event_date] >= DATEADD(day, -7, CAST(GETDATE() AS date))",
	)

	if !strings.Contains(sql, "ON target.[id] = source.[id] AND (target.[event_date] >= DATEADD(day, -7, CAST(GETDATE() AS date)))") {
		t.Fatalf("merge SQL missing incremental predicate: %s", sql)
	}
}
