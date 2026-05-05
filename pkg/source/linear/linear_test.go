package linear

import (
	"fmt"
	"testing"
	"time"
)

func TestNormalizeDictionaries(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]interface{}
		want map[string]interface{}
	}{
		{
			name: "flat fields pass through",
			in:   map[string]interface{}{"title": "hello", "id": "123"},
			want: map[string]interface{}{"title": "hello", "id": "123"},
		},
		{
			name: "nested object with id is flattened",
			in:   map[string]interface{}{"creator": map[string]interface{}{"id": "abc"}},
			want: map[string]interface{}{"creatorId": "abc"},
		},
		{
			name: "nested object with nodes is extracted",
			in: map[string]interface{}{
				"labels": map[string]interface{}{
					"nodes": []interface{}{"a", "b"},
				},
			},
			want: map[string]interface{}{"labels": []interface{}{"a", "b"}},
		},
		{
			name: "nested object without id or nodes kept as-is",
			in: map[string]interface{}{
				"metadata": map[string]interface{}{"foo": "bar"},
			},
			want: map[string]interface{}{"metadata": map[string]interface{}{"foo": "bar"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDictionaries(tt.in)
			for k, v := range tt.want {
				gv, ok := got[k]
				if !ok {
					t.Fatalf("missing key %q", k)
				}
				if fmt_val(gv) != fmt_val(v) {
					t.Fatalf("key %q: got %v, want %v", k, gv, v)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d keys, want %d", len(got), len(tt.want))
			}
		})
	}
}

func fmt_val(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", v)
}

func TestFilterByUpdatedAt(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	items := []map[string]interface{}{
		{"id": "1", "updatedAt": "2026-02-15T00:00:00Z"},
		{"id": "2", "updatedAt": "2026-03-05T00:00:00Z"},
		{"id": "3", "updatedAt": "2026-03-20T00:00:00Z"},
		{"id": "4", "updatedAt": "2026-03-01T00:00:00Z"},
		{"id": "5", "updatedAt": "2026-03-15T00:00:00Z"},
		{"id": "6"},
		{"id": "7", "updatedAt": "invalid"},
	}

	tests := []struct {
		name  string
		start *time.Time
		end   *time.Time
		want  []string
	}{
		{"both bounds", &start, &end, []string{"2", "4", "5", "6", "7"}},
		{"start only", &start, nil, []string{"2", "3", "4", "5", "6", "7"}},
		{"end only", nil, &end, []string{"1", "2", "4", "5", "6", "7"}},
		{"nil bounds", nil, nil, []string{"1", "2", "3", "4", "5", "6", "7"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByUpdatedAt(items, tt.start, tt.end)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d items, want %d", len(got), len(tt.want))
			}
			for i, item := range got {
				if item["id"] != tt.want[i] {
					t.Errorf("item[%d] id = %v, want %v", i, item["id"], tt.want[i])
				}
			}
		})
	}
}
