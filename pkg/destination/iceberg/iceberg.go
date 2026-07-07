package iceberg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	_ "github.com/apache/iceberg-go/catalog/glue"
	_ "github.com/apache/iceberg-go/catalog/hadoop"
	_ "github.com/apache/iceberg-go/catalog/hive"
	_ "github.com/apache/iceberg-go/catalog/rest"
	_ "github.com/apache/iceberg-go/catalog/sql"
	_ "github.com/apache/iceberg-go/io/gocloud"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type preparedTable struct {
	schema      *schema.TableSchema
	replace     bool
	partitionBy string
}

type Destination struct {
	cfg     icebergConfig
	catalog icebergcatalog.Catalog

	mu       sync.Mutex
	prepared map[string]preparedTable
}

func NewDestination() *Destination {
	return &Destination{}
}

func (d *Destination) Schemes() []string {
	return []string{"iceberg", "iceberg+rest", "iceberg+glue", "iceberg+hive", "iceberg+hadoop", "iceberg+sql", "iceberg+sqlite", "iceberg+postgres"}
}

func (d *Destination) Connect(ctx context.Context, rawURI string) error {
	cfg, err := parseIcebergConfig(rawURI)
	if err != nil {
		return err
	}

	applyS3EnvFromProperties(cfg.Properties)

	cat, err := icebergcatalog.Load(ctx, cfg.CatalogName, cfg.Properties)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load catalog: %w", err)
	}

	d.cfg = cfg
	d.catalog = cat
	d.prepared = make(map[string]preparedTable)
	config.Debug("[ICEBERG] Connected catalog type=%s name=%s", cat.CatalogType(), cfg.CatalogName)
	return nil
}

// applyS3EnvFromProperties bridges URI-parsed S3 creds into AWS SDK env, because
// iceberg-go's write FileIO reads the default AWS chain, not the catalog's s3.* props.
func applyS3EnvFromProperties(props iceberggo.Properties) {
	setenvIfEmpty("AWS_REGION", props["s3.region"])
	setenvIfEmpty("AWS_ACCESS_KEY_ID", props["s3.access-key-id"])
	setenvIfEmpty("AWS_SECRET_ACCESS_KEY", props["s3.secret-access-key"])
	setenvIfEmpty("AWS_SESSION_TOKEN", props["s3.session-token"])
	setenvIfEmpty("AWS_S3_ENDPOINT", props["s3.endpoint"])
	if props["s3.access-key-id"] != "" && props["s3.secret-access-key"] != "" {
		// Complete static keys supplied; skip the slow EC2 metadata (IMDS) probe.
		// Partial keys must NOT disable IMDS, or instance-role fallback breaks.
		setenvIfEmpty("AWS_EC2_METADATA_DISABLED", "true")
	}
}

func setenvIfEmpty(key, value string) {
	if value == "" || os.Getenv(key) != "" {
		return
	}
	_ = os.Setenv(key, value)
}

func (d *Destination) Close(ctx context.Context) error {
	cat := d.catalog
	d.catalog = nil
	d.prepared = nil
	if closer, ok := cat.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			return fmt.Errorf("iceberg: failed to close catalog: %w", err)
		}
	}
	return nil
}

func (d *Destination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if opts.Schema == nil {
		return errors.New("iceberg destination requires schema")
	}
	tableSchema := tableSchemaWithPrimaryKeys(opts.Schema, opts.PrimaryKeys)

	ident, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	namespace := icebergcatalog.NamespaceFromIdent(ident)
	if err := d.ensureNamespace(ctx, namespace); err != nil {
		return err
	}

	exists, err := d.tableExists(ctx, ident)
	if err != nil {
		return err
	}
	if exists {
		if opts.DropFirst {
			tbl, err := d.catalog.LoadTable(ctx, ident)
			if err != nil {
				return fmt.Errorf("iceberg: failed to load table %s: %w", opts.Table, err)
			}
			if err := validateIdentifierFieldsForEvolution(tbl.Schema(), tableSchema, true); err != nil {
				return err
			}
		}
	} else {
		createOpts := opts
		createOpts.Schema = tableSchema
		if err := d.createTable(ctx, ident, createOpts); err != nil {
			return err
		}
	}

	d.mu.Lock()
	d.prepared[opts.Table] = preparedTable{schema: tableSchema, replace: opts.DropFirst, partitionBy: opts.PartitionBy}
	d.mu.Unlock()
	return nil
}

func (d *Destination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *Destination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load table %s: %w", opts.Table, err)
	}

	prepared := d.lookupPrepared(opts.Table)
	writeSchema := opts.Schema
	if writeSchema == nil {
		writeSchema = prepared.schema
	}
	if writeSchema == nil {
		return errors.New("iceberg destination requires schema for write")
	}

	reader := newRecordBatchReader(ctx, records, icebergArrowSchema(writeSchema))
	defer reader.Release()

	props := iceberggo.Properties{
		"ingestr.destination": "iceberg",
	}
	if prepared.replace {
		props["ingestr.operation"] = "replace"
		_, err = d.overwritePrepared(ctx, tbl, reader, props, prepared)
	} else {
		props["ingestr.operation"] = "append"
		_, err = tbl.Append(ctx, reader, props)
		if err == nil {
			err = reader.Err()
		}
	}
	if err != nil {
		return fmt.Errorf("iceberg: failed to write table %s: %w", opts.Table, err)
	}
	return nil
}

func (d *Destination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return nil
}

func (d *Destination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for iceberg destination")
}

func (d *Destination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for iceberg destination")
}

func (d *Destination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for iceberg destination")
}

func (d *Destination) DropTable(ctx context.Context, table string) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return err
	}
	if err := d.catalog.DropTable(ctx, ident); err != nil && !isMissingTableOrNamespace(err) {
		return fmt.Errorf("iceberg: failed to drop table %s: %w", table, err)
	}
	return nil
}

func (d *Destination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return errors.New("iceberg destination does not support SQL execution")
}

func (d *Destination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, errors.New("iceberg destination does not support SQL transactions")
}

func (d *Destination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if d.catalog == nil {
		return nil, errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return nil, err
	}
	exists, err := d.tableExists(ctx, ident)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("iceberg: failed to load table %s: %w", table, err)
	}
	return tableSchemaFromIceberg(table, tbl.Schema())
}

func (d *Destination) GetScheme() string {
	return "iceberg"
}

func (d *Destination) SupportsReplaceStrategy() bool      { return true }
func (d *Destination) SupportsAppendStrategy() bool       { return true }
func (d *Destination) SupportsMergeStrategy() bool        { return false }
func (d *Destination) SupportsDeleteInsertStrategy() bool { return false }
func (d *Destination) SupportsSCD2Strategy() bool         { return false }
func (d *Destination) SupportsAtomicSwap() bool           { return false }

func (d *Destination) createTable(ctx context.Context, ident icebergtable.Identifier, opts destination.PrepareOptions) error {
	iceSchema, err := icebergSchemaFromTableSchema(opts.Schema)
	if err != nil {
		return err
	}

	createOpts := []icebergcatalog.CreateTableOpt{}
	if len(d.cfg.TableProperties) > 0 {
		createOpts = append(createOpts, icebergcatalog.WithProperties(d.cfg.TableProperties))
	}
	if d.cfg.TableLocation != "" {
		createOpts = append(createOpts, icebergcatalog.WithLocation(renderTableLocation(d.cfg.TableLocation, ident)))
	}
	if opts.PartitionBy != "" {
		spec, err := iceberggo.NewPartitionSpecOpts(
			iceberggo.AddPartitionFieldByName(opts.PartitionBy, opts.PartitionBy, iceberggo.IdentityTransform{}, iceSchema, nil),
		)
		if err != nil {
			return fmt.Errorf("iceberg: invalid partition column %q: %w", opts.PartitionBy, err)
		}
		createOpts = append(createOpts, icebergcatalog.WithPartitionSpec(&spec))
	}

	if _, err := d.catalog.CreateTable(ctx, ident, iceSchema, createOpts...); err != nil {
		if errors.Is(err, icebergcatalog.ErrTableAlreadyExists) {
			return nil
		}
		return fmt.Errorf("iceberg: failed to create table %s: %w", strings.Join(ident, "."), err)
	}
	return nil
}

func (d *Destination) stageTableSchemaUpdate(txn *icebergtable.Transaction, tbl *icebergtable.Table, desired *schema.TableSchema, reset bool) (bool, error) {
	if err := validateIdentifierFieldsForEvolution(tbl.Schema(), desired, reset); err != nil {
		return false, err
	}
	update := txn.UpdateSchema(true, reset, icebergtable.WithNameMapping(tbl.NameMapping()))
	changed := false

	desiredColumns := make(map[string]struct{}, len(desired.Columns))
	for _, col := range desired.Columns {
		desiredColumns[col.Name] = struct{}{}
	}
	if reset {
		for _, field := range tbl.Schema().Fields() {
			if _, ok := desiredColumns[field.Name]; ok {
				continue
			}
			update.DeleteColumn([]string{field.Name})
			changed = true
		}
	}

	for _, col := range desired.Columns {
		targetType, err := icebergTypeForColumn(col)
		if err != nil {
			return false, fmt.Errorf("iceberg: failed to map column %q type: %w", col.Name, err)
		}
		field, ok := tbl.Schema().FindFieldByName(col.Name)
		if !ok {
			update.AddColumn([]string{col.Name}, targetType, "", reset && !col.Nullable, nil)
			changed = true
			continue
		}

		if !field.Type.Equals(targetType) {
			if !reset {
				if _, err := iceberggo.PromoteType(field.Type, targetType); err != nil {
					return false, fmt.Errorf("iceberg: column %q type change from %s to %s is not supported: %w", col.Name, field.Type, targetType, err)
				}
			}
			update.UpdateColumn([]string{col.Name}, icebergtable.ColumnUpdate{
				FieldType: iceberggo.Optional[iceberggo.Type]{Valid: true, Val: targetType},
			})
			changed = true
		}
		if field.Required && col.Nullable {
			update.UpdateColumn([]string{col.Name}, icebergtable.ColumnUpdate{
				Required: iceberggo.Optional[bool]{Valid: true, Val: false},
			})
			changed = true
		}
		if reset && !field.Required && !col.Nullable {
			update.UpdateColumn([]string{col.Name}, icebergtable.ColumnUpdate{
				Required: iceberggo.Optional[bool]{Valid: true, Val: true},
			})
			changed = true
		}
	}
	if !identifierFieldsEqual(tbl.Schema(), desired.PrimaryKeys, reset) {
		paths := make([][]string, 0, len(desired.PrimaryKeys))
		for _, pk := range desired.PrimaryKeys {
			paths = append(paths, []string{pk})
		}
		update.SetIdentifierField(paths)
		changed = true
	}

	if !changed {
		return false, nil
	}
	if err := update.Commit(); err != nil {
		return false, fmt.Errorf("iceberg: failed to update table schema: %w", err)
	}
	return true, nil
}

func (d *Destination) stagePartitionSpecUpdate(txn *icebergtable.Transaction, tbl *icebergtable.Table, partitionBy string) (bool, error) {
	if partitionSpecMatches(tbl, partitionBy) {
		return false, nil
	}
	update := txn.UpdateSpec(true)
	spec := tbl.Metadata().PartitionSpec()
	for _, field := range spec.Fields() {
		update.RemoveField(field.Name)
	}
	if partitionBy != "" {
		update.AddIdentity(partitionBy)
	}
	if err := update.Commit(); err != nil {
		return false, fmt.Errorf("iceberg: failed to update partition spec: %w", err)
	}
	return true, nil
}

func (d *Destination) overwritePrepared(ctx context.Context, tbl *icebergtable.Table, reader *recordBatchReader, props iceberggo.Properties, prepared preparedTable) (*icebergtable.Table, error) {
	txn := tbl.NewTransaction()
	if prepared.schema != nil {
		if _, err := d.stageTableSchemaUpdate(txn, tbl, prepared.schema, true); err != nil {
			return nil, err
		}
	}
	if _, err := d.stagePartitionSpecUpdate(txn, tbl, prepared.partitionBy); err != nil {
		return nil, err
	}
	if err := txn.Overwrite(ctx, reader, props); err != nil {
		return nil, err
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	return txn.Commit(ctx)
}

func (d *Destination) ensureNamespace(ctx context.Context, namespace icebergtable.Identifier) error {
	if len(namespace) == 0 || !d.cfg.CreateNamespace {
		return nil
	}
	for i := 1; i <= len(namespace); i++ {
		current := namespace[:i]
		exists, err := d.catalog.CheckNamespaceExists(ctx, current)
		if err != nil && !errors.Is(err, icebergcatalog.ErrNoSuchNamespace) {
			return fmt.Errorf("iceberg: failed to check namespace %s: %w", strings.Join(current, "."), err)
		}
		if exists {
			continue
		}
		if err := d.catalog.CreateNamespace(ctx, current, iceberggo.Properties{}); err != nil && !errors.Is(err, icebergcatalog.ErrNamespaceAlreadyExists) {
			return fmt.Errorf("iceberg: failed to create namespace %s: %w", strings.Join(current, "."), err)
		}
	}
	return nil
}

func (d *Destination) tableExists(ctx context.Context, ident icebergtable.Identifier) (bool, error) {
	exists, err := d.catalog.CheckTableExists(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return false, nil
		}
		return false, fmt.Errorf("iceberg: failed to check table %s: %w", strings.Join(ident, "."), err)
	}
	return exists, nil
}

func (d *Destination) lookupPrepared(table string) preparedTable {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.prepared[table]
}

func parseIdentifier(table string) (icebergtable.Identifier, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return nil, errors.New("iceberg table identifier is required")
	}
	ident := icebergcatalog.ToIdentifier(table)
	for _, part := range ident {
		if part == "" {
			return nil, fmt.Errorf("iceberg table identifier %q contains an empty component", table)
		}
	}
	return ident, nil
}

func isMissingTableOrNamespace(err error) bool {
	return errors.Is(err, icebergcatalog.ErrNoSuchTable) || errors.Is(err, icebergcatalog.ErrNoSuchNamespace)
}

func renderTableLocation(template string, ident icebergtable.Identifier) string {
	namespaceParts := ident[:len(ident)-1]
	tableName := ident[len(ident)-1]
	replacer := strings.NewReplacer(
		"{namespace}", strings.Join(namespaceParts, "/"),
		"{namespace_dot}", strings.Join(namespaceParts, "."),
		"{table}", tableName,
		"{identifier}", strings.Join(ident, "/"),
		"{identifier_dot}", strings.Join(ident, "."),
	)
	return replacer.Replace(template)
}

func tableSchemaWithPrimaryKeys(s *schema.TableSchema, primaryKeys []string) *schema.TableSchema {
	if len(primaryKeys) == 0 {
		return s
	}
	out := *s
	out.PrimaryKeys = append([]string(nil), primaryKeys...)
	return &out
}

func validateIdentifierFieldsForEvolution(current *iceberggo.Schema, desired *schema.TableSchema, reset bool) error {
	if len(desired.PrimaryKeys) == 0 {
		return nil
	}
	if _, err := icebergSchemaFromTableSchema(desired); err != nil {
		return err
	}
	if reset {
		return nil
	}

	currentFields := make(map[string]iceberggo.NestedField, current.NumFields())
	for _, field := range current.Fields() {
		currentFields[field.Name] = field
	}
	for _, pk := range desired.PrimaryKeys {
		field, ok := currentFields[pk]
		if !ok {
			return fmt.Errorf("primary key %q cannot be set on a new column without replace mode", pk)
		}
		if err := validateIdentifierField(pk, field); err != nil {
			return err
		}
	}
	return nil
}

func identifierFieldsEqual(iceSchema *iceberggo.Schema, primaryKeys []string, allowClear bool) bool {
	if len(primaryKeys) == 0 && !allowClear {
		return true
	}
	if len(iceSchema.IdentifierFieldIDs) != len(primaryKeys) {
		return false
	}
	current := make(map[string]struct{}, len(iceSchema.IdentifierFieldIDs))
	for _, id := range iceSchema.IdentifierFieldIDs {
		if name, ok := iceSchema.FindColumnName(id); ok {
			current[name] = struct{}{}
		}
	}
	for _, pk := range primaryKeys {
		if _, ok := current[pk]; !ok {
			return false
		}
	}
	return true
}

func partitionSpecMatches(tbl *icebergtable.Table, partitionBy string) bool {
	spec := tbl.Metadata().PartitionSpec()
	fields := make([]iceberggo.PartitionField, 0)
	for _, field := range spec.Fields() {
		fields = append(fields, field)
	}
	if partitionBy == "" {
		return len(fields) == 0
	}
	if len(fields) != 1 {
		return false
	}
	sourceField, ok := tbl.Schema().FindFieldByName(partitionBy)
	if !ok {
		return false
	}
	field := fields[0]
	return field.SourceID() == sourceField.ID && field.Transform.Equals(iceberggo.IdentityTransform{})
}
