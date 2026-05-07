package blobstore

import (
	"context"
	"testing"

	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBlobstoreURI_S3(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    *parsedBlobstoreURI
		wantErr bool
	}{
		{
			name: "basic S3 with credentials",
			uri:  "s3://?access_key_id=AKIAIOSFODNN7EXAMPLE&secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			want: &parsedBlobstoreURI{
				provider:        ProviderS3,
				accessKeyID:     "AKIAIOSFODNN7EXAMPLE",
				secretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
		},
		{
			name: "S3 with region",
			uri:  "s3://?access_key_id=ABC&secret_access_key=XYZ&region=eu-west-1",
			want: &parsedBlobstoreURI{
				provider:        ProviderS3,
				accessKeyID:     "ABC",
				secretAccessKey: "XYZ",
				region:          "eu-west-1",
			},
		},
		{
			name: "S3 with endpoint URL (Minio)",
			uri:  "s3://?access_key_id=ABC&secret_access_key=XYZ&endpoint_url=http://localhost:9000",
			want: &parsedBlobstoreURI{
				provider:        ProviderS3,
				accessKeyID:     "ABC",
				secretAccessKey: "XYZ",
				endpointURL:     "http://localhost:9000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBlobstoreURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.provider, got.provider)
			assert.Equal(t, tt.want.accessKeyID, got.accessKeyID)
			assert.Equal(t, tt.want.secretAccessKey, got.secretAccessKey)
			assert.Equal(t, tt.want.region, got.region)
			assert.Equal(t, tt.want.endpointURL, got.endpointURL)
		})
	}
}

func TestParseBlobstoreURI_GCS(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    *parsedBlobstoreURI
		wantErr bool
	}{
		{
			name: "GCS with gs scheme",
			uri:  "gs://",
			want: &parsedBlobstoreURI{
				provider: ProviderGCS,
			},
		},
		{
			name: "GCS with gcs scheme",
			uri:  "gcs://",
			want: &parsedBlobstoreURI{
				provider: ProviderGCS,
			},
		},
		{
			name: "GCS with credentials file",
			uri:  "gs://?credentials_file=/path/to/credentials.json",
			want: &parsedBlobstoreURI{
				provider:        ProviderGCS,
				credentialsFile: "/path/to/credentials.json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBlobstoreURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.provider, got.provider)
			assert.Equal(t, tt.want.credentialsFile, got.credentialsFile)
		})
	}
}

func TestParseTablePattern(t *testing.T) {
	tests := []struct {
		table       string
		wantBucket  string
		wantPattern string
		wantFormat  FileFormat
	}{
		{"my_bucket/data/*.csv", "my_bucket", "data/*.csv", FormatUnknown},
		{"my_bucket/**/*.jsonl", "my_bucket", "**/*.jsonl", FormatUnknown},
		{"bucket/path/file.parquet", "bucket", "path/file.parquet", FormatUnknown},
		{"bucket", "bucket", "*", FormatUnknown},
		{"bucket/logs/event-data#jsonl", "bucket", "logs/event-data", FormatJSONL},
		{"bucket/data.dat#csv", "bucket", "data.dat", FormatCSV},
		{"bucket/file#parquet", "bucket", "file", FormatParquet},
		{"bucket/logs/**/*.log#jsonl", "bucket", "logs/**/*.log", FormatJSONL},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			bucket, pattern, format := parseTablePattern(tt.table)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantPattern, pattern)
			assert.Equal(t, tt.wantFormat, format)
		})
	}
}

func TestExtractPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{"data/*.csv", "data/"},
		{"**/*.csv", ""},
		{"path/to/file.csv", "path/to/file.csv"},
		{"data/logs/*.jsonl", "data/logs/"},
		{"*.parquet", ""},
		{"myFolder/**/*.jsonl", "myFolder/"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := extractPrefix(tt.pattern)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesGlobPattern(t *testing.T) {
	tests := []struct {
		key     string
		pattern string
		want    bool
	}{
		{"data/file.csv", "data/*.csv", true},
		{"data/file.parquet", "data/*.csv", false},
		{"data/subdir/file.csv", "data/*.csv", false},
		{"data/subdir/file.csv", "data/**/*.csv", true},
		{"data/a/b/c/file.csv", "data/**/*.csv", true},
		{"logs/2024/01/events.jsonl", "logs/**/*.jsonl", true},
		{"file.parquet", "*.parquet", true},
		{"users.parquet", "users.parquet", true},
		{"path/users.parquet", "path/users.parquet", true},
	}

	for _, tt := range tests {
		t.Run(tt.key+"_"+tt.pattern, func(t *testing.T) {
			got := matchesGlobPattern(tt.key, tt.pattern)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectFileFormat(t *testing.T) {
	tests := []struct {
		key  string
		hint FileFormat
		want FileFormat
	}{
		{"data.csv", FormatUnknown, FormatCSV},
		{"data.CSV", FormatUnknown, FormatCSV},
		{"data.jsonl", FormatUnknown, FormatJSONL},
		{"data.ndjson", FormatUnknown, FormatJSONL},
		{"data.parquet", FormatUnknown, FormatParquet},
		{"data.csv.gz", FormatUnknown, FormatCSV},
		{"data.jsonl.gz", FormatUnknown, FormatJSONL},
		{"data.parquet.gz", FormatUnknown, FormatParquet},
		{"data.dat", FormatUnknown, FormatUnknown},
		{"data.dat", FormatJSONL, FormatJSONL},
		{"data.txt", FormatCSV, FormatCSV},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := detectFileFormat(tt.key, tt.hint)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsGzipped(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"data.csv", false},
		{"data.csv.gz", true},
		{"data.CSV.GZ", true},
		{"data.jsonl.gz", true},
		{"data.gz", true},
		{"gzfile.csv", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isGzipped(tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSchemes(t *testing.T) {
	s := NewBlobstoreSource()
	schemes := s.Schemes()

	assert.Contains(t, schemes, "s3")
	assert.Contains(t, schemes, "gs")
	assert.Contains(t, schemes, "gcs")
	assert.Contains(t, schemes, "az")
	assert.Contains(t, schemes, "azure")
}

func TestGetTable(t *testing.T) {
	s := NewBlobstoreSource()
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "bucket/test.csv"})
	assert.NoError(t, err)
	assert.NotNil(t, table)
	assert.False(t, table.HasKnownSchema())
}

func TestParseCSVValue(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"", nil},
		{"true", true},
		{"false", false},
		{"TRUE", true},
		{"FALSE", false},
		{"123", int64(123)},
		{"45.67", float64(45.67)},
		{"hello", "hello"},
		{"  spaced  ", "spaced"},
		{"2024-01-15T10:30:00Z", "2024-01-15T10:30:00Z"},
		{"2024-01-15", "2024-01-15"},
		{"2024-01-15 10:30:00", "2024-01-15 10:30:00"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseCSVValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestItemsToArrowRecordWithSchema(t *testing.T) {
	items := []map[string]interface{}{
		{"name": "Alice", "age": float64(30), "active": true},
		{"name": "Bob", "age": float64(25), "active": false},
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, nil)
	require.NoError(t, err)
	defer record.Release()

	assert.Equal(t, int64(2), record.NumRows())
	assert.Equal(t, int64(3), record.NumCols())
}

func TestItemsToArrowRecordWithExclude(t *testing.T) {
	items := []map[string]interface{}{
		{"name": "Alice", "age": float64(30), "secret": "xyz"},
		{"name": "Bob", "age": float64(25), "secret": "abc"},
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, []string{"secret"})
	require.NoError(t, err)
	defer record.Release()

	assert.Equal(t, int64(2), record.NumRows())
	assert.Equal(t, int64(2), record.NumCols())

	hasSecret := false
	for i := 0; i < int(record.NumCols()); i++ {
		if record.Schema().Field(i).Name == "secret" {
			hasSecret = true
		}
	}
	assert.False(t, hasSecret)
}
