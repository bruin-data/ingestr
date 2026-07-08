package stats

import (
	"fmt"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
)

type Summary struct {
	RunID           string         `json:"run_id"`
	Source          string         `json:"source"`
	Destination     string         `json:"destination"`
	DurationSeconds float64        `json:"duration_seconds"`
	Tables          []TableSummary `json:"tables"`
}

type TableSummary struct {
	Name            string  `json:"name"`
	RowsLoaded      *int64  `json:"rows_loaded"`
	RowsSkipped     *int64  `json:"rows_skipped"`
	DurationSeconds float64 `json:"duration_seconds"`
	Mode            string  `json:"mode"`
}

type tableState struct {
	name       string
	mode       string
	rowsLoaded int64
	startedAt  time.Time
	finishedAt time.Time
}

type Collector struct {
	mu          sync.Mutex
	runID       string
	source      string
	destination string
	startedAt   time.Time
	tables      map[string]*tableState
	order       []string
}

func NewRunID(t time.Time) string {
	return fmt.Sprintf("%s-%s", t.UTC().Format(time.RFC3339), uuid.NewString()[:8])
}

func NewCollector(runID, source, destination string, startedAt time.Time) *Collector {
	return &Collector{
		runID:       runID,
		source:      source,
		destination: destination,
		startedAt:   startedAt,
		tables:      make(map[string]*tableState),
	}
}

func (c *Collector) StartTable(name, mode string) {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.ensureTableLocked(name)
	if st.startedAt.IsZero() {
		st.startedAt = now
	}
	if mode != "" {
		st.mode = mode
	}
}

func (c *Collector) FinishTable(name string) {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.ensureTableLocked(name)
	if st.startedAt.IsZero() {
		st.startedAt = now
	}
	st.finishedAt = now
}

func (c *Collector) RecordRows(name string, rows int64) {
	if c == nil || rows <= 0 {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.tableForRowsLocked(name)
	if st == nil {
		return
	}
	if st.startedAt.IsZero() {
		st.startedAt = now
	}
	st.rowsLoaded += rows
}

func (c *Collector) Wrap(defaultTable string, records <-chan source.RecordBatchResult) <-chan source.RecordBatchResult {
	if c == nil {
		return records
	}
	out := make(chan source.RecordBatchResult, cap(records))
	go func() {
		defer close(out)
		for result := range records {
			if result.Err == nil && result.Batch != nil {
				tableName := result.TableName
				if tableName == "" {
					tableName = defaultTable
				}
				c.RecordRows(tableName, result.Batch.NumRows())
			}
			out <- result
		}
	}()
	return out
}

func (c *Collector) Summary(now time.Time) Summary {
	c.mu.Lock()
	defer c.mu.Unlock()

	tables := make([]TableSummary, 0, len(c.order))
	for _, name := range c.order {
		st := c.tables[name]
		rowsLoaded := st.rowsLoaded
		durationEnd := st.finishedAt
		if durationEnd.IsZero() {
			durationEnd = now
		}
		duration := 0.0
		if !st.startedAt.IsZero() && !durationEnd.Before(st.startedAt) {
			duration = durationEnd.Sub(st.startedAt).Seconds()
		}

		tables = append(tables, TableSummary{
			Name:            st.name,
			RowsLoaded:      &rowsLoaded,
			RowsSkipped:     nil,
			DurationSeconds: duration,
			Mode:            st.mode,
		})
	}

	return Summary{
		RunID:           c.runID,
		Source:          c.source,
		Destination:     c.destination,
		DurationSeconds: now.Sub(c.startedAt).Seconds(),
		Tables:          tables,
	}
}

func (c *Collector) ensureTableLocked(name string) *tableState {
	if name == "" {
		name = "unknown"
	}
	if st, ok := c.tables[name]; ok {
		return st
	}
	st := &tableState{name: name}
	c.tables[name] = st
	c.order = append(c.order, name)
	return st
}

func (c *Collector) tableForRowsLocked(name string) *tableState {
	if name == "" {
		name = "unknown"
	}
	if st, ok := c.tables[name]; ok {
		return st
	}
	if len(c.tables) > 0 {
		return nil
	}
	return c.ensureTableLocked(name)
}
