package pipeline

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
)

func (p *Pipeline) resolveCDCSnapshotMode(ctx context.Context, manager *strategy.CDCStateManager) error {
	if !isPostgresCDCSource(p.config.SourceURI) {
		return nil
	}
	mode, err := manager.ResolveSnapshotMode(ctx, p.config.CDCSnapshotMode)
	if err != nil {
		return err
	}
	p.config.CDCSnapshotMode = mode
	return p.config.ValidateCDCSnapshotMode()
}

// prepareCDCNoSnapshot validates all targets before any are adopted or mutated.
// It fills missing resume positions only for a completely uninitialized connector.
func (p *Pipeline) prepareCDCNoSnapshot(ctx context.Context, manager *strategy.CDCStateManager, tables []source.SourceTableInfo, destinations, positions map[string]string, writeStrategy config.IncrementalStrategy) (bool, error) {
	if p.config.CDCSnapshotMode != "never" {
		return false, nil
	}
	if writeStrategy != config.StrategyMerge {
		return false, fmt.Errorf("CDC snapshot mode never requires keyed merge")
	}
	if aware, ok := p.dest.(destination.CDCMergeAware); !ok || !aware.SupportsCDCMerge() {
		return false, fmt.Errorf("CDC snapshot mode never requires a destination with CDC-aware merge")
	}
	bootstrap := p.config.CDCStartLSN != ""
	if bootstrap {
		empty, err := manager.StateEmpty(ctx)
		if err != nil {
			return false, err
		}
		if !empty {
			return false, fmt.Errorf("--cdc-start-lsn cannot reset existing CDC state; omit it to resume")
		}
	}
	for _, table := range tables {
		if len(table.PrimaryKeys) == 0 {
			return false, fmt.Errorf("CDC snapshot mode never requires merge keys for %s", table.Name)
		}
		if bootstrap {
			positions[table.Name] = p.config.CDCStartLSN
		}
		if positions[table.Name] == "" {
			return false, fmt.Errorf("CDC snapshot mode never: no valid baseline for %s; a fresh connector requires --cdc-start-lsn and externally loaded data", table.Name)
		}
		destTable := destinations[table.Name]
		existing, err := p.dest.GetTableSchema(ctx, destTable)
		if err != nil {
			return false, err
		}
		if existing == nil {
			return false, fmt.Errorf("CDC snapshot mode never requires preloaded destination table %s", destTable)
		}
		opts := &schemaevolution.CompareOptions{PrimaryKeys: table.PrimaryKeys}
		if normalizer, ok := p.dest.(destination.SchemaEvolutionColumnNormalizer); ok {
			opts.NormalizeColumn = normalizer.NormalizeSchemaEvolutionColumn
		}
		if normalizer, ok := p.dest.(destination.SchemaEvolutionSourceColumnNormalizer); ok {
			opts.NormalizeSourceColumn = normalizer.NormalizeSchemaEvolutionSourceColumn
		}
		comparison, err := schemaevolution.Compare(destination.DestinationTableSchema(table.Schema), existing, opts)
		if err != nil {
			return false, fmt.Errorf("incompatible CDC baseline %s: %w", destTable, err)
		}
		for _, change := range comparison.Changes {
			metadata := destination.IsCDCMetaColumn(change.ColumnName) || naming.IsIngestrColumn(change.ColumnName)
			if change.Type == schemaevolution.ChangeRelaxNullability || metadata && change.Type == schemaevolution.ChangeAddColumn {
				continue
			}
			return false, fmt.Errorf("incompatible CDC baseline %s: column %s differs from the source; snapshot mode never cannot reconstruct existing rows", destTable, change.ColumnName)
		}
		if err := manager.BindDestinationIncarnation(ctx, table.Name, destTable); err != nil {
			return false, err
		}
	}
	noSnapshot, ok := p.src.(source.CDCNoSnapshotSource)
	if !ok {
		return false, fmt.Errorf("source does not support CDC snapshot mode never")
	}
	if err := noSnapshot.ConfigureNoSnapshot(ctx, positions); err != nil {
		return false, err
	}
	return bootstrap, nil
}

func (p *Pipeline) prepareCDCBaseline(ctx context.Context, manager *strategy.CDCStateManager, tables []source.SourceTableInfo, destinations map[string]string) error {
	for _, table := range tables {
		if err := p.dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: destinations[table.Name], Schema: destination.DestinationTableSchema(table.Schema),
			PrimaryKeys: table.PrimaryKeys, CDCKeys: table.PrimaryKeys, CDCMode: true, RequirePrimaryKeyMatch: true,
		}); err != nil {
			return fmt.Errorf("failed to prepare external CDC baseline: %w", err)
		}
		if err := manager.BindDestinationIncarnation(ctx, table.Name, destinations[table.Name]); err != nil {
			return err
		}
	}
	return nil
}
