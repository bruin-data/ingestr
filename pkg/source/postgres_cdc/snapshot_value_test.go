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
		0,
		"0/1",
		time.Now(),
	)

	require.ErrorIs(t, err, sentinel)
	require.Nil(t, record)
	require.Zero(t, count)
}

func TestConvertSnapshotArrayRejectsInfinityElements(t *testing.T) {
	col := schema.Column{Name: "ts", DataType: schema.TypeArray, ArrayType: schema.TypeTimestamp}
	_, err := convertValue([]any{time.Unix(0, 0), pgtype.Infinity}, col)
	require.ErrorContains(t, err, "array element 1")
	got, err := convertValue([]any{nil, pgtype.Numeric{Int: big.NewInt(15), Exp: -1, Valid: true}}, schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Scale: 2})
	require.NoError(t, err)
	require.Equal(t, []any{nil, big.NewInt(150)}, got)
}

func TestDecimalRangeRejectsValuesTheColumnCannotHold(t *testing.T) {
	unbounded := schema.Column{Name: "n", DataType: schema.TypeDecimal, Scale: 9}
	require.NoError(t, checkDecimalRange("99999999999999999999999999999.999999999", unbounded))
	require.NoError(t, checkDecimalRange("-1.5", unbounded))
	require.NoError(t, checkDecimalRange("NaN", unbounded))
	require.ErrorContains(t, checkDecimalRange("100000000000000000000000000000", unbounded), "exceeds DECIMAL(38,9)")
	require.ErrorContains(t, checkDecimalRange("-1e35", unbounded), "exceeds DECIMAL(38,9)")
	require.ErrorContains(t, checkDecimalRange("1000", schema.Column{DataType: schema.TypeDecimal, Precision: 5, Scale: 2}), "exceeds DECIMAL(5,2)")

	_, err := convertValue(pgtype.Numeric{Int: big.NewInt(1), Exp: 30, Valid: true}, unbounded)
	require.ErrorContains(t, err, "exceeds DECIMAL(38,9)")
	_, err = convertTextValueWithMap("{1.5,1e35}", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Scale: 9}, nil)
	require.ErrorContains(t, err, "exceeds DECIMAL(38,9)")
}

func TestSnapshotNumericRoundsLikeWAL(t *testing.T) {
	require.Equal(t, big.NewInt(-1000000003), numericToBigInt(pgtype.Numeric{Int: big.NewInt(-10000000025), Exp: -10, Valid: true}, 9))
	require.Equal(t, big.NewInt(1000000000), numericToBigInt(pgtype.Numeric{Int: big.NewInt(100000000049), Exp: -11, Valid: true}, 9))
	boundary := schema.Column{DataType: schema.TypeDecimal, Scale: 9}
	require.ErrorContains(t, checkDecimalRange("99999999999999999999999999999.9999999999", boundary), "exceeds")
	require.NoError(t, checkDecimalRange("99999999999999999999999999999.9999999994", boundary))
}

func TestTextTimestampsAcceptYearsPast9999(t *testing.T) {
	ts, err := convertTextValueWithMap("10000-02-29 01:02:03.5+02", schema.Column{DataType: schema.TypeTimestampTZ}, nil)
	require.NoError(t, err)
	require.Equal(t, time.Date(10000, 2, 28, 23, 2, 3, 500000000, time.UTC), ts.(time.Time).UTC())
	d, err := convertTextValueWithMap("12345-06-07", schema.Column{DataType: schema.TypeDate}, nil)
	require.NoError(t, err)
	require.Equal(t, "12345-06-07", d.(time.Time).Format("2006-01-02"))
	arr, err := convertTextValueWithMap(`{"10001-01-01 00:00:00"}`, schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeTimestamp}, nil)
	require.NoError(t, err)
	require.Equal(t, 10001, arr.([]any)[0].(time.Time).Year())
	_, err = convertTextValueWithMap("not-a-date", schema.Column{DataType: schema.TypeDate}, nil)
	require.Error(t, err)
}
