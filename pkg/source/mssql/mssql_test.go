package mssql

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"
)

func TestURIToConnString(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantErr      bool
		wantDriver   string
		wantHost     string
		wantUser     string
		wantPassword string
		wantQuery    map[string]string
		absentQuery  []string
	}{
		{
			name:         "sql server keeps sqlserver driver",
			uri:          "mssql://sa:pass@localhost/testdb?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=true",
			wantDriver:   sqlServerDriverName,
			wantHost:     "localhost:1433",
			wantUser:     "sa",
			wantPassword: "pass",
			wantQuery: map[string]string{
				"database":               "testdb",
				"TrustServerCertificate": "true",
			},
			absentQuery: []string{"driver"},
		},
		{
			name:         "azure sql auth uses azuresql driver and encryption",
			uri:          "azuresql://app_user:s3cr3t@myserver.database.windows.net/appdb",
			wantDriver:   azureSQLDriverName,
			wantHost:     "myserver.database.windows.net:1433",
			wantUser:     "app_user",
			wantPassword: "s3cr3t",
			wantQuery: map[string]string{
				"database": "appdb",
				"encrypt":  "true",
			},
		},
		{
			name:         "azure sql service principal tenant is encoded in user",
			uri:          "azuresql://client-id:secret@myserver.database.windows.net/appdb?tenant_id=tenant-123",
			wantDriver:   azureSQLDriverName,
			wantHost:     "myserver.database.windows.net:1433",
			wantUser:     "client-id@tenant-123",
			wantPassword: "secret",
			wantQuery: map[string]string{
				"database": "appdb",
				"encrypt":  "true",
				"fedauth":  "ActiveDirectoryServicePrincipal",
			},
			absentQuery: []string{"tenant_id"},
		},
		{
			name:       "azure sql without credentials defaults to Entra default",
			uri:        "azure-sql://myserver.database.windows.net/appdb",
			wantDriver: azureSQLDriverName,
			wantHost:   "myserver.database.windows.net:1433",
			wantQuery: map[string]string{
				"database": "appdb",
				"encrypt":  "true",
				"fedauth":  "ActiveDirectoryDefault",
			},
		},
		{
			name:         "odbc access token authentication maps to fedauth",
			uri:          "mssql://:token@myserver.database.windows.net/appdb?Authentication=ActiveDirectoryAccessToken",
			wantDriver:   azureSQLDriverName,
			wantHost:     "myserver.database.windows.net:1433",
			wantUser:     "",
			wantPassword: "token",
			wantQuery: map[string]string{
				"database": "appdb",
				"fedauth":  "ActiveDirectoryServicePrincipalAccessToken",
			},
			absentQuery: []string{"Authentication"},
		},
		{
			name:    "wrong scheme errors",
			uri:     "postgres://user:pass@localhost/db",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr, driverName, err := URIToConnString(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (connStr=%q)", connStr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if driverName != tt.wantDriver {
				t.Fatalf("driver = %q, want %q", driverName, tt.wantDriver)
			}

			u, perr := url.Parse(connStr)
			if perr != nil {
				t.Fatalf("result DSN is not a valid URL: %v", perr)
			}
			if u.Scheme != "sqlserver" {
				t.Errorf("scheme = %q, want sqlserver", u.Scheme)
			}
			if u.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", u.Host, tt.wantHost)
			}

			gotUser := ""
			gotPassword := ""
			if u.User != nil {
				gotUser = u.User.Username()
				gotPassword, _ = u.User.Password()
			}
			if gotUser != tt.wantUser {
				t.Errorf("user = %q, want %q", gotUser, tt.wantUser)
			}
			if gotPassword != tt.wantPassword {
				t.Errorf("password = %q, want %q", gotPassword, tt.wantPassword)
			}

			query := u.Query()
			for key, want := range tt.wantQuery {
				if got := query.Get(key); got != want {
					t.Errorf("query[%q] = %q, want %q", key, got, want)
				}
			}
			for _, key := range tt.absentQuery {
				if query.Has(key) {
					t.Errorf("query[%q] should be absent, got %q", key, query.Get(key))
				}
			}
		})
	}
}

func TestBuildSelectQueryPreservesColumnCasing(t *testing.T) {
	columns := []schema.Column{
		{Name: "RowPointer"},
		{Name: "NoteExistsFlag"},
		{Name: "CreatedBy"},
	}

	query := buildSelectQuery("dbo.notes", columns, source.ReadOptions{})

	for _, name := range []string{"[RowPointer]", "[NoteExistsFlag]", "[CreatedBy]"} {
		if !strings.Contains(query, name) {
			t.Errorf("query %q missing original column %q", query, name)
		}
	}
	for _, name := range []string{"row_pointer", "note_exists_flag", "created_by"} {
		if strings.Contains(query, name) {
			t.Errorf("query %q must not contain renamed column %q", query, name)
		}
	}
}

func TestBuildSelectQueryAddsExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := buildSelectQuery("dbo.orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		IncrementalKey:        "updated_at",
		IntervalStart:         &intervalStart,
		IntervalEnd:           &intervalEnd,
		ExtractPartitionBy:    "created_at",
		ExtractPartitionStart: &windowStart,
		ExtractPartitionEnd:   &windowEnd,
	})

	want := "SELECT [id], [created_at] FROM [dbo].[orders] WHERE [updated_at] >= '2026-01-01 00:00:00' AND [updated_at] <= '2026-01-31 00:00:00' AND [created_at] >= '2026-01-08 00:00:00' AND [created_at] < '2026-01-15 00:00:00'"
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestParseTableNameSupportsMultipartIdentifiers(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		wantSchema string
		wantTable  string
	}{
		{
			name:       "unqualified table defaults schema",
			table:      "users",
			wantSchema: "dbo",
			wantTable:  "users",
		},
		{
			name:       "schema qualified table",
			table:      "sales.orders",
			wantSchema: "sales",
			wantTable:  "orders",
		},
		{
			name:       "database qualified table",
			table:      "RemoteDB.dbo.orders",
			wantSchema: "dbo",
			wantTable:  "orders",
		},
		{
			name:       "linked server qualified table",
			table:      "LINKED_SRV.RemoteDB.dbo.my_table",
			wantSchema: "dbo",
			wantTable:  "my_table",
		},
		{
			name:       "bracketed identifiers with dots",
			table:      "[LINKED_SRV].[RemoteDB].[erp.schema].[my.table]",
			wantSchema: "erp.schema",
			wantTable:  "my.table",
		},
		{
			name:       "escaped bracket in identifier",
			table:      "[LINKED]]SRV].[RemoteDB].[dbo].[my]]table]",
			wantSchema: "dbo",
			wantTable:  "my]table",
		},
		{
			name:       "empty schema segment defaults schema",
			table:      "RemoteDB..orders",
			wantSchema: "dbo",
			wantTable:  "orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSchema, gotTable := parseTableName(tt.table)
			if gotSchema != tt.wantSchema {
				t.Errorf("schema = %q, want %q", gotSchema, tt.wantSchema)
			}
			if gotTable != tt.wantTable {
				t.Errorf("table = %q, want %q", gotTable, tt.wantTable)
			}
		})
	}
}

func TestQuoteTableSupportsMultipartIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "unqualified table",
			table: "users",
			want:  "[users]",
		},
		{
			name:  "schema qualified table",
			table: "dbo.notes",
			want:  "[dbo].[notes]",
		},
		{
			name:  "linked server qualified table",
			table: "LINKED_SRV.RemoteDB.dbo.my_table",
			want:  "[LINKED_SRV].[RemoteDB].[dbo].[my_table]",
		},
		{
			name:  "bracketed identifiers with dots",
			table: "[LINKED_SRV].[RemoteDB].[erp.schema].[my.table]",
			want:  "[LINKED_SRV].[RemoteDB].[erp.schema].[my.table]",
		},
		{
			name:  "escaped bracket in identifier",
			table: "[LINKED]]SRV].[RemoteDB].[dbo].[my]]table]",
			want:  "[LINKED]]SRV].[RemoteDB].[dbo].[my]]table]",
		},
		{
			name:  "empty schema segment",
			table: "RemoteDB..orders",
			want:  "[RemoteDB]..[orders]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteTable(tt.table); got != tt.want {
				t.Errorf("quoteTable(%q) = %q, want %q", tt.table, got, tt.want)
			}
		})
	}
}

func TestInformationSchemaQualifierSkipsEmptyCatalogParts(t *testing.T) {
	tableRef := parseMSSQLTableRef("LINKED_SRV..dbo.my_table")

	got := tableRef.informationSchemaQualifier()
	want := "[LINKED_SRV].INFORMATION_SCHEMA"
	if got != want {
		t.Fatalf("information schema qualifier = %q, want %q", got, want)
	}
}

func TestBuildSelectQuerySupportsLinkedServerTable(t *testing.T) {
	columns := []schema.Column{
		{Name: "id"},
		{Name: "CreatedAt"},
	}

	query := buildSelectQuery("LINKED_SRV.RemoteDB.dbo.my_table", columns, source.ReadOptions{Limit: 10})
	want := "SELECT TOP 10 [id], [CreatedAt] FROM [LINKED_SRV].[RemoteDB].[dbo].[my_table]"
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestSchemaMetadataQueriesUseCatalogPrefix(t *testing.T) {
	tableRef := parseMSSQLTableRef("LINKED_SRV.RemoteDB.dbo.my_table")
	columnsQuery, pkQuery := schemaMetadataQueries(tableRef)

	if !strings.Contains(columnsQuery, "FROM [LINKED_SRV].[RemoteDB].INFORMATION_SCHEMA.COLUMNS") {
		t.Fatalf("columns query does not use linked-server information schema: %s", columnsQuery)
	}
	if !strings.Contains(pkQuery, "FROM [LINKED_SRV].[RemoteDB].INFORMATION_SCHEMA.TABLE_CONSTRAINTS") {
		t.Fatalf("pk query does not use linked-server table constraints: %s", pkQuery)
	}
	if !strings.Contains(pkQuery, "JOIN [LINKED_SRV].[RemoteDB].INFORMATION_SCHEMA.KEY_COLUMN_USAGE") {
		t.Fatalf("pk query does not use linked-server key column usage: %s", pkQuery)
	}
}

func TestGuidConversionEnabled(t *testing.T) {
	connStr, _, err := URIToConnString("mssql://sa:pass@localhost/db?guid+conversion=true")
	if err != nil {
		t.Fatalf("URIToConnString error: %v", err)
	}
	if !guidConversionEnabled(connStr) {
		t.Fatal("expected guid conversion to be enabled")
	}

	connStr, _, err = URIToConnString("mssql://sa:pass@localhost/db?guid+conversion=false")
	if err != nil {
		t.Fatalf("URIToConnString error: %v", err)
	}
	if guidConversionEnabled(connStr) {
		t.Fatal("expected guid conversion to be disabled")
	}

	connStr, _, err = URIToConnString("mssql://sa:pass@localhost/db?GUID+CONVERSION=1")
	if err != nil {
		t.Fatalf("URIToConnString error: %v", err)
	}
	if !guidConversionEnabled(connStr) {
		t.Fatal("expected case-insensitive guid conversion to be enabled")
	}
}

func TestNormalizeUUIDValueFormatsRawSQLServerBytes(t *testing.T) {
	raw := []byte{
		0x6F, 0x96, 0x19, 0xFF,
		0x8B, 0x86,
		0xD0, 0x11,
		0xB4, 0x2D,
		0x00, 0xC0, 0x4F, 0xC9, 0x64, 0xFF,
	}

	got, err := normalizeUUIDValue(raw, false)
	if err != nil {
		t.Fatalf("normalizeUUIDValue error: %v", err)
	}
	if got != "FF19966F-868B-11D0-B42D-00C04FC964FF" {
		t.Fatalf("got %q, want canonical UUID", got)
	}
}

func TestNormalizeUUIDValueFormatsGuidConvertedBytes(t *testing.T) {
	raw := []byte{
		0xFF, 0x19, 0x96, 0x6F,
		0x86, 0x8B,
		0x11, 0xD0,
		0xB4, 0x2D,
		0x00, 0xC0, 0x4F, 0xC9, 0x64, 0xFF,
	}

	got, err := normalizeUUIDValue(raw, true)
	if err != nil {
		t.Fatalf("normalizeUUIDValue error: %v", err)
	}
	if got != "FF19966F-868B-11D0-B42D-00C04FC964FF" {
		t.Fatalf("got %q, want canonical UUID", got)
	}
}

func TestNormalizeUUIDValueHandlesDriverUUIDTypes(t *testing.T) {
	uuid := mssqldb.UniqueIdentifier{
		0xFF, 0x19, 0x96, 0x6F,
		0x86, 0x8B,
		0x11, 0xD0,
		0xB4, 0x2D,
		0x00, 0xC0, 0x4F, 0xC9, 0x64, 0xFF,
	}

	got, err := normalizeUUIDValue(mssqldb.NullUniqueIdentifier{UUID: uuid, Valid: true}, false)
	if err != nil {
		t.Fatalf("normalizeUUIDValue error: %v", err)
	}
	if got != "FF19966F-868B-11D0-B42D-00C04FC964FF" {
		t.Fatalf("got %q, want canonical UUID", got)
	}

	got, err = normalizeUUIDValue(mssqldb.NullUniqueIdentifier{}, false)
	if err != nil {
		t.Fatalf("normalizeUUIDValue error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestNormalizeUUIDValuePassesStringsThrough(t *testing.T) {
	const want = "ff19966f-868b-11d0-b42d-00c04fc964ff"

	got, err := normalizeUUIDValue(want, false)
	if err != nil {
		t.Fatalf("normalizeUUIDValue error: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeUUIDValueRejectsInvalidByteLength(t *testing.T) {
	if _, err := normalizeUUIDValue([]byte{0x01, 0x02}, false); err == nil {
		t.Fatal("expected invalid uniqueidentifier length error")
	}
}
