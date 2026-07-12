package schemaevolution

import (
	"context"
)

// SchemaEvolver is implemented by destinations that can evolve an existing
// table's schema given an abstract EvolutionPlan. The destination is
// responsible for turning the abstract plan into whatever DDL (or API calls)
// it needs and applying it. Destinations that cannot evolve schemas (e.g.
// schema-less file or document stores) simply do not implement this interface,
// and the pipeline skips evolution for them.
type SchemaEvolver interface {
	// ApplySchemaEvolution generates and executes the changes described by the
	// comparison against the table, returning any human-readable warnings for
	// changes that could not be fully applied (e.g. an unsupported type change
	// that was skipped, or a lossy widening).
	ApplySchemaEvolution(ctx context.Context, table string, comparison *SchemaComparison) ([]string, error)

	// SupportsColumnTypeChanges reports whether the destination can change an
	// existing column's type. The pipeline uses this to compute the post-
	// evolution schema: when false, ChangeWidenType/ChangeOverrideType changes
	// are not reflected in the final schema because they will be skipped.
	SupportsColumnTypeChanges() bool
}

// IncarnationFencedSchemaEvolver applies schema changes only while the
// destination table still has the expected physical identity. The identity
// check, table lock, and DDL must share one destination transaction.
type IncarnationFencedSchemaEvolver interface {
	ApplySchemaEvolutionIfIncarnation(
		ctx context.Context,
		table string,
		comparison *SchemaComparison,
		expectedIncarnation string,
	) ([]string, error)
}

// ApplicableComparison returns the subset of changes that will actually be
// reflected in the destination schema given whether the destination supports
// column type changes. Add/remove/nullability changes always apply; type
// changes only apply when supportsTypeChanges is true.
func ApplicableComparison(comparison *SchemaComparison, supportsTypeChanges bool) *SchemaComparison {
	if comparison == nil || !comparison.HasChanges {
		return &SchemaComparison{}
	}

	filtered := &SchemaComparison{
		Changes: make([]SchemaChange, 0, len(comparison.Changes)),
	}
	for _, change := range comparison.Changes {
		switch change.Type {
		case ChangeWidenType, ChangeOverrideType:
			if !supportsTypeChanges {
				continue
			}
		}
		filtered.Changes = append(filtered.Changes, change)
	}
	filtered.HasChanges = len(filtered.Changes) > 0
	return filtered
}
