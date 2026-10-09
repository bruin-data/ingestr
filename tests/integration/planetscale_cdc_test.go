//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/mysqluri"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // register adbc_generic for DuckDB read-back
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestPlanetScaleCDC_EnumSetBit_SnapshotAndIncremental_DuckDB(t *testing.T) {
	db, keyspace, sourceURI := openPlanetScaleCDCTestDB(t)
	ctx := t.Context()
	table := fmt.Sprintf("ingestr_cdc_types_%d", time.Now().UnixNano())
	execSource := func(statement string) {
		t.Helper()
		queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, err := db.ExecContext(queryCtx, statement)
		require.NoError(t, err)
	}
	execSource(`CREATE TABLE ` + table + ` (
		id INT NOT NULL PRIMARY KEY,
		enum_value ENUM('', 'draft', 'live', '123', 'comma,value') NULL,
		set_value SET('red', 'green', 'blue') NULL,
		bit1 BIT(1) NULL,
		bit9 BIT(9) NULL,
		bit64 BIT(64) NULL
	)`)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := db.ExecContext(cleanupCtx, "DROP TABLE "+table)
		require.NoError(t, err, "failed to remove PlanetScale test table %s", table)
	})
	execSource(`INSERT INTO ` + table + ` VALUES
		(1, 'live', 'blue,red', b'1', 511, 18446744073709551615),
		(2, '', '', b'0', 0, 0),
		(3, NULL, NULL, NULL, NULL, NULL),
		(4, 'draft', 'green', b'1', 256, 9223372036854775808),
		(5, '123', 'green,blue', b'0', 1, 1)
	`)

	duckPath := filepath.Join(t.TempDir(), "planetscale_cdc.duckdb")
	run := func() {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:   sourceURI,
			SourceTable: keyspace + "." + table,
			DestURI:     "duckdb:///" + duckPath,
			DestTable:   "main.items_dest",
		}).Run(runCtx))
	}

	type row struct {
		Enum    sql.NullString
		Set     sql.NullString
		Bit1    []byte
		Bit9    []byte
		Bit64   []byte
		Deleted bool
	}
	readDest := func() (map[int]row, map[int]string) {
		t.Helper()
		duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckPath)
		require.NoError(t, err)
		defer func() { require.NoError(t, duck.Close()) }()
		rows, err := duck.QueryContext(ctx, `SELECT id, enum_value, set_value, bit1, bit9, bit64, "_cdc_deleted", "_cdc_lsn" FROM main.items_dest`)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		values := make(map[int]row)
		positions := make(map[int]string)
		for rows.Next() {
			var id int
			var value row
			var position string
			require.NoError(t, rows.Scan(&id, &value.Enum, &value.Set, &value.Bit1, &value.Bit9, &value.Bit64, &value.Deleted, &position))
			require.NotContains(t, values, id, "CDC destination must have one row per primary key")
			require.NotEmpty(t, position)
			values[id] = value
			positions[id] = position
		}
		require.NoError(t, rows.Err())
		return values, positions
	}
	str := func(value string) sql.NullString { return sql.NullString{String: value, Valid: true} }
	maxBits := row{Enum: str("live"), Set: str("red,blue"), Bit1: []byte{1}, Bit9: []byte{1, 255}, Bit64: []byte{255, 255, 255, 255, 255, 255, 255, 255}}
	empty := row{Enum: str(""), Set: str(""), Bit1: []byte{0}, Bit9: []byte{0, 0}, Bit64: make([]byte, 8)}
	highBits := row{Enum: str("draft"), Set: str("green"), Bit1: []byte{1}, Bit9: []byte{1, 0}, Bit64: []byte{128, 0, 0, 0, 0, 0, 0, 0}}

	run()
	snapshot, snapshotPositions := readDest()
	require.Equal(t, map[int]row{
		1: maxBits,
		2: empty,
		3: {},
		4: highBits,
		5: {Enum: str("123"), Set: str("green,blue"), Bit1: []byte{0}, Bit9: []byte{0, 1}, Bit64: []byte{0, 0, 0, 0, 0, 0, 0, 1}},
	}, snapshot, "snapshot must preserve labels, empty strings, NULLs, and BIT widths")

	execSource(`UPDATE ` + table + ` SET enum_value = 'draft', set_value = 'green', bit1 = b'0', bit9 = 0, bit64 = 9223372036854775808 WHERE id = 1`)
	execSource(`UPDATE ` + table + ` SET enum_value = NULL, set_value = NULL, bit1 = NULL, bit9 = NULL, bit64 = NULL WHERE id = 2`)
	execSource(`UPDATE ` + table + ` SET enum_value = '', set_value = '', bit1 = b'0', bit9 = 0, bit64 = 0 WHERE id = 3`)
	execSource(`UPDATE ` + table + ` SET id = 44, enum_value = 'comma,value', set_value = 'blue,green,red', bit1 = b'1', bit9 = 511, bit64 = 18446744073709551615 WHERE id = 4`)
	execSource(`DELETE FROM ` + table + ` WHERE id = 5`)
	execSource(`INSERT INTO ` + table + ` VALUES
		(6, 'live', 'blue,red', b'1', 511, 18446744073709551615),
		(7, NULL, NULL, NULL, NULL, NULL),
		(8, '', '', b'0', 0, 0)
	`)

	run()
	incremental, incrementalPositions := readDest()
	require.Len(t, incremental, 9, "deletes and primary-key changes must retain tombstones")
	for _, id := range []int{4, 5} {
		deleted := snapshot[id]
		deleted.Deleted = true
		require.Equal(t, deleted, incremental[id], "tombstone must preserve values for id %d", id)
	}
	delete(incremental, 4)
	delete(incremental, 5)
	require.Equal(t, map[int]row{
		1:  {Enum: str("draft"), Set: str("green"), Bit1: []byte{0}, Bit9: []byte{0, 0}, Bit64: []byte{128, 0, 0, 0, 0, 0, 0, 0}},
		2:  {},
		3:  empty,
		6:  maxBits,
		7:  {},
		8:  empty,
		44: {Enum: str("comma,value"), Set: str("red,green,blue"), Bit1: []byte{1}, Bit9: []byte{1, 255}, Bit64: []byte{255, 255, 255, 255, 255, 255, 255, 255}},
	}, incremental, "change events must use the same representation as the snapshot")
	require.NotEqual(t, snapshotPositions[1], incrementalPositions[1], "updated rows must advance their CDC position")

	run()
	resumed, resumedPositions := readDest()
	require.Equal(t, incrementalPositions, resumedPositions, "an idle resume must not replay or resnapshot rows")
	delete(resumed, 4)
	delete(resumed, 5)
	require.Equal(t, incremental, resumed, "idle resume must preserve decoded values")
}

func TestPlanetScaleCDC_EnumSetBit_CompositePrimaryKey_DuckDB(t *testing.T) {
	db, keyspace, sourceURI := openPlanetScaleCDCTestDB(t)
	ctx := t.Context()
	table := fmt.Sprintf("ingestr_cdc_keys_%d", time.Now().UnixNano())
	execSource := func(statement string) {
		t.Helper()
		queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, err := db.ExecContext(queryCtx, statement)
		require.NoError(t, err)
	}
	execSource(`CREATE TABLE ` + table + ` (
		enum_key ENUM('2', '1', 'final') NOT NULL,
		set_key SET('red', 'blue') NOT NULL,
		bit_key BIT(9) NOT NULL,
		value INT NOT NULL,
		PRIMARY KEY (enum_key, set_key, bit_key)
	)`)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := db.ExecContext(cleanupCtx, "DROP TABLE "+table)
		require.NoError(t, err, "failed to remove PlanetScale test table %s", table)
	})
	execSource(`INSERT INTO ` + table + ` VALUES ('1', 'red', 1, 10), ('2', 'blue,red', 256, 20), ('final', '', 0, 30)`)

	duckPath := filepath.Join(t.TempDir(), "planetscale_cdc_keys.duckdb")
	run := func() {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:   sourceURI,
			SourceTable: keyspace + "." + table,
			DestURI:     "duckdb:///" + duckPath,
			DestTable:   "main.items_dest",
		}).Run(runCtx))
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
		rows, err := duck.QueryContext(ctx, `SELECT enum_key, set_key, bit_key, value, "_cdc_deleted" FROM main.items_dest ORDER BY enum_key, set_key, bit_key`)
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

	execSource(`UPDATE ` + table + ` SET enum_key = '2', set_key = 'blue', bit_key = 511, value = 11 WHERE enum_key = '1' AND set_key = 'red' AND bit_key = 1`)
	execSource(`DELETE FROM ` + table + ` WHERE enum_key = '2' AND set_key = 'red,blue' AND bit_key = 256`)
	run()
	expected := []row{
		{Enum: "1", Set: "red", Bit: []byte{0, 1}, Value: 10, Deleted: true},
		{Enum: "2", Set: "blue", Bit: []byte{1, 255}, Value: 11},
		{Enum: "2", Set: "red,blue", Bit: []byte{1, 0}, Value: 20, Deleted: true},
		{Enum: "final", Set: "", Bit: []byte{0, 0}, Value: 30},
	}
	require.Equal(t, expected, readDest(), "primary-key changes and deletes must match decoded composite keys")

	execSource(`INSERT INTO ` + table + ` VALUES ('1', 'red', 1, 12)`)
	run()
	expected[0].Value = 12
	expected[0].Deleted = false
	require.Equal(t, expected, readDest(), "resumed insert must restore the existing composite key without duplicates")
}

func openPlanetScaleCDCTestDB(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	rawURI := os.Getenv("PLANETSCALE_TEST_URI")
	if rawURI == "" {
		t.Skip("PLANETSCALE_TEST_URI is not set")
	}
	u, err := mysqluri.ParseURL(rawURI)
	if err != nil {
		t.Fatal("PLANETSCALE_TEST_URI is not a valid URI")
	}
	u.Scheme = "ps_mysql"
	params := u.Query()
	params.Del("mode")
	params.Del("dest_schema")
	u.RawQuery = params.Encode()
	dsn, keyspace, err := mysqluri.ToDSN(u.String())
	require.NoError(t, err)
	require.NotEmpty(t, keyspace, "PLANETSCALE_TEST_URI must include a database")
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(1)

	u.Scheme = "ps_mysql+cdc"
	params.Set("mode", "batch")
	u.RawQuery = params.Encode()
	return db, keyspace, u.String()
}
