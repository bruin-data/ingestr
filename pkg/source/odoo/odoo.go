// Package odoo implements a source for Odoo ERP over its external API.
package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
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
	defaultPageSize = 1000
	maxPageSize     = 5000
	// Odoo publishes no API rate limit: self-hosted servers have none and Odoo Online / Odoo.sh are bound by
	// worker capacity, so requests are capped at a conservative 10 req/s.
	rateLimit      = 10.0
	rateLimitBurst = 5

	writeDateField     = "write_date"
	odooDateLayout     = "2006-01-02"
	odooDatetimeLayout = "2006-01-02 15:04:05"
)

var tableModels = map[string]string{
	"partners":             "res.partner",
	"partner_categories":   "res.partner.category",
	"users":                "res.users",
	"companies":            "res.company",
	"currencies":           "res.currency",
	"currency_rates":       "res.currency.rate",
	"countries":            "res.country",
	"product_categories":   "product.category",
	"product_templates":    "product.template",
	"products":             "product.product",
	"uoms":                 "uom.uom",
	"pricelists":           "product.pricelist",
	"sale_orders":          "sale.order",
	"sale_order_lines":     "sale.order.line",
	"purchase_orders":      "purchase.order",
	"purchase_order_lines": "purchase.order.line",
	"crm_leads":            "crm.lead",
	"crm_stages":           "crm.stage",
	"crm_teams":            "crm.team",
	"account_moves":        "account.move",
	"account_move_lines":   "account.move.line",
	"account_payments":     "account.payment",
	"accounts":             "account.account",
	"journals":             "account.journal",
	"taxes":                "account.tax",
	"payment_terms":        "account.payment.term",
	"analytic_lines":       "account.analytic.line",
	"stock_pickings":       "stock.picking",
	"stock_moves":          "stock.move",
	"stock_move_lines":     "stock.move.line",
	"stock_quants":         "stock.quant",
	"stock_locations":      "stock.location",
	"warehouses":           "stock.warehouse",
	"projects":             "project.project",
	"project_tasks":        "project.task",
	"project_task_stages":  "project.task.type",
	"employees":            "hr.employee",
	"departments":          "hr.department",
	"jobs":                 "hr.job",
	"activities":           "mail.activity",
	"messages":             "mail.message",
}

// Relational and binary columns that are not fetched: one2many is the inverse of a many2one on the child
// model, and binary fields are base64 attachments (images, files) that would bloat every row.
var skippedFieldTypes = map[string]bool{
	"one2many": true,
	"binary":   true,
}

type OdooSource struct {
	client   *rpcClient
	pageSize int
}

func NewOdooSource() *OdooSource {
	return &OdooSource{}
}

func (s *OdooSource) Schemes() []string { return []string{"odoo"} }

func (s *OdooSource) HandlesIncrementality() bool { return true }

type uriConfig struct {
	baseURL  string
	database string
	username string
	apiKey   string
	api      string
	pageSize int
}

func parseURI(uri string) (uriConfig, error) {
	var cfg uriConfig

	parsed, err := url.Parse(uri)
	if err != nil {
		return cfg, fmt.Errorf("odoo: could not parse source URI: %w", err)
	}
	if parsed.Scheme != "odoo" {
		return cfg, fmt.Errorf("odoo: source URI must start with odoo://")
	}
	if parsed.Host == "" {
		return cfg, fmt.Errorf("odoo: the server host is required, e.g. odoo://mycompany.odoo.com?database=mycompany&username=me@example.com&api_key=…")
	}
	q := parsed.Query()

	transport := q.Get("scheme")
	if transport == "" {
		transport = "https"
	}
	if transport != "https" && transport != "http" {
		return cfg, fmt.Errorf("odoo: scheme must be https or http, got %q", transport)
	}
	cfg.baseURL = transport + "://" + parsed.Host + strings.TrimSuffix(parsed.Path, "/")

	cfg.apiKey = q.Get("api_key")
	if cfg.apiKey == "" {
		cfg.apiKey = q.Get("password")
	}
	if cfg.apiKey == "" {
		return cfg, fmt.Errorf("odoo: api_key is required (Preferences → Account Security → New API Key)")
	}
	cfg.database = q.Get("database")
	cfg.username = q.Get("username")

	cfg.api = q.Get("api")
	if cfg.api == "" {
		cfg.api = apiAuto
	}
	switch cfg.api {
	case apiAuto, apiJSON2:
	case apiJSONRPC:
		if err := requireJSONRPCCredentials(cfg); err != nil {
			return cfg, err
		}
	default:
		return cfg, fmt.Errorf("odoo: api must be one of auto, json2, jsonrpc, got %q", cfg.api)
	}

	cfg.pageSize = defaultPageSize
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("odoo: page_size must be a positive integer, got %q", v)
		}
		if n > maxPageSize {
			return cfg, fmt.Errorf("odoo: page_size may not exceed %d, got %d", maxPageSize, n)
		}
		cfg.pageSize = n
	}

	return cfg, nil
}

func requireJSONRPCCredentials(cfg uriConfig) error {
	if cfg.database == "" || cfg.username == "" {
		return fmt.Errorf("odoo: database and username are required for the JSON-RPC API (Odoo 18 and older)")
	}
	return nil
}

func (s *OdooSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.pageSize = cfg.pageSize

	opts := []httpclient.Option{
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(120 * time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(3, 2*time.Second, 30*time.Second),
		// Every call is a POST, but the source only issues reads, so retrying them is safe.
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithDebug(config.DebugMode),
	}
	if cfg.database != "" {
		opts = append(opts, httpclient.WithHeader("X-Odoo-Database", cfg.database))
	}
	s.client = &rpcClient{
		http:     httpclient.New(opts...),
		api:      cfg.api,
		database: cfg.database,
		apiKey:   cfg.apiKey,
	}

	if s.client.api == apiAuto {
		s.client.api = apiJSONRPC
		if major, ok := s.client.serverMajorVersion(ctx); ok && major >= firstJSON2Version {
			s.client.api = apiJSON2
		}
		config.Debug("[ODOO] auto-selected the %s API", s.client.api)
	}

	if s.client.api == apiJSON2 {
		if _, err := s.client.call(ctx, "res.users", "context_get", map[string]interface{}{}); err != nil {
			return fmt.Errorf("odoo: failed to verify the API key: %w", err)
		}
	} else {
		if err := requireJSONRPCCredentials(cfg); err != nil {
			return err
		}
		if err := s.client.authenticate(ctx, cfg.username); err != nil {
			return err
		}
	}

	config.Debug("[ODOO] connected to %s", cfg.baseURL)
	return nil
}

func (s *OdooSource) Close(ctx context.Context) error {
	if s.client != nil && s.client.http != nil {
		return s.client.http.Close()
	}
	return nil
}

func supportedTables() []string {
	names := make([]string, 0, len(tableModels))
	for name := range tableModels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveModel maps a table name to an Odoo model. Other names are taken as technical model names when they
// contain a dot or carry the x_ prefix Odoo requires for custom models.
func resolveModel(table string) (string, error) {
	table = strings.TrimSpace(table)
	if model, ok := tableModels[table]; ok {
		return model, nil
	}
	if (strings.Contains(table, ".") || strings.HasPrefix(table, "x_")) && !strings.ContainsAny(table, " /") {
		return table, nil
	}
	return "", fmt.Errorf("odoo: unsupported table %q (supported: %s, or a technical model name such as res.partner or x_custom)",
		table, strings.Join(supportedTables(), ", "))
}

type fieldMeta struct {
	Type  string `json:"type"`
	Store bool   `json:"store"`
}

type tablePlan struct {
	model          string
	columns        []schema.Column
	fieldTypes     map[string]string
	incrementalKey string
}

func (p *tablePlan) fieldNames() []string {
	names := make([]string, len(p.columns))
	for i, c := range p.columns {
		names[i] = c.Name
	}
	return names
}

func (s *OdooSource) fieldsGet(ctx context.Context, model string) (map[string]fieldMeta, error) {
	raw, err := s.client.call(ctx, model, "fields_get", map[string]interface{}{
		"attributes": []string{"type", "store"},
	})
	if err != nil {
		return nil, fmt.Errorf("odoo: failed to read the fields of model %s (is its app installed?): %w", model, err)
	}
	var fields map[string]fieldMeta
	if err := decodeJSON(raw, &fields); err != nil {
		return nil, fmt.Errorf("odoo: failed to parse the fields of model %s: %w", model, err)
	}
	return fields, nil
}

func buildPlan(model string, fields map[string]fieldMeta, requestedKey string) (*tablePlan, error) {
	p := &tablePlan{model: model, fieldTypes: map[string]string{"id": "integer"}}
	p.columns = append(p.columns, schema.Column{Name: "id", DataType: schema.TypeInt64, IsPrimaryKey: true})

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		f := fields[name]
		if name == "id" || !f.Store || skippedFieldTypes[f.Type] {
			continue
		}
		p.fieldTypes[name] = f.Type
		p.columns = append(p.columns, schema.Column{Name: name, DataType: dataTypeFor(f.Type), Nullable: true})
	}

	if requestedKey != "" {
		switch p.fieldTypes[requestedKey] {
		case "datetime", "date":
			p.incrementalKey = requestedKey
		default:
			return nil, fmt.Errorf("odoo: incremental key %q is not a stored date or datetime field of model %s", requestedKey, model)
		}
	} else if p.fieldTypes[writeDateField] == "datetime" {
		p.incrementalKey = writeDateField
	}

	return p, nil
}

func dataTypeFor(odooType string) schema.DataType {
	switch odooType {
	case "integer", "many2one", "many2one_reference":
		return schema.TypeInt64
	case "float", "monetary":
		return schema.TypeFloat64
	case "boolean":
		return schema.TypeBoolean
	case "date":
		return schema.TypeDate
	case "datetime":
		return schema.TypeTimestamp
	case "many2many", "json", "properties", "properties_definition":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}

func (s *OdooSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	model, err := resolveModel(req.Name)
	if err != nil {
		return nil, err
	}
	fields, err := s.fieldsGet(ctx, model)
	if err != nil {
		return nil, err
	}
	plan, err := buildPlan(model, fields, req.IncrementalKey)
	if err != nil {
		return nil, err
	}

	strategy := config.StrategyMerge
	if plan.incrementalKey == "" {
		strategy = config.StrategyReplace
		config.Debug("[ODOO] model %s has no write_date: every run re-reads it in full", model)
	}

	tableSchema := &schema.TableSchema{Name: req.Name, Columns: plan.columns, PrimaryKeys: []string{"id"}}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: plan.incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			results := make(chan source.RecordBatchResult, 4)
			go func() {
				defer close(results)
				if err := s.readModel(ctx, plan, opts, results); err != nil {
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

func intervalDomain(plan *tablePlan, opts source.ReadOptions) []interface{} {
	if plan.incrementalKey == "" {
		return nil
	}
	layout := odooDatetimeLayout
	if plan.fieldTypes[plan.incrementalKey] == "date" {
		layout = odooDateLayout
	}
	var domain []interface{}
	if opts.IntervalStart != nil {
		domain = append(domain, []interface{}{plan.incrementalKey, ">=", opts.IntervalStart.UTC().Format(layout)})
	}
	if opts.IntervalEnd != nil {
		domain = append(domain, []interface{}{plan.incrementalKey, "<=", opts.IntervalEnd.UTC().Format(layout)})
	}
	return domain
}

// readModel pages with a keyset on id rather than offset, so rows written mid-run cannot shift pages.
func (s *OdooSource) readModel(ctx context.Context, plan *tablePlan, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := s.pageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	fieldNames := plan.fieldNames()
	filter := intervalDomain(plan, opts)

	var batch []map[string]interface{}
	var accBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, plan.columns, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("odoo: failed to convert %s to Arrow: %w", plan.model, err)
		}
		select {
		case results <- source.RecordBatchResult{Batch: record}:
		case <-ctx.Done():
			record.Release()
			return ctx.Err()
		}
		batch = nil
		accBytes = 0
		return nil
	}

	var lastID int64
	total := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pageStart := lastID
		domain := append([]interface{}{[]interface{}{"id", ">", lastID}}, filter...)
		raw, err := s.client.call(ctx, plan.model, "search_read", map[string]interface{}{
			"domain":  domain,
			"fields":  fieldNames,
			"limit":   pageSize,
			"order":   "id asc",
			"load":    nil,
			"context": map[string]interface{}{"active_test": false},
		})
		if err != nil {
			return fmt.Errorf("odoo: failed to read %s: %w", plan.model, err)
		}
		var rows []map[string]interface{}
		if err := decodeJSON(raw, &rows); err != nil {
			return fmt.Errorf("odoo: failed to parse %s records: %w", plan.model, err)
		}
		for _, r := range rows {
			row := make(map[string]interface{}, len(plan.columns))
			for _, c := range plan.columns {
				row[c.Name] = coerce(r[c.Name], plan.fieldTypes[c.Name])
			}
			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(row)
				if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return err
					}
				}
				accBytes += rowBytes
			}
			batch = append(batch, row)
		}
		if err := flush(); err != nil {
			return err
		}
		total += len(rows)

		if len(rows) > 0 {
			next, ok := toInt64(rows[len(rows)-1]["id"])
			if !ok || next <= lastID {
				return fmt.Errorf("odoo: %s returned ids out of order after id %d, refusing to loop", plan.model, lastID)
			}
			lastID = next
			config.Debug("[ODOO] %s: %d row(s) so far (last id %d)", plan.model, total, lastID)
		}
		if len(rows) == pageSize {
			continue
		}

		// Some models (mail.message, ir.attachment) drop rows the user cannot access after the SQL limit,
		// so a short or empty page does not mean the end; ask whether accessible rows remain.
		more, err := s.hasAccessibleAfter(ctx, plan, lastID, filter)
		if err != nil {
			return err
		}
		if !more {
			break
		}
		// The SQL limit covered at least pageSize matching ids after pageStart, and every accessible one among
		// them came back, so the cursor can skip past that window even when the page was empty.
		lastID = max(lastID, pageStart+int64(pageSize))
	}

	config.Debug("[ODOO] %s complete: %d row(s)", plan.model, total)
	return nil
}

func (s *OdooSource) hasAccessibleAfter(ctx context.Context, plan *tablePlan, lastID int64, filter []interface{}) (bool, error) {
	domain := append([]interface{}{[]interface{}{"id", ">", lastID}}, filter...)
	raw, err := s.client.call(ctx, plan.model, "search_count", map[string]interface{}{
		"domain":  domain,
		"context": map[string]interface{}{"active_test": false},
	})
	if err != nil {
		return false, fmt.Errorf("odoo: failed to count remaining %s records: %w", plan.model, err)
	}
	var remaining int64
	if err := decodeJSON(raw, &remaining); err != nil {
		return false, fmt.Errorf("odoo: failed to parse %s record count: %w", plan.model, err)
	}
	if remaining > 0 {
		config.Debug("[ODOO] %s: short page after id %d but %d accessible row(s) remain", plan.model, lastID, remaining)
	}
	return remaining > 0, nil
}

// coerce converts a search_read value to the column's Go type. Odoo sends false for every empty
// non-boolean field, so false becomes null there.
func coerce(v interface{}, odooType string) interface{} {
	if v == nil {
		return nil
	}
	if _, ok := v.(bool); ok && odooType != "boolean" {
		return nil
	}

	switch odooType {
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil
		}
		return b
	case "integer", "many2one", "many2one_reference":
		// many2one arrives as [id, display_name] when the server ignores load=None.
		if pair, ok := v.([]interface{}); ok {
			if len(pair) == 0 {
				return nil
			}
			v = pair[0]
		}
		n, ok := toInt64(v)
		if !ok {
			return nil
		}
		return n
	case "float", "monetary":
		switch t := v.(type) {
		case json.Number:
			f, err := t.Float64()
			if err != nil {
				return nil
			}
			return f
		case float64:
			return t
		}
		return nil
	case "date":
		s, ok := v.(string)
		if !ok || len(s) < len(odooDateLayout) {
			return nil
		}
		t, err := time.Parse(odooDateLayout, s[:len(odooDateLayout)])
		if err != nil {
			return nil
		}
		return t
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return nil
		}
		t, err := time.ParseInLocation(odooDatetimeLayout, s, time.UTC)
		if err != nil {
			return nil
		}
		return t
	case "many2many", "json", "properties", "properties_definition":
		return v
	default:
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			return t.String()
		default:
			b, err := json.Marshal(t)
			if err != nil {
				return fmt.Sprintf("%v", t)
			}
			return string(b)
		}
	}
}

func toInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	}
	return 0, false
}
