package amplitude

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		name      string
		uri       string
		want      amplitudeCredentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid URI, default region",
			uri:  "amplitude://?api_key=key123&secret_key=secret456",
			want: amplitudeCredentials{apiKey: "key123", secretKey: "secret456", region: "us"},
		},
		{
			name: "valid URI, eu region",
			uri:  "amplitude://?api_key=key123&secret_key=secret456&region=eu",
			want: amplitudeCredentials{apiKey: "key123", secretKey: "secret456", region: "eu"},
		},
		{
			name: "region is case-insensitive",
			uri:  "amplitude://?api_key=k&secret_key=s&region=EU",
			want: amplitudeCredentials{apiKey: "k", secretKey: "s", region: "eu"},
		},
		{
			name:      "missing api_key",
			uri:       "amplitude://?secret_key=secret456",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "missing secret_key",
			uri:       "amplitude://?api_key=key123",
			wantErr:   true,
			errSubstr: "secret_key is required",
		},
		{
			name:      "invalid region",
			uri:       "amplitude://?api_key=k&secret_key=s&region=jp",
			wantErr:   true,
			errSubstr: "invalid region",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?api_key=k&secret_key=s",
			wantErr:   true,
			errSubstr: "must start with amplitude://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Events"))
	assert.False(t, isValidTable("EVENTS"))
}

func TestSplitEventWindows(t *testing.T) {
	base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("windows are hour-disjoint and cover the full range", func(t *testing.T) {
		start := base
		end := base.Add(9 * time.Hour) // 10 inclusive hours
		windows := splitEventWindows(start, end, 4)

		require.NotEmpty(t, windows)
		assert.Equal(t, start, windows[0][0])
		assert.Equal(t, end, windows[len(windows)-1][1])

		for i, w := range windows {
			assert.False(t, w[1].Before(w[0]), "window %d end before start", i)
			if i > 0 {
				// next start is exactly one hour after previous end -> no overlap, no gap
				assert.Equal(t, windows[i-1][1].Add(time.Hour), w[0], "window %d overlaps/gaps with previous", i)
			}
		}
	})

	t.Run("caps window count at available hours", func(t *testing.T) {
		windows := splitEventWindows(base, base.Add(2*time.Hour), 8)
		assert.Len(t, windows, 3)
	})

	t.Run("single window when range is one hour or n<=1", func(t *testing.T) {
		assert.Len(t, splitEventWindows(base, base, 4), 1)
		assert.Len(t, splitEventWindows(base, base.Add(5*time.Hour), 1), 1)
	})
}

func TestJsonUseNumber(t *testing.T) {
	data := []byte(`{"id": 2033513821949367296, "amount": 3.14}`)
	var result map[string]interface{}
	require.NoError(t, jsonUseNumber(data, &result))

	id, ok := result["id"].(json.Number)
	require.True(t, ok, "id should be json.Number, got %T", result["id"])
	i, err := id.Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(2033513821949367296), i)
}

func TestAmplitudeByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	const nRows = 50

	// Build the export archive once: a single NDJSON entry with nRows wide events.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("export/events.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < nRows; i++ {
		line, _ := json.Marshal(map[string]interface{}{
			"event_type": "test",
			"blob":       wide,
			"id":         i,
		})
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := zipBuf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	start := time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	run := func(maxBytes int64) (int64, int64) {
		s := &AmplitudeSource{
			exportClient: httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "events", source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
			Parallelism:   1,
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
