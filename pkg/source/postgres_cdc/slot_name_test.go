package postgres_cdc

import (
	"strings"
	"testing"
)

func TestGenerateSlotName(t *testing.T) {
	tests := []struct {
		name        string
		tableName   string
		publication string
		suffix      string
		want        string
	}{
		{
			name:        "without suffix",
			tableName:   "users",
			publication: "gong_publication",
			suffix:      "",
			want:        "ingestr_users_gong_publication",
		},
		{
			name:        "with suffix",
			tableName:   "users",
			publication: "gong_publication",
			suffix:      "abc123",
			want:        "ingestr_users_gong_publication_abc123",
		},
		{
			name:        "schema-qualified table",
			tableName:   "public.users",
			publication: "pub1",
			suffix:      "def456",
			want:        "ingestr_public_users_pub1_def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateSlotName(tt.tableName, tt.publication, tt.suffix)
			if got != tt.want {
				t.Errorf("generateSlotName(%q, %q, %q) = %q, want %q", tt.tableName, tt.publication, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestGenerateSlotNameTruncation(t *testing.T) {
	longTable := strings.Repeat("a", 50)
	first := generateSlotName(longTable, "publication", "abcdef")
	second := generateSlotName(longTable, "publication", "123456")
	if len(first) != 63 || len(second) != 63 {
		t.Fatalf("long generated slot lengths = %d and %d, want 63", len(first), len(second))
	}
	if first == second {
		t.Fatalf("distinct connector suffixes produced the same truncated slot %q", first)
	}
	if got := generateSlotName(longTable, "publication", "abcdef"); got != first {
		t.Fatalf("long generated slot is not deterministic: first=%q second=%q", first, got)
	}
}

func TestGenerateLegacySlotNameUsesOldPrefixTruncation(t *testing.T) {
	table := "public." + strings.Repeat("orders", 12)
	publication := strings.Repeat("publication", 5)
	full := "ingestr_" + strings.ReplaceAll(table, ".", "_") + "_" + publication + "_abcdef"
	want := full[:63]
	if got := generateLegacySlotName(table, publication, "abcdef"); got != want {
		t.Fatalf("legacy long slot = %q, want %q", got, want)
	}
	if got := generateSlotName(table, publication, "abcdef"); got == want {
		t.Fatal("current slot unexpectedly uses legacy prefix truncation")
	}
	if legacySlotNameUnambiguous(want, "abcdef") {
		t.Fatal("prefix-truncated legacy slot was treated as connector-specific")
	}
	other := generateLegacySlotName(table, publication, "123456")
	if other != want {
		t.Fatalf("two old destination suffixes did not reproduce the expected collision: %q != %q", other, want)
	}
}

func TestLegacySlotNameUnambiguousWhenSuffixSurvives(t *testing.T) {
	name := generateLegacySlotName("public.orders", "publication", "abcdef")
	if !legacySlotNameUnambiguous(name, "abcdef") {
		t.Fatalf("short legacy slot %q did not retain its connector suffix", name)
	}
}

func TestGenerateMultiTableSlotName(t *testing.T) {
	tests := []struct {
		name        string
		publication string
		suffix      string
		want        string
	}{
		{
			name:        "without suffix",
			publication: "gong_publication",
			suffix:      "",
			want:        "ingestr_mt_gong_publication",
		},
		{
			name:        "with suffix",
			publication: "gong_publication",
			suffix:      "abc123",
			want:        "ingestr_mt_gong_publication_abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateMultiTableSlotName(tt.publication, tt.suffix)
			if got != tt.want {
				t.Errorf("generateMultiTableSlotName(%q, %q) = %q, want %q", tt.publication, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestGenerateMultiTableSlotNameTruncationPreservesUniqueness(t *testing.T) {
	publication := strings.Repeat("publication", 8)
	first := generateMultiTableSlotName(publication, "abcdef")
	second := generateMultiTableSlotName(publication, "123456")
	if len(first) != 63 || len(second) != 63 {
		t.Fatalf("long generated slot lengths = %d and %d, want 63", len(first), len(second))
	}
	if first == second {
		t.Fatalf("distinct connector suffixes produced the same truncated slot %q", first)
	}
}

func TestGenerateLegacyMultiTableSlotNameUsesOldPrefixTruncation(t *testing.T) {
	publication := strings.Repeat("publication", 8)
	full := "ingestr_mt_" + publication + "_abcdef"
	want := full[:63]
	if got := generateLegacyMultiTableSlotName(publication, "abcdef"); got != want {
		t.Fatalf("legacy long multi-table slot = %q, want %q", got, want)
	}
	if legacySlotNameUnambiguous(want, "abcdef") {
		t.Fatal("prefix-truncated legacy multi-table slot was treated as connector-specific")
	}
	if other := generateLegacyMultiTableSlotName(publication, "123456"); other != want {
		t.Fatalf("two old multi-table destination suffixes did not collide: %q != %q", other, want)
	}
}

func TestTruncateSlotName(t *testing.T) {
	short := "gong_users_pub"
	if got := truncateSlotName(short); got != short {
		t.Errorf("truncateSlotName(%q) = %q, want unchanged", short, got)
	}

	exact63 := strings.Repeat("x", 63)
	if got := truncateSlotName(exact63); got != exact63 {
		t.Errorf("truncateSlotName(63 chars) = %d chars, want 63", len(got))
	}

	long := strings.Repeat("x", 80)
	if got := truncateSlotName(long); len(got) != 63 {
		t.Errorf("truncateSlotName(80 chars) = %d chars, want 63", len(got))
	}
}
