package schemainfer

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// inferWithCap runs the source accumulate-and-flush loop over rows, feeding each
// emitted batch to a fresh inferrer, and returns the inferred schema. A
// maxBatchBytes of 0 emits a single batch over all rows.
func inferWithCap(t *testing.T, rows []map[string]interface{}, cols []schema.Column, maxBatchBytes int64) *schema.TableSchema {
	t.Helper()

	inferrer := NewSchemaInferrer()

	var batch []map[string]interface{}
	var accBytes int64
	flush := func() {
		if len(batch) == 0 {
			return
		}
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(batch, cols, nil)
		if err != nil {
			t.Fatalf("convert batch: %v", err)
		}
		if err := inferrer.AddBatch(rec); err != nil {
			t.Fatalf("add batch: %v", err)
		}
		rec.Release()
		batch = nil
		accBytes = 0
	}

	for _, row := range rows {
		if maxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(row)
			if len(batch) > 0 && accBytes+rowBytes > maxBatchBytes {
				flush()
			}
			accBytes += rowBytes
		}
		batch = append(batch, row)
	}
	flush()

	ts, err := inferrer.ToTableSchema("t")
	if err != nil {
		t.Fatalf("to table schema: %v", err)
	}
	return ts
}

func columnMap(ts *schema.TableSchema) map[string]schema.Column {
	if ts == nil {
		return nil
	}
	m := make(map[string]schema.Column, len(ts.Columns))
	for _, c := range ts.Columns {
		m[c.Name] = c
	}
	return m
}

// TestSchemaInferenceIsBatchGroupingInvariant asserts that splitting rows into
// more batches does not change the inferred schema. []map sources pass nil
// columns, so fields are discovered from row data; a capped first batch could
// otherwise miss columns that only appear in later rows.
func TestSchemaInferenceIsBatchGroupingInvariant(t *testing.T) {
	// Heterogeneous rows: keys that appear only in early rows, keys that appear
	// only in late rows, a column whose type changes across rows, and a column
	// that is null everywhere (which the inferrer drops).
	rows := []map[string]interface{}{
		{"id": int64(1), "name": "alice", "early_only": "x", "changer": int64(10), "always_null": nil},
		{"id": int64(2), "name": "bob", "changer": int64(20), "always_null": nil},
		{"id": int64(3), "name": "carol", "changer": int64(30), "always_null": nil},
		{"id": int64(4), "name": "dave", "late_only": true, "changer": "now-a-string", "always_null": nil},
		{"id": int64(5), "name": "erin", "late_only": false, "changer": "also-string", "always_null": nil},
	}

	whole := inferWithCap(t, rows, nil, 0)   // single batch (pre-change)
	perRow := inferWithCap(t, rows, nil, 1)  // one row per batch (max split)
	midway := inferWithCap(t, rows, nil, 40) // a few rows per batch

	if whole == nil {
		t.Fatal("expected a non-nil schema from the whole-batch path")
	}

	wm := columnMap(whole)
	for _, split := range []struct {
		name string
		ts   *schema.TableSchema
	}{{"per-row", perRow}, {"midway", midway}} {
		sm := columnMap(split.ts)
		if len(sm) != len(wm) {
			t.Fatalf("%s: column count %d != whole-batch column count %d (columns: %v vs %v)",
				split.name, len(sm), len(wm), keys(sm), keys(wm))
		}
		for name, wcol := range wm {
			scol, ok := sm[name]
			if !ok {
				t.Fatalf("%s: column %q present in whole-batch schema but missing when split", split.name, name)
			}
			if scol.DataType != wcol.DataType {
				t.Fatalf("%s: column %q type %v != whole-batch type %v", split.name, name, scol.DataType, wcol.DataType)
			}
			if scol.Nullable != wcol.Nullable {
				t.Fatalf("%s: column %q nullable %v != whole-batch nullable %v", split.name, name, scol.Nullable, wcol.Nullable)
			}
		}
	}

	// Sanity: the late-only column must be present despite appearing only after
	// the first row (and thus only in a later batch when split).
	if _, ok := wm["late_only"]; !ok {
		t.Fatal("expected late_only column to be discovered")
	}
}

func keys(m map[string]schema.Column) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
