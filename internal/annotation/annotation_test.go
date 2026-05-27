package annotation

import (
	"context"
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("empty disables annotations", func(t *testing.T) {
		p, err := Parse("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != nil {
			t.Fatalf("expected nil payload, got %v", p)
		}
	})

	t.Run("whitespace is treated as empty", func(t *testing.T) {
		p, err := Parse("   ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != nil {
			t.Fatalf("expected nil payload, got %v", p)
		}
	})

	t.Run("valid JSON parses", func(t *testing.T) {
		p, err := Parse(`{"asset":"raw.orders","pipeline":"shopify"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p["asset"] != "raw.orders" || p["pipeline"] != "shopify" {
			t.Fatalf("unexpected payload: %v", p)
		}
	})

	t.Run("invalid JSON errors", func(t *testing.T) {
		if _, err := Parse(`{not json`); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestPrepend_disabledWithoutPayload(t *testing.T) {
	ctx := context.Background()
	const sql = "SELECT 1"
	if got := Prepend(ctx, sql); got != sql {
		t.Fatalf("expected sql unchanged, got %q", got)
	}
	// A step alone, without a payload, must not produce a comment.
	ctx = WithStep(ctx, StepMerge)
	if got := Prepend(ctx, sql); got != sql {
		t.Fatalf("expected sql unchanged with step but no payload, got %q", got)
	}
}

func TestPrepend_buildsComment(t *testing.T) {
	p, _ := Parse(`{"asset":"raw.orders","pipeline":"shopify"}`)
	ctx := WithStep(WithPayload(context.Background(), p), StepMerge)

	got := Prepend(ctx, "MERGE INTO raw.orders ...")
	// Keys are emitted in sorted order by encoding/json.
	const wantComment = `-- @bruin.config: {"asset":"raw.orders","ingestr_step":"merge","pipeline":"shopify","type":"ingestr"}` + "\n"
	want := wantComment + "MERGE INTO raw.orders ..."
	if got != want {
		t.Fatalf("comment mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestPrepend_ingestrKeysWin(t *testing.T) {
	// Caller-supplied type/ingestr_step must be overridden by ingestr's own.
	p, _ := Parse(`{"asset":"a","type":"caller","ingestr_step":"caller"}`)
	ctx := WithStep(WithPayload(context.Background(), p), StepDDL)

	got := Prepend(ctx, "X")
	want := `-- @bruin.config: {"asset":"a","ingestr_step":"ddl","type":"ingestr"}` + "\n" + "X"
	if got != want {
		t.Fatalf("expected ingestr keys to win\n got: %q\nwant: %q", got, want)
	}
}

func TestPrepend_noStepOmitsStepKey(t *testing.T) {
	p, _ := Parse(`{"asset":"a"}`)
	ctx := WithPayload(context.Background(), p)

	got := Prepend(ctx, "X")
	want := `-- @bruin.config: {"asset":"a","type":"ingestr"}` + "\n" + "X"
	if got != want {
		t.Fatalf("expected no ingestr_step key\n got: %q\nwant: %q", got, want)
	}
}

func TestQueryTag(t *testing.T) {
	t.Run("disabled without payload", func(t *testing.T) {
		if _, ok := QueryTag(context.Background()); ok {
			t.Fatal("expected ok=false without payload")
		}
	})

	t.Run("returns same JSON as the comment body", func(t *testing.T) {
		p, _ := Parse(`{"asset":"raw.orders","pipeline":"shopify"}`)
		ctx := WithStep(WithPayload(context.Background(), p), StepMerge)

		tag, ok := QueryTag(ctx)
		if !ok {
			t.Fatal("expected ok=true")
		}
		want := `{"asset":"raw.orders","ingestr_step":"merge","pipeline":"shopify","type":"ingestr"}`
		if tag != want {
			t.Fatalf("tag mismatch\n got: %q\nwant: %q", tag, want)
		}
	})
}
