package clevertap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func kolkata(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	return loc
}

func TestParseURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		uri     string
		want    clevertapCredentials
		wantErr string
	}{
		{
			name: "valid with explicit region",
			uri:  "clevertap://?account_id=TEST-ABC-123&passcode=SECRET&region=in1",
			want: clevertapCredentials{accountID: "TEST-ABC-123", passcode: "SECRET", region: "in1", timezone: time.UTC},
		},
		{
			name: "region and timezone default",
			uri:  "clevertap://?account_id=TEST-ABC-123&passcode=SECRET",
			want: clevertapCredentials{accountID: "TEST-ABC-123", passcode: "SECRET", region: "eu1", timezone: time.UTC},
		},
		{
			name: "explicit timezone",
			uri:  "clevertap://?account_id=A&passcode=B&timezone=Asia/Kolkata",
			want: clevertapCredentials{accountID: "A", passcode: "B", region: "eu1", timezone: kolkata(t)},
		},
		{
			name: "passcode with url-encoded characters",
			uri:  "clevertap://?account_id=A-B-C&passcode=p%2Fss%2Bword",
			want: clevertapCredentials{accountID: "A-B-C", passcode: "p/ss+word", region: "eu1", timezone: time.UTC},
		},
		{
			name:    "missing account_id",
			uri:     "clevertap://?passcode=SECRET",
			wantErr: "account_id is required",
		},
		{
			name:    "missing passcode",
			uri:     "clevertap://?account_id=TEST-ABC-123",
			wantErr: "passcode is required",
		},
		{
			name:    "no parameters at all",
			uri:     "clevertap://",
			wantErr: "account_id is required",
		},
		{
			name:    "wrong scheme",
			uri:     "clickup://?account_id=TEST-ABC-123&passcode=SECRET",
			wantErr: "must start with clevertap://",
		},
		{
			// The dashboard labels the default EU project "global", so a value
			// copied straight from it has to be accepted.
			name: "region global is accepted",
			uri:  "clevertap://?account_id=A&passcode=B&region=global",
			want: clevertapCredentials{accountID: "A", passcode: "B", region: "global", timezone: time.UTC},
		},
		{
			name:    "invalid region",
			uri:     "clevertap://?account_id=A&passcode=B&region=uk1",
			wantErr: `invalid region "uk1"`,
		},
		{
			name:    "invalid timezone",
			uri:     "clevertap://?account_id=A&passcode=B&timezone=Mars/Olympus",
			wantErr: `invalid timezone "Mars/Olympus"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRegionBaseURL(t *testing.T) {
	t.Parallel()

	// Every one of these hosts was confirmed to serve the API.
	require.Equal(t, "https://api.clevertap.com", regionBaseURL("eu1"))
	require.Equal(t, "https://api.clevertap.com", regionBaseURL("global"))
	require.Equal(t, "https://in1.api.clevertap.com", regionBaseURL("in1"))
	require.Equal(t, "https://us1.api.clevertap.com", regionBaseURL("us1"))
	require.Equal(t, "https://sg1.api.clevertap.com", regionBaseURL("sg1"))
	require.Equal(t, "https://aps3.api.clevertap.com", regionBaseURL("aps3"))
	require.Equal(t, "https://mec1.api.clevertap.com", regionBaseURL("mec1"))
}

func TestIsValidTable(t *testing.T) {
	t.Parallel()

	for _, table := range supportedTables {
		require.True(t, isValidTable(table), "expected %q to be supported", table)
	}

	for _, table := range []string{"", "Events", "EVENTS", "event", "unknown", "campaign"} {
		require.False(t, isValidTable(table), "expected %q to be unsupported", table)
	}
}

func TestParseTableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantTable string
		wantEvent []string
		wantErr   string
	}{
		{
			name:      "table without parameters",
			raw:       "campaigns",
			wantTable: "campaigns",
		},
		{
			name:      "event name parameter",
			raw:       "events?event_name=Charged",
			wantTable: "events",
			wantEvent: []string{"Charged"},
		},
		{
			name:      "several event names",
			raw:       "events?event_name=Charged,App Launched",
			wantTable: "events",
			wantEvent: []string{"Charged", "App Launched"},
		},
		{
			name:      "surrounding spaces are trimmed",
			raw:       "events?event_name=Charged , App Launched",
			wantTable: "events",
			wantEvent: []string{"Charged", "App Launched"},
		},
		{
			name:      "event name with encoded space",
			raw:       "events?event_name=App%20Launched",
			wantTable: "events",
			wantEvent: []string{"App Launched"},
		},
		{
			name:      "event name with plus as space",
			raw:       "profiles?event_name=App+Launched",
			wantTable: "profiles",
			wantEvent: []string{"App Launched"},
		},
		{
			name:    "unknown parameter is rejected",
			raw:     "events?event=Charged",
			wantErr: "unknown table parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table, params, err := parseTableName(tt.raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTable, table)
			require.Equal(t, tt.wantEvent, params.EventName)
		})
	}
}

func TestGetTableParameters(t *testing.T) {
	t.Parallel()

	s := NewCleverTapSource()

	// No table requires event_name any more: events and profiles fall back to
	// discovering every event from the schema endpoint.
	for _, table := range supportedTables {
		got, err := s.GetTable(t.Context(), source.TableRequest{Name: table})
		require.NoError(t, err, "table %s should not require parameters", table)
		require.Equal(t, table, got.Name())
	}

	_, err := s.GetTable(t.Context(), source.TableRequest{Name: "nope"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported table")
}

func TestTableStrategies(t *testing.T) {
	t.Parallel()

	s := NewCleverTapSource()

	tests := []struct {
		table       string
		primaryKeys []string
		incremental string
		strategy    config.IncrementalStrategy
	}{
		// No unique event id exists, so the loaded window is rewritten wholesale.
		{"events?event_name=X", nil, "ts", config.StrategyDeleteInsert},
		// CleverTap cannot report profile edits or deletions, so it is a full rebuild.
		{"profiles?event_name=X", []string{"object_id"}, "", config.StrategyReplace},
		// scheduled_on is not a change timestamp, so neither table loads incrementally.
		{"campaigns", []string{"id"}, "", config.StrategyReplace},
		{"campaign_reports", []string{"id"}, "", config.StrategyReplace},
		{"content_blocks", []string{"id"}, "updatedAt", config.StrategyMerge},
		{"message_reports", []string{"message_id"}, "", config.StrategyReplace},
		// Catalogs have no date dimension, so each run replaces the snapshot.
		{"event_schema", []string{"name"}, "", config.StrategyReplace},
		{"user_properties", []string{"name"}, "", config.StrategyReplace},
		// Subscription groups are keyed on their numeric id, not their name.
		{"category_groups", []string{"key"}, "", config.StrategyReplace},
		// events without an event_name fans out over every discovered event.
		{"events", nil, "ts", config.StrategyDeleteInsert},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()
			got, err := s.GetTable(t.Context(), source.TableRequest{Name: tt.table})
			require.NoError(t, err)
			require.Equal(t, tt.primaryKeys, got.PrimaryKeys())
			require.Equal(t, tt.incremental, got.IncrementalKey())
			require.Equal(t, tt.strategy, got.Strategy())
		})
	}
}

func TestIntervalBounds(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 3, 5, 13, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 6, 0, 0, 0, time.UTC)

	from, to := intervalBounds(source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, time.UTC)
	require.Equal(t, 20240305, yyyymmdd(from))
	require.Equal(t, 20240401, yyyymmdd(to))

	// With no interval the export still needs bounds: a fixed floor and today.
	from, to = intervalBounds(source.ReadOptions{}, time.UTC)
	require.Equal(t, 20130101, yyyymmdd(from))
	require.Equal(t, yyyymmdd(time.Now().UTC()), yyyymmdd(to))
}

func TestIntervalBoundsUsesProjectTimezone(t *testing.T) {
	t.Parallel()

	// 20:00Z on the 6th is already 01:30 on the 7th in Kolkata. CleverTap's days
	// are project-local, so the 7th has to be requested or those events are never
	// fetched while delete+insert still removes them from the destination.
	start := time.Date(2026, 8, 5, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 6, 20, 0, 0, 0, time.UTC)
	opts := source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}

	from, to := intervalBounds(opts, kolkata(t))
	require.Equal(t, 20260806, yyyymmdd(from))
	require.Equal(t, 20260807, yyyymmdd(to))

	// The same instants in UTC land on the previous day at both ends.
	from, to = intervalBounds(opts, time.UTC)
	require.Equal(t, 20260805, yyyymmdd(from))
	require.Equal(t, 20260806, yyyymmdd(to))

	// A negative offset shifts the other way.
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	midnightUTC := time.Date(2026, 8, 6, 0, 30, 0, 0, time.UTC)
	from, _ = intervalBounds(source.ReadOptions{IntervalStart: &midnightUTC}, ny)
	require.Equal(t, 20260805, yyyymmdd(from))
}

func TestForEachDateWindow(t *testing.T) {
	t.Parallel()

	collect := func(start, end time.Time, days int) [][2]int {
		var got [][2]int
		require.NoError(t, forEachDateWindow(context.Background(), start, end, days, func(from, to int) error {
			got = append(got, [2]int{from, to})
			return nil
		}))
		return got
	}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// The last window is clamped to the end rather than running past it.
	require.Equal(t, [][2]int{{20240101, 20240110}, {20240111, 20240115}},
		collect(start, start.AddDate(0, 0, 14), 10))

	// Windows are contiguous and never overlap, so nothing is fetched twice.
	windows := collect(start, start.AddDate(0, 0, 29), 10)
	require.Len(t, windows, 3)
	for i := 1; i < len(windows); i++ {
		require.Greater(t, windows[i][0], windows[i-1][1])
	}

	// A range shorter than one window still yields exactly one.
	require.Equal(t, [][2]int{{20240101, 20240101}}, collect(start, start, 365))

	// An end before the start yields nothing at all.
	require.Empty(t, collect(start, start.AddDate(0, 0, -1), 10))

	// Cancellation stops the walk instead of running every window.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, forEachDateWindow(ctx, start, start.AddDate(10, 0, 0), 1, func(int, int) error {
		t.Fatal("window ran after cancellation")
		return nil
	}), context.Canceled)
}

func TestDecodeCursor(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc/def+ghi", decodeCursor("abc%2Fdef%2Bghi"))
	require.Equal(t, "plaincursor", decodeCursor("plaincursor"))
	// An unescapable value is passed through rather than dropped.
	require.Equal(t, "bad%zz", decodeCursor("bad%zz"))
}

func TestLiftProfileObjectID(t *testing.T) {
	t.Parallel()

	t.Run("top level objectId wins", func(t *testing.T) {
		t.Parallel()
		item := liftProfileObjectID(map[string]interface{}{
			"objectId":     "top-level-id",
			"platformInfo": []interface{}{map[string]interface{}{"objectId": "device-id"}},
		})
		require.Equal(t, "top-level-id", item["object_id"])
	})

	t.Run("falls back to platformInfo", func(t *testing.T) {
		t.Parallel()
		item := liftProfileObjectID(map[string]interface{}{
			"platformInfo": []interface{}{
				map[string]interface{}{"platform": "iOS"},
				map[string]interface{}{"objectId": "device-id"},
			},
		})
		require.Equal(t, "device-id", item["object_id"])
	})

	t.Run("absent when nothing carries an id", func(t *testing.T) {
		t.Parallel()
		item := liftProfileObjectID(map[string]interface{}{"identity": "5555"})
		_, ok := item["object_id"]
		require.False(t, ok)
	})
}

func TestParseEventTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("json.Number parsed in UTC", func(t *testing.T) {
		t.Parallel()
		got, ok := parseEventTimestamp(json.Number("20260801072952"), time.UTC)
		require.True(t, ok)
		require.Equal(t, time.Date(2026, 8, 1, 7, 29, 52, 0, time.UTC), got.UTC())
	})

	t.Run("project timezone shifts the instant", func(t *testing.T) {
		t.Parallel()
		got, ok := parseEventTimestamp(json.Number("20260801072952"), kolkata(t))
		require.True(t, ok)
		// 07:29:52 +05:30 is 01:59:52Z.
		require.Equal(t, time.Date(2026, 8, 1, 1, 59, 52, 0, time.UTC), got.UTC())
	})

	t.Run("nil location falls back to UTC", func(t *testing.T) {
		t.Parallel()
		got, ok := parseEventTimestamp(json.Number("20260801072952"), nil)
		require.True(t, ok)
		require.Equal(t, time.Date(2026, 8, 1, 7, 29, 52, 0, time.UTC), got.UTC())
	})

	t.Run("string form is accepted", func(t *testing.T) {
		t.Parallel()
		_, ok := parseEventTimestamp("20260801072952", time.UTC)
		require.True(t, ok)
	})

	t.Run("unparseable values are rejected, not guessed", func(t *testing.T) {
		t.Parallel()
		for _, v := range []interface{}{nil, "", "not-a-timestamp", json.Number("123"), 42} {
			_, ok := parseEventTimestamp(v, time.UTC)
			require.False(t, ok, "expected %v to be rejected", v)
		}
	})
}

func TestLiftProfileObjectIDIsDeviceScoped(t *testing.T) {
	t.Parallel()

	// A person with two devices has two objectIds but only the first becomes the
	// key, which is why events must join to profiles on identity instead.
	item := liftProfileObjectID(map[string]interface{}{
		"identity": "bruin-user-1",
		"platformInfo": []interface{}{
			map[string]interface{}{"objectId": "device-a"},
			map[string]interface{}{"objectId": "device-b"},
		},
	})
	require.Equal(t, "device-a", item["object_id"])
	require.Equal(t, "bruin-user-1", item["identity"])
}

func TestWithinIntervalRequiresConvertedTimestamp(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	inside := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	require.True(t, withinInterval(inside, &start, &end))
	require.False(t, withinInterval(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), &start, &end))
	require.True(t, withinInterval(inside, nil, nil))

	// readEvents drops records whose ts never converted, because delete+insert
	// cannot place them in a window; withinInterval itself keeps non-time values.
	require.True(t, withinInterval(json.Number("20260815000000"), &start, &end))
}

func TestIsRequestInProgress(t *testing.T) {
	t.Parallel()

	// Exports run asynchronously, so an overlapping fetch is refused and retryable.
	require.True(t, isRequestInProgress("fail", "Request still in progress, please retry later"))
	require.False(t, isRequestInProgress("fail", "Export not allowed for this event"))
	require.False(t, isRequestInProgress("success", ""))
}

func TestHasNoResultYet(t *testing.T) {
	t.Parallel()

	// Observed live: CleverTap answers 500 here, not the documented 409.
	require.True(t, hasNoResultYet([]byte(`{"status":"fail","error":"This target has no result as of yet","code":500}`)))
	require.False(t, hasNoResultYet([]byte(`{"status":"fail","error":"Invalid campaign id","code":400}`)))
	require.False(t, hasNoResultYet([]byte(`{"status":"success","result":{"sent":82}}`)))
	require.False(t, hasNoResultYet([]byte(`not json`)))

	// A third variant seen live on a scheduled campaign: HTTP 200, status
	// "success", an error message and no result. The nil-result check skips it.
	var page struct {
		Status string                 `json:"status"`
		Result map[string]interface{} `json:"result"`
	}
	require.NoError(t, jsonUseNumber([]byte(`{"status":"success","error":"This target hasn't been completed","code":200}`), &page))
	require.Equal(t, "success", page.Status)
	require.Nil(t, page.Result)
}

func TestIsExportNotAllowed(t *testing.T) {
	t.Parallel()

	// CleverTap sends this as a 400, so it must be recognised from the body.
	require.True(t, isExportNotAllowed([]byte(`{"status":"fail","error":"Export not allowed for this event","code":400}`)))
	require.False(t, isExportNotAllowed([]byte(`{"status":"fail","error":"Incorrect Usage","code":3}`)))
	require.False(t, isExportNotAllowed([]byte(`{"status":"success","cursor":"abc"}`)))
	require.False(t, isExportNotAllowed([]byte(`not json`)))
}

func TestLiftMessageID(t *testing.T) {
	t.Parallel()

	t.Run("spaced field is copied to message_id", func(t *testing.T) {
		t.Parallel()
		item := liftMessageID(map[string]interface{}{
			"message id":   json.Number("1457432766"),
			"message_name": "Welcome",
		})
		require.Equal(t, json.Number("1457432766"), item["message_id"])
		// The original key is preserved so nothing is lost from the payload.
		require.Equal(t, json.Number("1457432766"), item["message id"])
	})

	t.Run("absent when the API omits the field", func(t *testing.T) {
		t.Parallel()
		item := liftMessageID(map[string]interface{}{"message_name": "Welcome"})
		_, ok := item["message_id"]
		require.False(t, ok)
	})
}

func TestJSONUseNumber(t *testing.T) {
	t.Parallel()

	var out map[string]interface{}
	err := jsonUseNumber([]byte(`{"id":1457432766123456789,"rate":1.5,"name":"x"}`), &out)
	require.NoError(t, err)

	require.Equal(t, json.Number("1457432766123456789"), out["id"])
	require.Equal(t, json.Number("1.5"), out["rate"])
	require.Equal(t, "x", out["name"])

	require.Error(t, jsonUseNumber([]byte(`{"id":`), &out))
}

func TestCleverTapByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			// Step 1: open the cursor.
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"cursor": "c1",
			})
			return
		}
		// Step 2: one page of records within the interval, then terminate
		// (empty next_cursor).
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{
				"ts":   "20260102120000",
				"name": wide,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "success",
			"next_cursor": "",
			"records":     rows,
		})
	}))
	defer srv.Close()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	run := func(max int64) (int64, int64) {
		s := &CleverTapSource{
			client:   httpclient.New(httpclient.WithBaseURL(srv.URL)),
			timezone: time.UTC,
		}
		params := clevertapParams{EventName: []string{"evt1"}}
		results, err := s.read(context.Background(), "events", params, source.ReadOptions{
			MaxBatchBytes: max,
			IntervalStart: &start,
			IntervalEnd:   &end,
		})
		if err != nil {
			t.Fatal(err)
		}
		var b, rw int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			b++
			rw += res.Batch.NumRows()
			res.Batch.Release()
		}
		return b, rw
	}

	offB, offR := run(0)
	onB, onR := run(4096)
	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d", offR, onR)
	}
}
