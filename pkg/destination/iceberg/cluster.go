package iceberg

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
)

type clusterValueEncoder func(*bytes.Buffer, any) error

func newClusterSorter(sc *arrow.Schema, columns []string) (*spillSorter, error) {
	sorter, err := newSpillSorter(sc, columns)
	if err != nil {
		return nil, err
	}

	encoders := make([]clusterValueEncoder, len(sorter.keyIdx))
	for i, idx := range sorter.keyIdx {
		encoder, err := clusterEncoder(sc.Field(idx).Type)
		if err != nil {
			return nil, fmt.Errorf("iceberg: cluster column %q: %w", sc.Field(idx).Name, err)
		}
		encoders[i] = encoder
	}
	sorter.allowNil = true
	sorter.encode = func(values []any) (string, error) {
		var out bytes.Buffer
		for i, value := range values {
			if value == nil {
				out.WriteByte(0)
				continue
			}
			out.WriteByte(1)
			if err := encoders[i](&out, value); err != nil {
				return "", err
			}
		}
		return out.String(), nil
	}
	return sorter, nil
}

func clusterEncoder(dataType arrow.DataType) (clusterValueEncoder, error) {
	switch t := dataType.(type) {
	case *arrow.BooleanType:
		return func(out *bytes.Buffer, value any) error {
			v, ok := value.(bool)
			if !ok {
				return clusterTypeError(dataType, value)
			}
			if v {
				out.WriteByte(1)
			} else {
				out.WriteByte(0)
			}
			return nil
		}, nil
	case *arrow.Int8Type, *arrow.Int16Type, *arrow.Int32Type, *arrow.Int64Type,
		*arrow.Uint8Type, *arrow.Uint16Type, *arrow.Uint32Type, *arrow.Uint64Type,
		*arrow.Date32Type, *arrow.Date64Type, *arrow.Time32Type, *arrow.Time64Type, *arrow.TimestampType:
		return func(out *bytes.Buffer, value any) error {
			v, ok := value.(int64)
			if !ok {
				return clusterTypeError(dataType, value)
			}
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], uint64(v)^(uint64(1)<<63))
			out.Write(encoded[:])
			return nil
		}, nil
	case *arrow.Float32Type, *arrow.Float64Type:
		return func(out *bytes.Buffer, value any) error {
			v, ok := value.(float64)
			if !ok {
				return clusterTypeError(dataType, value)
			}
			var sortable uint64
			if math.IsNaN(v) {
				sortable = math.MaxUint64
			} else {
				sortable = math.Float64bits(v)
				if sortable&(uint64(1)<<63) != 0 {
					sortable = ^sortable
				} else {
					sortable ^= uint64(1) << 63
				}
			}
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], sortable)
			out.Write(encoded[:])
			return nil
		}, nil
	case *arrow.StringType, *arrow.LargeStringType:
		return func(out *bytes.Buffer, value any) error {
			v, ok := value.(string)
			if !ok {
				return clusterTypeError(dataType, value)
			}
			writeEscapedClusterBytes(out, []byte(v))
			return nil
		}, nil
	case *arrow.BinaryType, *arrow.LargeBinaryType, *arrow.FixedSizeBinaryType:
		return func(out *bytes.Buffer, value any) error {
			v, ok := value.([]byte)
			if !ok {
				return clusterTypeError(dataType, value)
			}
			writeEscapedClusterBytes(out, v)
			return nil
		}, nil
	case *extensions.UUIDType:
		return func(out *bytes.Buffer, value any) error {
			v, ok := value.(uuidVal)
			if !ok {
				return clusterTypeError(dataType, value)
			}
			writeEscapedClusterBytes(out, []byte(v))
			return nil
		}, nil
	case *arrow.Decimal128Type:
		return decimalClusterEncoder(dataType, t.Precision, t.Scale), nil
	case *arrow.Decimal256Type:
		return decimalClusterEncoder(dataType, t.Precision, t.Scale), nil
	default:
		return nil, fmt.Errorf("type %s is not orderable for clustering", dataType)
	}
}

func decimalClusterEncoder(dataType arrow.DataType, precision, scale int32) clusterValueEncoder {
	offset := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil)
	maximum := new(big.Int).Mul(new(big.Int).Set(offset), big.NewInt(2))
	width := (maximum.BitLen() + 7) / 8
	scaleFactor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	return func(out *bytes.Buffer, value any) error {
		v, ok := value.(decimalVal)
		if !ok {
			return clusterTypeError(dataType, value)
		}
		rational, ok := new(big.Rat).SetString(string(v))
		if !ok {
			return fmt.Errorf("invalid decimal cluster value %q", v)
		}
		rational.Mul(rational, new(big.Rat).SetInt(scaleFactor))
		if !rational.IsInt() {
			return fmt.Errorf("decimal cluster value %q exceeds scale %d", v, scale)
		}
		shifted := new(big.Int).Add(rational.Num(), offset)
		if shifted.Sign() < 0 || shifted.Cmp(maximum) >= 0 {
			return fmt.Errorf("decimal cluster value %q exceeds precision %d", v, precision)
		}
		encoded := make([]byte, width)
		shifted.FillBytes(encoded)
		out.Write(encoded)
		return nil
	}
}

func writeEscapedClusterBytes(out *bytes.Buffer, value []byte) {
	for _, b := range value {
		if b == 0 {
			out.Write([]byte{0, 255})
			continue
		}
		out.WriteByte(b)
	}
	out.Write([]byte{0, 0})
}

func clusterTypeError(dataType arrow.DataType, value any) error {
	return fmt.Errorf("cannot order value of type %T as %s", value, dataType)
}

func clusterRecordReader(ctx context.Context, input array.RecordReader, columns []string) (array.RecordReader, func(), error) {
	if len(columns) == 0 {
		return input, func() {}, nil
	}
	sorter, err := newClusterSorter(input.Schema(), columns)
	if err != nil {
		return nil, nil, err
	}
	for input.Next() {
		if err := ctx.Err(); err != nil {
			sorter.Close()
			return nil, nil, err
		}
		batch := input.RecordBatch()
		for row := 0; row < int(batch.NumRows()); row++ {
			if err := ctx.Err(); err != nil {
				sorter.Close()
				return nil, nil, err
			}
			values := make([]any, int(batch.NumCols()))
			for column := range values {
				value, err := rowValue(batch.Column(column), row)
				if err != nil {
					sorter.Close()
					return nil, nil, err
				}
				values[column] = value
			}
			if err := sorter.AddContext(ctx, values); err != nil {
				sorter.Close()
				return nil, nil, err
			}
		}
	}
	if err := input.Err(); err != nil {
		sorter.Close()
		return nil, nil, err
	}
	sc := input.Schema()
	reader := streamingReaderContext(ctx, sc, func(sink func(arrow.RecordBatch) error) error {
		it, err := sorter.IterContext(ctx)
		if err != nil {
			return err
		}
		defer it.Close()
		emitter := newBatchEmitter(newRowProjection(sc, arrowSchemaColumnNames(sc)), sink)
		defer emitter.release()
		for it.NextGroup() {
			for it.NextRow() {
				if err := emitter.add(it.Row()); err != nil {
					return err
				}
			}
		}
		if err := it.Err(); err != nil {
			return err
		}
		return emitter.flushBatch()
	})
	cleanup := func() {
		reader.Release()
		sorter.Close()
	}
	return reader, cleanup, nil
}

func deduplicateRecordReader(input array.RecordReader, primaryKeys []string, incrementalKey string) (array.RecordReader, func(), error) {
	sorter, err := newSpillSorter(input.Schema(), primaryKeys)
	if err != nil {
		return nil, nil, err
	}
	incrementalIdx := -1
	if incrementalKey != "" {
		indices := input.Schema().FieldIndices(incrementalKey)
		if len(indices) == 0 {
			sorter.Close()
			return nil, nil, fmt.Errorf("incremental key column %q not found", incrementalKey)
		}
		incrementalIdx = indices[0]
		if _, err := clusterEncoder(input.Schema().Field(incrementalIdx).Type); err != nil {
			sorter.Close()
			return nil, nil, fmt.Errorf("incremental key column %q is not orderable: %w", incrementalKey, err)
		}
	}
	for input.Next() {
		batch := input.RecordBatch()
		for row := 0; row < int(batch.NumRows()); row++ {
			values := make([]any, int(batch.NumCols()))
			for column := range values {
				value, err := rowValue(batch.Column(column), row)
				if err != nil {
					sorter.Close()
					return nil, nil, err
				}
				values[column] = value
			}
			if err := sorter.Add(values); err != nil {
				sorter.Close()
				return nil, nil, err
			}
		}
	}
	if err := input.Err(); err != nil {
		sorter.Close()
		return nil, nil, err
	}

	sc := input.Schema()
	reader := streamingReader(sc, func(sink func(arrow.RecordBatch) error) error {
		it, err := sorter.Iter()
		if err != nil {
			return err
		}
		defer it.Close()
		emitter := newBatchEmitter(newRowProjection(sc, arrowSchemaColumnNames(sc)), sink)
		defer emitter.release()
		for it.NextGroup() {
			if err := emitter.add(selectGroupWinner(it, incrementalIdx)); err != nil {
				return err
			}
		}
		if err := it.Err(); err != nil {
			return err
		}
		return emitter.flushBatch()
	})
	cleanup := func() {
		reader.Release()
		sorter.Close()
	}
	return reader, cleanup, nil
}
