package odoo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	t.Parallel()

	cfg, err := parseURI("odoo://erp.example.com:8069/odoo/?database=prod&username=me%40x.com&api_key=k%26y&scheme=http&api=jsonrpc&page_size=200")
	require.NoError(t, err)
	assert.Equal(t, "http://erp.example.com:8069/odoo", cfg.baseURL)
	assert.Equal(t, "prod", cfg.database)
	assert.Equal(t, "me@x.com", cfg.username)
	assert.Equal(t, "k&y", cfg.apiKey)
	assert.Equal(t, apiJSONRPC, cfg.api)
	assert.Equal(t, 200, cfg.pageSize)

	cfg, err = parseURI("odoo://acme.odoo.com?password=secret")
	require.NoError(t, err)
	assert.Equal(t, "https://acme.odoo.com", cfg.baseURL)
	assert.Equal(t, "secret", cfg.apiKey)
	assert.Equal(t, apiAuto, cfg.api)
	assert.Equal(t, defaultPageSize, cfg.pageSize)

	for _, tc := range []struct{ name, uri, want string }{
		{"wrong scheme", "https://host?api_key=x", "must start with odoo://"},
		{"missing host", "odoo://?api_key=x", "host is required"},
		{"missing api_key", "odoo://host?database=d", "api_key is required"},
		{"bad transport", "odoo://host?api_key=x&scheme=ftp", "scheme must be https or http"},
		{"bad api", "odoo://host?api_key=x&api=xmlrpc", "api must be one of"},
		{"jsonrpc without username", "odoo://host?api_key=x&api=jsonrpc&database=d", "database and username are required"},
		{"zero page_size", "odoo://host?api_key=x&page_size=0", "page_size must be a positive integer"},
		{"page_size over cap", "odoo://host?api_key=x&page_size=5001", "may not exceed 5000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseURI(tc.uri)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestResolveModel(t *testing.T) {
	t.Parallel()

	for table, want := range map[string]string{
		"sale_orders":   "sale.order",
		"partners":      "res.partner",
		"fleet.vehicle": "fleet.vehicle",
		"x_vehicles":    "x_vehicles",
	} {
		got, err := resolveModel(table)
		require.NoError(t, err, table)
		assert.Equal(t, want, got)
	}
	for _, table := range []string{"", "Partners", "sale_order", "res partner", "../res.partner"} {
		_, err := resolveModel(table)
		assert.Error(t, err, table)
	}
}

func TestBuildPlan(t *testing.T) {
	t.Parallel()

	fields := map[string]fieldMeta{
		"id":           {Type: "integer", Store: true},
		"name":         {Type: "char", Store: true},
		"partner_id":   {Type: "many2one", Store: true},
		"tag_ids":      {Type: "many2many", Store: true},
		"order_line":   {Type: "one2many", Store: true},
		"image_1920":   {Type: "binary", Store: true},
		"display_name": {Type: "char", Store: false},
		"date_order":   {Type: "datetime", Store: true},
		"write_date":   {Type: "datetime", Store: true},
		"amount_total": {Type: "monetary", Store: true},
	}

	p, err := buildPlan("sale.order", fields, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "amount_total", "date_order", "name", "partner_id", "tag_ids", "write_date"}, p.fieldNames())
	assert.Equal(t, "write_date", p.incrementalKey)
	assert.True(t, p.columns[0].IsPrimaryKey)
	types := map[string]schema.DataType{}
	for _, c := range p.columns {
		types[c.Name] = c.DataType
	}
	assert.Equal(t, schema.TypeInt64, types["partner_id"])
	assert.Equal(t, schema.TypeJSON, types["tag_ids"])
	assert.Equal(t, schema.TypeFloat64, types["amount_total"])
	assert.Equal(t, schema.TypeTimestamp, types["write_date"])

	p, err = buildPlan("sale.order", fields, "date_order")
	require.NoError(t, err)
	assert.Equal(t, "date_order", p.incrementalKey)

	_, err = buildPlan("sale.order", fields, "name")
	assert.ErrorContains(t, err, "not a stored date or datetime field")
	_, err = buildPlan("sale.order", fields, "display_name")
	assert.Error(t, err)

	p, err = buildPlan("ir.logging.like", map[string]fieldMeta{"name": {Type: "char", Store: true}}, "")
	require.NoError(t, err)
	assert.Empty(t, p.incrementalKey, "a model without write_date cannot load incrementally")
	assert.Equal(t, []string{"id", "name"}, p.fieldNames())
}

func TestCoerce(t *testing.T) {
	t.Parallel()

	assert.Nil(t, coerce(false, "char"), "Odoo encodes an empty char as false")
	assert.Nil(t, coerce(false, "many2one"))
	assert.Nil(t, coerce(false, "date"))
	assert.Equal(t, false, coerce(false, "boolean"))
	assert.Equal(t, true, coerce(true, "boolean"))
	assert.Equal(t, int64(9007199254740993), coerce(json.Number("9007199254740993"), "integer"))
	assert.Equal(t, int64(7), coerce([]interface{}{json.Number("7"), "Azure Interior"}, "many2one"))
	assert.Equal(t, 12.5, coerce(json.Number("12.5"), "monetary"))
	assert.Equal(t, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), coerce("2026-03-04", "date"))
	assert.Equal(t, time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC), coerce("2026-03-04 05:06:07", "datetime"))
	assert.Equal(t, "draft", coerce("draft", "selection"))
	assert.Equal(t, "3", coerce(json.Number("3"), "selection"))
	ids := []interface{}{json.Number("1"), json.Number("2")}
	assert.Equal(t, ids, coerce(ids, "many2many"))
}

func TestParseMajorVersion(t *testing.T) {
	t.Parallel()

	for in, want := range map[interface{}]int{float64(19): 19, "saas~18": 18, "17": 17} {
		got, ok := parseMajorVersion(in)
		require.True(t, ok, in)
		assert.Equal(t, want, got)
	}
	_, ok := parseMajorVersion("master")
	assert.False(t, ok)
}

// fakeJSON2 serves records 1..total over JSON-2. Like mail.message, it applies the limit before dropping
// hidden ids, so pages come back short or empty while later rows remain.
func fakeJSON2(t *testing.T, total int, hidden map[int]bool, domains *[][]interface{}) *httptest.Server {
	t.Helper()
	visibleAfter := func(after, limit int) []int {
		var ids []int
		for id := after + 1; id <= total && (limit == 0 || id <= after+limit); id++ {
			if !hidden[id] {
				ids = append(ids, id)
			}
		}
		return ids
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Domain  []interface{}          `json:"domain"`
			Limit   int                    `json:"limit"`
			Order   string                 `json:"order"`
			Context map[string]interface{} `json:"context"`
		}
		after := 0
		if r.Method == http.MethodPost {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			if len(body.Domain) > 0 {
				after = int(body.Domain[0].([]interface{})[2].(float64))
			}
		}
		switch r.URL.Path {
		case "/web/version":
			_, _ = w.Write([]byte(`{"version_info": [19, 0, 0, "final", 0, ""], "version": "19.0"}`))
		case "/json/2/res.users/context_get":
			assert.Equal(t, "Bearer key", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"lang": "en_US"}`))
		case "/json/2/res.partner/search_read":
			assert.Equal(t, "id asc", body.Order)
			assert.Equal(t, false, body.Context["active_test"], "archived records must be read too")
			*domains = append(*domains, body.Domain)
			rows := []map[string]interface{}{}
			for _, id := range visibleAfter(after, body.Limit) {
				rows = append(rows, map[string]interface{}{"id": id, "name": "p", "write_date": "2026-01-02 03:04:05"})
			}
			_ = json.NewEncoder(w).Encode(rows)
		case "/json/2/res.partner/search_count":
			_ = json.NewEncoder(w).Encode(len(visibleAfter(after, 0)))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestReadPagesByIDKeyset(t *testing.T) {
	t.Parallel()

	var domains [][]interface{}
	srv := fakeJSON2(t, 15, map[int]bool{5: true, 7: true, 8: true, 9: true, 10: true, 11: true, 12: true}, &domains)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	s := NewOdooSource()
	require.NoError(t, s.Connect(context.Background(), "odoo://"+host+"?scheme=http&api_key=key&page_size=3"))
	defer func() { _ = s.Close(context.Background()) }()
	assert.Equal(t, apiJSON2, s.client.api, "Odoo 19 must be read over JSON-2")

	plan, err := buildPlan("res.partner", map[string]fieldMeta{
		"name":       {Type: "char", Store: true},
		"write_date": {Type: "datetime", Store: true},
	}, "")
	require.NoError(t, err)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("TRT", 3*3600))
	results := make(chan source.RecordBatchResult, 10)
	require.NoError(t, s.readModel(context.Background(), plan, source.ReadOptions{IntervalStart: &start}, results))
	close(results)

	var ids []int64
	for r := range results {
		require.NoError(t, r.Err)
		col := r.Batch.Column(0).(*array.Int64)
		ids = append(ids, col.Int64Values()...)
	}
	assert.Equal(t, []int64{1, 2, 3, 4, 6, 13, 14, 15}, ids, "a short page (4, 6) and two empty pages (7-12 hidden) must not end the read")

	var afters []float64
	for _, d := range domains {
		afters = append(afters, d[0].([]interface{})[2].(float64))
		assert.Equal(t, []interface{}{"write_date", ">=", "2025-12-31 21:00:00"}, d[1], "the interval is sent in UTC")
	}
	assert.Equal(t, []float64{0, 3, 6, 9, 12, 15}, afters, "each hidden window is skipped by one page")
}
