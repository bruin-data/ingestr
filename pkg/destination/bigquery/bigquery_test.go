package bigquery

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

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
			uri:            "bigquery://test-project?credentials_base64=eyJ0eXBlIjoic2VydmljZV9hY2NvdW50IiwicHJvamVjdF9pZCI6InRlc3QifQ==",
			wantProjectID:  "test-project",
			wantCredJSON:   `{"type":"service_account","project_id":"test"}`,
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

func TestBeginTransaction_NotSupported(t *testing.T) {
	dest := NewBigQueryDestination()
	_, err := dest.BeginTransaction(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "transactions not supported") {
		t.Fatalf("unexpected error: %v", err)
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
		"other_dataset.other_table": otherCh,
		"my_dataset.my_table":       targetCh,
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

	if _, ok := dest.pendingTableErrs["my_dataset.my_table"]; ok {
		t.Fatal("matching pending table state was not cleared")
	}
	if _, ok := dest.pendingTableErrs["other_dataset.other_table"]; !ok {
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
		"my_dataset.my_table": targetCh,
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
		if _, ok := dest.pendingTableErrs["my_dataset.my_table"]; ok {
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

	sql, err := dest.buildAlterColumnTypeRewriteSQL("my_dataset", "my_table", "age", "STRING", meta)
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

func TestBuildMergeSQL(t *testing.T) {
	dest := NewBigQueryDestination()
	dest.projectID = "my-project"

	t.Run("single_pk", func(t *testing.T) {
		sql := dest.buildMergeSQL("target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id"}, []string{"id", "name", "updated_at"}, nil)

		if !contains(sql, "MERGE `my-project`.`target_ds`.`target_tbl` AS t\n") {
			t.Fatalf("sql missing merge header:\n%s", sql)
		}
		if !contains(sql, "USING (SELECT * FROM `my-project`.`staging_ds`.`staging_tbl` QUALIFY ROW_NUMBER() OVER (PARTITION BY `id`) = 1) AS s\n") {
			t.Fatalf("sql missing using clause with dedup:\n%s", sql)
		}
		if !contains(sql, "ON t.`id` = s.`id`\n") {
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
		sql := dest.buildMergeSQL("target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id"}, []string{"id"}, nil)
		if contains(sql, "WHEN MATCHED THEN") {
			t.Fatalf("sql should not include matched update when there are no non-PK columns:\n%s", sql)
		}
		if !contains(sql, "WHEN NOT MATCHED THEN\n") {
			t.Fatalf("sql missing insert clause:\n%s", sql)
		}
	})

	t.Run("with_cast_map", func(t *testing.T) {
		castMap := map[string]string{"day": "STRING"}
		sql := dest.buildMergeSQL("target_ds", "target_tbl", "staging_ds", "staging_tbl", []string{"id", "day"}, []string{"id", "day", "amount"}, castMap)

		if !contains(sql, "t.`day` = CAST(s.`day` AS STRING)") {
			t.Fatalf("sql missing cast in ON clause:\n%s", sql)
		}
		if !contains(sql, "t.`amount` = s.`amount`") {
			t.Fatalf("sql should not cast non-mismatched columns:\n%s", sql)
		}
		if !contains(sql, "CAST(s.`day` AS STRING)") {
			t.Fatalf("sql missing cast in INSERT values:\n%s", sql)
		}
		if !contains(sql, "t.`id` = s.`id`") {
			t.Fatalf("sql should not cast non-mismatched pk:\n%s", sql)
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
