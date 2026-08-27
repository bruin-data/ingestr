package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantWorkspaceID string
		wantToken       string
		wantErr         bool
		errSubstr       string
	}{
		{
			name:            "valid uri",
			uri:             "asana://123456789?access_token=mytoken",
			wantWorkspaceID: "123456789",
			wantToken:       "mytoken",
		},
		{
			name:            "token with special characters",
			uri:             "asana://123456789?access_token=2/abc123:xyz456",
			wantWorkspaceID: "123456789",
			wantToken:       "2/abc123:xyz456",
		},
		{
			name:      "missing access_token",
			uri:       "asana://123456789",
			wantErr:   true,
			errSubstr: "access_token is required",
		},
		{
			name:      "empty access_token",
			uri:       "asana://123456789?access_token=",
			wantErr:   true,
			errSubstr: "access_token is required",
		},
		{
			name:      "missing workspace_id",
			uri:       "asana://?access_token=mytoken",
			wantErr:   true,
			errSubstr: "workspace_id is required",
		},
		{
			name:      "wrong scheme",
			uri:       "https://123456789?access_token=mytoken",
			wantErr:   true,
			errSubstr: "must start with asana://",
		},
		{
			name:      "empty uri",
			uri:       "",
			wantErr:   true,
			errSubstr: "must start with asana://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceID, token, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantWorkspaceID, workspaceID)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Tasks"))
	assert.False(t, isValidTable("WORKSPACES"))
}

func TestAsanaByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"gid": i, "name": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": rows, "next_page": nil})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &AsanaSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "workspaces", source.ReadOptions{MaxBatchBytes: max})
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
