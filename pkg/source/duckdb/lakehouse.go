package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
)

const AttachAlias = "lake"

type CatalogType string

const (
	CatalogTypeDuckDB   CatalogType = "duckdb"
	CatalogTypeSQLite   CatalogType = "sqlite"
	CatalogTypePostgres CatalogType = "postgres"
)

type StorageType string

const (
	StorageTypeS3  StorageType = "s3"
	StorageTypeGCS StorageType = "gcs"
)

type CatalogConfig struct {
	Type CatalogType

	// duckdb / sqlite
	Path string

	// postgres
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

type StorageConfig struct {
	Type StorageType
	Path string

	Region   string
	Endpoint string
	URLStyle string
	UseSSL   *bool

	AccessKey    string
	SecretKey    string
	SessionToken string
}

type LakehouseConfig struct {
	Catalog CatalogConfig
	Storage StorageConfig
}

func ParseLakehouseURI(raw string) (*LakehouseConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid ducklake URI: %w", err)
	}
	if u.Scheme != "ducklake" {
		return nil, fmt.Errorf("expected ducklake scheme, got %q", u.Scheme)
	}

	q := u.Query()
	cfg := &LakehouseConfig{}

	cfg.Catalog.Type = CatalogType(q.Get("catalog_type"))
	switch cfg.Catalog.Type {
	case CatalogTypeDuckDB, CatalogTypeSQLite:
		cfg.Catalog.Path = q.Get("catalog_path")
		if cfg.Catalog.Path == "" {
			return nil, fmt.Errorf("catalog_path is required for catalog_type=%s", cfg.Catalog.Type)
		}
	case CatalogTypePostgres:
		cfg.Catalog.Host = q.Get("catalog_host")
		cfg.Catalog.Database = q.Get("catalog_database")
		cfg.Catalog.Username = q.Get("catalog_username")
		cfg.Catalog.Password = q.Get("catalog_password")
		if v := q.Get("catalog_port"); v != "" {
			p, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("invalid catalog_port %q: %w", v, err)
			}
			cfg.Catalog.Port = p
		}
		if cfg.Catalog.Host == "" {
			return nil, fmt.Errorf("catalog_host is required for catalog_type=postgres")
		}
		if cfg.Catalog.Database == "" {
			return nil, fmt.Errorf("catalog_database is required for catalog_type=postgres")
		}
		if cfg.Catalog.Username == "" || cfg.Catalog.Password == "" {
			return nil, fmt.Errorf("catalog_username and catalog_password are required for catalog_type=postgres")
		}
	case "":
		return nil, fmt.Errorf("catalog_type is required (one of: duckdb, sqlite, postgres)")
	default:
		return nil, fmt.Errorf("unsupported catalog_type: %s", cfg.Catalog.Type)
	}

	cfg.Storage.Type = StorageType(q.Get("storage_type"))
	switch cfg.Storage.Type {
	case StorageTypeS3, StorageTypeGCS:
	case "":
		return nil, fmt.Errorf("storage_type is required (one of: s3, gcs)")
	default:
		return nil, fmt.Errorf("unsupported storage_type: %s", cfg.Storage.Type)
	}
	cfg.Storage.Path = q.Get("storage_path")
	if cfg.Storage.Path == "" {
		return nil, fmt.Errorf("storage_path is required")
	}
	cfg.Storage.Region = q.Get("storage_region")
	cfg.Storage.Endpoint = q.Get("storage_endpoint")
	cfg.Storage.URLStyle = q.Get("storage_url_style")
	if v := q.Get("storage_use_ssl"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid storage_use_ssl %q: %w", v, err)
		}
		cfg.Storage.UseSSL = &b
	}
	cfg.Storage.AccessKey = q.Get("storage_access_key")
	cfg.Storage.SecretKey = q.Get("storage_secret_key")
	cfg.Storage.SessionToken = q.Get("storage_session_token")
	if cfg.Storage.AccessKey == "" || cfg.Storage.SecretKey == "" {
		return nil, fmt.Errorf("storage_access_key and storage_secret_key are required for storage_type=%s", cfg.Storage.Type)
	}

	if cfg.Storage.URLStyle != "" && cfg.Storage.URLStyle != "path" && cfg.Storage.URLStyle != "vhost" {
		return nil, fmt.Errorf("unsupported storage_url_style %q (supported: path, vhost)", cfg.Storage.URLStyle)
	}

	return cfg, nil
}

type LakehouseAttacher struct{}

func NewLakehouseAttacher() *LakehouseAttacher { return &LakehouseAttacher{} }

func (l *LakehouseAttacher) GenerateAttachStatements(cfg *LakehouseConfig, alias string) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}

	extensions := l.getRequiredExtensions(*cfg)
	statements := make([]string, 0, len(extensions)*2+4)

	for _, ext := range extensions {
		statements = append(statements, "INSTALL "+ext)
		statements = append(statements, "LOAD "+ext)
	}

	statements = append(statements, l.generateSecretStatements(*cfg, alias)...)

	// Without this the bootstrap completes silently even when
	// the bucket is missing or credentials are wrong.
	statements = append(statements, probeStorageSQL(cfg.Storage.Path))

	attachStmt, err := l.generateDuckLakeAttach(*cfg, alias)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ATTACH statement: %w", err)
	}
	statements = append(
		statements,
		attachStmt,
		"CREATE SCHEMA IF NOT EXISTS "+alias+".main",
		"USE "+alias,
	)
	return statements, nil
}

func probeStorageSQL(storagePath string) string {
	return "SELECT COUNT(*) FROM glob(" + quoteSQLStringLiteral(storagePath+"/**") + ")"
}

func IsStorageProbe(sql string) bool {
	return strings.HasPrefix(strings.TrimSpace(sql), "SELECT COUNT(*) FROM glob(")
}

func TranslateProbeError(storagePath string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP 404"), strings.Contains(msg, "NoSuchBucket"):
		return fmt.Errorf(
			"storage path %q is not reachable — the bucket does not exist or the path is wrong. "+
				"Create the bucket first, or check that storage_path points to an existing bucket: %w",
			storagePath, err,
		)
	case strings.Contains(msg, "HTTP 403"), strings.Contains(msg, "AccessDenied"), strings.Contains(msg, "InvalidAccessKeyId"), strings.Contains(msg, "SignatureDoesNotMatch"):
		return fmt.Errorf(
			"storage access denied for %q — check storage_access_key and storage_secret_key, "+
				"and that the credentials can list the bucket: %w",
			storagePath, err,
		)
	case strings.Contains(msg, "Could not resolve host"), strings.Contains(msg, "Connection refused"), strings.Contains(msg, "no such host"), strings.Contains(msg, "Connection timed out"):
		return fmt.Errorf(
			"storage endpoint unreachable — check storage_endpoint (or that the S3 service is running and accessible): %w",
			err,
		)
	case strings.Contains(msg, "SSL"), strings.Contains(msg, "TLS"), strings.Contains(msg, "certificate"):
		return fmt.Errorf(
			"storage TLS error — if using plain HTTP (e.g. local MinIO), add storage_use_ssl=false to the URI: %w",
			err,
		)
	default:
		return fmt.Errorf("storage probe against %q failed: %w", storagePath, err)
	}
}

func (l *LakehouseAttacher) getRequiredExtensions(cfg LakehouseConfig) []string {
	exts := []string{"ducklake"}
	if cfg.Storage.Type == StorageTypeS3 {
		exts = append(exts, "aws", "httpfs")
	}
	if cfg.Storage.Type == StorageTypeGCS {
		exts = append(exts, "httpfs")
	}
	if cfg.Catalog.Type == CatalogTypePostgres {
		exts = append(exts, "postgres")
	}
	if cfg.Catalog.Type == CatalogTypeSQLite {
		exts = append(exts, "sqlite")
	}
	return deduplicateExtensions(exts)
}

func deduplicateExtensions(xs []string) []string {
	seen := make(map[string]bool, len(xs))
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (l *LakehouseAttacher) generateSecretStatements(cfg LakehouseConfig, alias string) []string {
	var stmts []string

	var storageSecret string
	switch cfg.Storage.Type {
	case StorageTypeS3:
		storageSecret = l.generateS3Secret(defaultSecretName(alias, "storage"), cfg.Storage)
	case StorageTypeGCS:
		storageSecret = l.generateGCSSecret(defaultSecretName(alias, "storage"), cfg.Storage)
	}
	if storageSecret != "" {
		stmts = append(stmts, storageSecret)
	}

	if catalogSecret := l.generateCatalogSecret(defaultSecretName(alias, "catalog"), cfg.Catalog); catalogSecret != "" {
		stmts = append(stmts, catalogSecret)
	}
	return stmts
}

func (l *LakehouseAttacher) generateS3Secret(name string, st StorageConfig) string {
	if st.AccessKey == "" || st.SecretKey == "" {
		return ""
	}
	parts := []string{
		"CREATE OR REPLACE SECRET " + name + " (",
		"    TYPE s3",
		",   PROVIDER config",
		",   KEY_ID " + quoteSQLStringLiteral(st.AccessKey),
		",   SECRET " + quoteSQLStringLiteral(st.SecretKey),
	}
	if st.SessionToken != "" {
		parts = append(parts, ",   SESSION_TOKEN "+quoteSQLStringLiteral(st.SessionToken))
	}
	if st.Region != "" {
		parts = append(parts, ",   REGION "+quoteSQLStringLiteral(st.Region))
	}
	if st.Endpoint != "" {
		parts = append(parts, ",   ENDPOINT "+quoteSQLStringLiteral(st.Endpoint))
	}
	if st.URLStyle != "" {
		parts = append(parts, ",   URL_STYLE "+quoteSQLStringLiteral(st.URLStyle))
	}
	if st.UseSSL != nil {
		parts = append(parts, ",   USE_SSL "+strconv.FormatBool(*st.UseSSL))
	}
	scope := st.Path
	if scope == "" {
		scope = "s3://"
	}
	parts = append(parts, ",   SCOPE "+quoteSQLStringLiteral(scope), ")")
	return strings.Join(parts, "\n")
}

func (l *LakehouseAttacher) generateGCSSecret(name string, st StorageConfig) string {
	if st.AccessKey == "" || st.SecretKey == "" {
		return ""
	}
	scope := st.Path
	if scope == "" {
		scope = "gs://"
	}
	parts := []string{
		"CREATE OR REPLACE SECRET " + name + " (",
		"    TYPE gcs",
		",   KEY_ID " + quoteSQLStringLiteral(st.AccessKey),
		",   SECRET " + quoteSQLStringLiteral(st.SecretKey),
		",   SCOPE " + quoteSQLStringLiteral(scope),
		")",
	}
	return strings.Join(parts, "\n")
}

func (l *LakehouseAttacher) generateCatalogSecret(name string, cat CatalogConfig) string {
	if cat.Type != CatalogTypePostgres {
		return ""
	}
	return l.generatePostgresSecret(name, cat)
}

func (l *LakehouseAttacher) generatePostgresSecret(name string, cat CatalogConfig) string {
	if cat.Username == "" || cat.Password == "" || cat.Host == "" || cat.Database == "" {
		return ""
	}
	port := cat.Port
	if port == 0 {
		port = 5432
	}
	parts := []string{
		"CREATE OR REPLACE SECRET " + name + " (",
		"    TYPE postgres",
		",   HOST " + quoteSQLStringLiteral(cat.Host),
		",   PORT " + strconv.Itoa(port),
		",   DATABASE " + quoteSQLStringLiteral(cat.Database),
		",   USER " + quoteSQLStringLiteral(cat.Username),
		",   PASSWORD " + quoteSQLStringLiteral(cat.Password),
		")",
	}
	return strings.Join(parts, "\n")
}

func (l *LakehouseAttacher) generateDuckLakeAttach(cfg LakehouseConfig, alias string) (string, error) {
	dataOpt := "DATA_PATH " + quoteSQLStringLiteral(cfg.Storage.Path)

	switch cfg.Catalog.Type {
	case CatalogTypePostgres:
		opts := []string{
			dataOpt,
			"META_SECRET " + quoteSQLStringLiteral(defaultSecretName(alias, "catalog")),
			"OVERRIDE_DATA_PATH true",
		}
		return "ATTACH 'ducklake:postgres:' AS " + alias + " (" + strings.Join(opts, ", ") + ")", nil

	case CatalogTypeDuckDB:
		path := strings.TrimPrefix(strings.TrimSpace(cfg.Catalog.Path), "ducklake:")
		opts := []string{dataOpt, "OVERRIDE_DATA_PATH true"}
		return "ATTACH 'ducklake:" + escapeSQLStringLiteral(path) + "' AS " + alias + " (" + strings.Join(opts, ", ") + ")", nil

	case CatalogTypeSQLite:
		path := strings.TrimPrefix(strings.TrimSpace(cfg.Catalog.Path), "ducklake:")
		path = strings.TrimPrefix(path, "sqlite:")
		opts := []string{dataOpt, "OVERRIDE_DATA_PATH true"}
		return "ATTACH 'ducklake:sqlite:" + escapeSQLStringLiteral(path) + "' AS " + alias + " (" + strings.Join(opts, ", ") + ")", nil

	default:
		return "", fmt.Errorf("unsupported catalog type for ducklake: %s", cfg.Catalog.Type)
	}
}

func defaultSecretName(alias, kind string) string {
	if alias == "" {
		return "ingestr_" + kind
	}
	return "ingestr_" + sanitizeIdentifier(alias) + "_" + kind
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func escapeSQLStringLiteral(s string) string { return strings.ReplaceAll(s, "'", "''") }
func quoteSQLStringLiteral(s string) string  { return "'" + escapeSQLStringLiteral(s) + "'" }

func isSecretStatement(sql string) bool {
	return strings.HasPrefix(strings.TrimSpace(sql), "CREATE OR REPLACE SECRET")
}

type DuckLakeDialect struct {
	*Dialect
	cfg *LakehouseConfig
}

func NewDuckLakeDialect() *DuckLakeDialect {
	return &DuckLakeDialect{Dialect: NewDialect()}
}

func (d *DuckLakeDialect) Name() string      { return "DUCKLAKE" }
func (d *DuckLakeDialect) Schemes() []string { return []string{"ducklake"} }

func (d *DuckLakeDialect) BuildConnectionString(uri string) (string, error) {
	cfg, err := ParseLakehouseURI(uri)
	if err != nil {
		return "", fmt.Errorf("ducklake: %w", err)
	}
	d.cfg = cfg
	return d.Dialect.BuildConnectionString("duckdb://:memory:")
}

func (d *DuckLakeDialect) SetConnection(db *sql.DB) {
	if d.cfg == nil {
		return
	}

	// Pin the *sql.DB pool to exactly one physical connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	stmts, err := NewLakehouseAttacher().GenerateAttachStatements(d.cfg, AttachAlias)
	if err != nil {
		config.Debug("[DUCKLAKE] %v", err)
		return
	}

	for _, stmt := range stmts {
		if isSecretStatement(stmt) {
			config.Debug("[DUCKLAKE] CREATE OR REPLACE SECRET (redacted)") // Hide secrets in debug log
		} else {
			config.Debug("[DUCKLAKE] %s", stmt)
		}
		if err := execStmt(db, stmt); err != nil {
			if IsStorageProbe(stmt) {
				config.Debug("[DUCKLAKE] %v", TranslateProbeError(d.cfg.Storage.Path, err))
			} else {
				config.Debug("[DUCKLAKE] bootstrap failed at %q: %v", firstLine(stmt), err)
			}
			return
		}
	}
}

func execStmt(db *sql.DB, stmt string) error {
	rows, err := db.QueryContext(context.Background(), stmt)
	if err != nil {
		return err
	}
	return rows.Close()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
