package peeringdb

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantKey string
		wantErr string
	}{
		{name: "anonymous", uri: "peeringdb://"},
		{name: "api key", uri: "peeringdb://?api_key=abc.123", wantKey: "abc.123"},
		{name: "wrong scheme", uri: "https://www.peeringdb.com", wantErr: "must start with peeringdb://"},
		{name: "host", uri: "peeringdb://net", wantErr: "use peeringdb://"},
		{name: "empty key", uri: "peeringdb://?api_key=", wantErr: "api_key is empty"},
		{name: "unknown param", uri: "peeringdb://?apikey=abc", wantErr: `unknown parameter "apikey"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), table)
	}
	for _, table := range []string{"", "net", "Networks", "network", "users", "networks:20", "networks?asn=1"} {
		assert.False(t, isValidTable(table), table)
	}
}

func TestFetchPageThrottling(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/short":
			if calls == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"data": [{"id": 1}]}`))
		case "/long":
			w.Header().Set("Retry-After", "3595")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer server.Close()
	s := &PeeringDBSource{client: httpclient.New(httpclient.WithBaseURL(server.URL), httpclient.WithDisableRetry())}

	resp, err := s.fetchPage(context.Background(), "/short", nil)
	require.NoError(t, err)
	assert.Len(t, resp.Data, 1)
	assert.Equal(t, 2, calls)

	calls = 0
	_, err = s.fetchPage(context.Background(), "/long", nil)
	require.ErrorContains(t, err, "throttled /long for 3595 seconds; set api_key")
	assert.Equal(t, 1, calls)
}

func idRange(from, to int) []int {
	ids := make([]int, 0, to-from+1)
	for id := from; id <= to; id++ {
		ids = append(ids, id)
	}
	return ids
}

// The fake API sorts odd ids before even ones, as PeeringDB sorts net by allow_ixp_update before id,
// and hides ids divisible by 5 after applying limit, as PeeringDB does for non-public contacts,
// so the test fails if paging assumes id order, trusts a short limited page, stops early, or repeats rows.
func TestReadPaginatesLiveAndDeletedRows(t *testing.T) {
	live := append(append(idRange(1, 2*maxPageSize+3), 50_000, 70_000), idRange(200_000, 200_000+3*maxPageSize)...)
	for id := 400_000; id <= 400_000+5*maxPageSize; id += 5 { // more than a page of hidden ids before a visible one
		live = append(live, id)
	}
	live = append(live, 500_002) // even, so the fake sorts it after every hidden id
	deleted := append([]int{3, 4, 90_000}, idRange(300_000, 300_000+maxPageSize+7)...)
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	var requests []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "/net", r.URL.Path)
		requests = append(requests, q)
		ids, status := live, "ok"
		if q.Get("status") == "deleted" && q.Get("since") != "" {
			ids, status = deleted, "deleted"
		}
		gt, _ := strconv.Atoi(q.Get("id__gt"))
		lte, err := strconv.Atoi(q.Get("id__lte"))
		if err != nil {
			lte = math.MaxInt
		}
		var matched []int
		for _, id := range ids {
			if id > gt && id <= lte {
				matched = append(matched, id)
			}
		}
		sort.Slice(matched, func(i, j int) bool {
			if matched[i]%2 != matched[j]%2 {
				return matched[i]%2 == 1
			}
			return matched[i] < matched[j]
		})
		if limit, err := strconv.Atoi(q.Get("limit")); err == nil {
			matched = matched[:min(limit, len(matched))]
		}
		rows := []map[string]interface{}{}
		for _, id := range matched {
			if id%5 != 0 {
				rows = append(rows, map[string]interface{}{"id": id, "status": status})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": rows, "meta": map[string]interface{}{}})
	}))
	defer server.Close()

	s := &PeeringDBSource{client: httpclient.New(httpclient.WithBaseURL(server.URL))}
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "networks"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, table.PrimaryKeys())
	assert.Equal(t, "updated", table.IncrementalKey())

	ch, err := table.Read(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end, MaxBatchBytes: 50_000})
	require.NoError(t, err)

	seen := map[string]int{}
	for res := range ch {
		require.NoError(t, res.Err)
		assert.Less(t, res.Batch.NumRows(), int64(maxPageSize))
		ids := res.Batch.Column(res.Batch.Schema().FieldIndices("id")[0]).(array.ExtensionArray).Storage()
		for i := 0; i < ids.Len(); i++ {
			seen[ids.ValueStr(i)]++
		}
		res.Batch.Release()
	}

	want := map[string]int{}
	for _, id := range append(slices.Clone(live), deleted...) {
		if id%5 != 0 {
			want[strconv.Itoa(id)]++
		}
	}
	require.Equal(t, 2, want["3"])
	require.Equal(t, want, seen)
	for _, q := range requests {
		assert.Equal(t, "2026-01-02T03:03:35Z", q.Get("updated__gte"))
		assert.Equal(t, "2026-02-01T00:00:00Z", q.Get("updated__lte"))
		if q.Get("status") == "deleted" {
			assert.Equal(t, strconv.FormatInt(start.Unix()-30, 10), q.Get("since"))
		} else {
			assert.Empty(t, q.Get("since"))
		}
	}
}
