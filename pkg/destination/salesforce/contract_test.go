package salesforce

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/internal/reverseetltest"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/assert"
)

func TestRESTWriteContract(t *testing.T) {
	reverseetltest.Run(t, func(t *testing.T, s reverseetltest.Scenario) reverseetltest.Fixture {
		var mu sync.Mutex
		var effects reverseetltest.Effects
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			mu.Lock()
			defer mu.Unlock()
			reply := func(v any) { assert.NoError(t, json.NewEncoder(w).Encode(v)) }
			switch {
			case r.URL.Path == "/services/data/v59.0/":
				reply(map[string]any{})
				return true
			case strings.HasSuffix(r.URL.Path, "/describe"):
				reply(map[string]any{"fields": []any{map[string]any{"name": "Ext__c", "type": "string", "externalId": true}}})
				return true
			case strings.Contains(r.URL.Path, "/query"):
				if strings.Contains(r.URL.Query().Get("q"), " WHERE ") {
					var records []any
					for _, key := range []string{"alpha", "beta", "gamma"} {
						if strings.Contains(r.URL.Query().Get("q"), "'"+key+"'") {
							records = append(records, map[string]string{"Id": key, "Ext__c": key})
						}
					}
					reply(map[string]any{"done": true, "records": records})
					return true
				}
				if strings.HasSuffix(r.URL.Path, "/query") {
					effects.Pages = append(effects.Pages, "first")
					reply(map[string]any{"done": false, "nextRecordsUrl": "/services/data/v59.0/query/opaque-page-2", "records": []any{
						map[string]string{"Id": "alpha", "Ext__c": "reformatted"}, map[string]string{"Id": "stale-first"},
					}})
				} else {
					assert.Equal(t, "/services/data/v59.0/query/opaque-page-2", r.URL.Path)
					effects.Pages = append(effects.Pages, "second")
					if s.PageError {
						w.WriteHeader(http.StatusForbidden)
						reply([]any{map[string]string{"errorCode": "INSUFFICIENT_ACCESS", "message": "page denied"}})
						return true
					}
					reply(map[string]any{"done": true, "records": []any{map[string]string{"Id": "beta", "Ext__c": "beta"}, map[string]string{"Id": "gamma"}, map[string]string{"Id": "stale-second"}}})
				}
				return true
			case strings.Contains(r.URL.Path, "/composite/sobjects"):
				var results []any
				if r.Method == http.MethodDelete {
					for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
						effects.Deleted = append(effects.Deleted, id)
						results = append(results, map[string]any{"id": id, "success": true, "errors": []any{}})
					}
				} else {
					var payload struct {
						AllOrNone bool             `json:"allOrNone"`
						Records   []map[string]any `json:"records"`
					}
					assert.NoError(t, json.Unmarshal([]byte(body), &payload))
					assert.False(t, payload.AllOrNone)
					for _, record := range payload.Records {
						key, _ := record["Ext__c"].(string)
						if r.Method == http.MethodPost {
							key, _ = record["source_key"].(string)
						}
						if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/sobjects") {
							key, _ = record["Id"].(string)
						}
						if s.Reject && (!s.Mixed || key == "beta") {
							results = append(results, map[string]any{"success": false, "errors": []any{map[string]string{"statusCode": "FIELD_CUSTOM_VALIDATION_EXCEPTION", "message": "bad row"}}})
							continue
						}
						switch {
						case r.Method == http.MethodPost:
							effects.Created = append(effects.Created, key)
						case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/Contact/Ext__c"):
							effects.Upserted = append(effects.Upserted, key)
						case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/sobjects"):
							effects.Updated = append(effects.Updated, key)
						default:
							t.Errorf("unexpected write: %s %s", r.Method, r.URL.Path)
						}
						results = append(results, map[string]any{"id": key, "success": true, "errors": []any{}})
					}
				}
				reply(results)
				return true
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(400)
				return true
			}
		})
		table := "Contact?external_id=Ext__c&load_method=rest"
		if s.Strategy == "append" {
			table = "Contact?load_method=rest"
		}
		return reverseetltest.Fixture{Writer: d, Options: destination.WriteOptions{Table: table, PrimaryKeys: []string{"source_key"}}, Snapshot: func() reverseetltest.Effects { mu.Lock(); defer mu.Unlock(); return effects }}
	})
}
