package maxcompute

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestBuildCreateTableSQL_RejectsOversizedString(t *testing.T) {
	over := []schema.Column{{Name: "name", DataType: schema.TypeString, MaxLength: 70000}}
	if _, err := buildCreateTableSQL("t", over); err == nil {
		t.Fatal("expected an error for a string length above the MaxCompute VARCHAR maximum")
	}

	ok := []schema.Column{{Name: "name", DataType: schema.TypeString, MaxLength: 65535}}
	if _, err := buildCreateTableSQL("t", ok); err != nil {
		t.Fatalf("unexpected error for a length at the maximum: %v", err)
	}
}
