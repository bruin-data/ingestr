package starrocks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseStarRocksURI(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantDSN      string
		wantCatalog  string
		wantDatabase string
	}{
		{
			name:         "database only path",
			uri:          "starrocks://root:pass@fe-host:9030/analytics",
			wantDSN:      "root:pass@tcp(fe-host:9030)/?parseTime=true",
			wantCatalog:  defaultCatalog,
			wantDatabase: "analytics",
		},
		{
			name:         "catalog and database path",
			uri:          "starrocks://root:pass@host:9030/iceberg_catalog/lake",
			wantDSN:      "root:pass@tcp(host:9030)/?parseTime=true",
			wantCatalog:  "iceberg_catalog",
			wantDatabase: "lake",
		},
		{
			name:         "no path leaves both empty/default",
			uri:          "starrocks://root@host:9030/",
			wantDSN:      "root@tcp(host:9030)/?parseTime=true",
			wantCatalog:  defaultCatalog,
			wantDatabase: "",
		},
		{
			name:         "default port",
			uri:          "starrocks://root@localhost/db",
			wantDSN:      "root@tcp(localhost:9030)/?parseTime=true",
			wantCatalog:  defaultCatalog,
			wantDatabase: "db",
		},
		{
			name:         "ssl true maps to tls verify",
			uri:          "starrocks://root@host:9030/db?ssl=true",
			wantDSN:      "root@tcp(host:9030)/?parseTime=true&tls=true",
			wantCatalog:  defaultCatalog,
			wantDatabase: "db",
		},
		{
			name:         "ssl skip-verify maps to tls skip-verify",
			uri:          "starrocks://root@host:9030/db?ssl=skip-verify",
			wantDSN:      "root@tcp(host:9030)/?parseTime=true&tls=skip-verify",
			wantCatalog:  defaultCatalog,
			wantDatabase: "db",
		},
		{
			name:         "explicit tls param passes through",
			uri:          "starrocks://root@host:9030/db?tls=custom",
			wantDSN:      "root@tcp(host:9030)/?parseTime=true&tls=custom",
			wantCatalog:  defaultCatalog,
			wantDatabase: "db",
		},
		{
			name:         "ssl false leaves connection plaintext",
			uri:          "starrocks://root@host:9030/db?ssl=false",
			wantDSN:      "root@tcp(host:9030)/?parseTime=true",
			wantCatalog:  defaultCatalog,
			wantDatabase: "db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, catalog, database, err := parseStarRocksURI(tt.uri)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dsn != tt.wantDSN {
				t.Errorf("dsn: got %q want %q", dsn, tt.wantDSN)
			}
			if catalog != tt.wantCatalog {
				t.Errorf("catalog: got %q want %q", catalog, tt.wantCatalog)
			}
			if database != tt.wantDatabase {
				t.Errorf("database: got %q want %q", database, tt.wantDatabase)
			}
		})
	}
}

func TestParseStarRocksURIInvalidSSL(t *testing.T) {
	_, _, _, err := parseStarRocksURI("starrocks://root@host:9030/db?ssl=yes")
	if err == nil {
		t.Fatal("expected an error for an invalid ssl value")
	}
	if !strings.Contains(err.Error(), "invalid ssl value") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name         string
		srcCatalog   string
		srcDatabase  string
		table        string
		wantCatalog  string
		wantDatabase string
		wantTable    string
	}{
		// URI defaults applied to unqualified names.
		{"bare name uses uri defaults", defaultCatalog, "analytics", "events", defaultCatalog, "analytics", "events"},
		// Two-part name overrides the database, keeps the URI catalog.
		{"db.table overrides database", defaultCatalog, "analytics", "sales.orders", defaultCatalog, "sales", "orders"},
		// Three-part name overrides everything.
		{"full name overrides all", defaultCatalog, "analytics", "iceberg_catalog.lake.trips", "iceberg_catalog", "lake", "trips"},
		// URI carries a catalog default; a bare name inherits it.
		{"bare name inherits uri catalog", "iceberg_catalog", "lake", "trips", "iceberg_catalog", "lake", "trips"},
		// URI catalog default with a two-part name: catalog from URI, db from name.
		{"db.table under uri catalog", "iceberg_catalog", "lake", "other.trips", "iceberg_catalog", "other", "trips"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &StarRocksSource{catalog: tt.srcCatalog, database: tt.srcDatabase}
			catalog, database, table := s.parseTableName(tt.table)
			if catalog != tt.wantCatalog || database != tt.wantDatabase || table != tt.wantTable {
				t.Errorf("got (%q, %q, %q) want (%q, %q, %q)",
					catalog, database, table, tt.wantCatalog, tt.wantDatabase, tt.wantTable)
			}
		})
	}
}

func TestGetSchemaRequiresDatabase(t *testing.T) {
	// No URI database and a bare table name: the database cannot be resolved, so
	// getSchema must fail early with a clear message instead of issuing malformed
	// SQL. The guard fires before any DB call, so a nil connection is fine.
	s := &StarRocksSource{catalog: defaultCatalog, database: ""}
	_, err := s.getSchema(context.Background(), "events")
	if err == nil {
		t.Fatal("expected an error when no database can be resolved")
	}
	if !strings.Contains(err.Error(), "no database resolved") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildSelectQuery(t *testing.T) {
	s := &StarRocksSource{catalog: defaultCatalog, database: "analytics"}
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "created_at", DataType: schema.TypeTimestamp},
	}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	opts := source.ReadOptions{IncrementalKey: "created_at", IntervalStart: &start, IntervalEnd: &end}

	got := s.buildSelectQuery("iceberg_catalog.lake.trips", columns, opts)
	want := "SELECT `id`, `created_at` FROM `iceberg_catalog`.`lake`.`trips` " +
		"WHERE `created_at` >= '2024-01-01 00:00:00' AND `created_at` <= '2024-01-31 23:59:59'"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestBuildSelectQueryLimit(t *testing.T) {
	s := &StarRocksSource{catalog: defaultCatalog, database: "analytics"}
	columns := []schema.Column{{Name: "id", DataType: schema.TypeInt64}}
	got := s.buildSelectQuery("events", columns, source.ReadOptions{Limit: 10})
	want := "SELECT `id` FROM `default_catalog`.`analytics`.`events` LIMIT 10"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestMapStarRocksToDataType(t *testing.T) {
	tests := []struct {
		in        string
		want      schema.DataType
		precision int
		scale     int
		arrayType schema.DataType
	}{
		{"BOOLEAN", schema.TypeBoolean, 0, 0, schema.TypeUnknown},
		{"TINYINT", schema.TypeInt16, 0, 0, schema.TypeUnknown},
		{"INT", schema.TypeInt32, 0, 0, schema.TypeUnknown},
		{"BIGINT", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"LARGEINT", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"FLOAT", schema.TypeFloat32, 0, 0, schema.TypeUnknown},
		{"DOUBLE", schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"decimal(18,4)", schema.TypeDecimal, 18, 4, schema.TypeUnknown},
		{"DECIMAL", schema.TypeDecimal, 38, 9, schema.TypeUnknown},
		{"VARCHAR(255)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"STRING", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"DATE", schema.TypeDate, 0, 0, schema.TypeUnknown},
		{"DATETIME", schema.TypeTimestamp, 0, 0, schema.TypeUnknown},
		{"VARBINARY", schema.TypeBinary, 0, 0, schema.TypeUnknown},
		{"BLOB", schema.TypeBinary, 0, 0, schema.TypeUnknown},
		{"JSON", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"array<int>", schema.TypeArray, 0, 0, schema.TypeInt32},
		{"array<decimal(10,2)>", schema.TypeArray, 0, 0, schema.TypeDecimal},
		{"map<string,int>", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"struct<a:int>", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"HLL", schema.TypeString, 0, 0, schema.TypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			dt, precision, scale, arrayType := MapStarRocksToDataType(tt.in)
			if dt != tt.want || precision != tt.precision || scale != tt.scale || arrayType != tt.arrayType {
				t.Errorf("got (%v, %d, %d, %v) want (%v, %d, %d, %v)",
					dt, precision, scale, arrayType, tt.want, tt.precision, tt.scale, tt.arrayType)
			}
		})
	}
}

func TestBuildFQN(t *testing.T) {
	if got := buildFQN("", "", "t"); got != "`t`" {
		t.Errorf("bare table: got %q", got)
	}
	if got := buildFQN("", "db", "t"); got != "`db`.`t`" {
		t.Errorf("db.table: got %q", got)
	}
	if got := buildFQN("cat", "db", "t"); got != "`cat`.`db`.`t`" {
		t.Errorf("cat.db.table: got %q", got)
	}
	if got := buildFQN("cat", "db", "a`b"); !strings.Contains(got, "`a``b`") {
		t.Errorf("escaping: got %q", got)
	}
}
