package csv

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantPath string
		wantEnc  string
	}{
		{"hierarchical csv://", "csv:///tmp/file.csv", "/tmp/file.csv", ""},
		{"opaque csv:", "csv:/tmp/file.csv", "/tmp/file.csv", ""},
		{"percent-encoded space", "csv:///tmp/my%20file.csv", "/tmp/my file.csv", ""},
		{"with encoding param", "csv:///tmp/file.csv?encoding=windows-1252", "/tmp/file.csv", "windows-1252"},
		{"encoding among other params", "csv:///tmp/file.csv?other=foo&encoding=cp1252", "/tmp/file.csv", "cp1252"},
		{"empty encoding value", "csv:///tmp/file.csv?encoding=", "/tmp/file.csv", ""},
		{"non-csv scheme rejected", "http://example.com/file.csv", "", ""},
		{"empty input rejected", "", "", ""},
		{"windows path with backslashes", `csv://C:\actions-runner\bruin\assets\seed.csv`, `C:\actions-runner\bruin\assets\seed.csv`, ""},
		{"windows path with forward slashes", "csv://C:/data/seed.csv", "C:/data/seed.csv", ""},
		{"windows path with encoding", `csv://C:\data\seed.csv?encoding=cp1252`, `C:\data\seed.csv`, "cp1252"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotEnc := extractFilePath(tt.uri)
			if gotPath != tt.wantPath || gotEnc != tt.wantEnc {
				t.Errorf("extractFilePath(%q) = (%q, %q); want (%q, %q)",
					tt.uri, gotPath, gotEnc, tt.wantPath, tt.wantEnc)
			}
		})
	}
}

func TestDecode_ExplicitEncoding(t *testing.T) {
	t.Run("windows-1252 turns 0x92 into curly apostrophe", func(t *testing.T) {
		// 0x92 is the cp1252 byte for U+2019 ("right single quotation mark").
		assertDecodes(t, []byte{'C', 'a', 'n', 0x92, 't'}, "windows-1252", "Can’t")
	})

	t.Run("aliases all resolve to cp1252", func(t *testing.T) {
		// 0xE9 is "é" in cp1252 / latin1 / iso-8859-1.
		input := []byte{'h', 0xE9, 'l', 'l', 'o'}
		for _, alias := range []string{"cp1252", "latin1", "iso-8859-1"} {
			assertDecodes(t, input, alias, "héllo")
		}
	})

	t.Run("explicit encoding overrides BOM", func(t *testing.T) {
		// File starts with a UTF-8 BOM (EF BB BF) but user declares cp1252.
		// In cp1252 those three bytes are "ï»¿" — proves explicit wins.
		input := []byte{0xEF, 0xBB, 0xBF, 'x'}
		got, err := Decode(bytes.NewReader(input), "windows-1252")
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(got)
		if !strings.HasPrefix(string(out), "ï»¿") {
			t.Errorf("expected cp1252 decoding of BOM bytes, got %q", out)
		}
	})

	t.Run("unknown encoding returns error", func(t *testing.T) {
		_, err := Decode(bytes.NewReader([]byte("x")), "klingon-7")
		if err == nil || !strings.Contains(err.Error(), "unsupported encoding") {
			t.Errorf("want 'unsupported encoding' error, got %v", err)
		}
	})
}

// assertDecodes runs Decode and checks the output matches want.
func assertDecodes(t *testing.T, input []byte, encName, want string) {
	t.Helper()
	r, err := Decode(bytes.NewReader(input), encName)
	if err != nil {
		t.Fatalf("Decode(_, %q) error: %v", encName, err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(got) != want {
		t.Errorf("Decode(_, %q) = %q; want %q", encName, got, want)
	}
}
