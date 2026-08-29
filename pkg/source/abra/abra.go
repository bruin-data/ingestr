// Package abra implements an ABRA Flexi source.
package abra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	lastUpdateField  = "lastUpdate"
	externalIDsField = "external-ids"
	defaultPageSize  = 1000
	maxPages         = 5000
	defaultRateLimit = 4.0
	flexiTimeLayout  = "2006-01-02T15:04:05.000Z07:00"
)

type Source struct {
	client           *httpclient.Client
	company          string
	pageSize         int
	includeExpensive bool
}

func NewAbraSource() *Source { return &Source{} }

func (s *Source) Schemes() []string { return []string{"abra", "flexibee"} }

func (s *Source) HandlesIncrementality() bool { return false }

type uriConfig struct {
	baseURL          string
	username         string
	password         string
	company          string
	pageSize         int
	rateLimit        float64
	includeExpensive bool
}

func parseURI(uri string) (uriConfig, error) {
	var cfg uriConfig

	parsed, err := url.Parse(uri)
	if err != nil {
		return cfg, fmt.Errorf("abra: could not parse source URI: %w", err)
	}
	if parsed.Scheme != "abra" && parsed.Scheme != "flexibee" {
		return cfg, fmt.Errorf("abra: source URI must start with abra:// or flexibee://")
	}
	host := parsed.Host
	if host == "" {
		return cfg, fmt.Errorf("abra: the Flexi account host is required, e.g. abra://example.flexibee.eu?…")
	}
	q := parsed.Query()

	transport := q.Get("scheme")
	if transport == "" {
		transport = "https"
	}
	if transport != "https" && transport != "http" {
		return cfg, fmt.Errorf("abra: scheme must be https or http, got %q", transport)
	}
	if transport == "http" {
		hostname := parsed.Hostname()
		if hostname != "localhost" && hostname != "127.0.0.1" && hostname != "::1" {
			return cfg, fmt.Errorf("abra: scheme=http is only allowed for loopback hosts")
		}
	}
	cfg.baseURL = transport + "://" + host + strings.TrimSuffix(parsed.Path, "/")

	cfg.username = q.Get("username")
	if cfg.username == "" {
		return cfg, fmt.Errorf("abra: username is required (the shared Flexi API user)")
	}
	cfg.password = q.Get("password")
	if cfg.password == "" {
		return cfg, fmt.Errorf("abra: password is required")
	}

	cfg.company = q.Get("company")
	if cfg.company == "" {
		return cfg, fmt.Errorf(
			"abra: company is required — it selects WHICH set of books to read " +
				"(e.g. acme_s_r_o_, widgets__s_r_o_). Omitting it cannot be defaulted safely")
	}

	cfg.pageSize = defaultPageSize
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("abra: page_size must be a positive integer, got %q", v)
		}
		cfg.pageSize = n
	}

	cfg.rateLimit = defaultRateLimit
	if v := q.Get("rate_limit"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return cfg, fmt.Errorf("abra: rate_limit must be a positive number, got %q", v)
		}
		cfg.rateLimit = f
	}

	cfg.includeExpensive = true
	if v := q.Get("include_expensive"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("abra: include_expensive must be a boolean, got %q", v)
		}
		cfg.includeExpensive = b
	}

	return cfg, nil
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.company = cfg.company
	s.pageSize = cfg.pageSize
	s.includeExpensive = cfg.includeExpensive

	s.client = httpclient.New(
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(180*time.Second),
		httpclient.WithAuth(httpclient.NewBasicAuth(cfg.username, cfg.password)),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithRateLimiter(cfg.rateLimit, 1),
		httpclient.WithRetry(4, 2*time.Second, 30*time.Second),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

func (s *Source) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	evidence := strings.TrimSpace(req.Name)
	if evidence == "" {
		return nil, fmt.Errorf("abra: table name is required (the evidence path, e.g. faktura-vydana)")
	}

	doc, err := s.fetchProperties(ctx, evidence)
	if err != nil {
		return nil, err
	}
	plan, err := buildPlan(evidence, doc, s.includeExpensive)
	if err != nil {
		return nil, err
	}

	incremental := ""
	if plan.hasLastUpdate {
		incremental = lastUpdateField
	} else {
		config.Debug("[ABRA] evidence %s has no %s column: every run will re-read it in full",
			evidence, lastUpdateField)
	}
	if plan.expensiveCount > 0 {
		config.Debug("[ABRA] evidence %s declares %d expensive propert(ies) (included=%v)",
			evidence, plan.expensiveCount, s.includeExpensive)
	}

	tableSchema := &schema.TableSchema{Name: evidence, Columns: plan.columns}

	return &source.DynamicSourceTable{
		TableName:           evidence,
		TablePrimaryKeys:    []string{sanitizeColumn(plan.primaryKey)},
		TableIncrementalKey: incremental,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, plan, opts)
		},
	}, nil
}

func (s *Source) read(ctx context.Context, plan *tablePlan, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)
	go func() {
		defer close(results)
		if err := s.readPaged(ctx, plan, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func buildFilter(plan *tablePlan, opts source.ReadOptions) string {
	if !plan.hasLastUpdate || opts.IntervalStart == nil {
		return ""
	}
	return fmt.Sprintf("%s gte '%s'", lastUpdateField, opts.IntervalStart.Format(flexiTimeLayout))
}

func (s *Source) readPaged(ctx context.Context, plan *tablePlan, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := s.pageSize
	if opts.PageSize > 0 {
		pageSize = opts.PageSize
	}

	path := "/c/" + url.PathEscape(s.company) + "/" + url.PathEscape(plan.evidence) + ".json"
	filter := buildFilter(plan, opts)

	drift := map[string]struct{}{}
	total := 0
	rowCount := -1

	for page := 0; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if page >= maxPages {
			return fmt.Errorf("abra: evidence %s exceeded the %d-page guard", plan.evidence, maxPages)
		}

		start := page * pageSize

		req := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(pageSize)).
			SetQueryParam("start", strconv.Itoa(start)).
			SetQueryParam("detail", "full").
			SetQueryParam("order", "id@A")
		if page == 0 {
			req = req.SetQueryParam("add-row-count", "true")
		}
		if filter != "" {
			req = req.SetQueryParam("filter", filter)
		}

		resp, err := req.Get(path)
		if err != nil {
			return fmt.Errorf("abra: failed to read %s at offset %d: %w", plan.evidence, start, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("abra: read of %s at offset %d returned HTTP %d: %s",
				plan.evidence, start, resp.StatusCode(), truncate(resp.String(), 400))
		}

		items, count, err := extractRecords([]byte(resp.String()), plan.evidence)
		if err != nil {
			return err
		}
		if page == 0 && count >= 0 {
			rowCount = count
			config.Debug("[ABRA] %s: %d row(s) match%s", plan.evidence, rowCount,
				map[bool]string{true: " the window", false: ""}[filter != ""])
		}

		if len(items) > 0 {
			rows := make([]map[string]interface{}, 0, len(items))
			for _, it := range items {
				rows = append(rows, projectRow(it, plan, drift))
			}
			if err := emit(rows, plan.columns, opts, results); err != nil {
				return fmt.Errorf("abra: failed to convert %s to Arrow: %w", plan.evidence, err)
			}
			total += len(rows)
		}

		config.Debug("[ABRA] %s offset %d: %d row(s) (running total %d)",
			plan.evidence, start, len(items), total)

		if len(items) < pageSize {
			break
		}
	}

	if len(drift) > 0 {
		config.Debug("[ABRA] ⚠️ %s: %d undeclared field(s) dropped: %s",
			plan.evidence, len(drift), strings.Join(sortedKeys(drift), ", "))
	}
	if rowCount > 0 && total == 0 {
		return fmt.Errorf(
			"abra: %s reported %d matching row(s) but the read returned none — "+
				"this is a read bug, not an empty table", plan.evidence, rowCount)
	}
	if rowCount >= 0 && total != rowCount {
		config.Debug("[ABRA] %s: read %d row(s), server reported %d at start of walk",
			plan.evidence, total, rowCount)
	}
	config.Debug("[ABRA] %s complete: %d row(s)", plan.evidence, total)
	return nil
}

func extractRecords(body []byte, evidence string) ([]map[string]interface{}, int, error) {
	var outer struct {
		Winstrom map[string]json.RawMessage `json:"winstrom"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&outer); err != nil {
		return nil, -1, fmt.Errorf("abra: failed to parse response for %s: %w", evidence, err)
	}
	if outer.Winstrom == nil {
		return nil, -1, fmt.Errorf("abra: response for %s was not a winstrom envelope", evidence)
	}

	rowCount := -1
	if rc, ok := outer.Winstrom["@rowCount"]; ok {
		var s string
		if json.Unmarshal(rc, &s) == nil {
			if n, err := strconv.Atoi(s); err == nil {
				rowCount = n
			}
		}
	}

	decode := func(raw json.RawMessage) ([]map[string]interface{}, bool) {
		var items []map[string]interface{}
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(&items); err != nil {
			return nil, false
		}
		return items, true
	}

	if raw, ok := outer.Winstrom[evidence]; ok {
		items, ok := decode(raw)
		if !ok {
			return nil, rowCount, fmt.Errorf("abra: failed to parse %s records", evidence)
		}
		return items, rowCount, nil
	}

	for key, raw := range outer.Winstrom {
		if strings.HasPrefix(key, "@") {
			continue
		}
		if items, ok := decode(raw); ok {
			config.Debug("[ABRA] %s: records arrived under key %q, not the evidence path",
				evidence, key)
			return items, rowCount, nil
		}
	}
	return nil, rowCount, nil
}

func projectRow(item map[string]interface{}, plan *tablePlan, drift map[string]struct{}) map[string]interface{} {
	row := make(map[string]interface{}, len(plan.columns))
	for _, c := range plan.columns {
		row[c.Name] = nil
	}
	for key, val := range item {
		dest, ok := plan.sourceToColumn[key]
		if !ok {
			drift[key] = struct{}{}
			continue
		}
		row[dest] = coerce(val, plan.typeOf[dest])
	}
	return row
}

func coerce(v interface{}, dt schema.DataType) interface{} {
	if v == nil {
		return nil
	}
	switch dt {
	case schema.TypeInt64:
		switch t := v.(type) {
		case json.Number:
			n, err := t.Int64()
			if err != nil {
				return nil
			}
			return n
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
			if err != nil {
				return nil
			}
			return n
		case float64:
			return int64(t)
		default:
			return nil
		}

	case schema.TypeBoolean:
		switch t := v.(type) {
		case bool:
			return t
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(t))
			if err != nil {
				return nil
			}
			return b
		default:
			return nil
		}

	case schema.TypeDate:
		s, ok := v.(string)
		if !ok {
			return nil
		}
		return parseFlexiDate(s)

	case schema.TypeTimestamp:
		s, ok := v.(string)
		if !ok {
			return nil
		}
		return parseFlexiDateTime(s)

	case schema.TypeFloat64:
		switch t := v.(type) {
		case json.Number:
			f, err := t.Float64()
			if err != nil {
				return nil
			}
			return f
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
			if err != nil {
				return nil
			}
			return f
		case float64:
			return t
		default:
			return nil
		}

	case schema.TypeDecimal:
		switch t := v.(type) {
		case json.Number, string:
			return t
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64)
		default:
			return nil
		}

	default:
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			return t.String()
		case bool:
			return strconv.FormatBool(t)
		case map[string]interface{}, []interface{}:
			b, err := json.Marshal(t)
			if err != nil {
				return fmt.Sprintf("%v", t)
			}
			return string(b)
		default:
			return fmt.Sprintf("%v", t)
		}
	}
}

func parseFlexiDate(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t
		}
	}
	return nil
}

func parseFlexiDateTime(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{
		flexiTimeLayout,
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return nil
}

func emit(items []map[string]interface{}, cols []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
