package fastspring

import (
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseFastspringURI(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantUsername string
		wantPassword string
		wantError    bool
	}{
		{
			name:         "valid credentials",
			uri:          "fastspring://?username=user123&password=pass456",
			wantUsername: "user123",
			wantPassword: "pass456",
		},
		{
			name:         "credentials with special characters",
			uri:          "fastspring://?username=USER_abc&password=p%40ss%2Fword",
			wantUsername: "USER_abc",
			wantPassword: "p@ss/word",
		},
		{
			name:      "missing username",
			uri:       "fastspring://?password=pass456",
			wantError: true,
		},
		{
			name:      "missing password",
			uri:       "fastspring://?username=user123",
			wantError: true,
		},
		{
			name:      "empty URI",
			uri:       "fastspring://",
			wantError: true,
		},
		{
			name:      "wrong scheme",
			uri:       "paddle://?username=user123&password=pass456",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			username, password, err := parseFastspringURI(tt.uri)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if username != tt.wantUsername {
				t.Errorf("username = %q, want %q", username, tt.wantUsername)
			}
			if password != tt.wantPassword {
				t.Errorf("password = %q, want %q", password, tt.wantPassword)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	valid := []string{"orders", "subscriptions", "accounts", "coupons", "products"}
	for _, name := range valid {
		if !isValidTable(name) {
			t.Errorf("isValidTable(%q) = false, want true", name)
		}
	}

	invalid := []string{"", "Orders", "order", "events", "unknown", "ORDERS"}
	for _, name := range invalid {
		if isValidTable(name) {
			t.Errorf("isValidTable(%q) = true, want false", name)
		}
	}
}

func TestExtractItems(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		resultKey    string
		wantLen      int
		wantNextPage int
	}{
		{
			name:         "envelope with named key and next page",
			body:         `{"orders":[{"id":"a"},{"id":"b"}],"page":1,"limit":50,"nextPage":2,"total":100,"totalPages":2}`,
			resultKey:    "orders",
			wantLen:      2,
			wantNextPage: 2,
		},
		{
			name:         "envelope last page has no nextPage",
			body:         `{"orders":[{"id":"a"}],"page":2,"limit":50,"totalPages":2}`,
			resultKey:    "orders",
			wantLen:      1,
			wantNextPage: 0,
		},
		{
			name:         "missing result key yields no items",
			body:         `{"data":[{"id":"a"},{"id":"b"},{"id":"c"}],"page":1}`,
			resultKey:    "orders",
			wantLen:      0,
			wantNextPage: 0,
		},
		{
			name:         "bare array response",
			body:         `[{"id":"a"},{"id":"b"}]`,
			resultKey:    "products",
			wantLen:      2,
			wantNextPage: 0,
		},
		{
			name:         "array of product path strings",
			body:         `{"products":["path-one","path-two"]}`,
			resultKey:    "products",
			wantLen:      2,
			wantNextPage: 0,
		},
		{
			name:         "empty result",
			body:         `{"orders":[],"page":1}`,
			resultKey:    "orders",
			wantLen:      0,
			wantNextPage: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, nextPage, err := extractItems([]byte(tt.body), tt.resultKey)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(raw) != tt.wantLen {
				t.Errorf("len(raw) = %d, want %d", len(raw), tt.wantLen)
			}
			if nextPage != tt.wantNextPage {
				t.Errorf("nextPage = %d, want %d", nextPage, tt.wantNextPage)
			}
		})
	}
}

func TestExtractItemsError(t *testing.T) {
	// FastSpring returns HTTP 200 with a result:error envelope for some failures
	// (e.g. sending limit to /products). extractItems must surface this as an error
	// rather than silently returning zero items.
	body := `{"action":"products.getall","result":"error","error":{"limit":"Pagination is not supported for this endpoint"}}`
	_, _, err := extractItems([]byte(body), "products")
	if err == nil {
		t.Fatalf("expected error for result:error envelope, got none")
	}
}

func TestCollectIDs(t *testing.T) {
	// Plain list endpoints return identifier strings.
	strs := []interface{}{"a", "b", "c"}
	if got := collectIDs(strs); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("string list = %v, want [a b c]", got)
	}

	// A date-filtered /orders request returns full order objects; the id must
	// still be extracted from the "id" field rather than dropped.
	objs := []interface{}{
		map[string]interface{}{"id": "o1", "order": "o1", "total": 10},
		map[string]interface{}{"id": "o2", "order": "o2", "total": 20},
	}
	if got := collectIDs(objs); !reflect.DeepEqual(got, []string{"o1", "o2"}) {
		t.Errorf("object list = %v, want [o1 o2]", got)
	}

	// Objects without a usable id are skipped rather than yielding empty ids.
	mixed := []interface{}{"a", map[string]interface{}{"id": ""}, map[string]interface{}{"reference": "x"}, "b"}
	if got := collectIDs(mixed); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("mixed list = %v, want [a b]", got)
	}
}

func TestParseObjects(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		resultKey string
		wantLen   int
	}{
		{
			name:      "single object",
			body:      `{"id":"a1","email":"x@y.com"}`,
			resultKey: "accounts",
			wantLen:   1,
		},
		{
			name:      "envelope wrapping an array",
			body:      `{"orders":[{"id":"o1"},{"id":"o2"}]}`,
			resultKey: "orders",
			wantLen:   2,
		},
		{
			name:      "bare array",
			body:      `[{"id":"o1"},{"id":"o2"},{"id":"o3"}]`,
			resultKey: "orders",
			wantLen:   3,
		},
		{
			name:      "single object with a nested array is not mistaken for the list",
			body:      `{"id":"s1","items":["a","b"]}`,
			resultKey: "subscriptions",
			wantLen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseObjects([]byte(tt.body), tt.resultKey)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestResolveReportTable(t *testing.T) {
	s := NewFastspringSource()

	t.Run("non-report table falls through", func(t *testing.T) {
		st, ok, err := s.resolveReportTable(source.TableRequest{Name: "orders"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok || st != nil {
			t.Fatalf("orders should not resolve as a report table")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		st, ok, err := s.resolveReportTable(source.TableRequest{Name: "subscription_report"})
		if err != nil || !ok {
			t.Fatalf("expected report table, ok=%v err=%v", ok, err)
		}
		if st.Name() != "subscription_report" {
			t.Errorf("name = %q", st.Name())
		}
		wantPK := []string{"subscription_id", "transaction_date"}
		if !reflect.DeepEqual(st.PrimaryKeys(), wantPK) {
			t.Errorf("primary keys = %v, want %v", st.PrimaryKeys(), wantPK)
		}
		if st.IncrementalKey() != "sync_date" {
			t.Errorf("incremental key = %q, want sync_date", st.IncrementalKey())
		}
	})

	t.Run("overrides via colon segments", func(t *testing.T) {
		st, ok, err := s.resolveReportTable(source.TableRequest{
			Name: "revenue_report:income_in_usd,product_name:product_name,transaction_month",
		})
		if err != nil || !ok {
			t.Fatalf("expected report table, ok=%v err=%v", ok, err)
		}
		wantPK := []string{"product_name", "transaction_month"}
		if !reflect.DeepEqual(st.PrimaryKeys(), wantPK) {
			t.Errorf("primary keys = %v, want %v", st.PrimaryKeys(), wantPK)
		}
		// syncdate is auto-added to columns, so it is always the incremental key.
		if st.IncrementalKey() != "syncdate" {
			t.Errorf("incremental key = %q, want syncdate", st.IncrementalKey())
		}
	})

	t.Run("columns only keeps default group_by", func(t *testing.T) {
		st, ok, err := s.resolveReportTable(source.TableRequest{Name: "revenue_report:Order_ID,Transaction_Date"})
		if err != nil || !ok {
			t.Fatalf("expected report table, ok=%v err=%v", ok, err)
		}
		wantPK := []string{"order_id", "transaction_date"}
		if !reflect.DeepEqual(st.PrimaryKeys(), wantPK) {
			t.Errorf("primary keys = %v, want %v", st.PrimaryKeys(), wantPK)
		}
		// syncdate is auto-added even for a custom column set → always incremental.
		if st.IncrementalKey() != "syncdate" {
			t.Errorf("incremental key = %q, want syncdate", st.IncrementalKey())
		}
	})

	t.Run("too many segments errors", func(t *testing.T) {
		_, _, err := s.resolveReportTable(source.TableRequest{Name: "revenue_report:a:b:c"})
		if err == nil {
			t.Fatalf("expected error for too many segments")
		}
	})

	t.Run("empty columns segment keeps defaults and overrides group_by", func(t *testing.T) {
		st, ok, err := s.resolveReportTable(source.TableRequest{Name: "revenue_report::order_id,transaction_date"})
		if err != nil || !ok {
			t.Fatalf("expected report table, ok=%v err=%v", ok, err)
		}
		wantPK := []string{"order_id", "transaction_date"}
		if !reflect.DeepEqual(st.PrimaryKeys(), wantPK) {
			t.Errorf("primary keys = %v, want %v", st.PrimaryKeys(), wantPK)
		}
	})

	t.Run("group_by segment with no valid fields errors", func(t *testing.T) {
		_, _, err := s.resolveReportTable(source.TableRequest{Name: "revenue_report:income_in_usd: , "})
		if err == nil {
			t.Fatalf("expected error for empty group_by segment")
		}
	})

	t.Run("columns segment with no valid names errors", func(t *testing.T) {
		_, _, err := s.resolveReportTable(source.TableRequest{Name: "revenue_report: , :order_id"})
		if err == nil {
			t.Fatalf("expected error for empty columns segment")
		}
	})
}
