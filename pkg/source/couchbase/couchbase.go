package couchbase

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/couchbase/gocb/v2"
)

const defaultBatchSize = 10000

type CouchbaseSource struct {
	cluster *gocb.Cluster
	bucket  string
}

func NewCouchbaseSource() *CouchbaseSource {
	return &CouchbaseSource{}
}

func (s *CouchbaseSource) Schemes() []string {
	return []string{"couchbase"}
}

type couchbaseConfig struct {
	connectionString string
	username         string
	password         string
	bucket           string
	useSSL           bool
}

func parseURI(uri string) (*couchbaseConfig, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid URI: %w", err)
	}

	if u.Scheme != "couchbase" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	username := u.User.Username()
	if username == "" {
		return nil, fmt.Errorf("username is required in URI")
	}

	password, ok := u.User.Password()
	if !ok || password == "" {
		return nil, fmt.Errorf("password is required in URI")
	}

	useSSL := u.Query().Get("ssl") == "true"

	scheme := "couchbase"
	if useSSL {
		scheme = "couchbases"
	}
	connectionString := fmt.Sprintf("%s://%s", scheme, u.Host)

	bucket := strings.TrimPrefix(u.Path, "/")

	return &couchbaseConfig{
		connectionString: connectionString,
		username:         username,
		password:         password,
		bucket:           bucket,
		useSSL:           useSSL,
	}, nil
}

func (s *CouchbaseSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Couchbase URI: %w", err)
	}

	opts := gocb.ClusterOptions{
		Authenticator: gocb.PasswordAuthenticator{
			Username: cfg.username,
			Password: cfg.password,
		},
	}

	if cfg.useSSL {
		if err := opts.ApplyProfile(gocb.ClusterConfigProfileWanDevelopment); err != nil {
			return fmt.Errorf("failed to apply WAN profile: %w", err)
		}
	}

	cluster, err := gocb.Connect(cfg.connectionString, opts)
	if err != nil {
		return fmt.Errorf("failed to connect to Couchbase: %w", err)
	}

	deadline := 30 * time.Second
	if d, ok := ctx.Deadline(); ok {
		if remaining := time.Until(d); remaining < deadline {
			deadline = remaining
		}
	}
	if err := cluster.WaitUntilReady(deadline, nil); err != nil {
		_ = cluster.Close(nil)
		return fmt.Errorf("couchbase cluster not ready: %w", err)
	}

	s.cluster = cluster
	s.bucket = cfg.bucket
	config.Debug("[COUCHBASE] Connected to cluster: %s, bucket: %s", cfg.connectionString, cfg.bucket)
	return nil
}

func (s *CouchbaseSource) Close(ctx context.Context) error {
	if s.cluster != nil {
		return s.cluster.Close(nil)
	}
	return nil
}

func (s *CouchbaseSource) HandlesIncrementality() bool {
	return false
}

func (s *CouchbaseSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	bucket, scope, collection, err := parseTableName(req.Name, s.bucket)
	if err != nil {
		return nil, err
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = []string{"id"}
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("couchbase does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, bucket, scope, collection, opts)
		},
	}, nil
}

func escapeIdentifier(s string) (string, error) {
	if strings.ContainsRune(s, '`') {
		return "", fmt.Errorf("identifier %q contains illegal backtick character", s)
	}
	return "`" + s + "`", nil
}

func parseTableName(table, defaultBucket string) (bucket, scope, collection string, err error) {
	parts := strings.Split(table, ".")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2], nil
	case 2:
		if defaultBucket == "" {
			return "", "", "", fmt.Errorf("table format requires 3 parts (bucket.scope.collection) when bucket is not in URI, got: %s", table)
		}
		return defaultBucket, parts[0], parts[1], nil
	default:
		return "", "", "", fmt.Errorf("invalid table format: expected bucket.scope.collection or scope.collection, got: %s", table)
	}
}

func (s *CouchbaseSource) read(ctx context.Context, bucket, scope, collection string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[COUCHBASE] Starting read from %s.%s.%s", bucket, scope, collection)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		sendErr := func(err error) {
			select {
			case results <- source.RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
		}

		query, params, err := buildQuery(bucket, scope, collection, opts)
		if err != nil {
			sendErr(fmt.Errorf("failed to build Couchbase query: %w", err))
			return
		}
		config.Debug("[COUCHBASE] Executing N1QL query: %s", query)

		queryOpts := &gocb.QueryOptions{
			Adhoc:    true,
			Readonly: true,
			Context:  ctx,
		}
		if len(params) > 0 {
			queryOpts.NamedParameters = params
		}

		result, err := s.cluster.Query(query, queryOpts)
		if err != nil {
			sendErr(fmt.Errorf("failed to execute Couchbase N1QL query: %w", err))
			return
		}
		defer func() { _ = result.Close() }()

		batchNum := 0
		totalRows := int64(0)
		items := make([]map[string]interface{}, 0, batchSize)

		for result.Next() {
			select {
			case <-ctx.Done():
				sendErr(ctx.Err())
				return
			default:
			}

			var row map[string]interface{}
			if err := result.Row(&row); err != nil {
				sendErr(fmt.Errorf("failed to decode Couchbase row: %w", err))
				return
			}

			items = append(items, row)

			if len(items) >= batchSize {
				if err := sendBatch(ctx, items, opts, results); err != nil {
					sendErr(err)
					return
				}
				batchNum++
				totalRows += int64(len(items))
				config.Debug("[COUCHBASE] Batch %d: %d rows (total: %d) in %v", batchNum, len(items), totalRows, time.Since(startTotal))
				items = make([]map[string]interface{}, 0, batchSize)
			}

		}

		if err := result.Err(); err != nil {
			sendErr(fmt.Errorf("couchbase query iteration error: %w", err))
			return
		}

		if len(items) > 0 {
			if err := sendBatch(ctx, items, opts, results); err != nil {
				sendErr(err)
				return
			}
			batchNum++
			totalRows += int64(len(items))
		}

		config.Debug("[COUCHBASE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func buildQuery(bucket, scope, collection string, opts source.ReadOptions) (string, map[string]interface{}, error) {
	escapedBucket, err := escapeIdentifier(bucket)
	if err != nil {
		return "", nil, fmt.Errorf("invalid bucket name: %w", err)
	}
	escapedScope, err := escapeIdentifier(scope)
	if err != nil {
		return "", nil, fmt.Errorf("invalid scope name: %w", err)
	}
	escapedCollection, err := escapeIdentifier(collection)
	if err != nil {
		return "", nil, fmt.Errorf("invalid collection name: %w", err)
	}

	fullPath := fmt.Sprintf("%s.%s.%s", escapedBucket, escapedScope, escapedCollection)
	query := fmt.Sprintf("SELECT META(c).id AS id, c.* FROM %s c", fullPath)

	params := make(map[string]interface{})
	hasStart := opts.IntervalStart != nil
	hasEnd := opts.IntervalEnd != nil

	if opts.IncrementalKey != "" && (hasStart || hasEnd) {
		escapedKey, err := escapeIdentifier(opts.IncrementalKey)
		if err != nil {
			return "", nil, fmt.Errorf("invalid incremental key: %w", err)
		}
		var conditions []string
		if hasStart {
			conditions = append(conditions, fmt.Sprintf("c.%s >= $start_value", escapedKey))
			params["start_value"] = opts.IntervalStart.Format(time.RFC3339)
		}
		if hasEnd {
			conditions = append(conditions, fmt.Sprintf("c.%s < $end_value", escapedKey))
			params["end_value"] = opts.IntervalEnd.Format(time.RFC3339)
		}
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	return query, params, nil
}

func sendBatch(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert Couchbase rows to Arrow: %w", err)
	}
	select {
	case results <- source.RecordBatchResult{Batch: record}:
	case <-ctx.Done():
	}
	return nil
}

var _ source.Source = (*CouchbaseSource)(nil)
