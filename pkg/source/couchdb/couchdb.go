package couchdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const maxPageSize = 1000

type CouchDBSource struct {
	client *httpclient.Client
}

func NewCouchDBSource() *CouchDBSource { return &CouchDBSource{} }

func (s *CouchDBSource) Schemes() []string { return []string{"couchdb", "couchdb+https"} }

func parseURI(uri string) (*url.URL, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid CouchDB URI")
	}
	if u.Scheme != "couchdb" && u.Scheme != "couchdb+https" {
		return nil, fmt.Errorf("unsupported CouchDB scheme: %s", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("CouchDB host is required")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("CouchDB URI must contain only host and credentials; select the database with source-table")
	}
	if u.Scheme == "couchdb+https" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
		if u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), "5984")
		}
	}
	u.Path = ""
	return u, nil
}

func (s *CouchDBSource) Connect(ctx context.Context, uri string) error {
	u, err := parseURI(uri)
	if err != nil {
		return err
	}
	credentials := u.User
	u.User = nil
	opts := []httpclient.Option{httpclient.WithBaseURL(u.String())}
	if credentials != nil {
		password, _ := credentials.Password()
		opts = append(opts, httpclient.WithAuth(httpclient.NewBasicAuth(credentials.Username(), password)))
	}
	s.client = httpclient.New(opts...)
	if err := s.get(ctx, "/_up", nil, &struct{}{}); err != nil {
		_ = s.client.Close()
		s.client = nil
		return fmt.Errorf("failed to connect to CouchDB: %w", err)
	}
	return nil
}

func (s *CouchDBSource) Close(context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *CouchDBSource) HandlesIncrementality() bool { return false }

func (s *CouchDBSource) get(ctx context.Context, path string, params url.Values, result interface{}) error {
	resp, err := s.client.R(ctx).SetQueryParamValues(params).Get(path)
	if err != nil {
		return fmt.Errorf("CouchDB GET %s: %w", path, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("CouchDB GET %s: HTTP %d", path, resp.StatusCode())
	}
	decoder := json.NewDecoder(strings.NewReader(resp.String()))
	decoder.UseNumber()
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("decode CouchDB GET %s: %w", path, err)
	}
	return nil
}

func (s *CouchDBSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if s.client == nil {
		return nil, fmt.Errorf("CouchDB source is not connected")
	}
	if req.IncrementalKey != "" {
		return nil, fmt.Errorf("CouchDB does not support incremental keys; use a full replace or merge")
	}
	if req.Name == "" || strings.HasPrefix(req.Name, "_") || req.Name == "." || req.Name == ".." {
		return nil, fmt.Errorf("CouchDB source-table must be a user database name")
	}
	path := "/" + url.PathEscape(req.Name)
	if err := s.get(ctx, path, nil, &struct{}{}); err != nil {
		return nil, err
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}
	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = []string{"_id"}
	}
	return &source.DynamicSourceTable{
		TableName: req.Name, TablePrimaryKeys: pks, TableIncrementalKey: req.IncrementalKey,
		TableStrategy: strategy, KnownSchema: false,
		SchemaFn: func(context.Context) (*schema.TableSchema, error) {
			return &schema.TableSchema{Name: req.Name, Columns: []schema.Column{{Name: "_id", DataType: schema.TypeString}}, PrimaryKeys: pks}, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			if opts.IncrementalKey != "" || opts.IntervalStart != nil || opts.IntervalEnd != nil {
				return nil, fmt.Errorf("CouchDB does not support incremental filtering; use a full replace or merge")
			}
			results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, 2))
			go func() {
				defer close(results)
				if err := s.read(ctx, path, opts, results); err != nil {
					select {
					case results <- source.RecordBatchResult{Err: err}:
					case <-ctx.Done():
					}
				}
			}()
			return results, nil
		},
	}, nil
}

func (s *CouchDBSource) read(ctx context.Context, path string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := maxPageSize
	if opts.PageSize > 0 && opts.PageSize < pageSize {
		pageSize = opts.PageSize
	}
	params := url.Values{"include_docs": {"true"}, "limit": {strconv.Itoa(pageSize + 1)}}
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var page struct {
			Rows []struct {
				ID  string                 `json:"id"`
				Doc map[string]interface{} `json:"doc"`
			} `json:"rows"`
		}
		if err := s.get(ctx, path+"/_all_docs", params, &page); err != nil {
			return err
		}
		more := len(page.Rows) > pageSize
		if more {
			// Resume at the lookahead row, avoiding skip=1 losing a row if the cursor document is deleted.
			key, err := json.Marshal(page.Rows[pageSize].ID)
			if err != nil {
				return err
			}
			params.Set("startkey", string(key))
			page.Rows = page.Rows[:pageSize]
		}
		var batch []map[string]interface{}
		var size int64
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				return err
			}
			select {
			case results <- source.RecordBatchResult{Batch: record}:
			case <-ctx.Done():
				record.Release()
				return ctx.Err()
			}
			batch = nil
			size = 0
			return nil
		}
		for _, row := range page.Rows {
			if row.Doc == nil || strings.HasPrefix(row.ID, "_design/") || row.Doc["_deleted"] == true {
				continue
			}
			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(row.Doc)
				if len(batch) > 0 && size+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return err
					}
				}
				size += rowBytes
			}
			batch = append(batch, row.Doc)
			count++
			if opts.Limit > 0 && count >= opts.Limit {
				return flush()
			}
		}
		if err := flush(); err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}
