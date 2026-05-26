package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamingCollision_LastWinsEarlierDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csv := "id,userID,userId,user_ID,createdAt\n" +
		"1,A1,B1,C1,t1\n" +
		"2,A2,B2,C2,t2\n"

	csvPath := filepath.Join(tmpDir, "input.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csv), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "input",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.input",
		IncrementalStrategy: config.StrategyReplace,
		SchemaNaming:        "snake_case",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	require.Equal(t, 3, len(types), "got: %v", types)
	assert.Contains(t, types, "id")
	assert.Contains(t, types, "user_id")
	assert.Contains(t, types, "created_at")
	assert.NotContains(t, types, "userid", "userID or userId leaked through; both should be dropped") // duckdb lowercases to userid

	assert.Equal(t, 2, readDuckDBRowCount(t, duckDBPath, "main.input"))

	// Surviving user_id carries user_ID's data (C1/C2), not userID's (A1/A2)
	// or userId's (B1/B2).
	db := openDuckDBForTest(t, duckDBPath)
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT id, user_id, created_at FROM main.input ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type row struct {
		id        int
		userID    string
		createdAt string
	}
	var got []row
	for rows.Next() {
		var r row
		var idBytes, userIDBytes, createdAtBytes sql.RawBytes
		require.NoError(t, rows.Scan(&idBytes, &userIDBytes, &createdAtBytes))
		_, err = fmt.Sscanf(string(idBytes), "%d", &r.id)
		require.NoError(t, err)
		r.userID = string(userIDBytes)
		r.createdAt = string(createdAtBytes)
		got = append(got, r)
	}
	require.NoError(t, rows.Err())

	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].id)
	assert.Equal(t, "C1", got[0].userID, "user_id must carry user_ID's data, not userID/userId")
	assert.Equal(t, "t1", got[0].createdAt)
	assert.Equal(t, 2, got[1].id)
	assert.Equal(t, "C2", got[1].userID)
	assert.Equal(t, "t2", got[1].createdAt)
}

func TestNamingCollision_WinnerAlreadyInSnakeCase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csv := "id,FirstName,FirstNAME,First_Name,first_name\n" +
		"1,camel,caps_tail,underscored,winner_1\n" +
		"2,camel2,caps_tail2,underscored2,winner_2\n"

	csvPath := filepath.Join(tmpDir, "input.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csv), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "input",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.input",
		IncrementalStrategy: config.StrategyReplace,
		SchemaNaming:        "snake_case",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	require.Equal(t, 2, len(types), "expected [id, first_name]; got: %v", types)
	assert.Contains(t, types, "id")
	assert.Contains(t, types, "first_name")

	// Row data: the winner is the last source column `first_name`.
	db := openDuckDBForTest(t, duckDBPath)
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT id, first_name FROM main.input ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var pairs [][2]string
	for rows.Next() {
		var idB, nameB sql.RawBytes
		require.NoError(t, rows.Scan(&idB, &nameB))
		pairs = append(pairs, [2]string{string(idB), string(nameB)})
	}
	require.NoError(t, rows.Err())

	require.Len(t, pairs, 2)
	assert.Equal(t, "winner_1", pairs[0][1], "first_name must carry the LAST source column's data")
	assert.Equal(t, "winner_2", pairs[1][1])
}
