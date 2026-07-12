package iceberg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *Destination) CheckConnection(ctx context.Context) (err error) {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}

	suffix, err := connectionCheckSuffix()
	if err != nil {
		return err
	}
	namespace, dropNamespace, err := d.connectionCheckNamespace(ctx, suffix)
	if err != nil {
		return err
	}
	ident := append(append(icebergtable.Identifier(nil), namespace...), "write_read_"+suffix)
	tableName := strings.Join(ident, ".")
	tableSchema := &schema.TableSchema{
		Name: tableName,
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false, IsPrimaryKey: true},
		},
		PrimaryKeys: []string{"id"},
	}
	defer func() {
		d.mu.Lock()
		delete(d.prepared, tableName)
		d.mu.Unlock()
	}()

	var tableFS icebergio.IO
	var tableLocation string
	var writtenPaths []string
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		cleanupErr := d.cleanupConnectionCheck(cleanupCtx, ident, namespace, dropNamespace, tableFS, tableLocation, writtenPaths)
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	if err := validateS3TablesIdentifier(d.cfg, ident, tableSchema); err != nil {
		return fmt.Errorf("validate test table identifier: %w", err)
	}

	if err := d.createConnectionCheckTable(ctx, ident, tableSchema); err != nil {
		return fmt.Errorf("create test table: %w", err)
	}

	d.mu.Lock()
	d.prepared[tableName] = preparedTable{schema: tableSchema}
	d.mu.Unlock()

	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(1)
	values := builder.NewArray()
	builder.Release()
	batch := array.NewRecordBatch(tableSchema.ToArrowSchema(), []arrow.Array{values}, 1)
	values.Release()

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)
	if err := d.WriteParallel(ctx, records, destination.WriteOptions{
		Table:        tableName,
		Schema:       tableSchema,
		PrimaryKeys:  tableSchema.PrimaryKeys,
		Parallelism:  1,
		AtomicCommit: true,
	}); err != nil {
		return fmt.Errorf("write test row: %w", err)
	}

	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return fmt.Errorf("reload test table: %w", err)
	}
	tableFS, err = tbl.FS(ctx)
	if err != nil {
		return fmt.Errorf("load test table file IO: %w", err)
	}
	tableLocation = tbl.Location()
	writtenPaths = append(writtenPaths, tbl.MetadataLocation())
	tasks, err := tbl.Scan().PlanFiles(ctx)
	if err != nil {
		return fmt.Errorf("plan test table read: %w", err)
	}
	for _, task := range tasks {
		writtenPaths = append(writtenPaths, task.File.FilePath())
	}

	read, err := tbl.Scan().ToArrowTable(ctx)
	if err != nil {
		return fmt.Errorf("read test table: %w", err)
	}
	defer read.Release()
	if read.NumRows() != 1 || read.NumCols() != 1 {
		return fmt.Errorf("read test table: got %d rows and %d columns, want 1 row and 1 column", read.NumRows(), read.NumCols())
	}
	chunks := read.Column(0).Data().Chunks()
	if len(chunks) != 1 {
		return fmt.Errorf("read test table: got %d id chunks, want 1", len(chunks))
	}
	ids, ok := chunks[0].(*array.Int64)
	if !ok || ids.Len() != 1 || ids.IsNull(0) || ids.Value(0) != 1 {
		return errors.New("read test table: row value did not round-trip as id=1")
	}
	return nil
}

func (d *Destination) connectionCheckNamespace(ctx context.Context, suffix string) (icebergtable.Identifier, bool, error) {
	if d.cfg.CreateNamespace {
		namespace := icebergcatalog.ToIdentifier("ingestr_connection_check_" + suffix)
		if err := d.catalog.CreateNamespace(ctx, namespace, iceberggo.Properties{}); err != nil {
			return nil, false, fmt.Errorf("create test namespace: %w", err)
		}
		return namespace, true, nil
	}

	if d.cfg.CheckNamespace == "" {
		return nil, false, errors.New("connection check requires check_namespace when create_namespace=false")
	}
	namespace, err := parseIdentifier(d.cfg.CheckNamespace)
	if err != nil {
		return nil, false, fmt.Errorf("invalid configured check_namespace: %w", err)
	}
	if d.cfg.Properties.Get("type", "") == "hive" && len(namespace) != 1 {
		return nil, false, fmt.Errorf("iceberg: Hive catalog requires a single-level namespace, got %s", strings.Join(namespace, "."))
	}
	exists, err := d.catalog.CheckNamespaceExists(ctx, namespace)
	if err != nil && !errors.Is(err, icebergcatalog.ErrNoSuchNamespace) {
		return nil, false, fmt.Errorf("check configured namespace %s: %w", d.cfg.CheckNamespace, err)
	}
	if !exists {
		return nil, false, fmt.Errorf("configured check_namespace %q does not exist", d.cfg.CheckNamespace)
	}
	return namespace, false, nil
}

func (d *Destination) createConnectionCheckTable(ctx context.Context, ident icebergtable.Identifier, tableSchema *schema.TableSchema) error {
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	if err != nil {
		return err
	}
	props, err := lifecyclePropertiesForCreate(d.cfg.TableProperties, destination.ManagedStagingTTL)
	if err != nil {
		return err
	}
	opts := []icebergcatalog.CreateTableOpt{icebergcatalog.WithProperties(props)}
	if d.cfg.TableLocation != "" {
		base := strings.TrimSuffix(renderTableLocation(d.cfg.TableLocation, ident), "/")
		opts = append(opts, icebergcatalog.WithLocation(base+"/_ingestr_connection_check_"+ident[len(ident)-1]))
	}
	if err := d.ensureLocalTableDirs(ident); err != nil {
		return err
	}
	_, err = d.catalog.CreateTable(ctx, ident, iceSchema, opts...)
	return err
}

func (d *Destination) cleanupConnectionCheck(
	ctx context.Context,
	ident icebergtable.Identifier,
	namespace icebergtable.Identifier,
	dropNamespace bool,
	tableFS icebergio.IO,
	tableLocation string,
	writtenPaths []string,
) error {
	var cleanupErr error
	if err := d.DropTable(ctx, strings.Join(ident, ".")); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("purge test table: %w", err))
	}
	if exists, err := d.catalog.CheckTableExists(ctx, ident); err != nil && !isMissingTableOrNamespace(err) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("verify test table purge: %w", err))
	} else if exists {
		cleanupErr = errors.Join(cleanupErr, errors.New("verify test table purge: catalog entry still exists"))
	}
	if tableFS != nil {
		if listable, ok := tableFS.(icebergio.ListableIO); ok && tableLocation != "" {
			remaining := false
			walkErr := listable.WalkDir(tableLocation, func(_ string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() {
					remaining = true
				}
				return nil
			})
			if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("verify test table files were purged: %w", walkErr))
			} else if remaining {
				cleanupErr = errors.Join(cleanupErr, errors.New("verify test table files were purged: files remain under table location"))
			}
		} else {
			for _, path := range writtenPaths {
				file, openErr := tableFS.Open(path)
				if openErr == nil {
					_ = file.Close()
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("verify test table files were purged: %s still exists", path))
				} else if !errors.Is(openErr, fs.ErrNotExist) {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("verify test table file %s: %w", path, openErr))
				}
			}
		}
	}
	if localPath, ok := localFilesystemPath(tableLocation); ok {
		if removeErr := removeEmptyDirectoryTree(localPath); removeErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove empty test table directory: %w", removeErr))
		}
	}
	lockIdent := purgeLockIdentifier(ident)
	if lock, err := d.catalog.LoadTable(ctx, lockIdent); err == nil {
		if lock.Properties()[purgeLockModeKey] != purgeLockModeIdle {
			cleanupErr = errors.Join(cleanupErr, errors.New("remove test table lifecycle lock: lock is still active"))
		} else if err := d.catalog.DropTable(ctx, lockIdent); err != nil && !isMissingTableOrNamespace(err) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove test table lifecycle lock: %w", err))
		}
	} else if !isMissingTableOrNamespace(err) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("inspect test table lifecycle lock: %w", err))
	}
	if dropNamespace {
		if err := d.catalog.DropNamespace(ctx, namespace); err != nil && !isMissingTableOrNamespace(err) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("drop test namespace: %w", err))
		}
	}
	return cleanupErr
}

func removeEmptyDirectoryTree(root string) error {
	var directories []string
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		} else {
			files = append(files, path)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(files) > 0 {
		return fmt.Errorf("files remain after purge: %s", strings.Join(files, ", "))
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Remove(directories[i]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func connectionCheckSuffix() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate connection check identifier: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
