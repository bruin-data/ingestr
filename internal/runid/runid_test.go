package runid

import (
	"encoding/hex"
	"testing"
)

func TestNewReturns32HexCharacters(t *testing.T) {
	value := New()
	if len(value) != 32 {
		t.Fatalf("run id length = %d, want 32", len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		t.Fatalf("run id %q is not hexadecimal: %v", value, err)
	}
}
