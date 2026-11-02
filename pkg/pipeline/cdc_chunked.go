package pipeline

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
)

func usesStreamingExecutor(cfg *config.IngestConfig) bool {
	return cfg.Stream || cfg.CDCChunkedSnapshot
}

func resolveMultiTableStrategy(cfg *config.IngestConfig, src source.MultiTableSource) config.IncrementalStrategy {
	if cfg.CDCChunkedSnapshot {
		return config.StrategyMerge
	}
	resolved := cfg.IncrementalStrategy
	if cfg.Stream && resolved == "" {
		if streaming, ok := src.(source.StreamingSource); ok {
			resolved = streaming.DefaultStreamingStrategy()
		}
	}
	if isCDCSource(cfg.SourceURI) && (!cfg.FullRefresh || cfg.Stream) && (resolved == "" || resolved == config.StrategyReplace) {
		resolved = config.StrategyMerge
	}
	if cfg.FullRefresh && !cfg.Stream {
		resolved = config.StrategyReplace
	}
	return resolved
}

func (p *Pipeline) validateCDCChunkedSnapshotSource(ctx context.Context, src any, tables []source.SourceTableInfo) error {
	if !p.config.CDCChunkedSnapshot {
		return nil
	}
	validator, ok := src.(source.CDCChunkedSnapshotValidator)
	if !ok {
		return fmt.Errorf("source does not support chunked CDC snapshot validation")
	}
	if _, ok := src.(source.StreamCommitter); !ok {
		return fmt.Errorf("chunked CDC snapshots require source commit acknowledgments")
	}
	for _, table := range tables {
		if err := strategy.ValidateCDCChunkedSnapshotTable(table.Name, table.Schema, table.PrimaryKeys); err != nil {
			return err
		}
	}
	return validator.ValidateCDCChunkedSnapshot(ctx, tables)
}
