package schemainfer

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// inferWithCap reproduces the accumulate-and-flush loop that every []map API
// source runs: rows are appended to a batch and the batch is flushed (converted
// to an Arrow record and handed to the inferrer) whenever adding the next row
// would push the accumulated byte size past maxBatchBytes. A maxBatchBytes of 0
// disables the cap, producing a single batch over all rows — the pre-change
// behavior. This lets the test compare "one big batch" against "many small
// batches" over identical input.
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

// TestSchemaInferenceIsBatchGroupingInvariant is the core safety guarantee for
// the MaxBatchBytes rollout: splitting a source's output into more, smaller
// batches must not change the schema the pipeline infers for the destination.
// The []map sources pass nil columns, so every field is discovered from row
// data; if inference depended on how rows were grouped into batches, a capped
// first batch could miss columns that only appear in later rows. This asserts
// it does not — the same rows produce the same schema whether emitted as one
// batch or one-row-per-batch.
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
