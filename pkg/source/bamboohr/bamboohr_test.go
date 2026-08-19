package bamboohr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantDomain string
		wantKey    string
		wantToken  string
		wantZone   string
		wantError  string
	}{
		{
			name:       "company subdomain",
			uri:        "bamboohr://acme?api_key=secret",
			wantDomain: "acme",
			wantKey:    "secret",
		},
		{
			name:       "full company host",
			uri:        "bamboohr://Example-Org.bamboohr.com?api_key=key%2Fwith%2Fslashes",
			wantDomain: "example-org",
			wantKey:    "key/with/slashes",
		},
		{
			name:       "oauth token and company timezone",
			uri:        "bamboohr://acme?access_token=oauth-token&timezone=America%2FDenver",
			wantDomain: "acme",
			wantToken:  "oauth-token",
			wantZone:   "America/Denver",
		},
		{name: "wrong scheme", uri: "https://acme.bamboohr.com", wantError: "must start with bamboohr://"},
		{name: "missing company domain", uri: "bamboohr://?api_key=secret", wantError: "company domain is required"},
		{name: "invalid company domain", uri: "bamboohr://bad_domain?api_key=secret", wantError: "invalid BambooHR company domain"},
		{name: "missing credentials", uri: "bamboohr://acme", wantError: "api_key or access_token"},
		{name: "multiple credential types", uri: "bamboohr://acme?api_key=secret&access_token=token", wantError: "mutually exclusive"},
		{name: "invalid timezone", uri: "bamboohr://acme?api_key=secret&timezone=Mars%2FOlympus", wantError: "invalid BambooHR company timezone"},
		{name: "unknown parameter", uri: "bamboohr://acme?api_key=secret&region=us", wantError: "unknown bamboohr URI parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseURI(tt.uri)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDomain, creds.companyDomain)
			assert.Equal(t, tt.wantKey, creds.apiKey)
			assert.Equal(t, tt.wantToken, creds.accessToken)
			if tt.wantZone == "" {
				assert.Nil(t, creds.timezone)
			} else {
				require.NotNil(t, creds.timezone)
				assert.Equal(t, tt.wantZone, creds.timezone.String())
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.Truef(t, isValidTable(table), "expected %s to be valid", table)
	}
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Employees"))
	assert.False(t, isValidTable("goals"))
}

func TestGetTableMetadataAndParameters(t *testing.T) {
	src := NewBambooHRSource()

	employees, err := src.GetTable(context.Background(), source.TableRequest{
		Name: "employees?fields=workEmail,hireDate&fields=departmentName",
	})
	require.NoError(t, err)
	assert.Equal(t, "employees", employees.Name())
	assert.Equal(t, []string{"employeeId"}, employees.PrimaryKeys())
	assert.Empty(t, employees.IncrementalKey())
	assert.Equal(t, config.StrategyReplace, employees.Strategy())
	assert.False(t, employees.HasKnownSchema())

	timeOff, err := src.GetTable(context.Background(), source.TableRequest{Name: "time_off_requests"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, timeOff.PrimaryKeys())
	assert.Equal(t, "start", timeOff.IncrementalKey())
	assert.Equal(t, config.StrategyMerge, timeOff.Strategy())

	timesheets, err := src.GetTable(context.Background(), source.TableRequest{Name: "timesheet_entries"})
	require.NoError(t, err)
	assert.Equal(t, "date", timesheets.IncrementalKey())
	assert.Equal(t, config.StrategyMerge, timesheets.Strategy())

	_, err = src.GetTable(context.Background(), source.TableRequest{Name: "users?fields=email"})
	require.ErrorContains(t, err, "only supported for employees")
	_, err = src.GetTable(context.Background(), source.TableRequest{Name: "employees?field=workEmail"})
	require.ErrorContains(t, err, "unknown table parameter")
	_, err = src.GetTable(context.Background(), source.TableRequest{Name: "goals"})
	require.ErrorContains(t, err, "unsupported table")
}

func TestEmployeesUsesBasicAuthFieldsAndCursorPagination(t *testing.T) {
	var mu sync.Mutex
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "test-key", username)
		assert.Equal(t, "x", password)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		assert.Equal(t, "/api/v1/employees", r.URL.Path)
		assert.Equal(t, "2500", r.URL.Query().Get("page[limit]"))
		assert.Equal(t, "workEmail,hireDate,departmentName", r.URL.Query().Get("fields"))

		cursor := r.URL.Query().Get("page[after]")
		mu.Lock()
		cursors = append(cursors, cursor)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if cursor == "" {
			_, _ = fmt.Fprint(w, `{"data":[{"employeeId":"101","firstName":"Ada","lastName":"Lovelace","teams":[{"id":7,"name":"Platform"}]}],"meta":{"total":2,"page":{"limit":2500,"nextCursor":"next-page","prevCursor":null}},"_links":{}}`)
			return
		}
		require.Equal(t, "next-page", cursor)
		_, _ = fmt.Fprint(w, `{"data":[{"employeeId":"102","firstName":"Grace","lastName":"Hopper"}],"meta":{"total":2,"page":{"limit":2500,"nextCursor":null,"prevCursor":"previous"}},"_links":{}}`)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{
		Name: "employees?fields=workEmail,hireDate,departmentName",
	})
	require.NoError(t, err)

	rows, batches := drainTable(t, table, source.ReadOptions{})
	assert.EqualValues(t, 2, rows)
	assert.Equal(t, 2, batches)
	assert.Equal(t, []string{"", "next-page"}, cursors)
}

func TestOAuthBearerAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer oauth-token", r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	client := newHTTPClient(
		server.URL,
		httpclient.NewBearerAuth("oauth-token"),
		httpclient.WithRateLimiter(1000, 1000),
		httpclient.WithDisableRetry(),
	)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	src := &BambooHRSource{client: client, now: time.Now}

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "employee_fields"})
	require.NoError(t, err)
	rows, batches := drainTable(t, table, source.ReadOptions{})
	assert.Zero(t, rows)
	assert.Zero(t, batches)
}

func TestEmployeeDirectoryIncludesFutureEmployees(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/employees/directory", r.URL.Path)
		assert.Equal(t, "false", r.URL.Query().Get("onlyCurrent"))
		_, _ = fmt.Fprint(w, `{"fields":[{"id":"displayName","type":"text","name":"Display Name"}],"employees":[{"id":"101","displayName":"Ada Lovelace","supervisor":{"id":"99","displayName":"Charles Babbage"}}]}`)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "employee_directory"})
	require.NoError(t, err)
	rows, batches := drainTable(t, table, source.ReadOptions{})
	assert.EqualValues(t, 1, rows)
	assert.Equal(t, 1, batches)
}

func TestEmployeeDirectoryTreatsEmptyDirectory404AsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "employee_directory"})
	require.NoError(t, err)
	rows, batches := drainTable(t, table, source.ReadOptions{})
	assert.Zero(t, rows)
	assert.Zero(t, batches)
}

func TestMetadataAndTimeOffReferenceTablesUseFakeData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/meta/fields":
			_, _ = fmt.Fprint(w, `[{"id":1,"name":"First Name","type":"text","alias":"firstName"},{"id":"4340.4","name":"Custom Skill","type":"list","alias":"customSkill"}]`)
		case "/api/v1/meta/users":
			_, _ = fmt.Fprint(w, `{"9007199254740993":{"employeeId":101,"firstName":"Ada","lastName":"Lovelace","email":"ada@example.test","status":"enabled"}}`)
		case "/api/v1/meta/time_off/types":
			_, _ = fmt.Fprint(w, `{"timeOffTypes":[{"id":"64","name":"Vacation","units":"days","color":"56afdd","icon":"airplane","source":"internal"}],"defaultHours":[{"name":"Monday","amount":"8"},{"name":"Friday","amount":"8"}]}`)
		case "/api/v1/meta/time_off/policies":
			_, _ = fmt.Fprint(w, `[{"id":7,"timeOffTypeId":64,"name":"Standard Vacation","effectiveDate":null,"type":"accruing"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, tt := range []struct {
		table string
		rows  int64
	}{
		{table: "employee_fields", rows: 2},
		{table: "users", rows: 1},
		{table: "time_off_types", rows: 1},
		{table: "time_off_default_hours", rows: 2},
		{table: "time_off_policies", rows: 1},
	} {
		t.Run(tt.table, func(t *testing.T) {
			src := newTestSource(t, server.URL)
			table, err := src.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			require.NoError(t, err)
			rows, _ := drainTable(t, table, source.ReadOptions{})
			assert.Equal(t, tt.rows, rows)
		})
	}
}

func TestLocationsFetchesActiveArchivedAndAllPages(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer oauth-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/v1/hris/org/locations", r.URL.Path)
		assert.Equal(t, "500", r.URL.Query().Get("pageSize"))
		assert.Equal(t, "state,country", r.URL.Query().Get("expand"))
		filter := r.URL.Query().Get("filter")
		page := r.URL.Query().Get("page")
		requests = append(requests, filter+":"+page)

		w.Header().Set("Content-Type", "application/json")
		switch filter + ":" + page {
		case "archived eq false:0":
			_, _ = fmt.Fprint(w, `{"data":[{"id":1,"label":"London","archived":false,"address":{"city":"London","country":{"id":44,"name":"United Kingdom"}}}],"meta":{"page":0,"pageSize":500,"totalPages":2,"totalItems":2}}`)
		case "archived eq false:1":
			_, _ = fmt.Fprint(w, `{"data":[{"id":2,"label":"Remote","archived":false,"address":{"remoteLocation":true}}],"meta":{"page":1,"pageSize":500,"totalPages":2,"totalItems":2}}`)
		case "archived eq true:0":
			_, _ = fmt.Fprint(w, `{"data":[{"id":3,"label":"Old Office","archived":true,"archivedAt":"2025-01-01T00:00:00Z"}],"meta":{"page":0,"pageSize":500,"totalPages":1,"totalItems":1}}`)
		default:
			t.Fatalf("unexpected locations request %s:%s", filter, page)
		}
	}))
	defer server.Close()

	src := newOAuthTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "locations"})
	require.NoError(t, err)
	rows, batches := drainTable(t, table, source.ReadOptions{})
	assert.EqualValues(t, 3, rows)
	assert.Equal(t, 3, batches)
	assert.Equal(t, []string{"archived eq false:0", "archived eq false:1", "archived eq true:0"}, requests)
}

func TestLocationsRejectsAPIKeyAuthentication(t *testing.T) {
	src := NewBambooHRSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "locations"})
	require.ErrorContains(t, err, "requires access_token authentication")
}

func TestTimeOffRequestsUsesInclusiveOverlapWindow(t *testing.T) {
	start := time.Date(2026, time.January, 10, 15, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.February, 5, 22, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/time_off/requests", r.URL.Path)
		assert.Equal(t, "2026-01-10", r.URL.Query().Get("start"))
		assert.Equal(t, "2026-02-05", r.URL.Query().Get("end"))
		_, _ = fmt.Fprint(w, `[{"id":"1348","employeeId":"101","name":"Ada Lovelace","start":"2026-02-01","end":"2026-02-03","created":"2026-01-10","status":{"lastChanged":"2026-01-11","status":"approved"},"type":{"id":"64","name":"Vacation"},"amount":{"unit":"days","amount":3},"dates":{"2026-02-01":1}}]`)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "time_off_requests"})
	require.NoError(t, err)
	rows, _ := drainTable(t, table, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	assert.EqualValues(t, 1, rows)
}

func TestTimeOffRequestsWithoutIntervalRequestsFullRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1900-01-01", r.URL.Query().Get("start"))
		assert.Equal(t, "9999-12-31", r.URL.Query().Get("end"))
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "time_off_requests"})
	require.NoError(t, err)
	rows, batches := drainTable(t, table, source.ReadOptions{})
	assert.Zero(t, rows)
	assert.Zero(t, batches)
}

func TestTimesheetEntriesDefaultsToAvailable365DayWindow(t *testing.T) {
	now := time.Date(2026, time.August, 18, 20, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/time_tracking/timesheet_entries", r.URL.Path)
		assert.Equal(t, "2025-08-19", r.URL.Query().Get("start"))
		assert.Equal(t, "2026-08-18", r.URL.Query().Get("end"))
		_, _ = fmt.Fprint(w, `[{"id":9007199254740993,"employeeId":101,"type":"clock","date":"2026-08-18","start":"2026-08-18T08:00:00+00:00","end":"2026-08-18T16:00:00+00:00","hours":8,"projectInfo":{"project":{"id":1,"name":"Connector"}},"approved":true,"createdAt":"2026-08-18T08:00:00+00:00","updatedAt":"2026-08-18T16:01:00+00:00"}]`)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	src.now = func() time.Time { return now }
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "timesheet_entries"})
	require.NoError(t, err)
	rows, _ := drainTable(t, table, source.ReadOptions{})
	assert.EqualValues(t, 1, rows)
}

func TestTimesheetEntriesRequiresCompanyTimezone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("timesheet request should not be sent without a company timezone")
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	src.companyTimezone = nil
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "timesheet_entries"})
	require.NoError(t, err)
	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.ErrorContains(t, result.Err, "timezone is required")
}

func TestTimesheetWindowValidation(t *testing.T) {
	now := time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC)
	tooOld := time.Date(2025, time.August, 18, 0, 0, 0, 0, time.UTC)
	future := now.AddDate(0, 0, 1)
	start := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	beforeStart := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)

	_, _, err := timesheetWindow(source.ReadOptions{IntervalStart: &tooOld}, now, time.UTC)
	require.ErrorContains(t, err, "last 365 days")
	_, _, err = timesheetWindow(source.ReadOptions{IntervalEnd: &future}, now, time.UTC)
	require.ErrorContains(t, err, "future")
	_, _, err = timesheetWindow(source.ReadOptions{IntervalStart: &start, IntervalEnd: &beforeStart}, now, time.UTC)
	require.ErrorContains(t, err, "start date must not be after end date")
}

func TestTimesheetWindowUsesCompanyTimezoneForToday(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	require.NoError(t, err)
	now := time.Date(2026, time.August, 19, 3, 0, 0, 0, time.UTC)

	start, end, err := timesheetWindow(source.ReadOptions{}, now, denver)
	require.NoError(t, err)
	assert.Equal(t, "2025-08-19", start)
	assert.Equal(t, "2026-08-18", end)
}

func TestTimesheetWindowUsesCompanyTimezoneForExplicitBounds(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	require.NoError(t, err)
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	startBoundary := time.Date(2026, time.August, 19, 1, 0, 0, 0, time.UTC)
	endBoundary := time.Date(2026, time.August, 20, 1, 0, 0, 0, time.UTC)

	start, end, err := timesheetWindow(source.ReadOptions{
		IntervalStart: &startBoundary,
		IntervalEnd:   &endBoundary,
	}, now, denver)
	require.NoError(t, err)
	assert.Equal(t, "2026-08-18", start)
	assert.Equal(t, "2026-08-19", end)
}

func TestJSONUseNumberPreservesLargeIntegers(t *testing.T) {
	var payload map[string]interface{}
	require.NoError(t, jsonUseNumber([]byte(`{"id":9007199254740993}`), &payload))
	assert.Equal(t, json.Number("9007199254740993"), payload["id"])
}

func TestAPIErrorIncludesBambooHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-BambooHR-Error-Message", "Directory disabled for this account")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	src := newTestSource(t, server.URL)
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "employee_directory"})
	require.NoError(t, err)
	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.ErrorContains(t, result.Err, "Directory disabled for this account")
}

func newTestSource(t *testing.T, baseURL string) *BambooHRSource {
	t.Helper()
	client := newHTTPClient(
		baseURL,
		httpclient.NewBasicAuth("test-key", "x"),
		httpclient.WithRateLimiter(1000, 1000),
		httpclient.WithDisableRetry(),
	)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return &BambooHRSource{client: client, companyTimezone: time.UTC, now: time.Now}
}

func newOAuthTestSource(t *testing.T, baseURL string) *BambooHRSource {
	t.Helper()
	client := newHTTPClient(
		baseURL,
		httpclient.NewBearerAuth("oauth-token"),
		httpclient.WithRateLimiter(1000, 1000),
		httpclient.WithDisableRetry(),
	)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return &BambooHRSource{client: client, companyTimezone: time.UTC, usesOAuth: true, now: time.Now}
}

func drainTable(t *testing.T, table source.SourceTable, opts source.ReadOptions) (int64, int) {
	t.Helper()
	results, err := table.Read(context.Background(), opts)
	require.NoError(t, err)

	var rows int64
	var batches int
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch == nil {
			continue
		}
		rows += result.Batch.NumRows()
		batches++
		result.Batch.Release()
	}
	return rows, batches
}
