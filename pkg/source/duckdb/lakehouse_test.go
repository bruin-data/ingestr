package duckdb

import (
	"strings"
	"testing"
)

func mustParseLakehouse(t *testing.T, raw string) *LakehouseConfig {
	t.Helper()
	cfg, err := ParseLakehouseURI(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}

// -----------------------------------------------------------------------------
// ParseLakehouseURI
// -----------------------------------------------------------------------------

func TestParseLakehouseURI_DuckDBCatalogS3Storage_MinIO(t *testing.T) {
	t.Parallel()

	raw := "ducklake://?" +
		"catalog_type=duckdb&catalog_path=/tmp/metadata.duckdb" +
		"&storage_type=s3&storage_path=s3://ducklake/warehouse" +
		"&storage_endpoint=minio.local:9000" +
		"&storage_url_style=path" +
		"&storage_use_ssl=false" +
		"&storage_access_key=AKID&storage_secret_key=SECRET"

	cfg := mustParseLakehouse(t, raw)

	if cfg.Catalog.Type != CatalogTypeDuckDB || cfg.Catalog.Path != "/tmp/metadata.duckdb" {
		t.Errorf("catalog: %+v", cfg.Catalog)
	}
	if cfg.Storage.Type != StorageTypeS3 || cfg.Storage.Path != "s3://ducklake/warehouse" {
		t.Errorf("storage type/path: %+v", cfg.Storage)
	}
	if cfg.Storage.Endpoint != "minio.local:9000" || cfg.Storage.URLStyle != "path" {
		t.Errorf("storage endpoint/url_style: %+v", cfg.Storage)
	}
	if cfg.Storage.UseSSL == nil || *cfg.Storage.UseSSL != false {
		t.Errorf("UseSSL: got %v", cfg.Storage.UseSSL)
	}
	if cfg.Storage.AccessKey != "AKID" || cfg.Storage.SecretKey != "SECRET" {
		t.Errorf("creds: %+v", cfg.Storage)
	}
}

func TestParseLakehouseURI_PostgresCatalog(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=postgres&catalog_host=metastore.internal&catalog_port=5432"+
		"&catalog_database=ducklake_meta&catalog_username=lake_user&catalog_password=lake_pass"+
		"&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b")

	if cfg.Catalog.Type != CatalogTypePostgres {
		t.Errorf("catalog type: %v", cfg.Catalog.Type)
	}
	if cfg.Catalog.Host != "metastore.internal" || cfg.Catalog.Port != 5432 {
		t.Errorf("catalog host/port: %+v", cfg.Catalog)
	}
	if cfg.Catalog.Username != "lake_user" || cfg.Catalog.Password != "lake_pass" {
		t.Errorf("catalog auth: %+v", cfg.Catalog)
	}
}

func TestParseLakehouseURI_RejectsBadInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		uri     string
		wantSub string
	}{
		{
			"wrong scheme",
			"duckdb://?catalog_type=duckdb&catalog_path=/m&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"ducklake scheme",
		},
		{
			"missing catalog_type",
			"ducklake://?storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"catalog_type is required",
		},
		{
			"bad catalog_type",
			"ducklake://?catalog_type=glue&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"unsupported catalog_type",
		},
		{
			"missing catalog_path for duckdb",
			"ducklake://?catalog_type=duckdb&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"catalog_path is required",
		},
		{
			"missing catalog_host for postgres",
			"ducklake://?catalog_type=postgres&catalog_database=m&catalog_username=u&catalog_password=p&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"catalog_host is required",
		},
		{
			"missing catalog_database for postgres",
			"ducklake://?catalog_type=postgres&catalog_host=h&catalog_username=u&catalog_password=p&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"catalog_database is required",
		},
		{
			"missing catalog_username for postgres",
			"ducklake://?catalog_type=postgres&catalog_host=h&catalog_database=m&catalog_password=p&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"catalog_username and catalog_password",
		},
		{
			"missing storage_type",
			"ducklake://?catalog_type=duckdb&catalog_path=/m&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"storage_type is required",
		},
		{
			"missing storage_path",
			"ducklake://?catalog_type=duckdb&catalog_path=/m&storage_type=s3&storage_access_key=a&storage_secret_key=b",
			"storage_path is required",
		},
		{
			"missing storage_access_key",
			"ducklake://?catalog_type=duckdb&catalog_path=/m&storage_type=s3&storage_path=s3://b/p&storage_secret_key=b",
			"storage_access_key and storage_secret_key",
		},
		{
			"bad storage_use_ssl",
			"ducklake://?catalog_type=duckdb&catalog_path=/m&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b&storage_use_ssl=maybe",
			"invalid storage_use_ssl",
		},
		{
			"bad url_style",
			"ducklake://?catalog_type=duckdb&catalog_path=/m&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b&storage_url_style=fancy",
			"unsupported storage_url_style",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseLakehouseURI(tc.uri)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error for %s: got %q, want substring %q", tc.name, err.Error(), tc.wantSub)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// LakehouseAttacher
// -----------------------------------------------------------------------------

func TestGenerateAttachStatements_OrderAndShape(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=postgres&catalog_host=h&catalog_database=d"+
		"&catalog_username=u&catalog_password=p"+
		"&storage_type=s3&storage_path=s3://b/p"+
		"&storage_access_key=a&storage_secret_key=b")

	stmts, err := NewLakehouseAttacher().GenerateAttachStatements(cfg, AttachAlias)
	if err != nil {
		t.Fatal(err)
	}

	wantPrefixes := []string{
		"INSTALL ducklake",
		"LOAD ducklake",
		"INSTALL aws",
		"LOAD aws",
		"INSTALL httpfs",
		"LOAD httpfs",
		"INSTALL postgres",
		"LOAD postgres",
		"CREATE OR REPLACE SECRET ingestr_ducklake_catalog_storage",
		"CREATE OR REPLACE SECRET ingestr_ducklake_catalog_catalog",
		"SELECT COUNT(*) FROM glob(", // storage probe — fails fast on missing bucket
		"ATTACH 'ducklake:postgres:'",
		"CREATE SCHEMA IF NOT EXISTS ducklake_catalog.main",
		"USE " + AttachAlias,
	}
	if len(stmts) != len(wantPrefixes) {
		t.Fatalf("statement count: want %d, got %d (stmts: %v)", len(wantPrefixes), len(stmts), stmts)
	}
	for i, prefix := range wantPrefixes {
		if !strings.HasPrefix(stmts[i], prefix) {
			t.Errorf("stmt %d: want prefix %q, got %q", i, prefix, stmts[i])
		}
	}
}

func TestGenerateAttachStatements_IncludesStorageProbe(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=duckdb&catalog_path=/tmp/m.duckdb"+
		"&storage_type=s3&storage_path=s3://my-bucket/lake"+
		"&storage_access_key=a&storage_secret_key=b")

	stmts, err := NewLakehouseAttacher().GenerateAttachStatements(cfg, AttachAlias)
	if err != nil {
		t.Fatal(err)
	}

	// Probe must reference the configured storage_path so a missing bucket
	// fails the bootstrap rather than slipping through and silently inlining.
	wantProbe := "SELECT COUNT(*) FROM glob('s3://my-bucket/lake/**')"
	found := false
	for _, s := range stmts {
		if s == wantProbe {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected storage probe %q in statements; got: %v", wantProbe, stmts)
	}
}

func TestTranslateProbeError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{
			"404 bucket missing",
			"HTTP Error: HTTP GET error (HTTP 404 Not Found)",
			"bucket does not exist",
		},
		{
			"NoSuchBucket S3 code",
			"S3 error: NoSuchBucket",
			"bucket does not exist",
		},
		{
			"AccessDenied",
			"S3 error: AccessDenied",
			"access denied",
		},
		{
			"403 forbidden",
			"HTTP Error: HTTP 403 Forbidden",
			"access denied",
		},
		{
			"connection refused",
			"Connection refused",
			"endpoint unreachable",
		},
		{
			"DNS failure",
			"Could not resolve host",
			"endpoint unreachable",
		},
		{
			"TLS handshake",
			"SSL handshake failed",
			"TLS error",
		},
		{
			"unknown error stays generic",
			"some random duckdb internal panic",
			"probe against",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := TranslateProbeError("s3://my-bucket/lake", errString(tc.raw))
			if got == nil {
				t.Fatal("expected non-nil error")
			}
			if !strings.Contains(got.Error(), tc.wantSub) {
				t.Errorf("translated error %q does not contain %q", got.Error(), tc.wantSub)
			}
		})
	}
}

func TestTranslateProbeError_NilStaysNil(t *testing.T) {
	t.Parallel()
	if TranslateProbeError("s3://b/p", nil) != nil {
		t.Error("nil input should produce nil output")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestGenerateAttachStatements_SkipsPostgresWhenCatalogIsFile(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=duckdb&catalog_path=/tmp/m.duckdb"+
		"&storage_type=s3&storage_path=s3://b/p"+
		"&storage_access_key=a&storage_secret_key=b")

	stmts, err := NewLakehouseAttacher().GenerateAttachStatements(cfg, AttachAlias)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if strings.Contains(s, "postgres") && !strings.Contains(s, "ducklake:postgres:") {
			t.Errorf("postgres-related statement should be absent for duckdb catalog: %s", s)
		}
	}
}

func TestGenerateS3Secret_MinIO(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=duckdb&catalog_path=/tmp/m.duckdb"+
		"&storage_type=s3&storage_path=s3://ducklake/warehouse"+
		"&storage_endpoint=minio.local:9000&storage_url_style=path&storage_use_ssl=false"+
		"&storage_access_key=AKID&storage_secret_key=SECRET")

	got := NewLakehouseAttacher().generateS3Secret(defaultSecretName(AttachAlias, "storage"), cfg.Storage)
	for _, want := range []string{
		"CREATE OR REPLACE SECRET ingestr_ducklake_catalog_storage (",
		"TYPE s3",
		"KEY_ID 'AKID'",
		"SECRET 'SECRET'",
		"ENDPOINT 'minio.local:9000'",
		"URL_STYLE 'path'",
		"USE_SSL false",
		"SCOPE 's3://ducklake/warehouse'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("storage secret missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestParseLakehouseURI_AzureStorage_ConnectionString(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=sqlite&catalog_path=/tmp/catalog.sqlite"+
		"&storage_type=azure&storage_path=az://ducklake/warehouse"+
		"&storage_connection_string=DefaultEndpointsProtocol%3Dhttps%3BAccountName%3Dacct%3BAccountKey%3Dkey")

	if cfg.Storage.Type != StorageTypeAzure || cfg.Storage.Path != "az://ducklake/warehouse" {
		t.Errorf("storage type/path: %+v", cfg.Storage)
	}
	if cfg.Storage.ConnectionString != "DefaultEndpointsProtocol=https;AccountName=acct;AccountKey=key" {
		t.Errorf("connection string: %q", cfg.Storage.ConnectionString)
	}
}

func TestParseLakehouseURI_AzureStorage_AccountName(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=sqlite&catalog_path=/tmp/catalog.sqlite"+
		"&storage_type=azure&storage_path=az://ducklake/warehouse&storage_account_name=acct")

	if cfg.Storage.AccountName != "acct" || cfg.Storage.ConnectionString != "" {
		t.Errorf("azure account auth: %+v", cfg.Storage)
	}
}

func TestParseLakehouseURI_AzureStorage_RequiresAuth(t *testing.T) {
	t.Parallel()

	_, err := ParseLakehouseURI("ducklake://?" +
		"catalog_type=sqlite&catalog_path=/tmp/catalog.sqlite" +
		"&storage_type=azure&storage_path=az://ducklake/warehouse")
	if err == nil || !strings.Contains(err.Error(), "storage_connection_string or storage_account_name is required") {
		t.Errorf("expected azure auth error, got: %v", err)
	}
}

func TestGetRequiredExtensions_Azure(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=postgres&catalog_host=h&catalog_database=d&catalog_username=u&catalog_password=p"+
		"&storage_type=azure&storage_path=az://ducklake/warehouse&storage_account_name=acct")

	exts := NewLakehouseAttacher().getRequiredExtensions(*cfg)
	for _, want := range []string{"ducklake", "azure", "postgres"} {
		found := false
		for _, e := range exts {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected extension %q in %v", want, exts)
		}
	}
}

func TestGenerateAzureSecret_ConnectionString(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=sqlite&catalog_path=/tmp/m.sqlite"+
		"&storage_type=azure&storage_path=az://ducklake/warehouse"+
		"&storage_connection_string=DefaultEndpointsProtocol%3Dhttps%3BAccountName%3Dacct%3BAccountKey%3Dkey")

	got := NewLakehouseAttacher().generateAzureSecret(defaultSecretName(AttachAlias, "storage"), cfg.Storage)
	for _, want := range []string{
		"CREATE OR REPLACE SECRET ingestr_ducklake_catalog_storage (",
		"TYPE azure",
		"CONNECTION_STRING 'DefaultEndpointsProtocol=https;AccountName=acct;AccountKey=key'",
		"SCOPE 'az://ducklake/warehouse'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("azure secret missing %q\nfull:\n%s", want, got)
		}
	}
	if strings.Contains(got, "credential_chain") {
		t.Errorf("connection-string secret must not use credential_chain\nfull:\n%s", got)
	}
}

func TestGenerateAzureSecret_CredentialChain(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=sqlite&catalog_path=/tmp/m.sqlite"+
		"&storage_type=azure&storage_path=az://ducklake/warehouse&storage_account_name=acct")

	got := NewLakehouseAttacher().generateAzureSecret(defaultSecretName(AttachAlias, "storage"), cfg.Storage)
	for _, want := range []string{
		"TYPE azure",
		"PROVIDER credential_chain",
		"ACCOUNT_NAME 'acct'",
		"SCOPE 'az://ducklake/warehouse'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("azure credential-chain secret missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestGenerateAttachStatements_AzureSetsCurlTransport(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=sqlite&catalog_path=/tmp/m.sqlite"+
		"&storage_type=azure&storage_path=az://ducklake/warehouse&storage_account_name=acct")

	stmts, err := NewLakehouseAttacher().GenerateAttachStatements(cfg, AttachAlias)
	if err != nil {
		t.Fatalf("GenerateAttachStatements: %v", err)
	}
	found := false
	for _, s := range stmts {
		if strings.Contains(s, "SET GLOBAL azure_transport_option_type = 'curl'") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected azure curl transport statement, got:\n%s", strings.Join(stmts, "\n"))
	}
}

func TestGeneratePostgresSecret(t *testing.T) {
	t.Parallel()

	cfg := mustParseLakehouse(t, "ducklake://?"+
		"catalog_type=postgres&catalog_host=meta.host&catalog_port=5433&catalog_database=ducklake_meta"+
		"&catalog_username=u&catalog_password=p"+
		"&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b")

	got := NewLakehouseAttacher().generatePostgresSecret(defaultSecretName(AttachAlias, "catalog"), cfg.Catalog)
	for _, want := range []string{
		"CREATE OR REPLACE SECRET ingestr_ducklake_catalog_catalog (",
		"TYPE postgres",
		"HOST 'meta.host'",
		"PORT 5433",
		"DATABASE 'ducklake_meta'",
		"USER 'u'",
		"PASSWORD 'p'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("catalog secret missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestGenerateDuckLakeAttach(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			"duckdb catalog",
			"ducklake://?catalog_type=duckdb&catalog_path=/tmp/meta.duckdb&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"ATTACH 'ducklake:/tmp/meta.duckdb' AS ducklake_catalog (DATA_PATH 's3://b/p', OVERRIDE_DATA_PATH true)",
		},
		{
			"sqlite catalog",
			"ducklake://?catalog_type=sqlite&catalog_path=/tmp/meta.sqlite&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"ATTACH 'ducklake:sqlite:/tmp/meta.sqlite' AS ducklake_catalog (DATA_PATH 's3://b/p', OVERRIDE_DATA_PATH true)",
		},
		{
			"postgres catalog",
			"ducklake://?catalog_type=postgres&catalog_host=h&catalog_database=d&catalog_username=u&catalog_password=p&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b",
			"ATTACH 'ducklake:postgres:' AS ducklake_catalog (DATA_PATH 's3://b/p', META_SECRET 'ingestr_ducklake_catalog_catalog', OVERRIDE_DATA_PATH true)",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := mustParseLakehouse(t, tc.raw)
			got, err := NewLakehouseAttacher().generateDuckLakeAttach(*cfg, AttachAlias)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("attach SQL mismatch\nwant: %s\ngot:  %s", tc.want, got)
			}
		})
	}
}

func TestDefaultSecretName(t *testing.T) {
	t.Parallel()
	cases := map[[2]string]string{
		{"lake", "storage"}:     "ingestr_lake_storage",
		{"lake", "catalog"}:     "ingestr_lake_catalog",
		{"my-lake!", "storage"}: "ingestr_my_lake__storage", // non-ident chars become '_'
		{"", "storage"}:         "ingestr_storage",
	}
	for in, want := range cases {
		got := defaultSecretName(in[0], in[1])
		if got != want {
			t.Errorf("defaultSecretName(%q, %q): got %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestQuoteSQLStringLiteral(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":              "''",
		"abc":           "'abc'",
		"a'b":           "'a''b'",
		"O'Brien's key": "'O''Brien''s key'",
	}
	for in, want := range cases {
		if got := quoteSQLStringLiteral(in); got != want {
			t.Errorf("quoteSQLStringLiteral(%q): got %q, want %q", in, got, want)
		}
	}
}

// -----------------------------------------------------------------------------
// DuckLakeDialect
// -----------------------------------------------------------------------------

func TestDuckLakeDialect_NameAndSchemes(t *testing.T) {
	t.Parallel()
	d := NewDuckLakeDialect()
	if d.Name() != "DUCKLAKE" {
		t.Errorf("name: %s", d.Name())
	}
	if got := d.Schemes(); len(got) != 1 || got[0] != "ducklake" {
		t.Errorf("schemes: %v", got)
	}
}

func TestDuckLakeDialect_BuildConnectionString_TranslatesToInMemoryDuckDB(t *testing.T) {
	t.Parallel()

	d := NewDuckLakeDialect()
	raw := "ducklake://?catalog_type=duckdb&catalog_path=/tmp/m.duckdb&storage_type=s3&storage_path=s3://b/p&storage_access_key=a&storage_secret_key=b"

	connStr, err := d.BuildConnectionString(raw)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(connStr, "driver=duckdb") || !strings.Contains(connStr, "path=:memory:") {
		t.Errorf("expected in-memory duckdb connection string, got: %s", connStr)
	}
	if d.cfg == nil || d.cfg.Catalog.Type != CatalogTypeDuckDB {
		t.Errorf("cfg not stashed correctly: %+v", d.cfg)
	}
}

func TestDuckLakeDialect_BuildConnectionString_PropagatesParseErrors(t *testing.T) {
	t.Parallel()
	d := NewDuckLakeDialect()
	if _, err := d.BuildConnectionString("ducklake://?storage_type=s3&storage_path=s3://b/p"); err == nil {
		t.Fatal("expected error for missing catalog_type")
	}
}
