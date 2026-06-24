package destination

import "testing"

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"table", `"table"`},
		{`"already_quoted"`, `"already_quoted"`},
		{`has"quote`, `"has""quote"`},
		{"", `""`},
		{"with space", `"with space"`},
		{`"partial`, `"""partial"`},
		{`partial"`, `"partial"""`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := QuoteIdentifier(tt.input)
			if got != tt.expected {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestQuoteTableName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple table", "users", `"users"`},
		{"schema qualified", "public.users", `"public"."users"`},
		{"already quoted table", `"users"`, `"users"`},
		{"already quoted schema", `"public"."users"`, `"public"."users"`},
		{"multiple dots", "a.b.c", `"a"."b.c"`},
		{"empty string", "", `""`},
		{"dot only", ".", `"".""`},
		{"embedded quotes", `sch"ema.tab"le`, `"sch""ema"."tab""le"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteTableName(tt.input)
			if got != tt.expected {
				t.Errorf("QuoteTableName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
