package blobstore

import (
	"testing"

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
		{
			name: "S3 with layout",
			uri:  "s3://?access_key_id=ABC&secret_access_key=XYZ&layout={table_name}.{ext}",
			want: &parsedBlobstoreURI{
				provider:        ProviderS3,
				accessKeyID:     "ABC",
				secretAccessKey: "XYZ",
				layout:          "{table_name}.{ext}",
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
			assert.Equal(t, tt.want.layout, got.layout)
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

func TestParseBlobstoreURI_Azure(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    *parsedBlobstoreURI
		wantErr bool
	}{
		{
			name: "Azure with account key",
			uri:  "az://?account_name=myaccount&account_key=mykey",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzure,
				accountName: "myaccount",
				accountKey:  "mykey",
			},
		},
		{
			name: "Azure with azure scheme",
			uri:  "azure://?account_name=myaccount&account_key=mykey",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzure,
				accountName: "myaccount",
				accountKey:  "mykey",
			},
		},
		{
			name: "Azure with SAS token",
			uri:  "az://?account_name=myaccount&sas_token=sv=2020-08-04",
			want: &parsedBlobstoreURI{
				provider:    ProviderAzure,
				accountName: "myaccount",
				sasToken:    "sv=2020-08-04",
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
		})
	}
}

func TestParseBlobstoreURI_UnsupportedScheme(t *testing.T) {
	_, err := parseBlobstoreURI("ftp://bucket/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported blobstore scheme")
}

func TestParseBucketAndPath(t *testing.T) {
	tests := []struct {
		table      string
		wantBucket string
		wantPath   string
	}{
		{"my_bucket/records", "my_bucket", "records"},
		{"my_bucket/path/to/data", "my_bucket", "path/to/data"},
		{"my_bucket", "my_bucket", ""},
		{"bucket-name/", "bucket-name", ""},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			bucket, path := parseBucketAndPath(tt.table)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

func TestRenderLayout(t *testing.T) {
	d := &BlobstoreDestination{
		tableName: "my_bucket/records",
		layout:    "{table_name}/{load_id}.{file_id}.{ext}",
	}

	result := d.renderLayout("abc123", 0)
	assert.Equal(t, "records/abc123.0.parquet", result)

	d.layout = "{table_name}.{ext}"
	result = d.renderLayout("abc123", 0)
	assert.Equal(t, "records.parquet", result)

	d.tableName = "public.users"
	result = d.renderLayout("abc123", 0)
	assert.Equal(t, "users.parquet", result)
}

func TestSchemes(t *testing.T) {
	d := NewBlobstoreDestination()
	schemes := d.Schemes()

	assert.Contains(t, schemes, "s3")
	assert.Contains(t, schemes, "gs")
	assert.Contains(t, schemes, "gcs")
	assert.Contains(t, schemes, "az")
	assert.Contains(t, schemes, "azure")
}

func TestStrategySupport(t *testing.T) {
	d := NewBlobstoreDestination()

	assert.True(t, d.SupportsReplaceStrategy())
	assert.True(t, d.SupportsAppendStrategy())
	assert.False(t, d.SupportsMergeStrategy())
	assert.False(t, d.SupportsDeleteInsertStrategy())
}
