package athena

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type AthenaDestination struct {
	client *athena.Client
	cfg    athenaConfig

	// schemas captures the schema each prepared table was created with, keyed by
	// opts.Table. The cross-database swap branch uses it to recreate the target
	// Iceberg table with the right column types when ALTER TABLE RENAME isn't
	// available across Glue databases. Per-key writes are race-safe under parallel
	// PrepareTable calls in multi-table runs.
	schemas   map[string]*schema.TableSchema
	schemasMu sync.Mutex
}

type athenaConfig struct {
	OutputLocation  string
	DataRoot        string
	Region          string
	Workgroup       string
	Profile         string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	DefaultDatabase string
}

func NewAthenaDestination() *AthenaDestination {
	return &AthenaDestination{}
}

func (d *AthenaDestination) recordSchema(table string, sch *schema.TableSchema, pks []string) {
	if sch == nil {
		return
	}
	clone := *sch
	if len(pks) > 0 {
		clone.PrimaryKeys = pks
	}
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	if d.schemas == nil {
		d.schemas = map[string]*schema.TableSchema{}
	}
	d.schemas[table] = &clone
}

func (d *AthenaDestination) lookupSchema(table string) *schema.TableSchema {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	return d.schemas[table]
}

func (d *AthenaDestination) forgetSchema(table string) {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	delete(d.schemas, table)
}

func (d *AthenaDestination) Schemes() []string { return []string{"athena"} }

func (d *AthenaDestination) Connect(ctx context.Context, rawURI string) error {
	cfg, err := parseAthenaConfig(rawURI)
	if err != nil {
		return err
	}

	var loadOpts []func(*awsconfig.LoadOptions) error
	if cfg.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" || cfg.SessionToken != "" {
		if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
			return errors.New("athena uri: both access_key_id and secret_access_key are required when using static credentials")
		}
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("failed to load aws config: %w", err)
	}
	if awsCfg.Region == "" {
		return errors.New("athena uri: region_name is required (or configure a default AWS region via profile/environment)")
	}

	d.client = athena.NewFromConfig(awsCfg)
	d.cfg = cfg
	return nil
}

func (d *AthenaDestination) Close(ctx context.Context) error {
	d.client = nil
	return nil
}

func (d *AthenaDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return errors.New("schema is required")
	}

	d.recordSchema(opts.Table, opts.Schema, opts.PrimaryKeys)

	db, tbl, err := d.parseQualifiedTable(opts.Table)
	if err != nil {
		return err
	}

	if err := d.ensureDatabaseExists(ctx, db); err != nil {
		return err
	}

	if opts.DropFirst {
		if err := d.DropTable(ctx, opts.Table); err != nil {
			return err
		}
	}

	// If table already exists, enforce it is an Iceberg table.
	if !opts.DropFirst {
		if err := d.ensureIcebergTable(ctx, db, tbl); err != nil {
			// If table doesn't exist, we'll create it below.
			if !errors.Is(err, errTableNotFound) {
				return err
			}
		}
	}

	createSQL, err := buildCreateIcebergTableSQL(db, tbl, opts.Schema.Columns, d.tableLocation(db, tbl))
	if err != nil {
		return err
	}
	start := time.Now()
	if err := d.Exec(ctx, createSQL); err != nil {
		return err
	}
	config.Debug("[DEST] CREATE ICEBERG TABLE took %v", time.Since(start))
	return nil
}

func (d *AthenaDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	opts.Parallelism = 1
	return d.WriteParallel(ctx, records, opts)
}

func (d *AthenaDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if d.client == nil {
		return errors.New("athena destination not connected")
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	db, tbl, err := d.parseQualifiedTable(opts.Table)
	if err != nil {
		return err
	}

	// Enforce target is an Iceberg table before writing.
	if err := d.ensureIcebergTable(ctx, db, tbl); err != nil {
		return err
	}

	startTotal := time.Now()
	config.Debug("[DEST] Starting Athena parallel write with %d workers", parallelism)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	insertBatchRows := 1000
	if insertBatchRows < 1 {
		insertBatchRows = 1
	}

	type insertJob struct {
		sql string
	}

	jobs := make(chan insertJob, parallelism*4)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for job := range jobs {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := d.Exec(ctx, job.sql); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
				return
			}
		}
	}

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go worker()
	}

	var totalRows int64
	for result := range records {
		if result.Err != nil {
			cancel()
			close(jobs)
			wg.Wait()
			return result.Err
		}
		rec := result.Batch
		if rec == nil {
			continue
		}
		if rec.NumRows() == 0 {
			rec.Release()
			continue
		}

		rowCount := int(rec.NumRows())
		colCount := int(rec.NumCols())
		colNames := make([]string, colCount)
		for i := 0; i < colCount; i++ {
			colNames[i] = rec.ColumnName(i)
		}

		// Chunk into multi-row INSERT statements.
		for start := 0; start < rowCount; start += insertBatchRows {
			end := start + insertBatchRows
			if end > rowCount {
				end = rowCount
			}

			sql, err := buildInsertSQL(db, tbl, colNames, rec, start, end)
			if err != nil {
				rec.Release()
				cancel()
				close(jobs)
				wg.Wait()
				return err
			}

			select {
			case err := <-errCh:
				rec.Release()
				cancel()
				close(jobs)
				wg.Wait()
				return err
			case jobs <- insertJob{sql: sql}:
			case <-ctx.Done():
				rec.Release()
				close(jobs)
				wg.Wait()
				return ctx.Err()
			}
		}

		totalRows += int64(rowCount)
		rec.Release()
	}

	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}

	config.Debug("[DEST] Total: %d rows dispatched in %v", totalRows, time.Since(startTotal))
	return nil
}

func (d *AthenaDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	stagingDB, stagingName, err := d.parseQualifiedTable(stagingTable)
	if err != nil {
		return err
	}
	targetDB, targetName, err := d.parseQualifiedTable(targetTable)
	if err != nil {
		return err
	}

	if err := d.DropTable(ctx, targetTable); err != nil {
		return err
	}

	if stagingDB == targetDB {
		// ALTER TABLE RENAME goes through Athena's Hive DDL parser → backticks.
		stagingQualified, err := formatQualifiedTableHive(stagingDB, stagingName)
		if err != nil {
			return err
		}
		if err := validateIdent(targetName); err != nil {
			return err
		}
		return d.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingQualified, targetName))
	}

	// Cross-database swap: Athena's ALTER TABLE RENAME TO is same-database only.
	// Recreate the target Iceberg table with the staging's recorded schema (preserves
	// the column types we originally declared), then copy rows. The schema is keyed by
	// the staging table name so parallel multi-table PrepareTable calls don't race.
	sch := d.lookupSchema(stagingTable)
	if sch == nil {
		return fmt.Errorf("cannot swap %s -> %s: no recorded schema for staging table", stagingTable, targetTable)
	}

	// Replace only PrepareTables the staging side, so the target database may
	// not exist yet on first run with the _bruin_staging design.
	if err := d.ensureDatabaseExists(ctx, targetDB); err != nil {
		return fmt.Errorf("failed to ensure target database exists: %w", err)
	}

	createSQL, err := buildCreateIcebergTableSQL(targetDB, targetName, sch.Columns, d.tableLocation(targetDB, targetName))
	if err != nil {
		return err
	}
	if err := d.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to recreate target table: %w", err)
	}

	stagingQualified, err := formatQualifiedTable(stagingDB, stagingName)
	if err != nil {
		return err
	}
	targetQualified, err := formatQualifiedTable(targetDB, targetName)
	if err != nil {
		return err
	}
	quotedCols := make([]string, len(sch.Columns))
	for i, c := range sch.Columns {
		if err := validateIdent(c.Name); err != nil {
			return fmt.Errorf("invalid column name %q: %w", c.Name, err)
		}
		quotedCols[i] = fmt.Sprintf(`"%s"`, c.Name)
	}
	colList := strings.Join(quotedCols, ", ")
	copySQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		targetQualified, colList, colList, stagingQualified)
	if err := d.Exec(ctx, copySQL); err != nil {
		return fmt.Errorf("failed to copy staging rows into target: %w", err)
	}

	if err := d.DropTable(ctx, stagingTable); err != nil {
		return fmt.Errorf("failed to drop staging table after copy: %w", err)
	}
	d.forgetSchema(stagingTable)
	return nil
}

func (d *AthenaDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for athena destination")
}

func (d *AthenaDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return errors.New("delete-insert strategy is not supported for athena destination")
}

func (d *AthenaDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for athena destination")
}

func (d *AthenaDestination) DropTable(ctx context.Context, table string) error {
	db, tbl, err := d.parseQualifiedTable(table)
	if err != nil {
		return err
	}
	// DROP TABLE goes through Athena's Hive DDL parser → backticks.
	qualified, err := formatQualifiedTableHive(db, tbl)
	if err != nil {
		return err
	}
	return d.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", qualified))
}

func (d *AthenaDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if d.client == nil {
		return errors.New("athena destination not connected")
	}
	if len(args) > 0 {
		return errors.New("athena Exec does not support query arguments")
	}

	execID, err := d.startQuery(ctx, sql, d.cfg.DefaultDatabase)
	if err != nil {
		config.LogFailedQuery(sql, err)
		return err
	}
	if err := d.waitForQuery(ctx, execID); err != nil {
		config.LogFailedQuery(sql, err)
		return err
	}
	return nil
}

func (d *AthenaDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, errors.New("athena destination does not support transactions")
}

func (d *AthenaDestination) SupportsReplaceStrategy() bool      { return true }
func (d *AthenaDestination) SupportsAppendStrategy() bool       { return true }
func (d *AthenaDestination) SupportsMergeStrategy() bool        { return false }
func (d *AthenaDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *AthenaDestination) SupportsSCD2Strategy() bool         { return false }
func (d *AthenaDestination) SupportsAtomicSwap() bool           { return false }

func (d *AthenaDestination) GetScheme() string { return "athena" }

func (d *AthenaDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *AthenaDestination) ensureDatabaseExists(ctx context.Context, database string) error {
	if database == "" {
		return nil
	}
	if err := validateIdent(database); err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", database)
	start := time.Now()
	if err := d.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create database %s: %w", database, err)
	}
	config.Debug("[DEST] Ensured database exists: %s (took %v)", database, time.Since(start))
	return nil
}

func (d *AthenaDestination) parseQualifiedTable(table string) (database, tbl string, err error) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	if d.cfg.DefaultDatabase != "" {
		return d.cfg.DefaultDatabase, table, nil
	}
	return "", "", errors.New("athena table must be qualified as <database>.<table> (or provide a default database via athena:///database?...)")
}

func (d *AthenaDestination) tableLocation(database, table string) string {
	root := d.cfg.DataRoot
	if root == "" {
		root = d.cfg.OutputLocation
	}
	root = strings.TrimSuffix(root, "/") + "/"
	return root + "iceberg/" + database + "/" + table + "/"
}

func (d *AthenaDestination) startQuery(ctx context.Context, query, database string) (string, error) {
	input := &athena.StartQueryExecutionInput{
		QueryString: aws.String(query),
		ResultConfiguration: &types.ResultConfiguration{
			OutputLocation: aws.String(d.cfg.OutputLocation),
		},
	}
	if database != "" {
		input.QueryExecutionContext = &types.QueryExecutionContext{Database: aws.String(database)}
	}
	if d.cfg.Workgroup != "" {
		input.WorkGroup = aws.String(d.cfg.Workgroup)
	}

	resp, err := d.client.StartQueryExecution(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to start query execution: %w", err)
	}
	if resp.QueryExecutionId == nil || *resp.QueryExecutionId == "" {
		return "", errors.New("failed to start query execution: empty execution id")
	}
	return *resp.QueryExecutionId, nil
}

func (d *AthenaDestination) waitForQuery(ctx context.Context, executionID string) error {
	delay := 400 * time.Millisecond
	for {
		resp, err := d.client.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{QueryExecutionId: aws.String(executionID)})
		if err != nil {
			return fmt.Errorf("failed to get query execution status: %w", err)
		}
		if resp.QueryExecution == nil || resp.QueryExecution.Status == nil || resp.QueryExecution.Status.State == "" {
			return errors.New("failed to get query execution status: missing state")
		}

		switch resp.QueryExecution.Status.State {
		case types.QueryExecutionStateSucceeded:
			return nil
		case types.QueryExecutionStateFailed, types.QueryExecutionStateCancelled:
			reason := ""
			if resp.QueryExecution.Status.StateChangeReason != nil {
				reason = *resp.QueryExecution.Status.StateChangeReason
			}
			if reason == "" {
				reason = "unknown reason"
			}
			return fmt.Errorf("athena query %s %s: %s", executionID, strings.ToLower(string(resp.QueryExecution.Status.State)), reason)
		default:
			// QUEUED / RUNNING
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			if delay < 5*time.Second {
				delay *= 2
				if delay > 5*time.Second {
					delay = 5 * time.Second
				}
			}
		}
	}
}
