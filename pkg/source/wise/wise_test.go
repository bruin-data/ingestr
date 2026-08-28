package wise

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{
			name: "valid URI",
			uri:  "wise://?api_key=test-key-123",
			want: "test-key-123",
		},
		{
			name: "valid URI without question mark",
			uri:  "wise://api_key=test-key-123",
			want: "test-key-123",
		},
		{
			name:    "missing api_key",
			uri:     "wise://?other=value",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "wise://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://localhost",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "wise://?api_key=",
			wantErr: true,
		},
		{
			name:    "just question mark",
			uri:     "wise://?",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBalanceIntervalFiltering(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		balances      []map[string]interface{}
		intervalStart *time.Time
		intervalEnd   *time.Time
		wantCount     int
	}{
		{
			name: "no interval returns all",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-01-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			wantCount: 2,
		},
		{
			name: "filters before interval start",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-06-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "filters after interval end",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2025-03-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-09-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "missing modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "null modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": nil},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "non-string modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": 12345},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "unparseable modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "not-a-date"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "millisecond format is accepted",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2025-03-01T12:30:45.123Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "only interval start set",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-06-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
				{"id": "3", "modificationTime": "2026-01-01T00:00:00Z"},
			},
			intervalStart: &start,
			wantCount:     2,
		},
		{
			name: "only interval end set",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-06-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
				{"id": "3", "modificationTime": "2026-01-01T00:00:00Z"},
			},
			intervalEnd: &end,
			wantCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterBalances(tt.balances, tt.intervalStart, tt.intervalEnd)
			assert.Equal(t, tt.wantCount, len(got), "filtered balance count mismatch")
		})
	}
}

func TestWiseByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]map[string]interface{}, 0, 50)
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": i, "name": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &WiseSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "profiles", source.ReadOptions{MaxBatchBytes: max})
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
