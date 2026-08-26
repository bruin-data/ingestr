package chess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

// TestChessByteCap proves the MaxBatchBytes flush loop in readGames: with the cap
// off every game arrives in a single batch, and with a small cap the same games are
// split across multiple batches without losing any rows.
func TestChessByteCap(t *testing.T) {
	const rowCount = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/games/archives") {
			archiveURL := "http://" + r.Host + "/pub/player/tester/games/2024/01"
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"archives": []string{archiveURL},
			})
			return
		}
		games := make([]map[string]interface{}, 0, rowCount)
		for i := 0; i < rowCount; i++ {
			games = append(games, map[string]interface{}{
				"url":  fmt.Sprintf("game-%d", i),
				"blob": wide,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"games": games})
	}))
	defer srv.Close()

	run := func(maxBytes int64) (int64, int64) {
		s := &ChessSource{
			players: []string{"tester"},
			client:  httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.readTable(context.Background(), s.readGames, source.ReadOptions{MaxBatchBytes: maxBytes})
		if err != nil {
			t.Fatalf("readTable: %v", err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatalf("batch error: %v", res.Err)
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offBatches, offRows := run(0)
	onBatches, onRows := run(4096)

	if offBatches != 1 {
		t.Fatalf("cap-off batches=%d want 1", offBatches)
	}
	if onBatches <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onBatches)
	}
	if offRows != onRows || offRows != rowCount {
		t.Fatalf("row mismatch off=%d on=%d want %d", offRows, onRows, rowCount)
	}
}
