package iceberg

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseIcebergConfigCloudStorageShorthand(t *testing.T) {
	tests := []struct {
		name          string
		uri           string
		wantWarehouse string
		wantLocation  string
		wantProps     map[string]string
	}{
		{
			name: "gcs",
			uri: "iceberg+rest://catalog.internal:8181?catalog_use_ssl=true&storage=gcs&bucket=company-lake&prefix=prod" +
				"&endpoint=localhost:4443&use_ssl=false&gcs_key_path=/var/run/gcp.json&gcs_credential_type=service_account" +
				"&gcs_use_json_api=true&table_path={namespace}/{table}",
			wantWarehouse: "gs://company-lake/prod/",
			wantLocation:  "gs://company-lake/prod/{namespace}/{table}",
			wantProps: map[string]string{
				"type":           "rest",
				"uri":            "https://catalog.internal:8181",
				"gcs.endpoint":   "http://localhost:4443",
				"gcs.keypath":    "/var/run/gcp.json",
				"gcs.credtype":   "service_account",
				"gcs.usejsonapi": "true",
			},
		},
		{
			name: "azure shared key",
			uri: "iceberg+sqlite:///tmp/catalog.db?storage=azure&container=warehouse&account_name=devstoreaccount1" +
				"&account_key=secret&prefix=prod&endpoint=127.0.0.1:10000&use_ssl=false&table_path={namespace}/{table}",
			wantWarehouse: "abfss://warehouse@devstoreaccount1.dfs.core.windows.net/prod/",
			wantLocation:  "abfss://warehouse@devstoreaccount1.dfs.core.windows.net/prod/{namespace}/{table}",
			wantProps: map[string]string{
				"type":                              "sql",
				"adls.auth.shared-key.account.name": "devstoreaccount1",
				"adls.auth.shared-key.account.key":  "secret",
				"adls.endpoint":                     "127.0.0.1:10000",
				"adls.protocol":                     "http",
			},
		},
		{
			name:          "gcs inline external account",
			uri:           "iceberg+sqlite:///:memory:?storage=gcs&bucket=company-lake&gcs_json_key=%7B%22type%22%3A%22external_account%22%7D&gcs_credential_type=external_account",
			wantWarehouse: "gs://company-lake/",
			wantProps: map[string]string{
				"gcs.jsonkey":  `{"type":"external_account"}`,
				"gcs.credtype": "external_account",
			},
		},
		{
			name:          "azure sas with wasbs",
			uri:           "iceberg+sqlite:///:memory:?storage=adls&bucket=warehouse&account_name=companylake&adls_scheme=wasbs&sas_token=sig%3Dsecret",
			wantWarehouse: "wasbs://warehouse@companylake.blob.core.windows.net/",
			wantProps: map[string]string{
				"adls.sas-token.companylake.blob.core.windows.net": "sig=secret",
			},
		},
		{
			name:          "azure user assigned managed identity",
			uri:           "iceberg+sqlite:///:memory:?storage=azure&container=warehouse&account_name=companylake&managed_identity=true&client_id=client-id",
			wantWarehouse: "abfss://warehouse@companylake.dfs.core.windows.net/",
			wantProps: map[string]string{
				"adls.auth.managed-identity.enabled": "true",
				"adls.client-id":                     "client-id",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseIcebergConfig(tt.uri)
			require.NoError(t, err)
			require.Equal(t, tt.wantWarehouse, cfg.Properties["warehouse"])
			require.Equal(t, tt.wantLocation, cfg.TableLocation)
			for key, value := range tt.wantProps {
				require.Equal(t, value, cfg.Properties[key], key)
			}
		})
	}
}

func TestParseIcebergConfigRESTCatalogPresets(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantProps map[string]string
		absent    []string
	}{
		{
			name: "nessie",
			uri:  "iceberg+nessie://nessie.internal:19120?nessie_branch=experiments&nessie_warehouse=sales",
			wantProps: map[string]string{
				"type": "rest",
				"uri":  "http://nessie.internal:19120/iceberg/experiments%7Csales",
			},
		},
		{
			name: "nessie branch containing slash",
			uri:  "iceberg+nessie://nessie.internal:19120?nessie_branch=feature%2Forders&storage=s3&bucket=client-io&region=us-east-1",
			wantProps: map[string]string{
				"type":      "rest",
				"uri":       "http://nessie.internal:19120/iceberg/feature%2Forders",
				"s3.region": "us-east-1",
			},
			absent: []string{"warehouse"},
		},
		{
			name: "polaris",
			uri: "iceberg+polaris://catalog.example.com?warehouse=production&oauth_client_id=client" +
				"&oauth_client_secret=secret&scope=PRINCIPAL_ROLE:ALL&polaris_realm=POLARIS",
			wantProps: map[string]string{
				"type":                 "rest",
				"uri":                  "https://catalog.example.com/api/catalog",
				"warehouse":            "production",
				"credential":           "client:secret",
				"scope":                "PRINCIPAL_ROLE:ALL",
				"header.Polaris-Realm": "POLARIS",
			},
		},
		{
			name: "s3 tables",
			uri: "iceberg+s3tables://?region=us-east-1&warehouse=" +
				url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"),
			wantProps: map[string]string{
				"type":                "rest",
				"uri":                 "https://s3tables.us-east-1.amazonaws.com/iceberg",
				"rest.sigv4-enabled":  "true",
				"rest.signing-region": "us-east-1",
				"rest.signing-name":   "s3tables",
				"client.region":       "us-east-1",
				"s3.region":           "us-east-1",
				"glue.region":         "us-east-1",
				"warehouse":           "arn:aws:s3tables:us-east-1:123456789012:bucket/analytics",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseIcebergConfig(tt.uri)
			require.NoError(t, err)
			for key, value := range tt.wantProps {
				require.Equal(t, value, cfg.Properties[key], key)
			}
			for _, key := range tt.absent {
				require.NotContains(t, cfg.Properties, key)
			}
		})
	}
}

func TestNamedRESTCatalogRetainsStorageRootForPurgeJournals(t *testing.T) {
	cfg, err := parseIcebergConfig("iceberg+nessie://nessie.internal:19120?storage=s3&bucket=client-io&prefix=warehouse")
	require.NoError(t, err)
	require.Empty(t, cfg.Properties["warehouse"], "named REST warehouse must not be replaced by a storage URI")
	require.Equal(t, "s3://client-io/warehouse/", cfg.PurgeJournalRoot)
}

func TestParseIcebergConfigSpecializedSchemeCatalogTypeCannotBeOverridden(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		matching string
		conflict string
	}{
		{name: "sqlite", uri: "iceberg+sqlite:///:memory:?warehouse_path=/tmp/warehouse", matching: "sqlite", conflict: "rest"},
		{name: "postgres", uri: "iceberg+postgres://user@catalog.internal/db?warehouse_path=/tmp/warehouse", matching: "postgres", conflict: "rest"},
		{name: "rest", uri: "iceberg+rest://catalog.internal:8181?warehouse_path=/tmp/warehouse", matching: "rest", conflict: "hadoop"},
		{name: "nessie", uri: "iceberg+nessie://nessie.internal:19120?nessie_branch=main", matching: "nessie", conflict: "hadoop"},
		{name: "polaris", uri: "iceberg+polaris://catalog.example.com?warehouse=production", matching: "polaris", conflict: "hadoop"},
		{
			name: "s3 tables",
			uri: "iceberg+s3tables://?region=us-east-1&warehouse=" +
				url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"),
			matching: "s3tables",
			conflict: "hadoop",
		},
		{name: "glue", uri: "iceberg+glue://?region=us-east-1&warehouse_path=/tmp/warehouse", matching: "glue", conflict: "hadoop"},
		{name: "hive", uri: "iceberg+hive://catalog.internal:9083?warehouse_path=/tmp/warehouse", matching: "hive", conflict: "hadoop"},
		{name: "hadoop", uri: "iceberg+hadoop:///tmp/warehouse?warehouse=/tmp/warehouse", matching: "hadoop", conflict: "rest"},
		{name: "sql", uri: "iceberg+sql://?uri=file:/tmp/catalog.db&sql.driver=sqlite&sql.dialect=sqlite&warehouse_path=/tmp/warehouse", matching: "sql", conflict: "rest"},
	}

	for _, tt := range tests {
		for _, key := range []string{"catalog", "type"} {
			t.Run(tt.name+" matching "+key, func(t *testing.T) {
				cfg, err := parseIcebergConfig(tt.uri + "&" + key + "=" + tt.matching)
				require.NoError(t, err)
				require.Equal(t, normalizeCatalogType(tt.matching), cfg.Properties["type"])
			})

			t.Run(tt.name+" conflicting "+key, func(t *testing.T) {
				_, err := parseIcebergConfig(tt.uri + "&" + key + "=" + tt.conflict)
				require.ErrorContains(t, err, "conflicts with the URI scheme")
				require.ErrorContains(t, err, key+"=")
			})
		}
	}
}

func TestParseIcebergConfigGenericSchemeAcceptsCatalogTypeSelection(t *testing.T) {
	for _, tt := range []struct {
		name string
		uri  string
		want string
	}{
		{name: "catalog alias", uri: "iceberg://?catalog=sqlite", want: "sql"},
		{name: "type", uri: "iceberg://?type=hadoop", want: "hadoop"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseIcebergConfig(tt.uri)
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.Properties["type"])
		})
	}
}

func TestParseIcebergConfigRejectsInvalidPlatformShorthand(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr string
	}{
		{
			name:    "gcs credential type",
			uri:     "iceberg+sqlite:///:memory:?storage=gcs&bucket=lake&gcs_credential_type=magic",
			wantErr: `invalid gcs_credential_type "magic"`,
		},
		{
			name:    "gcs credentials conflict",
			uri:     "iceberg+sqlite:///:memory:?storage=gcs&bucket=lake&gcs_json_key={}&gcs_key_path=/key.json",
			wantErr: "mutually exclusive",
		},
		{
			name:    "missing object location",
			uri:     "iceberg+rest://catalog?storage=gcs",
			wantErr: "requires bucket/container or an explicit warehouse",
		},
		{
			name:    "azure account",
			uri:     "iceberg+sqlite:///:memory:?storage=azure&container=lake",
			wantErr: "account_name is required",
		},
		{
			name:    "azure authentication conflict",
			uri:     "iceberg+sqlite:///:memory:?storage=azure&container=lake&account_name=account&account_key=key&managed_identity=true",
			wantErr: "mutually exclusive",
		},
		{
			name:    "endpoint TLS conflict",
			uri:     "iceberg+sqlite:///:memory:?storage=gcs&bucket=lake&endpoint=https://localhost:4443&use_ssl=false",
			wantErr: "conflicts with use_ssl=false",
		},
		{
			name:    "oauth pair",
			uri:     "iceberg+polaris://catalog.example.com?warehouse=prod&oauth_client_id=client",
			wantErr: "must be provided together",
		},
		{
			name:    "nessie prefix",
			uri:     "iceberg+nessie://nessie.internal:19120?catalog_prefix=main",
			wantErr: "selected with nessie_branch",
		},
		{
			name:    "nessie warehouse property",
			uri:     "iceberg+nessie://nessie.internal:19120?warehouse=s3://lake/path",
			wantErr: "selected with nessie_warehouse",
		},
		{
			name:    "polaris warehouse",
			uri:     "iceberg+polaris://catalog.example.com",
			wantErr: "requires warehouse",
		},
		{
			name:    "polaris storage bucket is not catalog name",
			uri:     "iceberg+polaris://catalog.example.com?storage=s3&bucket=data-lake",
			wantErr: "requires warehouse",
		},
		{
			name:    "s3 tables region",
			uri:     "iceberg+s3tables://?warehouse=arn:aws:s3tables:us-east-1:123:bucket/lake",
			wantErr: "requires region",
		},
		{
			name:    "s3 tables warehouse",
			uri:     "iceberg+s3tables://?region=us-east-1&warehouse=s3://lake",
			wantErr: "requires an S3 Tables bucket ARN",
		},
		{
			name:    "s3 tables region mismatch",
			uri:     "iceberg+s3tables://?region=us-east-1&warehouse=arn:aws:s3tables:eu-west-1:123456789012:bucket/analytics",
			wantErr: `warehouse region "eu-west-1" does not match signing region "us-east-1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseIcebergConfig(tt.uri)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestParseIcebergConfigPartitionSpec(t *testing.T) {
	cfg, err := parseIcebergConfig("iceberg+sqlite:///:memory:?partition_spec=id,day(created_at),bucket%5B16%5D(customer_id)")
	require.NoError(t, err)
	require.Equal(t, "id,day(created_at),bucket[16](customer_id)", cfg.PartitionSpec)
	require.NotContains(t, cfg.Properties, "partition_spec")
	require.NotContains(t, cfg.Properties, "partition-spec")
}
