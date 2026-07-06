// Package annotation builds the query annotations ingestr attaches to the SQL
// it runs against a destination, so that warehouse cost can be attributed back
// to a pipeline/asset.
//
// The format matches bruin's own annotation comment so the same cost-tracking
// tooling parses both:
//
//	-- @bruin.config: {"asset":"raw.orders","pipeline":"shopify","type":"ingestr_transform","ingestr_step":"merge"}
//	MERGE INTO raw.orders ...
//
// The caller (typically bruin) supplies the external keys (e.g. pipeline,
// asset) via the --query-annotations flag. ingestr adds the keys only it knows
// at runtime: type (the ETL phase — ingestr_load or ingestr_transform, or plain
// ingestr when no step is set) and ingestr_step (which operation is running).
// Snowflake strips leading comments, so for Snowflake the same JSON is applied
// via the native QUERY_TAG instead (see QueryTag).
package annotation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// commentPrefix is prepended to annotated queries. Matches bruin's format.
const commentPrefix = "-- @bruin.config: "

// ingestrType is the value ingestr emits for the "type" key, giving ingestr its
// own slice in the cost report's Step Type breakdown.
const ingestrType = "ingestr"

// ingestr_step values, one per distinct operation.
const (
	StepExtract      = "extract"       // source read (SELECT) — only meaningful for warehouse sources
	StepCDCResume    = "cdc_resume"    // GetMaxCDCLSN: read the destination's max _cdc_lsn to resume a CDC stream
	StepDDL          = "ddl"           // PrepareTable: create target/staging tables
	StepEvolve       = "evolve"        // ApplyEvolution: ALTER existing table to match new source schema
	StepLoad         = "load"          // Write/WriteParallel: bulk-load rows
	StepMerge        = "merge"         // MergeTable
	StepDeleteInsert = "delete_insert" // DeleteInsertTable
	StepSCD2         = "scd2"          // SCD2Table
	StepSwap         = "swap"          // SwapTable: atomic rename staging->target
	StepTruncate     = "truncate"      // TruncateTable: empty target in place
	StepCleanup      = "cleanup"       // DropTable: drop staging tables
)

// ingestr classifies each query by ETL phase via the "type" value:
//
//	ingestr_extract   — read to drive extraction: the SELECT a warehouse source
//	                    runs, and the CDC resume-cursor lookup on the destination
//	ingestr_load      — move data in:   ddl, evolve, load, swap, truncate, cleanup
//	ingestr_transform — apply strategy: merge, delete_insert, scd2
const (
	typeIngestrExtract   = "ingestr_extract"
	typeIngestrLoad      = "ingestr_load"
	typeIngestrTransform = "ingestr_transform"
)

// typeForStep classifies an ingestr_step into its ETL-phase type.
func typeForStep(step string) string {
	switch step {
	case StepExtract, StepCDCResume:
		return typeIngestrExtract
	case StepMerge, StepDeleteInsert, StepSCD2:
		return typeIngestrTransform
	case StepDDL, StepEvolve, StepLoad, StepSwap, StepCleanup, StepTruncate:
		return typeIngestrLoad
	default:
		return ingestrType
	}
}

// Payload holds the external annotation keys supplied by the caller. Values are
// kept as interface{} (matching bruin) so callers may pass non-string values.
type Payload map[string]interface{}

type ctxKey int

const (
	payloadKey ctxKey = iota
	stepKey
)

// Parse parses the raw --query-annotations flag value, a JSON object holding the
// caller-supplied keys (e.g. pipeline, asset). An empty value returns a nil
// Payload, which simply means no caller keys are added; ingestr still annotates
// queries with its own keys (type, ingestr_step).
func Parse(raw string) (Payload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	p := Payload{}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("invalid --query-annotations JSON %q: %w", raw, err)
	}
	return p, nil
}

// WithPayload stores the caller-supplied base payload on the context. A
// nil/empty payload simply adds no extra keys; downstream Prepend/QueryTag calls
// still emit ingestr's own keys (type, ingestr_step).
func WithPayload(ctx context.Context, p Payload) context.Context {
	if len(p) == 0 {
		return ctx
	}
	return context.WithValue(ctx, payloadKey, p)
}

// WithStep tags the context with the operation about to run (one of the Step*
// constants). Destinations set this immediately before executing a query.
func WithStep(ctx context.Context, step string) context.Context {
	return context.WithValue(ctx, stepKey, step)
}

func payloadFrom(ctx context.Context) (Payload, bool) {
	p, ok := ctx.Value(payloadKey).(Payload)
	return p, ok && len(p) > 0
}

func stepFrom(ctx context.Context) string {
	s, _ := ctx.Value(stepKey).(string)
	return s
}

// build returns the merged annotation JSON for the current context. ingestr
// always annotates its destination queries with its own keys (a phase-specific
// type, and ingestr_step when a step is set), so build never returns "" in
// practice; the
// caller-supplied payload from --query-annotations is merged in on top. ingestr-
// owned keys (type, ingestr_step) always win over caller-supplied keys of the
// same name. encoding/json marshals map keys in sorted order, so the output is
// deterministic.
func build(ctx context.Context) string {
	merged := map[string]interface{}{}
	if p, ok := payloadFrom(ctx); ok {
		for k, v := range p {
			merged[k] = v
		}
	}
	merged["type"] = ingestrType
	if step := stepFrom(ctx); step != "" {
		merged["type"] = typeForStep(step)
		merged["ingestr_step"] = step
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return ""
	}
	return string(b)
}

// Prepend returns sql with the @bruin.config comment prepended. The comment
// always carries ingestr's own keys (type, ingestr_step) plus any caller-
// supplied keys. Use for destinations that keep leading SQL comments (everything
// except Snowflake).
func Prepend(ctx context.Context, sql string) string {
	j := build(ctx)
	if j == "" {
		return sql
	}
	return commentPrefix + j + "\n" + sql
}

// QueryTag returns the annotation JSON for use as Snowflake's QUERY_TAG.
// Snowflake strips leading comments, so it carries the same payload via the
// session QUERY_TAG instead of a comment. ok is false only if the JSON fails to
// build; in practice the tag always carries ingestr's own keys.
func QueryTag(ctx context.Context) (tag string, ok bool) {
	j := build(ctx)
	if j == "" {
		return "", false
	}
	return j, true
}
