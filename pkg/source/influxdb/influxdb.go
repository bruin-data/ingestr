package influxdb

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

type InfluxDBSource struct {
	v3Client *influxdb3.Client
	v2Client influxdb2.Client
	hostURL  string
	token    string
	org      string
	bucket   string
	useV3    bool
}

func NewInfluxDBSource() *InfluxDBSource {
	return &InfluxDBSource{}
}

func (s *InfluxDBSource) Schemes() []string {
	return []string{"influxdb"}
}

func (s *InfluxDBSource) HandlesIncrementality() bool {
	return true
}

func (s *InfluxDBSource) Connect(ctx context.Context, uri string) error {
	hostURL, token, org, bucket, secure, useV3, err := parseInfluxURI(uri)
	if err != nil {
		return err
	}

	s.hostURL = hostURL
	s.token = token
	s.org = org
	s.bucket = bucket
	s.useV3 = useV3

	if useV3 {
		client, err := influxdb3.New(influxdb3.ClientConfig{
			Host:         hostURL,
			Token:        token,
			Organization: org,
			Database:     bucket,
		})
		if err != nil {
			return fmt.Errorf("failed to create InfluxDB v3 client: %w", err)
		}
		s.v3Client = client
	} else {
		opts := influxdb2.DefaultOptions()
		if strings.ToLower(secure) == "false" {
			opts.SetTLSConfig(&tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // user explicitly opted out of TLS verification
			})
		}
		s.v2Client = influxdb2.NewClientWithOptions(hostURL, token, opts)

		ok, err := s.v2Client.Ping(ctx)
		if err != nil {
			return fmt.Errorf("failed to ping InfluxDB: %w", err)
		}
		if !ok {
			return fmt.Errorf("InfluxDB server at %s is not reachable", hostURL)
		}
	}

	config.Debug("[INFLUXDB] Connected to %s, org=%s, bucket=%s, secure=%s, useV3=%v", hostURL, org, bucket, secure, useV3)
	return nil
}

func parseInfluxURI(uri string) (hostURL, token, org, bucket, secure string, useV3 bool, err error) {
	prefix := "influxdb://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", "", "", "", false, fmt.Errorf("invalid influxdb URI: must start with influxdb://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", "", "", false, fmt.Errorf("invalid InfluxDB URI: %w", err)
	}

	params := parsed.Query()

	host := parsed.Hostname()
	if host == "" {
		return "", "", "", "", "", false, fmt.Errorf("host is required in InfluxDB URI")
	}

	secure = params.Get("secure")
	if secure == "" {
		secure = "true"
	}
	scheme := "https"
	if strings.ToLower(secure) == "false" {
		scheme = "http"
	}

	port := parsed.Port()
	if port != "" {
		hostURL = fmt.Sprintf("%s://%s:%s", scheme, host, port)
	} else if scheme == "http" {
		hostURL = fmt.Sprintf("%s://%s:%s", scheme, host, "8086")
	} else {
		hostURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	token = params.Get("token")
	if token == "" {
		return "", "", "", "", "", false, fmt.Errorf("token is required in InfluxDB URI query parameters")
	}

	org = params.Get("org")
	if org == "" {
		return "", "", "", "", "", false, fmt.Errorf("org is required in InfluxDB URI query parameters")
	}

	bucket = params.Get("bucket")
	if bucket == "" {
		return "", "", "", "", "", false, fmt.Errorf("bucket is required in InfluxDB URI query parameters")
	}

	useV3 = strings.ToLower(params.Get("influxdb3")) == "true"

	return hostURL, token, org, bucket, secure, useV3, nil
}

func (s *InfluxDBSource) Close(ctx context.Context) error {
	if s.v3Client != nil {
		return s.v3Client.Close()
	}
	if s.v2Client != nil {
		s.v2Client.Close()
	}
	return nil
}

func (s *InfluxDBSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	measurement := req.Name

	strategy := config.StrategyAppend
	if req.Strategy != "" {
		strategy = req.Strategy
	}

	return &source.DynamicSourceTable{
		TableName:           measurement,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: "time",
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("InfluxDB does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			if s.useV3 {
				return s.readV3(ctx, measurement, opts)
			}
			return s.readFlux(ctx, measurement, opts)
		},
	}, nil
}

// readV3 uses the InfluxDB v3 SQL client — returns pivoted/wide format (fields as columns).
func (s *InfluxDBSource) readV3(ctx context.Context, measurement string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	if opts.IntervalStart == nil {
		return nil, fmt.Errorf("interval_start is required for InfluxDB source")
	}
	startTime := *opts.IntervalStart
	var endTime time.Time
	if opts.IntervalEnd != nil {
		endTime = *opts.IntervalEnd
	} else {
		endTime = time.Now().UTC()
	}

	go func() {
		defer close(results)

		query := buildSQLQuery(measurement, startTime, endTime, opts.Limit)
		config.Debug("[INFLUXDB] Executing SQL query (v3): %s", query)

		iterator, err := s.v3Client.Query(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query InfluxDB: %w", err)}
			return
		}

		batchSize := 10000
		if opts.PageSize > 0 {
			batchSize = opts.PageSize
		}

		var items []map[string]any
		totalSent := 0

		for iterator.Next() {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			row := iterator.Value()
			items = append(items, row)

			if len(items) >= batchSize {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert InfluxDB records to Arrow: %w", err)}
					return
				}
				results <- source.RecordBatchResult{Batch: record}
				totalSent += len(items)
				config.Debug("[INFLUXDB] Sent %d records (total: %d)", len(items), totalSent)
				items = nil
			}
		}

		if err := iterator.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("InfluxDB query error: %w", err)}
			return
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert InfluxDB records to Arrow: %w", err)}
				return
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		config.Debug("[INFLUXDB] Read completed (v3), total records: %d", totalSent)
	}()

	return results, nil
}

// readFlux uses the InfluxDB v2 client with Flux — returns long/unpivoted format
// (one row per field: time, measurement, field, value, plus tag columns).
// Streams records and batches them incrementally instead of loading all into memory.
func (s *InfluxDBSource) readFlux(ctx context.Context, measurement string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	if opts.IntervalStart == nil {
		return nil, fmt.Errorf("interval_start is required for InfluxDB source")
	}
	startTime := *opts.IntervalStart
	var endTime time.Time
	if opts.IntervalEnd != nil {
		endTime = *opts.IntervalEnd
	} else {
		endTime = time.Now().UTC()
	}

	go func() {
		defer close(results)

		query := buildFluxQuery(s.bucket, measurement, startTime, endTime, opts.Limit)
		config.Debug("[INFLUXDB] Executing Flux query: %s", query)

		batchSize := 10000
		if opts.PageSize > 0 {
			batchSize = opts.PageSize
		}

		queryAPI := s.v2Client.QueryAPI(s.org)
		result, err := queryAPI.Query(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query InfluxDB via Flux: %w", err)}
			return
		}
		defer func() { _ = result.Close() }()

		skipKeys := map[string]bool{
			"result": true,
			"table":  true,
			"_start": true,
			"_stop":  true,
		}

		var items []map[string]any
		totalSent := 0

		for result.Next() {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			record := result.Record()
			values := record.Values()

			item := make(map[string]any, len(values))
			for k, v := range values {
				if skipKeys[k] {
					continue
				}
				key := strings.TrimPrefix(k, "_")
				item[key] = v
			}

			if len(item) > 0 {
				items = append(items, item)
			}

			if len(items) >= batchSize {
				rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert InfluxDB records to Arrow: %w", err)}
					return
				}
				results <- source.RecordBatchResult{Batch: rec}
				totalSent += len(items)
				config.Debug("[INFLUXDB] Sent %d records (total: %d)", len(items), totalSent)
				items = nil
			}
		}

		if err := result.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("flux query error: %w", err)}
			return
		}

		if len(items) > 0 {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert InfluxDB records to Arrow: %w", err)}
				return
			}
			results <- source.RecordBatchResult{Batch: rec}
			totalSent += len(items)
		}

		config.Debug("[INFLUXDB] Read completed (flux), total records: %d", totalSent)
	}()

	return results, nil
}

func buildSQLQuery(measurement string, start time.Time, end time.Time, limit int) string {
	escaped := strings.ReplaceAll(measurement, `"`, `\"`)
	query := fmt.Sprintf(`SELECT * FROM "%s" WHERE time >= '%s' AND time <= '%s'`,
		escaped, start.Format(time.RFC3339), end.Format(time.RFC3339))
	query += " ORDER BY time ASC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	return query
}

func buildFluxQuery(bucket, measurement string, start time.Time, end time.Time, limit int) string {
	escapedBucket := strings.ReplaceAll(bucket, `"`, `\"`)
	escapedMeasurement := strings.ReplaceAll(measurement, `"`, `\"`)
	// Flux range stop is exclusive, so add 1 second to match SQL's inclusive time <= behavior
	stop := end.Add(1 * time.Second)
	query := fmt.Sprintf(
		`from(bucket: "%s") |> range(start: %s, stop: %s) |> filter(fn: (r) => r._measurement == "%s")`,
		escapedBucket, start.Format(time.RFC3339), stop.Format(time.RFC3339), escapedMeasurement,
	)
	if limit > 0 {
		query += fmt.Sprintf(" |> limit(n: %d)", limit)
	}
	return query
}

var _ source.Source = (*InfluxDBSource)(nil)
