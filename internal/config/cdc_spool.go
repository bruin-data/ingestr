package config

import (
	"net/url"
	"strings"
	"time"
)

func (c *IngestConfig) ApplyCDCSnapshotSpoolDefaults() {
	if !c.CDCSnapshotSpool {
		return
	}
	if c.CDCWALStaging == "" {
		c.CDCWALStaging = "destination"
	}
	if c.CDCWALStaging == "memory" {
		c.CDCWALStaging = "inmemory"
	}
	if c.CDCWALBufferBytes == 0 {
		c.CDCWALBufferBytes = DefaultCDCWALBufferBytes
	}
	if c.FlushInterval == 0 {
		c.FlushInterval = 30 * time.Second
	}
	if c.FlushRecords == 0 {
		c.FlushRecords = 50000
	}
}

func (c *IngestConfig) validateCDCSnapshotSpool() error {
	if !c.CDCSnapshotSpool {
		return nil
	}
	c.ApplyCDCSnapshotSpoolDefaults()
	if c.CDCChunkedSnapshot {
		return &ValidationError{Field: "cdc-snapshot-spool", Message: "cannot be combined with --cdc-snapshot-chunked"}
	}
	u, err := url.Parse(c.SourceURI)
	if err != nil || (u.Scheme != "postgres+cdc" && u.Scheme != "postgresql+cdc") {
		return &ValidationError{Field: "cdc-snapshot-spool", Message: "is only supported for postgres+cdc sources"}
	}
	if c.CDCSnapshotMode == "never" {
		return &ValidationError{Field: "cdc-snapshot-spool", Message: "cannot be combined with CDC snapshot mode never"}
	}
	if c.CDCStartLSN != "" {
		return &ValidationError{Field: "cdc-snapshot-spool", Message: "cannot be combined with --cdc-start-lsn"}
	}
	if c.IncrementalStrategy != "" && c.IncrementalStrategy != StrategyMerge &&
		(c.IncrementalStrategy != StrategyReplace || c.IncrementalStrategyExplicit) {
		return &ValidationError{Field: "incremental-strategy", Message: "spooled CDC snapshots require merge"}
	}
	for _, option := range []struct {
		field string
		set   bool
	}{
		{"sql-limit", c.SQLLimit != 0},
		{"sql-exclude-columns", len(c.SQLExcludeColumns) != 0},
		{"incremental-key", c.IncrementalKey != ""},
		{"incremental-predicate", strings.TrimSpace(c.IncrementalPredicate) != ""},
		{"interval-start", c.IntervalStart != nil},
		{"interval-end", c.IntervalEnd != nil},
		{"extract-partition-by", c.ExtractPartitionBy != ""},
	} {
		if option.set {
			return &ValidationError{Field: option.field, Message: "cannot be combined with spooled CDC snapshots"}
		}
	}
	if c.CDCWALStaging == "disk" && strings.TrimSpace(c.CDCWALSpoolDir) == "" {
		return &ValidationError{Field: "cdc-wal-spool-dir", Message: "is required with --cdc-wal-staging disk"}
	}
	if c.CDCWALBufferBytes <= 0 {
		return &ValidationError{Field: "cdc-wal-buffer-bytes", Message: "must be positive"}
	}
	if c.FlushInterval <= 0 {
		return &ValidationError{Field: "flush-interval", Message: "must be positive"}
	}
	if c.FlushRecords <= 0 {
		return &ValidationError{Field: "flush-records", Message: "must be positive"}
	}
	return nil
}
