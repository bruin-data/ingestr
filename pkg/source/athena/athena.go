package athena

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

type AthenaSource struct {
	client *athena.Client
	cfg    athenaConfig
}

type athenaConfig struct {
	OutputLocation  string
	Region          string
	Workgroup       string
	Profile         string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	DefaultDatabase string
}

func NewAthenaSource() *AthenaSource {
	return &AthenaSource{}
}

func (s *AthenaSource) Schemes() []string { return []string{"athena"} }

func (s *AthenaSource) Connect(ctx context.Context, rawURI string) error {
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

	s.client = athena.NewFromConfig(awsCfg)
	s.cfg = cfg
	return nil
}

func (s *AthenaSource) Close(ctx context.Context) error {
	s.client = nil
	return nil
}

func (s *AthenaSource) HandlesIncrementality() bool {
	return false
}

func (s *AthenaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	tableName := req.Name
	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, tableSchema, opts)
		},
	}, nil
}

func (s *AthenaSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	db, tbl, err := s.parseQualifiedTable(table)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = '%s' AND table_name = '%s' ORDER BY ordinal_position",
		escapeSQLString(db),
		escapeSQLString(tbl),
	)

	cols, rows, err := s.queryAll(ctx, q, db)
	if err != nil {
		return nil, err
	}
	if len(cols) < 3 {
		return nil, fmt.Errorf("unexpected schema query result: expected 3 columns, got %d", len(cols))
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	result := &schema.TableSchema{
		Name:   tbl,
		Schema: db,
	}
	for _, r := range rows {
		if len(r) < 3 {
			continue
		}
		name := r[0]
		typ := r[1]
		nullable := strings.EqualFold(r[2], "YES")

		dt, precision, scale, arrayType := MapAthenaToDataType(typ)
		result.Columns = append(result.Columns, schema.Column{
			Name:      name,
			DataType:  dt,
			Nullable:  nullable,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		})
	}

	return result, nil
}

func (s *AthenaSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.client == nil {
		return nil, errors.New("athena source not connected")
	}

	db, tbl, err := s.parseQualifiedTable(table)
	if err != nil {
		return nil, err
	}

	columns := filterColumns(tableSchema.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	out := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(out)
		query := buildSelectQuery(db, tbl, columns, opts)
		if err := s.streamQuery(ctx, query, db, arrowSchema, columns, batchSize, out); err != nil {
			out <- source.RecordBatchResult{Err: err}
		}
	}()

	return out, nil
}

func (s *AthenaSource) parseQualifiedTable(table string) (database, tbl string, err error) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	if s.cfg.DefaultDatabase != "" {
		return s.cfg.DefaultDatabase, table, nil
	}
	return "", "", errors.New("athena table must be qualified as <database>.<table> (or provide a default database via athena:///database?...)")
}

func (s *AthenaSource) startQuery(ctx context.Context, query, database string) (string, error) {
	input := &athena.StartQueryExecutionInput{
		QueryString: aws.String(query),
		ResultConfiguration: &types.ResultConfiguration{
			OutputLocation: aws.String(s.cfg.OutputLocation),
		},
	}
	if database != "" {
		input.QueryExecutionContext = &types.QueryExecutionContext{Database: aws.String(database)}
	}
	if s.cfg.Workgroup != "" {
		input.WorkGroup = aws.String(s.cfg.Workgroup)
	}

	resp, err := s.client.StartQueryExecution(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to start query execution: %w", err)
	}
	if resp.QueryExecutionId == nil || *resp.QueryExecutionId == "" {
		return "", errors.New("failed to start query execution: empty execution id")
	}
	return *resp.QueryExecutionId, nil
}

func (s *AthenaSource) waitForQuery(ctx context.Context, executionID string) error {
	delay := 400 * time.Millisecond
	for {
		resp, err := s.client.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{QueryExecutionId: aws.String(executionID)})
		if err != nil {
			return fmt.Errorf("failed to get query execution status: %w", err)
		}
		if resp.QueryExecution == nil || resp.QueryExecution.Status == nil || resp.QueryExecution.Status.State == "" {
			return errors.New("failed to get query execution status: missing state")
		}

		switch resp.QueryExecution.Status.State {
		case "SUCCEEDED":
			return nil
		case "FAILED", "CANCELLED":
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

func (s *AthenaSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.client == nil {
		return nil, errors.New("athena source not connected")
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	out := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(out)

		config.Debug("[ATHENA] Executing custom query: %s", query)
		database := s.cfg.DefaultDatabase

		execID, err := s.startQuery(ctx, query, database)
		if err != nil {
			out <- source.RecordBatchResult{Err: fmt.Errorf("failed to start custom query: %w", err)}
			return
		}
		if err := s.waitForQuery(ctx, execID); err != nil {
			out <- source.RecordBatchResult{Err: fmt.Errorf("failed to wait for custom query: %w", err)}
			return
		}

		// Fetch column metadata from the completed query (no extra execution)
		metaResp, err := s.client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(execID),
			MaxResults:       aws.Int32(1),
		})
		if err != nil {
			out <- source.RecordBatchResult{Err: fmt.Errorf("failed to get column metadata: %w", err)}
			return
		}

		var columns []schema.Column
		if metaResp.ResultSet != nil && metaResp.ResultSet.ResultSetMetadata != nil {
			for _, c := range metaResp.ResultSet.ResultSetMetadata.ColumnInfo {
				name := ""
				if c.Name != nil {
					name = *c.Name
				}
				athenaType := "string"
				if c.Type != nil {
					athenaType = *c.Type
				}
				dt, precision, scale, arrayType := MapAthenaToDataType(athenaType)
				columns = append(columns, schema.Column{
					Name:      name,
					DataType:  dt,
					Nullable:  true,
					Precision: precision,
					Scale:     scale,
					ArrayType: arrayType,
				})
			}
		}
		arrowSchema := buildArrowSchema(columns)

		if err := s.streamQueryFromExecID(ctx, execID, arrowSchema, columns, batchSize, out); err != nil {
			out <- source.RecordBatchResult{Err: err}
		}
	}()

	return out, nil
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
