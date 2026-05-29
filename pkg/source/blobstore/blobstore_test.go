package blobstore

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/source"
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

func TestParseBlobstoreURI_AzureDatalake(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    *parsedBlobstoreURI
		wantErr bool
	}{
		{
			name: "ADLS Gen2 with account key",
			uri:  "adls://?account_name=myaccount&account_key=mykey",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzureDatalake,
				accountName: "myaccount",
				accountKey:  "mykey",
			},
		},
		{
			name: "ADLS Gen2 alias",
			uri:  "azdatalake://?account_name=myaccount&account_key=mykey",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzureDatalake,
				accountName: "myaccount",
				accountKey:  "mykey",
			},
		},
		{
			name: "ADLS Gen2 with SAS token",
			uri:  "adlsgen2://?account_name=myaccount&sas_token=sv=2020-08-04",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzureDatalake,
				accountName: "myaccount",
				sasToken:    "sv=2020-08-04",
			},
		},
		{
			name: "ADLS Gen2 with service principal credentials",
			uri:  "adls://?account_name=myaccount&tenant_id=tenant&client_id=client&client_secret=secret",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzureDatalake,
				accountName: "myaccount",
				clientCredentials: adlsutil.ClientCredentials{
					TenantID:     "tenant",
					ClientID:     "client",
					ClientSecret: "secret",
				},
			},
		},
		{
			name: "ABFSS with account in host",
			uri:  "abfss://filesystem@myaccount.dfs.core.windows.net?account_key=mykey",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzureDatalake,
				accountName: "myaccount",
				accountKey:  "mykey",
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
			assert.Equal(t, tt.want.accountName, got.accountName)
			assert.Equal(t, tt.want.accountKey, got.accountKey)
			assert.Equal(t, tt.want.sasToken, got.sasToken)
			assert.Equal(t, tt.want.clientCredentials, got.clientCredentials)
		})
	}
}

func TestParseTablePattern(t *testing.T) {
	tests := []struct {
		name         string
		table        string
		wantBucket   string
		wantPattern  string
		wantFormat   FileFormat
		wantEncoding string
	}{
		// Plain glob/path cases (no hints)
		{"glob in path", "my_bucket/data/*.csv", "my_bucket", "data/*.csv", FormatUnknown, ""},
		{"recursive glob", "my_bucket/**/*.jsonl", "my_bucket", "**/*.jsonl", FormatUnknown, ""},
		{"single file", "bucket/path/file.parquet", "bucket", "path/file.parquet", FormatUnknown, ""},
		{"bucket only defaults to *", "bucket", "bucket", "*", FormatUnknown, ""},
		{"deep recursive glob", "bucket/a/b/c/**/*.csv", "bucket", "a/b/c/**/*.csv", FormatUnknown, ""},

		// Format hints alone
		{"format jsonl", "bucket/logs/event-data#jsonl", "bucket", "logs/event-data", FormatJSONL, ""},
		{"format ndjson alias", "bucket/data#ndjson", "bucket", "data", FormatJSONL, ""},
		{"format csv", "bucket/data.dat#csv", "bucket", "data.dat", FormatCSV, ""},
		{"format parquet", "bucket/file#parquet", "bucket", "file", FormatParquet, ""},
		{"format hint case-insensitive", "bucket/file#CSV", "bucket", "file", FormatCSV, ""},
		{"format hint with glob", "bucket/logs/**/*.log#jsonl", "bucket", "logs/**/*.log", FormatJSONL, ""},
		{"unknown format hint silently ignored", "bucket/file#xml", "bucket", "file", FormatUnknown, ""},

		// Encoding hints alone
		{"encoding only", "bucket/file.csv#encoding=windows-1252", "bucket", "file.csv", FormatUnknown, "windows-1252"},
		{"encoding cp1252 alias", "bucket/file.csv#encoding=cp1252", "bucket", "file.csv", FormatUnknown, "cp1252"},
		{"encoding utf-16le", "bucket/file.csv#encoding=utf-16le", "bucket", "file.csv", FormatUnknown, "utf-16le"},
		{"encoding utf-32le", "bucket/file.csv#encoding=utf-32le", "bucket", "file.csv", FormatUnknown, "utf-32le"},
		{"encoding latin1", "bucket/file.csv#encoding=latin1", "bucket", "file.csv", FormatUnknown, "latin1"},
		{"encoding shift_jis underscore", "bucket/file.csv#encoding=shift_jis", "bucket", "file.csv", FormatUnknown, "shift_jis"},
		{"encoding key case-insensitive", "bucket/file.csv#ENCODING=windows-1252", "bucket", "file.csv", FormatUnknown, "windows-1252"},

		// Combined hints, both orders
		{"format then encoding", "bucket/file.dat#csv,encoding=windows-1252", "bucket", "file.dat", FormatCSV, "windows-1252"},
		{"encoding then format", "bucket/file.dat#encoding=cp1252,csv", "bucket", "file.dat", FormatCSV, "cp1252"},
		{"format and encoding with whitespace", "bucket/file.dat# csv , encoding=windows-1252 ", "bucket", "file.dat", FormatCSV, "windows-1252"},

		// Edge cases for the hint string
		{"empty hint after #", "bucket/file.csv#", "bucket", "file.csv", FormatUnknown, ""},
		{"trailing comma", "bucket/file.csv#csv,", "bucket", "file.csv", FormatCSV, ""},
		{"unknown key with =", "bucket/file.csv#delim=;", "bucket", "file.csv", FormatUnknown, ""},
		{"encoding with empty value", "bucket/file.csv#encoding=", "bucket", "file.csv", FormatUnknown, ""},
		{"three hints, last wins for encoding", "bucket/f#csv,encoding=cp1252,encoding=utf-8", "bucket", "f", FormatCSV, "utf-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, pattern, format, encoding := parseTablePattern(tt.table)
			assert.Equal(t, tt.wantBucket, bucket, "bucket")
			assert.Equal(t, tt.wantPattern, pattern, "pattern")
			assert.Equal(t, tt.wantFormat, format, "format")
			assert.Equal(t, tt.wantEncoding, encoding, "encoding")
		})
	}
}

func TestParseSFTPTablePattern(t *testing.T) {
	tests := []struct {
		name         string
		table        string
		wantPattern  string
		wantFormat   FileFormat
		wantEncoding string
	}{
		// Path normalization (leading slash)
		{"absolute path with slash", "/exports/data.csv", "exports/data.csv", FormatUnknown, ""},
		{"relative path gets leading slash added then trimmed", "exports/data.csv", "exports/data.csv", FormatUnknown, ""},
		{"single file no slash", "data.csv", "data.csv", FormatUnknown, ""},

		// Globs
		{"glob in path", "/exports/*.csv", "exports/*.csv", FormatUnknown, ""},
		{"recursive glob", "/exports/**/*.jsonl", "exports/**/*.jsonl", FormatUnknown, ""},
		{"deep recursive glob", "/var/data/a/b/**/*.parquet", "var/data/a/b/**/*.parquet", FormatUnknown, ""},

		// Format hints
		{"format csv", "/exports/data.dat#csv", "exports/data.dat", FormatCSV, ""},
		{"format jsonl", "/logs/events#jsonl", "logs/events", FormatJSONL, ""},
		{"format ndjson alias", "/logs/events#ndjson", "logs/events", FormatJSONL, ""},
		{"format parquet", "/data/file#parquet", "data/file", FormatParquet, ""},
		{"format hint case-insensitive", "/data/file#CSV", "data/file", FormatCSV, ""},

		// Encoding hints
		{"encoding only", "/exports/data.csv#encoding=windows-1252", "exports/data.csv", FormatUnknown, "windows-1252"},
		{"encoding utf-16le", "/data/file.csv#encoding=utf-16le", "data/file.csv", FormatUnknown, "utf-16le"},

		// Combined hints
		{"format then encoding", "/exports/data.dat#csv,encoding=windows-1252", "exports/data.dat", FormatCSV, "windows-1252"},
		{"encoding then format", "/exports/data.dat#encoding=cp1252,csv", "exports/data.dat", FormatCSV, "cp1252"},
		{"with whitespace", "/exports/data.dat# csv , encoding=cp1252 ", "exports/data.dat", FormatCSV, "cp1252"},

		// Edge cases
		{"empty hint", "/exports/data.csv#", "exports/data.csv", FormatUnknown, ""},
		{"unknown format silently ignored", "/exports/data#xml", "exports/data", FormatUnknown, ""},
		{"unknown key silently ignored", "/exports/data#delim=;", "exports/data", FormatUnknown, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, pattern, format, encoding := parseSFTPTablePattern(tt.table)
			assert.Equal(t, "", bucket, "SFTP bucket should always be empty")
			assert.Equal(t, tt.wantPattern, pattern, "pattern")
			assert.Equal(t, tt.wantFormat, format, "format")
			assert.Equal(t, tt.wantEncoding, encoding, "encoding")
		})
	}
}

func TestParseTableHints(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantFormat   FileFormat
		wantEncoding string
	}{
		{"empty string", "", FormatUnknown, ""},
		{"only commas", ",,,", FormatUnknown, ""},
		{"format csv", "csv", FormatCSV, ""},
		{"format jsonl", "jsonl", FormatJSONL, ""},
		{"format ndjson alias maps to jsonl", "ndjson", FormatJSONL, ""},
		{"format parquet", "parquet", FormatParquet, ""},
		{"unknown bare hint silently ignored", "yaml", FormatUnknown, ""},

		{"encoding only", "encoding=windows-1252", FormatUnknown, "windows-1252"},
		{"encoding empty value", "encoding=", FormatUnknown, ""},
		{"encoding key uppercase", "ENCODING=cp1252", FormatUnknown, "cp1252"},
		{"encoding value preserves case", "encoding=Windows-1252", FormatUnknown, "Windows-1252"},

		{"format then encoding", "csv,encoding=cp1252", FormatCSV, "cp1252"},
		{"encoding then format", "encoding=cp1252,csv", FormatCSV, "cp1252"},
		{"whitespace tolerated", " csv , encoding=cp1252 ", FormatCSV, "cp1252"},
		{"later encoding wins", "encoding=cp1252,encoding=utf-8", FormatUnknown, "utf-8"},
		{"unknown key=value silently ignored", "delim=;", FormatUnknown, ""},
		{"mix of known/unknown", "csv,delim=;,encoding=cp1252", FormatCSV, "cp1252"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, encoding := parseTableHints(tt.input)
			assert.Equal(t, tt.wantFormat, format, "format")
			assert.Equal(t, tt.wantEncoding, encoding, "encoding")
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

func TestAzureDatalakeListDirectory(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{"data/*.csv", "data"},
		{"data/logs/**/*.jsonl", "data/logs"},
		{"data/users.csv", "data"},
		{"users.csv", ""},
		{"**/*.parquet", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := azureDatalakeListDirectory(tt.pattern)
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
	assert.Contains(t, schemes, "adls")
	assert.Contains(t, schemes, "adlsgen2")
	assert.Contains(t, schemes, "azdatalake")
	assert.Contains(t, schemes, "abfs")
	assert.Contains(t, schemes, "abfss")
	assert.Contains(t, schemes, "sftp")
}

func TestBuildAzureDatalakeFilesystemURL(t *testing.T) {
	got := buildAzureDatalakeFilesystemURL("myaccount", "filesystem")
	assert.Equal(t, "https://myaccount.dfs.core.windows.net/filesystem", got)
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
