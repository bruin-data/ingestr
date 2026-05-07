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
			want:        "gong_users_gong_publication",
		},
		{
			name:        "with suffix",
			tableName:   "users",
			publication: "gong_publication",
			suffix:      "abc123",
			want:        "gong_users_gong_publication_abc123",
		},
		{
			name:        "schema-qualified table",
			tableName:   "public.users",
			publication: "pub1",
			suffix:      "def456",
			want:        "gong_public_users_pub1_def456",
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
	got := generateSlotName(longTable, "pub", "abcdef")
	if len(got) > 63 {
		t.Errorf("generateSlotName produced %d chars, want <= 63", len(got))
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
			want:        "gong_mt_gong_publication",
		},
		{
			name:        "with suffix",
			publication: "gong_publication",
			suffix:      "abc123",
			want:        "gong_mt_gong_publication_abc123",
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
