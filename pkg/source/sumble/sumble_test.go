package sumble

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantError string
	}{
		{name: "valid", uri: "sumble://?api_key=secret", wantKey: "secret"},
		{name: "valid with host", uri: "sumble://default?api_key=secret", wantKey: "secret"},
		{name: "wrong scheme", uri: "https://?api_key=secret", wantError: "must start with sumble://"},
		{name: "missing API key", uri: "sumble://", wantError: "api_key query parameter is required"},
		{name: "empty API key", uri: "sumble://?api_key=", wantError: "api_key query parameter is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiKey, err := parseURI(test.uri)
			if test.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantKey, apiKey)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("unknown"))
	assert.False(t, isValidTable("Signals"))
}

func TestGetTableMetadata(t *testing.T) {
	tests := []struct {
		name           string
		primaryKeys    []string
		incrementalKey string
		strategy       config.IncrementalStrategy
	}{
		{name: "organization_lists", primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
		{name: "organization_list_organizations", primaryKeys: []string{"_ingestr_id"}, strategy: config.StrategyReplace},
		{name: "contact_lists", primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
		{name: "contact_list_people", primaryKeys: []string{"_ingestr_id"}, strategy: config.StrategyReplace},
		{name: "signals", primaryKeys: []string{"_ingestr_id"}, incrementalKey: "date", strategy: config.StrategyMerge},
		{name: "priority_signals", primaryKeys: []string{"id"}, incrementalKey: "date", strategy: config.StrategyMerge},
		{name: "signal_configs", primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	}

	connector := NewSumbleSource()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table, err := connector.GetTable(context.Background(), source.TableRequest{Name: test.name})
			require.NoError(t, err)
			assert.Equal(t, test.name, table.Name())
			assert.Equal(t, test.primaryKeys, table.PrimaryKeys())
			assert.Equal(t, test.incrementalKey, table.IncrementalKey())
			assert.Equal(t, test.strategy, table.Strategy())
			assert.False(t, table.HasKnownSchema())
		})
	}
}

func TestParseTableSpec(t *testing.T) {
	t.Run("plain table", func(t *testing.T) {
		spec, err := parseTableSpec("organization_lists")
		require.NoError(t, err)
		assert.Equal(t, "organization_lists", spec.table)
		assert.Empty(t, spec.listIDs)
		assert.Empty(t, spec.filter)
	})

	t.Run("list IDs", func(t *testing.T) {
		spec, err := parseTableSpec("organization_list_organizations?list_ids=12,34")
		require.NoError(t, err)
		assert.Equal(t, []int64{12, 34}, spec.listIDs)
	})

	t.Run("signal filters", func(t *testing.T) {
		spec, err := parseTableSpec("signals?organization_ids=12,34&technology_slugs=kubernetes&priorities=high,low")
		require.NoError(t, err)
		assert.Equal(t, []int64{12, 34}, spec.filter["organization_ids"])
		assert.Equal(t, []string{"kubernetes"}, spec.filter["technology_slugs"])
		assert.Equal(t, []string{"high", "low"}, spec.filter["priorities"])
	})

	t.Run("false relevance filter", func(t *testing.T) {
		spec, err := parseTableSpec("priority_signals?is_relevant=false")
		require.NoError(t, err)
		assert.Equal(t, false, spec.filter["is_relevant"])
	})

	t.Run("unknown parameter", func(t *testing.T) {
		_, err := parseTableSpec("signals?organisation_ids=12")
		require.ErrorContains(t, err, "unknown table parameter")
	})

	t.Run("parameter rejected for table", func(t *testing.T) {
		_, err := parseTableSpec("organization_lists?list_ids=12")
		require.ErrorContains(t, err, "does not accept the list_ids parameter")
	})

	t.Run("invalid ID", func(t *testing.T) {
		_, err := parseTableSpec("contact_list_people?list_ids=not-a-number")
		require.ErrorContains(t, err, "positive integer IDs")
	})

	t.Run("invalid priority", func(t *testing.T) {
		_, err := parseTableSpec("signals?priorities=urgent")
		require.ErrorContains(t, err, "high, medium, or low")
	})

	t.Run("mutually exclusive signal config filters", func(t *testing.T) {
		_, err := parseTableSpec("signal_configs?signal_config_ids=12&priorities=high")
		require.ErrorContains(t, err, "cannot be combined")
	})

	t.Run("empty parameter value", func(t *testing.T) {
		_, err := parseTableSpec("priority_signals?organization_ids=5&is_relevant=")
		require.ErrorContains(t, err, "is_relevant parameter must not be empty")
	})

	t.Run("separator-only parameter value", func(t *testing.T) {
		_, err := parseTableSpec("organization_list_organizations?list_ids=,")
		require.ErrorContains(t, err, "list_ids parameter must not be empty")
	})

	t.Run("empty value for a rejected parameter", func(t *testing.T) {
		_, err := parseTableSpec("organization_lists?list_ids=")
		require.ErrorContains(t, err, "does not accept the list_ids parameter")
	})
}

func TestJsonUseNumber(t *testing.T) {
	var result map[string]any
	require.NoError(t, jsonUseNumber([]byte(`{"id":9007199254740993,"score":3.14}`), &result))

	id, ok := result["id"].(json.Number)
	require.True(t, ok)
	assert.Equal(t, "9007199254740993", id.String())
	assert.IsType(t, json.Number(""), result["score"])
}

func TestFilterItemsByInterval(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	items := []map[string]any{
		{"id": 1, "date": "2026-07-31T23:59:59Z"},
		{"id": 2, "date": "2026-08-01T00:00:00Z"},
		{"id": 3, "date": "2026-08-02"},
		{"id": 4, "date": "2026-08-03T00:00:00Z"},
		{"id": 5},
	}

	filtered := filterItemsByInterval(items, "date", &start, &end)
	require.Len(t, filtered, 3)
	assert.Equal(t, 2, filtered[0]["id"])
	assert.Equal(t, 3, filtered[1]["id"])
	assert.Equal(t, 5, filtered[2]["id"])
}

func TestFilterItemsByIntervalKeepsDateOnlyWithinSubDayInterval(t *testing.T) {
	start := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC)
	items := []map[string]any{
		{"id": 1, "date": "2026-08-01"},
		{"id": 2, "date": "2026-08-02"},
		{"id": 3, "date": "2026-08-03"},
		{"id": 4, "date": "2026-08-04"},
	}

	filtered := filterItemsByInterval(items, "date", &start, &end)
	require.Len(t, filtered, 2)
	assert.Equal(t, 2, filtered[0]["id"])
	assert.Equal(t, 3, filtered[1]["id"])
}

func TestAddSignalPrimaryKeys(t *testing.T) {
	items := []map[string]any{
		{"signal_id": json.Number("123"), "title": "with ID"},
		{"title": "without ID", "date": "2026-08-01T00:00:00Z"},
	}
	require.NoError(t, addSignalPrimaryKeys(items))
	assert.Equal(t, "signal:123", items[0]["_ingestr_id"])

	generated := items[1]["_ingestr_id"]
	assert.Regexp(t, `^hash:[0-9a-f]{64}$`, generated)
	delete(items[1], "_ingestr_id")
	require.NoError(t, addSignalPrimaryKeys(items[1:]))
	assert.Equal(t, generated, items[1]["_ingestr_id"])
}

func TestAddScopedPrimaryKeys(t *testing.T) {
	items := []map[string]any{
		{"id": json.Number("123"), "name": "matched"},
		{"name": "unmatched"},
	}
	require.NoError(t, addScopedPrimaryKeys(items, 42))
	assert.Equal(t, "42:id:123", items[0]["_ingestr_id"])
	assert.Regexp(t, `^42:hash:[0-9a-f]{64}$`, items[1]["_ingestr_id"])
}

func testSumbleClient(baseURL string) *ingestrhttp.Client {
	return ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithDisableRetry(),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth("test-key")),
		ingestrhttp.WithHeaders(map[string]string{"Accept": "application/json", "Content-Type": "application/json"}),
	)
}

// Sumble is schema-less, so every column arrives as a JSON-encoded unknown
// extension array.
func columnValue(t *testing.T, batch arrow.RecordBatch, name string, row int) string {
	t.Helper()
	indices := batch.Schema().FieldIndices(name)
	require.Len(t, indices, 1, "column %q not found", name)

	column, ok := batch.Column(indices[0]).(array.ExtensionArray)
	require.True(t, ok, "column %q is not an extension array", name)
	raw := column.Storage().(*array.String).Value(row)

	var decoded string
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return decoded
	}
	return raw
}

func drain(results <-chan source.RecordBatchResult) ([]arrow.RecordBatch, error) {
	var batches []arrow.RecordBatch
	var err error
	for result := range results {
		if result.Err != nil && err == nil {
			err = result.Err
		}
		if result.Batch != nil {
			batches = append(batches, result.Batch)
		}
	}
	return batches, err
}

// A failing list member fetch must surface the API error rather than racing the
// close of the results channel with the still-running workers.
func TestReadListMembersSurfacesWorkerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/organization-lists" {
			_, _ = w.Write([]byte(`{"organization_lists":[{"id":1},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8}]}`))
			return
		}
		if request.URL.Path == "/organization-lists/1" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"list_info":{"id":9,"name":"list"},"organizations":[{"id":100,"name":"acme"}]}`))
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	for i := 0; i < 20; i++ {
		results, err := connector.read(context.Background(), sumbleReadSpec{table: "organization_list_organizations"}, source.ReadOptions{})
		require.NoError(t, err)
		batches, readErr := drain(results)
		for _, batch := range batches {
			batch.Release()
		}
		require.ErrorContains(t, readErr, "500")
	}
}

func TestReadListMembersScopesRowsToTheirList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "true", request.URL.Query().Get("include_deleted"))
		_, _ = w.Write([]byte(`{"list_info":{"id":7,"name":"seven"},"organizations":[{"id":100,"name":"acme"}]}`))
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	results, err := connector.read(context.Background(), sumbleReadSpec{table: "organization_list_organizations", listIDs: []int64{7}}, source.ReadOptions{})
	require.NoError(t, err)
	batches, readErr := drain(results)
	require.NoError(t, readErr)
	require.Len(t, batches, 1)
	defer batches[0].Release()

	require.EqualValues(t, 1, batches[0].NumRows())
	assert.Equal(t, "7:id:100", columnValue(t, batches[0], "_ingestr_id", 0))
	assert.Equal(t, "7", columnValue(t, batches[0], "organization_list_id", 0))
	assert.Contains(t, columnValue(t, batches[0], "organization_list", 0), `"name":"seven"`)
}

func TestPaginateStopsOnShortPage(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/signals", request.URL.Path)

		var body map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		assert.NoError(t, decoder.Decode(&body))

		w.Header().Set("Content-Type", "application/json")
		switch body["offset"].(json.Number).String() {
		case "0":
			_, _ = w.Write([]byte(`{"signals":[{"signal_id":1,"date":"2026-08-01T00:00:00Z"},{"signal_id":2,"date":"2026-08-02T00:00:00Z"}]}`))
		default:
			_, _ = w.Write([]byte(`{"signals":[{"signal_id":3,"date":"2026-08-03T00:00:00Z"}]}`))
		}
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	results, err := connector.read(context.Background(), sumbleReadSpec{table: "signals"}, source.ReadOptions{PageSize: 2})
	require.NoError(t, err)
	batches, readErr := drain(results)
	require.NoError(t, readErr)
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()

	assert.EqualValues(t, 2, requests.Load())
	total := 0
	for _, batch := range batches {
		total += int(batch.NumRows())
	}
	assert.Equal(t, 3, total)
	assert.Equal(t, "signal:1", columnValue(t, batches[0], "_ingestr_id", 0))
}

// /signals is documented as newest-first, so paging stops once a page falls
// entirely before the interval start instead of walking the 60-day window.
func TestSignalsPaginationStopsAtIntervalStart(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)

		var body map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		assert.NoError(t, decoder.Decode(&body))

		w.Header().Set("Content-Type", "application/json")
		switch body["offset"].(json.Number).String() {
		case "0":
			_, _ = w.Write([]byte(`{"signals":[{"signal_id":1,"date":"2026-08-10T00:00:00Z"},{"signal_id":2,"date":"2026-08-09T00:00:00Z"}]}`))
		default:
			_, _ = w.Write([]byte(`{"signals":[{"signal_id":3,"date":"2026-07-01T00:00:00Z"},{"signal_id":4,"date":"2026-06-30T00:00:00Z"}]}`))
		}
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	results, err := connector.read(context.Background(), sumbleReadSpec{table: "signals"}, source.ReadOptions{PageSize: 2, IntervalStart: &start})
	require.NoError(t, err)
	batches, readErr := drain(results)
	require.NoError(t, readErr)
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()

	assert.EqualValues(t, 2, requests.Load())
	total := 0
	for _, batch := range batches {
		total += int(batch.NumRows())
	}
	assert.Equal(t, 2, total)
}

// Sumble caps offset at 10,000, so the last page starts there and paging stops.
func TestPaginationStopsAtOffsetLimit(t *testing.T) {
	var requests atomic.Int32
	lastOffset := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)

		var body map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		assert.NoError(t, decoder.Decode(&body))
		lastOffset = body["offset"].(json.Number).String()

		rows := make([]string, 0, maxPageSize)
		for i := 0; i < maxPageSize; i++ {
			rows = append(rows, `{"id":1}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"priority_signals":[` + strings.Join(rows, ",") + `]}`))
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	results, err := connector.read(context.Background(), sumbleReadSpec{table: "priority_signals"}, source.ReadOptions{})
	require.NoError(t, err)
	batches, readErr := drain(results)
	for _, batch := range batches {
		batch.Release()
	}
	require.NoError(t, readErr)

	assert.EqualValues(t, maxOffset/maxPageSize+1, requests.Load())
	assert.Equal(t, strconv.Itoa(maxOffset), lastOffset)
}

func TestPaginationHonorsRowLimit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"signals":[{"signal_id":1,"date":"2026-08-01T00:00:00Z"},{"signal_id":2,"date":"2026-08-01T00:00:00Z"}]}`))
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	results, err := connector.read(context.Background(), sumbleReadSpec{table: "signals"}, source.ReadOptions{PageSize: 2, Limit: 3})
	require.NoError(t, err)
	batches, readErr := drain(results)
	require.NoError(t, readErr)
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()

	total := 0
	for _, batch := range batches {
		total += int(batch.NumRows())
	}
	assert.Equal(t, 3, total)
	assert.EqualValues(t, 2, requests.Load())
}

func TestRowLimiterSharesBudgetAcrossWorkers(t *testing.T) {
	limiter := newRowLimiter(10)
	var group errgroup.Group
	for i := 0; i < 8; i++ {
		group.Go(func() error {
			for j := 0; j < 5; j++ {
				limiter.take(3)
			}
			return nil
		})
	}
	require.NoError(t, group.Wait())

	assert.EqualValues(t, 10, limiter.used.Load())
	assert.True(t, limiter.exhausted())
	assert.Equal(t, 0, limiter.take(1))

	unlimited := newRowLimiter(0)
	assert.Equal(t, 7, unlimited.take(7))
	assert.False(t, unlimited.exhausted())
}

// A row limit serializes the list fan-out so no worker spends a request the
// budget would immediately discard.
func TestListMembersStopFetchingAtRowLimit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"list_info":{"id":1},"people":[{"id":1},{"id":2}]}`))
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	spec := sumbleReadSpec{table: "contact_list_people", listIDs: []int64{1, 2, 3, 4, 5}}
	results, err := connector.read(context.Background(), spec, source.ReadOptions{Limit: 3})
	require.NoError(t, err)
	batches, readErr := drain(results)
	require.NoError(t, readErr)
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()

	total := 0
	for _, batch := range batches {
		total += int(batch.NumRows())
	}
	assert.Equal(t, 3, total)
	assert.EqualValues(t, 2, requests.Load())
}

// Without interval filtering every fetched row counts against the budget, so
// the last page asks for exactly the rows that are still needed.
func TestPaginationClampsPageToRowLimit(t *testing.T) {
	var limits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		assert.NoError(t, decoder.Decode(&body))
		limits = append(limits, body["limit"].(json.Number).String())

		rows := make([]string, 0)
		for i := 0; i < 2; i++ {
			rows = append(rows, `{"signal_id":1,"date":"2026-08-01T00:00:00Z"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"signals":[` + strings.Join(rows, ",") + `]}`))
	}))
	defer server.Close()

	connector := NewSumbleSource()
	connector.client = testSumbleClient(server.URL)

	results, err := connector.read(context.Background(), sumbleReadSpec{table: "signals"}, source.ReadOptions{PageSize: 2, Limit: 3})
	require.NoError(t, err)
	batches, readErr := drain(results)
	require.NoError(t, readErr)
	for _, batch := range batches {
		batch.Release()
	}

	assert.Equal(t, []string{"2", "1"}, limits)
}
