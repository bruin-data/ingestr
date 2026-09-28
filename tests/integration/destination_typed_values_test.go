//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Feed explicitly typed Arrow values rather than JSONL inference, which cannot
// distinguish a decimal from a float or a timestamp from a string. Each type has
// an interior NULL so an off-by-one validity bitmap cannot hide behind counts.
func TestDestinations_TypedValues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	for _, tc := range destinationCases() {
		if tc.sqlBackend == nil && tc.name != "jsonl" && tc.name != "parquet" && tc.name != "mongodb" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			destURI, table, cleanup := tc.setup(t, ctx)
			defer cleanup()
			for _, typ := range []schema.DataType{schema.TypeString, schema.TypeDecimal, schema.TypeTimestampTZ, schema.TypeJSON, schema.TypeArray} {
				t.Run(typ.String(), func(t *testing.T) {
					if tc.name == "cratedb" && typ == schema.TypeDecimal {
						t.Skip("CrateDB has no decimal type")
					}
					if (tc.name == "clickhouse" || tc.name == "bigquery") && typ == schema.TypeArray {
						t.Skip("backend does not preserve nullable arrays with nullable elements")
					}
					col := schema.Column{Name: "name", DataType: typ, Nullable: true, Precision: 18, Scale: 4, ArrayType: schema.TypeInt64}
					ts := &schema.TableSchema{Name: table, Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}, col}}
					mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
					defer mem.AssertSize(t, 0)
					rb := array.NewRecordBuilder(mem, ts.ToArrowSchema())
					defer rb.Release()
					rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
					switch b := rb.Field(1).(type) {
					case *array.StringBuilder:
						b.Append("quote ' slash \\ unicode İstanbul\nline")
						b.AppendNull()
						b.Append("zero is not NULL")
					case *array.Decimal128Builder:
						for i, value := range []string{"1234567890123.4567", "0", "-0.0001"} {
							if i == 1 {
								b.AppendNull()
								continue
							}
							n, err := decimal128.FromString(value, 18, 4)
							require.NoError(t, err)
							b.Append(n)
						}
					case *array.TimestampBuilder:
						for i, value := range []string{"2024-02-29T00:15:30.123456+05:45", "", "2024-03-31T23:59:59.654321-07:00"} {
							if i == 1 {
								b.AppendNull()
								continue
							}
							valueTime, err := time.Parse(time.RFC3339Nano, value)
							require.NoError(t, err)
							b.Append(arrow.Timestamp(valueTime.UnixMicro()))
						}
					case *array.ExtensionBuilder:
						b.Builder.(*array.StringBuilder).Append(`{"nested":{"flag":true},"items":[7,null,"x"],"n":-2}`)
						b.AppendNull()
						b.Builder.(*array.StringBuilder).Append(`{}`)
					case *array.ListBuilder:
						b.Append(true)
						values := b.ValueBuilder().(*array.Int64Builder)
						values.Append(7)
						values.AppendNull()
						values.Append(-2)
						b.AppendNull()
						b.Append(true)
					default:
						t.Fatalf("missing fixture for %T", b)
					}

					func() {
						d, err := uri.DefaultRegistry.GetDestination(destURI)
						require.NoError(t, err)
						require.NoError(t, d.Connect(ctx, destURI))
						defer func() { require.NoError(t, d.Close(ctx)) }()
						require.NoError(t, d.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: ts, DropFirst: true}))
						records := make(chan source.RecordBatchResult, 1)
						records <- source.RecordBatchResult{Batch: rb.NewRecordBatch()}
						close(records)
						require.NoError(t, d.Write(ctx, records, destination.WriteOptions{Table: table, Schema: ts}))
					}()
					if tc.sqlBackend == nil {
						rows := readTypedNonSQL(t, tc.name, destURI, table)
						require.Len(t, rows, 3)
						seen := map[string]bool{}
						for _, row := range rows {
							id := fmt.Sprint(row["id"])
							require.False(t, seen[id], "duplicate id %s", id)
							seen[id] = true
							switch id {
							case "1":
								assertConformanceValue(t, tc.name, typ, 1, row["name"])
							case "2":
								assert.Nil(t, row["name"])
							case "3":
								assertConformanceValue(t, tc.name, typ, 3, row["name"])
							default:
								t.Fatalf("unexpected id %s", id)
							}
						}
						return
					}

					db, err := tc.sqlBackend.openDB(destURI)
					require.NoError(t, err)
					defer func() { _ = db.Close() }()
					if tc.sqlBackend.refreshTable != nil {
						tc.sqlBackend.refreshTable(db, table)
					}
					var count int
					require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(table)).Scan(&count))
					require.Equal(t, 3, count)
					var nullValue any
					require.NoError(t, db.QueryRow(tc.sqlBackend.nameByIDQuery(table, 2)).Scan(&nullValue))
					assert.Nil(t, nullValue, "SQL NULL must not become an empty/default value")
					for _, id := range []int{1, 3} {
						var value any
						query := tc.sqlBackend.nameByIDQuery(table, id)
						if tc.name == "duckdb" {
							// The ADBC database/sql adapter exposes decimals as unscaled
							// integers and loses list child validity. Ask DuckDB to encode
							// those values, keeping readback independent of that adapter.
							if typ == schema.TypeDecimal {
								query = strings.Replace(query, "name", "CAST(name AS VARCHAR)", 1)
							}
							if typ == schema.TypeArray {
								query = strings.Replace(query, "name", "to_json(name)", 1)
							}
						}
						require.NoError(t, db.QueryRow(query).Scan(&value))
						assertConformanceValue(t, tc.name, typ, id, value)
					}
				})
			}
		})
	}
}

func assertConformanceValue(t *testing.T, backend string, typ schema.DataType, id int, value any) {
	t.Helper()
	if b, ok := value.([]byte); ok {
		value = string(b)
	}
	s := fmt.Sprint(value)
	switch typ {
	case schema.TypeString:
		want := "quote ' slash \\ unicode İstanbul\nline"
		if id == 3 {
			want = "zero is not NULL"
		}
		assert.Equal(t, want, s)
	case schema.TypeDecimal:
		want := "1234567890123.4567"
		if id == 3 {
			want = "-0.0001"
		}
		actualRat, ok := new(big.Rat).SetString(s)
		require.True(t, ok, "invalid decimal %q", s)
		wantRat, ok := new(big.Rat).SetString(want)
		require.True(t, ok)
		if backend == "sqlite" {
			// SQLite deliberately maps DECIMAL to REAL (binary64).
			a, _ := actualRat.Float64()
			w, _ := wantRat.Float64()
			assert.Equal(t, w, a)
		} else {
			assert.Zero(t, wantRat.Cmp(actualRat), "decimal digits must survive exactly: got %s, want %s", s, want)
		}
	case schema.TypeTimestampTZ:
		want := "2024-02-28T18:30:30.123456Z"
		if id == 3 {
			want = "2024-04-01T06:59:59.654321Z"
		}
		wantTime, err := time.Parse(time.RFC3339Nano, want)
		require.NoError(t, err)
		actual, ok := value.(time.Time)
		if !ok {
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-07", "2006-01-02 15:04:05.999999999"} {
				actual, err = time.Parse(layout, s)
				if err == nil {
					break
				}
			}
			require.NoError(t, err, "unrecognized timestamp %q", s)
		}
		if backend == "cratedb" {
			wantTime = wantTime.Truncate(time.Millisecond)
		}
		if backend == "mongodb" {
			wantTime = wantTime.Truncate(time.Millisecond)
		}
		assert.True(t, actual.Equal(wantTime), "got %s, want instant %s", actual, wantTime)
	case schema.TypeJSON:
		if _, ok := value.(string); !ok {
			data, err := json.Marshal(value)
			require.NoError(t, err)
			s = string(data)
		}
		want := `{"nested":{"flag":true},"items":[7,null,"x"],"n":-2}`
		if id == 3 {
			want = `{}`
		}
		assert.JSONEq(t, want, s)
	case schema.TypeArray:
		if backend == "postgres" {
			want := "{7,NULL,-2}"
			if id == 3 {
				want = "{}"
			}
			assert.Equal(t, want, s)
			return
		}
		if _, ok := value.(string); !ok {
			data, err := json.Marshal(value)
			require.NoError(t, err)
			s = string(data)
		}
		want := `[7,null,-2]`
		if id == 3 {
			want = `[]`
		}
		assert.JSONEq(t, want, strings.ToLower(s))
	}
}

func readTypedNonSQL(t *testing.T, backend, destURI, table string) []map[string]any {
	t.Helper()
	if backend == "mongodb" {
		return readMongoDBConformance(t, destURI, table)
	}
	u, err := url.Parse(destURI)
	require.NoError(t, err)
	if backend == "jsonl" {
		return readJSONLConformance(t, u.Path)
	}
	f, err := os.Open(u.Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	pr, err := file.NewParquetReader(f)
	require.NoError(t, err)
	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	require.NoError(t, err)
	tbl, err := fr.ReadTable(context.Background())
	require.NoError(t, err)
	defer tbl.Release()
	r := array.NewTableReader(tbl, -1)
	defer r.Release()
	var rows []map[string]any
	for r.Next() {
		record := r.RecordBatch()
		for i := 0; i < int(record.NumRows()); i++ {
			var value any
			col := record.Column(1)
			if !col.IsNull(i) {
				if col.DataType().ID() == arrow.DECIMAL128 {
					value = col.ValueStr(i)
				} else {
					data, err := json.Marshal(col.GetOneForMarshal(i))
					require.NoError(t, err)
					require.NoError(t, json.Unmarshal(data, &value))
				}
			}
			rows = append(rows, map[string]any{"id": record.Column(0).GetOneForMarshal(i), "name": value})
		}
	}
	require.NoError(t, r.Err())
	return rows
}
