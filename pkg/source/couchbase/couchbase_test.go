package couchbase

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    *couchbaseConfig
		wantErr string
	}{
		{
			name: "basic couchbase URI",
			uri:  "couchbase://user:pass@localhost",
			want: &couchbaseConfig{
				connectionString: "couchbase://localhost",
				username:         "user",
				password:         "pass",
				bucket:           "",
				useSSL:           false,
			},
		},
		{
			name:    "couchbases scheme rejected",
			uri:     "couchbases://user:pass@cb.example.com",
			wantErr: "unsupported scheme",
		},
		{
			name: "with bucket in path",
			uri:  "couchbase://user:pass@localhost/mybucket",
			want: &couchbaseConfig{
				connectionString: "couchbase://localhost",
				username:         "user",
				password:         "pass",
				bucket:           "mybucket",
				useSSL:           false,
			},
		},
		{
			name: "ssl query param upgrades to couchbases",
			uri:  "couchbase://user:pass@localhost?ssl=true",
			want: &couchbaseConfig{
				connectionString: "couchbases://localhost",
				username:         "user",
				password:         "pass",
				bucket:           "",
				useSSL:           true,
			},
		},
		{
			name: "with port",
			uri:  "couchbase://user:pass@localhost:8091",
			want: &couchbaseConfig{
				connectionString: "couchbase://localhost:8091",
				username:         "user",
				password:         "pass",
				bucket:           "",
				useSSL:           false,
			},
		},
		{
			name:    "missing username",
			uri:     "couchbase://:pass@localhost",
			wantErr: "username is required",
		},
		{
			name:    "missing password",
			uri:     "couchbase://user@localhost",
			wantErr: "password is required",
		},
		{
			name:    "unsupported scheme",
			uri:     "postgres://user:pass@localhost",
			wantErr: "unsupported scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name          string
		table         string
		defaultBucket string
		wantBucket    string
		wantScope     string
		wantColl      string
		wantErr       string
	}{
		{
			name:       "three parts",
			table:      "mybucket._default._default",
			wantBucket: "mybucket",
			wantScope:  "_default",
			wantColl:   "_default",
		},
		{
			name:          "two parts with default bucket",
			table:         "_default._default",
			defaultBucket: "mybucket",
			wantBucket:    "mybucket",
			wantScope:     "_default",
			wantColl:      "_default",
		},
		{
			name:    "two parts without default bucket",
			table:   "_default._default",
			wantErr: "table format requires 3 parts",
		},
		{
			name:    "single part",
			table:   "mybucket",
			wantErr: "invalid table format",
		},
		{
			name:    "four parts",
			table:   "a.b.c.d",
			wantErr: "invalid table format",
		},
		{
			name:    "empty string",
			table:   "",
			wantErr: "invalid table format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, scope, coll, err := parseTableName(tt.table, tt.defaultBucket)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantScope, scope)
			assert.Equal(t, tt.wantColl, coll)
		})
	}
}

func TestBuildQuery(t *testing.T) {
	tests := []struct {
		name       string
		bucket     string
		scope      string
		collection string
		opts       source.ReadOptions
		wantQuery  string
		wantParams map[string]interface{}
	}{
		{
			name:       "basic query",
			bucket:     "travel",
			scope:      "_default",
			collection: "_default",
			opts:       source.ReadOptions{},
			wantQuery:  "SELECT META(c).id AS id, c.* FROM `travel`.`_default`.`_default` c",
			wantParams: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, params, err := buildQuery(tt.bucket, tt.scope, tt.collection, tt.opts)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantQuery, query)
			assert.Equal(t, tt.wantParams, params)
		})
	}
}
