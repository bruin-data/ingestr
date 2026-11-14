package databricks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/databricks/databricks-sdk-go"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
)

func TestMergeTableSQL(t *testing.T) {
	for _, tt := range []struct {
		name           string
		incrementalKey string
		orderBy        string
	}{
		{name: "arbitrary row without incremental key", orderBy: "NULL"},
		{name: "latest row with incremental key", incrementalKey: "updated`at", orderBy: "`updated``at` DESC"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			statements := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/2.0/sql/statements" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				var request dbsql.ExecuteStatementRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode statement: %v", err)
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				statements <- request.Statement
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":{"state":"SUCCEEDED"}}`))
			}))
			defer server.Close()

			client, err := databricks.NewWorkspaceClient(&databricks.Config{
				Host: server.URL, Token: "test-token", AuthType: "pat",
			})
			if err != nil {
				t.Fatal(err)
			}
			dest := &DatabricksDestination{client: client, catalog: "main", httpPath: "/sql/1.0/warehouses/test"}
			err = dest.MergeTable(context.Background(), destination.MergeOptions{
				StagingTable: "scratch.orders_merge", TargetTable: "analytics.orders",
				Columns: []string{"tenant", "order`id", "updated`at"}, PrimaryKeys: []string{"tenant", "order`id"},
				IncrementalKey: tt.incrementalKey,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("MERGE INTO `main`.`analytics`.`orders` AS target\n"+
				"USING (SELECT `tenant`, `order``id`, `updated``at` FROM (SELECT `tenant`, `order``id`, `updated``at`, ROW_NUMBER() OVER (PARTITION BY `tenant`, `order``id` ORDER BY %s) AS __bruin_dedup_rn FROM `main`.`ingestr_staging`.`orders_merge`) AS _numbered WHERE __bruin_dedup_rn = 1) AS source\n"+
				"ON target.`tenant` = source.`tenant` AND target.`order``id` = source.`order``id`\n"+
				"WHEN MATCHED THEN\n  UPDATE SET target.`updated``at` = source.`updated``at`\n"+
				"WHEN NOT MATCHED THEN\n  INSERT (`tenant`, `order``id`, `updated``at`)\n"+
				"  VALUES (source.`tenant`, source.`order``id`, source.`updated``at`)", tt.orderBy)
			statement := <-statements
			if statement != want {
				t.Fatalf("merge SQL =\n%s\nwant:\n%s", statement, want)
			}
		})
	}
}

func TestMapDataTypeToDatabricks_SizedString(t *testing.T) {
	tests := []struct {
		name     string
		col      schema.Column
		expected string
	}{
		{"sized", schema.Column{DataType: schema.TypeString, MaxLength: 50}, "VARCHAR(50)"},
		{"unsized", schema.Column{DataType: schema.TypeString}, "STRING"},
		{"over cap falls back to STRING", schema.Column{DataType: schema.TypeString, MaxLength: 70000}, "STRING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapDataTypeToDatabricks(tt.col); got != tt.expected {
				t.Fatalf("MapDataTypeToDatabricks() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestBuildDeleteInsertSQLUsesAtomicBlock(t *testing.T) {
	t.Parallel()

	dest := &DatabricksDestination{catalog: "main"}
	if !dest.SupportsDeleteInsertStrategy() {
		t.Fatal("SupportsDeleteInsertStrategy() = false, want true")
	}

	deleteSQL, insertSQL, atomicSQL := dest.buildDeleteInsertSQL(destination.DeleteInsertOptions{
		StagingTable:   "scratch.orders_di",
		TargetTable:    "analytics.orders",
		IncrementalKey: "updated_at",
		IntervalStart:  "2026-01-01",
		IntervalEnd:    "2026-01-31",
		Columns:        []string{"id", "name", "updated_at"},
		PrimaryKeys:    []string{"id"},
	})

	if want := "DELETE FROM `main`.`analytics`.`orders` WHERE `updated_at` >= '2026-01-01' AND `updated_at` <= '2026-01-31'"; deleteSQL != want {
		t.Fatalf("deleteSQL = %q, want %q", deleteSQL, want)
	}
	wantInsert := "INSERT INTO `main`.`analytics`.`orders` (`id`, `name`, `updated_at`) " +
		"SELECT `id`, `name`, `updated_at` FROM (SELECT `id`, `name`, `updated_at`, " +
		"ROW_NUMBER() OVER (PARTITION BY `id` ORDER BY CASE WHEN `updated_at` IS NULL THEN 1 ELSE 0 END ASC, `updated_at` DESC) AS __bruin_dedup_rn " +
		"FROM `main`.`ingestr_staging`.`orders_di`) AS _numbered WHERE __bruin_dedup_rn = 1"
	if insertSQL != wantInsert {
		t.Fatalf("insertSQL = %q, want %q", insertSQL, wantInsert)
	}

	wantAtomic := "BEGIN ATOMIC\n  " + deleteSQL + ";\n  " + insertSQL + ";\nEND;"
	if atomicSQL != wantAtomic {
		t.Fatalf("atomicSQL = %q, want %q", atomicSQL, wantAtomic)
	}
}

func TestBeginTransactionUnsupported(t *testing.T) {
	t.Parallel()

	dest := NewDatabricksDestination()
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
