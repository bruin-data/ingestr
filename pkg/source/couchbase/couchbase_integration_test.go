//go:build integration

package couchbase_test

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

func TestCouchbasePipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cbURI := os.Getenv("COUCHBASE_URI")
	if cbURI == "" {
		t.Skip("Set COUCHBASE_URI to run Couchbase integration tests")
	}

	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("couchbase_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "_default._default",
			DestTable:        "main.couchbase_test",
			KeyColumn:        "id",
			ExpectedRowCount: 20,
			ExpectedSchema: []schema.Column{
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "age", DataType: schema.TypeInt64},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "email", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "score", DataType: schema.TypeFloat64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "user_1",
					Fields: map[string]any{
						"name":   "User 1",
						"email":  "user1@example.com",
						"age":    int64(21),
						"active": true,
						"score":  1.5,
					},
				},
			},
		},
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, cbURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}
