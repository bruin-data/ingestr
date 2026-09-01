package twocheckout

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// `source` and `external_reference` do not survive to the destination on their own:
// schema inference drops a column that is null in every row of the batch. Declaring
// them gives them a type, which is what makes them survive. If the declaration is
// removed, the columns silently vanish again.
func TestOrdersDeclaresTheColumnsInferenceLoses(t *testing.T) {
	tc, ok := supportedTables["orders"]
	if !ok {
		t.Fatal("orders is not a supported table")
	}
	want := map[string]bool{"Source": false, "ExternalReference": false}
	for _, c := range tc.minColumns {
		if _, ok := want[c.Name]; ok {
			want[c.Name] = true
			if !c.Nullable {
				t.Errorf("%s must be nullable — the API leaves it empty on most orders", c.Name)
			}
			if c.DataType != schema.TypeString {
				t.Errorf("%s should be String, got %v", c.Name, c.DataType)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("orders.minColumns is missing %q", name)
		}
	}
}

// The actual failure mode: every row null, and the column disappears unless it is
// declared. Asserting on the config alone would not have caught the original bug.
func TestAllNullColumnSurvivesOnlyWhenDeclared(t *testing.T) {
	items := []map[string]any{
		{"RefNo": "ABC1", "Source": nil, "ExternalReference": nil},
		{"RefNo": "ABC2", "Source": nil, "ExternalReference": nil},
	}
	has := func(cols []schema.Column) map[string]bool {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, nil)
		if err != nil {
			t.Fatalf("arrow conversion failed: %v", err)
		}
		defer rec.Release()
		out := map[string]bool{}
		for i := 0; i < int(rec.Schema().NumFields()); i++ {
			out[rec.Schema().Field(i).Name] = true
		}
		return out
	}

	// Baseline: inference alone. This documents the loss rather than asserting it is
	// desirable — if arrowconv is ever fixed to keep all-null keys, this flips and
	// the declaration below becomes belt-and-braces instead of load-bearing.
	inferred := has(nil)
	t.Logf("inference-only fields: %v", inferred)

	declared := has(supportedTables["orders"].minColumns)
	for _, name := range []string{"Source", "ExternalReference"} {
		if !declared[name] {
			t.Errorf("declared column %q missing from the arrow schema", name)
		}
	}
	// Declared columns must be ADDITIVE — the vendor's other keys still come through.
	if !declared["RefNo"] {
		t.Error("declaring minColumns must not suppress inferred columns (RefNo lost)")
	}
}
