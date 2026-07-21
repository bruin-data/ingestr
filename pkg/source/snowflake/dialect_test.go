package snowflake

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source/adbc"
)

func TestDialectCustomQueryIdentifierQuotingPreservesCase(t *testing.T) {
	dialect := NewDialect()
	quoter, ok := any(dialect).(adbc.CustomQueryIdentifierQuoter)
	if !ok {
		t.Fatal("snowflake dialect does not implement custom query identifier quoting")
	}

	if got, want := dialect.QuoteIdentifier("partition_ts"), `"PARTITION_TS"`; got != want {
		t.Fatalf("QuoteIdentifier() = %q, want %q", got, want)
	}
	if got, want := quoter.QuoteCustomQueryIdentifier("partition_ts"), `"partition_ts"`; got != want {
		t.Fatalf("QuoteCustomQueryIdentifier() = %q, want %q", got, want)
	}
}
