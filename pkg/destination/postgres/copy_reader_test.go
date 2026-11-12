package postgres

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresCopyReaderCloseWaitsForActiveRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		record := &releaseCountingRecord{RecordBatch: int32Record(t, 1)}
		records := make(chan source.RecordBatchResult)
		close(records)
		stream, err := newPostgresRecordCopyStream(ctx, records, record, int32TableSchema(), pgtype.NewMap(), []uint32{pgtype.Int4OID})
		if err != nil {
			t.Fatal(err)
		}
		reader := newPostgresCopyReader(stream, cancel)
		buffer := make([]byte, 64*1024)
		if _, err := reader.Read(buffer); err != nil {
			t.Fatal(err)
		}

		entered := make(chan struct{})
		resume := make(chan struct{})
		encode := stream.current.columns[0]
		stream.current.columns[0] = func(row int, dst []byte) ([]byte, bool, error) {
			close(entered)
			<-resume
			if record.releases.Load() != 0 {
				return nil, false, errors.New("Arrow record released during COPY read")
			}
			return encode(row, dst)
		}
		readDone := make(chan error, 1)
		go func() {
			_, err := reader.Read(buffer)
			readDone <- err
		}()
		<-entered

		closeDone := make(chan struct{})
		go func() {
			reader.Close()
			stream.Close()
			close(closeDone)
		}()
		synctest.Wait()
		releasedDuringRead := record.releases.Load()
		closedDuringRead := false
		select {
		case <-closeDone:
			closedDuringRead = true
		default:
		}

		close(resume)
		readErr := <-readDone
		<-closeDone
		if releasedDuringRead != 0 {
			t.Errorf("record released %d times while COPY read was active", releasedDuringRead)
		}
		if closedDuringRead {
			t.Error("Close returned while COPY read was active")
		}
		if readErr != nil {
			t.Errorf("active Read failed: %v", readErr)
		}
		if got := record.releases.Load(); got != 1 {
			t.Errorf("record releases = %d, want 1", got)
		}
		if _, err := reader.Read(buffer); !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Read after Close = %v, want io.ErrClosedPipe", err)
		}
	})
}

func TestPostgresCopyReaderCloseCancelsBlockedSourceRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		first := &releaseCountingRecord{RecordBatch: int32Record(t, 1)}
		records := make(chan source.RecordBatchResult, 1)
		stream, err := newPostgresRecordCopyStream(ctx, records, first, int32TableSchema(), pgtype.NewMap(), []uint32{pgtype.Int4OID})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		reader := newPostgresCopyReader(stream, cancel)
		buffer := make([]byte, 64*1024)
		for range 2 {
			if _, err := reader.Read(buffer); err != nil {
				t.Fatal(err)
			}
		}

		readDone := make(chan error, 1)
		go func() {
			_, err := reader.Read(buffer)
			readDone <- err
		}()
		synctest.Wait()
		reader.Close()
		if err := <-readDone; !errors.Is(err, context.Canceled) {
			t.Errorf("blocked Read = %v, want context.Canceled", err)
		}
		if got := first.releases.Load(); got != 1 {
			t.Errorf("first record releases = %d, want 1", got)
		}

		second := &releaseCountingRecord{RecordBatch: int32Record(t, 2)}
		records <- source.RecordBatchResult{Batch: second}
		if _, err := reader.Read(buffer); !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Read after Close = %v, want io.ErrClosedPipe", err)
		}
		select {
		case result := <-records:
			result.Batch.Release()
		default:
			t.Error("Read after Close consumed a queued record")
		}
		reader.Close()
	})
}
