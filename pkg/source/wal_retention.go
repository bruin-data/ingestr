package source

import "context"

// WALRetentionSnapshot reports the WAL retained by an ingestion slot. Nil
// values are unavailable, and an empty Slot means no slot has been selected.
// MaxKeepBytes is -1 when slot WAL retention is unlimited.
type WALRetentionSnapshot struct {
	Source        string
	Slot          string
	RetainedBytes *float64
	SafeBytes     *int64
	MaxKeepBytes  *int64
	Status        string
}

// WALRetentionReporter reads current slot retention without changing ingestion
// behavior. Implementations must honor the context's cancellation and deadline.
type WALRetentionReporter interface {
	WALRetention(context.Context) (WALRetentionSnapshot, error)
}
