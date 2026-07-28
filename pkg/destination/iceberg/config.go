package iceberg

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	iceberggo "github.com/apache/iceberg-go"
)

type icebergConfig struct {
	CatalogName     string
	Properties      iceberggo.Properties
	TableProperties iceberggo.Properties
	TableLocation   string
	CreateNamespace bool
}

func parseIcebergConfig(rawURI string) (icebergConfig, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return icebergConfig{}, fmt.Errorf("iceberg uri: failed to parse uri: %w", err)
	}

	cfg := icebergConfig{
		CatalogName:     "ingestr",
		Properties:      iceberggo.Properties{},
		TableProperties: iceberggo.Properties{},
		CreateNamespace: true,
	}

	if err := applyCatalogShorthand(parsed, &cfg); err != nil {
		return icebergConfig{}, err
	}

	query := parsed.Query()
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		value := values[0]
		switch key {
		case "catalog":
			cfg.Properties["type"] = normalizeCatalogType(value)
		case "type":
			cfg.Properties["type"] = normalizeCatalogType(value)
		case "catalog_name":
			cfg.CatalogName = value
		case "table_location", "table-location":
			cfg.TableLocation = value
		case "table_path", "table-path":
			continue
		case "create_namespace", "create-namespace":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return icebergConfig{}, fmt.Errorf("iceberg uri: invalid %s value %q: %w", key, value, err)
			}
			cfg.CreateNamespace = enabled
		default:
			if tableKey, ok := strings.CutPrefix(key, "table."); ok {
				cfg.TableProperties[tableKey] = value
				continue
			}
			if isFriendlyStorageParam(key) || isCatalogShorthandParam(key) {
				continue
			}
			cfg.Properties[key] = value
		}
	}

	if err := applyStorageShorthand(query, &cfg); err != nil {
		return icebergConfig{}, err
	}
	applyPropertyAliases(cfg.Properties)

	// The generic sql catalog needs both; iceberg+postgres/iceberg+sqlite set them,
	// iceberg+sql doesn't. Fail clearly instead of iceberg-go's opaque sql.Open error.
	if cfg.Properties["type"] == "sql" && (cfg.Properties["sql.driver"] == "" || cfg.Properties["sql.dialect"] == "") {
		return icebergConfig{}, fmt.Errorf("iceberg uri: sql catalog requires both sql.driver and sql.dialect (e.g. sql.driver=pgx&sql.dialect=postgres), or use the iceberg+postgres / iceberg+sqlite scheme which set them automatically")
	}

	// Non-AWS S3 endpoints (MinIO, GCS interop, R2) need iceberg-go's compat-mode;
	// enable it by default unless set explicitly.
	if cfg.Properties["s3.endpoint"] != "" {
		if _, ok := cfg.Properties["s3.compat-mode"]; !ok {
			cfg.Properties["s3.compat-mode"] = "true"
		}
	}
	return cfg, nil
}

func catalogTypeFromScheme(scheme string) string {
	const prefix = "iceberg+"
	if strings.HasPrefix(scheme, prefix) {
		return strings.TrimPrefix(scheme, prefix)
	}
	return ""
}

func normalizeCatalogType(value string) string {
	switch value {
	case "sqlite", "postgres":
		return "sql"
	default:
		return value
	}
}

func applyCatalogShorthand(parsed *url.URL, cfg *icebergConfig) error {
	switch parsed.Scheme {
	case "iceberg":
		return nil
	case "iceberg+sqlite":
		cfg.Properties["type"] = "sql"
		cfg.Properties["sql.dialect"] = "sqlite"
		cfg.Properties["sql.driver"] = "sqlite"
		if uri := sqliteCatalogURI(parsed); uri != "" {
			cfg.Properties["uri"] = uri
		}
	case "iceberg+postgres":
		cfg.Properties["type"] = "sql"
		cfg.Properties["sql.dialect"] = "postgres"
		cfg.Properties["sql.driver"] = "pgx"
		if parsed.Host != "" {
			cfg.Properties["uri"] = postgresCatalogURI(parsed)
		}
	case "iceberg+rest":
		cfg.Properties["type"] = "rest"
		if parsed.Host != "" {
			cfg.Properties["uri"] = catalogHTTPURL(parsed, "rest")
		}
	case "iceberg+hive":
		cfg.Properties["type"] = "hive"
		if parsed.Host != "" {
			cfg.Properties["uri"] = catalogURL(parsed, "thrift")
		}
	case "iceberg+hadoop":
		cfg.Properties["type"] = "hadoop"
		if parsed.Path != "" && parsed.Path != "/" {
			cfg.Properties["warehouse"] = parsed.Path
		}
	case "iceberg+glue":
		cfg.Properties["type"] = "glue"
	case "iceberg+sql":
		cfg.Properties["type"] = "sql"
	default:
		if catalogType := catalogTypeFromScheme(parsed.Scheme); catalogType != "" {
			cfg.Properties["type"] = catalogType
		}
	}
	return nil
}

func sqliteCatalogURI(parsed *url.URL) string {
	if parsed.Path == "" || parsed.Path == "/" {
		return ""
	}
	path := parsed.Path
	if strings.TrimPrefix(path, "/") == ":memory:" {
		return ":memory:"
	}
	if strings.HasPrefix(path, "file:") {
		return path
	}
	return "file:" + path
}

func catalogURL(parsed *url.URL, scheme string) string {
	out := &url.URL{
		Scheme: scheme,
		User:   parsed.User,
		Host:   parsed.Host,
		Path:   parsed.Path,
	}
	return out.String()
}

func postgresCatalogURI(parsed *url.URL) string {
	out := &url.URL{
		Scheme: "postgres",
		User:   parsed.User,
		Host:   parsed.Host,
		Path:   parsed.Path,
	}
	query := url.Values{}
	for key, values := range parsed.Query() {
		if !isPostgresDSNParam(key) {
			continue
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}
	out.RawQuery = query.Encode()
	return out.String()
}

func isPostgresDSNParam(key string) bool {
	switch key {
	case "application_name", "connect_timeout", "fallback_application_name", "krbsrvname", "options",
		"passfile", "password", "replication", "requiressl", "service", "servicefile", "sslcert", "sslcompression",
		"sslcrl", "sslcrldir", "sslkey", "sslmode", "sslpassword", "sslrootcert", "sslsni",
		"target_session_attrs", "tcp_user_timeout":
		return true
	default:
		return false
	}
}

func catalogHTTPURL(parsed *url.URL, catalog string) string {
	scheme := "http"
	query := parsed.Query()
	for _, key := range []string{catalog + "_use_ssl", catalog + "-use-ssl", "catalog_use_ssl", "catalog-use-ssl"} {
		if value := query.Get(key); value != "" {
			if enabled, err := strconv.ParseBool(value); err == nil && enabled {
				scheme = "https"
			}
			break
		}
	}
	return catalogURL(parsed, scheme)
}

func isCatalogShorthandParam(key string) bool {
	switch key {
	case "catalog_use_ssl", "catalog-use-ssl", "rest_use_ssl", "rest-use-ssl":
		return true
	default:
		return false
	}
}

func isFriendlyStorageParam(key string) bool {
	switch key {
	case "storage", "bucket", "warehouse_bucket", "warehouse-bucket", "warehouse_path", "warehouse-path",
		"prefix", "endpoint", "storage_endpoint", "storage-endpoint", "use_ssl", "use-ssl":
		return true
	default:
		return false
	}
}

func applyStorageShorthand(query url.Values, cfg *icebergConfig) error {
	storage := strings.ToLower(query.Get("storage"))
	bucket := firstQueryValue(query, "bucket", "warehouse_bucket", "warehouse-bucket")
	prefix := query.Get("prefix")

	// storage is optional; when set it must name a supported backend.
	switch storage {
	case "", "s3", "gcs", "local":
	default:
		return fmt.Errorf("iceberg uri: unsupported storage %q (supported: s3, gcs, local)", storage)
	}

	// bucket/prefix name an object store but carry no scheme, so storage must say
	// which one; without a bucket the warehouse URI scheme decides the backend.
	scheme := ""
	if bucket != "" {
		switch storage {
		case "s3":
			scheme = "s3://"
		case "gcs":
			scheme = "gs://"
		case "local":
			return fmt.Errorf("iceberg uri: bucket/prefix are not valid for local storage; use warehouse_path (a file:// path)")
		default: // storage == ""
			return fmt.Errorf("iceberg uri: bucket/prefix require storage=s3 or storage=gcs to set the warehouse scheme")
		}
	}

	if _, ok := cfg.Properties["warehouse"]; !ok {
		switch {
		case firstQueryValue(query, "warehouse_path", "warehouse-path") != "":
			cfg.Properties["warehouse"] = firstQueryValue(query, "warehouse_path", "warehouse-path")
		case bucket != "":
			cfg.Properties["warehouse"] = objectLocation(scheme, bucket, prefix, true)
		}
	}

	if endpoint := firstQueryValue(query, "endpoint", "storage_endpoint", "storage-endpoint"); endpoint != "" {
		normalized, err := normalizeStorageEndpoint(endpoint, firstQueryValue(query, "use_ssl", "use-ssl"))
		if err != nil {
			return err
		}
		if _, ok := cfg.Properties["s3.endpoint"]; !ok {
			cfg.Properties["s3.endpoint"] = normalized
		}
	}

	tablePath := firstQueryValue(query, "table_path", "table-path")
	if tablePath != "" && cfg.TableLocation == "" {
		if bucket != "" {
			cfg.TableLocation = objectLocation(scheme, bucket, joinPathParts(prefix, tablePath), false)
		} else if warehouse := cfg.Properties.Get("warehouse", ""); hasObjectStoreScheme(warehouse) {
			cfg.TableLocation = joinPathParts(warehouse, tablePath)
		}
	}

	return nil
}

func firstQueryValue(query url.Values, keys ...string) string {
	for _, key := range keys {
		if value := query.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func normalizeStorageEndpoint(endpoint, useSSL string) (string, error) {
	if strings.Contains(endpoint, "://") {
		return endpoint, nil
	}

	scheme := "https"
	if useSSL != "" {
		enabled, err := strconv.ParseBool(useSSL)
		if err != nil {
			return "", fmt.Errorf("iceberg uri: invalid use_ssl value %q: %w", useSSL, err)
		}
		if !enabled {
			scheme = "http"
		}
	}
	return scheme + "://" + endpoint, nil
}

// objectLocation builds a "<scheme>bucket/path" warehouse URI (scheme is "s3://"
// or "gs://"), trimming any duplicate scheme and stray slashes.
func objectLocation(scheme, bucket, path string, trailingSlash bool) string {
	bucket = strings.TrimPrefix(bucket, scheme)
	bucket = strings.Trim(bucket, "/")
	out := scheme + bucket
	if path != "" {
		out += "/" + strings.Trim(path, "/")
	}
	if trailingSlash && !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

// hasObjectStoreScheme reports whether the warehouse is an object-store URI
// (s3:// or gs://) under which a table location can be derived.
func hasObjectStoreScheme(warehouse string) bool {
	return strings.HasPrefix(warehouse, "s3://") || strings.HasPrefix(warehouse, "gs://")
}

func joinPathParts(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, "/")
}

func applyPropertyAliases(props iceberggo.Properties) {
	aliasIfMissing(props, "region", "glue.region")
	aliasIfMissing(props, "region", "s3.region")
	aliasIfMissing(props, "region_name", "glue.region")
	aliasIfMissing(props, "region_name", "s3.region")
	aliasIfMissing(props, "access_key_id", "glue.access-key-id")
	aliasIfMissing(props, "access_key_id", "s3.access-key-id")
	aliasIfMissing(props, "secret_access_key", "glue.secret-access-key")
	aliasIfMissing(props, "secret_access_key", "s3.secret-access-key")
	aliasIfMissing(props, "session_token", "glue.session-token")
	aliasIfMissing(props, "session_token", "s3.session-token")
	aliasIfMissing(props, "endpoint_url", "s3.endpoint")
}

func aliasIfMissing(props iceberggo.Properties, from, to string) {
	value, ok := props[from]
	if !ok || value == "" {
		return
	}
	if _, exists := props[to]; !exists {
		props[to] = value
	}
}
