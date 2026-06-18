package fabric

import (
	"net/url"
	"strings"
	"testing"
)

func TestURIToConnString(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantErr      bool
		wantDatabase string
		wantHost     string
		wantUser     string // expected decoded user-id ("" means no userinfo)
		wantPassword string
		wantQuery    map[string]string
	}{
		{
			name:         "service principal with tenant",
			uri:          "fabric://client-id:s3cr3t@abc.datawarehouse.fabric.microsoft.com/MyWarehouse?tenant_id=tenant-123",
			wantDatabase: "MyWarehouse",
			wantHost:     "abc.datawarehouse.fabric.microsoft.com:1433",
			wantUser:     "client-id@tenant-123",
			wantPassword: "s3cr3t",
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryServicePrincipal",
				"database": "MyWarehouse",
				"encrypt":  "true",
			},
		},
		{
			name:         "no credentials defaults to ActiveDirectoryDefault",
			uri:          "fabric://abc.datawarehouse.fabric.microsoft.com/wh",
			wantDatabase: "wh",
			wantHost:     "abc.datawarehouse.fabric.microsoft.com:1433",
			wantUser:     "",
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryDefault",
				"database": "wh",
				"encrypt":  "true",
			},
		},
		{
			name:         "explicit fedauth and port are preserved",
			uri:          "fabric://client-id:token@host.example.com:1234/wh?fedauth=ActiveDirectoryServicePrincipalAccessToken&tenant_id=t1",
			wantDatabase: "wh",
			wantHost:     "host.example.com:1234",
			wantUser:     "client-id@t1",
			wantPassword: "token",
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryServicePrincipalAccessToken",
				"database": "wh",
				"encrypt":  "true",
			},
		},
		{
			name:    "wrong scheme errors",
			uri:     "mssql://user:pass@host/db",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr, database, err := uriToConnString(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (connStr=%q)", connStr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if database != tt.wantDatabase {
				t.Errorf("database = %q, want %q", database, tt.wantDatabase)
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
			if u.User != nil {
				gotUser = u.User.Username()
			}
			if gotUser != tt.wantUser {
				t.Errorf("user = %q, want %q", gotUser, tt.wantUser)
			}
			if tt.wantPassword != "" {
				gotPass, _ := u.User.Password()
				if gotPass != tt.wantPassword {
					t.Errorf("password = %q, want %q", gotPass, tt.wantPassword)
				}
			}

			q := u.Query()
			for k, want := range tt.wantQuery {
				if got := q.Get(k); got != want {
					t.Errorf("query[%q] = %q, want %q", k, got, want)
				}
			}
			// tenant_id must never leak into the DSN query.
			if q.Has("tenant_id") {
				t.Errorf("tenant_id should not appear in DSN query, got %q", q.Get("tenant_id"))
			}
		})
	}
}

func TestBuildDeleteInsertDeleteSQLUsesTableLock(t *testing.T) {
	sql := buildDeleteInsertDeleteSQL("dbo.events", "updated_at")

	if !strings.Contains(sql, "DELETE FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)") {
		t.Fatalf("delete SQL missing table lock: %s", sql)
	}
	if !strings.Contains(sql, "[updated_at] >= @p1") || !strings.Contains(sql, "[updated_at] <= @p2") {
		t.Fatalf("delete SQL missing interval predicate: %s", sql)
	}
}
