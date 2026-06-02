package mmap

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/duckdbtest"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMMapSourceSingleFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	duckdbPath := filepath.Join(t.TempDir(), "output.duckdb")

	// source.arrow: 200 rows (ids 0-199), 2 batches of 100
	const totalRows = 200
	sourceURI := "mmap://" + filepath.Join("testdata", "source.arrow")
	destTableName := "mmap_events"

	src := NewMMapSource()
	require.NoError(t, src.Connect(ctx, sourceURI))
	t.Cleanup(func() { _ = src.Close(ctx) })

	table, err := src.GetTable(ctx, source.TableRequest{Name: "events"})
	require.NoError(t, err)
	require.True(t, table.HasKnownSchema())

	schema, err := table.GetSchema(ctx)
	require.NoError(t, err)

	duckdbtest.LockADBC(t)
	dest := duckdb.NewDuckDBDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("duckdb:///%s", duckdbPath)))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       destTableName,
		Schema:      schema,
		PrimaryKeys: []string{"id"},
	}))

	results, err := table.Read(ctx, source.ReadOptions{PageSize: 50})
	require.NoError(t, err)
	require.NoError(t, dest.Write(ctx, results, destination.WriteOptions{
		Table:       destTableName,
		PrimaryKeys: []string{"id"},
	}))

	db := openDuckDBForTest(t, duckdbPath)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", destTableName)).Scan(&count))
	assert.Equal(t, totalRows, count)

	var minID, maxID int64
	var scoreSum int64
	require.NoError(t, db.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT MIN(id), MAX(id), CAST(SUM(score) AS BIGINT) FROM %s", destTableName),
	).Scan(&minID, &maxID, &scoreSum))
	assert.Equal(t, int64(0), minID)
	assert.Equal(t, int64(totalRows-1), maxID)
	assert.Equal(t, expectedScoreSum(totalRows), scoreSum)

	var sampleName string
	var sampleActive bool
	var sampleScore int32
	require.NoError(t, db.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT name, score, is_active FROM %s WHERE id = ?", destTableName),
		42,
	).Scan(&sampleName, &sampleScore, &sampleActive))
	assert.Equal(t, "event_000042", sampleName)
	assert.Equal(t, int32(42%997), sampleScore)
	assert.True(t, sampleActive)
}

func TestMMapSourceGlobPattern(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	duckdbPath := filepath.Join(t.TempDir(), "output.duckdb")

	// part_001.arrow, part_002.arrow, part_003.arrow: 100 rows each (ids 0-299)
	const totalRows = 300
	sourceURI := "mmap://" + filepath.Join("testdata", "part_*.arrow")
	destTableName := "mmap_glob_events"

	src := NewMMapSource()
	require.NoError(t, src.Connect(ctx, sourceURI))
	require.Len(t, src.filePaths, 3)
	t.Cleanup(func() { _ = src.Close(ctx) })

	table, err := src.GetTable(ctx, source.TableRequest{Name: "events"})
	require.NoError(t, err)

	schema, err := table.GetSchema(ctx)
	require.NoError(t, err)

	duckdbtest.LockADBC(t)
	dest := duckdb.NewDuckDBDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("duckdb:///%s", duckdbPath)))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       destTableName,
		Schema:      schema,
		PrimaryKeys: []string{"id"},
	}))

	results, err := table.Read(ctx, source.ReadOptions{PageSize: 50})
	require.NoError(t, err)
	require.NoError(t, dest.Write(ctx, results, destination.WriteOptions{
		Table:       destTableName,
		PrimaryKeys: []string{"id"},
	}))

	db := openDuckDBForTest(t, duckdbPath)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", destTableName)).Scan(&count))
	assert.Equal(t, totalRows, count)

	var minID, maxID int64
	require.NoError(t, db.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT MIN(id), MAX(id) FROM %s", destTableName),
	).Scan(&minID, &maxID))
	assert.Equal(t, int64(0), minID)
	assert.Equal(t, int64(totalRows-1), maxID)
}

func expectedScoreSum(totalRows int) int64 {
	var sum int64
	for i := 0; i < totalRows; i++ {
		sum += int64(i % 997)
	}
	return sum
}

func openDuckDBForTest(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
