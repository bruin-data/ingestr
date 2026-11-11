package starrocks

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func expectStatePositionColumn(mock sqlmock.Sqlmock, length int64, nullable string) {
	mock.ExpectQuery("SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, IS_NULLABLE FROM information_schema.columns").
		WithArgs("state", "cdc_state", destination.CDCLSNColumn).
		WillReturnRows(sqlmock.NewRows([]string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "IS_NULLABLE"}).AddRow("varchar", length, nullable))
}

func expectStatePositionSchemaJob(mock sqlmock.Sqlmock, id, state string) {
	rows := sqlmock.NewRows([]string{"JobId", "State", "Msg"})
	if id != "" {
		rows.AddRow(id, state, "")
	}
	mock.ExpectQuery("SHOW ALTER TABLE COLUMN FROM `state` WHERE TableName = 'cdc_state'").WillReturnRows(rows)
}

func TestEnsureCDCStatePositionColumnWidensOnlyPosition(t *testing.T) {
	for _, nullable := range []string{"NO", "YES"} {
		t.Run(nullable, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectStatePositionColumn(mock, 65533, nullable)
			expectStatePositionSchemaJob(mock, "1", "FINISHED")
			nullability := " NOT NULL"
			if nullable == "YES" {
				nullability = " NULL"
			}
			mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE `state`.`cdc_state` MODIFY COLUMN `_cdc_lsn` VARCHAR(1048576)" + nullability)).WillReturnResult(sqlmock.NewResult(0, 0))
			expectStatePositionSchemaJob(mock, "2", "FINISHED")
			expectStatePositionColumn(mock, maxVarcharBytes, nullable)
			require.NoError(t, d.EnsureCDCStatePositionColumn(t.Context(), "state.cdc_state"))
		})
	}
}

func TestEnsureCDCStatePositionColumnKeepsCurrentWidth(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	expectStatePositionColumn(mock, maxVarcharBytes, "NO")
	require.NoError(t, d.EnsureCDCStatePositionColumn(t.Context(), "state.cdc_state"))
}

func TestEnsureCDCStatePositionColumnWaitsForExistingMigration(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	expectStatePositionColumn(mock, 256, "NO")
	expectStatePositionSchemaJob(mock, "2", "RUNNING")
	expectStatePositionSchemaJob(mock, "2", "FINISHED")
	expectStatePositionColumn(mock, maxVarcharBytes, "NO")
	require.NoError(t, d.EnsureCDCStatePositionColumn(t.Context(), "state.cdc_state"))
}

func TestEnsureCDCStatePositionColumnPropagatesMetadataFailure(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	want := errors.New("metadata unavailable")
	mock.ExpectQuery("SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, IS_NULLABLE FROM information_schema.columns").
		WithArgs("state", "cdc_state", destination.CDCLSNColumn).WillReturnError(want)
	require.ErrorIs(t, d.EnsureCDCStatePositionColumn(t.Context(), "state.cdc_state"), want)
}

func TestEnsureCDCStatePositionColumnRejectsIncompleteMigration(t *testing.T) {
	for _, state := range []string{"CANCELLED", "FINISHED"} {
		t.Run(state, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectStatePositionColumn(mock, 65533, "NO")
			expectStatePositionSchemaJob(mock, "1", "FINISHED")
			mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE `state`.`cdc_state` MODIFY COLUMN `_cdc_lsn` VARCHAR(1048576) NOT NULL")).WillReturnResult(sqlmock.NewResult(0, 0))
			expectStatePositionSchemaJob(mock, "2", state)
			if state == "FINISHED" {
				expectStatePositionColumn(mock, 65533, "NO")
			}
			require.Error(t, d.EnsureCDCStatePositionColumn(t.Context(), "state.cdc_state"))
		})
	}
}
