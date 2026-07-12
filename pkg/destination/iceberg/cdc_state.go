package iceberg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
)

const (
	cdcTargetClaimPropertyPrefix = "ingestr.cdc-target-claim."
	cdcTargetClaimLockPrefix     = "ingestr_cdc_claim_"
	cdcTargetClaimLockTargetKey  = "ingestr.cdc-target-claim.target"
	cdcTargetClaimLockOwnerKey   = "ingestr.cdc-target-claim.owner"
	icebergCDCStatePruneBatch    = 500
)

var (
	_ destination.CDCStateReader               = (*Destination)(nil)
	_ destination.CDCStateFenceReader          = (*Destination)(nil)
	_ destination.CDCStateWriter               = (*Destination)(nil)
	_ destination.CDCStatePruner               = (*Destination)(nil)
	_ destination.CDCStatePruneBatchSizer      = (*Destination)(nil)
	_ destination.CDCTargetClaimer             = (*Destination)(nil)
	_ destination.CDCTargetIdentityProvider    = (*Destination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*Destination)(nil)
	_ destination.ManagedCDCStateValidator     = (*Destination)(nil)
	_ destination.ManagedCDCTargetValidator    = (*Destination)(nil)
	_ destination.CDCTruncateCapable           = (*Destination)(nil)
	_ destination.CDCConditionalTruncater      = (*Destination)(nil)
)

func (d *Destination) ValidateManagedCDCState() error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if !supportsAtomicCDCTargetClaims(d.catalog.CatalogType()) {
		return fmt.Errorf(
			"iceberg: managed CDC requires a catalog with atomic table creation; catalog type %q cannot provide permanent cross-process target claims",
			d.catalog.CatalogType(),
		)
	}
	if err := validateIsolatedTableFilePaths(d.cfg.TableProperties); err != nil {
		return fmt.Errorf("iceberg: managed CDC state requires isolated table file paths: %w", err)
	}
	return nil
}

func (d *Destination) ValidateManagedCDCTarget(ctx context.Context, table string) error {
	if err := d.ValidateManagedCDCState(); err != nil {
		return err
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return err
	}
	exists, err := d.tableExists(ctx, ident)
	if err != nil || !exists {
		return err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return fmt.Errorf("iceberg: failed to validate managed CDC target %s: %w", table, err)
	}
	if tbl.Metadata().TableUUID() == uuid.Nil {
		return fmt.Errorf("iceberg: managed CDC target %s has no stable table UUID", table)
	}
	if err := validateIsolatedTableFilePaths(tbl.Properties()); err != nil {
		return fmt.Errorf("iceberg: managed CDC target %s: %w", table, err)
	}
	return nil
}

func (d *Destination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	if d.catalog == nil {
		return "", errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return "", err
	}
	catalogURI, err := icebergCatalogIdentityURI(d.cfg.Properties.Get("uri", ""))
	if err != nil {
		return "", fmt.Errorf("iceberg: catalog URI cannot be safely canonicalized for managed CDC: %w", err)
	}
	warehouse, err := icebergCatalogIdentityURI(d.cfg.Properties.Get("warehouse", ""))
	if err != nil {
		return "", fmt.Errorf("iceberg: warehouse cannot be safely canonicalized for managed CDC: %w", err)
	}
	glueID, glueRegion, glueEndpoint, err := icebergGlueCatalogIdentity(d.catalog.CatalogType(), d.cfg)
	if err != nil {
		return "", err
	}
	components := []string{
		strings.ToLower(strings.TrimSpace(string(d.catalog.CatalogType()))),
		icebergCatalogIdentityName(d.catalog.CatalogType(), d.cfg),
		icebergRESTCatalogIdentityPrefix(d.catalog.CatalogType(), d.cfg.Properties.Get("prefix", "")),
		catalogURI,
		warehouse,
		glueID,
		glueRegion,
		glueEndpoint,
	}
	components = append(components, ident...)
	return destination.CDCTargetKeyDigest(components...), nil
}

func icebergCatalogIdentityURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("unsupported or malformed catalog location")
	}
	if isInMemoryFileCatalogURI(parsed) {
		return "", errors.New("in-memory file catalog location is unsupported for managed CDC")
	}
	if parsed.Opaque != "" {
		if !strings.EqualFold(parsed.Scheme, "file") {
			return "", errors.New("opaque catalog DSN is unsupported; use a URL or keyword-value DSN")
		}
		absolute, err := filepath.Abs(filepath.FromSlash(parsed.Opaque))
		if err != nil {
			return "", errors.New("relative file catalog location cannot be resolved")
		}
		parsed.Opaque = ""
		parsed.Path = filepath.ToSlash(filepath.Clean(absolute))
		parsed.RawPath = ""
	}
	if looksLikeKeywordCatalogDSN(raw) {
		return canonicalKeywordCatalogDSN(raw)
	}
	if parsed.Scheme == "postgres" || parsed.Scheme == "postgresql" {
		for key := range parsed.Query() {
			switch normalizedCatalogIdentityQueryKey(key) {
			case "service", "servicefile":
				return "", errors.New("service-based catalog DSN is unsupported for managed CDC")
			}
		}
	}
	if parsed.Scheme == "" && parsed.Host == "" {
		if strings.HasPrefix(parsed.Path, "/") {
			return canonicalLocalFileIdentity(parsed.Path), nil
		}
		if strings.HasPrefix(parsed.Path, "./") || strings.HasPrefix(parsed.Path, "../") {
			absolute, err := filepath.Abs(filepath.FromSlash(parsed.Path))
			if err != nil {
				return "", errors.New("relative catalog location cannot be resolved")
			}
			return canonicalLocalFileIdentity(absolute), nil
		}
		return "", errors.New("ambiguous catalog location")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.User = nil
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Host != "" {
		hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		port := parsed.Port()
		if parsed.Scheme == "file" && hostname == "localhost" && port == "" {
			parsed.Host = ""
		} else {
			if port == defaultCatalogIdentityPort(parsed.Scheme) {
				port = ""
			}
			if strings.Contains(hostname, ":") {
				hostname = "[" + strings.Trim(hostname, "[]") + "]"
			}
			parsed.Host = hostname
			if port != "" {
				parsed.Host = net.JoinHostPort(strings.Trim(hostname, "[]"), port)
			}
		}
	}
	if parsed.Opaque == "" {
		cleanPath := path.Clean(parsed.Path)
		switch cleanPath {
		case ".":
			cleanPath = ""
		case "/":
			if parsed.Scheme != "file" {
				cleanPath = ""
			}
		}
		parsed.Path = cleanPath
		parsed.RawPath = ""
		if parsed.Scheme == "file" && parsed.Host == "" {
			parsed.OmitHost = false
		}
	}
	query := make(url.Values)
	for key, values := range parsed.Query() {
		if isCatalogIdentityLocationQueryKey(key) {
			normalized := normalizedCatalogIdentityQueryKey(key)
			query[normalized] = append(query[normalized], values...)
		}
	}
	for key, values := range query {
		sort.Strings(values)
		query[key] = slices.Compact(values)
	}
	parsed.RawQuery = query.Encode()
	parsed.ForceQuery = false
	return parsed.String(), nil
}

func isInMemoryFileCatalogURI(parsed *url.URL) bool {
	if parsed == nil || !strings.EqualFold(parsed.Scheme, "file") {
		return false
	}
	if strings.HasPrefix(parsed.Opaque, ":") {
		return true
	}
	for key, values := range parsed.Query() {
		if !strings.EqualFold(key, "mode") {
			continue
		}
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value), "memory") {
				return true
			}
		}
	}
	return false
}

func canonicalLocalFileIdentity(rawPath string) string {
	return (&url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(filepath.Clean(filepath.FromSlash(rawPath))),
		OmitHost: false,
	}).String()
}

func looksLikeKeywordCatalogDSN(raw string) bool {
	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || strings.HasPrefix(strings.ToLower(raw), "file:") {
		return false
	}
	return strings.Contains(raw, "=")
}

func canonicalKeywordCatalogDSN(raw string) (string, error) {
	fields, err := parseKeywordCatalogDSN(raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(fields["service"]) != "" || strings.TrimSpace(fields["servicefile"]) != "" {
		return "", errors.New("service-based keyword-value catalog DSN is unsupported for managed CDC")
	}
	locationFields := make(map[string]string)
	for key, value := range fields {
		if isCatalogIdentityLocationDSNKey(key) {
			normalized := normalizedCatalogIdentityQueryKey(key)
			if existing, ok := locationFields[normalized]; ok && existing != value {
				return "", errors.New("keyword-value catalog DSN has conflicting location fields")
			}
			locationFields[normalized] = value
		}
	}
	fields = locationFields
	if len(fields) == 0 {
		return "", errors.New("keyword-value catalog DSN has no supported location fields")
	}
	if host, ok := fields["host"]; ok {
		hosts := strings.Split(host, ",")
		for i := range hosts {
			hosts[i] = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hosts[i])), ".")
		}
		fields["host"] = strings.Join(hosts, ",")
	}
	if fields["port"] == "5432" {
		delete(fields, "port")
	}
	values := make(url.Values, len(fields))
	for key, value := range fields {
		values.Set(key, value)
	}
	return "keyword-dsn:?" + values.Encode(), nil
}

func parseKeywordCatalogDSN(raw string) (map[string]string, error) {
	fields := make(map[string]string)
	for offset := 0; ; {
		for offset < len(raw) && isCatalogDSNSpace(raw[offset]) {
			offset++
		}
		if offset == len(raw) {
			break
		}
		keyStart := offset
		for offset < len(raw) && !isCatalogDSNSpace(raw[offset]) && raw[offset] != '=' {
			offset++
		}
		key := strings.ToLower(strings.TrimSpace(raw[keyStart:offset]))
		for offset < len(raw) && isCatalogDSNSpace(raw[offset]) {
			offset++
		}
		if key == "" || offset >= len(raw) || raw[offset] != '=' {
			return nil, errors.New("malformed keyword-value catalog DSN")
		}
		offset++
		for offset < len(raw) && isCatalogDSNSpace(raw[offset]) {
			offset++
		}
		value, next, err := parseKeywordCatalogDSNValue(raw, offset)
		if err != nil {
			return nil, err
		}
		fields[key] = value
		offset = next
	}
	if len(fields) == 0 {
		return nil, errors.New("empty keyword-value catalog DSN")
	}
	return fields, nil
}

func parseKeywordCatalogDSNValue(raw string, offset int) (string, int, error) {
	var value strings.Builder
	quoted := offset < len(raw) && raw[offset] == '\''
	if quoted {
		offset++
	}
	for offset < len(raw) {
		character := raw[offset]
		if character == '\\' {
			offset++
			if offset >= len(raw) {
				return "", 0, errors.New("malformed keyword-value catalog DSN escape")
			}
			value.WriteByte(raw[offset])
			offset++
			continue
		}
		if quoted {
			if character == '\'' {
				offset++
				if offset < len(raw) && !isCatalogDSNSpace(raw[offset]) {
					return "", 0, errors.New("malformed quoted keyword-value catalog DSN")
				}
				return value.String(), offset, nil
			}
		} else if isCatalogDSNSpace(character) {
			return value.String(), offset, nil
		}
		value.WriteByte(character)
		offset++
	}
	if quoted {
		return "", 0, errors.New("unterminated quoted keyword-value catalog DSN")
	}
	return value.String(), offset, nil
}

func isCatalogDSNSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func defaultCatalogIdentityPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	case "postgres", "postgresql":
		return "5432"
	case "mysql":
		return "3306"
	default:
		return ""
	}
}

func normalizedCatalogIdentityQueryKey(key string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
}

func isCatalogIdentityLocationQueryKey(key string) bool {
	normalized := normalizedCatalogIdentityQueryKey(key)
	switch normalized {
	case "tenant", "region", "regionname",
		"database", "dbname", "schema", "namespace", "catalog", "warehouse", "prefix", "branch", "ref",
		"project", "projectid", "account", "accountid", "cluster", "instance", "endpoint",
		"host", "hostaddr", "port":
		return true
	default:
		return false
	}
}

func isCatalogIdentityLocationDSNKey(key string) bool {
	normalized := normalizedCatalogIdentityQueryKey(key)
	if isCatalogIdentityLocationQueryKey(normalized) {
		return true
	}
	switch normalized {
	case "service", "targetsessionattrs", "searchpath", "unixsocket", "socket", "protocol":
		return true
	default:
		return false
	}
}

func icebergCatalogIdentityName(catalogType icebergcatalog.Type, cfg icebergConfig) string {
	if catalogType != icebergcatalog.SQL && catalogType != icebergcatalog.DynamoDB {
		return ""
	}
	if !cfg.CatalogNameExplicit {
		if catalogType == icebergcatalog.SQL {
			return "sql"
		}
		return cfg.CatalogName
	}
	return cfg.CatalogName
}

func icebergGlueCatalogIdentity(catalogType icebergcatalog.Type, cfg icebergConfig) (string, string, string, error) {
	if catalogType != icebergcatalog.Glue {
		return "", "", "", nil
	}
	glueID := strings.TrimSpace(cfg.Properties.Get("glue.id", ""))
	if glueID == "" {
		return "", "", "", errors.New("iceberg: managed CDC with Glue requires explicit glue.id; ambient AWS account identity is unsupported")
	}
	glueRegion := strings.ToLower(strings.TrimSpace(cfg.Properties.Get("glue.region", "")))
	if glueRegion == "" {
		return "", "", "", errors.New("iceberg: managed CDC with Glue requires explicit glue.region; ambient AWS region is unsupported")
	}
	rawEndpoint := strings.TrimSpace(cfg.Properties.Get("glue.endpoint", ""))
	if rawEndpoint == "" {
		return "", "", "", errors.New("iceberg: managed CDC with Glue requires explicit glue.endpoint; ambient AWS endpoint resolution is unsupported")
	}
	glueEndpoint, err := icebergCatalogIdentityURI(rawEndpoint)
	if err != nil {
		return "", "", "", fmt.Errorf("iceberg: Glue endpoint cannot be safely canonicalized for managed CDC: %w", err)
	}
	return glueID, glueRegion, glueEndpoint, nil
}

func icebergRESTCatalogIdentityPrefix(catalogType icebergcatalog.Type, raw string) string {
	if catalogType != icebergcatalog.REST {
		return ""
	}
	clean := path.Clean("/" + strings.TrimSpace(raw))
	if clean == "/" || clean == "." {
		return ""
	}
	return strings.TrimPrefix(clean, "/")
}

func (d *Destination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	if d.catalog == nil {
		return "", false, errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return "", false, err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("iceberg: failed to load CDC target %s: %w", table, err)
	}
	incarnation := tbl.Metadata().TableUUID()
	if incarnation == uuid.Nil {
		return "", false, fmt.Errorf("iceberg: CDC target %s has no stable table UUID", table)
	}
	return incarnation.String(), true, nil
}

func (d *Destination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	canonicalTarget, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}
	ident, err := parseIdentifier(claimTable)
	if err != nil {
		return err
	}
	claimTableState, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load CDC target claim table %s: %w", claimTable, err)
	}
	property := cdcTargetClaimPropertyPrefix + destination.CDCTargetKeyDigest(canonicalTarget)
	existing := claimTableState.Properties()[property]
	if existing == "" {
		existing, err = icebergCDCClaimOwner(ctx, claimTableState, canonicalTarget)
		if err != nil {
			return err
		}
	}
	if err := d.ensureCDCTargetClaimLock(ctx, ident, canonicalTarget, ownerID, existing); err != nil {
		return err
	}

	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		tbl, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return fmt.Errorf("iceberg: failed to load CDC target claim table %s: %w", claimTable, err)
		}
		existing := tbl.Properties()[property]
		if existing == "" {
			existing, err = icebergCDCClaimOwner(ctx, tbl, canonicalTarget)
			if err != nil {
				return err
			}
		}
		if existing != "" && existing != ownerID {
			return fmt.Errorf(
				"iceberg: CDC target registry mirror for %q conflicts with its permanent claim lock",
				claim.DestinationTable,
			)
		}
		if tbl.Properties()[property] == ownerID {
			return d.writeCDCClaimRow(ctx, claimTable, canonicalTarget, ownerID)
		}

		txn := tbl.NewTransaction()
		if err := txn.SetProperties(iceberggo.Properties{property: ownerID}); err != nil {
			return fmt.Errorf("iceberg: failed to stage CDC target claim: %w", err)
		}
		if _, err := txn.Commit(ctx); err == nil {
			return d.writeCDCClaimRow(ctx, claimTable, canonicalTarget, ownerID)
		} else if !errors.Is(err, icebergtable.ErrCommitFailed) {
			latest, loadErr := d.catalog.LoadTable(ctx, ident)
			if loadErr == nil && latest.Properties()[property] == ownerID {
				return d.writeCDCClaimRow(ctx, claimTable, canonicalTarget, ownerID)
			}
			return fmt.Errorf("iceberg: failed to commit CDC target claim: %w", errors.Join(err, loadErr))
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("iceberg: failed to claim CDC destination target %q after retries", claim.DestinationTable)
}

func (d *Destination) ensureCDCTargetClaimLock(
	ctx context.Context,
	claimTable icebergtable.Identifier,
	canonicalTarget, ownerID, legacyOwnerID string,
) error {
	if !supportsAtomicCDCTargetClaims(d.catalog.CatalogType()) {
		return fmt.Errorf("iceberg: catalog type %q cannot atomically claim CDC targets across processes", d.catalog.CatalogType())
	}
	lockIdent := icebergCDCTargetClaimLockIdentifier(claimTable, canonicalTarget)
	lockSchema := iceberggo.NewSchema(0, iceberggo.NestedField{
		ID: 1, Name: "claimed", Type: iceberggo.PrimitiveTypes.Bool, Required: true,
	})
	for attempt := range 5 {
		lock, err := d.catalog.LoadTable(ctx, lockIdent)
		if isMissingTableOrNamespace(err) {
			if legacyOwnerID != "" && legacyOwnerID != ownerID {
				return errors.New("iceberg: CDC destination target is already claimed by another connector")
			}
			_, err = d.catalog.CreateTable(ctx, lockIdent, lockSchema, icebergcatalog.WithProperties(iceberggo.Properties{
				cdcTargetClaimLockTargetKey: canonicalTarget,
				cdcTargetClaimLockOwnerKey:  ownerID,
			}))
			if err == nil {
				return nil
			}
			if errors.Is(err, icebergcatalog.ErrTableAlreadyExists) ||
				strings.Contains(strings.ToLower(err.Error()), "already exists") ||
				strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
				continue
			}
			if isRetryableCDCClaimCatalogError(err) {
				if err := waitForCommitRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("iceberg: failed to create durable CDC target claim: %w", err)
		}
		if err != nil {
			if isRetryableCDCClaimCatalogError(err) {
				if err := waitForCommitRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("iceberg: failed to inspect durable CDC target claim: %w", err)
		}
		if lock.Properties()[cdcTargetClaimLockTargetKey] != canonicalTarget {
			return fmt.Errorf("iceberg: durable CDC target claim %s has invalid target metadata", strings.Join(lockIdent, "."))
		}
		if lock.Properties()[cdcTargetClaimLockOwnerKey] != ownerID {
			return fmt.Errorf("iceberg: CDC destination target is already claimed by another connector")
		}
		return nil
	}
	return fmt.Errorf("iceberg: failed to resolve concurrent CDC target claim for %s", canonicalTarget)
}

func isRetryableCDCClaimCatalogError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "serialization failure") ||
		strings.Contains(message, "deadlock")
}

func supportsAtomicCDCTargetClaims(catalogType icebergcatalog.Type) bool {
	switch catalogType {
	case icebergcatalog.REST, icebergcatalog.Hive, icebergcatalog.Glue, icebergcatalog.DynamoDB, icebergcatalog.SQL:
		return true
	default:
		return false
	}
}

func icebergCDCTargetClaimLockIdentifier(claimTable icebergtable.Identifier, canonicalTarget string) icebergtable.Identifier {
	ident := append(icebergtable.Identifier(nil), icebergcatalog.NamespaceFromIdent(claimTable)...)
	return append(ident, cdcTargetClaimLockPrefix+destination.CDCTargetKeyDigest(canonicalTarget)[:24])
}

func icebergCDCClaimOwner(ctx context.Context, tbl *icebergtable.Table, canonicalTarget string) (string, error) {
	rows, err := scanTableRows(ctx, tbl, iceberggo.EqualTo(iceberggo.Reference("destination_table"), canonicalTarget))
	if err != nil {
		return "", fmt.Errorf("iceberg: failed to inspect existing CDC target claims: %w", err)
	}
	if err := requireColumns(rows, []string{"destination_table", "connector_id"}, strings.Join(tbl.Identifier(), ".")); err != nil {
		return "", err
	}
	var owner string
	for _, row := range rows.Rows {
		candidate, ok := rows.Value(row, "connector_id").(string)
		if !ok || candidate == "" {
			return "", errors.New("iceberg: existing CDC target claim has an invalid connector identifier")
		}
		if owner != "" && candidate != owner {
			return "", errors.New("iceberg: CDC target claim table contains conflicting owners")
		}
		owner = candidate
	}
	return owner, nil
}

func (d *Destination) writeCDCClaimRow(ctx context.Context, table, canonicalTarget, ownerID string) error {
	tbl, err := d.loadIcebergTable(ctx, table)
	if err != nil {
		return err
	}
	visibleOwner, err := icebergCDCClaimOwner(ctx, tbl, canonicalTarget)
	if err != nil {
		return err
	}
	if visibleOwner != "" {
		if visibleOwner != ownerID {
			return errors.New("iceberg: CDC target claim table contains a row owned by another connector")
		}
		return nil
	}
	tableSchema, err := d.GetTableSchema(ctx, table)
	if err != nil {
		return err
	}
	if tableSchema == nil {
		return fmt.Errorf("iceberg: CDC target claim table %s does not exist", table)
	}
	arrowSchema := icebergArrowSchema(tableSchema)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer builder.Release()
	values := map[string]any{
		"destination_table": canonicalTarget,
		"connector_id":      ownerID,
		"claimed_at":        time.Now().UTC().UnixMicro(),
	}
	for index, field := range arrowSchema.Fields() {
		value, ok := values[strings.ToLower(field.Name)]
		if !ok {
			return fmt.Errorf("iceberg: unexpected column %q in CDC target claim table", field.Name)
		}
		if err := appendValueAtPath(builder.Field(index), value, field.Name, field.Nullable); err != nil {
			return err
		}
	}
	record := builder.NewRecordBatch()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	return d.WriteParallel(ctx, records, destination.WriteOptions{
		Table:         table,
		Schema:        tableSchema,
		Parallelism:   1,
		StagingTable:  true,
		CommitToken:   "cdc-target-claim:" + destination.CDCTargetKeyDigest(canonicalTarget, ownerID),
		SkipCDCResume: true,
	})
}

func (d *Destination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	batches := make([]arrow.RecordBatch, 0, 1)
	eventIDs := make([]string, 0)
	var inputErr error
	for result := range records {
		if result.Err != nil && inputErr == nil {
			inputErr = result.Err
		}
		if result.Batch == nil {
			continue
		}
		if inputErr != nil {
			result.Batch.Release()
			continue
		}
		ids, err := cdcStateEventIDs(result.Batch)
		if err != nil {
			inputErr = err
			result.Batch.Release()
			continue
		}
		batches = append(batches, result.Batch)
		eventIDs = append(eventIDs, ids...)
	}
	if inputErr != nil {
		for _, batch := range batches {
			batch.Release()
		}
		return inputErr
	}
	if len(eventIDs) == 0 {
		for _, batch := range batches {
			batch.Release()
		}
		return nil
	}
	sort.Strings(eventIDs)
	opts.CommitToken = struct {
		Kind     string
		EventIDs []string
	}{Kind: "iceberg-managed-cdc-state-v1", EventIDs: eventIDs}
	opts.SkipCDCResume = true
	opts.StagingTable = true
	opts.CDCExpectedIncarnation = ""
	forwarded := make(chan source.RecordBatchResult, len(batches))
	for _, batch := range batches {
		forwarded <- source.RecordBatchResult{Batch: batch}
	}
	close(forwarded)
	return d.WriteParallel(ctx, forwarded, opts)
}

func cdcStateEventIDs(batch arrow.RecordBatch) ([]string, error) {
	index := -1
	for i, field := range batch.Schema().Fields() {
		if strings.EqualFold(field.Name, "event_id") {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, errors.New("iceberg: CDC state batch has no event_id column")
	}
	values, ok := batch.Column(index).(*array.String)
	if !ok {
		return nil, fmt.Errorf("iceberg: CDC state event_id column has type %s, want string", batch.Column(index).DataType())
	}
	ids := make([]string, 0, values.Len())
	for row := 0; row < values.Len(); row++ {
		if values.IsNull(row) || values.Value(row) == "" {
			return nil, errors.New("iceberg: CDC state event_id must be non-empty")
		}
		ids = append(ids, values.Value(row))
	}
	return ids, nil
}

func (d *Destination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	tbl, err := d.loadOptionalCDCTable(ctx, table)
	if err != nil || tbl == nil {
		return nil, err
	}
	rows, err := scanTableRows(ctx, tbl, iceberggo.EqualTo(iceberggo.Reference("connector_id"), connectorID))
	if err != nil {
		return nil, err
	}
	required := []string{"event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn", "recorded_at"}
	if err := requireColumns(rows, required, table); err != nil {
		return nil, err
	}
	entries := make([]destination.CDCStateEntry, 0, len(rows.Rows))
	for _, row := range rows.Rows {
		entry, err := icebergCDCStateEntry(rows, row)
		if err != nil {
			return nil, fmt.Errorf("iceberg: invalid CDC state row: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func icebergCDCStateEntry(rows *scannedTable, row []any) (destination.CDCStateEntry, error) {
	stringValue := func(column string) (string, error) {
		value, ok := rows.Value(row, column).(string)
		if !ok {
			return "", fmt.Errorf("column %s has type %T, want string", column, rows.Value(row, column))
		}
		return value, nil
	}
	eventID, err := stringValue("event_id")
	if err != nil {
		return destination.CDCStateEntry{}, err
	}
	sourceTable, err := stringValue("source_table")
	if err != nil {
		return destination.CDCStateEntry{}, err
	}
	destinationTable, err := stringValue("destination_table")
	if err != nil {
		return destination.CDCStateEntry{}, err
	}
	stateKind, err := stringValue("state_kind")
	if err != nil {
		return destination.CDCStateEntry{}, err
	}
	status, err := stringValue("state_status")
	if err != nil {
		return destination.CDCStateEntry{}, err
	}
	position, err := stringValue("_cdc_lsn")
	if err != nil {
		return destination.CDCStateEntry{}, err
	}
	generation, ok := rows.Value(row, "state_generation").(int64)
	if !ok {
		return destination.CDCStateEntry{}, fmt.Errorf("column state_generation has type %T, want int64", rows.Value(row, "state_generation"))
	}
	entry := destination.CDCStateEntry{
		EventID:          eventID,
		SourceTable:      sourceTable,
		DestinationTable: destinationTable,
		StateKind:        stateKind,
		Generation:       generation,
		Status:           status,
		Position:         position,
	}
	recordedAt, ok := rows.Value(row, "recorded_at").(int64)
	if !ok {
		return destination.CDCStateEntry{}, fmt.Errorf("column recorded_at has type %T, want timestamp", rows.Value(row, "recorded_at"))
	}
	entry.RecordedAt = time.UnixMicro(recordedAt).UTC()
	return entry, nil
}

func (d *Destination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	tbl, err := d.loadOptionalCDCTable(ctx, table)
	if err != nil || tbl == nil {
		return destination.CDCStateFence{}, err
	}
	filter := iceberggo.NewAnd(
		iceberggo.EqualTo(iceberggo.Reference("connector_id"), connectorID),
		iceberggo.EqualTo(iceberggo.Reference("state_kind"), "run"),
	)
	rows, err := scanTableRows(ctx, tbl, filter)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	if err := requireColumns(rows, []string{"event_id", "state_generation"}, table); err != nil {
		return destination.CDCStateFence{}, err
	}
	var fence destination.CDCStateFence
	ids := make(map[string]struct{})
	for _, row := range rows.Rows {
		generation, ok := rows.Value(row, "state_generation").(int64)
		if !ok {
			return destination.CDCStateFence{}, errors.New("iceberg: CDC state fence generation is not int64")
		}
		eventID, ok := rows.Value(row, "event_id").(string)
		if !ok {
			return destination.CDCStateFence{}, errors.New("iceberg: CDC state fence event ID is not string")
		}
		if generation > fence.Generation {
			fence.Generation = generation
			clear(ids)
		}
		if generation == fence.Generation {
			ids[eventID] = struct{}{}
		}
	}
	for eventID := range ids {
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	sort.Strings(fence.RunEventIDs)
	return fence, nil
}

func (d *Destination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	for start := 0; start < len(eventIDs); start += icebergCDCStatePruneBatch {
		end := min(start+icebergCDCStatePruneBatch, len(eventIDs))
		if err := d.deleteCDCStateEventBatch(ctx, table, connectorID, slices.Clone(eventIDs[start:end])); err != nil {
			return err
		}
	}
	return nil
}

func (d *Destination) deleteCDCStateEventBatch(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return err
	}
	eventFilter := iceberggo.BooleanExpression(nil)
	for _, eventID := range eventIDs {
		predicate := iceberggo.EqualTo(iceberggo.Reference("event_id"), eventID)
		if eventFilter == nil {
			eventFilter = predicate
		} else {
			eventFilter = iceberggo.NewOr(eventFilter, predicate)
		}
	}
	filter := iceberggo.NewAnd(iceberggo.EqualTo(iceberggo.Reference("connector_id"), connectorID), eventFilter)
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		tbl, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			if isMissingTableOrNamespace(err) {
				return nil
			}
			return err
		}
		if tbl.CurrentSnapshot() == nil {
			return nil
		}
		txn := tbl.NewTransaction()
		if err := txn.Delete(ctx, filter, snapshotProps("cdc-state-prune")); err != nil {
			return fmt.Errorf("iceberg: failed to stage CDC state pruning: %w", err)
		}
		if _, err := txn.Commit(ctx); err == nil {
			d.afterSuccessfulCommit(ctx, table)
			return nil
		} else if !errors.Is(err, icebergtable.ErrCommitFailed) {
			return fmt.Errorf("iceberg: failed to commit CDC state pruning: %w", err)
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("iceberg: failed to prune CDC state from %s after retries", table)
}

func (*Destination) CDCStatePruneBatchSize() int { return icebergCDCStatePruneBatch }

func (d *Destination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.truncateCDCTable(ctx, table, "")
}

func (d *Destination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if strings.TrimSpace(expectedIncarnation) == "" {
		return errors.New("iceberg: conditional CDC truncate requires an expected incarnation")
	}
	return d.truncateCDCTable(ctx, table, expectedIncarnation)
}

func (d *Destination) truncateCDCTable(ctx context.Context, table, expectedIncarnation string) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return err
	}
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		tbl, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return fmt.Errorf("iceberg: failed to load CDC target %s: %w", table, err)
		}
		actual := tbl.Metadata().TableUUID().String()
		if expectedIncarnation != "" && actual != expectedIncarnation {
			return fmt.Errorf("iceberg: refused to truncate CDC target %s because its incarnation changed", table)
		}
		txn := tbl.NewTransaction()
		if err := stageResetCommitTokenLedger(txn, ""); err != nil {
			return err
		}
		props := snapshotProps("cdc-truncate")
		props[snapshotCDCResetKey] = "true"
		if err := stageCDCResumeState(txn, props); err != nil {
			return err
		}
		if tbl.CurrentSnapshot() != nil {
			if err := txn.Delete(ctx, iceberggo.AlwaysTrue{}, props); err != nil {
				return fmt.Errorf("iceberg: failed to stage CDC truncate of %s: %w", table, err)
			}
		}
		if _, err := txn.Commit(ctx); err == nil {
			d.afterSuccessfulCommitExpected(ctx, table, expectedIncarnation)
			return nil
		} else if !errors.Is(err, icebergtable.ErrCommitFailed) {
			return fmt.Errorf("iceberg: failed to commit CDC truncate of %s: %w", table, err)
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("iceberg: failed to truncate CDC target %s after retries", table)
}

func (d *Destination) loadOptionalCDCTable(ctx context.Context, table string) (*icebergtable.Table, error) {
	if d.catalog == nil {
		return nil, errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return nil, err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if errors.Is(err, icebergcatalog.ErrNoSuchTable) || errors.Is(err, icebergcatalog.ErrNoSuchNamespace) {
			return nil, nil
		}
		return nil, fmt.Errorf("iceberg: failed to load CDC state table %s: %w", table, err)
	}
	return tbl, nil
}
