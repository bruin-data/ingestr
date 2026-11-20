package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCDCSnapshotSpoolValidation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*IngestConfig)
		want   string
	}{
		{name: "destination"},
		{name: "memory alias", change: func(c *IngestConfig) { c.CDCWALStaging = "memory" }},
		{name: "inmemory", change: func(c *IngestConfig) { c.CDCWALStaging = "inmemory" }},
		{name: "disk", change: func(c *IngestConfig) { c.CDCWALStaging, c.CDCWALSpoolDir = "disk", "/spool" }},
		{name: "disk directory required", change: func(c *IngestConfig) { c.CDCWALStaging = "disk" }, want: "cdc-wal-spool-dir"},
		{name: "unused directory", change: func(c *IngestConfig) { c.CDCWALSpoolDir = "/spool" }, want: "requires --cdc-snapshot-spool and --cdc-wal-staging disk"},
		{name: "disabled disk", change: func(c *IngestConfig) { c.CDCSnapshotSpool, c.CDCWALStaging = false, "disk" }, want: "disk requires --cdc-snapshot-spool"},
		{name: "chunked", change: func(c *IngestConfig) { c.CDCChunkedSnapshot = true }, want: "cannot be combined with --cdc-snapshot-chunked"},
		{name: "mysql", change: func(c *IngestConfig) { c.SourceURI = "mysql+cdc://source/db" }, want: "only supported for postgres+cdc"},
		{name: "never", change: func(c *IngestConfig) { c.CDCSnapshotMode = "never" }, want: "cannot be combined with CDC snapshot mode never"},
		{name: "external boundary", change: func(c *IngestConfig) { c.CDCStartLSN = "0/100" }, want: "cannot be combined with --cdc-start-lsn"},
		{name: "explicit replace", change: func(c *IngestConfig) { c.IncrementalStrategy, c.IncrementalStrategyExplicit = StrategyReplace, true }, want: "require merge"},
		{name: "append", change: func(c *IngestConfig) { c.IncrementalStrategy = StrategyAppend }, want: "require merge"},
		{name: "limit", change: func(c *IngestConfig) { c.SQLLimit = 1 }, want: "sql-limit"},
		{name: "exclude columns", change: func(c *IngestConfig) { c.SQLExcludeColumns = []string{"payload"} }, want: "sql-exclude-columns"},
		{name: "incremental key", change: func(c *IngestConfig) { c.IncrementalKey = "id" }, want: "incremental-key"},
		{name: "predicate", change: func(c *IngestConfig) { c.IncrementalPredicate = "id > 5" }, want: "incremental-predicate"},
		{name: "interval", change: func(c *IngestConfig) { now := time.Now(); c.IntervalStart = &now }, want: "interval-start"},
		{name: "partition", change: func(c *IngestConfig) { c.ExtractPartitionBy = "id" }, want: "extract-partition-by"},
		{name: "negative buffer", change: func(c *IngestConfig) { c.CDCWALBufferBytes = -1 }, want: "cdc-wal-buffer-bytes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &IngestConfig{SourceURI: "postgres+cdc://source/db", DestURI: "postgres://dest/db", SourceTable: "public.orders", CDCSnapshotSpool: true}
			if tt.change != nil {
				tt.change(cfg)
			}
			err := cfg.Validate()
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				return
			}
			require.NoError(t, err)
			require.Positive(t, cfg.CDCWALBufferBytes)
			require.Positive(t, cfg.FlushInterval)
			require.Positive(t, cfg.FlushRecords)
			if tt.name == "destination" {
				require.Equal(t, "destination", cfg.CDCWALStaging)
			}
			if tt.name == "memory alias" {
				require.Equal(t, "inmemory", cfg.CDCWALStaging)
			}
		})
	}
	require.False(t, DefaultConfig().CDCSnapshotSpool)
}
