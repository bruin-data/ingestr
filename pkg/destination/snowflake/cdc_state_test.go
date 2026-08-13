package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalCDCTargetUppercasesIdentifiers(t *testing.T) {
	d := &SnowflakeDestination{database: "raw_db"}
	got, err := d.CanonicalCDCTarget(context.Background(), "raw_replay.test_runs")
	require.NoError(t, err)
	want := destination.CDCTargetKey("RAW_DB", "RAW_REPLAY", "TEST_RUNS")
	assert.Equal(t, want, got)

	got, err = d.CanonicalCDCTarget(context.Background(), `RAW_DB.RAW_REPLAY.TEST_RUNS`)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestManagedCDCStateCatalog(t *testing.T) {
	d := &SnowflakeDestination{database: "analytics"}
	assert.Equal(t, "ANALYTICS", d.ManagedCDCStateCatalog())
}

func TestLoadCDCState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &SnowflakeDestination{db: db, database: "RAW_DB"}
	rows := sqlmock.NewRows([]string{
		"event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn",
	}).AddRow("evt-1", "public.users", "RAW_REPLAY.USERS", "checkpoint", int64(3), "complete", "00000000/0000002A")

	mock.ExpectQuery(`SELECT .+ FROM "_BRUIN_STAGING"\."CDC_STATE" WHERE "CONNECTOR_ID" = \?`).
		WithArgs("connector-a").
		WillReturnRows(rows)

	entries, err := d.LoadCDCState(context.Background(), "_bruin_staging.cdc_state", "connector-a")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "evt-1", entries[0].EventID)
	assert.Equal(t, "public.users", entries[0].SourceTable)
	assert.Equal(t, int64(3), entries[0].Generation)
	assert.Equal(t, "00000000/0000002A", entries[0].Position)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCDCStateMissingTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &SnowflakeDestination{db: db, database: "RAW_DB"}
	mock.ExpectQuery(`SELECT .+ FROM .+`).
		WithArgs("connector-a").
		WillReturnError(errors.New("SQL compilation error: Object '_BRUIN_STAGING.CDC_STATE' does not exist or not authorized. (002003)"))

	entries, err := d.LoadCDCState(context.Background(), "_bruin_staging.cdc_state", "connector-a")
	require.NoError(t, err)
	assert.Nil(t, entries)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCDCStateFence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &SnowflakeDestination{db: db, database: "RAW_DB"}
	rows := sqlmock.NewRows([]string{"event_id", "state_generation"}).
		AddRow("run-a", int64(7)).
		AddRow("run-b", int64(7))
	mock.ExpectQuery(`SELECT DISTINCT .+`).
		WithArgs("connector-a", "connector-a").
		WillReturnRows(rows)

	fence, err := d.LoadCDCStateFence(context.Background(), "_bruin_staging.cdc_state", "connector-a")
	require.NoError(t, err)
	assert.Equal(t, int64(7), fence.Generation)
	assert.Equal(t, []string{"run-a", "run-b"}, fence.RunEventIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCDCStateEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &SnowflakeDestination{db: db, database: "RAW_DB"}
	mock.ExpectExec(`DELETE FROM .+ WHERE "CONNECTOR_ID" = \? AND "EVENT_ID" IN \(\?, \?\)`).
		WithArgs("connector-a", "evt-1", "evt-2").
		WillReturnResult(sqlmock.NewResult(0, 2))

	require.NoError(t, d.DeleteCDCStateEvents(context.Background(), "_bruin_staging.cdc_state", "connector-a", []string{"evt-1", "evt-2"}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCDCStateEventsEmpty(t *testing.T) {
	d := &SnowflakeDestination{}
	require.NoError(t, d.DeleteCDCStateEvents(context.Background(), "_bruin_staging.cdc_state", "connector-a", nil))
}

func TestClaimCDCTargetIdempotentAndRejectsForeignOwner(t *testing.T) {
	t.Run("claims empty target", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &SnowflakeDestination{db: db, database: "RAW_DB"}
		claim := destination.CDCTargetClaim{
			DestinationTable: "RAW_REPLAY.USERS",
			ConnectorID:      "connector-a",
			SourceTable:      "public.users",
		}
		ownerID, err := claim.OwnerID()
		require.NoError(t, err)
		canonical, err := d.CanonicalCDCTarget(context.Background(), claim.DestinationTable)
		require.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectExec(`MERGE INTO .+`).
			WithArgs(canonical, ownerID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT "CONNECTOR_ID" FROM .+`).
			WithArgs(canonical).
			WillReturnRows(sqlmock.NewRows([]string{"connector_id"}).AddRow(ownerID))
		mock.ExpectCommit()

		require.NoError(t, d.ClaimCDCTarget(context.Background(), "_bruin_staging.cdc_targets", claim))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rejects foreign owner", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &SnowflakeDestination{db: db, database: "RAW_DB"}
		claim := destination.CDCTargetClaim{
			DestinationTable: "RAW_REPLAY.USERS",
			ConnectorID:      "connector-b",
			SourceTable:      "public.users",
		}
		ownerID, err := claim.OwnerID()
		require.NoError(t, err)
		canonical, err := d.CanonicalCDCTarget(context.Background(), claim.DestinationTable)
		require.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectExec(`MERGE INTO .+`).
			WithArgs(canonical, ownerID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT "CONNECTOR_ID" FROM .+`).
			WithArgs(canonical).
			WillReturnRows(sqlmock.NewRows([]string{"connector_id"}).AddRow("other-owner"))
		mock.ExpectRollback()

		err = d.ClaimCDCTarget(context.Background(), "_bruin_staging.cdc_targets", claim)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already claimed")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCDCTargetIncarnation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &SnowflakeDestination{db: db, database: "RAW_DB"}
	created := time.Date(2026, 8, 11, 10, 0, 0, 123456789, time.UTC)
	mock.ExpectQuery(`SELECT CREATED FROM "RAW_DB"\.INFORMATION_SCHEMA\.TABLES WHERE TABLE_SCHEMA = \? AND TABLE_NAME = \? AND TABLE_TYPE = 'BASE TABLE'`).
		WithArgs("RAW_REPLAY", "TEST_RUNS").
		WillReturnRows(sqlmock.NewRows([]string{"CREATED"}).AddRow(created))

	got, exists, err := d.CDCTargetIncarnation(context.Background(), "raw_replay.test_runs")
	require.NoError(t, err)
	assert.True(t, exists)
	want := destination.CDCTargetKey("RAW_DB", "RAW_REPLAY", "TEST_RUNS", strconv.FormatInt(created.UnixNano(), 10))
	assert.Equal(t, want, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCDCTargetIncarnationMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &SnowflakeDestination{db: db, database: "RAW_DB"}
	mock.ExpectQuery(`SELECT CREATED FROM .+`).
		WithArgs("RAW_REPLAY", "MISSING").
		WillReturnError(sql.ErrNoRows)

	got, exists, err := d.CDCTargetIncarnation(context.Background(), "raw_replay.missing")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIsSnowflakeMissingObject(t *testing.T) {
	assert.True(t, isSnowflakeMissingObject(errors.New("Object 'FOO' does not exist or not authorized")))
	assert.True(t, isSnowflakeMissingObject(errors.New("SQL compilation error: ... (002003)")))
	assert.False(t, isSnowflakeMissingObject(errors.New("authentication failed")))
	assert.False(t, isSnowflakeMissingObject(nil))
}
