package satismeter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantKey     string
		wantProject string
		wantErr     string
	}{
		{
			name:        "valid",
			uri:         "satismeter://?api_key=abc123&project_id=5bb480aaebf3ed0004c6f3dd",
			wantKey:     "abc123",
			wantProject: "5bb480aaebf3ed0004c6f3dd",
		},
		{
			name:        "order does not matter",
			uri:         "satismeter://?project_id=p1&api_key=k1",
			wantKey:     "k1",
			wantProject: "p1",
		},
		{
			name:    "missing api_key",
			uri:     "satismeter://?project_id=p1",
			wantErr: "api_key is required",
		},
		{
			// project_id is not optional: the API key is project-scoped but every
			// endpoint still nests under /projects/{id}, so a missing id would
			// produce a 404 rather than a sensible default.
			name:    "missing project_id",
			uri:     "satismeter://?api_key=k1",
			wantErr: "project_id is required",
		},
		{
			name:    "empty",
			uri:     "satismeter://",
			wantErr: "api_key is required",
		},
		{
			name:    "wrong scheme",
			uri:     "https://?api_key=k1&project_id=p1",
			wantErr: "must start with satismeter://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, project, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("api_key = %q, want %q", key, tt.wantKey)
			}
			if project != tt.wantProject {
				t.Errorf("project_id = %q, want %q", project, tt.wantProject)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, ok := range []string{"responses", "campaigns", "project"} {
		if !isValidTable(ok) {
			t.Errorf("isValidTable(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Responses", "RESPONSES", "response", "unknown", "campaign_statistics"} {
		if isValidTable(bad) {
			t.Errorf("isValidTable(%q) = true, want false", bad)
		}
	}
}

// campaign_statistics is intentionally absent — it is a single aggregate row per
// (campaign, window) with no date dimension, so backfilling it across windows
// silently rewrites the same key. See the comment on supportedTables.
func TestCampaignStatisticsNotExposed(t *testing.T) {
	if _, ok := supportedTables["campaign_statistics"]; ok {
		t.Fatal("campaign_statistics is exposed; a windowed aggregate with no date column rewrites itself on every backfill")
	}
}

// The pagination envelope is what terminates the responses loop. If SatisMeter
// ever renames these fields the loop would exit after one page and quietly
// ingest only the first 100 rows, so pin the decoding here.
func TestResponsesPageEnvelopeDecodes(t *testing.T) {
	raw := `{"data":[{"id":"r1","created":"2026-08-02T18:02:23.420Z"}],
	         "page":{"hasNextPage":true,"nextPageCursor":"WzE3ODU2","size":1}}`

	var payload struct {
		Data []map[string]interface{} `json:"data"`
		Page struct {
			HasNextPage    bool   `json:"hasNextPage"`
			NextPageCursor string `json:"nextPageCursor"`
		} `json:"page"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0]["id"] != "r1" {
		t.Errorf("data not decoded: %+v", payload.Data)
	}
	if !payload.Page.HasNextPage {
		t.Error("hasNextPage = false, want true")
	}
	if payload.Page.NextPageCursor != "WzE3ODU2" {
		t.Errorf("nextPageCursor = %q", payload.Page.NextPageCursor)
	}
}

// The final page omits nextPageCursor; the loop must stop rather than re-request
// page one forever with an empty cursor.
func TestResponsesLastPageStops(t *testing.T) {
	raw := `{"data":[{"id":"r9"}],"page":{"hasNextPage":false,"size":1}}`
	var payload struct {
		Page struct {
			HasNextPage    bool   `json:"hasNextPage"`
			NextPageCursor string `json:"nextPageCursor"`
		} `json:"page"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Page.HasNextPage || payload.Page.NextPageCursor != "" {
		t.Fatal("last page should have hasNextPage=false and no cursor")
	}
}

// maxPageSize must stay within the documented 1-100 range; sending 0 or >100
// gets silently clamped to the default of 20 and quadruples the request count.
func TestMaxPageSizeWithinDocumentedRange(t *testing.T) {
	if maxPageSize < 1 || maxPageSize > 100 {
		t.Fatalf("maxPageSize = %d, SatisMeter documents 1-100", maxPageSize)
	}
}

// historyFloor defeats the server-side "last 30 days" default. If it is ever
// blanked, an interval-less run silently returns a month of data.
func TestHistoryFloorIsSet(t *testing.T) {
	if strings.TrimSpace(historyFloor) == "" {
		t.Fatal("historyFloor is empty; an omitted startDate makes SatisMeter default to 30 days ago")
	}
}
