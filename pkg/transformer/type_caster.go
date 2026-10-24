package transformer

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// TypeCaster casts record batch columns to match a target Arrow schema.
// Used when --columns specifies type overrides for known-schema sources.
type TypeCaster struct {
	targetSchema *arrow.Schema
	safe         bool
	retarget     bool
	jsonText     func(string) bool
}

func NewTypeCaster(targetSchema *arrow.Schema) *TypeCaster {
	return &TypeCaster{targetSchema: targetSchema}
}

func NewSafeTypeCaster(targetSchema *arrow.Schema) *TypeCaster {
	return &TypeCaster{targetSchema: targetSchema, safe: true}
}

// EnableJSONText keeps a string column cast to the JSON type as JSON text:
// for a known-schema source, a column typed json by --columns or by the
// destination holds documents, not strings to quote (a value that is not valid
// JSON is still stored as a JSON string). Inference flows leave it off; the
// buffer replay decides per column (see Pipeline.buildBufferReaderTarget).
func (tc *TypeCaster) EnableJSONText() *TypeCaster {
	tc.jsonText = func(string) bool { return true }
	return tc
}

// EnableRetarget makes the caster follow mid-stream schema announcements
// (RecordBatchResult.TableInfo): the announced columns become the new target
// and target-only decoration columns (e.g. _ingestr_loaded_at) are preserved
// at the end. Without this, a schema aligner pinned to the job-start schema
// would silently drop columns added on the source while streaming.
func (tc *TypeCaster) EnableRetarget() *TypeCaster {
	tc.retarget = true
	return tc
}

func (tc *TypeCaster) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if batch.Schema().Equal(tc.targetSchema) {
		batch.Retain()
		return batch, nil
	}
	return databuffer.CastRecordToSchemaWithOptions(batch, tc.targetSchema, databuffer.CastOptions{Safe: tc.safe, JSONText: tc.jsonText})
}

func (tc *TypeCaster) OutputSchema(_ *arrow.Schema) *arrow.Schema {
	return tc.targetSchema
}

// RetargetSchema swaps the target to the announced schema, keeping columns
// only the old target had (pipeline decorations like _ingestr_loaded_at)
// appended in their original order. Wrap calls this from the same goroutine
// that calls Transform, so no synchronization is needed.
func (tc *TypeCaster) RetargetSchema(s *schema.TableSchema) {
	if !tc.retarget || s == nil {
		return
	}
	announced := s.ToArrowSchema()
	fields := append([]arrow.Field{}, announced.Fields()...)
	names := make(map[string]bool, len(fields))
	for _, f := range fields {
		names[strings.ToLower(f.Name)] = true
	}
	for _, f := range tc.targetSchema.Fields() {
		if !names[strings.ToLower(f.Name)] {
			fields = append(fields, f)
		}
	}
	tc.targetSchema = arrow.NewSchema(fields, nil)
}
