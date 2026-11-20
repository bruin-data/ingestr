package postgres

import (
	"context"
	"io"
)

type postgresCopyReader struct {
	reader io.Reader
	cancel context.CancelFunc
	guard  chan struct{}
	closed bool
}

func newPostgresCopyReader(reader io.Reader, cancel context.CancelFunc) *postgresCopyReader {
	return &postgresCopyReader{reader: reader, cancel: cancel, guard: make(chan struct{}, 1)}
}

func (r *postgresCopyReader) Read(p []byte) (int, error) {
	r.guard <- struct{}{}
	defer func() { <-r.guard }()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	return r.reader.Read(p)
}

// PgConn.CopyFrom can return on a connection error while its reader is active.
// Close cancels blocked source reads and joins active encoding before Arrow cleanup.
func (r *postgresCopyReader) Close() {
	r.cancel()
	r.guard <- struct{}{}
	defer func() { <-r.guard }()
	r.closed = true
}
