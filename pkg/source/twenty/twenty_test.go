package twenty

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		cfg, err := parseURI("twenty://crm.example.com?api_key=k%3Dey%26x&page_size=50&rate_limit=2&include_deleted=false")
		require.NoError(t, err)
		assert.Equal(t, "https://crm.example.com/rest", cfg.baseURL)
		assert.Equal(t, "k=ey&x", cfg.apiKey, "the API key must survive URL decoding intact")
		assert.Equal(t, 50, cfg.pageSize)
		assert.Equal(t, 2.0, cfg.rateLimit)
		assert.False(t, cfg.includeDeleted)
	})

	t.Run("defaults", func(t *testing.T) {
		cfg, err := parseURI("twenty://api.twenty.com?api_key=x")
		require.NoError(t, err)
		assert.Equal(t, "https://api.twenty.com/rest", cfg.baseURL)
		assert.Equal(t, defaultPageSize, cfg.pageSize)
		assert.Equal(t, defaultRateLimit, cfg.rateLimit)
		// Soft-deleted rows are included by default — without the second pass a
		// deleted record silently persists in the warehouse looking live.
		assert.True(t, cfg.includeDeleted)
	})

	t.Run("base_path override", func(t *testing.T) {
		cfg, err := parseURI("twenty://host?api_key=x&base_path=api/rest")
		require.NoError(t, err)
		assert.Equal(t, "https://host/api/rest", cfg.baseURL)
	})

	for _, tc := range []struct{ name, uri, want string }{
		{"missing api_key", "twenty://host", "api_key is required"},
		{"missing host", "twenty://?api_key=x", "host is required"},
		{"wrong scheme", "https://host?api_key=x", "must start with twenty://"},
		{"bad page_size", "twenty://host?api_key=x&page_size=0", "page_size must be a positive integer"},
		{"page_size over cap", "twenty://host?api_key=x&page_size=500", "may not exceed 200"},
		{"bad rate_limit", "twenty://host?api_key=x&rate_limit=-1", "rate_limit must be a positive number"},
		{"bad include_deleted", "twenty://host?api_key=x&include_deleted=maybe", "include_deleted must be a boolean"},
		{"bad transport", "twenty://host?api_key=x&scheme=ftp", "scheme must be https or http"},
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
	// Twenty enforces camelCase api names, so this is normally a no-op — and the
	// casing must be preserved so the column still reads as Twenty's.
	assert.Equal(t, "companyId", sanitizeColumn("companyId"))
	assert.Equal(t, "updatedAt", sanitizeColumn("updatedAt"))
	assert.Equal(t, "utmSource", sanitizeColumn("utmSource"))
	assert.Equal(t, "odd_name", sanitizeColumn("odd-name"))
}

func TestDataTypeFor(t *testing.T) {
	t.Parallel()

	str := func(typ string) schema.DataType {
		dt, keep := dataTypeFor(fieldMeta{Type: typ})
		require.True(t, keep)
		return dt
	}

	assert.Equal(t, schema.TypeBoolean, str("BOOLEAN"))
	assert.Equal(t, schema.TypeTimestamp, str("DATE_TIME"))
	assert.Equal(t, schema.TypeDate, str("DATE"))
	assert.Equal(t, schema.TypeFloat64, str("POSITION"))
	assert.Equal(t, schema.TypeString, str("UUID"))
	assert.Equal(t, schema.TypeString, str("TEXT"))
	assert.Equal(t, schema.TypeString, str("SELECT"))

	for _, composite := range []string{"EMAILS", "PHONES", "LINKS", "ADDRESS", "CURRENCY", "FULL_NAME", "ACTOR", "RICH_TEXT_V2", "ARRAY", "MULTI_SELECT", "RAW_JSON"} {
		assert.Equal(t, schema.TypeJSON, str(composite), composite)
	}

	// NUMBER honours settings.dataType: ints stay ints, anything else keeps its
	// exact decimal text rather than rounding through a float.
	assert.Equal(t, schema.TypeInt64, str("NUMBER"))
	dt, keep := dataTypeFor(fieldMeta{Type: "NUMBER", Settings: &relationSettings{DataType: "int"}})
	require.True(t, keep)
	assert.Equal(t, schema.TypeInt64, dt)
	dt, keep = dataTypeFor(fieldMeta{Type: "NUMBER", Settings: &relationSettings{DataType: "float"}})
	require.True(t, keep)
	assert.Equal(t, schema.TypeString, dt)

	// searchVector is a Postgres full-text index — derived, bulky, never useful.
	_, keep = dataTypeFor(fieldMeta{Type: "TS_VECTOR"})
	assert.False(t, keep)
}

// personMeta mirrors a live `person` object closely enough to exercise
// every branch of buildPlan.
func personMeta() objectMeta {
	return objectMeta{
		NameSingular: "person", NamePlural: "people", IsActive: true,
		Fields: []fieldMeta{
			{Name: "id", Type: "UUID", IsActive: true},
			{Name: "createdAt", Type: "DATE_TIME", IsActive: true},
			{Name: "updatedAt", Type: "DATE_TIME", IsActive: true},
			{Name: "deletedAt", Type: "DATE_TIME", IsActive: true},
			{Name: "name", Type: "FULL_NAME", IsActive: true},
			{Name: "emails", Type: "EMAILS", IsActive: true},
			{Name: "position", Type: "POSITION", IsActive: true},
			{Name: "searchVector", Type: "TS_VECTOR", IsActive: true},
			{Name: "marketingConsent", Type: "BOOLEAN", IsActive: true},
			{Name: "retiredField", Type: "TEXT", IsActive: false},
			// MANY_TO_ONE: the FK is what depth=0 actually returns.
			{
				Name: "company", Type: "RELATION", IsActive: true,
				Settings: &relationSettings{RelationType: "MANY_TO_ONE", JoinColumnName: "companyId"},
			},
			// ONE_TO_MANY: no column on this side of the edge.
			{
				Name: "noteTargets", Type: "RELATION", IsActive: true,
				Settings: &relationSettings{RelationType: "ONE_TO_MANY"},
			},
		},
	}
}

func TestBuildPlan(t *testing.T) {
	t.Parallel()

	plan, err := buildPlan(personMeta())
	require.NoError(t, err)

	names := make([]string, 0, len(plan.columns))
	for _, c := range plan.columns {
		names = append(names, c.Name)
	}

	assert.Contains(t, names, "companyId")
	assert.NotContains(t, names, "company")

	// ONE_TO_MANY and TS_VECTOR are dropped, and dropped deliberately — so they
	// must not later be reported as drift.
	assert.NotContains(t, names, "noteTargets")
	assert.NotContains(t, names, "searchVector")
	assert.Contains(t, plan.dropped, "noteTargets")
	assert.Contains(t, plan.dropped, "searchVector")

	// Inactive fields are not columns.
	assert.NotContains(t, names, "retiredField")

	assert.True(t, plan.hasUpdatedAt)
	assert.True(t, plan.hasDeletedAt)
	assert.Equal(t, "people", plan.object)

	// id is the primary key and is never nullable.
	var id schema.Column
	for _, c := range plan.columns {
		if c.Name == "id" {
			id = c
		}
	}
	assert.True(t, id.IsPrimaryKey)
	assert.False(t, id.Nullable)
	assert.Equal(t, schema.TypeString, id.DataType)
	assert.Equal(t, schema.TypeJSON, plan.typeOf["name"])
	assert.Equal(t, schema.TypeJSON, plan.typeOf["emails"])
}

// Without `id` there is nothing to deduplicate on, and under an append-style
// incremental strategy every run would add the whole object forever.
func TestBuildPlanRefusesObjectWithoutID(t *testing.T) {
	t.Parallel()
	_, err := buildPlan(objectMeta{
		NameSingular: "thing", NamePlural: "things", IsActive: true,
		Fields: []fieldMeta{{Name: "name", Type: "TEXT", IsActive: true}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no `id` field")
}

func TestFindObject(t *testing.T) {
	t.Parallel()
	objects := []objectMeta{personMeta(), {NameSingular: "company", NamePlural: "companies"}}

	got, err := findObject(objects, "people")
	require.NoError(t, err)
	assert.Equal(t, "people", got.NamePlural)

	// "person" vs "people" is the easiest CronJob typo to make, and the API's own
	// 404 says nothing useful.
	_, err = findObject(objects, "person")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `use the plural api name "people"`)

	_, err = findObject(objects, "leads")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no object \"leads\"")
	assert.Contains(t, err.Error(), "companies, people", "the error must list what IS available")
}

func TestBuildFilter(t *testing.T) {
	t.Parallel()
	plan, err := buildPlan(personMeta())
	require.NoError(t, err)

	start := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)

	assert.Equal(t, "", buildFilter(plan, source.ReadOptions{}, false))
	assert.Equal(t, "deletedAt[is]:NOT_NULL", buildFilter(plan, source.ReadOptions{}, true))
	assert.Equal(t, "updatedAt[gte]:2026-08-01T12:30:00.000Z",
		buildFilter(plan, source.ReadOptions{IntervalStart: &start}, false))
	assert.Equal(t, "and(updatedAt[gte]:2026-08-01T12:30:00.000Z,deletedAt[is]:NOT_NULL)",
		buildFilter(plan, source.ReadOptions{IntervalStart: &start}, true))

	// Only the START bound is ever applied — an upper bound would make a re-run of
	// an old window silently drop rows edited since.
	end := start.Add(24 * time.Hour)
	assert.NotContains(t, buildFilter(plan, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, false), "lte")
}

func TestExtractRecords(t *testing.T) {
	t.Parallel()

	// The exact envelope shape the live API returns.
	body := `{"data":{"people":[{"id":"a","name":{"firstName":"X"}}]},
	          "pageInfo":{"endCursor":"eyJpZCI6ImEifQ==","hasNextPage":true,"hasPreviousPage":false},
	          "totalCount":68279}`
	items, info, total, err := extractRecords([]byte(body), "people")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "a", items[0]["id"])
	assert.True(t, info.HasNextPage)
	assert.Equal(t, "eyJpZCI6ImEifQ==", info.EndCursor)
	assert.Equal(t, 68279, total)

	items, _, _, err = extractRecords([]byte(`{"data":{"somethingElse":[{"id":"b"}]},"pageInfo":{},"totalCount":1}`), "people")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "b", items[0]["id"])

	_, _, _, err = extractRecords([]byte(`{"pageInfo":{}}`), "people")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data envelope")
}

func TestExtractRecordsKeepsBigIntegersExact(t *testing.T) {
	t.Parallel()
	body := `{"data":{"opportunities":[{"id":"a","amount":{"amountMicros":9007199254740993,"currencyCode":"CZK"}}]},"pageInfo":{},"totalCount":1}`
	items, _, _, err := extractRecords([]byte(body), "opportunities")
	require.NoError(t, err)

	got, err := json.Marshal(coerce(items[0]["amount"], schema.TypeJSON))
	require.NoError(t, err)
	assert.Contains(t, string(got), "9007199254740993", "amountMicros must survive verbatim, not as 9.007199254740992e+15")
}

func TestCoerce(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(400), coerce(json.Number("400"), schema.TypeInt64))
	assert.Equal(t, -586576.5, coerce(json.Number("-586576.5"), schema.TypeFloat64))
	assert.Equal(t, true, coerce(true, schema.TypeBoolean))
	assert.Nil(t, coerce(nil, schema.TypeString))

	composite := map[string]interface{}{"primaryEmail": "a@b.c", "additionalEmails": []interface{}{}}
	assert.Equal(t, composite, coerce(composite, schema.TypeJSON))

	// Twenty's "unset" for several custom fields is an empty string, not null.
	assert.Nil(t, coerce("", schema.TypeDate))
	assert.Nil(t, coerce("", schema.TypeTimestamp))
}

func TestParseTwentyTimestampAndDate(t *testing.T) {
	t.Parallel()

	ts := parseTwentyTimestamp("2026-08-15T01:44:03.057Z")
	require.IsType(t, time.Time{}, ts)
	assert.Equal(t, time.Date(2026, 8, 15, 1, 44, 3, 57000000, time.UTC), ts)

	d := parseTwentyDate("2026-07-30")
	require.IsType(t, time.Time{}, d)
	assert.Equal(t, time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), d)

	// Unparseable input becomes a NULL we chose, rather than an Arrow builder
	// silently AppendNull()ing it.
	assert.Nil(t, parseTwentyTimestamp("not a time"))
	assert.Nil(t, parseTwentyDate("nope"))
}

type fakeTwenty struct {
	t       *testing.T
	objects []objectMeta
	people  []map[string]interface{}
	deleted []map[string]interface{}

	pageSizesSeen []string
	depthsSeen    []string
	filtersSeen   []string
}

func (f *fakeTwenty) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/rest/metadata/objects" {
			objects := f.objects
			if len(objects) == 0 {
				objects = []objectMeta{personMeta()}
			}
			require.NoError(f.t, json.NewEncoder(w).Encode(map[string]interface{}{
				"data":       map[string]interface{}{"objects": objects},
				"pageInfo":   map[string]interface{}{"hasNextPage": false},
				"totalCount": len(objects),
			}))
			return
		}
		if r.URL.Path != "/rest/people" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		q := r.URL.Query()
		f.pageSizesSeen = append(f.pageSizesSeen, q.Get("limit"))
		f.depthsSeen = append(f.depthsSeen, q.Get("depth"))
		f.filtersSeen = append(f.filtersSeen, q.Get("filter"))

		set := f.people
		if strings.Contains(q.Get("filter"), "deletedAt[is]:NOT_NULL") {
			set = f.deleted
		}

		// Cursor = index of the last row served, base64-free for test clarity.
		start := 0
		if c := q.Get("starting_after"); c != "" {
			_, err := fmt.Sscanf(c, "cur-%d", &start)
			require.NoError(f.t, err)
			start++
		}
		end := start + 2 // deliberately smaller than the requested limit
		if end > len(set) {
			end = len(set)
		}
		page := set[start:end]

		info := map[string]interface{}{"hasNextPage": end < len(set)}
		if len(page) > 0 {
			info["endCursor"] = fmt.Sprintf("cur-%d", end-1)
		}
		require.NoError(f.t, json.NewEncoder(w).Encode(map[string]interface{}{
			"data":       map[string]interface{}{"people": page},
			"pageInfo":   info,
			"totalCount": len(set),
		}))
	}
}

func person(id string, deleted bool) map[string]interface{} {
	p := map[string]interface{}{
		"id":               id,
		"createdAt":        "2026-01-01T00:00:00.000Z",
		"updatedAt":        "2026-08-01T00:00:00.000Z",
		"deletedAt":        nil,
		"name":             map[string]interface{}{"firstName": "A", "lastName": "B"},
		"emails":           map[string]interface{}{"primaryEmail": "a@b.c"},
		"position":         1,
		"searchVector":     "'a':1",
		"marketingConsent": true,
		"companyId":        "c-1",
	}
	if deleted {
		p["deletedAt"] = "2026-08-10T00:00:00.000Z"
	}
	return p
}

func connectTo(t *testing.T, srv *httptest.Server, extra string) *Source {
	t.Helper()
	s := NewTwentySource()
	host := strings.TrimPrefix(srv.URL, "http://")
	uri := "twenty://" + host + "?api_key=k&scheme=http&rate_limit=1000" + extra
	require.NoError(t, s.Connect(context.Background(), uri))
	return s
}

func drain(t *testing.T, ch <-chan source.RecordBatchResult) (int, error) {
	t.Helper()
	rows := 0
	for res := range ch {
		if res.Err != nil {
			return rows, res.Err
		}
		if res.Batch != nil {
			rows += int(res.Batch.NumRows())
			res.Batch.Release()
		}
	}
	return rows, nil
}

func TestReadWalksCursorAndRunsSoftDeletePass(t *testing.T) {
	fake := &fakeTwenty{t: t}
	for i := 0; i < 5; i++ {
		fake.people = append(fake.people, person(fmt.Sprintf("p-%d", i), false))
	}
	fake.deleted = append(fake.deleted, person("p-gone", true))

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := connectTo(t, srv, "")
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: "people"})
	require.NoError(t, err)

	dyn := tbl.(*source.DynamicSourceTable)
	assert.Equal(t, []string{"id"}, dyn.TablePrimaryKeys)
	assert.Equal(t, updatedAtField, dyn.TableIncrementalKey)

	ch, err := dyn.ReadFn(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	rows, err := drain(t, ch)
	require.NoError(t, err)

	// 5 live (walked across 3 pages of 2) + 1 soft-deleted.
	assert.Equal(t, 6, rows)

	// depth=0 on every single request — the row shape must never depend on a
	// server-side default.
	for _, d := range fake.depthsSeen {
		assert.Equal(t, "0", d)
	}
	// The page size is pinned to the API cap, not left to the API's default of 20.
	for _, l := range fake.pageSizesSeen {
		assert.Equal(t, "200", l)
	}
	// The soft-delete pass really ran.
	assert.Contains(t, strings.Join(fake.filtersSeen, "|"), "deletedAt[is]:NOT_NULL")
}

func TestReadSkipsSoftDeletePassWhenDisabled(t *testing.T) {
	fake := &fakeTwenty{t: t}
	fake.people = append(fake.people, person("p-0", false))
	fake.deleted = append(fake.deleted, person("p-gone", true))

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := connectTo(t, srv, "&include_deleted=false")
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: "people"})
	require.NoError(t, err)

	ch, err := tbl.(*source.DynamicSourceTable).ReadFn(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	rows, err := drain(t, ch)
	require.NoError(t, err)
	assert.Equal(t, 1, rows)
	assert.NotContains(t, strings.Join(fake.filtersSeen, "|"), "deletedAt")
}

func TestReadFailsWhenServerReportsRowsButReturnsNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/metadata/objects" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data":     map[string]interface{}{"objects": []objectMeta{personMeta()}},
				"pageInfo": map[string]interface{}{"hasNextPage": false},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":       map[string]interface{}{"people": []interface{}{}},
			"pageInfo":   map[string]interface{}{"hasNextPage": false},
			"totalCount": 4321,
		})
	}))
	defer srv.Close()

	s := connectTo(t, srv, "")
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: "people"})
	require.NoError(t, err)
	ch, err := tbl.(*source.DynamicSourceTable).ReadFn(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	_, err = drain(t, ch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reported 4321 matching row(s) but the read returned none")
}

// A server that advertises another page without advancing the cursor must not
// spin until the page guard.
func TestReadRefusesRepeatedCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/metadata/objects" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data":     map[string]interface{}{"objects": []objectMeta{personMeta()}},
				"pageInfo": map[string]interface{}{"hasNextPage": false},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":       map[string]interface{}{"people": []interface{}{person("p-0", false)}},
			"pageInfo":   map[string]interface{}{"hasNextPage": true, "endCursor": "stuck"},
			"totalCount": 99,
		})
	}))
	defer srv.Close()

	s := connectTo(t, srv, "")
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: "people"})
	require.NoError(t, err)
	ch, err := tbl.(*source.DynamicSourceTable).ReadFn(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	_, err = drain(t, ch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same cursor twice")
}

func TestGetTableRequiresCustomPrefix(t *testing.T) {
	fake := &fakeTwenty{t: t}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := connectTo(t, srv, "")
	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "leads"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use 'custom:<object_name>'")
}

func TestGetTableResolvesCustomObject(t *testing.T) {
	leads := personMeta()
	leads.NameSingular = "lead"
	leads.NamePlural = "leads"
	fake := &fakeTwenty{t: t, objects: []objectMeta{leads}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := connectTo(t, srv, "")
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "custom:leads"})
	require.NoError(t, err)
	assert.Equal(t, "leads", table.Name())
}

func TestSchemesAndIncrementality(t *testing.T) {
	t.Parallel()
	s := NewTwentySource()
	assert.Equal(t, []string{"twenty"}, s.Schemes())
	assert.False(t, s.HandlesIncrementality())
}
