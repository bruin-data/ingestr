package iceberg

import (
	"container/heap"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// spillRunRows bounds how many rows a spillSorter holds in memory before
// writing a sorted run to disk. Memory usage is O(spillRunRows × row width)
// regardless of how many rows pass through. Variable so tests can force
// multi-run merges with small datasets.
var spillRunRows = 32 * 1024

// spillMergeFanIn caps how many runs are merged at once. Each open run holds
// one read batch in memory, so without a cap the k-way merge would grow with
// the number of runs; runs beyond the cap are first compacted into larger
// sorted runs in multiple passes. Variable so tests can force multi-pass
// merging.
var spillMergeFanIn = 64

const (
	spillKeyColumn = "__ingestr_spill_key"
	spillSeqColumn = "__ingestr_spill_seq"
)

// spillSorter externally sorts rows by their encoded primary key. Rows are
// buffered up to spillRunRows, sorted, and flushed as Arrow IPC runs on disk;
// iteration k-way merges the runs so rows stream back grouped by key in
// arrival order within each key.
type spillSorter struct {
	schema   *arrow.Schema
	runFile  *arrow.Schema
	keyIdx   []int
	tmpDir   string
	runs     []string
	buf      []spillRow
	seq      int64
	finished bool
}

type spillRow struct {
	key  string
	seq  int64
	vals []any
}

// newSpillSorter sorts rows laid out per schema by the key columns.
func newSpillSorter(schema *arrow.Schema, keyColumns []string) (*spillSorter, error) {
	keyIdx := make([]int, len(keyColumns))
	for i, col := range keyColumns {
		indices := schema.FieldIndices(col)
		if len(indices) == 0 {
			return nil, fmt.Errorf("iceberg: sort key column %q not found", col)
		}
		keyIdx[i] = indices[0]
	}

	fields := append([]arrow.Field{}, schema.Fields()...)
	fields = append(
		fields,
		arrow.Field{Name: spillKeyColumn, Type: arrow.BinaryTypes.String},
		arrow.Field{Name: spillSeqColumn, Type: arrow.PrimitiveTypes.Int64},
	)

	return &spillSorter{
		schema:  schema,
		runFile: arrow.NewSchema(fields, nil),
		keyIdx:  keyIdx,
	}, nil
}

func (s *spillSorter) Add(vals []any) error {
	if s.finished {
		return errors.New("iceberg: spill sorter already finalized")
	}
	keyVals := make([]any, len(s.keyIdx))
	for i, idx := range s.keyIdx {
		if vals[idx] == nil {
			return fmt.Errorf("primary key column %q contains NULL", s.schema.Field(idx).Name)
		}
		keyVals[i] = vals[idx]
	}
	key, err := encodeRowKey(keyVals)
	if err != nil {
		return err
	}

	s.buf = append(s.buf, spillRow{key: key, seq: s.seq, vals: vals})
	s.seq++
	if len(s.buf) >= spillRunRows {
		return s.flushRun()
	}
	return nil
}

func (s *spillSorter) Len() int64 { return s.seq }

func sortSpillRows(rows []spillRow) {
	sort.Slice(rows, func(i, j int) bool {
		if c := strings.Compare(rows[i].key, rows[j].key); c != 0 {
			return c < 0
		}
		return rows[i].seq < rows[j].seq
	})
}

func (s *spillSorter) flushRun() error {
	if len(s.buf) == 0 {
		return nil
	}
	sortSpillRows(s.buf)

	if s.tmpDir == "" {
		dir, err := os.MkdirTemp("", "ingestr-iceberg-spill-")
		if err != nil {
			return fmt.Errorf("iceberg: failed to create spill directory: %w", err)
		}
		s.tmpDir = dir
	}

	f, err := os.CreateTemp(s.tmpDir, "run-*.arrow")
	if err != nil {
		return fmt.Errorf("iceberg: failed to create spill run: %w", err)
	}
	defer func() { _ = f.Close() }()

	writer := ipc.NewWriter(f, ipc.WithSchema(s.runFile))
	numFields := len(s.schema.Fields())
	for start := 0; start < len(s.buf); start += batchBuildRows {
		end := min(start+batchBuildRows, len(s.buf))
		builder := array.NewRecordBuilder(memory.DefaultAllocator, s.runFile)
		for _, row := range s.buf[start:end] {
			for c := range numFields {
				if err := appendValue(builder.Field(c), row.vals[c]); err != nil {
					builder.Release()
					_ = writer.Close()
					return fmt.Errorf("iceberg: spill column %q: %w", s.schema.Field(c).Name, err)
				}
			}
			builder.Field(numFields).(*array.StringBuilder).Append(row.key)
			builder.Field(numFields + 1).(*array.Int64Builder).Append(row.seq)
		}
		batch := builder.NewRecordBatch()
		builder.Release()
		err := writer.Write(batch)
		batch.Release()
		if err != nil {
			_ = writer.Close()
			return fmt.Errorf("iceberg: failed to write spill run: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("iceberg: failed to finish spill run: %w", err)
	}

	s.runs = append(s.runs, f.Name())
	s.buf = s.buf[:0]
	return nil
}

// finalize seals the sorter for iteration. When runs were spilled, the
// remaining buffer becomes a final run and runs beyond the merge fan-in are
// compacted into larger sorted runs; otherwise the buffer is sorted in place
// and iterated from memory.
func (s *spillSorter) finalize() error {
	if s.finished {
		return nil
	}
	if len(s.runs) > 0 {
		if err := s.flushRun(); err != nil {
			return err
		}
		for len(s.runs) > spillMergeFanIn {
			merged, err := s.mergeRuns(s.runs[:spillMergeFanIn])
			if err != nil {
				return err
			}
			for _, run := range s.runs[:spillMergeFanIn] {
				_ = os.Remove(run)
			}
			s.runs = append(s.runs[spillMergeFanIn:], merged)
		}
	} else {
		sortSpillRows(s.buf)
	}
	s.finished = true
	return nil
}

// mergeRuns k-way merges the given sorted runs into one larger sorted run.
func (s *spillSorter) mergeRuns(runs []string) (path string, err error) {
	numFields := len(s.schema.Fields())

	var cursors runHeap
	defer func() {
		for _, c := range cursors {
			c.Close()
		}
	}()
	for _, run := range runs {
		cursor, cErr := newRunCursor(run, numFields)
		if cErr != nil {
			return "", cErr
		}
		if cursor.advance() {
			cursors = append(cursors, cursor)
			continue
		}
		cErr = cursor.err
		cursor.Close()
		if cErr != nil {
			return "", cErr
		}
	}
	heap.Init(&cursors)

	f, err := os.CreateTemp(s.tmpDir, "run-*.arrow")
	if err != nil {
		return "", fmt.Errorf("iceberg: failed to create spill run: %w", err)
	}
	defer func() { _ = f.Close() }()

	writer := ipc.NewWriter(f, ipc.WithSchema(s.runFile))
	builder := array.NewRecordBuilder(memory.DefaultAllocator, s.runFile)
	defer builder.Release()

	rows := 0
	flush := func() error {
		if rows == 0 {
			return nil
		}
		batch := builder.NewRecordBatch()
		rows = 0
		wErr := writer.Write(batch)
		batch.Release()
		return wErr
	}

	for len(cursors) > 0 {
		cursor := cursors[0]
		for c := range numFields {
			if err := appendValue(builder.Field(c), cursor.vals[c]); err != nil {
				_ = writer.Close()
				return "", fmt.Errorf("iceberg: spill column %q: %w", s.schema.Field(c).Name, err)
			}
		}
		builder.Field(numFields).(*array.StringBuilder).Append(cursor.key)
		builder.Field(numFields + 1).(*array.Int64Builder).Append(cursor.seq)
		rows++
		if rows >= batchBuildRows {
			if err := flush(); err != nil {
				_ = writer.Close()
				return "", fmt.Errorf("iceberg: failed to write spill run: %w", err)
			}
		}

		if cursor.advance() {
			heap.Fix(&cursors, 0)
		} else {
			heap.Pop(&cursors)
			cErr := cursor.err
			cursor.Close()
			if cErr != nil {
				_ = writer.Close()
				return "", cErr
			}
		}
	}
	if err := flush(); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("iceberg: failed to write spill run: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("iceberg: failed to finish spill run: %w", err)
	}
	return f.Name(), nil
}

func (s *spillSorter) Close() {
	s.buf = nil
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
		s.tmpDir = ""
	}
	s.runs = nil
}

// Iter returns a fresh cursor over all rows grouped by key. It may be called
// multiple times; each call re-merges the spilled runs.
func (s *spillSorter) Iter() (*spillIter, error) {
	if err := s.finalize(); err != nil {
		return nil, err
	}
	it := &spillIter{}
	if len(s.runs) == 0 {
		it.mem = s.buf
		return it, nil
	}

	for _, run := range s.runs {
		cursor, err := newRunCursor(run, len(s.schema.Fields()))
		if err != nil {
			it.Close()
			return nil, err
		}
		if cursor.advance() {
			it.heap = append(it.heap, cursor)
		} else if cursor.err != nil {
			err := cursor.err
			cursor.Close()
			it.Close()
			return nil, err
		} else {
			cursor.Close()
		}
	}
	heap.Init(&it.heap)
	return it, nil
}

// spillIter iterates rows grouped by key:
//
//	for it.NextGroup() {
//		key := it.Key()
//		for it.NextRow() {
//			row := it.Row()
//		}
//	}
//	if err := it.Err(); err != nil { ... }
type spillIter struct {
	// in-memory mode
	mem []spillRow
	pos int

	// spilled mode
	heap runHeap

	groupKey string
	current  []any
	started  bool
	err      error
}

func (it *spillIter) Close() {
	for _, c := range it.heap {
		c.Close()
	}
	it.heap = nil
	it.mem = nil
}

func (it *spillIter) Err() error { return it.err }

func (it *spillIter) Key() string { return it.groupKey }

func (it *spillIter) Row() []any { return it.current }

// NextGroup advances to the next key group, skipping any unconsumed rows of
// the current group.
func (it *spillIter) NextGroup() bool {
	if it.err != nil {
		return false
	}
	if it.started {
		for it.hasNext() && it.peekKey() == it.groupKey {
			if !it.pop() {
				return false
			}
		}
	} else {
		it.started = true
	}
	if it.err != nil || !it.hasNext() {
		return false
	}
	it.groupKey = it.peekKey()
	return true
}

// NextRow advances within the current group.
func (it *spillIter) NextRow() bool {
	if it.err != nil {
		return false
	}
	if !it.hasNext() || it.peekKey() != it.groupKey {
		return false
	}
	return it.pop()
}

func (it *spillIter) hasNext() bool {
	if it.mem != nil || it.heap == nil {
		return it.pos < len(it.mem)
	}
	return len(it.heap) > 0
}

func (it *spillIter) peekKey() string {
	if it.mem != nil || it.heap == nil {
		if it.pos < len(it.mem) {
			return it.mem[it.pos].key
		}
		return ""
	}
	if len(it.heap) > 0 {
		return it.heap[0].key
	}
	return ""
}

func (it *spillIter) pop() bool {
	if it.mem != nil || it.heap == nil {
		if it.pos >= len(it.mem) {
			return false
		}
		it.current = it.mem[it.pos].vals
		it.pos++
		return true
	}

	if len(it.heap) == 0 {
		return false
	}
	cursor := it.heap[0]
	it.current = cursor.vals
	if cursor.advance() {
		heap.Fix(&it.heap, 0)
	} else {
		heap.Pop(&it.heap)
		if cursor.err != nil {
			it.err = cursor.err
		}
		cursor.Close()
	}
	return true
}

// runCursor streams one sorted run file.
type runCursor struct {
	file      *os.File
	rdr       *ipc.Reader
	batch     arrow.RecordBatch
	batchPos  int
	numFields int

	key  string
	seq  int64
	vals []any
	err  error
}

func newRunCursor(path string, numFields int) (*runCursor, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to open spill run: %w", err)
	}
	rdr, err := ipc.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("iceberg: failed to read spill run: %w", err)
	}
	return &runCursor{file: f, rdr: rdr, numFields: numFields}, nil
}

func (c *runCursor) Close() {
	if c.batch != nil {
		c.batch.Release()
		c.batch = nil
	}
	if c.rdr != nil {
		c.rdr.Release()
		c.rdr = nil
	}
	if c.file != nil {
		_ = c.file.Close()
		c.file = nil
	}
}

// advance loads the next row into key/seq/vals. Returns false at the end of
// the run or on error (recorded in c.err).
func (c *runCursor) advance() bool {
	for c.batch == nil || c.batchPos >= int(c.batch.NumRows()) {
		if c.batch != nil {
			c.batch.Release()
			c.batch = nil
		}
		if !c.rdr.Next() {
			c.err = c.rdr.Err()
			return false
		}
		c.batch = c.rdr.RecordBatch()
		c.batch.Retain()
		c.batchPos = 0
	}

	row := make([]any, c.numFields)
	for i := range c.numFields {
		v, err := rowValue(c.batch.Column(i), c.batchPos)
		if err != nil {
			c.err = fmt.Errorf("iceberg: failed to read spill run: %w", err)
			return false
		}
		row[i] = v
	}
	c.key = c.batch.Column(c.numFields).(*array.String).Value(c.batchPos)
	c.seq = c.batch.Column(c.numFields + 1).(*array.Int64).Value(c.batchPos)
	c.vals = row
	c.batchPos++
	return true
}

type runHeap []*runCursor

func (h runHeap) Len() int { return len(h) }
func (h runHeap) Less(i, j int) bool {
	if c := strings.Compare(h[i].key, h[j].key); c != 0 {
		return c < 0
	}
	return h[i].seq < h[j].seq
}
func (h runHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *runHeap) Push(x any)   { *h = append(*h, x.(*runCursor)) }
func (h *runHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}
