package postgres_cdc

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	defaultTransactionMemoryBytes = int64(8 << 20)
	defaultDecoderMemoryBytes     = int64(32 << 20)
	defaultWALBufferBytes         = int64(32 << 20)
	defaultCommittedDrainBytes    = int64(8 << 20)
	defaultCommittedDrainChanges  = 1024
)

func init() {
	gob.Register(time.Time{})
	gob.Register(tupleUnchanged{})
	gob.Register([]interface{}{})
}

type byteBudget struct {
	mu     sync.Mutex
	limit  int64
	used   int64
	notify chan struct{}
}

func newByteBudget(limit int64) *byteBudget {
	if limit < 1 {
		limit = 1
	}
	return &byteBudget{limit: limit, notify: make(chan struct{}, 1)}
}

func (b *byteBudget) tryReserve(size int64) bool {
	if b == nil || size <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+size > b.limit {
		return false
	}
	b.used += size
	return true
}

func (b *byteBudget) reserveWithHeartbeat(ctx context.Context, size int64, interval time.Duration, heartbeat func() error) error {
	if b == nil || size <= 0 {
		return nil
	}
	var ticker *time.Ticker
	var ticks <-chan time.Time
	if interval > 0 && heartbeat != nil {
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		b.mu.Lock()
		if b.used+size <= b.limit || b.used == 0 && size > b.limit {
			b.used += size
			b.mu.Unlock()
			return nil
		}
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.notify:
		case <-ticks:
			if err := heartbeat(); err != nil {
				return err
			}
		}
	}
}

func (b *byteBudget) release(size int64) {
	if b == nil || size <= 0 {
		return
	}
	b.mu.Lock()
	b.used -= size
	if b.used < 0 {
		b.used = 0
	}
	b.mu.Unlock()
	select {
	case b.notify <- struct{}{}:
	default:
	}
}

func (b *byteBudget) Used() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

type changeSpool[T any] struct {
	memoryLimit int64
	drainLimit  int64
	budget      *byteBudget
	memory      []T
	memorySizes []int64
	memoryBytes int64
	file        *os.File
	encoder     *gob.Encoder
	decoder     *gob.Decoder
	rawCount    int
	rawRead     int
	remaining   int
	sealed      bool
	keyFn       func(T) uint32
	keyFirstRaw map[uint32]int
	excludedRaw []rawRange
}

type rawRange struct {
	start int
	end   int
}

func newChangeSpool[T any](memoryLimit int64) *changeSpool[T] {
	return newChangeSpoolWithBudget[T](memoryLimit, nil, nil)
}

func newChangeSpoolWithBudget[T any](memoryLimit int64, budget *byteBudget, keyFn func(T) uint32) *changeSpool[T] {
	if memoryLimit < 1 {
		memoryLimit = 1
	}
	return &changeSpool[T]{
		memoryLimit: memoryLimit,
		drainLimit:  defaultCommittedDrainBytes,
		budget:      budget,
		keyFn:       keyFn,
		keyFirstRaw: make(map[uint32]int),
	}
}

func encodedChangeSize[T any](value T) (int64, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&value); err != nil {
		return 0, fmt.Errorf("failed to size CDC change: %w", err)
	}
	return int64(buf.Len()), nil
}

func (s *changeSpool[T]) Append(value T) error {
	if s.sealed {
		return errors.New("cannot append to sealed transaction spool")
	}
	size, err := encodedChangeSize(value)
	if err != nil {
		return err
	}
	if s.file == nil && s.memoryBytes+size <= s.memoryLimit && s.budget.tryReserve(size) {
		s.memory = append(s.memory, value)
		s.memorySizes = append(s.memorySizes, size)
		s.memoryBytes += size
		s.noteAppend(value)
		return nil
	}
	if s.file == nil {
		if err := s.createFile(); err != nil {
			return err
		}
		for i := range s.memory {
			if err := s.encoder.Encode(&s.memory[i]); err != nil {
				return fmt.Errorf("failed to spill buffered CDC change: %w", err)
			}
		}
		s.budget.release(s.memoryBytes)
		s.memory = nil
		s.memorySizes = nil
		s.memoryBytes = 0
	}
	if err := s.encoder.Encode(&value); err != nil {
		return fmt.Errorf("failed to spill CDC change: %w", err)
	}
	s.noteAppend(value)
	return nil
}

func (s *changeSpool[T]) noteAppend(value T) {
	rawIndex := s.rawCount
	s.rawCount++
	s.remaining++
	if s.keyFn != nil {
		key := s.keyFn(value)
		if _, exists := s.keyFirstRaw[key]; !exists {
			s.keyFirstRaw[key] = rawIndex
		}
	}
}

func (s *changeSpool[T]) createFile() error {
	file, err := os.CreateTemp("", "ingestr-postgres-cdc-tx-*")
	if err != nil {
		return fmt.Errorf("failed to create CDC transaction spool: %w", err)
	}
	s.file = file
	s.encoder = gob.NewEncoder(file)
	return nil
}

func (s *changeSpool[T]) Seal() error {
	if s.sealed {
		return nil
	}
	s.sealed = true
	if s.file == nil {
		return nil
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("failed to flush CDC transaction spool: %w", err)
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind CDC transaction spool: %w", err)
	}
	s.encoder = nil
	s.decoder = gob.NewDecoder(s.file)
	return nil
}

func (s *changeSpool[T]) Drain(limit int) ([]T, error) {
	if !s.sealed {
		return nil, errors.New("cannot drain unsealed transaction spool")
	}
	if s.remaining == 0 {
		return nil, nil
	}
	if limit < 1 || limit > s.remaining {
		limit = s.remaining
	}
	out := make([]T, 0, limit)
	var outBytes int64
	for len(out) < limit && s.rawRead < s.rawCount {
		var value T
		var size int64
		memoryIndex := -1
		if s.file == nil {
			memoryIndex = s.rawRead
			value = s.memory[memoryIndex]
			size = s.memorySizes[memoryIndex]
		} else {
			if err := s.decoder.Decode(&value); err != nil {
				return nil, fmt.Errorf("failed to read CDC transaction spool: %w", err)
			}
			var err error
			size, err = encodedChangeSize(value)
			if err != nil {
				return nil, err
			}
		}
		s.rawRead++
		if memoryIndex >= 0 {
			var zero T
			s.memory[memoryIndex] = zero
			s.memoryBytes -= size
			s.budget.release(size)
		}
		if s.rawExcluded(s.rawRead - 1) {
			continue
		}
		out = append(out, value)
		s.remaining--
		outBytes += size
		if outBytes >= s.drainLimit {
			break
		}
	}
	return out, nil
}

func (s *changeSpool[T]) rawExcluded(rawIndex int) bool {
	for _, excluded := range s.excludedRaw {
		if rawIndex >= excluded.start && rawIndex < excluded.end {
			return true
		}
	}
	return false
}

// ExcludeFrom discards the already-buffered suffix beginning with the first
// change produced by key. PostgreSQL STREAM ABORT uses this semantics for a
// subtransaction rollback: the named subtransaction and every descendant (or
// later savepoint) is removed, while changes appended after the abort remain.
func (s *changeSpool[T]) ExcludeFrom(key uint32) {
	if s.keyFn == nil {
		return
	}
	start, exists := s.keyFirstRaw[key]
	if !exists || start >= s.rawCount {
		return
	}
	end := s.rawCount
	removed := 0
	for rawIndex := start; rawIndex < end; rawIndex++ {
		if !s.rawExcluded(rawIndex) {
			removed++
		}
	}
	if removed == 0 {
		return
	}
	s.excludedRaw = append(s.excludedRaw, rawRange{start: start, end: end})
	s.remaining -= removed
}

func (s *changeSpool[T]) Len() int { return s.remaining }

func (s *changeSpool[T]) InMemoryLen() int { return len(s.memory) }

func (s *changeSpool[T]) InMemoryBytes() int64 { return s.memoryBytes }

func (s *changeSpool[T]) Close() error {
	var err error
	if s.file != nil {
		name := s.file.Name()
		err = errors.Join(s.file.Close(), os.Remove(name))
	}
	s.budget.release(s.memoryBytes)
	s.memory = nil
	s.memorySizes = nil
	s.memoryBytes = 0
	s.file = nil
	s.encoder = nil
	s.decoder = nil
	s.rawCount = 0
	s.rawRead = 0
	s.remaining = 0
	s.sealed = false
	s.keyFirstRaw = make(map[uint32]int)
	s.excludedRaw = nil
	return err
}
