package cloudflareradar

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const apiTablePrefix = "api:"

type apiTable struct {
	path   string
	query  url.Values
	config *tableConfig
}

var paginatedAPIEndpoints = map[string]tableConfig{
	"annotations":                      {resultField: "annotations", maxPageSize: 1000},
	"annotations/outages":              {resultField: "annotations", maxPageSize: 1000},
	"bgp/hijacks/events":               {resultField: "events", maxPageSize: 5000, pageNumber: true},
	"bgp/leaks/events":                 {resultField: "events", maxPageSize: 5000, pageNumber: true},
	"bots":                             {resultField: "bots", maxPageSize: 1000},
	"ct/authorities":                   {resultField: "certificateAuthorities", maxPageSize: 1000},
	"ct/logs":                          {resultField: "certificateLogs", maxPageSize: 1000},
	"datasets":                         {resultField: "datasets", maxPageSize: 100},
	"entities/asns":                    {resultField: "asns", maxPageSize: 100},
	"entities/asns/botnet_threat_feed": {resultField: "ases", maxPageSize: 1000},
	"entities/locations":               {resultField: "locations", maxPageSize: 1000},
	"geolocations":                     {resultField: "geolocations", maxPageSize: 1000},
	"origins":                          {resultField: "origins", maxPageSize: 10},
	"tlds":                             {resultField: "tlds", maxPageSize: 2000},
	"traffic_anomalies":                {resultField: "trafficAnomalies", maxPageSize: 1000},
}

func (s *CloudflareRadarSource) getAPITable(req source.TableRequest) (source.SourceTable, error) {
	table, err := parseAPITable(req.Name)
	if err != nil {
		return nil, err
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("schema inference is required for Cloudflare Radar API tables")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readAPI(ctx, req.Name, table, opts)
		},
	}, nil
}

func parseAPITable(name string) (apiTable, error) {
	raw := strings.TrimPrefix(name, apiTablePrefix)
	parsed, err := url.Parse(raw)
	if err != nil {
		return apiTable{}, fmt.Errorf("invalid Cloudflare Radar API table: %w", err)
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimPrefix(path, "radar/")
	if path == "" || parsed.IsAbs() || parsed.Host != "" || strings.Contains(path, `\`) {
		return apiTable{}, fmt.Errorf("invalid Cloudflare Radar API table %q: expected api:<path>", name)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return apiTable{}, fmt.Errorf("invalid Cloudflare Radar API path %q", path)
		}
	}
	query := parsed.Query()
	if format := query.Get("format"); format != "" && format != "json" {
		return apiTable{}, fmt.Errorf("only format=json is supported for Cloudflare Radar API tables")
	}
	query.Set("format", "json")

	table := apiTable{path: path, query: query}
	if cfg, ok := paginatedAPIEndpoints[path]; ok {
		table.config = &cfg
	}
	return table, nil
}

func (s *CloudflareRadarSource) readAPI(ctx context.Context, name string, table apiTable, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		if err := s.fetchAPI(ctx, name, table, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func (s *CloudflareRadarSource) fetchAPI(ctx context.Context, name string, table apiTable, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	query := cloneValues(table.query)
	setAPIDateRange(query, opts)
	limiter := &rowLimiter{limit: opts.Limit}
	if table.config == nil {
		items, err := s.fetchAPIRows(ctx, name, table.path, query, "")
		if err != nil {
			return err
		}
		items, _ = limiter.trim(items)
		return sendAPIItems(name, items, opts, results)
	}

	pageSize := table.config.maxPageSize
	if opts.PageSize > 0 && opts.PageSize < pageSize {
		pageSize = opts.PageSize
	}
	if configuredLimit, err := strconv.Atoi(query.Get("limit")); err == nil && configuredLimit > 0 && configuredLimit < pageSize {
		pageSize = configuredLimit
	}
	if configuredPageSize, err := strconv.Atoi(query.Get("per_page")); err == nil && configuredPageSize > 0 && configuredPageSize < pageSize {
		pageSize = configuredPageSize
	}

	for page := 0; ; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if table.config.pageNumber {
			query.Set("page", strconv.Itoa(page+1))
			query.Set("per_page", strconv.Itoa(pageSize))
		} else {
			query.Set("offset", strconv.Itoa(page*pageSize))
			query.Set("limit", strconv.Itoa(pageSize))
		}

		items, err := s.fetchAPIRows(ctx, name, table.path, query, table.config.resultField)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		fetchedCount := len(items)
		items, reachedLimit := limiter.trim(items)
		if err := sendAPIItems(name, items, opts, results); err != nil {
			return err
		}
		if reachedLimit || fetchedCount < pageSize {
			return nil
		}
	}
}

func (s *CloudflareRadarSource) fetchAPIRows(ctx context.Context, name, path string, query url.Values, resultField string) ([]map[string]interface{}, error) {
	resp, err := s.client.R(ctx).SetQueryParamValues(query).Get("/radar/" + path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Cloudflare Radar %s: %w", name, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("failed to fetch Cloudflare Radar %s: status %d: %s", name, resp.StatusCode(), resp.String())
	}
	if strings.Contains(resp.Header().Get("Content-Type"), "text/csv") {
		items, err := decodeCSVItems(resp.Body())
		if err != nil {
			return nil, fmt.Errorf("failed to decode Cloudflare Radar %s: %w", name, err)
		}
		return items, nil
	}
	if resultField != "" {
		items, err := decodeItems(resp.Body(), resultField)
		if err != nil {
			return nil, fmt.Errorf("failed to decode Cloudflare Radar %s: %w", name, err)
		}
		return items, nil
	}
	items, err := decodeAPIItems(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to decode Cloudflare Radar %s: %w", name, err)
	}
	return items, nil
}

func sendAPIItems(name string, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert Cloudflare Radar %s to Arrow: %w", name, err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, value := range values {
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
}

func setAPIDateRange(query url.Values, opts source.ReadOptions) {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return
	}
	params := make(map[string]string, 2)
	setDateRange(params, opts, "")
	for key, value := range params {
		if !query.Has(key) {
			query.Set(key, value)
		}
	}
}

func decodeAPIItems(body []byte) ([]map[string]interface{}, error) {
	var envelope struct {
		Success *bool       `json:"success"`
		Result  interface{} `json:"result"`
		Errors  interface{} `json:"errors"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.Success != nil && !*envelope.Success {
		return nil, fmt.Errorf("API response was not successful: %v", envelope.Errors)
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("response is missing result")
	}
	return normalizeAPIResult(envelope.Result)
}

func decodeCSVItems(body []byte) ([]map[string]interface{}, error) {
	reader := csv.NewReader(bytes.NewReader(body))
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}
	seen := make(map[string]struct{}, len(headers))
	for index, header := range headers {
		header = strings.TrimPrefix(header, "\ufeff")
		if header == "" {
			return nil, fmt.Errorf("CSV column %d has an empty name", index+1)
		}
		if _, ok := seen[header]; ok {
			return nil, fmt.Errorf("CSV has duplicate column %q", header)
		}
		headers[index] = header
		seen[header] = struct{}{}
	}

	items := make([]map[string]interface{}, 0)
	for {
		values, err := reader.Read()
		if err == io.EOF {
			return items, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}
		item := make(map[string]interface{}, len(headers))
		for index, value := range values {
			item[headers[index]] = value
		}
		items = append(items, item)
	}
}

func normalizeAPIResult(result interface{}) ([]map[string]interface{}, error) {
	switch value := result.(type) {
	case []interface{}:
		return rowsFromArray(value, ""), nil
	case map[string]interface{}:
		return rowsFromObject(value), nil
	default:
		return []map[string]interface{}{{"value": value}}, nil
	}
}

func rowsFromObject(result map[string]interface{}) []map[string]interface{} {
	keys := sortedKeys(result)
	seriesKeys := keysWithPrefix(keys, "serie_")
	if len(seriesKeys) > 0 {
		return rowsFromSeries(result, seriesKeys)
	}
	histogramKeys := keysWithPrefix(keys, "histogram_")
	if len(histogramKeys) > 0 {
		return rowsFromHistograms(result, histogramKeys)
	}
	summaryKeys := keysWithPrefix(keys, "summary_")
	if len(summaryKeys) > 0 {
		return rowsFromSummaries(result, summaryKeys)
	}
	topKeys := keysWithPrefix(keys, "top_")
	if len(topKeys) > 0 {
		return rowsFromCollections(result, topKeys)
	}

	collectionKeys := make([]string, 0)
	objectKeys := make([]string, 0)
	for _, key := range keys {
		if isMetadataKey(key) {
			continue
		}
		switch result[key].(type) {
		case []interface{}:
			collectionKeys = append(collectionKeys, key)
		case map[string]interface{}:
			objectKeys = append(objectKeys, key)
		}
	}
	if len(collectionKeys) > 0 {
		return rowsFromCollections(result, collectionKeys)
	}
	if len(objectKeys) == 1 && len(keys)-metadataKeyCount(result) == 1 {
		row := result[objectKeys[0]].(map[string]interface{})
		if meta := result["meta"]; meta != nil {
			row["_meta"] = meta
		}
		return []map[string]interface{}{row}
	}
	return []map[string]interface{}{result}
}

func rowsFromSeries(result map[string]interface{}, seriesKeys []string) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0)
	meta := result["meta"]
	for _, seriesKey := range seriesKeys {
		series, ok := result[seriesKey].(map[string]interface{})
		if !ok {
			continue
		}
		timestamps, _ := series["timestamps"].([]interface{})
		for index, timestamp := range timestamps {
			row := map[string]interface{}{"timestamp": timestamp}
			if len(seriesKeys) > 1 {
				row["_series"] = seriesKey
			}
			for _, key := range sortedKeys(series) {
				if key == "timestamps" {
					continue
				}
				if values, ok := series[key].([]interface{}); ok && index < len(values) {
					row[key] = values[index]
				} else if !ok {
					row[key] = series[key]
				}
			}
			if meta != nil {
				row["_meta"] = meta
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func rowsFromSummaries(result map[string]interface{}, summaryKeys []string) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0)
	for _, summaryKey := range summaryKeys {
		summary, ok := result[summaryKey].(map[string]interface{})
		if !ok {
			continue
		}
		for _, dimension := range sortedKeys(summary) {
			row := map[string]interface{}{"dimension": dimension, "value": summary[dimension]}
			if len(summaryKeys) > 1 {
				row["_series"] = summaryKey
			}
			if meta := result["meta"]; meta != nil {
				row["_meta"] = meta
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func rowsFromHistograms(result map[string]interface{}, histogramKeys []string) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0)
	for _, histogramKey := range histogramKeys {
		histogram, ok := result[histogramKey].(map[string]interface{})
		if !ok {
			continue
		}
		rowCount := 0
		for _, value := range histogram {
			if values, ok := value.([]interface{}); ok && len(values) > rowCount {
				rowCount = len(values)
			}
		}
		for index := 0; index < rowCount; index++ {
			row := make(map[string]interface{}, len(histogram)+2)
			if len(histogramKeys) > 1 {
				row["_series"] = histogramKey
			}
			for _, key := range sortedKeys(histogram) {
				if values, ok := histogram[key].([]interface{}); ok && index < len(values) {
					row[key] = values[index]
				}
			}
			if meta := result["meta"]; meta != nil {
				row["_meta"] = meta
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func rowsFromCollections(result map[string]interface{}, collectionKeys []string) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0)
	for _, collectionKey := range collectionKeys {
		collection, ok := result[collectionKey].([]interface{})
		if !ok {
			continue
		}
		collectionRows := rowsFromArray(collection, collectionKey)
		if len(collectionKeys) == 1 {
			for _, row := range collectionRows {
				delete(row, "_collection")
			}
		}
		if meta := result["meta"]; meta != nil {
			for _, row := range collectionRows {
				row["_meta"] = meta
			}
		}
		rows = append(rows, collectionRows...)
	}
	return rows
}

func rowsFromArray(items []interface{}, collection string) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok {
			row = map[string]interface{}{"value": item}
		}
		if collection != "" {
			row["_collection"] = collection
		}
		rows = append(rows, row)
	}
	return rows
}

func sortedKeys(value map[string]interface{}) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func keysWithPrefix(keys []string, prefix string) []string {
	matched := make([]string, 0)
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			matched = append(matched, key)
		}
	}
	return matched
}

func isMetadataKey(key string) bool {
	return key == "meta" || key == "result_info" || key == "success"
}

func metadataKeyCount(result map[string]interface{}) int {
	count := 0
	for key := range result {
		if isMetadataKey(key) {
			count++
		}
	}
	return count
}
