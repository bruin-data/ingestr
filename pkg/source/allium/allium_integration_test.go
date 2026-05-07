package allium_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestAlliumPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key := os.Getenv("ALLIUM_API_KEY")
	if key == "" {
		t.Skip("Set ALLIUM_API_KEY to run Allium integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("allium://?api_key=%s", key)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("allium_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	start := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 25, 0, 0, 0, 0, time.UTC)

	// FGB7ZcJGLYlbGgRK7z9Q runs:
	//   SELECT 1 as id, 'test' as name, CURRENT_TIMESTAMP as created_at,
	//          '{{start_date}}' as start_date_param, '{{end_date}}' as end_date_param
	// Static data: always returns 1 row with id=1, name='test', and the interval params echoed back
	expectations := []testutil.TableExpectation{
		{
			SourceTable: "query:FGB7ZcJGLYlbGgRK7z9Q",
			DestTable:   "main.allium_query_test",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "start_date_param", DataType: schema.TypeDate},
				{Name: "end_date_param", DataType: schema.TypeDate},
			},
			ExpectedRowCount: 1,
			IntervalStart:    &start,
			IntervalEnd:      &end,
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"name":             "test",
						"start_date_param": time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
						"end_date_param":   time.Date(2025, 12, 25, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}
