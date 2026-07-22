package pipeline

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// preStageReport captures the inference facts needed to decide whether
// pre-staged load files (written with raw, pre-inference values) are safe to
// load into the final schema.
type preStageReport struct {
	typeUnstableColumns   []string
	unknownStorageColumns map[string]bool
}

// preStageStrategies are the write strategies whose write phase consumes the
// record stream verbatim (no strategy-injected columns and no data-dependent
// bookkeeping such as interval tracking), so an empty stream plus pre-staged
// files is equivalent.
func preStageStrategyAllowed(strategy config.IncrementalStrategy) bool {
	switch strategy {
	case config.StrategyMerge, config.StrategyReplace, config.StrategyAppend, config.StrategyTruncateInsert:
		return true
	default:
		return false
	}
}

// maybeStartPreStage starts extract-time load-file staging when the
// destination supports it and the run configuration guarantees that batches
// need no replay-time transformation the pre-stage writer cannot apply.
// It returns nil when pre-staging should not be attempted.
func (p *Pipeline) maybeStartPreStage(
	ctx context.Context,
	strategy config.IncrementalStrategy,
	_ []string,
	loadTimestamp time.Time,
) (destination.PreStageWriter, func(string) string) {
	if p.config.DisablePreStaging {
		return nil, nil
	}

	prestager, ok := p.dest.(destination.PreStager)
	if !ok {
		return nil, nil
	}

	if p.config.Stream || !preStageStrategyAllowed(strategy) {
		return nil, nil
	}
	if len(p.config.Mask) > 0 || p.config.TrimWhitespace {
		return nil, nil
	}
	if strings.TrimSpace(p.config.Columns) != "" {
		return nil, nil
	}
	if contract := strings.TrimSpace(p.config.SchemaContract); contract != "" && contract != "evolve" {
		return nil, nil
	}

	keyTransform := p.resolvePreStageKeyTransform(ctx)
	if keyTransform == nil {
		return nil, nil
	}

	loadTimestampColumn := ""
	if !p.config.NoLoadTimestamp {
		loadTimestampColumn = naming.IngestrLoadedAtColumn
	}

	usesStagingTable := strategy == config.StrategyMerge ||
		strategy == config.StrategyReplace ||
		strategy == config.StrategyTruncateInsert

	writer, err := prestager.NewPreStageWriter(ctx, destination.PreStageOptions{
		Table:               p.config.DestTable,
		KeyTransform:        keyTransform,
		LoadTimestampColumn: loadTimestampColumn,
		LoadTimestamp:       loadTimestamp,
		StagingTable:        usesStagingTable,
		StagingBucket:       p.config.StagingBucket,
		LoaderFileSize:      p.config.LoaderFileSize,
		LoaderFileFormat:    p.config.LoaderFileFormat,
		Parallelism:         p.config.EffectiveDestinationParallelism(),
	})
	if err != nil {
		if !errors.Is(err, destination.ErrPreStageUnsupported) {
			config.Debug("[PIPELINE] Pre-staging unavailable: %v", err)
		}
		return nil, nil
	}

	config.Debug("[PIPELINE] Pre-staging load files during extract")
	return writer, keyTransform
}

// resolvePreStageKeyTransform returns the column-name transform the naming
// convention will apply, when that is already determined before the source
// schema is known. It mirrors resolveNamingConvention: an explicit convention
// is fixed, and "auto" resolves to snake_case unless the destination table
// already exists (in which case detection depends on the inferred schema and
// pre-staging is skipped).
func (p *Pipeline) resolvePreStageKeyTransform(ctx context.Context) func(string) string {
	convention, err := naming.ParseConvention(p.config.SchemaNaming)
	if err != nil {
		return nil
	}

	if convention == naming.Auto {
		destSchema, err := p.dest.GetTableSchema(ctx, p.config.DestTable)
		if err == nil && destSchema != nil {
			return nil
		}
		convention = naming.SnakeCase
	}

	return naming.Get(convention).Normalize
}

// preStagedUsable verifies, after inference and name resolution, that the
// assumptions the pre-stage writer made at extract time hold:
//   - no column needed type promotion across batches (raw early values might
//     not coerce into the promoted type),
//   - no unknown-storage column resolved to a temporal type (ingestr's date
//     parsing is more lenient than BigQuery's load-time parsing),
//   - the assumed key transform produced exactly the final column names, with
//     no collisions, and no legacy ingestr metadata columns need filling,
//   - no source column collides with the injected load-timestamp column.
func (p *Pipeline) preStagedUsable(
	report *preStageReport,
	keyTransform func(string) string,
	originalSourceSchema *schema.TableSchema,
	writeSchema *schema.TableSchema,
) bool {
	if report == nil || keyTransform == nil || originalSourceSchema == nil || writeSchema == nil {
		return false
	}

	if len(report.typeUnstableColumns) > 0 {
		config.Debug("[PIPELINE] Pre-staged files unusable: type promotion on columns %v", report.typeUnstableColumns)
		return false
	}

	if p.ingestrColumnFiller != nil && p.ingestrColumnFiller.HasColumns() {
		config.Debug("[PIPELINE] Pre-staged files unusable: legacy ingestr columns need filling")
		return false
	}

	var renames map[string]string
	if p.columnRenamer != nil && p.columnRenamer.HasRenames() {
		renames = p.columnRenamer.Mapping()
	}
	finalName := func(original string) string {
		if renamed, ok := renames[original]; ok {
			return renamed
		}
		return original
	}

	seen := make(map[string]bool, len(originalSourceSchema.Columns))
	for _, col := range originalSourceSchema.Columns {
		if strings.EqualFold(col.Name, naming.IngestrLoadedAtColumn) {
			config.Debug("[PIPELINE] Pre-staged files unusable: source column %q collides with the load timestamp column", col.Name)
			return false
		}
		final := finalName(col.Name)
		if keyTransform(col.Name) != final {
			config.Debug("[PIPELINE] Pre-staged files unusable: column %q resolved to %q, staged as %q", col.Name, final, keyTransform(col.Name))
			return false
		}
		lower := strings.ToLower(final)
		if seen[lower] {
			config.Debug("[PIPELINE] Pre-staged files unusable: column name collision on %q", final)
			return false
		}
		seen[lower] = true
	}

	if len(report.unknownStorageColumns) > 0 {
		finalTypes := make(map[string]schema.DataType, len(writeSchema.Columns))
		for _, col := range writeSchema.Columns {
			finalTypes[strings.ToLower(col.Name)] = col.DataType
		}
		for name := range report.unknownStorageColumns {
			dt, ok := finalTypes[strings.ToLower(finalName(name))]
			if !ok {
				continue // dropped column; IgnoreUnknownValues handles it
			}
			switch dt {
			// Temporal: ingestr's date parsing accepts formats BigQuery's
			// load-time parsing rejects. String: an unknown column can resolve
			// to string via a merge conflict (mixed scalar types), leaving raw
			// JSON numbers in the staged files that won't coerce to STRING.
			case schema.TypeTimestamp, schema.TypeTimestampTZ, schema.TypeDate, schema.TypeTime, schema.TypeString:
				config.Debug("[PIPELINE] Pre-staged files unusable: column %q resolved to %s from JSON-encoded values", name, dt)
				return false
			}
		}
	}

	return true
}
