package maxcompute

import (
	"github.com/bruin-data/ingestr/internal/maxcomputeutil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDataTypeToMaxCompute(col schema.Column) string {
	return maxcomputeutil.MapDataTypeToMaxCompute(col)
}
