package postgres_cdc

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/source"
)

func (r *CDCReader) finiteSnapshotReader(ctx context.Context, opts source.ReadOptions) (*MultiTableCDCReader, error) {
	incarnation, err := r.source.TableIncarnation(ctx, r.tableName)
	if err != nil {
		return nil, err
	}
	fingerprint, err := r.source.TableSchemaFingerprint(ctx, r.tableName)
	if err != nil {
		return nil, err
	}
	if opts.CDCResumeLSN != "" && resumeMetadataChanged(opts.CDCResumeIncarnation, opts.CDCResumeSchemaFingerprint, incarnation, fingerprint) {
		if err := r.source.requireSnapshotAllowed(); err != nil {
			return nil, err
		}
		opts.CDCResumeLSN = ""
		tableSchema, err := r.source.getTableSchema(ctx, r.tableName)
		if err != nil {
			return nil, err
		}
		tableSchema = addCDCColumns(tableSchema)
		if err := validateConfiguredKeys(ctx, r.source.queryPool, r.tableName, tableSchema, r.tableSchema.PrimaryKeys); err != nil {
			return nil, err
		}
		tableSchema.PrimaryKeys = r.tableSchema.PrimaryKeys
		r.tableSchema = tableSchema
	}
	table := source.SourceTableInfo{
		Name: r.tableName, Schema: r.tableSchema, PrimaryKeys: r.tableSchema.PrimaryKeys,
		Incarnation: incarnation, SchemaFingerprint: fingerprint,
	}
	cfg, legacySlots := singleTableSnapshotConfig(r.cdcConfig, r.tableName, opts)
	resumeLSNs := make(map[string]string)
	if opts.CDCResumeLSN != "" {
		position, err := parseStoredPostgresLSN(opts.CDCResumeLSN)
		if err != nil {
			return nil, fmt.Errorf("failed to parse resume LSN: %w", err)
		}
		resumeLSNs[r.tableName] = FormatLSN(position)
	}
	reader := NewMultiTableCDCReader(r.source, []source.SourceTableInfo{table}, cfg, resumeLSNs, opts.CDCSlotSuffix)
	reader.legacySlotCandidates = legacySlots
	if opts.CDCSnapshotResume != nil {
		if err := reader.setSnapshotResumes(ctx, map[string]source.CDCSnapshotProgress{r.tableName: *opts.CDCSnapshotResume}); err != nil {
			return nil, err
		}
	}
	return reader, nil
}

func singleTableSnapshotConfig(cfg CDCConfig, table string, opts source.ReadOptions) (CDCConfig, []string) {
	cfg.DiscoverInterval = 0
	if cfg.SlotName != "" {
		return cfg, nil
	}
	cfg.SlotName = generateSlotName(table, cfg.Publication, opts.CDCSlotSuffix)
	if opts.CDCLegacySlotSuffix != "" {
		candidate := generateLegacySlotName(table, cfg.Publication, opts.CDCLegacySlotSuffix)
		if legacySlotNameUnambiguous(candidate, opts.CDCLegacySlotSuffix) {
			return cfg, []string{candidate}
		}
	}
	return cfg, nil
}
