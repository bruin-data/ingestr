package smartsheet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseSmartsheetURI(t *testing.T) {
	tests := []struct {
		name             string
		uri              string
		wantToken        string
		wantSmartsheetID string
		wantErr          string
	}{
		{
			name:      "access token only",
			uri:       "smartsheet://?access_token=tok123",
			wantToken: "tok123",
		},
		{
			name:             "access token and smartsheet_id",
			uri:              "smartsheet://?access_token=tok123&smartsheet_id=987654321",
			wantToken:        "tok123",
			wantSmartsheetID: "987654321",
		},
		{
			name:    "missing access token",
			uri:     "smartsheet://?smartsheet_id=987654321",
			wantErr: "access_token is required",
		},
		{
			name:    "wrong scheme",
			uri:     "https://example.com/?access_token=tok",
			wantErr: "must start with smartsheet://",
		},
		{
			name:    "non-numeric smartsheet_id",
			uri:     "smartsheet://?access_token=tok&smartsheet_id=not-a-number",
			wantErr: "must be a numeric sheet ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok, sid, err := parseSmartsheetURI(tt.uri)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tok != tt.wantToken {
				t.Errorf("token = %q, want %q", tok, tt.wantToken)
			}
			if sid != tt.wantSmartsheetID {
				t.Errorf("smartsheetID = %q, want %q", sid, tt.wantSmartsheetID)
			}
		})
	}
}

func TestResolveSheetID(t *testing.T) {
	tests := []struct {
		name         string
		sourceTable  string
		smartsheetID string
		want         string
		wantErr      string
	}{
		{
			name:        "numeric source-table is used as-is",
			sourceTable: "1234567890",
			want:        "1234567890",
		},
		{
			name:         "numeric source-table wins even when smartsheet_id is set",
			sourceTable:  "1234567890",
			smartsheetID: "999",
			want:         "1234567890",
		},
		{
			name:        "sheet:<id> form returns the id after the colon",
			sourceTable: "sheet:5555",
			want:        "5555",
		},
		{
			name:         "sheet:<id> ignores smartsheet_id",
			sourceTable:  "sheet:5555",
			smartsheetID: "999",
			want:         "5555",
		},
		{
			name:         "literal \"sheet\" falls back to smartsheet_id URI param",
			sourceTable:  "sheet",
			smartsheetID: "777",
			want:         "777",
		},
		{
			name:        "literal \"sheet\" without smartsheet_id errors",
			sourceTable: "sheet",
			wantErr:     "requires the smartsheet_id URI parameter",
		},
		{
			name:        "sheet: with empty id errors",
			sourceTable: "sheet:",
			wantErr:     "missing sheet ID",
		},
		{
			name:         "empty source-table falls back to smartsheet_id",
			sourceTable:  "",
			smartsheetID: "888",
			want:         "888",
		},
		{
			name:    "empty source-table without smartsheet_id errors",
			wantErr: "sheet ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SmartsheetSource{smartsheetID: tt.smartsheetID}
			got, err := s.resolveSheetID(tt.sourceTable)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveSheetID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetTable_PassesResolvedSheetID(t *testing.T) {
	ctx := context.Background()

	s := &SmartsheetSource{accessToken: "tok", smartsheetID: "111"}
	tbl, err := s.GetTable(ctx, source.TableRequest{Name: "sheet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tbl.Name() != "111" {
		t.Errorf("expected sheet ID 111 (from URI via 'sheet' alias), got %s", tbl.Name())
	}
}

func TestSmartsheetByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]map[string]interface{}, 0, 50)
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{
				"id":    i,
				"cells": []map[string]interface{}{{"columnId": 1, "value": wide}},
			})
		}
		body := map[string]interface{}{
			"name":    "sheet",
			"columns": []map[string]interface{}{{"id": 1, "title": "name", "type": "TEXT_NUMBER"}},
			"rows":    rows,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &SmartsheetSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "123", source.ReadOptions{MaxBatchBytes: max})
		if err != nil {
			t.Fatal(err)
		}
		var b, rw int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			b++
			rw += res.Batch.NumRows()
			res.Batch.Release()
		}
		return b, rw
	}

	offB, offR := run(0)
	onB, onR := run(4096)
	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d", offR, onR)
	}
}
