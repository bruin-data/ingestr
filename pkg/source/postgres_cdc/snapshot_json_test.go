package postgres_cdc

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type jsonSnapshotRows struct {
	failedSnapshotRows
	field   pgconn.FieldDescription
	values  [][]byte
	index   int
	buffer  []byte
	current []byte
}

func (r *jsonSnapshotRows) Next() bool {
	if r.index == len(r.values) {
		return false
	}
	value := r.values[r.index]
	r.index++
	r.current = nil
	if value != nil {
		r.buffer = append(r.buffer[:0], value...)
		r.current = r.buffer
	}
	return true
}

func (r *jsonSnapshotRows) RawValues() [][]byte {
	return [][]byte{r.current}
}

func (r *jsonSnapshotRows) FieldDescriptions() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{r.field}
}

func TestSnapshotJSONPreservesWireValuesAndOwnsBuffers(t *testing.T) {
	values := [][]byte{
		nil,
		[]byte(`null`),
		[]byte(`"null"`),
		[]byte(`"quoted \"value\", newline\n and Unicode \u263a"`),
		[]byte(`true`),
		[]byte(`false`),
		[]byte(`9007199254740993`),
		[]byte(`123456789012345678901234567890.12345678901234567890`),
		[]byte(`{"z": [null, false, "x"], "number": 9007199254740993}`),
		[]byte(`[1, 1.00000000000000000001, {"x":"<>&"}]`),
		[]byte(" {\n  \"duplicate\": 1, \"duplicate\": 2\n } "),
	}
	for _, tc := range []struct {
		name   string
		oid    uint32
		format int16
	}{
		{name: "json text", oid: pgtype.JSONOID, format: pgx.TextFormatCode},
		{name: "json binary", oid: pgtype.JSONOID, format: pgx.BinaryFormatCode},
		{name: "jsonb text", oid: pgtype.JSONBOID, format: pgx.TextFormatCode},
		{name: "jsonb binary", oid: pgtype.JSONBOID, format: pgx.BinaryFormatCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			columns := []schema.Column{{Name: "payload", DataType: schema.TypeJSON, Nullable: true}}
			table := addCDCColumns(&schema.TableSchema{Columns: columns})
			snapshot := &Snapshot{tableSchema: table}
			rows := &jsonSnapshotRows{field: pgconn.FieldDescription{Name: "payload", DataTypeOID: tc.oid, Format: tc.format}}
			for _, value := range values {
				if value != nil && tc.oid == pgtype.JSONBOID && tc.format == pgx.BinaryFormatCode {
					value = append([]byte{1}, value...)
				}
				rows.values = append(rows.values, value)
			}
			var batches []arrow.RecordBatch
			defer func() {
				for _, batch := range batches {
					batch.Release()
				}
			}()
			for {
				batch, count, _, err := snapshot.rowsToBatch(rows, buildArrowSchema(table.Columns), columns, 0, 3, 0, "0/1", time.Unix(0, 0))
				require.NoError(t, err)
				if batch == nil {
					require.Zero(t, count)
					break
				}
				batches = append(batches, batch)
			}
			for i := range rows.buffer {
				rows.buffer[i] = '#'
			}
			index := 0
			for _, batch := range batches {
				storage := batch.Column(0).(array.ExtensionArray).Storage().(*array.String)
				for i := range int(batch.NumRows()) {
					require.Equal(t, values[index] == nil, storage.IsNull(i), "SQL NULL and JSON null must remain distinct")
					if values[index] != nil {
						require.Equal(t, string(values[index]), storage.Value(i), "wire JSON must survive later pgx rows and retained batches")
					}
					index++
				}
			}
			require.Equal(t, len(values), index)
		})
	}
}

func TestSnapshotJSONRejectsInvalidBinaryJSONBHeader(t *testing.T) {
	for _, value := range [][]byte{{}, {1}, {2, '{', '}'}, {0, 'n', 'u', 'l', 'l'}} {
		columns := []schema.Column{{Name: "payload", DataType: schema.TypeJSON}}
		table := addCDCColumns(&schema.TableSchema{Columns: columns})
		snapshot := &Snapshot{tableSchema: table}
		rows := &jsonSnapshotRows{
			field:  pgconn.FieldDescription{Name: "payload", DataTypeOID: pgtype.JSONBOID, Format: pgx.BinaryFormatCode},
			values: [][]byte{value},
			buffer: make([]byte, 0, 16),
		}
		batch, _, _, err := snapshot.rowsToBatch(rows, buildArrowSchema(table.Columns), columns, 0, 10, 0, "0/1", time.Unix(0, 0))
		require.ErrorContains(t, err, `invalid binary JSONB in snapshot column "payload"`)
		require.Nil(t, batch)
	}
}

func TestSnapshotJSONBatchBudgetUsesEncodedBytes(t *testing.T) {
	columns := []schema.Column{{Name: "payload", DataType: schema.TypeJSON}}
	table := addCDCColumns(&schema.TableSchema{Columns: columns})
	snapshot := &Snapshot{tableSchema: table}
	value := []byte(`{"escaped":"` + strings.Repeat(`\u0061`, 1024) + `"}`)
	rows := &jsonSnapshotRows{
		field:  pgconn.FieldDescription{Name: "payload", DataTypeOID: pgtype.JSONBOID, Format: pgx.TextFormatCode},
		values: [][]byte{value, value, value},
	}
	for range 3 {
		batch, count, _, err := snapshot.rowsToBatch(rows, buildArrowSchema(table.Columns), columns, 0, 100, 2048, "0/1", time.Unix(0, 0))
		require.NoError(t, err)
		require.EqualValues(t, 1, count, "each encoded row exceeds the byte budget even though its decoded string is small")
		batch.Release()
	}
}

func BenchmarkSnapshotJSONRowsToBatch(b *testing.B) {
	columns := []schema.Column{{Name: "payload", DataType: schema.TypeJSON}}
	table := addCDCColumns(&schema.TableSchema{Columns: columns})
	snapshot := &Snapshot{tableSchema: table}
	arrowSchema := buildArrowSchema(table.Columns)
	item := []byte(`{"sku":"item","qty":2,"price":123.45,"tracking":"0123456789abcdef0123456789abcdef"},`)
	payload := append([]byte(`{"items":[`), bytes.Repeat(item, 300)...)
	payload[len(payload)-1] = ']'
	payload = append(payload, '}')
	values := make([][]byte, 100)
	for i := range values {
		values[i] = payload
	}
	b.SetBytes(int64(len(payload) * len(values)))
	b.ReportAllocs()
	for b.Loop() {
		rows := &jsonSnapshotRows{field: pgconn.FieldDescription{DataTypeOID: pgtype.JSONBOID}, values: values}
		batch, _, _, err := snapshot.rowsToBatch(rows, arrowSchema, columns, 0, len(values), 0, "0/1", time.Unix(0, 0))
		if err != nil {
			b.Fatal(err)
		}
		batch.Release()
	}
}
