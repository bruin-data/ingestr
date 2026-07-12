package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

type mockPreStageWriter struct {
	discarded bool
}

func (m *mockPreStageWriter) Append(_ context.Context, _ arrow.RecordBatch) error {
	return nil
}

func (m *mockPreStageWriter) Finish() (destination.PreStagedData, error) {
	return nil, nil
}

func (m *mockPreStageWriter) Discard() {
	m.discarded = true
}

type mockPreStageDestination struct {
	mockDestination
	writer   destination.PreStageWriter
	err      error
	lastOpts *destination.PreStageOptions
}

func (m *mockPreStageDestination) NewPreStageWriter(_ context.Context, opts destination.PreStageOptions) (destination.PreStageWriter, error) {
	m.lastOpts = &opts
	if m.err != nil {
		return nil, m.err
	}
	return m.writer, nil
}

func preStageTestPipeline(cfg *config.IngestConfig, dest destination.Destination) *Pipeline {
	p := New(cfg)
	p.dest = dest
	return p
}

func baselinePreStageConfig() *config.IngestConfig {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mongodb://localhost/db"
	cfg.DestURI = "bigquery://project"
	cfg.SourceTable = "db.users"
	cfg.DestTable = "ds.users"
	return cfg
}

func TestMaybeStartPreStageHappyPath(t *testing.T) {
	cfg := baselinePreStageConfig()
	dest := &mockPreStageDestination{writer: &mockPreStageWriter{}}
	p := preStageTestPipeline(cfg, dest)

	ts := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	writer, transform := p.maybeStartPreStage(context.Background(), config.StrategyMerge, []string{"_id"}, ts)
	if writer == nil || transform == nil {
		t.Fatal("expected pre-staging to start")
	}
	if transform("userName") != "user_name" {
		t.Fatalf("transform(userName) = %q, want user_name (snake_case default)", transform("userName"))
	}
	if dest.lastOpts == nil || !dest.lastOpts.StagingTable {
		t.Fatal("merge strategy must pre-stage with staging-table chunking")
	}
	if dest.lastOpts.LoadTimestampColumn != naming.IngestrLoadedAtColumn {
		t.Fatalf("LoadTimestampColumn = %q", dest.lastOpts.LoadTimestampColumn)
	}
	if !dest.lastOpts.LoadTimestamp.Equal(ts) {
		t.Fatalf("LoadTimestamp = %v, want %v", dest.lastOpts.LoadTimestamp, ts)
	}
}

func TestMaybeStartPreStageGates(t *testing.T) {
	ts := time.Now()

	cases := []struct {
		name     string
		mutate   func(*config.IngestConfig)
		strategy config.IncrementalStrategy
	}{
		{"disabled by flag", func(c *config.IngestConfig) { c.DisablePreStaging = true }, config.StrategyMerge},
		{"scd2 strategy", func(c *config.IngestConfig) {}, config.StrategySCD2},
		{"delete+insert strategy", func(c *config.IngestConfig) {}, config.StrategyDeleteInsert},
		{"stream mode", func(c *config.IngestConfig) { c.Stream = true }, config.StrategyMerge},
		{"masking", func(c *config.IngestConfig) { c.Mask = []string{"email:hash"} }, config.StrategyMerge},
		{"trim whitespace", func(c *config.IngestConfig) { c.TrimWhitespace = true }, config.StrategyMerge},
		{"column overrides", func(c *config.IngestConfig) { c.Columns = "id:int64" }, config.StrategyMerge},
		{"discard contract", func(c *config.IngestConfig) { c.SchemaContract = "discard_row" }, config.StrategyMerge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baselinePreStageConfig()
			tc.mutate(cfg)
			dest := &mockPreStageDestination{writer: &mockPreStageWriter{}}
			p := preStageTestPipeline(cfg, dest)

			writer, _ := p.maybeStartPreStage(context.Background(), tc.strategy, []string{"_id"}, ts)
			if writer != nil {
				t.Fatal("expected pre-staging to be skipped")
			}
		})
	}
}

func TestMaybeStartPreStageSkipsNonPreStagerDestination(t *testing.T) {
	cfg := baselinePreStageConfig()
	p := preStageTestPipeline(cfg, &mockDestination{})

	writer, _ := p.maybeStartPreStage(context.Background(), config.StrategyMerge, nil, time.Now())
	if writer != nil {
		t.Fatal("expected pre-staging to be skipped for non-PreStager destination")
	}
}

func TestMaybeStartPreStageSkipsWhenUnsupported(t *testing.T) {
	cfg := baselinePreStageConfig()
	dest := &mockPreStageDestination{err: destination.ErrPreStageUnsupported}
	p := preStageTestPipeline(cfg, dest)

	writer, _ := p.maybeStartPreStage(context.Background(), config.StrategyMerge, nil, time.Now())
	if writer != nil {
		t.Fatal("expected pre-staging to be skipped when destination reports unsupported")
	}
}

func TestResolvePreStageKeyTransform(t *testing.T) {
	t.Run("explicit direct", func(t *testing.T) {
		cfg := baselinePreStageConfig()
		cfg.SchemaNaming = "direct"
		p := preStageTestPipeline(cfg, &mockDestination{})
		transform := p.resolvePreStageKeyTransform(context.Background())
		if transform == nil {
			t.Fatal("expected transform for direct naming")
		}
		if transform("userName") != "userName" {
			t.Fatal("direct naming must not rename")
		}
	})

	t.Run("auto with existing destination table", func(t *testing.T) {
		cfg := baselinePreStageConfig()
		p := preStageTestPipeline(cfg, &mockDestination{tableSchema: &schema.TableSchema{Name: "users"}})
		if transform := p.resolvePreStageKeyTransform(context.Background()); transform != nil {
			t.Fatal("auto naming with an existing table depends on the inferred schema; transform must be nil")
		}
	})

	t.Run("auto without destination table", func(t *testing.T) {
		cfg := baselinePreStageConfig()
		p := preStageTestPipeline(cfg, &mockDestination{})
		transform := p.resolvePreStageKeyTransform(context.Background())
		if transform == nil {
			t.Fatal("expected snake_case transform when destination table does not exist")
		}
		if transform("userName") != "user_name" {
			t.Fatalf("transform(userName) = %q", transform("userName"))
		}
	})
}

func identityTransform(name string) string { return name }

func simpleSchema(cols ...schema.Column) *schema.TableSchema {
	return &schema.TableSchema{Name: "users", Columns: cols}
}

func TestPreStagedUsableHappyPath(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})

	src := simpleSchema(
		schema.Column{Name: "_id", DataType: schema.TypeString},
		schema.Column{Name: "value", DataType: schema.TypeInt64},
	)
	write := simpleSchema(
		schema.Column{Name: "_id", DataType: schema.TypeString},
		schema.Column{Name: "value", DataType: schema.TypeInt64},
	)

	if !p.preStagedUsable(&preStageReport{}, identityTransform, src, write) {
		t.Fatal("expected pre-staged data to be usable")
	}
}

func TestPreStagedUsableRejectsTypePromotion(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})
	report := &preStageReport{typeUnstableColumns: []string{"value"}}

	if p.preStagedUsable(report, identityTransform, simpleSchema(), simpleSchema()) {
		t.Fatal("expected rejection for type-promoted columns")
	}
}

func TestPreStagedUsableRejectsRenameMismatch(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})
	// The renamer decided on a different final name than the transform assumed.
	p.columnRenamer = transformer.NewColumnRenamer(map[string]string{"userName": "user_name_2"})

	src := simpleSchema(schema.Column{Name: "userName", DataType: schema.TypeString})
	if p.preStagedUsable(&preStageReport{}, naming.Get(naming.SnakeCase).Normalize, src, simpleSchema()) {
		t.Fatal("expected rejection when renamer output differs from staged keys")
	}
}

func TestPreStagedUsableAcceptsMatchingRenames(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})
	p.columnRenamer = transformer.NewColumnRenamer(map[string]string{"userName": "user_name"})

	src := simpleSchema(
		schema.Column{Name: "userName", DataType: schema.TypeString},
		schema.Column{Name: "_id", DataType: schema.TypeString},
	)
	write := simpleSchema(
		schema.Column{Name: "user_name", DataType: schema.TypeString},
		schema.Column{Name: "_id", DataType: schema.TypeString},
	)

	if !p.preStagedUsable(&preStageReport{}, naming.Get(naming.SnakeCase).Normalize, src, write) {
		t.Fatal("expected matching renames to be usable")
	}
}

func TestPreStagedUsableRejectsNameCollision(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})

	src := simpleSchema(
		schema.Column{Name: "userName", DataType: schema.TypeString},
		schema.Column{Name: "user_name", DataType: schema.TypeString},
	)
	if p.preStagedUsable(&preStageReport{}, naming.Get(naming.SnakeCase).Normalize, src, simpleSchema()) {
		t.Fatal("expected rejection when two source columns collide after renaming")
	}
}

func TestPreStagedUsableRejectsLoadTimestampCollision(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})

	src := simpleSchema(schema.Column{Name: "_INGESTR_LOADED_AT", DataType: schema.TypeString})
	if p.preStagedUsable(&preStageReport{}, identityTransform, src, simpleSchema()) {
		t.Fatal("expected rejection when a source column collides with the load timestamp column")
	}
}

func TestPreStagedUsableRejectsLegacyIngestrColumns(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})
	p.ingestrColumnFiller = schemaevolution.NewIngestrColumnFiller([]string{"_ingestr_extracted_at"})

	if p.preStagedUsable(&preStageReport{}, identityTransform, simpleSchema(), simpleSchema()) {
		t.Fatal("expected rejection when legacy ingestr columns must be filled")
	}
}

func TestPreStagedUsableUnknownStorageColumns(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})

	cases := []struct {
		name     string
		dataType schema.DataType
		want     bool
	}{
		{"json is fine", schema.TypeJSON, true},
		{"int64 is fine", schema.TypeInt64, true},
		{"float64 is fine", schema.TypeFloat64, true},
		{"boolean is fine", schema.TypeBoolean, true},
		{"timestamp rejected", schema.TypeTimestampTZ, false},
		{"date rejected", schema.TypeDate, false},
		{"string rejected", schema.TypeString, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := &preStageReport{unknownStorageColumns: map[string]bool{"payload": true}}
			src := simpleSchema(schema.Column{Name: "payload", DataType: tc.dataType})
			write := simpleSchema(schema.Column{Name: "payload", DataType: tc.dataType})

			got := p.preStagedUsable(report, identityTransform, src, write)
			if got != tc.want {
				t.Fatalf("preStagedUsable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPreStagedUsableAllowsDroppedUnknownColumns(t *testing.T) {
	p := preStageTestPipeline(baselinePreStageConfig(), &mockDestination{})

	report := &preStageReport{unknownStorageColumns: map[string]bool{"dropped_col": true}}
	src := simpleSchema(schema.Column{Name: "_id", DataType: schema.TypeString})
	write := simpleSchema(schema.Column{Name: "_id", DataType: schema.TypeString})

	if !p.preStagedUsable(report, identityTransform, src, write) {
		t.Fatal("expected dropped unknown columns to be allowed (IgnoreUnknownValues)")
	}
}
