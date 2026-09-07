package trino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type recordedTrinoRequest struct {
	method        string
	path          string
	transactionID string
	body          string
}

func TestDeleteInsertTransactional(t *testing.T) {
	tests := []struct {
		name               string
		failInsert         bool
		cancelInsert       bool
		incrementalKeyType schema.DataType
		intervalStart      interface{}
		intervalEnd        interface{}
		wantDeleteContains []string
	}{
		{name: "commit"},
		{name: "rollback after insert failure", failInsert: true},
		{name: "rollback after cancellation", cancelInsert: true},
		{
			name:               "date incremental key casts bounds",
			incrementalKeyType: schema.TypeDate,
			intervalStart:      "2024-01-01",
			intervalEnd:        "2024-01-02",
			wantDeleteContains: []string{`"updated_at" >= CAST(? AS DATE)`, `"updated_at" <= CAST(? AS DATE)`, "USING '2024-01-01', '2024-01-02'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCtx := t.Context()
			cancelQuery := func() {}
			if tt.cancelInsert {
				callCtx, cancelQuery = context.WithCancel(callCtx)
			}
			defer cancelQuery()

			var (
				mu       sync.Mutex
				requests []recordedTrinoRequest
				server   *httptest.Server
			)
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				requestIndex := len(requests)
				requests = append(requests, recordedTrinoRequest{
					method:        r.Method,
					path:          r.URL.Path,
					transactionID: r.Header.Get(transactionIDHeader),
					body:          string(body),
				})
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				switch requestIndex {
				case 0:
					_, _ = fmt.Fprintf(w, `{"id":"start","nextUri":%q,"stats":{"state":"QUEUED"}}`, server.URL+"/start/1")
				case 1:
					w.Header().Set(startedTransactionIDHeader, "tx-123")
					_, _ = io.WriteString(w, `{"id":"start","stats":{"state":"FINISHED"},"updateType":"START TRANSACTION"}`)
				case 2:
					_, _ = io.WriteString(w, `{"id":"delete","stats":{"state":"FINISHED"},"updateType":"DELETE","updateCount":2}`)
				case 3:
					if tt.cancelInsert {
						cancelQuery()
						<-r.Context().Done()
						return
					}
					if tt.failInsert {
						_, _ = io.WriteString(w, `{"id":"insert","stats":{"state":"FAILED"},"error":{"message":"insert failed","errorName":"GENERIC_INTERNAL_ERROR","errorType":"INTERNAL_ERROR"}}`)
						return
					}
					_, _ = io.WriteString(w, `{"id":"insert","stats":{"state":"FINISHED"},"updateType":"INSERT","updateCount":2}`)
				case 4:
					if tt.failInsert || tt.cancelInsert {
						w.Header().Set(clearTransactionIDHeader, "true")
						_, _ = io.WriteString(w, `{"id":"rollback","stats":{"state":"FINISHED"},"updateType":"ROLLBACK"}`)
						return
					}
					_, _ = fmt.Fprintf(w, `{"id":"commit","nextUri":%q,"stats":{"state":"RUNNING"}}`, server.URL+"/commit/1")
				case 5:
					w.Header().Set(clearTransactionIDHeader, "true")
					_, _ = io.WriteString(w, `{"id":"commit","stats":{"state":"FINISHED"},"updateType":"COMMIT"}`)
				default:
					http.Error(w, "unexpected request", http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			serverURL, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			dest := NewTrinoDestination()
			uri := fmt.Sprintf("trino://user@%s/hive/default?explicitPrepare=false", serverURL.Host)
			if err := dest.Connect(t.Context(), uri); err != nil {
				t.Fatalf("Connect() error = %v", err)
			}
			defer func() { _ = dest.Close(t.Context()) }()
			if !dest.SupportsDeleteInsertStrategy() {
				t.Fatal("SupportsDeleteInsertStrategy() = false, want true")
			}

			intervalStart, intervalEnd := interface{}(10), interface{}(20)
			if tt.intervalStart != nil {
				intervalStart, intervalEnd = tt.intervalStart, tt.intervalEnd
			}
			err = dest.DeleteInsertTable(callCtx, destination.DeleteInsertOptions{
				StagingTable:       "stage.events",
				TargetTable:        "prod.events",
				IncrementalKey:     "updated_at",
				IncrementalKeyType: tt.incrementalKeyType,
				IntervalStart:      intervalStart,
				IntervalEnd:        intervalEnd,
				Columns:            []string{"id", "updated_at", "value"},
				PrimaryKeys:        []string{"id"},
			})
			if tt.cancelInsert {
				if err == nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("DeleteInsertTable() error = %v, want context cancellation", err)
				}
			} else if tt.failInsert {
				if err == nil || !strings.Contains(err.Error(), "failed to insert records") {
					t.Fatalf("DeleteInsertTable() error = %v, want insert failure", err)
				}
			} else if err != nil {
				t.Fatalf("DeleteInsertTable() error = %v", err)
			}

			mu.Lock()
			got := append([]recordedTrinoRequest(nil), requests...)
			mu.Unlock()
			wantCount := 6
			if tt.failInsert || tt.cancelInsert {
				wantCount = 5
			}
			if len(got) != wantCount {
				t.Fatalf("request count = %d, want %d: %+v", len(got), wantCount, got)
			}
			for i, request := range got {
				wantTransactionID := "tx-123"
				if i < 2 {
					wantTransactionID = noTransactionID
				}
				if request.transactionID != wantTransactionID {
					t.Errorf("request %d transaction ID = %q, want %q", i, request.transactionID, wantTransactionID)
				}
			}
			if got[0].method != http.MethodPost || got[0].body != "START TRANSACTION READ WRITE" {
				t.Errorf("start request = %+v", got[0])
			}
			if got[1].method != http.MethodGet || got[1].path != "/start/1" {
				t.Errorf("start follow-up request = %+v", got[1])
			}
			wantDeleteContains := tt.wantDeleteContains
			if wantDeleteContains == nil {
				wantDeleteContains = []string{`"updated_at" >= ?`, "USING 10, 20"}
			}
			wantDeleteContains = append(wantDeleteContains, `DELETE FROM "hive"."prod"."events"`)
			for _, want := range wantDeleteContains {
				if !strings.Contains(got[2].body, want) {
					t.Errorf("delete request body = %q, want substring %q", got[2].body, want)
				}
			}
			if !strings.Contains(got[3].body, `INSERT INTO "hive"."prod"."events"`) ||
				!strings.Contains(got[3].body, `PARTITION BY "id" ORDER BY "updated_at" DESC`) {
				t.Errorf("insert request body = %q", got[3].body)
			}
			wantFinish := "COMMIT"
			if tt.failInsert || tt.cancelInsert {
				wantFinish = "ROLLBACK"
			}
			if got[4].body != wantFinish {
				t.Errorf("finish request body = %q, want %q", got[4].body, wantFinish)
			}
		})
	}
}
