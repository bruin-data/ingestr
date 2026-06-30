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
		"passfile", "replication", "requiressl", "service", "servicefile", "sslcert", "sslcompression",
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

	if storage != "" && storage != "s3" {
		return fmt.Errorf("iceberg uri: unsupported storage %q", storage)
	}

	if _, ok := cfg.Properties["warehouse"]; !ok {
		switch {
		case firstQueryValue(query, "warehouse_path", "warehouse-path") != "":
			cfg.Properties["warehouse"] = firstQueryValue(query, "warehouse_path", "warehouse-path")
		case bucket != "":
			cfg.Properties["warehouse"] = s3Location(bucket, prefix, true)
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
			cfg.TableLocation = s3Location(bucket, joinPathParts(prefix, tablePath), false)
		} else if warehouse := cfg.Properties.Get("warehouse", ""); strings.HasPrefix(warehouse, "s3://") {
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

func s3Location(bucket, path string, trailingSlash bool) string {
	bucket = strings.TrimPrefix(bucket, "s3://")
	bucket = strings.Trim(bucket, "/")
	out := "s3://" + bucket
	if path != "" {
		out += "/" + strings.Trim(path, "/")
	}
	if trailingSlash && !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
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
	aliasIfMissing(props, "endpoint", "s3.endpoint")
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
