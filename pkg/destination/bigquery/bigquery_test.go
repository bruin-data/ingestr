package bigquery

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination"
	duckdbdest "github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

func duckdbCompatible(sql string) string {
	return strings.ReplaceAll(sql, "`", `"`)
}

func connectTestDuckDBDest(t *testing.T, ctx context.Context) (*duckdbdest.DuckDBDestination, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.duckdb")

	dest := duckdbdest.NewDuckDBDestination()
	if err := dest.Connect(ctx, fmt.Sprintf("duckdb:///%s", path)); err != nil {
		t.Skipf("DuckDB unavailable: %v", err)
	}
	// No t.Cleanup for dest.Close — caller must close before opening sql.DB.

	return dest, path
}

func openTestDuckDBQuery(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	if err != nil {
		t.Skipf("DuckDB sql driver unavailable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type stubStorageArrowAppender struct {
	tablePath          string
	parallelism        int
	batchCount         int
	appendErr          error
	pendingTablePath   string
	pendingParallelism int
	pendingBatchCount  int
	pendingAppendErr   error
}

func (s *stubStorageArrowAppender) AppendArrowStreamFromSource(_ context.Context, tablePath string, records <-chan source.RecordBatchResult, parallelism int) error {
	s.tablePath = tablePath
	s.parallelism = parallelism
	if s.appendErr != nil {
		return s.appendErr
	}
	for range records {
		s.batchCount++
	}
	return nil
}

func (s *stubStorageArrowAppender) AppendArrowPendingStreamsFromSource(_ context.Context, tablePath string, records <-chan source.RecordBatchResult, parallelism int) error {
	s.pendingTablePath = tablePath
	s.pendingParallelism = parallelism
	if s.pendingAppendErr != nil {
		return s.pendingAppendErr
	}
	for range records {
		s.pendingBatchCount++
	}
	return nil
}

func (s *stubStorageArrowAppender) Close() error {
	return nil
}

func TestNewBigQueryDestination(t *testing.T) {
	dest := NewBigQueryDestination()
	if dest == nil {
		t.Fatal("NewBigQueryDestination returned nil")
	}
}

func TestRunQueryJobWithRetryRecoversDuplicateJobInsert(t *testing.T) {
	ctx := context.Background()
	const sql = "SELECT 1"

	var insertCalls int
	var jobGetCalls int
	var queryResultsCalls int
	var gotJobID string
	var gotSQL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/projects/test-project/jobs"):
			insertCalls++
			var req struct {
				JobReference struct {
					ProjectID string `json:"projectId"`
					JobID     string `json:"jobId"`
					Location  string `json:"location"`
				} `json:"jobReference"`
				Configuration struct {
					Query struct {
						Query string `json:"query"`
					} `json:"query"`
				} `json:"configuration"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotJobID = req.JobReference.JobID
			gotSQL = req.Configuration.Query.Query

			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"code":    http.StatusConflict,
					"message": fmt.Sprintf("Already Exists: Job test-project:US.%s", gotJobID),
					"status":  "ALREADY_EXISTS",
					"errors": []map[string]string{
						{
							"domain":  "global",
							"message": fmt.Sprintf("Already Exists: Job test-project:US.%s", gotJobID),
							"reason":  "duplicate",
						},
					},
				},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/projects/test-project/jobs/"):
			jobGetCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jobReference": map[string]string{
					"projectId": "test-project",
					"jobId":     gotJobID,
					"location":  "US",
				},
				"configuration": map[string]interface{}{
					"query": map[string]interface{}{
						"query":        gotSQL,
						"useLegacySql": false,
					},
				},
				"status": map[string]string{
					"state": "DONE",
				},
				"statistics": map[string]interface{}{
					"query": map[string]string{
						"statementType": "SELECT",
					},
				},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/projects/test-project/queries/"):
			queryResultsCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jobReference": map[string]string{
					"projectId": "test-project",
					"jobId":     gotJobID,
					"location":  "US",
				},
				"jobComplete": true,
				"totalRows":   "0",
				"schema": map[string]interface{}{
					"fields": []interface{}{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := bigquery.NewClient(ctx, "test-project", option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	dest := &BigQueryDestination{
		client:    client,
		projectID: "test-project",
		location:  "US",
	}

	job, err := dest.runQueryJobWithRetryAttempts(ctx, sql, "MERGE", 1)
	if err != nil {
		t.Fatalf("runQueryJobWithRetryAttempts() error = %v", err)
	}
	if job == nil {
		t.Fatal("runQueryJobWithRetryAttempts() returned nil job")
	}
	if job.ID() != gotJobID {
		t.Fatalf("job.ID() = %q, want %q", job.ID(), gotJobID)
	}
	if !strings.HasPrefix(gotJobID, "ingestr_") {
		t.Fatalf("job ID = %q, want ingestr_ prefix", gotJobID)
	}
	if !strings.Contains(gotSQL, sql) {
		t.Fatalf("submitted SQL = %q, want it to contain %q", gotSQL, sql)
	}
	if insertCalls != 1 {
		t.Fatalf("insertCalls = %d, want 1", insertCalls)
	}
	if jobGetCalls == 0 {
		t.Fatal("expected duplicate recovery to fetch the existing job")
	}
	if queryResultsCalls != 1 {
		t.Fatalf("queryResultsCalls = %d, want 1", queryResultsCalls)
	}
}

func TestRunQueryJobWithRetryUsesRemainingAttemptsAfterDuplicateRecoveryFailure(t *testing.T) {
	ctx := context.Background()
	const sql = "SELECT 1"

	var insertCalls int
	var jobGetCalls int
	var queryResultsCalls int
	var gotJobID string
	var gotSQL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/projects/test-project/jobs"):
			insertCalls++
			var req struct {
				JobReference struct {
					JobID string `json:"jobId"`
				} `json:"jobReference"`
				Configuration struct {
					Query struct {
						Query string `json:"query"`
					} `json:"query"`
				} `json:"configuration"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotJobID = req.JobReference.JobID
			gotSQL = req.Configuration.Query.Query

			if insertCalls == 1 {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{
						"code":    http.StatusConflict,
						"message": fmt.Sprintf("Already Exists: Job test-project:US.%s", gotJobID),
						"errors": []map[string]string{
							{
								"domain":  "global",
								"message": fmt.Sprintf("Already Exists: Job test-project:US.%s", gotJobID),
								"reason":  "duplicate",
							},
						},
					},
				})
				return
			}

			writeBigQueryTestJob(w, gotJobID, gotSQL)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/projects/test-project/jobs/"):
			jobGetCalls++
			if jobGetCalls == 1 {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{
						"code":    http.StatusNotFound,
						"message": "Not found: Job",
					},
				})
				return
			}
			writeBigQueryTestJob(w, gotJobID, gotSQL)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/projects/test-project/queries/"):
			queryResultsCalls++
			writeBigQueryTestQueryResults(w, gotJobID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := bigquery.NewClient(ctx, "test-project", option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	dest := &BigQueryDestination{
		client:    client,
		projectID: "test-project",
		location:  "US",
	}

	job, err := dest.runQueryJobWithRetryAttempts(ctx, sql, "MERGE", 2)
	if err != nil {
		t.Fatalf("runQueryJobWithRetryAttempts() error = %v", err)
	}
	if job == nil {
		t.Fatal("runQueryJobWithRetryAttempts() returned nil job")
	}
	if insertCalls != 2 {
		t.Fatalf("insertCalls = %d, want 2", insertCalls)
	}
	if jobGetCalls != 2 {
		t.Fatalf("jobGetCalls = %d, want 2", jobGetCalls)
	}
	if queryResultsCalls != 1 {
		t.Fatalf("queryResultsCalls = %d, want 1", queryResultsCalls)
	}
}

func TestRecoverDuplicateQueryJobReportsSQLMismatch(t *testing.T) {
	ctx := context.Background()
	const expectedSQL = "SELECT 1"
	const existingSQL = "SELECT 2"
	const jobID = "ingestr_test"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/projects/test-project/jobs/") {
			writeBigQueryTestJob(w, jobID, existingSQL)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := bigquery.NewClient(ctx, "test-project", option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	dest := &BigQueryDestination{
		client:    client,
		projectID: "test-project",
		location:  "US",
	}

	_, err = dest.recoverDuplicateQueryJob(ctx, jobID, expectedSQL)
	if err == nil {
		t.Fatal("recoverDuplicateQueryJob() error = nil, want mismatch")
	}
	if !strings.Contains(err.Error(), `existing="SELECT 2"`) || !strings.Contains(err.Error(), `expected="SELECT 1"`) {
		t.Fatalf("recoverDuplicateQueryJob() error = %q, want existing and expected SQL snippets", err)
	}
}

func TestRecoverDuplicateQueryJobAllowsDifferentAnnotation(t *testing.T) {
	ctx := context.Background()
	const expectedSQL = "-- @bruin.config: {\"request_id\":\"expected\"}\nSELECT 1"
	const existingSQL = "-- @bruin.config: {\"request_id\":\"existing\"}\nSELECT 1"
	const jobID = "ingestr_test"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/projects/test-project/jobs/") {
			writeBigQueryTestJob(w, jobID, existingSQL)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := bigquery.NewClient(ctx, "test-project", option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	dest := &BigQueryDestination{
		client:    client,
		projectID: "test-project",
		location:  "US",
	}

	job, err := dest.recoverDuplicateQueryJob(ctx, jobID, expectedSQL)
	if err != nil {
		t.Fatalf("recoverDuplicateQueryJob() error = %v", err)
	}
	if job == nil {
		t.Fatal("recoverDuplicateQueryJob() returned nil job")
	}
}

func writeBigQueryTestJob(w http.ResponseWriter, jobID, sql string) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"jobReference": map[string]string{
			"projectId": "test-project",
			"jobId":     jobID,
			"location":  "US",
		},
		"configuration": map[string]interface{}{
			"query": map[string]interface{}{
				"query":        sql,
				"useLegacySql": false,
			},
		},
		"status": map[string]string{
			"state": "DONE",
		},
		"statistics": map[string]interface{}{
			"query": map[string]string{
				"statementType": "SELECT",
			},
		},
	})
}

func writeBigQueryTestQueryResults(w http.ResponseWriter, jobID string) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"jobReference": map[string]string{
			"projectId": "test-project",
			"jobId":     jobID,
			"location":  "US",
		},
		"jobComplete": true,
		"totalRows":   "0",
		"schema": map[string]interface{}{
			"fields": []interface{}{},
		},
	})
}

func TestIsBigQueryDuplicateJobError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "googleapi duplicate job",
			err: &googleapi.Error{
				Code:    http.StatusConflict,
				Message: "Already Exists: Job test:US.job",
				Errors: []googleapi.ErrorItem{
					{Reason: "duplicate", Message: "Already Exists: Job test:US.job"},
				},
			},
			want: true,
		},
		{
			name: "string duplicate job",
			err:  errors.New("googleapi: Error 409: Already Exists: Job bruin-internal-dwh:US.w61mnz2N9xQMtY2nzHLeEotitcQ, duplicate"),
			want: true,
		},
		{
			name: "table already exists is not duplicate job",
			err: &googleapi.Error{
				Code:    http.StatusConflict,
				Message: "Already Exists: Table test.dataset.table",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBigQueryDuplicateJobError(tt.err); got != tt.want {
				t.Fatalf("isBigQueryDuplicateJobError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchemes(t *testing.T) {
	dest := NewBigQueryDestination()
	schemes := dest.Schemes()

	if len(schemes) != 1 {
		t.Errorf("expected 1 scheme, got %d", len(schemes))
	}
	if schemes[0] != "bigquery" {
		t.Errorf("expected scheme 'bigquery', got '%s'", schemes[0])
	}
}

func TestParseBigQueryURI(t *testing.T) {
	fakeServiceAccountJSON := `{"type":"service_account","project_id":"test"}`
	fakeServiceAccountBase64 := base64.StdEncoding.EncodeToString([]byte(fakeServiceAccountJSON))

	tests := []struct {
		name           string
		uri            string
		wantProjectID  string
		wantDatasetID  string
		wantLocation   string
		wantCredPath   string
		wantCredJSON   string
		wantLoadMethod bigQueryLoadMethod
		wantErr        bool
		errContains    string
	}{
		{
			name:           "simple_uri",
			uri:            "bigquery://my-project",
			wantProjectID:  "my-project",
			wantLoadMethod: loadMethodLoadJob,
			wantErr:        false,
		},
		{
			name:           "with_dataset",
			uri:            "bigquery://my-project/my-dataset",
			wantProjectID:  "my-project",
			wantDatasetID:  "my-dataset",
			wantLoadMethod: loadMethodLoadJob,
			wantErr:        false,
		},
		{
			name:           "with_credentials_path",
			uri:            "bigquery://my-project?credentials_path=/path/to/creds.json",
			wantProjectID:  "my-project",
			wantCredPath:   "/path/to/creds.json",
			wantLoadMethod: loadMethodLoadJob,
			wantErr:        false,
		},
		{
			name:           "with_location",
			uri:            "bigquery://my-project?location=us-central1",
			wantProjectID:  "my-project",
			wantLocation:   "us-central1",
			wantLoadMethod: loadMethodLoadJob,
			wantErr:        false,
		},
		{
			name:           "with_all_params",
			uri:            "bigquery://my-project/my-dataset?credentials_path=/creds.json&location=EU",
			wantProjectID:  "my-project",
			wantDatasetID:  "my-dataset",
			wantCredPath:   "/creds.json",
			wantLocation:   "EU",
			wantLoadMethod: loadMethodLoadJob,
			wantErr:        false,
		},
		{
			name:           "with_base64_credentials",
			uri:            "bigquery://test-project?credentials_base64=" + fakeServiceAccountBase64,
			wantProjectID:  "test-project",
			wantCredJSON:   fakeServiceAccountJSON,
			wantLoadMethod: loadMethodLoadJob,
			wantErr:        false,
		},
		{
			name:           "with_storage_write_load_method",
			uri:            "bigquery://my-project/my-dataset?load_method=storage_write",
			wantProjectID:  "my-project",
			wantDatasetID:  "my-dataset",
			wantLoadMethod: loadMethodStorageWrite,
			wantErr:        false,
		},
		{
			name:        "missing_project",
			uri:         "bigquery://",
			wantErr:     true,
			errContains: "must include project_id",
		},
		{
			name:        "invalid_uri_no_host",
			uri:         "not a uri",
			wantErr:     true,
			errContains: "must include project_id",
		},
		{
			name:        "invalid_base64",
			uri:         "bigquery://test-project?credentials_base64=invalid!base64",
			wantErr:     true,
			errContains: "decode base64",
		},
		{
			name:        "invalid_load_method",
			uri:         "bigquery://test-project?load_method=invalid",
			wantErr:     true,
			errContains: "unsupported load_method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseBigQueryURI(tt.uri)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want substring %s", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if cfg.projectID != tt.wantProjectID {
				t.Errorf("projectID = %s, want %s", cfg.projectID, tt.wantProjectID)
			}
			if cfg.datasetID != tt.wantDatasetID {
				t.Errorf("datasetID = %s, want %s", cfg.datasetID, tt.wantDatasetID)
			}
			if cfg.location != tt.wantLocation {
				t.Errorf("location = %s, want %s", cfg.location, tt.wantLocation)
			}
			if cfg.credPath != tt.wantCredPath {
				t.Errorf("credPath = %s, want %s", cfg.credPath, tt.wantCredPath)
			}
			if cfg.credJSON != tt.wantCredJSON {
				t.Errorf("credJSON = %s, want %s", cfg.credJSON, tt.wantCredJSON)
			}
			if cfg.loadMethod != tt.wantLoadMethod {
				t.Errorf("loadMethod = %s, want %s", cfg.loadMethod, tt.wantLoadMethod)
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil_error",
			err:      nil,
			expected: false,
		},
		{
			name:     "not_found_lowercase",
			err:      &testError{msg: "table not found"},
			expected: true,
		},
		{
			name:     "not_found_uppercase",
			err:      &testError{msg: "Table Not found"},
			expected: true,
		},
		{
			name:     "not_found_all_caps",
			err:      &testError{msg: "NOT_FOUND: table does not exist"},
			expected: true,
		},
		{
			name:     "other_error",
			err:      &testError{msg: "permission denied"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotFoundError(tt.err)
			if result != tt.expected {
				t.Errorf("isNotFoundError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestSupportsStrategies(t *testing.T) {
	dest := NewBigQueryDestination()
	if !dest.SupportsReplaceStrategy() {
		t.Error("SupportsReplaceStrategy() = false, want true")
	}
	if !dest.SupportsAppendStrategy() {
		t.Error("SupportsAppendStrategy() = false, want true")
	}
	if !dest.SupportsMergeStrategy() {
		t.Error("SupportsMergeStrategy() = false, want true")
	}
	if !dest.SupportsDeleteInsertStrategy() {
		t.Error("SupportsDeleteInsertStrategy() = false, want true")
	}
}

// TestEnsureDatasetExists_ConcurrentCallers exercises the knownDatasets
// mutex. Without the mutex (the bug fix), running ensureDatasetExists from
// many goroutines for the same (project, dataset) racing all the way through
// the success path would either panic with "fatal error: concurrent map
// writes" or be flagged by `go test -race`. The fix lives in
// ensureDatasetExists and markDatasetKnown (bigquery.go).
func TestEnsureDatasetExists_ConcurrentCallers(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Respond to any dataset metadata GET with a minimal stub.
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/datasets/") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"datasetReference": map[string]string{
					"projectId": "test-project",
					"datasetId": "test-dataset",
				},
				"location": "US",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := bigquery.NewClient(ctx, "test-project", option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	dest := &BigQueryDestination{
		client:    client,
		projectID: "test-project",
		location:  "US",
	}

	const goroutines = 32
	errs := make(chan error, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			<-start
			errs <- dest.ensureDatasetExists(ctx, "test-project", "test-dataset")
		}()
	}
	close(start)

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("ensureDatasetExists returned error: %v", err)
		}
	}
}

func TestBeginTransaction_ReturnsScriptTransaction(t *testing.T) {
	dest := NewBigQueryDestination()
	tx, err := dest.BeginTransaction(context.Background())
	if err != nil {
		t.Fatalf("BeginTransaction returned error: %v", err)
	}

	bqTx, ok := tx.(*bigQueryTransaction)
	if !ok {
		t.Fatalf("transaction type = %T, want *bigQueryTransaction", tx)
	}

	if err := bqTx.Exec(context.Background(), "DELETE FROM `p`.`d`.`t` WHERE TRUE"); err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if err := bqTx.Exec(context.Background(), "INSERT INTO `p`.`d`.`t` SELECT * FROM `p`.`d`.`s`"); err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}

	got := buildBigQueryTransactionScript(bqTx.statements...)
	want := "BEGIN TRANSACTION;\n" +
		"DELETE FROM `p`.`d`.`t` WHERE TRUE;\n" +
		"INSERT INTO `p`.`d`.`t` SELECT * FROM `p`.`d`.`s`;\n" +
		"COMMIT TRANSACTION;"
	if got != want {
		t.Fatalf("transaction script =\n%s\nwant:\n%s", got, want)
	}

	if err := bqTx.Exec(context.Background(), "SELECT 1", 1); err == nil || !contains(err.Error(), "does not support positional query args") {
		t.Fatalf("Exec with args error = %v", err)
	}
}

func TestClose_IgnoresTypedNilStorageClient(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.storageArrowClient = (*StorageWriteArrowClient)(nil)

	if err := dest.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestNormalizeSchemaForLoadMethod_RelaxesNullabilityForLoadJobs(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.loadMethod = loadMethodLoadJob

	input := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: false},
		},
	}

	got := dest.normalizeSchemaForLoadMethod(input)
	if got == input {
		t.Fatal("normalizeSchemaForLoadMethod should clone the schema for load jobs")
	}
	for _, col := range got.Columns {
		if !col.Nullable {
			t.Fatalf("column %q remained non-nullable in load-job mode", col.Name)
		}
	}
	if input.Columns[0].Nullable {
		t.Fatal("normalizeSchemaForLoadMethod should not mutate the input schema")
	}
}

func TestNormalizeSchemaForLoadMethod_KeepsStorageWriteSchema(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.loadMethod = loadMethodStorageWrite

	input := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
	}

	got := dest.normalizeSchemaForLoadMethod(input)
	if got != input {
		t.Fatal("storage write mode should keep the original schema")
	}
	if got.Columns[0].Nullable {
		t.Fatal("storage write mode should preserve nullability")
	}
}

func TestWriteParallel_ForwardsTablePath(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table:       "my_dataset.my_table",
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	stub := dest.storageArrowClient.(*stubStorageArrowAppender)
	if stub.tablePath != "projects/my-project/datasets/my_dataset/tables/my_table/streams/_default" {
		t.Fatalf("AppendArrowStream tablePath = %q", stub.tablePath)
	}
}

func TestWriteParallel_UsesRequestedParallelismWithinLimit(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table:       "my_dataset.my_table",
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	stub := dest.storageArrowClient.(*stubStorageArrowAppender)
	if stub.parallelism != 2 {
		t.Fatalf("AppendArrowStream parallelism = %d, want 2", stub.parallelism)
	}
}

func TestWriteParallel_UsesPendingStreamsForAtomicCommit(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table:        "my_dataset.my_table",
		Parallelism:  8,
		AtomicCommit: true,
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	stub := dest.storageArrowClient.(*stubStorageArrowAppender)
	if stub.pendingTablePath != "projects/my-project/datasets/my_dataset/tables/my_table" {
		t.Fatalf("AppendArrowPendingStreams tablePath = %q", stub.pendingTablePath)
	}
	if stub.pendingParallelism != 8 {
		t.Fatalf("AppendArrowPendingStreams parallelism = %d, want 8", stub.pendingParallelism)
	}
	if stub.tablePath != "" {
		t.Fatalf("default stream path should not be used when AtomicCommit=true, got %q", stub.tablePath)
	}
}

func TestSupportsAtomicCommitWrites(t *testing.T) {
	tests := []struct {
		name       string
		loadMethod bigQueryLoadMethod
		want       bool
	}{
		{name: "load job", loadMethod: loadMethodLoadJob, want: true},
		{name: "storage write", loadMethod: loadMethodStorageWrite, want: true},
		{name: "unset defaults to load job", loadMethod: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := NewBigQueryDestination()
			dest.loadMethod = tt.loadMethod
			if got := dest.SupportsAtomicCommitWrites(); got != tt.want {
				t.Fatalf("SupportsAtomicCommitWrites() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteParallel_CapsParallelismForDefaultStream(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table:       "my_dataset.my_table",
		Parallelism: 16,
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	stub := dest.storageArrowClient.(*stubStorageArrowAppender)
	if stub.parallelism != maxDefaultStreamParallelism {
		t.Fatalf("AppendArrowStream parallelism = %d, want %d", stub.parallelism, maxDefaultStreamParallelism)
	}
}

func TestWriteParallel_DefaultsParallelismWhenUnset(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table: "my_dataset.my_table",
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	stub := dest.storageArrowClient.(*stubStorageArrowAppender)
	if stub.parallelism != defaultWriteParallelism {
		t.Fatalf("AppendArrowStream parallelism = %d, want %d", stub.parallelism, defaultWriteParallelism)
	}
}

func TestWriteParallel_CapsParallelismForPendingStreams(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table:        "my_dataset.my_table",
		Parallelism:  128,
		AtomicCommit: true,
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	stub := dest.storageArrowClient.(*stubStorageArrowAppender)
	if stub.pendingParallelism != maxPendingStreamParallelism {
		t.Fatalf(
			"AppendArrowPendingStreams parallelism = %d, want %d",
			stub.pendingParallelism,
			maxPendingStreamParallelism,
		)
	}
}

func TestWriteParallel_CancelsBridgeOnAppendError(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodStorageWrite
	dest.storageArrowClient = &stubStorageArrowAppender{appendErr: errors.New("append failed")}

	records := make(chan source.RecordBatchResult)

	done := make(chan error, 1)
	go func() {
		done <- dest.WriteParallel(context.Background(), records, destination.WriteOptions{
			Table: "my_dataset.my_table",
		})
	}()

	select {
	case err := <-done:
		if err == nil || err.Error() != "append failed" {
			t.Fatalf("WriteParallel error = %v, want append failed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteParallel did not return after append error")
	}
}

func TestWriteParallel_WaitsOnlyForMatchingPendingTable(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodLoadJob

	var gotDataset string
	var gotTable string
	dest.loadJobWriter = func(_ context.Context, dataset, table string, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
		gotDataset = dataset
		gotTable = table
		for range records {
		}
		return nil
	}

	otherCh := make(chan error, 1)
	targetCh := make(chan error, 1)
	targetCh <- nil
	dest.pendingTableErrs = map[string]chan error{
		"my-project.other_dataset.other_table": otherCh,
		"my-project.my_dataset.my_table":       targetCh,
	}

	records := make(chan source.RecordBatchResult)
	close(records)

	done := make(chan error, 1)
	go func() {
		done <- dest.WriteParallel(context.Background(), records, destination.WriteOptions{
			Table: "my_dataset.my_table",
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WriteParallel returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteParallel blocked on unrelated pending table state")
	}

	if _, ok := dest.pendingTableErrs["my-project.my_dataset.my_table"]; ok {
		t.Fatal("matching pending table state was not cleared")
	}
	if _, ok := dest.pendingTableErrs["my-project.other_dataset.other_table"]; !ok {
		t.Fatal("unrelated pending table state should remain")
	}
	if gotDataset != "my_dataset" || gotTable != "my_table" {
		t.Fatalf("load job writer got dataset=%q table=%q", gotDataset, gotTable)
	}
}

func TestWriteParallel_ClearsPendingTableAfterPrepareError(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"
	dest.loadMethod = loadMethodLoadJob
	dest.loadJobWriter = func(_ context.Context, _, _ string, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
		for range records {
		}
		return nil
	}

	targetCh := make(chan error, 1)
	targetCh <- errors.New("prepare failed")
	dest.pendingTableErrs = map[string]chan error{
		"my-project.my_dataset.my_table": targetCh,
	}

	records := make(chan source.RecordBatchResult)
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table: "my_dataset.my_table",
	})
	if err == nil || err.Error() != "failed to prepare table: prepare failed" {
		t.Fatalf("WriteParallel error = %v, want prepare failure", err)
	}

	if dest.pendingTableErrs != nil {
		if _, ok := dest.pendingTableErrs["my-project.my_dataset.my_table"]; ok {
			t.Fatal("failed pending table state was not cleared")
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- dest.WriteParallel(context.Background(), records, destination.WriteOptions{
			Table: "my_dataset.my_table",
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second WriteParallel returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second WriteParallel blocked on drained pending table channel")
	}
}

func TestWriteParallel_UsesLoadJobsByDefault(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"

	var gotDataset string
	var gotTable string
	var gotBucket string
	var gotLoaderFileSize int
	var gotLoaderFileFormat string
	var gotParallelism int
	var gotStagingTable bool
	var batchCount int
	dest.loadJobWriter = func(_ context.Context, dataset, table string, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
		gotDataset = dataset
		gotTable = table
		gotBucket = opts.StagingBucket
		gotLoaderFileSize = opts.LoaderFileSize
		gotLoaderFileFormat = opts.LoaderFileFormat
		gotParallelism = opts.Parallelism
		gotStagingTable = opts.StagingTable
		for range records {
			batchCount++
		}
		return nil
	}

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{}
	close(records)

	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table:            "my_dataset.my_table",
		Parallelism:      7,
		StagingTable:     true,
		StagingBucket:    "gs://bucket/prefix",
		LoaderFileSize:   1234,
		LoaderFileFormat: "jsonl",
	})
	if err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	if gotDataset != "my_dataset" || gotTable != "my_table" {
		t.Fatalf("load job writer got dataset=%q table=%q", gotDataset, gotTable)
	}
	if gotBucket != "gs://bucket/prefix" {
		t.Fatalf("load job writer got staging bucket %q", gotBucket)
	}
	if gotLoaderFileSize != 1234 {
		t.Fatalf("load job writer got loader file size %d", gotLoaderFileSize)
	}
	if gotLoaderFileFormat != "jsonl" {
		t.Fatalf("load job writer got loader file format %q", gotLoaderFileFormat)
	}
	if gotParallelism != 7 {
		t.Fatalf("load job writer got parallelism %d", gotParallelism)
	}
	if !gotStagingTable {
		t.Fatal("load job writer did not receive staging table flag")
	}
	if batchCount != 1 {
		t.Fatalf("load job writer consumed %d batches, want 1", batchCount)
	}
}

func TestFormatBigQueryValue(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 6_000, time.UTC)
	tsPtr := &ts

	tests := []struct {
		name    string
		in      any
		keyType schema.DataType
		want    string
	}{
		{name: "time", in: ts, keyType: schema.TypeTimestampTZ, want: "TIMESTAMP '2024-01-02 03:04:05.000006'"},
		{name: "time_ptr", in: tsPtr, keyType: schema.TypeTimestampTZ, want: "TIMESTAMP '2024-01-02 03:04:05.000006'"},
		{name: "time_ptr_nil", in: (*time.Time)(nil), keyType: schema.TypeTimestampTZ, want: "NULL"},
		{name: "time_date", in: ts, keyType: schema.TypeDate, want: "DATE '2024-01-02'"},
		{name: "time_ptr_date", in: tsPtr, keyType: schema.TypeDate, want: "DATE '2024-01-02'"},
		{name: "string", in: "abc", keyType: schema.TypeString, want: "'abc'"},
		{name: "int", in: int(7), keyType: schema.TypeInt64, want: "7"},
		{name: "int32", in: int32(8), keyType: schema.TypeInt32, want: "8"},
		{name: "int64", in: int64(9), keyType: schema.TypeInt64, want: "9"},
		{name: "float32", in: float32(1.25), keyType: schema.TypeFloat32, want: "1.25"},
		{name: "float64", in: float64(2.5), keyType: schema.TypeFloat64, want: "2.5"},
		{name: "default", in: true, keyType: schema.TypeUnknown, want: "'true'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBigQueryValue(tt.in, tt.keyType)
			if got != tt.want {
				t.Fatalf("formatBigQueryValue(%T) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildDeleteInsertTransactionScript(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"

	opts := destination.DeleteInsertOptions{
		StagingTable:       "staging_ds.staging_tbl",
		TargetTable:        "target_ds.target_tbl",
		IncrementalKey:     "ts",
		IncrementalKeyType: schema.TypeInt64,
		IntervalStart:      int64(1),
		IntervalEnd:        int64(10),
		Columns:            []string{"id", "ts", "name"},
		PrimaryKeys:        []string{"id"},
	}

	deleteSQL, insertSQL := dest.buildDeleteInsertStatements("my-project", "staging_ds", "staging_tbl", "target_ds", "target_tbl", opts)
	got := buildBigQueryTransactionScript(deleteSQL, insertSQL)
	want := "BEGIN TRANSACTION;\n" +
		"DELETE FROM `my-project`.`target_ds`.`target_tbl` WHERE `ts` >= 1 AND `ts` <= 10;\n" +
		"INSERT INTO `my-project`.`target_ds`.`target_tbl` (`id`, `ts`, `name`) SELECT `id`, `ts`, `name` FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `id` ORDER BY `ts` DESC) = 1;\n" +
		"COMMIT TRANSACTION;"
	if got != want {
		t.Fatalf("transaction script =\n%s\nwant:\n%s", got, want)
	}
}

func TestParseAlterColumnTypeSQL(t *testing.T) {
	table, column, newType, ok := parseAlterColumnTypeSQL("ALTER TABLE my_dataset.my_table ALTER COLUMN `age` SET DATA TYPE STRING")
	if !ok {
		t.Fatal("expected ALTER COLUMN TYPE SQL to parse")
	}
	if table != "my_dataset.my_table" || column != "age" || newType != "STRING" {
		t.Fatalf("unexpected parse result: table=%q column=%q newType=%q", table, column, newType)
	}
}

func TestNormalizeBigQueryDecimalPrecisionScale(t *testing.T) {
	precision, scale := normalizeBigQueryDecimalPrecisionScale(bigquery.NumericFieldType, 0, 0)
	if precision != 38 || scale != 9 {
		t.Fatalf("NUMERIC(0,0) normalized to (%d,%d), want (38,9)", precision, scale)
	}

	precision, scale = normalizeBigQueryDecimalPrecisionScale(bigquery.BigNumericFieldType, 0, 0)
	if precision != 76 || scale != 38 {
		t.Fatalf("BIGNUMERIC(0,0) normalized to (%d,%d), want (76,38)", precision, scale)
	}
}

func TestBuildAlterColumnTypeRewriteSQL(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"

	meta := &bigquery.TableMetadata{
		Schema: bigquery.Schema{
			{Name: "id", Type: bigquery.IntegerFieldType},
			{Name: "age", Type: bigquery.IntegerFieldType},
			{Name: "name", Type: bigquery.StringFieldType},
		},
		Clustering: &bigquery.Clustering{Fields: []string{"id"}},
		TimePartitioning: &bigquery.TimePartitioning{
			Field: "created_at",
		},
	}

	sql, err := dest.buildAlterColumnTypeRewriteSQL("my-project", "my_dataset", "my_table", "age", "STRING", meta)
	if err != nil {
		t.Fatalf("buildAlterColumnTypeRewriteSQL returned error: %v", err)
	}
	if !contains(sql, "CREATE OR REPLACE TABLE `my-project`.`my_dataset`.`my_table`") {
		t.Fatalf("rewrite SQL missing table header:\n%s", sql)
	}
	if !contains(sql, "PARTITION BY DATE(`created_at`)") {
		t.Fatalf("rewrite SQL missing partition clause:\n%s", sql)
	}
	if !contains(sql, "CLUSTER BY `id`") {
		t.Fatalf("rewrite SQL missing cluster clause:\n%s", sql)
	}
	if !contains(sql, "CAST(`age` AS STRING) AS `age`") {
		t.Fatalf("rewrite SQL missing cast expression:\n%s", sql)
	}
}

func TestBuildAlterColumnTypeRewriteSQL_DatePartitionNotWrapped(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"

	meta := &bigquery.TableMetadata{
		Schema: bigquery.Schema{
			{Name: "day", Type: bigquery.DateFieldType},
			{Name: "age", Type: bigquery.IntegerFieldType},
		},
		TimePartitioning: &bigquery.TimePartitioning{
			Field: "day",
		},
	}

	sql, err := dest.buildAlterColumnTypeRewriteSQL("my-project", "my_dataset", "my_table", "age", "STRING", meta)
	if err != nil {
		t.Fatalf("buildAlterColumnTypeRewriteSQL returned error: %v", err)
	}
	if !contains(sql, "PARTITION BY `day`") {
		t.Fatalf("DATE partition column should be referenced bare:\n%s", sql)
	}
	if contains(sql, "PARTITION BY DATE(`day`)") {
		t.Fatalf("DATE partition column must not be wrapped in DATE():\n%s", sql)
	}
}

func TestIsDatePartitionColumn(t *testing.T) {
	s := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "day", DataType: schema.TypeDate},
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	if !isDatePartitionColumn(s, "day") {
		t.Fatal("expected day to be detected as a DATE column")
	}
	if isDatePartitionColumn(s, "created_at") {
		t.Fatal("expected created_at not to be detected as a DATE column")
	}
	if isDatePartitionColumn(s, "missing") {
		t.Fatal("expected missing column to default to false")
	}
	if isDatePartitionColumn(nil, "day") {
		t.Fatal("expected nil schema to default to false")
	}
	if isDatePartitionColumn(s, "") {
		t.Fatal("expected empty column to default to false")
	}
	if !isDatePartitionColumn(s, "Day") {
		t.Fatal("expected case-insensitive match for BigQuery identifiers")
	}
}

func TestPartitionFieldIsDate(t *testing.T) {
	s := bigquery.Schema{
		{Name: "day", Type: bigquery.DateFieldType},
		{Name: "created_at", Type: bigquery.TimestampFieldType},
	}

	if !partitionFieldIsDate(s, "day") {
		t.Fatal("expected day to be detected as a DATE column")
	}
	if partitionFieldIsDate(s, "created_at") {
		t.Fatal("expected created_at not to be detected as a DATE column")
	}
	if partitionFieldIsDate(s, "missing") {
		t.Fatal("expected missing column to default to false")
	}
	if partitionFieldIsDate(nil, "day") {
		t.Fatal("expected nil schema to default to false")
	}
	if !partitionFieldIsDate(s, "Day") {
		t.Fatal("expected case-insensitive match for BigQuery identifiers")
	}
}

func TestPartitionByClause(t *testing.T) {
	if got := partitionByClause("day", true); got != "PARTITION BY `day`\n" {
		t.Fatalf("DATE column clause = %q", got)
	}
	if got := partitionByClause("created_at", false); got != "PARTITION BY DATE(`created_at`)\n" {
		t.Fatalf("timestamp column clause = %q", got)
	}
}

func TestBuildMergePartitionPruning(t *testing.T) {
	meta := &bigquery.TableMetadata{
		Schema: bigquery.Schema{
			{Name: "id", Type: bigquery.IntegerFieldType},
			{Name: "day", Type: bigquery.DateFieldType},
			{Name: "created_at", Type: bigquery.TimestampFieldType},
		},
		TimePartitioning: &bigquery.TimePartitioning{Field: "day"},
	}

	pruning := buildMergePartitionPruning(meta, []string{"id", "DAY"})
	if pruning == nil {
		t.Fatal("expected pruning when the partition column is part of the primary key")
	}
	if pruning.Column != "day" || !pruning.IsDate {
		t.Fatalf("unexpected pruning config: %+v", pruning)
	}

	if got := buildMergePartitionPruning(meta, []string{"id"}); got != nil {
		t.Fatalf("expected no pruning when partition column is not part of primary key, got %+v", got)
	}

	meta.TimePartitioning.Field = "created_at"
	pruning = buildMergePartitionPruning(meta, []string{"id", "created_at"})
	if pruning == nil {
		t.Fatal("expected pruning for timestamp partition primary key")
	}
	if pruning.Column != "created_at" || pruning.IsDate {
		t.Fatalf("unexpected timestamp pruning config: %+v", pruning)
	}

	meta.TimePartitioning.Field = ""
	if got := buildMergePartitionPruning(meta, []string{"id", "created_at"}); got != nil {
		t.Fatalf("expected no pruning for ingestion-time partitioned table, got %+v", got)
	}

	if !hasCastForColumn(map[string]string{"Day": "DATE"}, "day") {
		t.Fatal("expected cast map lookup to be case-insensitive")
	}
}

func TestNonNullablePKColumns(t *testing.T) {
	targetMeta := &bigquery.TableMetadata{Schema: bigquery.Schema{
		{Name: "ID", Type: bigquery.IntegerFieldType, Required: true},
		{Name: "tenant_id", Type: bigquery.StringFieldType, Required: true},
		{Name: "region", Type: bigquery.StringFieldType},
	}}

	got := nonNullablePKColumns(targetMeta, nil, []string{"id", "tenant_id", "region"})
	if !got["id"] || !got["tenant_id"] {
		t.Fatalf("expected required PK columns id and tenant_id, got %v", got)
	}
	if got["region"] {
		t.Fatalf("nullable PK column must not be reported as non-nullable, got %v", got)
	}

	if got := nonNullablePKColumns(targetMeta, nil, []string{"region"}); got != nil {
		t.Fatalf("expected nil when no PK column is required, got %v", got)
	}
	if got := nonNullablePKColumns(nil, nil, []string{"id"}); got != nil {
		t.Fatalf("expected nil for missing schema, got %v", got)
	}

	// A relaxed (all-NULLABLE) target must still qualify when the ingestion
	// schema declares the key NOT NULL — staging then holds no NULL keys.
	relaxedMeta := &bigquery.TableMetadata{Schema: bigquery.Schema{
		{Name: "id", Type: bigquery.IntegerFieldType},
	}}
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64, Nullable: false},
	}}
	got = nonNullablePKColumns(relaxedMeta, tableSchema, []string{"id"})
	if !got["id"] {
		t.Fatalf("expected NOT NULL ingestion schema column to qualify, got %v", got)
	}
}

func TestBuildMergeSQL(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"

	t.Run("single_pk", func(t *testing.T) {
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id"}, []string{"id", "name", "updated_at"}, nil, "")

		if !contains(sql, "MERGE `my-project`.`target_ds`.`target_tbl` AS t\n") {
			t.Fatalf("sql missing merge header:\n%s", sql)
		}
		if !contains(sql, "USING (SELECT * FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `id`) = 1) AS s\n") {
			t.Fatalf("sql missing using clause with dedup:\n%s", sql)
		}
		if !contains(sql, "ON (t.`id` = s.`id` OR (t.`id` IS NULL AND s.`id` IS NULL))\n") {
			t.Fatalf("sql missing on clause:\n%s", sql)
		}
		if !contains(sql, "WHEN MATCHED THEN\n") || !contains(sql, "UPDATE SET") {
			t.Fatalf("sql missing matched update:\n%s", sql)
		}
		if contains(sql, "UPDATE SET t.`id` = s.`id`") {
			t.Fatalf("sql should not update primary key column:\n%s", sql)
		}
		if !contains(sql, "t.`name` = s.`name`") || !contains(sql, "t.`updated_at` = s.`updated_at`") {
			t.Fatalf("sql missing update columns:\n%s", sql)
		}
		if !contains(sql, "WHEN NOT MATCHED THEN\n") || !contains(sql, "INSERT (`id`, `name`, `updated_at`)") {
			t.Fatalf("sql missing insert clause:\n%s", sql)
		}
		if !contains(sql, "VALUES (s.`id`, s.`name`, s.`updated_at`)") {
			t.Fatalf("sql missing insert values:\n%s", sql)
		}
	})

	t.Run("all_columns_are_pk_no_update", func(t *testing.T) {
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id"}, []string{"id"}, nil, "")
		if contains(sql, "WHEN MATCHED THEN") {
			t.Fatalf("sql should not include matched update when there are no non-PK columns:\n%s", sql)
		}
		if !contains(sql, "WHEN NOT MATCHED THEN\n") {
			t.Fatalf("sql missing insert clause:\n%s", sql)
		}
	})

	t.Run("on_clause_is_null_safe_single_pk", func(t *testing.T) {
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id"}, []string{"id", "name"}, nil, "")

		if !contains(sql, "ON (t.`id` = s.`id` OR (t.`id` IS NULL AND s.`id` IS NULL))\n") {
			t.Fatalf("sql missing null-safe on clause:\n%s", sql)
		}
		if contains(sql, "ON t.`id` = s.`id`\n") {
			t.Fatalf("sql should not use bare equality ON clause:\n%s", sql)
		}
	})

	t.Run("on_clause_is_null_safe_composite_pk", func(t *testing.T) {
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"tenant_id", "user_id"}, []string{"tenant_id", "user_id", "value"}, nil, "")

		expected := "ON (t.`tenant_id` = s.`tenant_id` OR (t.`tenant_id` IS NULL AND s.`tenant_id` IS NULL)) AND (t.`user_id` = s.`user_id` OR (t.`user_id` IS NULL AND s.`user_id` IS NULL))\n"
		if !contains(sql, expected) {
			t.Fatalf("sql missing null-safe composite on clause:\n%s", sql)
		}
		if contains(sql, "ON t.`tenant_id` = s.`tenant_id` AND t.`user_id` = s.`user_id`\n") {
			t.Fatalf("sql should not use bare equality composite ON clause:\n%s", sql)
		}
	})

	t.Run("bare_equality_for_required_pk", func(t *testing.T) {
		sql := dest.buildMergeSQLWithPartitionPruning(
			"my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id"}, []string{"id", "name"}, nil, "",
			map[string]bool{"id": true}, nil,
		)

		want := strings.Join([]string{
			"MERGE `my-project`.`target_ds`.`target_tbl` AS t",
			"USING (SELECT * FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `id`) = 1) AS s",
			"ON t.`id` = s.`id`",
			"WHEN MATCHED THEN",
			"  UPDATE SET t.`name` = s.`name`",
			"WHEN NOT MATCHED THEN",
			"  INSERT (`id`, `name`)",
			"  VALUES (s.`id`, s.`name`)",
		}, "\n")
		if sql != want {
			t.Fatalf("unexpected merge SQL:\ngot:\n%s\n\nwant:\n%s", sql, want)
		}
	})

	t.Run("incremental_predicate_is_added_to_on_clause", func(t *testing.T) {
		sql := dest.buildMergeSQLWithPredicate(
			"my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id"}, []string{"id", "event_date", "name"}, nil, "",
			map[string]bool{"id": true}, nil, "t.`event_date` >= DATE '2026-07-01'",
		)

		if !contains(sql, "ON t.`id` = s.`id` AND (t.`event_date` >= DATE '2026-07-01')\n") {
			t.Fatalf("sql missing incremental predicate in ON clause:\n%s", sql)
		}
	})

	t.Run("mixed_required_and_nullable_pks", func(t *testing.T) {
		sql := dest.buildMergeSQLWithPartitionPruning(
			"my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"tenant_id", "user_id"}, []string{"tenant_id", "user_id", "value"}, nil, "",
			map[string]bool{"tenant_id": true}, nil,
		)

		want := strings.Join([]string{
			"MERGE `my-project`.`target_ds`.`target_tbl` AS t",
			"USING (SELECT * FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `tenant_id`, `user_id`) = 1) AS s",
			"ON t.`tenant_id` = s.`tenant_id` AND (t.`user_id` = s.`user_id` OR (t.`user_id` IS NULL AND s.`user_id` IS NULL))",
			"WHEN MATCHED THEN",
			"  UPDATE SET t.`value` = s.`value`",
			"WHEN NOT MATCHED THEN",
			"  INSERT (`tenant_id`, `user_id`, `value`)",
			"  VALUES (s.`tenant_id`, s.`user_id`, s.`value`)",
		}, "\n")
		if sql != want {
			t.Fatalf("unexpected merge SQL:\ngot:\n%s\n\nwant:\n%s", sql, want)
		}
	})

	t.Run("required_pk_lookup_is_case_insensitive", func(t *testing.T) {
		sql := dest.buildMergeSQLWithPartitionPruning(
			"my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"ID"}, []string{"ID", "name"}, nil, "",
			map[string]bool{"id": true}, nil,
		)

		want := strings.Join([]string{
			"MERGE `my-project`.`target_ds`.`target_tbl` AS t",
			"USING (SELECT * FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `ID`) = 1) AS s",
			"ON t.`ID` = s.`ID`",
			"WHEN MATCHED THEN",
			"  UPDATE SET t.`name` = s.`name`",
			"WHEN NOT MATCHED THEN",
			"  INSERT (`ID`, `name`)",
			"  VALUES (s.`ID`, s.`name`)",
		}, "\n")
		if sql != want {
			t.Fatalf("unexpected merge SQL:\ngot:\n%s\n\nwant:\n%s", sql, want)
		}
	})

	t.Run("with_cast_map", func(t *testing.T) {
		castMap := map[string]string{"day": "STRING"}
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id", "day"}, []string{"id", "day", "amount"}, castMap, "")

		if !contains(sql, "(t.`day` = CAST(s.`day` AS STRING) OR (t.`day` IS NULL AND CAST(s.`day` AS STRING) IS NULL))") {
			t.Fatalf("sql missing cast in ON clause:\n%s", sql)
		}
		if !contains(sql, "t.`amount` = s.`amount`") {
			t.Fatalf("sql should not cast non-mismatched columns:\n%s", sql)
		}
		if !contains(sql, "CAST(s.`day` AS STRING)") {
			t.Fatalf("sql missing cast in INSERT values:\n%s", sql)
		}
		if !contains(sql, "(t.`id` = s.`id` OR (t.`id` IS NULL AND s.`id` IS NULL))") {
			t.Fatalf("sql missing null-safe on clause for non-cast pk:\n%s", sql)
		}
	})

	t.Run("cdc_mode", func(t *testing.T) {
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id"}, []string{"id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}, nil, "")

		if !contains(sql, "SELECT la.`id`, act.`name`, la.`_cdc_lsn`, la.`_cdc_deleted`, la.`_cdc_synced_at`, act.`_cdc_lsn` IS NOT NULL AS `__ingestr_has_active`") {
			t.Fatalf("sql missing composed source columns (data from latest active, CDC from latest overall):\n%s", sql)
		}
		if !contains(sql, "ORDER BY `_cdc_lsn` DESC, `_cdc_deleted` DESC) = 1) AS la") {
			t.Fatalf("sql missing latest-overall dedup:\n%s", sql)
		}
		if !contains(sql, "WHERE `_cdc_deleted` = false QUALIFY ROW_NUMBER() OVER (PARTITION BY `id` ORDER BY `_cdc_lsn` DESC) = 1) AS act") {
			t.Fatalf("sql missing latest-active dedup:\n%s", sql)
		}
		if !contains(sql, "WHEN MATCHED AND (s.`_cdc_deleted` = false OR s.`__ingestr_has_active`) THEN\n  UPDATE SET t.`name` = s.`name`") {
			t.Fatalf("sql missing full update for active or update-then-deleted rows:\n%s", sql)
		}
		if !contains(sql, "WHEN MATCHED AND s.`_cdc_deleted` = true THEN\n  UPDATE SET t.`_cdc_deleted` = true, t.`_cdc_lsn` = s.`_cdc_lsn`, t.`_cdc_synced_at` = s.`_cdc_synced_at`") {
			t.Fatalf("sql missing CDC-only update for delete-only windows:\n%s", sql)
		}
		if !contains(sql, "WHEN NOT MATCHED AND (s.`_cdc_deleted` = false OR s.`__ingestr_has_active`) THEN\n  INSERT (`id`, `name`, `_cdc_lsn`, `_cdc_deleted`, `_cdc_synced_at`)") {
			t.Fatalf("sql missing insert clause materializing insert-then-deleted rows:\n%s", sql)
		}
		if contains(sql, "WHEN NOT MATCHED AND s.`_cdc_deleted` = false THEN") {
			t.Fatalf("sql still has the old insert clause that drops insert-then-deleted rows:\n%s", sql)
		}
	})

	t.Run("cdc_mode_unchanged_cols_cased_columns", func(t *testing.T) {
		// The source emits _cdc_unchanged_cols with source-side (lower-case)
		// column names; a destination table created with cased columns must
		// still match them, so the containment check compares lower-cased.
		columns := []string{"id", "Name", "CONFIG_DATA", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"}
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id"}, columns, nil, "")

		if !contains(sql, "t.`CONFIG_DATA` = IF('config_data' IN UNNEST(IFNULL(JSON_EXTRACT_STRING_ARRAY(LOWER(s.`_cdc_unchanged_cols`)), [])), t.`CONFIG_DATA`, s.`CONFIG_DATA`)") {
			t.Fatalf("sql missing case-normalized unchanged-cols preservation:\n%s", sql)
		}
		if !contains(sql, "t.`Name` = IF('name' IN UNNEST(") {
			t.Fatalf("sql missing lower-cased literal for cased column:\n%s", sql)
		}
		// staging-only column must not be persisted on the destination
		if !contains(sql, "INSERT (`id`, `Name`, `CONFIG_DATA`, `_cdc_lsn`, `_cdc_deleted`, `_cdc_synced_at`)\n") {
			t.Fatalf("sql INSERT list should exclude _cdc_unchanged_cols:\n%s", sql)
		}
	})

	t.Run("cdc_mode_without_unchanged_cols_column", func(t *testing.T) {
		// Sources that materialize full change rows (e.g. SQL Server CDC) emit
		// no _cdc_unchanged_cols; the merge must not reference it.
		columns := []string{"id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		sql := dest.buildMergeSQL("my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id"}, columns, nil, "")

		if contains(sql, "_cdc_unchanged_cols") {
			t.Fatalf("sql must not reference _cdc_unchanged_cols when absent:\n%s", sql)
		}
	})

	t.Run("date_partition_pruning_when_partition_column_is_pk", func(t *testing.T) {
		sql := dest.buildMergeSQLWithPartitionPruning(
			"my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id", "day"}, []string{"id", "day", "name"}, nil, "", nil,
			&mergePartitionPruning{Column: "day", IsDate: true},
		)

		if !contains(sql, "DECLARE _ingestr_merge_partition_min DATE DEFAULT (SELECT MIN(`day`) FROM `my-project`.`staging_ds`.`staging_tbl`);\n") {
			t.Fatalf("sql missing date partition min declaration:\n%s", sql)
		}
		if !contains(sql, "DECLARE _ingestr_merge_partition_max DATE DEFAULT (SELECT MAX(`day`) FROM `my-project`.`staging_ds`.`staging_tbl`);\n") {
			t.Fatalf("sql missing date partition max declaration:\n%s", sql)
		}
		if !contains(sql, "DECLARE _ingestr_merge_partition_has_null BOOL DEFAULT (SELECT COALESCE(LOGICAL_OR(`day` IS NULL), FALSE) FROM `my-project`.`staging_ds`.`staging_tbl`);\n") {
			t.Fatalf("sql missing partition null declaration:\n%s", sql)
		}
		if !contains(sql, "AND (t.`day` BETWEEN _ingestr_merge_partition_min AND _ingestr_merge_partition_max OR (_ingestr_merge_partition_has_null AND t.`day` IS NULL))\n") {
			t.Fatalf("sql missing target partition pruning predicate:\n%s", sql)
		}
	})

	t.Run("timestamp_partition_pruning_uses_date_expression", func(t *testing.T) {
		sql := dest.buildMergeSQLWithPartitionPruning(
			"my-project", "target_ds", "target_tbl", "staging_ds", "staging_tbl",
			[]string{"id", "created_at"}, []string{"id", "created_at", "name"}, nil, "", nil,
			&mergePartitionPruning{Column: "created_at"},
		)

		if !contains(sql, "DECLARE _ingestr_merge_partition_min DATE DEFAULT (SELECT MIN(DATE(`created_at`)) FROM `my-project`.`staging_ds`.`staging_tbl`);\n") {
			t.Fatalf("sql missing timestamp partition min declaration:\n%s", sql)
		}
		if !contains(sql, "AND (DATE(t.`created_at`) BETWEEN _ingestr_merge_partition_min AND _ingestr_merge_partition_max OR (_ingestr_merge_partition_has_null AND t.`created_at` IS NULL))\n") {
			t.Fatalf("sql missing timestamp target partition pruning predicate:\n%s", sql)
		}
	})
}

func TestBuildBigQueryDedupSelect_StringShape(t *testing.T) {
	t.Run("no_primary_keys_returns_plain_select", func(t *testing.T) {
		sql := buildBigQueryDedupSelect("`my-project`.`staging_ds`.`staging_tbl`", nil, "")
		want := "SELECT * FROM `my-project`.`staging_ds`.`staging_tbl`"
		if sql != want {
			t.Fatalf("expected plain select, got:\n%s", sql)
		}
		if contains(sql, "QUALIFY") {
			t.Fatalf("did not expect QUALIFY without primary keys:\n%s", sql)
		}
	})

	t.Run("single_primary_key_adds_qualify_row_number", func(t *testing.T) {
		sql := buildBigQueryDedupSelect("`my-project`.`staging_ds`.`staging_tbl`", []string{"id"}, "")
		want := "SELECT * FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `id`) = 1"
		if sql != want {
			t.Fatalf("expected single-PK dedup select, got:\n%s", sql)
		}
	})

	t.Run("composite_primary_keys_partition_in_order", func(t *testing.T) {
		sql := buildBigQueryDedupSelect("`p`.`d`.`t`", []string{"tenant_id", "user_id"}, "")
		if !contains(sql, "PARTITION BY `tenant_id`, `user_id`") {
			t.Fatalf("expected composite PARTITION BY in declared order:\n%s", sql)
		}
	})
}

func TestBuildBigQueryDedupSelect_DuckDBBehavior(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDBDest(t, ctx)

	// All DDL/DML up front through the destination (ADBC native). DuckDB only
	// allows one connection to a file, so we close before opening sql.DB for
	// reads below.
	if err := dest.Exec(ctx, `CREATE TABLE staging (id BIGINT, name VARCHAR, score DOUBLE)`); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	if err := dest.Exec(ctx, `INSERT INTO staging VALUES
		(1, 'A',     10.0),
		(1, 'A-dup', 11.0),
		(2, 'B',     20.0),
		(2, 'B-dup', 21.0),
		(3, 'C',     30.0)`); err != nil {
		t.Fatalf("insert staging: %v", err)
	}
	if err := dest.Exec(ctx, `CREATE TABLE staging_composite (tenant_id INT, user_id INT, payload VARCHAR)`); err != nil {
		t.Fatalf("create staging_composite: %v", err)
	}
	if err := dest.Exec(ctx, `INSERT INTO staging_composite VALUES
		(1, 100, 'a'), (1, 100, 'a-dup'),
		(1, 200, 'b'),
		(2, 100, 'c'), (2, 100, 'c-dup')`); err != nil {
		t.Fatalf("insert staging_composite: %v", err)
	}
	if err := dest.Close(ctx); err != nil {
		t.Fatalf("close destination: %v", err)
	}

	db := openTestDuckDBQuery(t, path)

	t.Run("no_pks_returns_all_rows", func(t *testing.T) {
		sql := buildBigQueryDedupSelect("staging", nil, "")
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+sql+")").Scan(&n); err != nil {
			t.Fatalf("execute generated SQL: %v\nSQL: %s", err, sql)
		}
		if n != 5 {
			t.Fatalf("expected all 5 rows returned without dedup, got %d", n)
		}
	})

	t.Run("single_pk_dedups_to_one_row_per_id", func(t *testing.T) {
		sql := duckdbCompatible(buildBigQueryDedupSelect("staging", []string{"id"}, ""))
		rows, err := db.QueryContext(ctx, sql)
		if err != nil {
			t.Fatalf("execute generated SQL: %v\nSQL: %s", err, sql)
		}
		defer func() { _ = rows.Close() }()

		seen := map[int64]bool{}
		for rows.Next() {
			var id int64
			var name string
			var score float64
			if err := rows.Scan(&id, &name, &score); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if seen[id] {
				t.Fatalf("dedup failed: id %d returned more than once", id)
			}
			seen[id] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows.Err: %v", err)
		}
		if len(seen) != 3 {
			t.Fatalf("expected 3 distinct ids after dedup, got %d", len(seen))
		}
	})

	t.Run("composite_pk_dedups_by_combined_key", func(t *testing.T) {
		sql := duckdbCompatible(buildBigQueryDedupSelect("staging_composite", []string{"tenant_id", "user_id"}, ""))
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+sql+")").Scan(&n); err != nil {
			t.Fatalf("execute generated SQL: %v\nSQL: %s", err, sql)
		}
		// Three distinct (tenant_id, user_id) groups: (1,100), (1,200), (2,100).
		if n != 3 {
			t.Fatalf("expected 3 rows after composite-PK dedup, got %d", n)
		}
	})
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{name: "empty_substr", s: "abc", substr: "", expected: true},
		{name: "equal", s: "abc", substr: "abc", expected: true},
		{name: "prefix", s: "abc", substr: "a", expected: true},
		{name: "middle", s: "abc", substr: "b", expected: true},
		{name: "suffix", s: "abc", substr: "c", expected: true},
		{name: "missing", s: "abc", substr: "d", expected: false},
		{name: "substr_longer", s: "abc", substr: "abcd", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.s, tt.substr)
			if got != tt.expected {
				t.Fatalf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.expected)
			}
		})
	}
}

func TestIsNotFoundError_Wrapped(t *testing.T) {
	err := errors.New("NOT_FOUND: dataset does not exist")
	wrapped := errors.Join(errors.New("other"), err)
	if !isNotFoundError(wrapped) {
		t.Fatalf("expected isNotFoundError to return true for wrapped error: %v", wrapped)
	}
}
