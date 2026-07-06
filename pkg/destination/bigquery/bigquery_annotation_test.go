package bigquery

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/annotation"
)

// TestAnnotatedQueryComment documents what a BigQuery query looks like once
// ingestr annotates it for cost attribution. Unlike Snowflake, BigQuery keeps
// leading SQL comments (they show up in INFORMATION_SCHEMA.JOBS.query), so the
// "-- @bruin.config" comment is prepended to the query text. This is exactly
// the transformation applied at every d.client.Query(...) site via
// annotation.Prepend(ctx, sql).
func TestAnnotatedQueryComment(t *testing.T) {
	payload, _ := annotation.Parse(`{"asset":"raw.orders","pipeline":"shopify"}`)
	base := annotation.WithPayload(context.Background(), payload)

	t.Run("merge query carries the merge step", func(t *testing.T) {
		ctx := annotation.WithStep(base, annotation.StepMerge)
		got := annotation.Prepend(ctx, "MERGE INTO `p`.`raw`.`orders` ...")

		wantComment := `-- @bruin.config: {"asset":"raw.orders","ingestr_step":"merge","pipeline":"shopify","type":"ingestr_transform"}`
		if !strings.HasPrefix(got, wantComment+"\n") {
			t.Fatalf("missing/incorrect annotation comment\n got: %q\nwant prefix: %q", got, wantComment)
		}
		if !strings.Contains(got, "MERGE INTO") {
			t.Fatalf("original query body was dropped: %q", got)
		}
	})

	t.Run("truncate during table prep carries the ddl step", func(t *testing.T) {
		ctx := annotation.WithStep(base, annotation.StepDDL)
		got := annotation.Prepend(ctx, "TRUNCATE TABLE `p`.`raw`.`orders`")

		wantComment := `-- @bruin.config: {"asset":"raw.orders","ingestr_step":"ddl","pipeline":"shopify","type":"ingestr_load"}`
		if !strings.HasPrefix(got, wantComment+"\n") {
			t.Fatalf("missing/incorrect annotation comment\n got: %q\nwant prefix: %q", got, wantComment)
		}
	})

	t.Run("cdc resume lookup carries the cdc_resume extract step", func(t *testing.T) {
		ctx := annotation.WithStep(base, annotation.StepCDCResume)
		got := annotation.Prepend(ctx, "SELECT MAX(`_cdc_lsn`) FROM `p`.`raw`.`orders`")

		wantComment := `-- @bruin.config: {"asset":"raw.orders","ingestr_step":"cdc_resume","pipeline":"shopify","type":"ingestr_extract"}`
		if !strings.HasPrefix(got, wantComment+"\n") {
			t.Fatalf("missing/incorrect annotation comment\n got: %q\nwant prefix: %q", got, wantComment)
		}
		if !strings.Contains(got, "SELECT MAX(`_cdc_lsn`)") {
			t.Fatalf("original query body was dropped: %q", got)
		}
	})

	t.Run("no caller annotation still carries ingestr's own keys", func(t *testing.T) {
		ctx := annotation.WithStep(context.Background(), annotation.StepMerge)
		const sql = "MERGE INTO `p`.`raw`.`orders` ..."
		got := annotation.Prepend(ctx, sql)

		wantComment := `-- @bruin.config: {"ingestr_step":"merge","type":"ingestr_transform"}`
		if !strings.HasPrefix(got, wantComment+"\n") {
			t.Fatalf("expected ingestr annotation without caller keys\n got: %q\nwant prefix: %q", got, wantComment)
		}
		if !strings.Contains(got, "MERGE INTO") {
			t.Fatalf("original query body was dropped: %q", got)
		}
	})
}
