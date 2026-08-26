package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseAPIKeyFromURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{
			name:    "valid admin api key",
			uri:     "anthropic://?api_key=sk-ant-admin01-abcdefghijk",
			want:    "sk-ant-admin01-abcdefghijk",
			wantErr: false,
		},
		{
			name:    "missing api_key",
			uri:     "anthropic://",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "anthropic://?api_key=",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-admin api key",
			uri:     "anthropic://?api_key=sk-ant-api01-abcdefghijk",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			uri:     "postgres://?api_key=sk-ant-admin01-abcdefghijk",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAPIKeyFromURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAPIKeyFromURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseAPIKeyFromURI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnthropicSourceSchemes(t *testing.T) {
	src := NewAnthropicSource()
	schemes := src.Schemes()
	if len(schemes) != 1 || schemes[0] != "anthropic" {
		t.Errorf("Schemes() = %v, want [anthropic]", schemes)
	}
}

func TestAnthropicSourceGetTable(t *testing.T) {
	src := NewAnthropicSource()
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "test"})
	if err != nil {
		t.Errorf("GetTable() should not return error, got: %v", err)
	}
	if table == nil {
		t.Error("GetTable() should return table")
	}
	if table.HasKnownSchema() {
		t.Error("HasKnownSchema() = true, want false")
	}
}

func TestFlattenClaudeCodeUsageItem(t *testing.T) {
	input := map[string]interface{}{
		"date":            "2025-01-01T00:00:00Z",
		"organization_id": "org-123",
		"customer_type":   "api",
		"terminal_type":   "vscode",
		"actor": map[string]interface{}{
			"type":          "user_actor",
			"email_address": "test@example.com",
		},
		"core_metrics": map[string]interface{}{
			"num_sessions":                 float64(5),
			"commits_by_claude_code":       float64(10),
			"pull_requests_by_claude_code": float64(2),
			"lines_of_code": map[string]interface{}{
				"added":   float64(100),
				"removed": float64(50),
			},
		},
		"tool_actions": map[string]interface{}{
			"edit_tool": map[string]interface{}{
				"accepted": float64(20),
				"rejected": float64(2),
			},
			"write_tool": map[string]interface{}{
				"accepted": float64(5),
				"rejected": float64(1),
			},
		},
		"model_breakdown": []interface{}{
			map[string]interface{}{
				"model": "claude-sonnet-4",
				"tokens": map[string]interface{}{
					"input":          float64(1000),
					"output":         float64(500),
					"cache_read":     float64(100),
					"cache_creation": float64(50),
				},
				"estimated_cost": map[string]interface{}{
					"amount":   float64(150),
					"currency": "USD",
				},
			},
		},
	}

	result := flattenClaudeCodeUsageItem(input)

	if result["date"] != "2025-01-01T00:00:00Z" {
		t.Errorf("date = %v, want 2025-01-01T00:00:00Z", result["date"])
	}
	if result["actor_type"] != "user_actor" {
		t.Errorf("actor_type = %v, want user_actor", result["actor_type"])
	}
	if result["actor_id"] != "test@example.com" {
		t.Errorf("actor_id = %v, want test@example.com", result["actor_id"])
	}
	if result["num_sessions"] != float64(5) {
		t.Errorf("num_sessions = %v, want 5", result["num_sessions"])
	}
	if result["lines_added"] != float64(100) {
		t.Errorf("lines_added = %v, want 100", result["lines_added"])
	}
	if result["edit_tool_accepted"] != float64(20) {
		t.Errorf("edit_tool_accepted = %v, want 20", result["edit_tool_accepted"])
	}
	if result["total_input_tokens"] != float64(1000) {
		t.Errorf("total_input_tokens = %v, want 1000", result["total_input_tokens"])
	}
	if result["models_used"] != "claude-sonnet-4" {
		t.Errorf("models_used = %v, want claude-sonnet-4", result["models_used"])
	}
}

func TestFlattenUsageReportItem(t *testing.T) {
	input := map[string]interface{}{
		"api_key_id":              "key-123",
		"workspace_id":            "ws-456",
		"model":                   "claude-3-sonnet",
		"service_tier":            "standard",
		"uncached_input_tokens":   float64(1000),
		"output_tokens":           float64(500),
		"cache_read_input_tokens": float64(200),
		"cache_creation": map[string]interface{}{
			"ephemeral_1h_input_tokens": float64(100),
			"ephemeral_5m_input_tokens": float64(50),
		},
	}

	result := flattenUsageReportItem(input, "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")

	if result["bucket_start"] != "2025-01-01T00:00:00Z" {
		t.Errorf("bucket_start = %v, want 2025-01-01T00:00:00Z", result["bucket_start"])
	}
	if result["api_key_id"] != "key-123" {
		t.Errorf("api_key_id = %v, want key-123", result["api_key_id"])
	}
	if result["cache_creation_1h_tokens"] != float64(100) {
		t.Errorf("cache_creation_1h_tokens = %v, want 100", result["cache_creation_1h_tokens"])
	}
}

func TestAnthropicByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	const nRows = 50

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]map[string]interface{}, 0, nRows)
		for i := 0; i < nRows; i++ {
			rows = append(rows, map[string]interface{}{
				// organization_id survives flattenClaudeCodeUsageItem and carries the wide payload.
				"organization_id": wide,
				"date":            "2024-01-01",
				"customer_type":   "api",
				"terminal_type":   "iTerm",
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":     rows,
			"has_more": false,
		})
	}))
	defer srv.Close()

	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	run := func(maxBytes int64) (int64, int64) {
		s := &AnthropicSource{
			apiKey: "dummy",
			client: httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "claude_code_usage", source.ReadOptions{
			IntervalStart: &day,
			IntervalEnd:   &day,
			MaxBatchBytes: maxBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offB, offR := run(0)
	onB, onR := run(4096)

	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != nRows {
		t.Fatalf("row mismatch off=%d on=%d want %d", offR, onR, nRows)
	}
}
