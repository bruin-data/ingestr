package strategy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func ValidateCDCSnapshotSpoolDestination(dest destination.Destination) error {
	aware, ok := dest.(destination.CDCMergeAware)
	if !ok || !aware.SupportsCDCMerge() {
		return fmt.Errorf("spooled CDC snapshots require a destination with CDC-aware merge")
	}
	if _, ok := dest.(destination.TruncateCapable); !ok {
		return fmt.Errorf("spooled CDC snapshots require a destination supporting snapshot truncation")
	}
	return nil
}

func ValidateCDCSnapshotSpoolTable(table string, tableSchema *schema.TableSchema, keys []string) error {
	if tableSchema == nil || len(keys) == 0 {
		return fmt.Errorf("spooled CDC snapshots require primary keys for table %q", table)
	}
	for _, key := range keys {
		found := false
		for _, column := range tableSchema.Columns {
			if column.Name != key {
				continue
			}
			if column.Nullable {
				return fmt.Errorf("spooled CDC snapshots require non-null key %q in table %q", key, table)
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("spooled CDC snapshot key %q is missing from table %q", key, table)
		}
	}
	return nil
}

func ValidateCDCSnapshotSpoolResume(ctx context.Context, cfg *config.IngestConfig, manager *CDCStateManager) error {
	if cfg.CDCSnapshotSpool || cfg.FullRefresh || manager == nil {
		return nil
	}
	data, err := manager.ChunkStore().Load(ctx, "normal-spool/bootstrap")
	if err != nil {
		return fmt.Errorf("failed to load CDC snapshot spool checkpoint: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var checkpoint struct {
		Complete bool
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return fmt.Errorf("failed to decode CDC snapshot spool checkpoint: %w", err)
	}
	if !checkpoint.Complete {
		return fmt.Errorf("an incomplete CDC snapshot spool requires --cdc-snapshot-spool to resume; use --full-refresh to restart the snapshot")
	}
	return nil
}

func (e *StreamingExecutor) snapshotSpoolOptions(cfg *config.IngestConfig, dest destination.Destination) (*source.CDCSnapshotSpoolOptions, error) {
	if !cfg.CDCSnapshotSpool {
		return nil, nil
	}
	if e.opts.Strategy != config.StrategyMerge {
		return nil, fmt.Errorf("spooled CDC snapshots require the merge strategy")
	}
	if e.opts.StateManager == nil || e.opts.Committer == nil {
		return nil, fmt.Errorf("spooled CDC snapshots require destination-managed CDC state and source commit acknowledgments")
	}
	if err := ValidateCDCSnapshotSpoolDestination(dest); err != nil {
		return nil, err
	}
	cfg.ApplyCDCSnapshotSpoolDefaults()
	if e.opts.FlushInterval == 0 {
		e.opts.FlushInterval = cfg.FlushInterval
	}
	if e.opts.FlushRecords == 0 {
		e.opts.FlushRecords = int64(cfg.FlushRecords)
	}
	var refreshID string
	e.opts.StateManager.mu.Lock()
	if refresh := e.opts.StateManager.refresh; refresh != nil && !refresh.complete {
		refreshID = refresh.ID
	}
	e.opts.StateManager.mu.Unlock()
	return &source.CDCSnapshotSpoolOptions{
		WALStaging:     cfg.CDCWALStaging,
		WALBufferBytes: cfg.CDCWALBufferBytes,
		Directory:      cfg.CDCWALSpoolDir,
		Continuous:     cfg.Stream,
		RefreshID:      refreshID,
		Store:          e.opts.StateManager.ChunkStore(),
	}, nil
}
