package airtable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL = "https://api.airtable.com/v0"
	// Airtable allows 5 requests/second per base; use 80% = 4.0
	rateLimit      = 4.0
	rateLimitBurst = 5
	maxPageSize    = 100
)

type AirtableSource struct {
	accessToken string
	baseID      string
	client      *gonghttp.Client
}

func NewAirtableSource() *AirtableSource {
	return &AirtableSource{}
}

func (s *AirtableSource) HandlesIncrementality() bool {
	return false
}

func (s *AirtableSource) Schemes() []string {
	return []string{"airtable"}
}

func (s *AirtableSource) Connect(ctx context.Context, uri string) error {
	token, baseID, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.accessToken = token
	s.baseID = baseID

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("Authorization", "Bearer "+s.accessToken),
	)
	config.Debug("[AIRTABLE] Connected successfully")
	return nil
}

func (s *AirtableSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "airtable://") {
		return "", "", fmt.Errorf("invalid airtable URI: must start with airtable://")
	}

	rest := strings.TrimPrefix(uri, "airtable://")
	if rest == "" || rest == "?" {
		return "", "", fmt.Errorf("access_token is required in airtable URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse airtable URI query: %w", err)
	}

	accessToken := values.Get("access_token")
	if accessToken == "" {
		return "", "", fmt.Errorf("access_token is required in airtable URI")
	}

	return accessToken, values.Get("base_id"), nil
}

type tableRef struct {
	baseID    string
	tableName string
}

func parseTableName(name, defaultBaseID string) (tableRef, error) {
	if name == "" {
		return tableRef{}, fmt.Errorf("airtable source table is required")
	}

	if baseID, tableName, ok := strings.Cut(name, "/"); ok {
		if baseID == "" || tableName == "" {
			return tableRef{}, fmt.Errorf("airtable source table must be in format '<base_id>/<table_id_or_name>' or '<table_id_or_name>' with base_id in URI, got: %s", name)
		}
		return tableRef{baseID: baseID, tableName: tableName}, nil
	}

	if defaultBaseID == "" {
		return tableRef{}, fmt.Errorf("airtable base_id is required: provide it as '<base_id>/<table_id_or_name>' in --source-table or as base_id query param in the URI")
	}
	return tableRef{baseID: defaultBaseID, tableName: name}, nil
}

func (s *AirtableSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	ref, err := parseTableName(req.Name, s.baseID)
	if err != nil {
		return nil, err
	}

	primaryKeys := []string{"id"}
	pkField, err := s.fetchPrimaryFieldName(ctx, ref)
	if err != nil {
		config.Debug("[AIRTABLE] Failed to fetch primary field from metadata, falling back to 'id': %v", err)
	} else if pkField != "" {
		primaryKeys = []string{"id", "fields__" + pkField}
		config.Debug("[AIRTABLE] Resolved primary field: %s", pkField)
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("airtable source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, ref, opts)
		},
	}, nil
}

type tableMetaResponse struct {
	Tables []tableMeta `json:"tables"`
}

type tableMeta struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	PrimaryFieldID string      `json:"primaryFieldId"`
	Fields         []fieldMeta `json:"fields"`
}

type fieldMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (s *AirtableSource) fetchPrimaryFieldName(ctx context.Context, ref tableRef) (string, error) {
	endpoint := fmt.Sprintf("/meta/bases/%s/tables", ref.baseID)

	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to fetch base metadata: %w", err)
	}

	if !resp.IsSuccess() {
		return "", fmt.Errorf("metadata API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var meta tableMetaResponse
	if err := json.Unmarshal(resp.Body(), &meta); err != nil {
		return "", fmt.Errorf("failed to parse metadata response: %w", err)
	}

	for _, table := range meta.Tables {
		if table.ID != ref.tableName && table.Name != ref.tableName {
			continue
		}
		for _, field := range table.Fields {
			if field.ID == table.PrimaryFieldID {
				return field.Name, nil
			}
		}
		return "", fmt.Errorf("primaryFieldId %s not found in fields for table %s", table.PrimaryFieldID, ref.tableName)
	}

	return "", fmt.Errorf("table %s not found in base %s metadata", ref.tableName, ref.baseID)
}

func (s *AirtableSource) read(ctx context.Context, ref tableRef, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		err := s.readTable(ctx, ref, opts, results)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *AirtableSource) readTable(ctx context.Context, ref tableRef, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[AIRTABLE] Reading table %s from base %s", ref.tableName, ref.baseID)

	endpoint := fmt.Sprintf("/%s/%s", ref.baseID, url.PathEscape(ref.tableName))
	offset := ""
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("pageSize", fmt.Sprintf("%d", maxPageSize))

		if offset != "" {
			req.SetQueryParam("offset", offset)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch records from table %s: %w", ref.tableName, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("airtable API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
		decoder.UseNumber()

		var body struct {
			Records []json.RawMessage `json:"records"`
			Offset  string            `json:"offset"`
		}
		if err := decoder.Decode(&body); err != nil {
			return fmt.Errorf("failed to parse records response from table %s: %w", ref.tableName, err)
		}

		if len(body.Records) == 0 {
			break
		}

		items, err := flattenRecords(body.Records)
		if err != nil {
			return fmt.Errorf("failed to flatten records from table %s: %w", ref.tableName, err)
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert records to Arrow: %w", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}

		totalSent += len(items)
		config.Debug("[AIRTABLE] Sent %d records (total: %d)", len(items), totalSent)

		if body.Offset == "" {
			break
		}
		offset = body.Offset
	}

	if totalSent == 0 {
		config.Debug("[AIRTABLE] No records found in table %s", ref.tableName)
	}

	return nil
}

func flattenRecords(rawRecords []json.RawMessage) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(rawRecords))

	for _, raw := range rawRecords {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()

		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("failed to decode record: %w", err)
		}

		flat := make(map[string]any)

		if id, ok := record["id"]; ok {
			flat["id"] = id
		}
		if ct, ok := record["createdTime"]; ok {
			flat["createdTime"] = ct
		}

		if fields, ok := record["fields"].(map[string]any); ok {
			for key, value := range fields {
				flat["fields__"+key] = value
			}
		}

		items = append(items, flat)
	}

	return items, nil
}
