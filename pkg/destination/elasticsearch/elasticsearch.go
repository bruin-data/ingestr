package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/gong/internal/arrowutil"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/destination"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	elasticsearch "github.com/elastic/go-elasticsearch/v9"
)

const bulkFlushSize = 1000

type esConfig struct {
	baseURL     string
	username    string
	password    string
	apiKey      string
	verifyCerts bool
}

type ElasticsearchDestination struct {
	client *elasticsearch.Client
	config *esConfig
}

func NewElasticsearchDestination() *ElasticsearchDestination {
	return &ElasticsearchDestination{}
}

func (d *ElasticsearchDestination) Schemes() []string {
	return []string{"elasticsearch"}
}

func (d *ElasticsearchDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	d.config = cfg

	esCfg := elasticsearch.Config{
		Addresses: []string{cfg.baseURL},
	}

	if cfg.apiKey != "" {
		esCfg.APIKey = cfg.apiKey
	} else if cfg.username != "" && cfg.password != "" {
		esCfg.Username = cfg.username
		esCfg.Password = cfg.password
	}

	if !cfg.verifyCerts {
		esCfg.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // user-configured option
			},
		}
	}

	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	res, err := client.Info(client.Info.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		return fmt.Errorf("elasticsearch connection error: %s", res.Status())
	}

	d.client = client
	config.Debug("[ELASTICSEARCH DEST] Connected to %s", cfg.baseURL)
	return nil
}

func (d *ElasticsearchDestination) Close(_ context.Context) error {
	return nil
}

func (d *ElasticsearchDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	indexName := resolveIndexName(opts.Table)

	if opts.DropFirst {
		res, err := d.client.Indices.Exists([]string{indexName}, d.client.Indices.Exists.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("failed to check index existence: %w", err)
		}
		defer func() { _ = res.Body.Close() }()

		if !res.IsError() {
			delRes, err := d.client.Indices.Delete([]string{indexName}, d.client.Indices.Delete.WithContext(ctx))
			if err != nil {
				return fmt.Errorf("failed to delete index: %w", err)
			}
			defer func() { _ = delRes.Body.Close() }()

			if delRes.IsError() {
				return fmt.Errorf("failed to delete index %s: %s", indexName, delRes.Status())
			}
			config.Debug("[ELASTICSEARCH DEST] Deleted index: %s", indexName)
		}
	}

	return nil
}

func (d *ElasticsearchDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTotal := time.Now()
	indexName := resolveIndexName(opts.Table)
	var totalRows int64
	batchNum := 0

	for result := range records {
		batchNum++
		if result.Err != nil {
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		if record.NumRows() == 0 {
			record.Release()
			continue
		}

		startBatch := time.Now()
		rows, err := d.writeBatch(ctx, indexName, record)
		record.Release()
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		config.Debug("[ELASTICSEARCH DEST] Batch %d: %d docs in %v (%.0f docs/sec)", batchNum, rows, time.Since(startBatch), float64(rows)/time.Since(startBatch).Seconds())
	}

	config.Debug("[ELASTICSEARCH DEST] Total: %d docs written in %v (%.0f docs/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *ElasticsearchDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	config.Debug("[ELASTICSEARCH DEST] Starting parallel write with %d workers", parallelism)
	startTotal := time.Now()
	indexName := resolveIndexName(opts.Table)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type writeResult struct {
		batchNum int
		rows     int64
		duration time.Duration
		err      error
	}

	results := make(chan writeResult, parallelism*2)
	var wg sync.WaitGroup
	batchNum := int64(0)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range records {
				if ctx.Err() != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					return
				}

				myBatch := int(atomic.AddInt64(&batchNum, 1))
				if result.Err != nil {
					results <- writeResult{batchNum: myBatch, err: result.Err}
					cancel()
					return
				}

				record := result.Batch
				if record == nil {
					continue
				}

				if record.NumRows() == 0 {
					record.Release()
					continue
				}

				startBatch := time.Now()
				rows, err := d.writeBatch(ctx, indexName, record)
				record.Release()

				results <- writeResult{
					batchNum: myBatch,
					rows:     rows,
					duration: time.Since(startBatch),
					err:      err,
				}

				if err != nil {
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var totalRows int64
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			config.Debug("[ELASTICSEARCH DEST] Worker error on batch %d: %v", res.batchNum, res.err)
			continue
		}
		if res.err == nil {
			totalRows += res.rows
			config.Debug("[ELASTICSEARCH DEST] Batch %d: %d docs in %v (%.0f docs/sec)", res.batchNum, res.rows, res.duration, float64(res.rows)/res.duration.Seconds())
		}
	}

	if firstErr != nil {
		return fmt.Errorf("parallel write failed: %w", firstErr)
	}

	config.Debug("[ELASTICSEARCH DEST] Total: %d docs written in %v (%.0f docs/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *ElasticsearchDestination) writeBatch(ctx context.Context, indexName string, record arrow.RecordBatch) (int64, error) {
	rows := int(record.NumRows())
	cols := int(record.NumCols())
	if rows == 0 {
		return 0, nil
	}

	columns := make([]string, cols)
	for i := 0; i < cols; i++ {
		columns[i] = record.ColumnName(i)
	}

	var buf bytes.Buffer
	var totalIndexed int64

	for row := 0; row < rows; row++ {
		doc := make(map[string]interface{}, cols)
		for col := 0; col < cols; col++ {
			val := arrowToValue(record.Column(col), row)
			doc[columns[col]] = val
		}

		// Build action line: use "id" or "_id" field as document ID if present
		action := map[string]interface{}{"index": map[string]interface{}{"_index": indexName}}
		if id, ok := doc["_id"]; ok {
			action["index"].(map[string]interface{})["_id"] = fmt.Sprintf("%v", id)
			delete(doc, "_id")
		} else if id, ok := doc["id"]; ok {
			action["index"].(map[string]interface{})["_id"] = fmt.Sprintf("%v", id)
		}

		actionLine, err := json.Marshal(action)
		if err != nil {
			return totalIndexed, fmt.Errorf("failed to marshal action: %w", err)
		}

		docLine, err := json.Marshal(doc)
		if err != nil {
			return totalIndexed, fmt.Errorf("failed to marshal document: %w", err)
		}

		buf.Write(actionLine)
		buf.WriteByte('\n')
		buf.Write(docLine)
		buf.WriteByte('\n')

		if (row+1)%bulkFlushSize == 0 || row == rows-1 {
			indexed, err := d.flushBulk(ctx, &buf)
			if err != nil {
				return totalIndexed, err
			}
			totalIndexed += indexed
			buf.Reset()
		}
	}

	return totalIndexed, nil
}

func (d *ElasticsearchDestination) flushBulk(ctx context.Context, buf *bytes.Buffer) (int64, error) {
	if buf.Len() == 0 {
		return 0, nil
	}

	res, err := d.client.Bulk(bytes.NewReader(buf.Bytes()), d.client.Bulk.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("elasticsearch bulk request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		return 0, fmt.Errorf("elasticsearch bulk error: %s", res.Status())
	}

	var bulkRes struct {
		Errors bool `json:"errors"`
		Items  []struct {
			Index struct {
				Status int                      `json:"status"`
				Error  *struct{ Reason string } `json:"error,omitempty"`
			} `json:"index"`
		} `json:"items"`
	}

	if err := json.NewDecoder(res.Body).Decode(&bulkRes); err != nil {
		return 0, fmt.Errorf("failed to parse bulk response: %w", err)
	}

	if bulkRes.Errors {
		var failCount int
		for _, item := range bulkRes.Items {
			if item.Index.Error != nil {
				failCount++
				config.Debug("[ELASTICSEARCH DEST] Bulk item error: %s", item.Index.Error.Reason)
			}
		}
		return int64(len(bulkRes.Items) - failCount), fmt.Errorf("elasticsearch bulk insert: %d of %d documents failed", failCount, len(bulkRes.Items))
	}

	return int64(len(bulkRes.Items)), nil
}

func (d *ElasticsearchDestination) SwapTable(_ context.Context, _, _ string) error {
	return errors.New("elasticsearch destination does not support atomic swap")
}

func (d *ElasticsearchDestination) MergeTable(_ context.Context, _ destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for elasticsearch destination")
}

func (d *ElasticsearchDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for elasticsearch destination")
}

func (d *ElasticsearchDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for elasticsearch destination")
}

func (d *ElasticsearchDestination) DropTable(ctx context.Context, table string) error {
	indexName := resolveIndexName(table)
	res, err := d.client.Indices.Delete([]string{indexName}, d.client.Indices.Delete.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to delete index: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		return fmt.Errorf("failed to delete index %s: %s", indexName, res.Status())
	}
	return nil
}

func (d *ElasticsearchDestination) Exec(_ context.Context, _ string, _ ...interface{}) error {
	return errors.New("exec is not supported for elasticsearch destination")
}

func (d *ElasticsearchDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for elasticsearch destination")
}

func (d *ElasticsearchDestination) SupportsReplaceStrategy() bool      { return true }
func (d *ElasticsearchDestination) SupportsAppendStrategy() bool       { return true }
func (d *ElasticsearchDestination) SupportsMergeStrategy() bool        { return false }
func (d *ElasticsearchDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *ElasticsearchDestination) SupportsSCD2Strategy() bool         { return false }
func (d *ElasticsearchDestination) SupportsAtomicSwap() bool           { return false }
func (d *ElasticsearchDestination) GetScheme() string                  { return "elasticsearch" }

func (d *ElasticsearchDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return nil, nil
}

func parseURI(uri string) (*esConfig, error) {
	if !strings.HasPrefix(uri, "elasticsearch://") {
		return nil, fmt.Errorf("invalid elasticsearch URI: must start with elasticsearch://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse elasticsearch URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("host is required in elasticsearch URI")
	}

	port := parsed.Port()
	if port == "" {
		port = "9200"
	}

	secure := true
	if v := parsed.Query().Get("secure"); v != "" {
		secure = v == "true" || v == "1"
	}

	verifyCerts := true
	if v := parsed.Query().Get("verify_certs"); v != "" {
		verifyCerts = v == "true" || v == "1"
	}

	scheme := "https"
	if !secure {
		scheme = "http"
	}

	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}

	apiKey := parsed.Query().Get("api_key")

	return &esConfig{
		baseURL:     fmt.Sprintf("%s://%s:%s", scheme, host, port),
		username:    username,
		password:    password,
		apiKey:      apiKey,
		verifyCerts: verifyCerts,
	}, nil
}

func resolveIndexName(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return table
}

func arrowToValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			val := arrowutil.Value(arr, idx)
			str, ok := val.(string)
			if !ok || str == "" {
				return val
			}
			var decoded interface{}
			if err := json.Unmarshal([]byte(str), &decoded); err != nil {
				return str
			}
			return decoded
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return int64(a.Value(idx))
	case *array.Int16:
		return int64(a.Value(idx))
	case *array.Int32:
		return int64(a.Value(idx))
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint16:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint32:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint64:
		return convertUint(a.Value(idx))
	case *array.Float32:
		return float64(a.Value(idx))
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return a.Value(idx)
	case *array.LargeBinary:
		return a.Value(idx)
	case *array.Decimal128:
		val := a.Value(idx)
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			return val.ToString(dt.Scale)
		}
		return val.ToString(0)
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		micros := int64(a.Value(idx))
		h := micros / 3600000000
		micros %= 3600000000
		m := micros / 60000000
		micros %= 60000000
		s := micros / 1000000
		micros %= 1000000
		return fmt.Sprintf("%02d:%02d:%02d.%06d", h, m, s, micros)
	case *array.Timestamp:
		return a.Value(idx).ToTime(arrow.Microsecond).Format(time.RFC3339Nano)
	case *array.Struct:
		structType := a.DataType().(*arrow.StructType)
		fields := structType.Fields()
		result := make(map[string]interface{}, len(fields))
		for i, field := range fields {
			result[field.Name] = arrowToValue(a.Field(i), idx)
		}
		return result
	case array.ListLike:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		list := make([]interface{}, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			list = append(list, arrowToValue(values, i))
		}
		return list
	case array.ExtensionArray:
		return arrowutil.Value(a.Storage(), idx)
	default:
		return arrowutil.Value(arr, idx)
	}
}

func convertUint(v uint64) interface{} {
	if v <= math.MaxInt64 {
		return int64(v)
	}
	return fmt.Sprintf("%d", v)
}

var _ destination.Destination = (*ElasticsearchDestination)(nil)
