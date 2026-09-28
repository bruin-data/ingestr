package hubspot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/internal/reverseetltest"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/assert"
)

func TestWriteContract(t *testing.T) {
	reverseetltest.Run(t, func(t *testing.T, s reverseetltest.Scenario) reverseetltest.Fixture {
		var mu sync.Mutex
		var effects reverseetltest.Effects
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			reply := func(v any) { assert.NoError(t, json.NewEncoder(w).Encode(v)) }
			if r.URL.Path == "/crm/v3/properties/contacts/email" {
				reply(map[string]any{"hasUniqueValue": true})
				return
			}
			if r.URL.Path == "/crm/v3/objects/contacts" && r.Method == http.MethodGet {
				page := r.URL.Query().Get("after")
				if page == "" {
					effects.Pages = append(effects.Pages, "first")
					reply(map[string]any{"results": []any{
						map[string]any{"id": "alpha", "properties": map[string]string{"email": "reformatted"}},
						map[string]any{"id": "stale-first"},
					}, "paging": map[string]any{"next": map[string]string{"after": "opaque-page-2"}}})
				} else {
					assert.Equal(t, "opaque-page-2", page)
					effects.Pages = append(effects.Pages, "second")
					if s.PageError {
						w.WriteHeader(http.StatusForbidden)
						reply(map[string]string{"message": "page denied"})
						return
					}
					reply(map[string]any{"results": []any{
						map[string]any{"id": "beta", "properties": map[string]string{"email": "beta"}}, map[string]any{"id": "gamma"}, map[string]any{"id": "stale-second"},
					}})
				}
				return
			}
			assert.Equal(t, http.MethodPost, r.Method)
			var body struct {
				Inputs []struct {
					ID         string            `json:"id"`
					Properties map[string]string `json:"properties"`
				} `json:"inputs"`
			}
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				w.WriteHeader(400)
				return
			}
			op := strings.TrimPrefix(r.URL.Path, "/crm/v3/objects/contacts/batch/")
			if s.Reject && op != "read" && op != "archive" {
				for _, input := range body.Inputs {
					if !s.Mixed || input.ID == "beta" || input.Properties["source_key"] == "beta" {
						w.WriteHeader(http.StatusBadRequest)
						reply(map[string]string{"category": "VALIDATION_ERROR", "message": "bad row"})
						return
					}
				}
			}
			var results []any
			for _, input := range body.Inputs {
				key := input.ID
				if op == "create" {
					key = input.Properties["source_key"]
				}
				switch op {
				case "read":
				case "create":
					effects.Created = append(effects.Created, key)
				case "upsert":
					effects.Upserted = append(effects.Upserted, key)
				case "update":
					effects.Updated = append(effects.Updated, key)
				case "archive":
					effects.Deleted = append(effects.Deleted, key)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(400)
					return
				}
				results = append(results, map[string]any{"id": key, "properties": map[string]string{"email": key}})
			}
			if op == "archive" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			reply(map[string]any{"status": "COMPLETE", "results": results})
		}))
		t.Cleanup(srv.Close)
		table := "contacts?id_property=email"
		if s.Strategy == "append" {
			table = "contacts"
		}
		return reverseetltest.Fixture{Writer: connectTest(t, srv.URL), Options: destination.WriteOptions{Table: table, PrimaryKeys: []string{"source_key"}}, Snapshot: func() reverseetltest.Effects { mu.Lock(); defer mu.Unlock(); return effects }}
	})
}
