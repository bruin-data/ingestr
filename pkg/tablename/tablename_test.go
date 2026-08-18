package tablename

import "testing"

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "users", []string{"users"}},
		{"two", "public.users", []string{"public", "users"}},
		{"three", "db.public.users", []string{"db", "public", "users"}},
		{"four", "srv.db.dbo.users", []string{"srv", "db", "dbo", "users"}},
		{"brackets", "[my db].[dbo].[my.table]", []string{"my db", "dbo", "my.table"}},
		{"bracket escape", "[a]]b].t", []string{"a]b", "t"}},
		{"double quotes", `"my.schema"."tbl"`, []string{"my.schema", "tbl"}},
		{"backticks", "`my.db`.`tbl`", []string{"my.db", "tbl"}},
		{"whitespace", " a . b ", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Split(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("Split(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Split(%q) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

func TestSplitRawPreservesIdentifierReferences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"double quotes", `"appUser"."order.events"`, []string{`"appUser"`, `"order.events"`}},
		{"escaped double quote", `"app""User".orders`, []string{`"app""User"`, "orders"}},
		{"brackets", `[Case Schema].[order.events]`, []string{`[Case Schema]`, `[order.events]`}},
		{"backticks", "`Case Schema`.`order.events`", []string{"`Case Schema`", "`order.events`"}},
		{"whitespace", ` "appUser" . orders `, []string{`"appUser"`, "orders"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitRaw(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitRaw(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("SplitRaw(%q) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		cap      Capability
		in       string
		defaults Defaults
		want     TableName
		wantErr  bool
	}{
		{"snowflake bare", Snowflake, "users", Defaults{Schema: "PUBLIC"}, TableName{Schema: "PUBLIC", Table: "users"}, false},
		{"snowflake two", Snowflake, "sales.users", Defaults{Catalog: "DB1"}, TableName{Catalog: "DB1", Schema: "sales", Table: "users"}, false},
		{"snowflake three", Snowflake, "db.sales.users", Defaults{}, TableName{Catalog: "db", Schema: "sales", Table: "users"}, false},
		{"bigquery two", BigQuery, "ds.t", Defaults{Catalog: "proj"}, TableName{Catalog: "proj", Schema: "ds", Table: "t"}, false},
		{"bigquery three", BigQuery, "proj2.ds.t", Defaults{Catalog: "proj"}, TableName{Catalog: "proj2", Schema: "ds", Table: "t"}, false},
		{"bigquery bare rejected", BigQuery, "t", Defaults{}, TableName{}, true},
		{"postgres two ok", TwoLevel("postgres"), "public.users", Defaults{}, TableName{Schema: "public", Table: "users"}, false},
		{"postgres three rejected", TwoLevel("postgres"), "db.public.users", Defaults{}, TableName{}, true},
		{"mssql four ok (unbounded)", MSSQL, "srv.db.dbo.t", Defaults{}, TableName{Catalog: "srv.db", Schema: "dbo", Table: "t"}, false},
		{"empty component rejected", Snowflake, "db..t", Defaults{}, TableName{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cap.Parse(tt.in, tt.defaults)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) expected error, got %+v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSchemaToCreate(t *testing.T) {
	id := func(s string) string { return `"` + s + `"` }
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"users", "", false},
		{"public.users", `"public"`, true},
		{"db.public.users", `"db"."public"`, true},
		{"a.b.c.d", "", false},
	}
	for _, tt := range tests {
		got, ok := SchemaToCreate(tt.in, id)
		if ok != tt.wantOK || got != tt.want {
			t.Fatalf("SchemaToCreate(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestContainerToCreate(t *testing.T) {
	id := func(s string) string { return `"` + s + `"` }
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"users", "", false},
		{"public.users", "", false},
		{"db.public.users", `"db"`, true},
	}
	for _, tt := range tests {
		got, ok := ContainerToCreate(tt.in, id)
		if ok != tt.wantOK || got != tt.want {
			t.Fatalf("ContainerToCreate(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "users", []string{"users"}},
		{"two", "users,orders", []string{"users", "orders"}},
		{"qualified", "public.users, sales.orders", []string{"public.users", "sales.orders"}},
		{"quoted comma", `"od,d".t,public.users`, []string{`"od,d".t`, "public.users"}},
		{"bracketed comma", "[a,b].c,d", []string{"[a,b].c", "d"}},
		{"backticked comma", "`a,b`,c", []string{"`a,b`", "c"}},
		{"trailing comma", "users,orders,", []string{"users", "orders"}},
		{"blank entries", " users , , orders ", []string{"users", "orders"}},
		{"empty", "", nil},
		{"separators only", " , ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitList(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitList(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("SplitList(%q) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

func TestSplitListElementsStayParsable(t *testing.T) {
	// A list element keeps its quoting so callers can Split it themselves.
	names := SplitList(`"od,d"."t.1",users`)
	if len(names) != 2 {
		t.Fatalf("SplitList = %v, want 2 elements", names)
	}
	parts := Split(names[0])
	if len(parts) != 2 || parts[0] != "od,d" || parts[1] != "t.1" {
		t.Fatalf("Split(%q) = %v, want [od,d t.1]", names[0], parts)
	}
}
