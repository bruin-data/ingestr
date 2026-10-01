package mssql

import (
	"errors"
	"fmt"
	"testing"

	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"
)

func TestExplainWriteError(t *testing.T) {
	keyTooLong := mssqldb.Error{Number: errIndexEntryTooLong, Message: "Operation failed. The index entry of length 1000 bytes for the index 'PK__t' exceeds the maximum length of 900 bytes for clustered indexes."}
	columnTooLong := mssqldb.Error{Number: errBulkCopyColumnTooLong, Message: "Received an invalid column length from the bcp client for colid 2."}
	nonclusteredKeyTooLong := mssqldb.Error{Number: errIndexEntryTooLong, Message: "Operation failed. The index entry of length 2000 bytes for the index 'PK__t' exceeds the maximum length of 1700 bytes for nonclustered indexes."}
	other := mssqldb.Error{Number: 2627, Message: "Violation of PRIMARY KEY constraint 'PK__t'."}

	tests := []struct {
		name    string
		err     error
		columns []string
		want    string
	}{
		{"key too long", fmt.Errorf("failed to execute merge: %w", keyTooLong), nil, "primary key value is too long: SQL Server keys hold at most 900 bytes (450 NVARCHAR characters), regardless of the declared column length: failed to execute merge: mssql: " + keyTooLong.Message},
		{"nonclustered key too long", nonclusteredKeyTooLong, nil, "primary key value is too long: SQL Server keys hold at most 1700 bytes (850 NVARCHAR characters), regardless of the declared column length: mssql: " + nonclusteredKeyTooLong.Message},
		{"key too long without limit in message", mssqldb.Error{Number: errIndexEntryTooLong, Message: "Operation failed."}, nil, "primary key value is too long: SQL Server keys hold at most 900 bytes (450 NVARCHAR characters), regardless of the declared column length: mssql: Operation failed."},
		{"column too long", columnTooLong, []string{"id", "attachment_key"}, `a value in column "attachment_key" is longer than the destination column allows: mssql: ` + columnTooLong.Message},
		{"column too long without names", columnTooLong, nil, "a value is longer than its destination column allows: mssql: " + columnTooLong.Message},
		{"colid out of range", columnTooLong, []string{"id"}, "a value is longer than its destination column allows: mssql: " + columnTooLong.Message},
		{"nested in All", mssqldb.Error{Number: 3621, Message: "The statement has been terminated.", All: []mssqldb.Error{keyTooLong}}, nil, "primary key value is too long: SQL Server keys hold at most 900 bytes (450 NVARCHAR characters), regardless of the declared column length: mssql: The statement has been terminated."},
		{"other SQL error", other, nil, "mssql: " + other.Message},
		{"non-SQL error", errors.New("boom"), nil, "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := explainWriteError(tt.err, tt.columns)
			require.EqualError(t, got, tt.want)
			require.Equal(t, errors.As(tt.err, new(mssqldb.Error)), errors.As(got, new(mssqldb.Error)))
		})
	}
	require.NoError(t, explainWriteError(nil, nil))
}
