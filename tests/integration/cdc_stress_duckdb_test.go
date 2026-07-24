//go:build stress

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/require"
)

type cdcStressPipeline struct {
	name   string
	config *config.IngestConfig
}

type cdcStressRunResult struct {
	err      error
	duration time.Duration
}

func runCDCStressPipelines(ctx context.Context, pipelines []cdcStressPipeline) []cdcStressRunResult {
	results := make([]cdcStressRunResult, len(pipelines))
	var wg sync.WaitGroup
	for i := range pipelines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			started := time.Now()
			results[i].err = pipeline.New(pipelines[i].config).Run(ctx)
			results[i].duration = time.Since(started)
		}(i)
	}
	wg.Wait()
	return results
}

func openCDCStressDuckDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := openCDCStressDuckDBPath(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openCDCStressDuckDBPath(path string) (*sql.DB, error) {
	return sql.Open("adbc_generic", "driver=duckdb;path="+path)
}

func withCDCStressDuckDB(path string, fn func(*sql.DB) error) error {
	db, err := openCDCStressDuckDBPath(path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return fn(db)
}

// withCDCStressDuckDBReadOnly opens a read-only monitoring connection. Any
// query that runs while a pipeline may still be writing the file must use
// this: in-process DuckDB instances do not conflict on the file lock, and a
// read-write open replays and truncates the writer's WAL, corrupting the
// database under load.
func withCDCStressDuckDBReadOnly(path string, fn func(*sql.DB) error) error {
	db, err := sql.Open("adbc_generic", "driver=duckdb;path="+path+";access_mode=read_only")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return fn(db)
}

func canonicalizeCDCStressJSON(raw string) (string, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	normalized, err := normalizeCDCStressJSONValue(value)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeCDCStressJSONValue(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		rational, ok := new(big.Rat).SetString(value.String())
		if !ok {
			return nil, fmt.Errorf("invalid JSON number %q", value)
		}
		return map[string]string{"$number": rational.RatString()}, nil
	case string:
		if normalized, ok := normalizeCDCStressTime(value); ok {
			return map[string]string{"$time": normalized}, nil
		}
		return value, nil
	case []any:
		for i := range value {
			normalized, err := normalizeCDCStressJSONValue(value[i])
			if err != nil {
				return nil, err
			}
			value[i] = normalized
		}
		return value, nil
	case map[string]any:
		for key, item := range value {
			normalized, err := normalizeCDCStressJSONValue(item)
			if err != nil {
				return nil, err
			}
			value[key] = normalized
		}
		return value, nil
	default:
		return value, nil
	}
}

func normalizeCDCStressTime(value string) (string, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999Z07",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), true
		}
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), true
		}
	}
	return "", false
}
