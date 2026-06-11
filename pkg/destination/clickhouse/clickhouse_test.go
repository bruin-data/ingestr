package clickhouse

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestValidateEngineType(t *testing.T) {
	tests := []struct {
		name           string
		engineType     string
		hasPrimaryKeys bool
		wantEngine     string
		wantMergeTree  bool
	}{
		{"empty without PK defaults to MergeTree", "", false, "MergeTree()", true},
		{"empty with PK defaults to ReplacingMergeTree", "", true, "ReplacingMergeTree()", true},
		{"merge_tree", "merge_tree", false, "MergeTree()", true},
		{"replacing_merge_tree", "replacing_merge_tree", false, "ReplacingMergeTree()", true},
		{"shared_merge_tree", "shared_merge_tree", false, "SharedMergeTree()", true},
		{"replicated_merge_tree", "replicated_merge_tree", false, "ReplicatedMergeTree()", true},
		{"case insensitive", "MERGE_TREE", false, "MergeTree()", true},
		{"explicit engine overrides PK default", "merge_tree", true, "MergeTree()", true},
		{"unknown without PK falls back to MergeTree", "CustomEngine", false, "MergeTree()", true},
		{"unknown with PK falls back to ReplacingMergeTree", "CustomEngine", true, "ReplacingMergeTree()", true},
		{"tiny_log falls back to default", "tiny_log", false, "MergeTree()", true},
		{"CamelCase variant falls back to default", "ReplacingMergeTree", false, "MergeTree()", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEngine, gotMergeTree := validateEngineType(tt.engineType, tt.hasPrimaryKeys)
			if gotEngine != tt.wantEngine {
				t.Errorf("engine: got %q, want %q", gotEngine, tt.wantEngine)
			}
			if gotMergeTree != tt.wantMergeTree {
				t.Errorf("mergeTree: got %v, want %v", gotMergeTree, tt.wantMergeTree)
			}
		})
	}
}

func TestStrategySupport(t *testing.T) {
	t.Parallel()
	dest := NewClickHouseDestination()
	if !dest.SupportsReplaceStrategy() {
		t.Fatal("replace strategy should be supported")
	}
	if !dest.SupportsAppendStrategy() {
		t.Fatal("append strategy should be supported")
	}
	if !dest.SupportsMergeStrategy() {
		t.Fatal("merge strategy should be supported")
	}
	if !dest.SupportsDeleteInsertStrategy() {
		t.Fatal("delete+insert strategy should be supported")
	}
	if !dest.SupportsSCD2Strategy() {
		t.Fatal("scd2 strategy should be supported")
	}
}

func TestBuildEngineSettingsClause(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]string
		want     string
	}{
		{"empty", nil, ""},
		{"int emitted raw", map[string]string{"index_granularity": "8192"}, "SETTINGS index_granularity = 8192"},
		{"negative int emitted raw", map[string]string{"x": "-1"}, "SETTINGS x = -1"},
		{"float emitted raw", map[string]string{"merge_ratio": "0.5"}, "SETTINGS merge_ratio = 0.5"},
		{"bool true emitted raw lowercased", map[string]string{"allow_nullable_key": "True"}, "SETTINGS allow_nullable_key = true"},
		{"bool false emitted raw", map[string]string{"allow_nullable_key": "false"}, "SETTINGS allow_nullable_key = false"},
		{"string value quoted", map[string]string{"storage_policy": "default"}, "SETTINGS storage_policy = 'default'"},
		{
			"single quotes in value are escaped",
			map[string]string{"storage_policy": "cold'drop"},
			`SETTINGS storage_policy = 'cold\'drop'`,
		},
		{
			"trailing backslash is escaped",
			map[string]string{"storage_policy": `value\`},
			`SETTINGS storage_policy = 'value\\'`,
		},
		{
			"backslash before quote is escaped in right order",
			map[string]string{"storage_policy": `a\'b`},
			`SETTINGS storage_policy = 'a\\\'b'`,
		},
		{
			"NaN falls through to quoted string",
			map[string]string{"x": "NaN"},
			"SETTINGS x = 'NaN'",
		},
		{
			"Inf falls through to quoted string",
			map[string]string{"x": "Inf"},
			"SETTINGS x = 'Inf'",
		},
		{
			"mixed types preserve typing",
			map[string]string{"ttl": "1 day", "index_granularity": "8192"},
			"SETTINGS index_granularity = 8192, ttl = '1 day'",
		},
		{
			"injection attempt in value is neutralized",
			map[string]string{"x": "1, DROP TABLE foo --"},
			"SETTINGS x = '1, DROP TABLE foo --'",
		},
		{"invalid key is dropped", map[string]string{"bad key": "1"}, ""},
		{"injection attempt in key is dropped", map[string]string{"x, DROP TABLE foo --": "1"}, ""},
		{
			"valid key kept while invalid key dropped",
			map[string]string{"good_key": "1", "bad key": "2"},
			"SETTINGS good_key = 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEngineSettingsClause(tt.settings)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseClickHouseURI_Engine(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantEngine   string
		wantSettings map[string]string
	}{
		{
			name:         "no engine params",
			uri:          "clickhouse://user:pw@localhost:9000/db",
			wantEngine:   "",
			wantSettings: map[string]string{},
		},
		{
			name:         "engine only",
			uri:          "clickhouse://user:pw@localhost:9000/db?engine=merge_tree",
			wantEngine:   "merge_tree",
			wantSettings: map[string]string{},
		},
		{
			name:       "engine settings only",
			uri:        "clickhouse://user:pw@localhost:9000/db?engine.index_granularity=8192&engine.ttl=1%20day",
			wantEngine: "",
			wantSettings: map[string]string{
				"index_granularity": "8192",
				"ttl":               "1 day",
			},
		},
		{
			name:       "engine and settings",
			uri:        "clickhouse://user:pw@localhost:9000/db?engine=replacing_merge_tree&engine.index_granularity=8192",
			wantEngine: "replacing_merge_tree",
			wantSettings: map[string]string{
				"index_granularity": "8192",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, engineType, engineSettings, err := parseClickHouseURI(tt.uri)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if engineType != tt.wantEngine {
				t.Errorf("engine: got %q, want %q", engineType, tt.wantEngine)
			}
			if len(engineSettings) != len(tt.wantSettings) {
				t.Errorf("settings length: got %d, want %d (%v)", len(engineSettings), len(tt.wantSettings), engineSettings)
			}
			for k, v := range tt.wantSettings {
				if engineSettings[k] != v {
					t.Errorf("settings[%q]: got %q, want %q", k, engineSettings[k], v)
				}
			}
		})
	}
}

func TestBuildCreateTableSQL_Engine(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}

	tests := []struct {
		name        string
		primaryKeys []string
		engineType  string
		settings    map[string]string
		wantHas     []string
		wantMissing []string
	}{
		{
			name:        "default with PK uses ReplacingMergeTree with PK order",
			primaryKeys: []string{"id"},
			wantHas:     []string{"ENGINE = ReplacingMergeTree()", "ORDER BY (`id`)"},
		},
		{
			name:    "default without PK uses MergeTree",
			wantHas: []string{"ENGINE = MergeTree()", "ORDER BY (tuple())"},
		},
		{
			name:       "explicit merge_tree",
			engineType: "merge_tree",
			wantHas:    []string{"ENGINE = MergeTree()", "ORDER BY (tuple())"},
		},
		{
			name:       "explicit shared_merge_tree",
			engineType: "shared_merge_tree",
			wantHas:    []string{"ENGINE = SharedMergeTree()", "ORDER BY (tuple())"},
		},
		{
			name:       "explicit replicated_merge_tree",
			engineType: "replicated_merge_tree",
			wantHas:    []string{"ENGINE = ReplicatedMergeTree()", "ORDER BY (tuple())"},
		},
		{
			name:     "settings clause appended and sorted",
			settings: map[string]string{"ttl": "1 day", "index_granularity": "8192"},
			wantHas:  []string{"SETTINGS index_granularity = 8192, ttl = '1 day'"},
		},
		{
			name:       "engine with settings",
			engineType: "merge_tree",
			settings:   map[string]string{"index_granularity": "8192"},
			wantHas: []string{
				"ENGINE = MergeTree()",
				"ORDER BY (tuple())",
				"SETTINGS index_granularity = 8192",
			},
		},
		{
			name:        "unknown engine falls back to ReplacingMergeTree with PK",
			primaryKeys: []string{"id"},
			engineType:  "FakeEngine",
			wantHas:     []string{"ENGINE = ReplacingMergeTree()", "ORDER BY (`id`)"},
			wantMissing: []string{"FakeEngine"},
		},
		{
			name:        "unknown engine falls back to MergeTree without PK",
			engineType:  "FakeEngine",
			wantHas:     []string{"ENGINE = MergeTree()", "ORDER BY (tuple())"},
			wantMissing: []string{"FakeEngine"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCreateTableSQL("db", "t", cols, tt.primaryKeys, tt.engineType, tt.settings)
			for _, s := range tt.wantHas {
				if !strings.Contains(got, s) {
					t.Errorf("missing %q in:\n%s", s, got)
				}
			}
			for _, s := range tt.wantMissing {
				if strings.Contains(got, s) {
					t.Errorf("unexpected %q in:\n%s", s, got)
				}
			}
		})
	}
}

func TestBuildDeleteInsertStatements(t *testing.T) {
	t.Parallel()

	dest := &ClickHouseDestination{database: "default"}
	start := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC)
	end := time.Date(2026, 1, 3, 4, 5, 6, 7000, time.UTC)

	deleteSQL, insertSQL, targetDB, targetName := dest.buildDeleteInsertStatements(destination.DeleteInsertOptions{
		StagingTable:       "analytics.events_staging",
		TargetTable:        "analytics.events",
		IncrementalKey:     "event_time",
		IncrementalKeyType: schema.TypeTimestamp,
		IntervalStart:      start,
		IntervalEnd:        end,
		Columns:            []string{"id", "name", "event_time"},
		PrimaryKeys:        []string{"id"},
	})

	if targetDB != "analytics" || targetName != "events" {
		t.Fatalf("target = %s.%s, want analytics.events", targetDB, targetName)
	}

	wantDelete := "ALTER TABLE `analytics`.`events` DELETE WHERE `event_time` >= toDateTime64('2026-01-02 03:04:05.000006', 6) AND `event_time` <= toDateTime64('2026-01-03 04:05:06.000007', 6)"
	if deleteSQL != wantDelete {
		t.Fatalf("delete SQL = %q, want %q", deleteSQL, wantDelete)
	}

	wantParts := []string{
		"INSERT INTO `analytics`.`events` (`id`, `name`, `event_time`)",
		"ROW_NUMBER() OVER (PARTITION BY `id` ORDER BY `event_time` DESC)",
		"FROM `analytics`.`events_staging`",
		"WHERE __bruin_dedup_rn = 1",
	}
	for _, part := range wantParts {
		if !strings.Contains(insertSQL, part) {
			t.Fatalf("insert SQL missing %q:\n%s", part, insertSQL)
		}
	}
}

func TestFormatClickHouseLiteralTimeFallbackIsQuoted(t *testing.T) {
	t.Parallel()

	value := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC)

	got := formatClickHouseLiteral(value, schema.TypeString)
	want := "'2026-01-02T03:04:05.000006Z'"
	if got != want {
		t.Fatalf("literal = %q, want %q", got, want)
	}
}

func TestFormatClickHouseLiteralStringFallbackIsQuotedAndEscaped(t *testing.T) {
	t.Parallel()

	got := formatClickHouseLiteral("2026-01-02' OR 1=1 --", schema.TypeJSON)
	want := "'2026-01-02\\' OR 1=1 --'"
	if got != want {
		t.Fatalf("literal = %q, want %q", got, want)
	}
}

func TestBeginTransactionUnsupported(t *testing.T) {
	t.Parallel()

	dest := NewClickHouseDestination()
	tx, err := dest.BeginTransaction(context.Background())
	if err == nil {
		t.Fatal("BeginTransaction() error = nil, want unsupported error")
	}
	if tx != nil {
		t.Fatalf("BeginTransaction() tx = %#v, want nil", tx)
	}
	if !strings.Contains(err.Error(), "does not support transactions") {
		t.Fatalf("BeginTransaction() error = %v, want transaction unsupported error", err)
	}
}
