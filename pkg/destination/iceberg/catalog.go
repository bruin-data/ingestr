package iceberg

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergrest "github.com/apache/iceberg-go/catalog/rest"
	icebergsql "github.com/apache/iceberg-go/catalog/sql"
	icebergtable "github.com/apache/iceberg-go/table"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/bruin-data/ingestr/pkg/schema"
)

const s3TablesIdentifierMaxLength = 255

type ownedSQLCatalog struct {
	icebergcatalog.Catalog
	db        *sql.DB
	closeOnce sync.Once
	closeErr  error
}

func (c *ownedSQLCatalog) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.db.Close()
	})
	return c.closeErr
}

func (c *ownedSQLCatalog) PurgeTable(ctx context.Context, identifier icebergtable.Identifier) error {
	purger, ok := c.Catalog.(icebergcatalog.PurgeableTable)
	if !ok {
		return errors.New("iceberg: SQL catalog does not support physical table purge")
	}
	return purger.PurgeTable(ctx, identifier)
}

func loadIcebergCatalog(ctx context.Context, cfg icebergConfig) (icebergcatalog.Catalog, error) {
	if cfg.Properties.Get("type", "") == "sql" {
		return loadOwnedSQLCatalog(cfg)
	}
	if cfg.Properties.Get("rest.signing-name", "") != "s3tables" {
		cat, err := icebergcatalog.Load(ctx, cfg.CatalogName, cfg.Properties)
		if err != nil {
			return nil, err
		}
		if cfg.Properties.Get("type", "") == "hive" {
			return &hiveFileCatalog{Catalog: cat}, nil
		}
		return cat, nil
	}

	region := cfg.Properties.Get("rest.signing-region", "")
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	accessKey := cfg.Properties.Get("s3.access-key-id", "")
	secretKey := cfg.Properties.Get("s3.secret-access-key", "")
	if (accessKey == "") != (secretKey == "") {
		return nil, fmt.Errorf("iceberg: access_key_id and secret_access_key must be provided together")
	}
	if accessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			cfg.Properties.Get("s3.session-token", ""),
		)))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to load AWS configuration for S3 Tables: %w", err)
	}

	additional := iceberggo.Properties{}
	for key, value := range cfg.Properties {
		switch key {
		case "type", "uri", "warehouse", "rest.sigv4-enabled", "rest.signing-region", "rest.signing-name", "rest.tls.skip-verify":
			continue
		default:
			additional[key] = value
		}
	}
	opts := []icebergrest.Option{
		icebergrest.WithWarehouseLocation(cfg.Properties.Get("warehouse", "")),
		icebergrest.WithSigV4RegionSvc(region, "s3tables"),
		icebergrest.WithAwsConfig(awsCfg),
		icebergrest.WithAdditionalProps(additional),
	}
	if strings.EqualFold(cfg.Properties.Get("rest.tls.skip-verify", ""), "true") {
		//nolint:gosec // Explicit user opt-in for private/local REST endpoints.
		opts = append(opts, icebergrest.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	}
	return icebergrest.NewCatalog(ctx, cfg.CatalogName, cfg.Properties.Get("uri", ""), opts...)
}

func loadOwnedSQLCatalog(cfg icebergConfig) (icebergcatalog.Catalog, error) {
	driver := strings.TrimSpace(cfg.Properties[icebergsql.DriverKey])
	if driver == "" {
		return nil, errors.New("must provide driver to pass to sql.Open")
	}
	dialect := strings.ToLower(strings.TrimSpace(cfg.Properties[icebergsql.DialectKey]))
	if dialect == "" {
		return nil, errors.New("must provide sql dialect to use")
	}
	supportedDialect := icebergsql.SupportedDialect(dialect)
	switch supportedDialect {
	case icebergsql.Postgres, icebergsql.MySQL, icebergsql.SQLite, icebergsql.MSSQL, icebergsql.Oracle:
	default:
		return nil, fmt.Errorf("unsupported SQL catalog dialect %q", dialect)
	}

	dsn := strings.TrimPrefix(cfg.Properties.Get("uri", ""), "sql://")
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	catalogName := "sql"
	if cfg.CatalogNameExplicit {
		catalogName = cfg.CatalogName
	}
	cat, err := icebergsql.NewCatalog(
		catalogName,
		db,
		supportedDialect,
		cfg.Properties,
	)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &ownedSQLCatalog{Catalog: cat, db: db}, nil
}

func validateS3TablesIdentifier(cfg icebergConfig, ident icebergtable.Identifier, tableSchema *schema.TableSchema) error {
	if cfg.Properties.Get("rest.signing-name", "") != "s3tables" {
		return nil
	}
	if len(ident) != 2 {
		return fmt.Errorf("iceberg: S3 Tables requires a single-level namespace and table identifier")
	}
	for i, part := range ident {
		kind := "table"
		if i == 0 {
			kind = "namespace"
		}
		if err := validateS3TablesName(kind, part); err != nil {
			return err
		}
	}
	if strings.HasPrefix(ident[0], "aws") {
		return fmt.Errorf("iceberg: S3 Tables namespace name must not start with reserved prefix %q: %q", "aws", ident[0])
	}
	for _, col := range tableSchema.Columns {
		if col.Name != strings.ToLower(col.Name) {
			return fmt.Errorf("iceberg: S3 Tables column names must be lowercase: %q", col.Name)
		}
	}
	return nil
}

func validateS3TablesName(kind, name string) error {
	if len(name) < 1 || len(name) > s3TablesIdentifierMaxLength {
		return fmt.Errorf("iceberg: S3 Tables %s name must be between 1 and %d characters: %q", kind, s3TablesIdentifierMaxLength, name)
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("iceberg: S3 Tables namespace and table names must be lowercase: %q", name)
	}
	for i, r := range name {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
		if !valid {
			return fmt.Errorf("iceberg: S3 Tables %s name may contain only lowercase letters, numbers, and underscores: %q", kind, name)
		}
		if i == 0 && r == '_' {
			return fmt.Errorf("iceberg: S3 Tables %s name must begin with a letter or number: %q", kind, name)
		}
	}
	return nil
}
