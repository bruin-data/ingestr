// Package annotation builds the query annotations ingestr attaches to the SQL
// it runs against a destination, so that warehouse cost can be attributed back
// to a pipeline/asset.
//
// The format matches bruin's own annotation comment so the same cost-tracking
// tooling parses both:
//
//	-- @bruin.config: {"asset":"raw.orders","pipeline":"shopify","type":"ingestr","ingestr_step":"merge"}
//	MERGE INTO raw.orders ...
//
// The caller (typically bruin) supplies the external keys (e.g. pipeline,
// asset) via the --query-annotations flag. ingestr adds the keys only it knows
// at runtime: type ("ingestr") and ingestr_step (which operation is running).
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

// ingestr_step values, one per distinct destination operation.
const (
	StepDDL          = "ddl"           // PrepareTable: create target/staging tables
	StepLoad         = "load"          // Write/WriteParallel: bulk-load rows
	StepMerge        = "merge"         // MergeTable
	StepDeleteInsert = "delete_insert" // DeleteInsertTable
	StepSCD2         = "scd2"          // SCD2Table
	StepSwap         = "swap"          // SwapTable: atomic rename staging->target
	StepTruncate     = "truncate"      // TruncateTable: empty target in place
	StepCleanup      = "cleanup"       // DropTable: drop staging tables
)

// Payload holds the external annotation keys supplied by the caller. Values are
// kept as interface{} (matching bruin) so callers may pass non-string values.
type Payload map[string]interface{}

type ctxKey int

const (
	payloadKey ctxKey = iota
	stepKey
)

// Parse parses the raw --query-annotations flag value, a JSON object. An empty
// value disables annotations and returns a nil Payload (opt-in behaviour).
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

// WithPayload stores the base payload on the context. A nil/empty payload is a
// no-op, so downstream Prepend/QueryTag calls become no-ops too.
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

// build returns the merged annotation JSON for the current context, or "" when
// annotations are disabled. ingestr-owned keys (type, ingestr_step) always win
// over caller-supplied keys of the same name. encoding/json marshals map keys
// in sorted order, so the output is deterministic.
func build(ctx context.Context) string {
	p, ok := payloadFrom(ctx)
	if !ok {
		return ""
	}
	merged := make(map[string]interface{}, len(p)+2)
	for k, v := range p {
		merged[k] = v
	}
	merged["type"] = ingestrType
	if step := stepFrom(ctx); step != "" {
		merged["ingestr_step"] = step
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return ""
	}
	return string(b)
}

// Prepend returns sql with the @bruin.config comment prepended when annotations
// are enabled, otherwise sql unchanged. Use for destinations that keep leading
// SQL comments (everything except Snowflake).
func Prepend(ctx context.Context, sql string) string {
	j := build(ctx)
	if j == "" {
		return sql
	}
	return commentPrefix + j + "\n" + sql
}

// QueryTag returns the annotation JSON for use as Snowflake's QUERY_TAG and
// ok=false when annotations are disabled. Snowflake strips leading comments, so
// it carries the same payload via the session QUERY_TAG instead of a comment.
func QueryTag(ctx context.Context) (tag string, ok bool) {
	j := build(ctx)
	if j == "" {
		return "", false
	}
	return j, true
}
