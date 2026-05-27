package snowflake

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/annotation"
)

// TestAnnotate documents how the Snowflake destination tags queries for cost
// attribution. Snowflake strips leading SQL comments, so instead of a
// "-- @bruin.config" comment the payload is carried on the session QUERY_TAG.
// annotate sets the per-operation step on the context; the resulting context's
// QueryTag is what gets attached to every query the operation runs.
func TestAnnotate(t *testing.T) {
	d := &SnowflakeDestination{}

	t.Run("disabled when no annotations are configured", func(t *testing.T) {
		ctx := d.annotate(context.Background(), annotation.StepMerge)
		if _, ok := annotation.QueryTag(ctx); ok {
			t.Fatal("expected no query tag when annotations are not configured")
		}
	})

	t.Run("threads the operation step into the query tag", func(t *testing.T) {
		payload, _ := annotation.Parse(`{"asset":"raw.orders","pipeline":"shopify"}`)
		base := annotation.WithPayload(context.Background(), payload)

		cases := map[string]string{
			annotation.StepDDL:          `{"asset":"raw.orders","ingestr_step":"ddl","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepLoad:         `{"asset":"raw.orders","ingestr_step":"load","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepMerge:        `{"asset":"raw.orders","ingestr_step":"merge","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepDeleteInsert: `{"asset":"raw.orders","ingestr_step":"delete_insert","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepSCD2:         `{"asset":"raw.orders","ingestr_step":"scd2","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepSwap:         `{"asset":"raw.orders","ingestr_step":"swap","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepTruncate:     `{"asset":"raw.orders","ingestr_step":"truncate","pipeline":"shopify","type":"ingestr"}`,
			annotation.StepCleanup:      `{"asset":"raw.orders","ingestr_step":"cleanup","pipeline":"shopify","type":"ingestr"}`,
		}

		for step, want := range cases {
			ctx := d.annotate(base, step)
			tag, ok := annotation.QueryTag(ctx)
			if !ok {
				t.Fatalf("step %q: expected a query tag", step)
			}
			if tag != want {
				t.Fatalf("step %q query tag mismatch\n got: %s\nwant: %s", step, tag, want)
			}
		}
	})
}
