package pubsub

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParsePubSubURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		check   func(*testing.T, pubSubConfig)
	}{
		{
			name: "emulator endpoint",
			uri:  "pubsub://test-project?endpoint=http://localhost:8085&ack_deadline_seconds=30&pull_timeout_seconds=1.5",
			check: func(t *testing.T, cfg pubSubConfig) {
				if cfg.ProjectID != "test-project" {
					t.Fatalf("ProjectID = %q", cfg.ProjectID)
				}
				if cfg.Endpoint != "localhost:8085" {
					t.Fatalf("Endpoint = %q", cfg.Endpoint)
				}
				if cfg.AckDeadlineSeconds != 30 {
					t.Fatalf("AckDeadlineSeconds = %d", cfg.AckDeadlineSeconds)
				}
				if cfg.PullTimeout.String() != "1.5s" {
					t.Fatalf("PullTimeout = %s", cfg.PullTimeout)
				}
			},
		},
		{
			name: "credentials path alias",
			uri:  "pubsub://test-project?credentials_path=/tmp/service-account.json",
			check: func(t *testing.T, cfg pubSubConfig) {
				if cfg.CredentialsFile != "/tmp/service-account.json" {
					t.Fatalf("CredentialsFile = %q", cfg.CredentialsFile)
				}
			},
		},
		{
			name: "base64 credentials",
			uri:  "pubsub://test-project?credentials_base64=" + base64.StdEncoding.EncodeToString([]byte(`{"type":"service_account"}`)),
			check: func(t *testing.T, cfg pubSubConfig) {
				if cfg.CredentialsJSON != `{"type":"service_account"}` {
					t.Fatalf("CredentialsJSON = %q", cfg.CredentialsJSON)
				}
			},
		},
		{
			name: "default application credentials",
			uri:  "pubsub://test-project",
			check: func(t *testing.T, cfg pubSubConfig) {
				if cfg.CredentialsFile != "" || cfg.CredentialsJSON != "" {
					t.Fatalf("unexpected explicit credentials: %#v", cfg)
				}
			},
		},
		{
			name:    "missing project",
			uri:     "pubsub://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "sqs://test-project",
			wantErr: true,
		},
		{
			name:    "invalid ack deadline",
			uri:     "pubsub://test-project?ack_deadline_seconds=0",
			wantErr: true,
		},
		{
			name:    "invalid base64 credentials",
			uri:     "pubsub://test-project?credentials_base64=not-base64",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parsePubSubURI(tt.uri)
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

func TestSubscriptionName(t *testing.T) {
	if got := subscriptionName("proj", "sub"); got != "projects/proj/subscriptions/sub" {
		t.Fatalf("subscriptionName = %q", got)
	}
	full := "projects/other/subscriptions/sub"
	if got := subscriptionName("proj", full); got != full {
		t.Fatalf("subscriptionName = %q", got)
	}
}

func TestGetTableStreamingDefaults(t *testing.T) {
	src := NewPubSubSource()
	src.cfg.ProjectID = "proj"

	table, err := src.GetTable(context.Background(), source.TableRequest{
		Name:      "orders-subscription",
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
