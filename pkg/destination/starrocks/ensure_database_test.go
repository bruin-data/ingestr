package starrocks

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

const schemataQuery = "SELECT COUNT(*) FROM information_schema.schemata WHERE SCHEMA_NAME = ?"

func TestPrepareTableSkipsCreateForExistingDatabase(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	mock.ExpectQuery(regexp.QuoteMeta(schemataQuery)).WithArgs("staging").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "staging.events"}))
}

func TestPrepareTableCreatesMissingDatabase(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	mock.ExpectQuery(regexp.QuoteMeta(schemataQuery)).WithArgs("staging").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("CREATE DATABASE IF NOT EXISTS `staging`")).WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "staging.events"}))
}
