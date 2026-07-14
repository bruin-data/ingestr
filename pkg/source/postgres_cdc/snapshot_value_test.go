package postgres_cdc

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type failedSnapshotRows struct{ err error }

func (r *failedSnapshotRows) Close()                                       {}
func (r *failedSnapshotRows) Err() error                                   { return r.err }
func (r *failedSnapshotRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *failedSnapshotRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *failedSnapshotRows) Next() bool                                   { return false }
func (r *failedSnapshotRows) Scan(...any) error                            { return nil }
func (r *failedSnapshotRows) Values() ([]any, error)                       { return nil, nil }
func (r *failedSnapshotRows) RawValues() [][]byte                          { return nil }
func (r *failedSnapshotRows) Conn() *pgx.Conn                              { return nil }

func TestConvertSnapshotValueRejectsPostgresInfinity(t *testing.T) {
	tests := []struct {
		name     string
		value    pgtype.InfinityModifier
		dataType schema.DataType
	}{
		{name: "date positive", value: pgtype.Infinity, dataType: schema.TypeDate},
		{name: "date negative", value: pgtype.NegativeInfinity, dataType: schema.TypeDate},
		{name: "timestamp positive", value: pgtype.Infinity, dataType: schema.TypeTimestamp},
		{name: "timestamp negative", value: pgtype.NegativeInfinity, dataType: schema.TypeTimestamp},
		{name: "timestamptz positive", value: pgtype.Infinity, dataType: schema.TypeTimestampTZ},
		{name: "timestamptz negative", value: pgtype.NegativeInfinity, dataType: schema.TypeTimestampTZ},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := convertValue(tt.value, schema.Column{Name: "occurred_at", DataType: tt.dataType})
			require.ErrorContains(t, err, "not representable")
			require.Nil(t, value)
		})
	}
}

func TestConvertSnapshotValueRejectsSpecialNumericValues(t *testing.T) {
	tests := []struct {
		name  string
		value pgtype.Numeric
	}{
		{name: "NaN", value: pgtype.Numeric{NaN: true, Valid: true}},
		{name: "positive infinity", value: pgtype.Numeric{InfinityModifier: pgtype.Infinity, Valid: true}},
		{name: "negative infinity", value: pgtype.Numeric{InfinityModifier: pgtype.NegativeInfinity, Valid: true}},
		{name: "missing coefficient", value: pgtype.Numeric{Valid: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convertValue(tt.value, schema.Column{DataType: schema.TypeDecimal, Precision: 38, Scale: 9})
			require.Error(t, err)
		})
	}

	got, err := convertValue(pgtype.Numeric{Int: big.NewInt(123), Exp: -2, Valid: true}, schema.Column{DataType: schema.TypeDecimal, Precision: 38, Scale: 2})
	require.NoError(t, err)
	require.Equal(t, big.NewInt(123), got)
}

func TestSnapshotRowsToBatchDoesNotTreatFirstRowDecodeErrorAsEmpty(t *testing.T) {
	sentinel := errors.New("cached row description used the pre-DDL type")
	columns := append([]schema.Column{{Name: "id", DataType: schema.TypeInt64}}, cdcMetaColumns()...)
	snapshot := &Snapshot{tableSchema: &schema.TableSchema{Columns: columns}}

	record, count, err := snapshot.rowsToBatch(
		&failedSnapshotRows{err: sentinel},
		buildArrowSchema(columns),
		columns[:1],
		100,
		"0/1",
		time.Now(),
	)

	require.ErrorIs(t, err, sentinel)
	require.Nil(t, record)
	require.Zero(t, count)
}
