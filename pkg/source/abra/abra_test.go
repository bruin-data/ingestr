package abra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	t.Parallel()

	t.Run("full URI", func(t *testing.T) {
		cfg, err := parseURI("abra://example.flexibee.eu?username=API&password=p%40ss&company=acme_s_r_o_&page_size=250&rate_limit=2")
		require.NoError(t, err)
		assert.Equal(t, "https://example.flexibee.eu", cfg.baseURL)
		assert.Equal(t, "API", cfg.username)
		assert.Equal(t, "p@ss", cfg.password, "the password must survive URL decoding intact")
		assert.Equal(t, "acme_s_r_o_", cfg.company)
		assert.Equal(t, 250, cfg.pageSize)
		assert.Equal(t, 2.0, cfg.rateLimit)
		assert.True(t, cfg.includeExpensive, "expensive properties are included by default in the raw layer")
	})

	t.Run("defaults", func(t *testing.T) {
		cfg, err := parseURI("abra://host?username=API&password=x&company=c")
		require.NoError(t, err)
		assert.Equal(t, defaultPageSize, cfg.pageSize)
		assert.Equal(t, defaultRateLimit, cfg.rateLimit)
	})

	// ⚠️ The company is the ONLY thing selecting which set of books is read, and
	// getting it wrong is silent. It must never acquire a default.
	t.Run("company is required", func(t *testing.T) {
		_, err := parseURI("abra://host?username=API&password=x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "company is required")
	})

	for _, tc := range []struct{ name, uri, want string }{
		{"missing username", "abra://host?password=x&company=c", "username is required"},
		{"missing password", "abra://host?username=API&company=c", "password is required"},
		{"missing host", "abra://?username=API&password=x&company=c", "host is required"},
		{"wrong scheme", "https://host?username=API&password=x&company=c", "must start with abra://"},
		{"bad page_size", "abra://host?username=API&password=x&company=c&page_size=0", "page_size must be a positive integer"},
		{"bad rate_limit", "abra://host?username=API&password=x&company=c&rate_limit=-1", "rate_limit must be a positive number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseURI(tc.uri)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSanitizeColumn(t *testing.T) {
	t.Parallel()
	// Casing is preserved on purpose — the substitution is mechanical, not a rename.
	assert.Equal(t, "mena", sanitizeColumn("mena"))
	assert.Equal(t, "mena_ref", sanitizeColumn("mena@ref"))
	assert.Equal(t, "mena_showAs", sanitizeColumn("mena@showAs"))
	assert.Equal(t, "external_ids", sanitizeColumn("external-ids"))
	assert.Equal(t, "lastUpdate", sanitizeColumn("lastUpdate"))
}

func TestDataTypeFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, schema.TypeInt64, dataTypeFor("integer"))
	assert.Equal(t, schema.TypeBoolean, dataTypeFor("logic"))
	assert.Equal(t, schema.TypeDate, dataTypeFor("date"))
	assert.Equal(t, schema.TypeTimestamp, dataTypeFor("datetime"))
	assert.Equal(t, schema.TypeString, dataTypeFor("string"))
	assert.Equal(t, schema.TypeString, dataTypeFor("relation"))

	// Money is TEXT: Float64 would lose cents on an accounting ledger. See the
	// comment on dataTypeFor.
	assert.Equal(t, schema.TypeString, dataTypeFor("numeric"),
		"numeric must stay a string column — see dataTypeFor for why")
}

func TestBuildPlanRefusesEvidenceWithoutPrimaryKey(t *testing.T) {
	t.Parallel()
	// This is `ucetni-denik`: a derived view, 47k rows, id = -1, empty lastUpdate.
	doc := &propertiesDoc{Property: []property{
		{PropertyName: "ucet", Type: "string"},
		{PropertyName: "castka", Type: "numeric"},
	}}
	_, err := buildPlan("ucetni-denik", doc, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "derived view")
	assert.Contains(t, err.Error(), "merge")
}

func TestBuildPlanExpandsRelations(t *testing.T) {
	t.Parallel()
	doc := &propertiesDoc{Property: []property{
		{PropertyName: "id", Type: "integer", InID: "true"},
		{PropertyName: "lastUpdate", Type: "datetime"},
		{PropertyName: "sumCelkem", Type: "numeric"},
		{PropertyName: "mena", Type: "relation"},
		{PropertyName: "stavUhrK", Type: "select"},
	}}
	plan, err := buildPlan("faktura-vydana", doc, true)
	require.NoError(t, err)
	assert.True(t, plan.hasLastUpdate)
	assert.Equal(t, "id", plan.primaryKey)

	names := map[string]schema.DataType{}
	for _, c := range plan.columns {
		names[c.Name] = c.DataType
	}
	// Both relation and select get the full triple: an always-NULL column is
	// harmless, a missing one loses data.
	for _, want := range []string{"mena", "mena_ref", "mena_showAs", "stavUhrK", "stavUhrK_ref", "stavUhrK_showAs"} {
		assert.Contains(t, names, want)
	}
	assert.Equal(t, schema.TypeString, names["sumCelkem"])
	assert.Equal(t, schema.TypeInt64, names["id"])
	assert.Contains(t, names, "external_ids")

	// ingestr's strategy layer owns the load timestamp and the promote uses it as
	// the ReplacingMergeTree version column — we must not declare our own.
	assert.NotContains(t, names, "_ingestr_loaded_at")

	for _, c := range plan.columns {
		if c.Name == "id" {
			assert.True(t, c.IsPrimaryKey)
			assert.False(t, c.Nullable)
		}
	}
}

func TestBuildPlanExpensiveExclusion(t *testing.T) {
	t.Parallel()
	doc := &propertiesDoc{Property: []property{
		{PropertyName: "id", Type: "integer", InID: "true"},
		{PropertyName: "slow", Type: "string", InExpensive: "true"},
	}}

	included, err := buildPlan("e", doc, true)
	require.NoError(t, err)
	assert.Equal(t, 1, included.expensiveCount)
	assert.Contains(t, included.sourceToColumn, "slow")

	excluded, err := buildPlan("e", doc, false)
	require.NoError(t, err)
	assert.NotContains(t, excluded.sourceToColumn, "slow")
}

// Flexi wraps its three endpoints three different ways and the wrapper name matches
// neither the URL nor the inner key, so findArray must tolerate both depths.
func TestFindArrayHandlesBothEnvelopes(t *testing.T) {
	t.Parallel()

	wrapped := []byte(`{"properties":{"@version":"1.0","property":[{"propertyName":"id"}]}}`)
	got, err := findArray(wrapped, "property")
	require.NoError(t, err)
	assert.JSONEq(t, `[{"propertyName":"id"}]`, string(got))

	flat := []byte(`{"@version":"1.0","property":[{"propertyName":"kod"}]}`)
	got, err = findArray(flat, "property")
	require.NoError(t, err)
	assert.JSONEq(t, `[{"propertyName":"kod"}]`, string(got))

	// evidence-list uses a third wrapper name again.
	got, err = findArray([]byte(`{"evidences":{"evidence":[{"evidencePath":"adresar"}]}}`), "evidence")
	require.NoError(t, err)
	assert.JSONEq(t, `[{"evidencePath":"adresar"}]`, string(got))

	// A miss must name the keys it did see — that diagnostic is what turned an
	// opaque KeyError into a one-line fix.
	_, err = findArray([]byte(`{"winstrom":{"something":[]}}`), "property")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "winstrom")
}

func TestExtractRecords(t *testing.T) {
	t.Parallel()

	body := []byte(`{"winstrom":{"@version":"1.0","@rowCount":"14035","faktura-vydana":[{"id":730},{"id":731}]}}`)
	items, count, err := extractRecords(body, "faktura-vydana")
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, 14035, count, "@rowCount arrives as a STRING and must still parse")

	// ⚠️ THE REGRESSION THAT COST A SILENT ZERO. `stav-ceniku` returns its rows under a
	// key that is NOT the evidence path. Looking up by name alone produced 0 rows, no
	// error, "Ingestion completed successfully" and no table — caught only by counting
	// rows against the probe. The array must be found even when the key differs.
	items, count, err = extractRecords(
		[]byte(`{"winstrom":{"@version":"1.0","@rowCount":"5","cenikStav":[{"id":1},{"id":2}]}}`),
		"stav-ceniku")
	require.NoError(t, err)
	assert.Len(t, items, 2, "records under a differently-named key must still be found")
	assert.Equal(t, 5, count)

	// Only a genuinely array-less envelope is an empty page.
	items, count, err = extractRecords([]byte(`{"winstrom":{"@version":"1.0"}}`), "faktura-vydana")
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, -1, count)

	_, _, err = extractRecords([]byte(`{"nope":{}}`), "faktura-vydana")
	require.Error(t, err)
}

// ⚠️ The offset on a Flexi DATE must be discarded, not applied. An invoice issued
// 2026-01-01 in CET would move to 2025-12-31 under UTC conversion — a different
// accounting period, and a wrong number in a VAT return.
func TestParseFlexiDateDiscardsOffset(t *testing.T) {
	t.Parallel()
	got, ok := parseFlexiDate("2026-01-01+01:00").(time.Time)
	require.True(t, ok)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.January, got.Month())
	assert.Equal(t, 1, got.Day(), "the calendar date must not shift")

	got2, ok := parseFlexiDate("2025-12-12").(time.Time)
	require.True(t, ok)
	assert.Equal(t, 12, got2.Day())

	assert.Nil(t, parseFlexiDate(""))
	assert.Nil(t, parseFlexiDate("not-a-date"))
}

// A datetime IS a true instant, so here the offset is applied and normalised.
func TestParseFlexiDateTimeNormalisesToUTC(t *testing.T) {
	t.Parallel()
	got, ok := parseFlexiDateTime("2026-02-11T15:09:06.376+01:00").(time.Time)
	require.True(t, ok)
	assert.Equal(t, time.UTC, got.Location())
	assert.Equal(t, 14, got.Hour(), "15:09 +01:00 is 14:09 UTC")
	assert.Equal(t, 376, got.Nanosecond()/1e6)

	assert.Nil(t, parseFlexiDateTime(""))
	assert.Nil(t, parseFlexiDateTime("garbage"))
}

// Money must survive as the exact text Flexi sent — no float round-trip anywhere.
func TestCoerceKeepsNumericTextExact(t *testing.T) {
	t.Parallel()
	var payload map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(`{"sumCelkem": 12345678901234.57, "small": 0.1}`))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&payload))

	assert.Equal(t, "12345678901234.57", coerce(payload["sumCelkem"], schema.TypeString))
	assert.Equal(t, "0.1", coerce(payload["small"], schema.TypeString))

	assert.Equal(t, int64(730), coerce(json.Number("730"), schema.TypeInt64))
	assert.Equal(t, true, coerce("true", schema.TypeBoolean))
	assert.Nil(t, coerce(nil, schema.TypeString))
	// A nested object becomes JSON text rather than being dropped.
	assert.Equal(t, `{"a":1}`, coerce(map[string]interface{}{"a": json.Number("1")}, schema.TypeString))
}

func TestBuildFilter(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	plan := &tablePlan{hasLastUpdate: true}
	assert.Equal(t, "lastUpdate gte '2026-01-15T10:30:00.000Z'",
		buildFilter(plan, source.ReadOptions{IntervalStart: &start}))

	// No cursor column -> no filter, so the evidence is re-read in full.
	assert.Equal(t, "", buildFilter(&tablePlan{hasLastUpdate: false}, source.ReadOptions{IntervalStart: &start}))
	// No window -> full read.
	assert.Equal(t, "", buildFilter(plan, source.ReadOptions{}))
}

// End-to-end against a fake Flexi: schema fetch, paging, projection and the
// short-page stop condition.
func TestReadPagesEndToEnd(t *testing.T) {
	t.Parallel()

	const total = 25
	var gotLimits, gotStarts, gotOrders []string
	var sawFilter string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "API" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/properties.json") {
			// ⚠️ The REAL wire shape: wrapped in "properties". Verified against the
			// live API on 2026-08-14 — do not "simplify" this fixture to the flat
			// form, it is what two probe runs died on.
			_, _ = w.Write([]byte(`{"properties":{"@version":"1.0","evidenceName":"Vydané faktury","property":[
				{"propertyName":"id","type":"integer","inId":"true"},
				{"propertyName":"lastUpdate","type":"datetime"},
				{"propertyName":"datVyst","type":"date"},
				{"propertyName":"sumCelkem","type":"numeric"},
				{"propertyName":"mena","type":"relation"}]}}`))
			return
		}

		q := r.URL.Query()
		gotLimits = append(gotLimits, q.Get("limit"))
		gotStarts = append(gotStarts, q.Get("start"))
		gotOrders = append(gotOrders, q.Get("order"))
		if f := q.Get("filter"); f != "" {
			sawFilter = f
		}

		limit, _ := strconv.Atoi(q.Get("limit"))
		start, _ := strconv.Atoi(q.Get("start"))

		rows := []string{}
		for i := start; i < start+limit && i < total; i++ {
			rows = append(rows, fmt.Sprintf(`{"id":%d,"lastUpdate":"2026-02-11T15:09:06.376+01:00",
				"datVyst":"2026-01-01+01:00","sumCelkem":%d.55,"mena":"code:CZK",
				"mena@ref":"/c/x/mena/1.json","mena@showAs":"CZK","surprise":"undeclared"}`, i, i))
		}
		env := fmt.Sprintf(`{"winstrom":{"@version":"1.0","@rowCount":"%d","faktura-vydana":[%s]}}`,
			total, strings.Join(rows, ","))
		_, _ = w.Write([]byte(env))
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	s := NewAbraSource()
	uri := fmt.Sprintf("abra://%s?scheme=http&username=API&password=secret&company=test_co&page_size=10", host)
	require.NoError(t, s.Connect(context.Background(), uri))
	defer func() { _ = s.Close(context.Background()) }()

	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: "faktura-vydana"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, tbl.PrimaryKeys())
	assert.Equal(t, "lastUpdate", tbl.IncrementalKey())

	sch, err := tbl.GetSchema(context.Background())
	require.NoError(t, err)
	names := map[string]bool{}
	for _, c := range sch.Columns {
		names[c.Name] = true
	}
	assert.True(t, names["mena_ref"] && names["mena_showAs"], "relation columns must be sanitized and present")
	assert.False(t, names["surprise"], "undeclared fields are dropped, not silently typed")

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ch, err := tbl.Read(context.Background(), source.ReadOptions{IntervalStart: &start})
	require.NoError(t, err)

	rows := 0
	for res := range ch {
		require.NoError(t, res.Err)
		rows += int(res.Batch.NumRows())
	}
	assert.Equal(t, total, rows, "all rows across all pages must arrive exactly once")

	// limit is ALWAYS explicit; ordering is by the immutable id, not the cursor.
	for _, l := range gotLimits {
		assert.Equal(t, "10", l)
	}
	assert.Equal(t, []string{"0", "10", "20"}, gotStarts)
	for _, o := range gotOrders {
		assert.Equal(t, "id@A", o)
	}
	assert.Equal(t, "lastUpdate gte '2026-01-01T00:00:00.000Z'", sawFilter)
}

func TestGetTableRejectsUnknownEvidence(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := NewAbraSource()
	host := strings.TrimPrefix(srv.URL, "http://")
	require.NoError(t, s.Connect(context.Background(),
		fmt.Sprintf("abra://%s?scheme=http&username=API&password=x&company=c", host)))

	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "no-such-evidence"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}
