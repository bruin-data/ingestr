package sqs

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseSQSURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		check   func(*testing.T, sqsConfig)
	}{
		{
			name: "query endpoint with aliases",
			uri:  "sqs://?endpoint_url=http://localhost:4566&access_key_id=AKID&secret_access_key=SECRET&region=us-east-1&visibility_timeout=45&wait_time_seconds=5",
			check: func(t *testing.T, cfg sqsConfig) {
				if cfg.EndpointURL != "http://localhost:4566" {
					t.Fatalf("EndpointURL = %q", cfg.EndpointURL)
				}
				if cfg.AccessKeyID != "AKID" || cfg.SecretAccessKey != "SECRET" || cfg.Region != "us-east-1" {
					t.Fatalf("unexpected credentials/region: %#v", cfg)
				}
				if cfg.VisibilitySeconds != 45 || cfg.WaitTimeSeconds != 5 {
					t.Fatalf("timeouts = visibility %d wait %d", cfg.VisibilitySeconds, cfg.WaitTimeSeconds)
				}
			},
		},
		{
			name: "host endpoint",
			uri:  "sqs://localhost:4566?aws_access_key_id=AKID&aws_secret_access_key=SECRET&region_name=us-east-1",
			check: func(t *testing.T, cfg sqsConfig) {
				if cfg.EndpointURL != "http://localhost:4566" {
					t.Fatalf("EndpointURL = %q", cfg.EndpointURL)
				}
			},
		},
		{
			name: "default aws credentials",
			uri:  "sqs://?region=us-west-2",
			check: func(t *testing.T, cfg sqsConfig) {
				if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" || cfg.SessionToken != "" {
					t.Fatalf("unexpected static credentials: %#v", cfg)
				}
				if cfg.Region != "us-west-2" {
					t.Fatalf("Region = %q", cfg.Region)
				}
			},
		},
		{
			name:    "wrong scheme",
			uri:     "kafka://localhost:9092",
			wantErr: true,
		},
		{
			name:    "invalid visibility timeout",
			uri:     "sqs://?region=us-east-1&visibility_timeout=0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseSQSURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
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

func TestQueueTableName(t *testing.T) {
	if got := queueTableName("https://sqs.us-east-1.amazonaws.com/123456789012/orders"); got != "orders" {
		t.Fatalf("queueTableName = %q", got)
	}
	if got := queueTableName("orders"); got != "orders" {
		t.Fatalf("queueTableName = %q", got)
	}
}

func TestEndpointFromHostPrivateRanges(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{host: "172.16.0.1:4566", want: "http://172.16.0.1:4566"},
		{host: "172.17.0.1:4566", want: "http://172.17.0.1:4566"},
		{host: "172.31.255.255:4566", want: "http://172.31.255.255:4566"},
		{host: "172.32.0.1:4566", want: "https://172.32.0.1:4566"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := endpointFromHost(tt.host); got != tt.want {
				t.Fatalf("endpointFromHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestGetTableStreamingDefaults(t *testing.T) {
	src := NewSQSSource()

	table, err := src.GetTable(context.Background(), source.TableRequest{
		Name:      "https://sqs.us-east-1.amazonaws.com/123456789012/orders",
		Streaming: true,
	})
	if err != nil {
		t.Fatalf("GetTable returned error: %v", err)
	}

	if table.Strategy() != config.StrategyMerge {
		t.Fatalf("Strategy = %q, want %q", table.Strategy(), config.StrategyMerge)
	}
	if table.IncrementalKey() != streamOrderColumn {
		t.Fatalf("IncrementalKey = %q, want %q", table.IncrementalKey(), streamOrderColumn)
	}
	if pks := table.PrimaryKeys(); len(pks) != 1 || pks[0] != "msg_id" {
		t.Fatalf("PrimaryKeys = %v, want [msg_id]", pks)
	}
}
