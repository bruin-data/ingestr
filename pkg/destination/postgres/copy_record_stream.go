package postgres

import (
	"context"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgtype"
)

type postgresRecordCopyStream struct {
	ctx            context.Context
	records        <-chan source.RecordBatchResult
	tableSchema    *schema.TableSchema
	typeMap        *pgtype.Map
	oids           []uint32
	recordSchema   *arrow.Schema
	current        *arrowCopyReader
	currentRecord  arrow.RecordBatch
	pending        *source.RecordBatchResult
	headerOffset   int
	trailerOffset  int
	inputExhausted bool
	batches        int
}

func newPostgresRecordCopyStream(ctx context.Context, records <-chan source.RecordBatchResult, first arrow.RecordBatch, tableSchema *schema.TableSchema, typeMap *pgtype.Map, oids []uint32) (*postgresRecordCopyStream, error) {
	stream := &postgresRecordCopyStream{
		ctx:          ctx,
		records:      records,
		tableSchema:  tableSchema,
		typeMap:      typeMap,
		oids:         oids,
		recordSchema: first.Schema(),
	}
	if err := stream.setRecord(first); err != nil {
		return nil, err
	}
	return stream, nil
}

func recordSupportsDirectPostgresCopy(record arrow.RecordBatch, oids []uint32) bool {
	if len(oids) != int(record.NumCols()) {
		return false
	}
	for column := 0; column < int(record.NumCols()); column++ {
		if _, ok := directArrowCopyColumn(record.Column(column), oids[column]); !ok {
			return false
		}
	}
	return true
}

func (s *postgresRecordCopyStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if s.headerOffset < len(postgresBinaryCopyHeader) {
		n := copy(p, postgresBinaryCopyHeader[s.headerOffset:])
		s.headerOffset += n
		return n, nil
	}

	for {
		if s.current != nil {
			n, err := s.current.Read(p)
			if err == nil {
				return n, nil
			}
			if err != io.EOF {
				return n, err
			}
			s.releaseCurrent()
		}

		if !s.inputExhausted {
			if err := s.nextRecord(); err != nil {
				return 0, err
			}
			if s.current != nil {
				continue
			}
		}

		if s.trailerOffset < 2 {
			trailer := [...]byte{0xff, 0xff}
			n := copy(p, trailer[s.trailerOffset:])
			s.trailerOffset += n
			return n, nil
		}
		return 0, io.EOF
	}
}

func (s *postgresRecordCopyStream) Close() {
	s.releaseCurrent()
}

func (s *postgresRecordCopyStream) Pending() *source.RecordBatchResult {
	return s.pending
}

func (s *postgresRecordCopyStream) Batches() int {
	return s.batches
}

func (s *postgresRecordCopyStream) nextRecord() error {
	for {
		var result source.RecordBatchResult
		var ok bool
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case result, ok = <-s.records:
		}
		if !ok {
			s.inputExhausted = true
			return nil
		}
		if result.Err != nil {
			s.pending = &result
			s.inputExhausted = true
			return nil
		}
		if result.Batch == nil {
			continue
		}
		if result.Batch.NumRows() == 0 {
			result.Batch.Release()
			continue
		}
		if !result.Batch.Schema().Equal(s.recordSchema) {
			s.pending = &result
			s.inputExhausted = true
			return nil
		}
		return s.setRecord(result.Batch)
	}
}

func (s *postgresRecordCopyStream) setRecord(record arrow.RecordBatch) error {
	reader, ok := newArrowCopyReaderWithBoundaries(record, s.tableSchema, s.typeMap, s.oids, false, false)
	if !ok {
		record.Release()
		return fmt.Errorf("record schema no longer supports direct PostgreSQL COPY")
	}
	s.current = reader
	s.currentRecord = record
	s.batches++
	return nil
}

func (s *postgresRecordCopyStream) releaseCurrent() {
	if s.currentRecord != nil {
		s.currentRecord.Release()
		s.currentRecord = nil
	}
	s.current = nil
}
