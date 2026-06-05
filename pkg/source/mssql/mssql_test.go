package mssql

import (
	"net/url"
	"testing"
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
			connStr, driverName, err := uriToConnString(tt.uri)
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
