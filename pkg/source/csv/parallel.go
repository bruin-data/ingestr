package csv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Vars instead of consts so tests can shrink them to force many segments.
var (
	// parallelBlockSize is the target byte size of each independently parsed
	// file segment.
	parallelBlockSize int64 = 8 << 20
	// parallelMinFileSize gates the parallel reader; smaller files parse
	// sequentially in well under the worker startup cost.
	parallelMinFileSize int64 = 16 << 20
)

// parallelEligible reports whether the file can be split into byte-range
// segments and parsed concurrently. Only plain single-byte-newline encodings
// qualify: an explicit encoding or a UTF-16/32 BOM requires the sequential
// transform path. skip is the number of leading bytes (UTF-8 BOM) to drop.
func parallelEligible(f *os.File, declaredEncoding string) (skip, size int64, ok bool) {
	if declaredEncoding != "" {
		return 0, 0, false
	}
	info, err := f.Stat()
	if err != nil || info.Size() < parallelMinFileSize {
		return 0, 0, false
	}

	var head [4]byte
	n, err := f.ReadAt(head[:], 0)
	if err != nil && err != io.EOF {
		return 0, 0, false
	}
	if hasUTF16or32BOM(head[:n]) {
		return 0, 0, false
	}
	if n >= 3 && head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF {
		skip = 3
	}
	return skip, info.Size(), true
}

type csvSegment struct {
	data []byte
	// startRecord is the 1-based index of the segment's first record counted
	// the way the sequential reader counts lineNum (the header row is record
	// 1, the first data row is 2). Used for warning and error messages.
	startRecord int
}

type segmentJob struct {
	seg  csvSegment
	slot chan []source.RecordBatchResult
}

func (s *CSVSource) readParallel(ctx context.Context, opts source.ReadOptions, f *os.File, skip, size int64, batchSize int) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()

	headerReader := csv.NewReader(bufio.NewReaderSize(io.NewSectionReader(f, skip, size-skip), 64<<10))
	headerReader.FieldsPerRecord = -1
	headers, err := headerReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("failed to extract headers from the CSV, are you sure the given file contains a header row?")
	}
	if opts.IncrementalKey != "" && !containsHeader(headers, opts.IncrementalKey) {
		return nil, fmt.Errorf("incremental_key '%s' not found in the CSV file", opts.IncrementalKey)
	}
	dataStart := skip + headerReader.InputOffset()

	workers := min(runtime.GOMAXPROCS(0), 8)
	results := make(chan source.RecordBatchResult, 8)
	jobs := make(chan segmentJob, workers)
	pending := make(chan chan []source.RecordBatchResult, workers)
	segCtx, cancel := context.WithCancel(ctx)

	// Reader: split the data section into segments cut at record boundaries.
	go func() {
		defer close(jobs)
		defer close(pending)
		defer func() { _ = f.Close() }()
		err := splitSegments(segCtx, f, dataStart, size, func(seg csvSegment) bool {
			slot := make(chan []source.RecordBatchResult, 1)
			select {
			case jobs <- segmentJob{seg: seg, slot: slot}:
			case <-segCtx.Done():
				return false
			}
			select {
			case pending <- slot:
			case <-segCtx.Done():
				return false
			}
			return true
		})
		if err != nil && segCtx.Err() == nil {
			slot := make(chan []source.RecordBatchResult, 1)
			slot <- []source.RecordBatchResult{{Err: fmt.Errorf("failed to read CSV file: %w", err)}}
			select {
			case pending <- slot:
			case <-segCtx.Done():
			}
		}
	}()

	for range workers {
		go func() {
			parser := newSegmentParser(headers, opts, batchSize)
			for {
				var job segmentJob
				var ok bool
				select {
				case job, ok = <-jobs:
					if !ok {
						return
					}
				case <-segCtx.Done():
					return
				}
				job.slot <- parser.parse(job.seg)
			}
		}()
	}

	// Merger: forward per-segment results in file order. After an error (or
	// downstream cancellation) remaining slots are drained and released so no
	// batch leaks and no worker stays blocked.
	go func() {
		defer close(results)
		defer cancel()

		totalRows := 0
		batchNum := 0
		failed := false
		for slot := range pending {
			var segResults []source.RecordBatchResult
			select {
			case segResults = <-slot:
			case <-segCtx.Done():
				select {
				case segResults = <-slot:
				default:
				}
			}
			for _, res := range segResults {
				if failed {
					if res.Batch != nil {
						res.Batch.Release()
					}
					continue
				}
				if res.Err == nil && res.Batch != nil {
					batchNum++
					totalRows += int(res.Batch.NumRows())
					config.Debug("[CSV] Batch %d: %d rows (total: %d)", batchNum, res.Batch.NumRows(), totalRows)
				}
				select {
				case results <- res:
					if res.Err != nil {
						failed = true
					}
				case <-ctx.Done():
					if res.Batch != nil {
						res.Batch.Release()
					}
					failed = true
				}
			}
			if failed {
				cancel()
			}
		}
		config.Debug("[CSV] Total: %d rows in %d batches, read time: %v (parallel)", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

// splitSegments reads the byte range [start, size) of f in blocks and invokes
// emit with consecutive segments that always end on a record boundary. Every
// segment starts at a record boundary, where the cumulative quote count is
// even, so each block (carry + fresh bytes) can be scanned independently.
// Returns early when emit returns false.
func splitSegments(ctx context.Context, f *os.File, start, size int64, emit func(csvSegment) bool) error {
	offset := start
	var carry []byte
	nextRecord := 2 // record 1 is the header row

	for offset < size {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		readLen := min(parallelBlockSize, size-offset)
		block := make([]byte, len(carry)+int(readLen))
		copy(block, carry)
		n, err := io.ReadFull(io.NewSectionReader(f, offset, readLen), block[len(carry):])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return err
		}
		block = block[:len(carry)+n]
		offset += int64(n)

		cut, records := lastRecordBoundary(block)
		if cut < 0 {
			// No boundary in the block (huge record or open quote): grow the
			// carry and read more.
			carry = block
			continue
		}

		seg := csvSegment{data: block[:cut], startRecord: nextRecord}
		nextRecord += records
		carry = append([]byte(nil), block[cut:]...)
		if !emit(seg) {
			return nil
		}
	}

	if len(carry) > 0 {
		emit(csvSegment{data: carry, startRecord: nextRecord})
	}
	return nil
}

// lastRecordBoundary returns the index just past the last newline that lies
// at even cumulative quote parity (i.e. outside any quoted field, per RFC
// 4180: inside a quoted field the count is odd and escaped "" pairs preserve
// parity), plus the number of records before that cut. The block must start
// at a record boundary. Returns cut = -1 when the block holds no complete
// record.
func lastRecordBoundary(block []byte) (cut, records int) {
	if bytes.IndexByte(block, '"') < 0 {
		last := bytes.LastIndexByte(block, '\n')
		if last < 0 {
			return -1, 0
		}
		return last + 1, bytes.Count(block[:last+1], []byte{'\n'})
	}

	cut = -1
	parity := 0
	for i, c := range block {
		switch c {
		case '"':
			parity ^= 1
		case '\n':
			if parity == 0 {
				records++
				cut = i + 1
			}
		}
	}
	if cut < 0 {
		return -1, 0
	}
	return cut, records
}

// segmentParser parses file segments into record batches. Each worker owns
// one parser; the builder resets between batches via finish().
type segmentParser struct {
	opts      source.ReadOptions
	batchSize int
	builder   *batchBuilder
	incIdx    []int
	startTime *time.Time
}

func newSegmentParser(headers []string, opts source.ReadOptions, batchSize int) *segmentParser {
	p := &segmentParser{
		opts:      opts,
		batchSize: batchSize,
		builder:   newBatchBuilder(headers, opts.ExcludeColumns),
		incIdx:    headerIndexes(headers, opts.IncrementalKey),
	}
	if opts.IntervalStart != nil {
		t := *opts.IntervalStart
		p.startTime = &t
	}
	return p
}

func (p *segmentParser) parse(seg csvSegment) []source.RecordBatchResult {
	var out []source.RecordBatchResult

	cr := csv.NewReader(bytes.NewReader(seg.data))
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true

	recordNum := seg.startRecord - 1
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if p.builder.rows > 0 {
				out = append(out, source.RecordBatchResult{Batch: p.builder.finish()})
			}
			return append(out, source.RecordBatchResult{Err: fmt.Errorf("failed to read CSV row %d: %w", recordNum+1, err)})
		}
		recordNum++

		if isAllEmpty(record) {
			continue
		}

		if p.opts.IncrementalKey != "" && p.startTime != nil {
			incValue, ok := lastNonEmptyValue(record, p.incIdx)
			if !ok {
				output.Warnf("[CSV] Row %d: skipping row with empty incremental key '%s'\n", recordNum, p.opts.IncrementalKey)
				continue
			}
			incTime, err := dateparse.ParseAny(incValue)
			if err != nil {
				output.Warnf("[CSV] Row %d: skipping row with unparseable incremental key value '%s'\n", recordNum, incValue)
				continue
			}
			if incTime.Before(*p.startTime) {
				continue
			}
		}

		p.builder.appendRow(record)
		if p.builder.rows >= p.batchSize {
			out = append(out, source.RecordBatchResult{Batch: p.builder.finish()})
		}
	}

	if p.builder.rows > 0 {
		out = append(out, source.RecordBatchResult{Batch: p.builder.finish()})
	}
	return out
}
