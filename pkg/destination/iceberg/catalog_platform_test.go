package iceberg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestLoadIcebergCatalogS3TablesSignsConfigWithURIStaticCredentials(t *testing.T) {
	authHeader := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"defaults":{},"overrides":{}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	query := url.Values{}
	query.Set("uri", server.URL)
	query.Set("region", "us-east-1")
	query.Set("warehouse", "arn:aws:s3tables:us-east-1:123456789012:bucket/analytics")
	query.Set("access_key_id", "uri-access-key")
	query.Set("secret_access_key", "uri-secret-key")
	cfg, err := parseIcebergConfig("iceberg+s3tables://?" + query.Encode())
	require.NoError(t, err)

	_, err = loadIcebergCatalog(context.Background(), cfg)
	require.NoError(t, err)

	authorization := <-authHeader
	require.Contains(t, authorization, "AWS4-HMAC-SHA256 Credential=uri-access-key/")
	require.Contains(t, authorization, "/us-east-1/s3tables/aws4_request")
	require.NotContains(t, authorization, "uri-secret-key")
}

func TestS3TablesDropNamespaceUsesURIStaticCredentialsWithoutAmbientAWS(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/missing-credentials")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/missing-config")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	authHeader := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"defaults":{},"overrides":{}}`))
	})
	mux.HandleFunc("/v1/namespaces/analytics", func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	query := url.Values{}
	query.Set("uri", server.URL)
	query.Set("region", "us-east-1")
	query.Set("warehouse", "arn:aws:s3tables:us-east-1:123456789012:bucket/analytics")
	query.Set("access_key_id", "uri-cleanup-key")
	query.Set("secret_access_key", "uri-cleanup-secret")
	dest := NewDestination()
	require.NoError(t, dest.Connect(context.Background(), "iceberg+s3tables://?"+query.Encode()))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	require.NoError(t, dest.DropNamespace(context.Background(), "analytics"))

	authorization := <-authHeader
	require.Contains(t, authorization, "AWS4-HMAC-SHA256 Credential=uri-cleanup-key/")
	require.Contains(t, authorization, "/us-east-1/s3tables/aws4_request")
	require.NotContains(t, authorization, "uri-cleanup-secret")
}

func TestLoadIcebergCatalogRESTSendsOAuthTokenAsBearerOnConfig(t *testing.T) {
	authHeader := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"defaults":{},"overrides":{}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	query := url.Values{}
	query.Set("uri", server.URL)
	query.Set("warehouse", "s3://catalog-warehouse")
	query.Set("oauth_token", "uri-oauth-token")
	cfg, err := parseIcebergConfig("iceberg+rest://?" + query.Encode())
	require.NoError(t, err)

	_, err = loadIcebergCatalog(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, "Bearer uri-oauth-token", <-authHeader)
}

func TestParseIcebergConfigRejectsInvalidS3TablesBucketNames(t *testing.T) {
	for _, bucket := range []string{
		"data_lake", "-data-lake", "data-lake-", "xn--data", "sthree-data", "amzn-s3-demo-data", "aws-data",
		"data-s3alias", "data--ol-s3", "data--x-s3", "data--table-s3",
	} {
		t.Run(bucket, func(t *testing.T) {
			query := url.Values{}
			query.Set("region", "us-east-1")
			query.Set("warehouse", "arn:aws:s3tables:us-east-1:123456789012:bucket/"+bucket)

			_, err := parseIcebergConfig("iceberg+s3tables://?" + query.Encode())
			require.ErrorContains(t, err, "requires an S3 Tables bucket ARN")
		})
	}
}

func TestPrepareTableRejectsInvalidS3TablesIdentifiersBeforeCatalogMutation(t *testing.T) {
	dest := newHadoopDestination(t)
	dest.cfg.Properties["rest.signing-name"] = "s3tables"
	ctx := context.Background()

	tests := []struct {
		name    string
		table   string
		column  string
		wantErr string
	}{
		{name: "nested namespace", table: "company.analytics.events", column: "event_id", wantErr: "requires a single-level namespace"},
		{name: "uppercase namespace", table: "Analytics.events", column: "event_id", wantErr: "namespace and table names must be lowercase"},
		{name: "uppercase table", table: "analytics.Events", column: "event_id", wantErr: "namespace and table names must be lowercase"},
		{name: "namespace starts underscore", table: "_analytics.events", column: "event_id", wantErr: "must begin with a letter or number"},
		{name: "table starts underscore", table: "analytics._events", column: "event_id", wantErr: "must begin with a letter or number"},
		{name: "hyphen", table: "analytics.foo-bar", column: "event_id", wantErr: "may contain only lowercase letters, numbers, and underscores"},
		{name: "reserved namespace", table: "aws_analytics.events", column: "event_id", wantErr: "must not start with reserved prefix"},
		{name: "too long table", table: "analytics." + strings.Repeat("a", 256), column: "event_id", wantErr: "between 1 and 255 characters"},
		{name: "uppercase column", table: "analytics.events", column: "Event_ID", wantErr: "column names must be lowercase"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dest.PrepareTable(ctx, destination.PrepareOptions{
				Table: tt.table,
				Schema: &schema.TableSchema{Columns: []schema.Column{{
					Name: tt.column, DataType: schema.TypeInt64, Nullable: false,
				}}},
			})
			require.ErrorContains(t, err, tt.wantErr)
		})
	}

	namespaces, err := dest.catalog.ListNamespaces(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, namespaces, "identifier validation must run before namespace creation")
}

func TestS3TablesManagedStagingPolicyUsesValidNamespace(t *testing.T) {
	dest := newHadoopDestination(t)
	dest.cfg.Properties["rest.signing-name"] = "s3tables"

	policy := dest.ManagedStagingPolicy()
	require.Equal(t, destination.ReplaceStagingManagedSchema, policy.DefaultPlacement)
	require.Equal(t, s3TablesManagedStagingNamespace, policy.DefaultManagedSchema)
	require.NoError(t, validateS3TablesName("namespace", policy.DefaultManagedSchema))
	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table: policy.DefaultManagedSchema + ".cdc_state",
		Schema: &schema.TableSchema{Columns: []schema.Column{{
			Name: "event_id", DataType: schema.TypeString, Nullable: false,
		}}},
	}))
}

func TestIcebergManagedStagingPolicyKeepsDefaultNamespace(t *testing.T) {
	policy := NewDestination().ManagedStagingPolicy()
	require.Equal(t, destination.ReplaceStagingManagedSchema, policy.DefaultPlacement)
	require.Equal(t, defaultManagedStagingNamespace, policy.DefaultManagedSchema)
}

func TestPrepareTableRejectsNestedHiveNamespaceBeforeCatalogMutation(t *testing.T) {
	dest := newHadoopDestination(t)
	dest.cfg.Properties["type"] = "hive"
	ctx := context.Background()

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: "company.analytics.events",
		Schema: &schema.TableSchema{Columns: []schema.Column{{
			Name: "event_id", DataType: schema.TypeInt64, Nullable: false,
		}}},
	})
	require.ErrorContains(t, err, "Hive catalog requires a single-level namespace")
	namespaces, listErr := dest.catalog.ListNamespaces(ctx, nil)
	require.NoError(t, listErr)
	require.Empty(t, namespaces, "Hive namespace validation must precede every catalog mutation")
}

func TestValidateS3TablesIdentifierAcceptsDocumentedBoundaries(t *testing.T) {
	cfg := icebergConfig{Properties: map[string]string{"rest.signing-name": "s3tables"}}
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "event_id", DataType: schema.TypeInt64}}}

	for _, ident := range []icebergtable.Identifier{
		{"a", "0"},
		{strings.Repeat("n", 255), strings.Repeat("t", 255)},
		{"analytics", "aws_events"},
	} {
		require.NoError(t, validateS3TablesIdentifier(cfg, ident, tableSchema), ident)
	}
}
