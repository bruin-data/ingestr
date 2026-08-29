package blobstore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/archiveutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type fakeAthenaAPI struct {
	results []*athena.GetQueryResultsOutput
}

func (f *fakeAthenaAPI) StartQueryExecution(context.Context, *athena.StartQueryExecutionInput, ...func(*athena.Options)) (*athena.StartQueryExecutionOutput, error) {
	return &athena.StartQueryExecutionOutput{QueryExecutionId: aws.String("exec-1")}, nil
}

func (f *fakeAthenaAPI) GetQueryExecution(context.Context, *athena.GetQueryExecutionInput, ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error) {
	return &athena.GetQueryExecutionOutput{
		QueryExecution: &athenatypes.QueryExecution{
			Status: &athenatypes.QueryExecutionStatus{State: athenatypes.QueryExecutionStateSucceeded},
		},
	}, nil
}

func (f *fakeAthenaAPI) GetQueryResults(context.Context, *athena.GetQueryResultsInput, ...func(*athena.Options)) (*athena.GetQueryResultsOutput, error) {
	if len(f.results) == 0 {
		return &athena.GetQueryResultsOutput{}, nil
	}
	out := f.results[0]
	f.results = f.results[1:]
	return out, nil
}

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
				provider:                      ProviderS3,
				accessKeyID:                   "AKIAIOSFODNN7EXAMPLE",
				secretAccessKey:               "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				s3FileDiscovery:               s3FileDiscoveryList,
				athenaInventoryBucketColumn:   "bucket",
				athenaInventoryKeyColumn:      "key",
				athenaInventoryModifiedColumn: "last_modified_date",
			},
		},
		{
			name: "S3 with region",
			uri:  "s3://?access_key_id=ABC&secret_access_key=XYZ&region=eu-west-1",
			want: &parsedBlobstoreURI{
				provider:                      ProviderS3,
				accessKeyID:                   "ABC",
				secretAccessKey:               "XYZ",
				region:                        "eu-west-1",
				s3FileDiscovery:               s3FileDiscoveryList,
				athenaInventoryBucketColumn:   "bucket",
				athenaInventoryKeyColumn:      "key",
				athenaInventoryModifiedColumn: "last_modified_date",
			},
		},
		{
			name: "S3 with endpoint URL (Minio)",
			uri:  "s3://?access_key_id=ABC&secret_access_key=XYZ&endpoint_url=http://localhost:9000",
			want: &parsedBlobstoreURI{
				provider:                      ProviderS3,
				accessKeyID:                   "ABC",
				secretAccessKey:               "XYZ",
				endpointURL:                   "http://localhost:9000",
				s3FileDiscovery:               s3FileDiscoveryList,
				athenaInventoryBucketColumn:   "bucket",
				athenaInventoryKeyColumn:      "key",
				athenaInventoryModifiedColumn: "last_modified_date",
			},
		},
		{
			name: "S3 with Athena inventory discovery",
			uri:  "s3://?access_key_id=ABC&secret_access_key=XYZ&region=us-east-1&file_discovery=athena_inventory&athena_inventory_table=inventory_db.inventory_table&athena_results_location=s3://query-results/ingestr&athena_workgroup=primary",
			want: &parsedBlobstoreURI{
				provider:                      ProviderS3,
				accessKeyID:                   "ABC",
				secretAccessKey:               "XYZ",
				region:                        "us-east-1",
				s3FileDiscovery:               s3FileDiscoveryAthenaInventory,
				athenaInventoryTable:          "inventory_db.inventory_table",
				athenaInventoryBucketColumn:   "bucket",
				athenaInventoryKeyColumn:      "key",
				athenaInventoryModifiedColumn: "last_modified_date",
				athenaResultsLocation:         "s3://query-results/ingestr/",
				athenaWorkgroup:               "primary",
			},
		},
		{
			name: "S3 with Athena inventory custom columns",
			uri:  "s3://?file_discovery=athena_inventory&athena_inventory_table=inventory_db.inventory_table&athena_results_location=query-results/ingestr&athena_inventory_bucket_column=b&athena_inventory_key_column=k&athena_inventory_modified_column=m&athena_region=eu-west-1",
			want: &parsedBlobstoreURI{
				provider:                      ProviderS3,
				s3FileDiscovery:               s3FileDiscoveryAthenaInventory,
				athenaInventoryTable:          "inventory_db.inventory_table",
				athenaInventoryBucketColumn:   "b",
				athenaInventoryKeyColumn:      "k",
				athenaInventoryModifiedColumn: "m",
				athenaResultsLocation:         "s3://query-results/ingestr/",
				athenaRegion:                  "eu-west-1",
			},
		},
		{
			name:    "S3 with invalid file discovery",
			uri:     "s3://?file_discovery=magic",
			wantErr: true,
		},
		{
			name:    "S3 Athena inventory requires table",
			uri:     "s3://?file_discovery=athena_inventory&athena_results_location=s3://query-results/ingestr",
			wantErr: true,
		},
		{
			name:    "S3 Athena inventory requires results location",
			uri:     "s3://?file_discovery=athena_inventory&athena_inventory_table=inventory_db.inventory_table",
			wantErr: true,
		},
		{
			name:    "S3 Athena inventory requires qualified table",
			uri:     "s3://?file_discovery=athena_inventory&athena_inventory_table=inventory_table&athena_results_location=s3://query-results/ingestr",
			wantErr: true,
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
			assert.Equal(t, tt.want.s3FileDiscovery, got.s3FileDiscovery)
			assert.Equal(t, tt.want.athenaInventoryTable, got.athenaInventoryTable)
			assert.Equal(t, tt.want.athenaInventoryBucketColumn, got.athenaInventoryBucketColumn)
			assert.Equal(t, tt.want.athenaInventoryKeyColumn, got.athenaInventoryKeyColumn)
			assert.Equal(t, tt.want.athenaInventoryModifiedColumn, got.athenaInventoryModifiedColumn)
			assert.Equal(t, tt.want.athenaResultsLocation, got.athenaResultsLocation)
			assert.Equal(t, tt.want.athenaWorkgroup, got.athenaWorkgroup)
			assert.Equal(t, tt.want.athenaRegion, got.athenaRegion)
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

func TestParseBlobstoreURI_SFTPHostKeyOptions(t *testing.T) {
	parsed, err := parseBlobstoreURI("sftp://user:password@example.com?known_hosts_file=~%2F.ssh%2Fcustom_hosts&host_key_fingerprint=SHA256%3Afirst%2CSHA256%3Asecond&host_key_fingerprint=SHA256%3Athird&insecure_skip_host_key_check=true")
	require.NoError(t, err)

	assert.Equal(t, "~/.ssh/custom_hosts", parsed.sftpKnownHostsFile)
	assert.Equal(t, []string{"SHA256:first", "SHA256:second", "SHA256:third"}, parsed.sftpHostKeyFingerprints)
	assert.True(t, parsed.sftpInsecureSkipHostKeyCheck)

	_, err = parseBlobstoreURI("sftp://user:password@example.com?insecure_skip_host_key_check=perhaps")
	require.EqualError(t, err, `invalid insecure_skip_host_key_check value "perhaps": expected true or false`)
}

func TestParseBlobstoreURI_SFTPPreservesLegacyKeyPassphrase(t *testing.T) {
	parsed, err := parseBlobstoreURI("sftp://user@example.com?key_file=%2Fkey&key_passphrase=legacy-password")
	require.NoError(t, err)
	require.Equal(t, "legacy-password", parsed.sftpKeyPassphrase)
}

func TestParseSFTPPrivateKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	unencryptedBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	require.NoError(t, err)
	_, err = parseSFTPPrivateKey(pem.EncodeToMemory(unencryptedBlock), "unused-password")
	require.NoError(t, err)

	encryptedBlock, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "", []byte("key-password"))
	require.NoError(t, err)
	encryptedKey := pem.EncodeToMemory(encryptedBlock)

	_, err = parseSFTPPrivateKey(encryptedKey, "key-password")
	require.NoError(t, err)

	_, err = parseSFTPPrivateKey(encryptedKey, "wrong-password")
	require.Error(t, err)

	_, err = parseSFTPPrivateKey(encryptedKey, "")
	var passphraseMissing *ssh.PassphraseMissingError
	require.ErrorAs(t, err, &passphraseMissing)
}

func TestSFTPHostKeyCallbackKnownHosts(t *testing.T) {
	trustedKey := newSSHTestPublicKey(t)
	otherKey := newSSHTestPublicKey(t)
	knownHostsFile := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(knownHostsFile, []byte(knownhosts.Line([]string{"sftp.example.com"}, trustedKey)+"\n"), 0o600))

	callback, err := createSFTPHostKeyCallback(&parsedBlobstoreURI{sftpKnownHostsFile: knownHostsFile})
	require.NoError(t, err)
	remote := &net.TCPAddr{}

	require.NoError(t, callback("sftp.example.com:22", remote, trustedKey))

	err = callback("unknown.example.com:22", remote, trustedKey)
	require.ErrorContains(t, err, "unknown SFTP host key")
	require.ErrorContains(t, err, ssh.FingerprintSHA256(trustedKey))

	err = callback("sftp.example.com:22", remote, otherKey)
	require.ErrorContains(t, err, "SFTP host key mismatch")
	require.ErrorContains(t, err, ssh.FingerprintSHA256(otherKey))
}

func TestSFTPHostKeyCallbackFingerprints(t *testing.T) {
	trustedKey := newSSHTestPublicKey(t)
	otherKey := newSSHTestPublicKey(t)
	trustedFingerprint := ssh.FingerprintSHA256(trustedKey)

	callback, err := createSFTPHostKeyCallback(&parsedBlobstoreURI{
		sftpHostKeyFingerprints: []string{ssh.FingerprintSHA256(otherKey), trustedFingerprint},
	})
	require.NoError(t, err)
	require.NoError(t, callback("sftp.example.com:22", nil, trustedKey))

	err = callback("sftp.example.com:22", nil, newSSHTestPublicKey(t))
	require.ErrorContains(t, err, "SFTP host key mismatch")
	require.ErrorContains(t, err, trustedFingerprint)

	_, err = createSFTPHostKeyCallback(&parsedBlobstoreURI{sftpHostKeyFingerprints: []string{"MD5:invalid"}})
	require.ErrorContains(t, err, "expected an OpenSSH SHA256 fingerprint")
}

func TestSFTPHostKeyCallbackPreservesUnconfiguredConnections(t *testing.T) {
	stdout, stderr, mode := output.Current()
	defer output.Init(stdout, stderr, mode)

	var warning bytes.Buffer
	output.Init(&warning, &bytes.Buffer{}, output.ModeText)
	callback, err := createSFTPHostKeyCallback(&parsedBlobstoreURI{})
	require.NoError(t, err)
	require.Contains(t, warning.String(), "continuing without verification for backwards compatibility")
	require.NoError(t, callback("sftp.example.com:22", nil, newSSHTestPublicKey(t)))
}

func TestSFTPInsecureHostKeyCallbackWarns(t *testing.T) {
	stdout, stderr, mode := output.Current()
	defer output.Init(stdout, stderr, mode)

	var warning bytes.Buffer
	output.Init(&warning, &bytes.Buffer{}, output.ModeText)
	callback, err := createSFTPHostKeyCallback(&parsedBlobstoreURI{sftpInsecureSkipHostKeyCheck: true})
	require.NoError(t, err)
	require.Contains(t, warning.String(), "SFTP host key verification is disabled")
	require.NoError(t, callback("sftp.example.com:22", nil, newSSHTestPublicKey(t)))
}

func newSSHTestPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewPublicKey(publicKey)
	require.NoError(t, err)
	return key
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

func TestReadJSONLFileLimitWithByteFlush(t *testing.T) {
	s := NewBlobstoreSource()
	input := strings.NewReader("{\"id\":1}\n{\"id\":2}\n{\"id\":3}\n{\"id\":4}\n{\"id\":5}\n")
	results := make(chan source.RecordBatchResult, 5)
	var totalRows int64
	var batchNum int

	err := s.readJSONLFile(context.Background(), input, results, &totalRows, &batchNum, 3, source.ReadOptions{
		Limit:         3,
		MaxBatchBytes: 1,
	}, blobstoreFileMetadata{})
	require.NoError(t, err)
	close(results)

	var emittedRows int64
	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		emittedRows += result.Batch.NumRows()
		batchRows = append(batchRows, result.Batch.NumRows())
		result.Batch.Release()
	}
	require.Equal(t, int64(3), emittedRows)
	require.Equal(t, int64(3), totalRows)
	require.Equal(t, []int64{1, 1, 1}, batchRows)
}

func TestReadCSVFileLimitWithByteFlush(t *testing.T) {
	s := NewBlobstoreSource()
	input := strings.NewReader("id\n1\n2\n3\n4\n5\n")
	results := make(chan source.RecordBatchResult, 5)
	var totalRows int64
	var batchNum int

	err := s.readCSVFile(context.Background(), input, "", results, &totalRows, &batchNum, 3, source.ReadOptions{
		Limit:         3,
		MaxBatchBytes: 1,
	}, blobstoreFileMetadata{})
	require.NoError(t, err)
	close(results)

	var emittedRows int64
	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		emittedRows += result.Batch.NumRows()
		batchRows = append(batchRows, result.Batch.NumRows())
		result.Batch.Release()
	}
	require.Equal(t, int64(3), emittedRows)
	require.Equal(t, int64(3), totalRows)
	require.Equal(t, []int64{1, 1, 1}, batchRows)
}

func TestProcessZIPReaderCSV(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, entry := range []struct {
		name string
		data string
	}{
		{name: "data/day-1.csv", data: "id,name\n1,Alice\n2,Bob\n"},
		{name: "data/day-2.csv", data: "id,name\n3,Carol\n"},
		{name: "notes.txt", data: "ignored"},
	} {
		member, err := writer.Create(entry.name)
		require.NoError(t, err)
		_, err = member.Write([]byte(entry.data))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	zipReader, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	require.NoError(t, err)

	s := NewBlobstoreSource()
	s.provider = ProviderS3
	s.parsedURI = &parsedBlobstoreURI{archiveLimits: archiveutil.DefaultLimits()}
	results := make(chan source.RecordBatchResult, 4)
	err = s.processZIPReader(
		context.Background(),
		"bucket",
		blobstoreFile{key: "releases/data.zip"},
		zipReader,
		"data/*.csv",
		FormatUnknown,
		"",
		100,
		source.ReadOptions{},
		results,
	)
	require.NoError(t, err)
	close(results)

	var rows int64
	for result := range results {
		require.NoError(t, result.Err)
		require.Equal(t, int64(2), result.Batch.NumCols())
		rows += result.Batch.NumRows()
		result.Batch.Release()
	}
	assert.Equal(t, int64(3), rows)
}

func TestHandlesIncrementality_BlobstoreUsesFrameworkKeyHandling(t *testing.T) {
	s := NewBlobstoreSource()
	assert.False(t, s.HandlesIncrementality())

	s.provider = ProviderS3
	assert.False(t, s.HandlesIncrementality())

	s.provider = ProviderGCS
	assert.False(t, s.HandlesIncrementality())

	s.provider = ProviderSFTP
	assert.False(t, s.HandlesIncrementality())
}

func TestGetTableBlobstoreIncrementalKey(t *testing.T) {
	s := NewBlobstoreSource()
	s.provider = ProviderS3

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "bucket/test.csv"})
	require.NoError(t, err)
	assert.Empty(t, table.IncrementalKey())

	table, err = s.GetTable(context.Background(), source.TableRequest{
		Name:           "bucket/test.csv",
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
	})
	require.NoError(t, err)
	assert.Equal(t, defaultBlobstoreModifiedAtColumn, table.IncrementalKey())

	table, err = s.GetTable(context.Background(), source.TableRequest{
		Name:           "bucket/test.csv",
		IncrementalKey: defaultBlobstoreCreatedAtColumn,
	})
	require.NoError(t, err)
	assert.Equal(t, defaultBlobstoreCreatedAtColumn, table.IncrementalKey())
}

func TestObjectTimestampInInterval(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	before := start.Add(-time.Second)
	justBeforeEnd := end.Add(-time.Nanosecond)
	after := end.Add(time.Second)
	zero := time.Time{}

	assert.True(t, objectTimestampInInterval(&mid, nil, nil))
	assert.True(t, objectTimestampInInterval(&start, &start, &end), "start bound is inclusive")
	assert.True(t, objectTimestampInInterval(&justBeforeEnd, &start, &end), "values before end bound are included")
	assert.False(t, objectTimestampInInterval(&end, &start, &end), "end bound is exclusive")
	assert.False(t, objectTimestampInInterval(&before, &start, &end))
	assert.False(t, objectTimestampInInterval(&after, &start, &end))
	assert.False(t, objectTimestampInInterval(nil, &start, &end))
	assert.False(t, objectTimestampInInterval(&zero, &start, &end))
}

func TestObjectMatchesIncrementalOptionsRequiresReservedKey(t *testing.T) {
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	before := start.Add(-time.Hour)
	after := start.Add(time.Hour)

	assert.True(t, objectMatchesIncrementalOptions(ProviderS3, &before, &before, source.ReadOptions{IntervalStart: &start}))
	assert.True(t, objectMatchesIncrementalOptions(ProviderS3, &before, &before, source.ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &start,
	}))
	assert.False(t, objectMatchesIncrementalOptions(ProviderS3, &before, &after, source.ReadOptions{
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
		IntervalStart:  &start,
	}))
	assert.True(t, objectMatchesIncrementalOptions(ProviderS3, &before, &after, source.ReadOptions{
		IncrementalKey: defaultBlobstoreCreatedAtColumn,
		IntervalStart:  &start,
	}))
	assert.True(t, objectMatchesIncrementalOptions(ProviderSFTP, &before, &before, source.ReadOptions{
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
		IntervalStart:  &start,
	}))
}

func TestBuildS3InventoryQuery(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC+2", 2*60*60))
	end := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	parsed := &parsedBlobstoreURI{
		athenaInventoryTable:          "inventory_db.inventory_table",
		athenaInventoryBucketColumn:   "bucket",
		athenaInventoryKeyColumn:      "key",
		athenaInventoryModifiedColumn: "last_modified_date",
	}

	query, database, err := buildS3InventoryQuery(parsed, "my-bucket", "logs/", source.ReadOptions{
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
		IntervalStart:  &start,
		IntervalEnd:    &end,
	})
	require.NoError(t, err)
	assert.Equal(t, "inventory_db", database)
	assert.Equal(t, `SELECT "key", "last_modified_date" FROM "inventory_db"."inventory_table" WHERE "bucket" = 'my-bucket' AND substr("key", 1, 5) = 'logs/' AND "last_modified_date" >= timestamp '2026-01-02 01:04:05' AND "last_modified_date" < timestamp '2026-01-03 04:05:06'`, query)

	query, database, err = buildS3InventoryQuery(parsed, "my-bucket", "logs/", source.ReadOptions{
		IncrementalKey: defaultBlobstoreCreatedAtColumn,
		IntervalStart:  &start,
		IntervalEnd:    &end,
	})
	require.NoError(t, err)
	assert.Equal(t, "inventory_db", database)
	assert.Equal(t, `SELECT "key", "last_modified_date" FROM "inventory_db"."inventory_table" WHERE "bucket" = 'my-bucket' AND substr("key", 1, 5) = 'logs/' AND "last_modified_date" >= timestamp '2026-01-02 01:04:05' AND "last_modified_date" < timestamp '2026-01-03 04:05:06'`, query)
}

func TestBuildS3InventoryQueryWithoutModifiedIncrementality(t *testing.T) {
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	parsed := &parsedBlobstoreURI{
		athenaInventoryTable:          "inventory_db.inventory_table",
		athenaInventoryBucketColumn:   "bucket",
		athenaInventoryKeyColumn:      "key",
		athenaInventoryModifiedColumn: "last_modified_date",
	}

	query, database, err := buildS3InventoryQuery(parsed, "my-bucket", "", source.ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &start,
	})
	require.NoError(t, err)
	assert.Equal(t, "inventory_db", database)
	assert.Equal(t, `SELECT "key", "last_modified_date" FROM "inventory_db"."inventory_table" WHERE "bucket" = 'my-bucket'`, query)
}

func TestBuildS3InventoryQueryEscapesIdentifiersAndValues(t *testing.T) {
	parsed := &parsedBlobstoreURI{
		athenaInventoryTable:          `inventory_db.inventory"table`,
		athenaInventoryBucketColumn:   `bucket"col`,
		athenaInventoryKeyColumn:      `key"col`,
		athenaInventoryModifiedColumn: "modified",
	}

	query, _, err := buildS3InventoryQuery(parsed, "bucket'1", "logs/o'hare/", source.ReadOptions{})
	require.NoError(t, err)
	assert.Equal(t, `SELECT "key""col", "modified" FROM "inventory_db"."inventory""table" WHERE "bucket""col" = 'bucket''1' AND substr("key""col", 1, 12) = 'logs/o''hare/'`, query)
}

func TestParseAthenaInventoryTime(t *testing.T) {
	tests := []struct {
		value string
		want  time.Time
	}{
		{"2026-01-02T03:04:05Z", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{"2026-01-02 03:04:05", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{"2026-01-02 03:04:05.123456", time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseAthenaInventoryTime(tt.value)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, *got)
		})
	}

	got, err := parseAthenaInventoryTime("")
	require.NoError(t, err)
	assert.Nil(t, got)

	_, err = parseAthenaInventoryTime("not-a-time")
	require.Error(t, err)
}

func TestStreamS3InventoryQueryResultsFiltersRows(t *testing.T) {
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	s := &BlobstoreSource{
		provider: ProviderS3,
		athenaClient: &fakeAthenaAPI{
			results: []*athena.GetQueryResultsOutput{
				{
					ResultSet: &athenatypes.ResultSet{
						Rows: []athenatypes.Row{
							{Data: []athenatypes.Datum{{VarCharValue: aws.String("key")}, {VarCharValue: aws.String("last_modified_date")}}},
							{Data: []athenatypes.Datum{{VarCharValue: aws.String("logs/2026/keep.jsonl")}, {VarCharValue: aws.String("2026-01-02 03:04:05")}}},
							{Data: []athenatypes.Datum{{VarCharValue: aws.String("logs/2026/skip.csv")}, {VarCharValue: aws.String("2026-01-02 03:04:05")}}},
							{Data: []athenatypes.Datum{{VarCharValue: aws.String("logs/2026/old.jsonl")}, {VarCharValue: aws.String("2026-01-01 03:04:05")}}},
						},
					},
				},
			},
		},
	}
	files := make(chan blobstoreFile, 3)

	count, err := s.streamS3InventoryQueryResults(context.Background(), "exec-1", "logs/**/*.jsonl", source.ReadOptions{
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
		IntervalStart:  &start,
	}, files)
	require.NoError(t, err)
	close(files)

	require.Equal(t, 1, count)
	file := <-files
	assert.Equal(t, "logs/2026/keep.jsonl", file.key)
	require.NotNil(t, file.modifiedAt)
	require.NotNil(t, file.createdAt)
	assert.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), *file.modifiedAt)
	assert.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), *file.createdAt)
}

func TestBlobstoreFileMetadata(t *testing.T) {
	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC+2", 2*60*60))
	created := time.Date(2026, 1, 1, 3, 4, 5, 0, time.FixedZone("UTC+2", 2*60*60))
	s := NewBlobstoreSource()
	s.provider = ProviderS3

	metadata := s.fileMetadata(source.ReadOptions{}, "bucket", "data/file.csv", &modified, &created)
	assert.Empty(t, metadata.incrementalKey)
	assert.Nil(t, metadata.incrementalAt)
	assert.Empty(t, metadata.filepathColumn)
	assert.Empty(t, metadata.filepath)

	metadata = s.fileMetadata(source.ReadOptions{IncrementalKey: defaultBlobstoreModifiedAtColumn}, "bucket", "data/file.csv", &modified, &created)
	require.NotNil(t, metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreModifiedAtColumn, metadata.incrementalKey)
	assert.Equal(t, time.UTC, metadata.incrementalAt.Location())
	assert.Equal(t, modified.UTC(), *metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreFilePathColumn, metadata.filepathColumn)
	assert.Equal(t, "s3://bucket/data/file.csv", metadata.filepath)

	metadata = s.fileMetadata(source.ReadOptions{IncrementalKey: defaultBlobstoreCreatedAtColumn}, "bucket", "data/file.csv", &modified, &created)
	require.NotNil(t, metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreCreatedAtColumn, metadata.incrementalKey)
	assert.Equal(t, created.UTC(), *metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreFilePathColumn, metadata.filepathColumn)
	assert.Equal(t, "s3://bucket/data/file.csv", metadata.filepath)

	s.provider = ProviderGCS
	metadata = s.fileMetadata(source.ReadOptions{IncrementalKey: defaultBlobstoreModifiedAtColumn}, "bucket", "data/file.csv", &modified, &created)
	require.NotNil(t, metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreModifiedAtColumn, metadata.incrementalKey)
	assert.Equal(t, defaultBlobstoreFilePathColumn, metadata.filepathColumn)
	assert.Equal(t, "gs://bucket/data/file.csv", metadata.filepath)

	metadata = s.fileMetadata(source.ReadOptions{IncrementalKey: defaultBlobstoreCreatedAtColumn}, "bucket", "data/file.csv", &modified, &created)
	require.NotNil(t, metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreCreatedAtColumn, metadata.incrementalKey)
	assert.Equal(t, created.UTC(), *metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreFilePathColumn, metadata.filepathColumn)
	assert.Equal(t, "gs://bucket/data/file.csv", metadata.filepath)

	s.provider = ProviderS3
	metadata = s.fileMetadata(source.ReadOptions{IncrementalKey: "modified_at"}, "bucket", "data/file.csv", &modified, &created)
	assert.Empty(t, metadata.incrementalKey)
	assert.Nil(t, metadata.incrementalAt)
	assert.Empty(t, metadata.filepathColumn)
	assert.Empty(t, metadata.filepath)

	metadata = s.fileMetadata(source.ReadOptions{
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
		ExcludeColumns: []string{"_INGESTR_SOURCE_FILE_MODIFIED_AT"},
	}, "bucket", "data/file.csv", &modified, &created)
	assert.Empty(t, metadata.incrementalKey)
	assert.Nil(t, metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreFilePathColumn, metadata.filepathColumn)

	metadata = s.fileMetadata(source.ReadOptions{
		IncrementalKey: defaultBlobstoreCreatedAtColumn,
		ExcludeColumns: []string{"_INGESTR_SOURCE_FILE_CREATED_AT"},
	}, "bucket", "data/file.csv", &modified, &created)
	assert.Empty(t, metadata.incrementalKey)
	assert.Nil(t, metadata.incrementalAt)
	assert.Equal(t, defaultBlobstoreFilePathColumn, metadata.filepathColumn)

	metadata = s.fileMetadata(source.ReadOptions{
		IncrementalKey: defaultBlobstoreModifiedAtColumn,
		ExcludeColumns: []string{"_INGESTR_SOURCE_FILE_PATH"},
	}, "bucket", "data/file.csv", &modified, &created)
	assert.Empty(t, metadata.filepathColumn)
	assert.Empty(t, metadata.filepath)

	s.provider = ProviderSFTP
	metadata = s.fileMetadata(source.ReadOptions{IncrementalKey: defaultBlobstoreModifiedAtColumn}, "", "data/file.csv", &modified, &created)
	assert.Empty(t, metadata.incrementalKey)
	assert.Nil(t, metadata.incrementalAt)
	assert.Empty(t, metadata.filepathColumn)
	assert.Empty(t, metadata.filepath)
}

func TestBlobstoreFilepath(t *testing.T) {
	s := NewBlobstoreSource()

	s.provider = ProviderS3
	assert.Equal(t, "s3://bucket/data/file.csv", s.filepath("bucket", "data/file.csv"))
	assert.Equal(t, "s3://bucket/data/file.csv", s.filepath("bucket/", "/data/file.csv"))
	assert.Equal(t, "data/file.csv", s.filepath("", "data/file.csv"))

	s.provider = ProviderGCS
	assert.Equal(t, "gs://bucket/data/file.csv", s.filepath("bucket", "data/file.csv"))
	assert.Equal(t, "gs://bucket/data/file.csv", s.filepath("bucket/", "/data/file.csv"))
}

func TestAddBlobstoreMetadataColumns(t *testing.T) {
	mem := memory.NewGoAllocator()
	idBuilder := array.NewInt64Builder(mem)
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArray := idBuilder.NewArray()
	idBuilder.Release()
	defer idArray.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	input := array.NewRecordBatch(inputSchema, []arrow.Array{idArray}, 2)
	defer input.Release()

	modified := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	output, added, err := addBlobstoreMetadataColumns(input, blobstoreFileMetadata{
		incrementalKey: defaultBlobstoreModifiedAtColumn,
		incrementalAt:  &modified,
		filepathColumn: defaultBlobstoreFilePathColumn,
		filepath:       "s3://bucket/data/file.csv",
	})
	require.NoError(t, err)
	require.True(t, added)
	defer output.Release()

	assert.Equal(t, int64(2), output.NumRows())
	assert.Equal(t, int64(3), output.NumCols())
	assert.Equal(t, defaultBlobstoreModifiedAtColumn, output.Schema().Field(1).Name)
	assert.Equal(t, defaultBlobstoreFilePathColumn, output.Schema().Field(2).Name)

	tsCol, ok := output.Column(1).(*array.Timestamp)
	require.True(t, ok)
	assert.Equal(t, modified, tsCol.Value(0).ToTime(arrow.Microsecond))
	assert.Equal(t, modified, tsCol.Value(1).ToTime(arrow.Microsecond))

	pathCol, ok := output.Column(2).(*array.String)
	require.True(t, ok)
	assert.Equal(t, "s3://bucket/data/file.csv", pathCol.Value(0))
	assert.Equal(t, "s3://bucket/data/file.csv", pathCol.Value(1))
}

func TestAddBlobstoreMetadataColumnsRejectsExistingColumn(t *testing.T) {
	mem := memory.NewGoAllocator()
	builder := array.NewStringBuilder(mem)
	builder.Append("existing")
	existingArray := builder.NewArray()
	builder.Release()
	defer existingArray.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: defaultBlobstoreModifiedAtColumn, Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	input := array.NewRecordBatch(inputSchema, []arrow.Array{existingArray}, 1)
	defer input.Release()

	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	output, added, err := addBlobstoreMetadataColumns(input, blobstoreFileMetadata{
		incrementalKey: defaultBlobstoreModifiedAtColumn,
		incrementalAt:  &modified,
	})
	require.Error(t, err)
	assert.Nil(t, output)
	assert.False(t, added)
	assert.Contains(t, err.Error(), "already exists")
}

func TestAddBlobstoreMetadataColumnsRejectsMetadataColumnConflict(t *testing.T) {
	mem := memory.NewGoAllocator()
	idBuilder := array.NewInt64Builder(mem)
	idBuilder.Append(1)
	idArray := idBuilder.NewArray()
	idBuilder.Release()
	defer idArray.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	input := array.NewRecordBatch(inputSchema, []arrow.Array{idArray}, 1)
	defer input.Release()

	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	output, added, err := addBlobstoreMetadataColumns(input, blobstoreFileMetadata{
		incrementalKey: defaultBlobstoreFilePathColumn,
		incrementalAt:  &modified,
		filepathColumn: defaultBlobstoreFilePathColumn,
		filepath:       "s3://bucket/data/file.csv",
	})
	require.Error(t, err)
	assert.Nil(t, output)
	assert.False(t, added)
	assert.Contains(t, err.Error(), "conflict")
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

func writeWideBlobstoreJSONL(t *testing.T, path string, rows, payloadSize int) {
	t.Helper()

	var sb strings.Builder
	payload := strings.Repeat("x", payloadSize)
	for i := 0; i < rows; i++ {
		line, err := json.Marshal(map[string]any{
			"id":      i,
			"payload": payload,
		})
		require.NoError(t, err)
		sb.Write(line)
		sb.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o644))
}

// runReadJSONLFile drives the real byte-cap read loop in readJSONLFile against a
// local file. Blobstore has no local backend (only S3/GCS/ADLS/SFTP), so the
// download/list layers are bypassed while the batching path under test runs for
// real.
func runReadJSONLFile(t *testing.T, path string, opts source.ReadOptions) (batches int, rows int64) {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	s := NewBlobstoreSource()
	results := make(chan source.RecordBatchResult, 128)

	var totalRows int64
	var batchNum int
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.readJSONLFile(context.Background(), f, results, &totalRows, &batchNum, 10000, opts, blobstoreFileMetadata{})
		close(results)
	}()

	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		batches++
		rows += r.Batch.NumRows()
		r.Batch.Release()
	}
	require.NoError(t, <-errCh)
	return batches, rows
}

func TestBlobstoreByteCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "wide.jsonl")

	const recordCount = 50
	writeWideBlobstoreJSONL(t, jsonlPath, recordCount, 2048)

	// Cap OFF: everything lands in a single batch.
	batchesOff, rowsOff := runReadJSONLFile(t, jsonlPath, source.ReadOptions{MaxBatchBytes: 0})
	require.Equal(t, 1, batchesOff, "cap off must produce exactly one batch")
	require.Equal(t, int64(recordCount), rowsOff)

	// Cap ON (small): same rows must split across more than one batch, no row loss.
	batchesOn, rowsOn := runReadJSONLFile(t, jsonlPath, source.ReadOptions{MaxBatchBytes: 4096})
	require.Greater(t, batchesOn, 1, "small cap must split into more than one batch")
	require.Equal(t, int64(recordCount), rowsOn, "byte cap must not drop rows")
}
