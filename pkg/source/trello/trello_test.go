package trello

import (
	"testing"
	"time"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid",
			uri:       "trello://?api_key=abc123&token=tok456",
			wantKey:   "abc123",
			wantToken: "tok456",
		},
		{
			name:      "valid without leading question mark",
			uri:       "trello://api_key=abc123&token=tok456",
			wantKey:   "abc123",
			wantToken: "tok456",
		},
		{
			name:    "missing api_key",
			uri:     "trello://?token=tok456",
			wantErr: true,
		},
		{
			name:    "missing token",
			uri:     "trello://?api_key=abc123",
			wantErr: true,
		},
		{
			name:    "empty",
			uri:     "trello://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "clickup://?api_key=abc123&token=tok456",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, token, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	valid := []string{"boards", "organizations", "lists", "members", "labels", "checklists", "cards", "actions"}
	for _, tbl := range valid {
		if !isValidTable(tbl) {
			t.Errorf("isValidTable(%q) = false, want true", tbl)
		}
	}

	invalid := []string{"", "board", "Cards", "action", "unknown", "list"}
	for _, tbl := range invalid {
		if isValidTable(tbl) {
			t.Errorf("isValidTable(%q) = true, want false", tbl)
		}
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		in        string
		wantName  string
		wantParam string
	}{
		{"cards", "cards", ""},
		{"cards:abc", "cards", "abc"},
		{"cards:abc,def", "cards", "abc,def"},
		{"boards", "boards", ""},
		{"lists:", "lists", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			name, param := parseTableName(tt.in)
			if name != tt.wantName || param != tt.wantParam {
				t.Errorf("parseTableName(%q) = (%q, %q), want (%q, %q)", tt.in, name, param, tt.wantName, tt.wantParam)
			}
		})
	}
}

func TestParseBoardIDs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"abc", []string{"abc"}},
		{"abc,def", []string{"abc", "def"}},
		{" abc , def ", []string{"abc", "def"}},
		{"abc,,def", []string{"abc", "def"}},
		{",", nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := parseBoardIDs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("parseBoardIDs(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseBoardIDs(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	items := []map[string]interface{}{
		{"id": "before", "date": "2023-12-15T10:00:00.000Z"},
		{"id": "in-range", "date": "2024-01-15T10:00:00.000Z"},
		{"id": "at-end", "date": "2024-02-01T00:00:00.000Z"}, // end is exclusive
		{"id": "after", "date": "2024-03-01T10:00:00.000Z"},
		{"id": "no-date"},
	}

	t.Run("both bounds", func(t *testing.T) {
		got := filterItemsByInterval(items, "date", &start, &end)
		ids := idsOf(got)
		// no-date items are retained (unparseable timestamp)
		want := map[string]bool{"in-range": true, "no-date": true}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want keys %v", ids, want)
		}
		for _, id := range ids {
			if !want[id] {
				t.Errorf("unexpected id %q in %v", id, ids)
			}
		}
	})

	t.Run("no bounds returns all", func(t *testing.T) {
		got := filterItemsByInterval(items, "date", nil, nil)
		if len(got) != len(items) {
			t.Errorf("got %d, want %d", len(got), len(items))
		}
	})

	t.Run("empty field returns all", func(t *testing.T) {
		got := filterItemsByInterval(items, "", &start, &end)
		if len(got) != len(items) {
			t.Errorf("got %d, want %d", len(got), len(items))
		}
	})

	t.Run("start only", func(t *testing.T) {
		got := filterItemsByInterval(items, "date", &start, nil)
		ids := idsOf(got)
		want := map[string]bool{"in-range": true, "at-end": true, "after": true, "no-date": true}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
	})
}

func TestFilterActionsByInterval(t *testing.T) {
	start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	items := []map[string]interface{}{
		// created before window, not edited -> excluded
		{"id": "old", "date": "2024-01-01T10:00:00.000Z"},
		// created in window -> included
		{"id": "new", "date": "2024-02-10T10:00:00.000Z"},
		// created before window but edited in window -> included via dateLastEdited
		{"id": "edited-old", "date": "2024-01-01T10:00:00.000Z", "data": map[string]interface{}{"dateLastEdited": "2024-02-15T10:00:00.000Z"}},
		// created in window but edited after window -> excluded (effective ts after end)
		{"id": "edited-after", "date": "2024-02-05T10:00:00.000Z", "data": map[string]interface{}{"dateLastEdited": "2024-04-01T10:00:00.000Z"}},
	}

	got := idsOf(filterActionsByInterval(items, &start, &end))
	want := map[string]bool{"new": true, "edited-old": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q in %v", id, got)
		}
	}

	if n := len(filterActionsByInterval(items, nil, nil)); n != len(items) {
		t.Errorf("no bounds: got %d, want %d", n, len(items))
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		ok   bool
	}{
		{"rfc3339 nano", "2024-01-15T10:00:00.123Z", true},
		{"rfc3339", "2024-01-15T10:00:00Z", true},
		{"empty string", "", false},
		{"non-string", 12345, false},
		{"invalid", "not-a-date", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := parseTimestamp(tt.raw)
			if ok != tt.ok {
				t.Errorf("parseTimestamp(%v) ok = %v, want %v", tt.raw, ok, tt.ok)
			}
		})
	}
}

func idsOf(items []map[string]interface{}) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		if id, ok := it["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}
