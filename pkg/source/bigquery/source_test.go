package bigquery

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReadAPI(t *testing.T) {
	tests := []struct {
		value   string
		want    ReadAPI
		wantErr bool
	}{
		{value: "", want: ReadAPIStorage},
		{value: "storage", want: ReadAPIStorage},
		{value: "rest", want: ReadAPIREST},
		{value: "REST", wantErr: true},
		{value: "rest ", wantErr: true},
		{value: "grpc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseReadAPI(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), `invalid BigQuery read_api "`+tt.value+`"`)
				assert.Contains(t, err.Error(), `must be "storage" or "rest"`)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildConnectionStringReadAPI(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want ReadAPI
	}{
		{name: "default", uri: "bigquery://my-project?location=EU", want: ReadAPIStorage},
		{name: "storage", uri: "bigquery://my-project?read_api=storage&location=EU", want: ReadAPIStorage},
		{name: "rest", uri: "bigquery://my-project?location=EU&read_api=rest", want: ReadAPIREST},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDialect()
			connStr, err := d.BuildConnectionString(tt.uri)
			require.NoError(t, err)
			assert.Equal(t, tt.want, d.readAPI)
			assert.Equal(t, "my-project", d.projectID)
			assert.Equal(t, "EU", d.location)
			assert.NotContains(t, connStr, "read_api")
			assert.Equal(t, "driver=bigquery;adbc.bigquery.sql.project_id=my-project;adbc.bigquery.sql.auth_type=adbc.bigquery.sql.auth_type.auth_bigquery;adbc.bigquery.sql.location=EU", connStr)
		})
	}
}

func TestBuildConnectionStringRejectsInvalidReadAPI(t *testing.T) {
	_, err := NewDialect().BuildConnectionString("bigquery://my-project?read_api=fast")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid BigQuery read_api "fast"`)
}

func TestSourceConnectRejectsInvalidReadAPI(t *testing.T) {
	s := NewSource()
	err := s.Connect(context.Background(), "bigquery://my-project?read_api=storage_api")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid BigQuery read_api "storage_api"`)
	assert.Nil(t, s.active, "an invalid read_api must fail before any connection is attempted")
	require.NoError(t, s.Close(context.Background()))
}

func TestSourceConnectRESTUsesRESTSource(t *testing.T) {
	s := NewSource()
	err := s.Connect(context.Background(), "bigquery://my-project?read_api=rest&credentials_base64="+fakeServiceAccountBase64(t))
	require.NoError(t, err)
	defer func() { _ = s.Close(context.Background()) }()

	rest, ok := s.active.(*restSource)
	require.True(t, ok, "read_api=rest must not use the ADBC source, got %T", s.active)
	assert.Equal(t, ReadAPIREST, rest.dialect.readAPI)
}

func TestStorageClientRefusedInRESTMode(t *testing.T) {
	d := NewDialect()
	_, err := d.BuildConnectionString("bigquery://my-project?read_api=rest")
	require.NoError(t, err)

	_, err = d.createStorageClient(context.Background())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "read_api=rest"), err.Error())

	_, err = d.ReadWithStorageAPI(context.Background(), "dataset.table", source.ReadOptions{})
	require.Error(t, err)
}

func fakeServiceAccountBase64(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	creds, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "my-project",
		"private_key_id": "test",
		"private_key":    string(keyPEM),
		"client_email":   "test@my-project.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(creds)
}
