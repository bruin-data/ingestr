package iceberg

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	iceberggo "github.com/apache/iceberg-go"
)

type icebergConfig struct {
	CatalogName         string
	CatalogNameExplicit bool
	Properties          iceberggo.Properties
	TableProperties     iceberggo.Properties
	TableLocation       string
	PurgeJournalRoot    string
	PartitionSpec       string
	CheckNamespace      string
	CreateNamespace     bool
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
	if err := validateCatalogTypeOverride(parsed.Scheme, query); err != nil {
		return icebergConfig{}, err
	}
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
			if strings.TrimSpace(value) == "" {
				return icebergConfig{}, fmt.Errorf("iceberg uri: catalog_name must not be empty")
			}
			cfg.CatalogName = value
			cfg.CatalogNameExplicit = true
		case "table_location", "table-location":
			cfg.TableLocation = value
		case "partition_spec", "partition-spec":
			cfg.PartitionSpec = value
		case "check_namespace", "check-namespace":
			namespace, err := normalizeCheckNamespace(value)
			if err != nil {
				return icebergConfig{}, err
			}
			cfg.CheckNamespace = namespace
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
				switch tableKey {
				case managedTableProperty, managedTableKindProperty, managedTableExpiresAt, managedTableExpiresAfterMS, managedTablePurgeClaim, atomicSnapshotAttemptProperty, atomicSnapshotTargetProperty, atomicSnapshotTargetUUID, tableCommitTokenLedgerKey, tableCDCResumeStateKey, prepareOwnershipProperty:
					return icebergConfig{}, fmt.Errorf("iceberg uri: table property %q is managed internally", tableKey)
				}
				cfg.TableProperties[tableKey] = value
				continue
			}
			if isFriendlyStorageParam(key) || isCatalogShorthandParam(key) ||
				(parsed.Scheme == "iceberg+nessie" && (key == "branch" || key == "ref")) {
				continue
			}
			cfg.Properties[key] = value
		}
	}

	if err := applyStorageShorthand(parsed.Scheme, query, &cfg); err != nil {
		return icebergConfig{}, err
	}
	if err := applyCatalogPropertyShorthand(parsed.Scheme, query, &cfg); err != nil {
		return icebergConfig{}, err
	}
	applyPropertyAliases(cfg.Properties)
	if token := strings.TrimSpace(cfg.Properties["token"]); token != "" {
		if _, exists := cfg.Properties["header.Authorization"]; !exists {
			cfg.Properties["header.Authorization"] = "Bearer " + token
		}
		delete(cfg.Properties, "token")
	}
	if err := validateCatalogShorthand(parsed.Scheme, cfg); err != nil {
		return icebergConfig{}, err
	}
	return cfg, nil
}

func validateCatalogTypeOverride(scheme string, query url.Values) error {
	if scheme == "iceberg" {
		return nil
	}
	expected := normalizeCatalogType(catalogTypeFromScheme(scheme))
	if expected == "" {
		return nil
	}
	for _, key := range []string{"catalog", "type"} {
		values, exists := query[key]
		if !exists || len(values) == 0 {
			continue
		}
		actual := normalizeCatalogType(values[0])
		if actual != expected {
			return fmt.Errorf("iceberg uri: %s requires catalog type %q; %s=%q conflicts with the URI scheme", scheme, expected, key, values[0])
		}
	}
	return nil
}

func normalizeCheckNamespace(value string) (string, error) {
	ident, err := parseIdentifier(value)
	if err != nil {
		return "", fmt.Errorf("iceberg uri: invalid check_namespace %q: %w", value, err)
	}
	for _, part := range ident {
		if part != strings.TrimSpace(part) {
			return "", fmt.Errorf("iceberg uri: invalid check_namespace %q: identifier components must not have surrounding whitespace", value)
		}
	}
	return strings.Join(ident, "."), nil
}

func catalogTypeFromScheme(scheme string) string {
	const prefix = "iceberg+"
	if strings.HasPrefix(scheme, prefix) {
		return strings.TrimPrefix(scheme, prefix)
	}
	return ""
}

func normalizeCatalogType(value string) string {
	normalized := strings.ToLower(value)
	switch normalized {
	case "sqlite", "postgres":
		return "sql"
	case "nessie", "polaris", "s3tables":
		return "rest"
	default:
		return normalized
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
			uri, err := catalogHTTPURL(parsed, "rest", false)
			if err != nil {
				return err
			}
			cfg.Properties["uri"] = uri
		}
	case "iceberg+nessie":
		cfg.Properties["type"] = "rest"
		if parsed.Host != "" {
			withDefaultPath := *parsed
			if withDefaultPath.Path == "" || withDefaultPath.Path == "/" {
				withDefaultPath.Path = "/iceberg"
			}
			uri, err := catalogHTTPURL(&withDefaultPath, "nessie", false)
			if err != nil {
				return err
			}
			cfg.Properties["uri"] = uri
		}
	case "iceberg+polaris":
		cfg.Properties["type"] = "rest"
		if parsed.Host != "" {
			withDefaultPath := *parsed
			if withDefaultPath.Path == "" || withDefaultPath.Path == "/" {
				withDefaultPath.Path = "/api/catalog"
			}
			uri, err := catalogHTTPURL(&withDefaultPath, "polaris", true)
			if err != nil {
				return err
			}
			cfg.Properties["uri"] = uri
		}
	case "iceberg+s3tables":
		cfg.Properties["type"] = "rest"
		cfg.Properties["rest.sigv4-enabled"] = "true"
		cfg.Properties["rest.signing-name"] = "s3tables"
		region := firstQueryValue(parsed.Query(), "rest.signing-region", "region", "region_name")
		if region != "" {
			cfg.Properties["rest.signing-region"] = region
			cfg.Properties["client.region"] = region
		}
		if parsed.Host != "" {
			withDefaultPath := *parsed
			if withDefaultPath.Path == "" || withDefaultPath.Path == "/" {
				withDefaultPath.Path = "/iceberg"
			}
			uri, err := catalogHTTPURL(&withDefaultPath, "s3tables", true)
			if err != nil {
				return err
			}
			cfg.Properties["uri"] = uri
		} else if region != "" {
			cfg.Properties["uri"] = "https://s3tables." + region + ".amazonaws.com/iceberg"
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

func catalogHTTPURL(parsed *url.URL, catalog string, defaultTLS bool) (string, error) {
	scheme := "http"
	if defaultTLS {
		scheme = "https"
	}
	query := parsed.Query()
	for _, key := range []string{catalog + "_use_ssl", catalog + "-use-ssl", "catalog_use_ssl", "catalog-use-ssl"} {
		if value := query.Get(key); value != "" {
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return "", fmt.Errorf("iceberg uri: invalid %s value %q: %w", key, value, err)
			}
			if enabled {
				scheme = "https"
			} else {
				scheme = "http"
			}
			break
		}
	}
	return catalogURL(parsed, scheme), nil
}

func isCatalogShorthandParam(key string) bool {
	switch key {
	case "catalog_use_ssl", "catalog-use-ssl", "rest_use_ssl", "rest-use-ssl",
		"nessie_use_ssl", "nessie-use-ssl", "polaris_use_ssl", "polaris-use-ssl",
		"s3tables_use_ssl", "s3tables-use-ssl", "catalog_prefix", "catalog-prefix",
		"oauth_client_id", "oauth-client-id", "oauth_client_secret", "oauth-client-secret", "oauth_token", "oauth-token",
		"nessie_branch", "nessie-branch", "nessie_warehouse", "nessie-warehouse",
		"polaris_realm", "polaris-realm":
		return true
	default:
		return false
	}
}

func applyCatalogPropertyShorthand(scheme string, query url.Values, cfg *icebergConfig) error {
	setIfMissing(cfg.Properties, "prefix", firstQueryValue(query, "catalog_prefix", "catalog-prefix"))
	setIfMissing(cfg.Properties, "token", firstQueryValue(query, "oauth_token", "oauth-token"))

	clientID := firstQueryValue(query, "oauth_client_id", "oauth-client-id")
	clientSecret := firstQueryValue(query, "oauth_client_secret", "oauth-client-secret")
	if (clientID == "") != (clientSecret == "") {
		return fmt.Errorf("iceberg uri: oauth_client_id and oauth_client_secret must be provided together")
	}
	if clientID != "" {
		setIfMissing(cfg.Properties, "credential", clientID+":"+clientSecret)
	}
	setIfMissing(cfg.Properties, "header.Polaris-Realm", firstQueryValue(query, "polaris_realm", "polaris-realm"))

	if scheme == "iceberg+nessie" {
		if err := applyNessieReferenceShorthand(query, cfg.Properties); err != nil {
			return err
		}
	}

	if credType := cfg.Properties.Get("gcs.credtype", ""); credType != "" {
		if err := validateGCSCredentialType(credType); err != nil {
			return err
		}
	}
	return nil
}

func validateCatalogShorthand(scheme string, cfg icebergConfig) error {
	switch scheme {
	case "iceberg+nessie", "iceberg+polaris":
		if cfg.Properties.Get("type", "") != "rest" {
			return fmt.Errorf("iceberg uri: %s requires the REST catalog type", scheme)
		}
		if err := validateRESTCatalogURI(cfg.Properties.Get("uri", "")); err != nil {
			return err
		}
	}
	if scheme == "iceberg+nessie" && cfg.Properties.Get("prefix", "") != "" {
		return fmt.Errorf("iceberg uri: Nessie branches are selected with nessie_branch, not catalog_prefix")
	}
	if scheme == "iceberg+nessie" && cfg.Properties.Get("warehouse", "") != "" {
		return fmt.Errorf("iceberg uri: Nessie warehouses are selected with nessie_warehouse, not warehouse")
	}
	if scheme == "iceberg+polaris" && cfg.Properties.Get("warehouse", "") == "" {
		return fmt.Errorf("iceberg uri: iceberg+polaris requires warehouse (the Polaris catalog name)")
	}

	if scheme != "iceberg+s3tables" {
		return nil
	}
	if cfg.Properties.Get("type", "") != "rest" {
		return fmt.Errorf("iceberg uri: %s requires the REST catalog type", scheme)
	}
	region := cfg.Properties.Get("rest.signing-region", "")
	if region == "" {
		return fmt.Errorf("iceberg uri: iceberg+s3tables requires region or rest.signing-region")
	}
	if err := validateRESTCatalogURI(cfg.Properties.Get("uri", "")); err != nil {
		return err
	}
	if cfg.Properties.Get("rest.sigv4-enabled", "") != "true" {
		return fmt.Errorf("iceberg uri: iceberg+s3tables requires rest.sigv4-enabled=true")
	}
	if cfg.Properties.Get("rest.signing-name", "") != "s3tables" {
		return fmt.Errorf("iceberg uri: iceberg+s3tables requires rest.signing-name=s3tables")
	}
	warehouse := cfg.Properties.Get("warehouse", "")
	if err := validateS3TablesWarehouse(warehouse, region); err != nil {
		return err
	}
	return nil
}

func validateS3TablesWarehouse(warehouse, region string) error {
	parts := strings.SplitN(warehouse, ":", 6)
	bucketName := ""
	if len(parts) == 6 {
		bucketName = strings.TrimPrefix(parts[5], "bucket/")
	}
	if len(parts) != 6 || parts[0] != "arn" || parts[1] == "" || parts[2] != "s3tables" ||
		parts[3] == "" || len(parts[4]) != 12 || !allASCIIDigits(parts[4]) ||
		!strings.HasPrefix(parts[5], "bucket/") || !validS3TablesBucketName(bucketName) {
		return fmt.Errorf("iceberg uri: iceberg+s3tables requires an S3 Tables bucket ARN in warehouse")
	}
	if parts[3] != region {
		return fmt.Errorf("iceberg uri: S3 Tables warehouse region %q does not match signing region %q", parts[3], region)
	}
	return nil
}

func validS3TablesBucketName(value string) bool {
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	if value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, prefix := range []string{"xn--", "sthree-", "amzn-s3-demo-", "aws"} {
		if strings.HasPrefix(value, prefix) {
			return false
		}
	}
	for _, suffix := range []string{"-s3alias", "--ol-s3", "--x-s3", "--table-s3"} {
		if strings.HasSuffix(value, suffix) {
			return false
		}
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func allASCIIDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func applyNessieReferenceShorthand(query url.Values, props iceberggo.Properties) error {
	branch := strings.TrimSpace(firstQueryValue(query, "nessie_branch", "nessie-branch", "branch", "ref"))
	warehouse := strings.TrimSpace(firstQueryValue(query, "nessie_warehouse", "nessie-warehouse"))
	if branch == "" && warehouse == "" {
		return nil
	}
	if strings.Contains(branch, "|") {
		return fmt.Errorf("iceberg uri: invalid Nessie branch %q", branch)
	}
	if branch != "" && strings.Trim(branch, "/") == "" {
		return fmt.Errorf("iceberg uri: invalid Nessie branch %q", branch)
	}
	if strings.ContainsAny(warehouse, "/|") {
		return fmt.Errorf("iceberg uri: invalid Nessie warehouse %q", warehouse)
	}

	rawURI := props.Get("uri", "")
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("iceberg uri: invalid Nessie REST endpoint %q", rawURI)
	}
	path := strings.TrimRight(parsed.Path, "/") + "/"
	rawPath := strings.TrimRight(parsed.EscapedPath(), "/") + "/"
	if branch != "" {
		branch = strings.Trim(branch, "/")
		path += branch
		rawPath += url.PathEscape(branch)
	}
	if warehouse != "" {
		path += "|" + warehouse
		rawPath += "%7C" + url.PathEscape(warehouse)
	}
	parsed.Path = path
	parsed.RawPath = rawPath
	props["uri"] = parsed.String()
	return nil
}

func validateRESTCatalogURI(rawURI string) error {
	if rawURI == "" {
		return fmt.Errorf("iceberg uri: REST catalog endpoint is required")
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("iceberg uri: invalid REST catalog endpoint %q", rawURI)
	}
	return nil
}

func isFriendlyStorageParam(key string) bool {
	switch key {
	case "storage", "bucket", "warehouse_bucket", "warehouse-bucket", "warehouse_path", "warehouse-path",
		"prefix", "storage_prefix", "storage-prefix", "endpoint", "storage_endpoint", "storage-endpoint", "use_ssl", "use-ssl",
		"gcs_json_key", "gcs-json-key", "gcs_key_path", "gcs-key-path", "gcs_credential_type", "gcs-credential-type",
		"gcs_use_json_api", "gcs-use-json-api", "container", "account_name", "account-name", "account_key", "account-key",
		"sas_token", "sas-token", "connection_string", "connection-string", "managed_identity", "managed-identity",
		"client_id", "client-id", "azure_host", "azure-host", "adls_scheme", "adls-scheme":
		return true
	default:
		return false
	}
}

func applyStorageShorthand(catalogScheme string, query url.Values, cfg *icebergConfig) error {
	storage := strings.ToLower(query.Get("storage"))
	if storage == "azure" {
		storage = "adls"
	}
	if storage == "" {
		storage = "s3"
	}
	if storage != "s3" && storage != "gcs" && storage != "adls" {
		return fmt.Errorf("iceberg uri: unsupported storage %q", storage)
	}

	bucket := firstQueryValue(query, "bucket", "warehouse_bucket", "warehouse-bucket")
	if storage == "adls" {
		bucket = firstQueryValue(query, "container", "bucket", "warehouse_bucket", "warehouse-bucket")
	}
	prefix := firstQueryValue(query, "storage_prefix", "storage-prefix", "prefix")
	warehousePath := firstQueryValue(query, "warehouse_path", "warehouse-path")
	if catalogScheme == "iceberg+nessie" && warehousePath != "" {
		return fmt.Errorf("iceberg uri: Nessie warehouses are selected with nessie_warehouse, not warehouse_path")
	}

	var generatedWarehouse string
	if bucket != "" {
		var err error
		generatedWarehouse, err = storageLocation(storage, bucket, prefix, true, query)
		if err != nil {
			return err
		}
	}
	if warehousePath != "" {
		cfg.PurgeJournalRoot = warehousePath
	} else if generatedWarehouse != "" {
		cfg.PurgeJournalRoot = generatedWarehouse
	}

	catalogUsesNamedWarehouse := catalogScheme == "iceberg+nessie" || catalogScheme == "iceberg+polaris"
	if _, ok := cfg.Properties["warehouse"]; !ok && !catalogUsesNamedWarehouse {
		switch {
		case warehousePath != "":
			cfg.Properties["warehouse"] = warehousePath
		case generatedWarehouse != "":
			cfg.Properties["warehouse"] = generatedWarehouse
		}
	}
	if query.Get("storage") != "" && bucket == "" && cfg.Properties.Get("warehouse", "") == "" && cfg.TableLocation == "" {
		return fmt.Errorf("iceberg uri: %s storage shorthand requires bucket/container or an explicit warehouse", storage)
	}

	if err := applyStorageProperties(storage, query, cfg.Properties); err != nil {
		return err
	}

	tablePath := firstQueryValue(query, "table_path", "table-path")
	if tablePath != "" && cfg.TableLocation == "" {
		if generatedWarehouse != "" {
			cfg.TableLocation = appendLocationPath(generatedWarehouse, tablePath, false)
		} else if warehouse := cfg.Properties.Get("warehouse", ""); isObjectStorageLocation(warehouse) {
			cfg.TableLocation = appendLocationPath(warehouse, tablePath, false)
		}
	}

	return nil
}

func applyStorageProperties(storage string, query url.Values, props iceberggo.Properties) error {
	endpoint := firstQueryValue(query, "endpoint", "storage_endpoint", "storage-endpoint")
	useSSL := firstQueryValue(query, "use_ssl", "use-ssl")

	switch storage {
	case "s3":
		if endpoint == "" {
			return validateOptionalBool("use_ssl", useSSL)
		}
		normalized, err := normalizeStorageEndpoint(endpoint, useSSL)
		if err != nil {
			return err
		}
		setIfMissing(props, "s3.endpoint", normalized)
	case "gcs":
		if endpoint != "" {
			normalized, err := normalizeStorageEndpoint(endpoint, useSSL)
			if err != nil {
				return err
			}
			setIfMissing(props, "gcs.endpoint", normalized)
		} else if err := validateOptionalBool("use_ssl", useSSL); err != nil {
			return err
		}
		jsonKey := firstQueryValue(query, "gcs_json_key", "gcs-json-key")
		keyPath := firstQueryValue(query, "gcs_key_path", "gcs-key-path")
		if jsonKey != "" && keyPath != "" {
			return fmt.Errorf("iceberg uri: gcs_json_key and gcs_key_path are mutually exclusive")
		}
		setIfMissing(props, "gcs.jsonkey", jsonKey)
		setIfMissing(props, "gcs.keypath", keyPath)
		credType := firstQueryValue(query, "gcs_credential_type", "gcs-credential-type")
		if err := validateGCSCredentialType(credType); err != nil {
			return err
		}
		setIfMissing(props, "gcs.credtype", credType)
		useJSONAPI := firstQueryValue(query, "gcs_use_json_api", "gcs-use-json-api")
		if useJSONAPI != "" {
			enabled, err := strconv.ParseBool(useJSONAPI)
			if err != nil {
				return fmt.Errorf("iceberg uri: invalid gcs_use_json_api value %q: %w", useJSONAPI, err)
			}
			if enabled {
				setIfMissing(props, "gcs.usejsonapi", "true")
			}
		}
	case "adls":
		return applyADLSProperties(query, props, endpoint, useSSL)
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

func storageLocation(storage, bucket, path string, trailingSlash bool, query url.Values) (string, error) {
	switch storage {
	case "s3":
		return objectStorageLocation("s3", bucket, path, trailingSlash), nil
	case "gcs":
		return objectStorageLocation("gs", bucket, path, trailingSlash), nil
	case "adls":
		account := firstQueryValue(query, "account_name", "account-name")
		if account == "" {
			return "", fmt.Errorf("iceberg uri: account_name is required for Azure storage shorthand")
		}
		if strings.ContainsAny(account, "/@") {
			return "", fmt.Errorf("iceberg uri: invalid Azure account_name %q", account)
		}
		if strings.ContainsAny(bucket, "/@") {
			return "", fmt.Errorf("iceberg uri: invalid Azure container %q", bucket)
		}
		scheme := strings.ToLower(firstQueryValue(query, "adls_scheme", "adls-scheme"))
		if scheme == "" {
			scheme = "abfss"
		}
		switch scheme {
		case "abfs", "abfss", "wasb", "wasbs":
		default:
			return "", fmt.Errorf("iceberg uri: invalid adls_scheme %q", scheme)
		}
		host := firstQueryValue(query, "azure_host", "azure-host")
		if host == "" {
			if strings.HasPrefix(scheme, "wasb") {
				host = account + ".blob.core.windows.net"
			} else {
				host = account + ".dfs.core.windows.net"
			}
		}
		if strings.Contains(host, "://") || strings.ContainsAny(host, "/@") {
			return "", fmt.Errorf("iceberg uri: invalid azure_host %q", host)
		}
		location := (&url.URL{Scheme: scheme, User: url.User(bucket), Host: host}).String()
		return appendLocationPath(location, path, trailingSlash), nil
	default:
		return "", fmt.Errorf("iceberg uri: unsupported storage %q", storage)
	}
}

func objectStorageLocation(scheme, bucket, path string, trailingSlash bool) string {
	bucket = strings.TrimPrefix(bucket, scheme+"://")
	if scheme == "gs" {
		bucket = strings.TrimPrefix(bucket, "gcs://")
	}
	bucket = strings.Trim(bucket, "/")
	return appendLocationPath(scheme+"://"+bucket, path, trailingSlash)
}

func appendLocationPath(location, path string, trailingSlash bool) string {
	out := strings.TrimRight(location, "/")
	if path != "" {
		out += "/" + strings.Trim(path, "/")
	}
	if trailingSlash && !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

func isObjectStorageLocation(location string) bool {
	for _, prefix := range []string{"s3://", "s3a://", "s3n://", "gs://", "abfs://", "abfss://", "wasb://", "wasbs://"} {
		if strings.HasPrefix(strings.ToLower(location), prefix) {
			return true
		}
	}
	return false
}

func applyADLSProperties(query url.Values, props iceberggo.Properties, endpoint, useSSL string) error {
	account := firstQueryValue(query, "account_name", "account-name")
	accountKey := firstQueryValue(query, "account_key", "account-key")
	sasToken := firstQueryValue(query, "sas_token", "sas-token")
	connectionString := firstQueryValue(query, "connection_string", "connection-string")
	managedIdentity := firstQueryValue(query, "managed_identity", "managed-identity")
	clientID := firstQueryValue(query, "client_id", "client-id")
	managedIdentityEnabled := false
	if managedIdentity != "" {
		var err error
		managedIdentityEnabled, err = strconv.ParseBool(managedIdentity)
		if err != nil {
			return fmt.Errorf("iceberg uri: invalid managed_identity value %q: %w", managedIdentity, err)
		}
	}

	authMethods := 0
	for _, configured := range []bool{accountKey != "", sasToken != "", connectionString != "", managedIdentityEnabled} {
		if configured {
			authMethods++
		}
	}
	if authMethods > 1 {
		return fmt.Errorf("iceberg uri: Azure account_key, sas_token, connection_string, and managed_identity are mutually exclusive")
	}
	if accountKey != "" {
		if account == "" {
			return fmt.Errorf("iceberg uri: account_name is required with account_key")
		}
		setIfMissing(props, "adls.auth.shared-key.account.name", account)
		setIfMissing(props, "adls.auth.shared-key.account.key", accountKey)
	}
	if sasToken != "" {
		if account == "" {
			return fmt.Errorf("iceberg uri: account_name is required with sas_token")
		}
		host := firstQueryValue(query, "azure_host", "azure-host")
		if host == "" {
			scheme := strings.ToLower(firstQueryValue(query, "adls_scheme", "adls-scheme"))
			if strings.HasPrefix(scheme, "wasb") {
				host = account + ".blob.core.windows.net"
			} else {
				host = account + ".dfs.core.windows.net"
			}
		}
		setIfMissing(props, "adls.sas-token."+host, strings.TrimPrefix(sasToken, "?"))
	}
	if connectionString != "" {
		if account == "" {
			return fmt.Errorf("iceberg uri: account_name is required with connection_string")
		}
		setIfMissing(props, "adls.connection-string."+account, connectionString)
	}
	if managedIdentity != "" {
		setIfMissing(props, "adls.auth.managed-identity.enabled", strconv.FormatBool(managedIdentityEnabled))
		if clientID != "" && !managedIdentityEnabled {
			return fmt.Errorf("iceberg uri: client_id requires managed_identity=true")
		}
	} else if clientID != "" {
		return fmt.Errorf("iceberg uri: client_id requires managed_identity=true")
	}
	setIfMissing(props, "adls.client-id", clientID)

	if endpoint != "" {
		domain, protocol, err := normalizeADLSEndpoint(endpoint, useSSL)
		if err != nil {
			return err
		}
		setIfMissing(props, "adls.endpoint", domain)
		setIfMissing(props, "adls.protocol", protocol)
	} else if useSSL != "" {
		enabled, err := strconv.ParseBool(useSSL)
		if err != nil {
			return fmt.Errorf("iceberg uri: invalid use_ssl value %q: %w", useSSL, err)
		}
		protocol := "http"
		if enabled {
			protocol = "https"
		}
		setIfMissing(props, "adls.protocol", protocol)
	}
	return nil
}

func normalizeADLSEndpoint(endpoint, useSSL string) (string, string, error) {
	protocol := "https"
	domain := endpoint
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", "", fmt.Errorf("iceberg uri: invalid Azure endpoint %q", endpoint)
		}
		if parsed.Path != "" && parsed.Path != "/" {
			return "", "", fmt.Errorf("iceberg uri: Azure endpoint must not contain a path: %q", endpoint)
		}
		protocol = parsed.Scheme
		domain = parsed.Host
	}
	if useSSL != "" {
		enabled, err := strconv.ParseBool(useSSL)
		if err != nil {
			return "", "", fmt.Errorf("iceberg uri: invalid use_ssl value %q: %w", useSSL, err)
		}
		requested := "http"
		if enabled {
			requested = "https"
		}
		if strings.Contains(endpoint, "://") && requested != protocol {
			return "", "", fmt.Errorf("iceberg uri: endpoint scheme %q conflicts with use_ssl=%s", protocol, useSSL)
		}
		protocol = requested
	}
	return domain, protocol, nil
}

func validateGCSCredentialType(value string) error {
	if value == "" {
		return nil
	}
	switch value {
	case "service_account", "authorized_user", "impersonated_service_account", "external_account":
		return nil
	default:
		return fmt.Errorf("iceberg uri: invalid gcs_credential_type %q", value)
	}
}

func validateOptionalBool(name, value string) error {
	if value == "" {
		return nil
	}
	if _, err := strconv.ParseBool(value); err != nil {
		return fmt.Errorf("iceberg uri: invalid %s value %q: %w", name, value, err)
	}
	return nil
}

func setIfMissing(props iceberggo.Properties, key, value string) {
	if value == "" {
		return
	}
	if _, ok := props[key]; !ok {
		props[key] = value
	}
}

func normalizeStorageEndpoint(endpoint, useSSL string) (string, error) {
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", fmt.Errorf("iceberg uri: invalid storage endpoint %q", endpoint)
		}
		if useSSL != "" {
			enabled, err := strconv.ParseBool(useSSL)
			if err != nil {
				return "", fmt.Errorf("iceberg uri: invalid use_ssl value %q: %w", useSSL, err)
			}
			if enabled != (parsed.Scheme == "https") {
				return "", fmt.Errorf("iceberg uri: endpoint scheme %q conflicts with use_ssl=%s", parsed.Scheme, useSSL)
			}
		}
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
