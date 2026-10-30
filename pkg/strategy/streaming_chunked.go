package strategy

import (
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func ValidateCDCChunkedSnapshotDestination(dest destination.Destination) error {
	aware, ok := dest.(destination.CDCMergeAware)
	if !ok || !aware.SupportsCDCMerge() {
		return fmt.Errorf("chunked CDC snapshots require a destination with CDC-aware merge")
	}
	if _, ok := dest.(destination.TruncateCapable); !ok {
		return fmt.Errorf("chunked CDC snapshots require a destination supporting snapshot truncation")
	}
	return nil
}

func ValidateCDCChunkedSnapshotTable(table string, tableSchema *schema.TableSchema, keys []string) error {
	if tableSchema == nil || len(keys) == 0 {
		return fmt.Errorf("chunked CDC snapshots require primary keys for table %q", table)
	}
	for _, key := range keys {
		found := false
		for _, column := range tableSchema.Columns {
			if column.Name != key {
				continue
			}
			if column.Nullable {
				return fmt.Errorf("chunked CDC snapshots require non-null key %q in table %q", key, table)
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("chunked CDC snapshot key %q is missing from table %q", key, table)
		}
	}
	return nil
}

func (e *StreamingExecutor) chunkedSnapshotOptions(cfg *config.IngestConfig, dest destination.Destination) (*source.CDCChunkedSnapshotOptions, error) {
	if !cfg.CDCChunkedSnapshot {
		return nil, nil
	}
	if e.opts.Strategy != config.StrategyMerge {
		return nil, fmt.Errorf("chunked CDC snapshots require the merge strategy")
	}
	if e.opts.StateManager == nil || e.opts.Committer == nil {
		return nil, fmt.Errorf("chunked CDC snapshots require destination-managed CDC state and source commit acknowledgments")
	}
	if err := ValidateCDCChunkedSnapshotDestination(dest); err != nil {
		return nil, err
	}
	cfg.ApplyCDCChunkedSnapshotDefaults()
	if e.opts.FlushInterval == 0 {
		e.opts.FlushInterval = cfg.FlushInterval
	}
	if e.opts.FlushRecords == 0 {
		e.opts.FlushRecords = int64(cfg.FlushRecords)
	}
	return &source.CDCChunkedSnapshotOptions{
		ChunkSize:      cfg.CDCSnapshotChunkSize,
		ChunkTimeout:   cfg.CDCSnapshotChunkTimeout,
		WALStaging:     cfg.CDCWALStaging,
		WALBufferBytes: cfg.CDCWALBufferBytes,
		Continuous:     cfg.Stream,
		Store:          e.opts.StateManager.ChunkStore(),
	}, nil
}
