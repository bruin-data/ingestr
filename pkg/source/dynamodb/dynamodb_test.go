package dynamodb

import (
	"testing"

	"github.com/bruin-data/ingestr/internal/dynamodbutil"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		check   func(*testing.T, *dynamodbutil.Config)
	}{
		{
			name: "valid URI with region in host",
			uri:  "dynamodb://dynamodb.us-east-1.amazonaws.com?access_key_id=AKID&secret_access_key=SECRET",
			check: func(t *testing.T, cfg *dynamodbutil.Config) {
				if cfg.Region != "us-east-1" {
					t.Errorf("Region = %q, want %q", cfg.Region, "us-east-1")
				}
				if cfg.AccessKeyID != "AKID" {
					t.Errorf("AccessKeyID = %q, want %q", cfg.AccessKeyID, "AKID")
				}
				if cfg.SecretAccessKey != "SECRET" {
					t.Errorf("SecretAccessKey = %q, want %q", cfg.SecretAccessKey, "SECRET")
				}
				if cfg.EndpointURL != "https://dynamodb.us-east-1.amazonaws.com" {
					t.Errorf("EndpointURL = %q, want %q", cfg.EndpointURL, "https://dynamodb.us-east-1.amazonaws.com")
				}
			},
		},
		{
			name: "valid URI with eu-west-1 region",
			uri:  "dynamodb://dynamodb.eu-west-1.amazonaws.com?access_key_id=AKID&secret_access_key=SECRET",
			check: func(t *testing.T, cfg *dynamodbutil.Config) {
				if cfg.Region != "eu-west-1" {
					t.Errorf("Region = %q, want %q", cfg.Region, "eu-west-1")
				}
			},
		},
		{
			name: "local endpoint with port",
			uri:  "dynamodb://localhost:8000?access_key_id=AKID&secret_access_key=SECRET&region=us-east-1",
			check: func(t *testing.T, cfg *dynamodbutil.Config) {
				if cfg.EndpointURL != "http://localhost:8000" {
					t.Errorf("EndpointURL = %q, want %q", cfg.EndpointURL, "http://localhost:8000")
				}
				if cfg.Region != "us-east-1" {
					t.Errorf("Region = %q, want %q", cfg.Region, "us-east-1")
				}
			},
		},
		{
			name:    "missing access_key_id",
			uri:     "dynamodb://dynamodb.us-east-1.amazonaws.com?secret_access_key=SECRET",
			wantErr: true,
		},
		{
			name:    "missing secret_access_key",
			uri:     "dynamodb://dynamodb.us-east-1.amazonaws.com?access_key_id=AKID",
			wantErr: true,
		},
		{
			name:    "missing region",
			uri:     "dynamodb://localhost:8000?access_key_id=AKID&secret_access_key=SECRET",
			wantErr: true,
		},
		{
			name: "region in query param",
			uri:  "dynamodb://?access_key_id=AKID&secret_access_key=SECRET&region=ap-northeast-1",
			check: func(t *testing.T, cfg *dynamodbutil.Config) {
				if cfg.Region != "ap-northeast-1" {
					t.Errorf("Region = %q, want %q", cfg.Region, "ap-northeast-1")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := dynamodbutil.ParseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
