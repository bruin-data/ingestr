//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // register adbc_generic for DuckDB read-back
	"github.com/stretchr/testify/require"
)

func TestMySQLCDC_EnumSetBitSpatial_SnapshotAndIncremental_MySQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	sourceContainer, sourceURI := setupMySQLCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	runMySQLCDCEnumSetBitSpatialTest(t, sourceURI)
}

func TestMySQLCDC_EnumSetBit_CompositePrimaryKey_MySQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	sourceContainer, sourceURI := setupMySQLCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	runMySQLCDCEnumSetBitCompositeKeyTest(t, sourceURI)
}

func openMySQLCDCTypesSource(t *testing.T, sourceURI string) (*sql.DB, func(string)) {
	t.Helper()
	db, err := sql.Open("mysql", mysqlDSN(sourceURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(statement string) {
		t.Helper()
		_, err := db.ExecContext(t.Context(), statement)
		require.NoError(t, err)
	}
	return db, exec
}

func runMySQLCDCTypesPipeline(t *testing.T, sourceURI, sourceTable, destURI, destTable string) {
	t.Helper()
	runCtx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	require.NoError(t, pipeline.New(&config.IngestConfig{
		SourceURI:   mysqlCDCURI(t, sourceURI, map[string]string{"server_id": "18889"}),
		SourceTable: sourceTable,
		DestURI:     destURI,
		DestTable:   destTable,
	}).Run(runCtx))
}

func runMySQLCDCEnumSetBitSpatialTest(t *testing.T, sourceURI string) {
	db, execSource := openMySQLCDCTypesSource(t, sourceURI)
	execSource(`CREATE TABLE cdc_types (
		id INT NOT NULL PRIMARY KEY,
		enum_value ENUM('', 'draft', 'live', '123', 'comma,value', 'it''s') NULL,
		set_value SET('red', 'green', 'blue') NULL,
		bit1 BIT(1) NULL,
		bit9 BIT(9) NULL,
		bit64 BIT(64) NULL,
		location POINT SRID 4326 NULL,
		shape GEOMETRY NULL
	)`)
	execSource(`INSERT INTO cdc_types VALUES
		(1, 'live', 'blue,red', b'1', 511, 18446744073709551615,
			ST_SRID(POINT(-73.9857, 40.7484), 4326),
			ST_GeomFromText('POLYGON((0 0,10 0,10 10,0 10,0 0),(2 2,3 2,3 3,2 2))')),
		(2, '', '', b'0', 0, 0, ST_SRID(POINT(0, 0), 4326), ST_GeomFromText('GEOMETRYCOLLECTION EMPTY')),
		(3, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
		(4, 'draft', 'green', b'1', 256, 9223372036854775808,
			ST_SRID(POINT(-0.5, 51.25), 4326),
			ST_GeomFromText('MULTIPOINT((1.5 2.5),(3 4))')),
		(5, 'it''s', 'green,blue', b'0', 1, 1,
			ST_SRID(POINT(139.6917, 35.6895), 4326),
			ST_GeomFromText('GEOMETRYCOLLECTION(POINT(1 2),LINESTRING(0 0,1 1))'))
	`)

	type row struct {
		Enum     sql.NullString
		Set      sql.NullString
		Bit1     []byte
		Bit9     []byte
		Bit64    []byte
		Location sql.NullString
		Shape    sql.NullString
		Deleted  bool
	}
	readDest := func() (map[int]row, map[int]string) {
		t.Helper()
		rows, err := db.QueryContext(t.Context(), "SELECT id, enum_value, set_value, bit1, bit9, bit64, location, shape, `_cdc_deleted`, `_cdc_lsn` FROM cdc_types_dest")
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		values := make(map[int]row)
		positions := make(map[int]string)
		for rows.Next() {
			var id int
			var value row
			var position string
			require.NoError(t, rows.Scan(&id, &value.Enum, &value.Set, &value.Bit1, &value.Bit9, &value.Bit64, &value.Location, &value.Shape, &value.Deleted, &position))
			require.NotContains(t, values, id, "CDC destination must have one row per primary key")
			values[id] = value
			positions[id] = position
		}
		require.NoError(t, rows.Err())
		return values, positions
	}
	str := func(value string) sql.NullString { return sql.NullString{String: value, Valid: true} }
	maxBits := row{
		Enum: str("live"), Set: str("red,blue"), Bit1: []byte{1}, Bit9: []byte{1, 255}, Bit64: []byte{255, 255, 255, 255, 255, 255, 255, 255},
		Location: str("POINT(-73.9857 40.7484)"), Shape: str("POLYGON((0 0,10 0,10 10,0 10,0 0),(2 2,3 2,3 3,2 2))"),
	}
	empty := row{
		Enum: str(""), Set: str(""), Bit1: []byte{0}, Bit9: []byte{0, 0}, Bit64: make([]byte, 8),
		Location: str("POINT(0 0)"), Shape: str("GEOMETRYCOLLECTION EMPTY"),
	}
	highBits := row{
		Enum: str("draft"), Set: str("green"), Bit1: []byte{1}, Bit9: []byte{1, 0}, Bit64: []byte{128, 0, 0, 0, 0, 0, 0, 0},
		Location: str("POINT(-0.5 51.25)"), Shape: str("MULTIPOINT((1.5 2.5),(3 4))"),
	}

	runMySQLCDCTypesPipeline(t, sourceURI, "cdc_types", sourceURI, "cdc_types_dest")
	snapshot, snapshotPositions := readDest()
	require.Equal(t, map[int]row{
		1: maxBits,
		2: empty,
		3: {},
		4: highBits,
		5: {
			Enum: str("it's"), Set: str("green,blue"), Bit1: []byte{0}, Bit9: []byte{0, 1}, Bit64: []byte{0, 0, 0, 0, 0, 0, 0, 1},
			Location: str("POINT(139.6917 35.6895)"), Shape: str("GEOMETRYCOLLECTION(POINT(1 2),LINESTRING(0 0,1 1))"),
		},
	}, snapshot, "snapshot must preserve labels, empty strings, NULLs, BIT widths, and geometries")

	for id, value := range snapshot {
		var location, shape sql.NullString
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT ST_AsText(location, 'axis-order=long-lat'), ST_AsText(shape) FROM cdc_types WHERE id = ?`, id).Scan(&location, &shape))
		require.Equal(t, location, value.Location, "geometry WKT for id %d must match MySQL's longitude-latitude ST_AsText", id)
		require.Equal(t, shape, value.Shape, "geometry WKT for id %d must match MySQL's ST_AsText", id)
	}

	execSource(`UPDATE cdc_types SET enum_value = 'draft', set_value = 'green', bit1 = b'0', bit9 = 0, bit64 = 9223372036854775808,
		location = ST_SRID(POINT(-0.5, 51.25), 4326), shape = ST_GeomFromText('LINESTRING(0 0,1.25 -3.5)') WHERE id = 1`)
	execSource(`UPDATE cdc_types SET enum_value = NULL, set_value = NULL, bit1 = NULL, bit9 = NULL, bit64 = NULL, location = NULL, shape = NULL WHERE id = 2`)
	execSource(`UPDATE cdc_types SET enum_value = '', set_value = '', bit1 = b'0', bit9 = 0, bit64 = 0,
		location = ST_SRID(POINT(0, 0), 4326), shape = ST_GeomFromText('GEOMETRYCOLLECTION EMPTY') WHERE id = 3`)
	execSource(`UPDATE cdc_types SET id = 44, enum_value = 'comma,value', set_value = 'blue,green,red', bit1 = b'1', bit9 = 511, bit64 = 18446744073709551615,
		shape = ST_GeomFromText('MULTIPOLYGON(((0 0,1 0,1 1,0 0)),((5 5,6 5,6 6,5 5)))') WHERE id = 4`)
	execSource(`DELETE FROM cdc_types WHERE id = 5`)
	execSource(`INSERT INTO cdc_types VALUES
		(6, 'live', 'blue,red', b'1', 511, 18446744073709551615,
			ST_SRID(POINT(-73.9857, 40.7484), 4326),
			ST_GeomFromText('POLYGON((0 0,10 0,10 10,0 10,0 0),(2 2,3 2,3 3,2 2))')),
		(7, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
		(8, '', '', b'0', 0, 0, ST_SRID(POINT(0, 0), 4326), ST_GeomFromText('GEOMETRYCOLLECTION EMPTY'))
	`)
	// Non-strict mode stores an unknown ENUM label as index 0, which reads back as ''.
	nonStrict, err := db.Conn(t.Context())
	require.NoError(t, err)
	_, err = nonStrict.ExecContext(t.Context(), `SET SESSION sql_mode = ''`)
	require.NoError(t, err)
	_, err = nonStrict.ExecContext(t.Context(), `INSERT INTO cdc_types (id, enum_value) VALUES (9, 'unknown')`)
	require.NoError(t, err)
	require.NoError(t, nonStrict.Close())

	runMySQLCDCTypesPipeline(t, sourceURI, "cdc_types", sourceURI, "cdc_types_dest")
	incremental, incrementalPositions := readDest()
	require.Len(t, incremental, 10, "deletes and primary-key changes must retain tombstones")
	for _, id := range []int{4, 5} {
		deleted := snapshot[id]
		deleted.Deleted = true
		require.Equal(t, deleted, incremental[id], "tombstone must preserve values for id %d", id)
	}
	delete(incremental, 4)
	delete(incremental, 5)
	require.Equal(t, map[int]row{
		1: {
			Enum: str("draft"), Set: str("green"), Bit1: []byte{0}, Bit9: []byte{0, 0}, Bit64: []byte{128, 0, 0, 0, 0, 0, 0, 0},
			Location: str("POINT(-0.5 51.25)"), Shape: str("LINESTRING(0 0,1.25 -3.5)"),
		},
		2: {},
		3: empty,
		6: maxBits,
		7: {},
		8: empty,
		9: {Enum: str("")},
		44: {
			Enum: str("comma,value"), Set: str("red,green,blue"), Bit1: []byte{1}, Bit9: []byte{1, 255}, Bit64: []byte{255, 255, 255, 255, 255, 255, 255, 255},
			Location: str("POINT(-0.5 51.25)"), Shape: str("MULTIPOLYGON(((0 0,1 0,1 1,0 0)),((5 5,6 5,6 6,5 5)))"),
		},
	}, incremental, "binlog changes must use the same representation as the snapshot")
	require.NotEqual(t, snapshotPositions[1], incrementalPositions[1], "updated rows must advance their CDC position")

	runMySQLCDCTypesPipeline(t, sourceURI, "cdc_types", sourceURI, "cdc_types_dest")
	resumed, resumedPositions := readDest()
	require.Equal(t, incrementalPositions, resumedPositions, "an idle resume must not replay or resnapshot rows")
	delete(resumed, 4)
	delete(resumed, 5)
	require.Equal(t, incremental, resumed, "idle resume must preserve decoded values")
}

func runMySQLCDCEnumSetBitCompositeKeyTest(t *testing.T, sourceURI string) {
	_, execSource := openMySQLCDCTypesSource(t, sourceURI)
	execSource(`CREATE TABLE cdc_type_keys (
		enum_key ENUM('2', '1', 'final') NOT NULL,
		set_key SET('red', 'blue') NOT NULL,
		bit_key BIT(9) NOT NULL,
		value INT NOT NULL,
		PRIMARY KEY (enum_key, set_key, bit_key)
	)`)
	execSource(`INSERT INTO cdc_type_keys VALUES ('1', 'red', 1, 10), ('2', 'blue,red', 256, 20), ('final', '', 0, 30)`)

	// MySQL destinations cannot key on BLOB columns, so the BIT key lands in DuckDB.
	duckPath := filepath.Join(t.TempDir(), "mysql_cdc_keys.duckdb")
	run := func() {
		t.Helper()
		runMySQLCDCTypesPipeline(t, sourceURI, "cdc_type_keys", "duckdb:///"+duckPath, "main.cdc_type_keys_dest")
	}
	type row struct {
		Enum    string
		Set     string
		Bit     []byte
		Value   int
		Deleted bool
	}
	readDest := func() []row {
		t.Helper()
		duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckPath)
		require.NoError(t, err)
		defer func() { require.NoError(t, duck.Close()) }()
		rows, err := duck.QueryContext(t.Context(), `SELECT enum_key, set_key, bit_key, value, "_cdc_deleted" FROM main.cdc_type_keys_dest ORDER BY enum_key, set_key, bit_key`)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		var values []row
		for rows.Next() {
			var value row
			require.NoError(t, rows.Scan(&value.Enum, &value.Set, &value.Bit, &value.Value, &value.Deleted))
			values = append(values, value)
		}
		require.NoError(t, rows.Err())
		return values
	}

	run()
	require.Equal(t, []row{
		{Enum: "1", Set: "red", Bit: []byte{0, 1}, Value: 10},
		{Enum: "2", Set: "red,blue", Bit: []byte{1, 0}, Value: 20},
		{Enum: "final", Set: "", Bit: []byte{0, 0}, Value: 30},
	}, readDest(), "snapshot must decode composite primary-key values")

	execSource(`UPDATE cdc_type_keys SET enum_key = '2', set_key = 'blue', bit_key = 511, value = 11 WHERE enum_key = '1' AND set_key = 'red' AND bit_key = 1`)
	execSource(`DELETE FROM cdc_type_keys WHERE enum_key = '2' AND set_key = 'red,blue' AND bit_key = 256`)
	run()
	expected := []row{
		{Enum: "1", Set: "red", Bit: []byte{0, 1}, Value: 10, Deleted: true},
		{Enum: "2", Set: "blue", Bit: []byte{1, 255}, Value: 11},
		{Enum: "2", Set: "red,blue", Bit: []byte{1, 0}, Value: 20, Deleted: true},
		{Enum: "final", Set: "", Bit: []byte{0, 0}, Value: 30},
	}
	require.Equal(t, expected, readDest(), "primary-key changes and deletes must match decoded composite keys")

	execSource(`INSERT INTO cdc_type_keys VALUES ('1', 'red', 1, 12)`)
	run()
	expected[0].Value = 12
	expected[0].Deleted = false
	require.Equal(t, expected, readDest(), "resumed insert must restore the existing composite key without duplicates")
}
