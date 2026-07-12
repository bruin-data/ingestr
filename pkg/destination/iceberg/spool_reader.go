package iceberg

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/bruin-data/ingestr/pkg/naming"
)

type orderedRowContentHasher struct {
	schema *arrow.Schema
	hash   hash.Hash
}

func newOrderedRowContentHasher(sc *arrow.Schema) *orderedRowContentHasher {
	h := sha256.New()
	_, _ = h.Write([]byte(sc.String()))
	return &orderedRowContentHasher{schema: sc, hash: h}
}

func (h *orderedRowContentHasher) Add(row []any) {
	var encoded bytes.Buffer
	for i, value := range row {
		if strings.EqualFold(h.schema.Field(i).Name, naming.IngestrLoadedAtColumn) {
			continue
		}
		encodeContentValue(&encoded, value)
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(encoded.Len()))
	_, _ = h.hash.Write(size[:])
	_, _ = h.hash.Write(encoded.Bytes())
}

func (h *orderedRowContentHasher) Identity() string {
	return hex.EncodeToString(h.hash.Sum(nil))
}

func spoolRecordReader(reader array.RecordReader) (array.RecordReader, func(), error) {
	file, err := os.CreateTemp("", "ingestr-iceberg-overwrite-*.arrow")
	if err != nil {
		return nil, nil, fmt.Errorf("iceberg: failed to create overwrite spool: %w", err)
	}
	cleanupFile := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}

	writer := ipc.NewWriter(file, ipc.WithSchema(reader.Schema()))
	for reader.Next() {
		if err := writer.Write(reader.RecordBatch()); err != nil {
			_ = writer.Close()
			cleanupFile()
			return nil, nil, fmt.Errorf("iceberg: failed to spool overwrite batch: %w", err)
		}
	}
	if err := reader.Err(); err != nil {
		_ = writer.Close()
		cleanupFile()
		return nil, nil, err
	}
	if err := writer.Close(); err != nil {
		cleanupFile()
		return nil, nil, fmt.Errorf("iceberg: failed to finish overwrite spool: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		cleanupFile()
		return nil, nil, fmt.Errorf("iceberg: failed to rewind overwrite spool: %w", err)
	}
	spooled, err := ipc.NewReader(file)
	if err != nil {
		cleanupFile()
		return nil, nil, fmt.Errorf("iceberg: failed to open overwrite spool: %w", err)
	}
	cleanup := func() {
		spooled.Release()
		cleanupFile()
	}
	return spooled, cleanup, nil
}

func spoolReplayableRecordReader(reader array.RecordReader) (func() (array.RecordReader, func(), error), func(), error) {
	file, err := os.CreateTemp("", "ingestr-iceberg-replay-*.arrow")
	if err != nil {
		return nil, nil, fmt.Errorf("iceberg: failed to create replay spool: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	writer := ipc.NewWriter(file, ipc.WithSchema(reader.Schema()))
	for reader.Next() {
		if err := writer.Write(reader.RecordBatch()); err != nil {
			_ = writer.Close()
			cleanup()
			return nil, nil, fmt.Errorf("iceberg: failed to write replay spool: %w", err)
		}
	}
	if err := reader.Err(); err != nil {
		_ = writer.Close()
		cleanup()
		return nil, nil, err
	}
	if err := writer.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("iceberg: failed to finish replay spool: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("iceberg: failed to close replay spool: %w", err)
	}

	open := func() (array.RecordReader, func(), error) {
		input, err := os.Open(path)
		if err != nil {
			return nil, nil, fmt.Errorf("iceberg: failed to open replay spool: %w", err)
		}
		replayed, err := ipc.NewReader(input)
		if err != nil {
			_ = input.Close()
			return nil, nil, fmt.Errorf("iceberg: failed to read replay spool: %w", err)
		}
		release := func() {
			replayed.Release()
			_ = input.Close()
		}
		return replayed, release, nil
	}
	return open, cleanup, nil
}

func spoolAppendRecordReader(reader array.RecordReader) (array.RecordReader, func(), string, error) {
	file, err := os.CreateTemp("", "ingestr-iceberg-append-*.arrow")
	if err != nil {
		return nil, nil, "", fmt.Errorf("iceberg: failed to create append spool: %w", err)
	}
	cleanupFile := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}

	writer := ipc.NewWriter(file, ipc.WithSchema(reader.Schema()))
	var aggregate [sha256.Size]byte
	var rowCount uint64
	for reader.Next() {
		batch := reader.RecordBatch()
		if err := writer.Write(batch); err != nil {
			_ = writer.Close()
			cleanupFile()
			return nil, nil, "", fmt.Errorf("iceberg: failed to spool append batch: %w", err)
		}
		for row := 0; row < int(batch.NumRows()); row++ {
			var encoded bytes.Buffer
			for column := 0; column < int(batch.NumCols()); column++ {
				if strings.EqualFold(batch.Schema().Field(column).Name, naming.IngestrLoadedAtColumn) {
					continue
				}
				value, err := rowValue(batch.Column(column), row)
				if err != nil {
					_ = writer.Close()
					cleanupFile()
					return nil, nil, "", err
				}
				encodeContentValue(&encoded, value)
			}
			rowHash := sha256.Sum256(encoded.Bytes())
			addDigest(&aggregate, rowHash)
			rowCount++
		}
	}
	if err := reader.Err(); err != nil {
		_ = writer.Close()
		cleanupFile()
		return nil, nil, "", err
	}
	if err := writer.Close(); err != nil {
		cleanupFile()
		return nil, nil, "", fmt.Errorf("iceberg: failed to finish append spool: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		cleanupFile()
		return nil, nil, "", fmt.Errorf("iceberg: failed to rewind append spool: %w", err)
	}
	spooled, err := ipc.NewReader(file)
	if err != nil {
		cleanupFile()
		return nil, nil, "", fmt.Errorf("iceberg: failed to open append spool: %w", err)
	}
	identity := sha256.New()
	_, _ = identity.Write([]byte(reader.Schema().String()))
	_, _ = identity.Write(aggregate[:])
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], rowCount)
	_, _ = identity.Write(count[:])
	cleanup := func() {
		spooled.Release()
		cleanupFile()
	}
	return spooled, cleanup, hex.EncodeToString(identity.Sum(nil)), nil
}

func addDigest(total *[sha256.Size]byte, value [sha256.Size]byte) {
	carry := uint16(0)
	for i := sha256.Size - 1; i >= 0; i-- {
		sum := uint16(total[i]) + uint16(value[i]) + carry
		total[i] = byte(sum)
		carry = sum >> 8
	}
}

func encodeContentValue(out *bytes.Buffer, value any) {
	switch value := value.(type) {
	case nil:
		out.WriteByte(0)
	case bool:
		out.WriteByte(1)
		if value {
			out.WriteByte(1)
		} else {
			out.WriteByte(0)
		}
	case int64:
		out.WriteByte(2)
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], uint64(value))
		out.Write(encoded[:])
	case float64:
		out.WriteByte(3)
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], math.Float64bits(value))
		out.Write(encoded[:])
	case string:
		writeContentBytes(out, 4, []byte(value))
	case []byte:
		writeContentBytes(out, 5, value)
	case decimalVal:
		writeContentBytes(out, 6, []byte(value))
	case uuidVal:
		writeContentBytes(out, 7, []byte(value))
	case []any:
		out.WriteByte(8)
		_, _ = out.WriteString(strconv.Itoa(len(value)))
		out.WriteByte(':')
		for _, element := range value {
			encodeContentValue(out, element)
		}
	case structVal:
		out.WriteByte(9)
		_, _ = out.WriteString(strconv.Itoa(len(value)))
		out.WriteByte(':')
		for _, field := range value {
			encodeContentValue(out, field)
		}
	case mapVal:
		out.WriteByte(10)
		_, _ = out.WriteString(strconv.Itoa(len(value)))
		out.WriteByte(':')
		for _, entry := range value {
			encodeContentValue(out, entry.key)
			encodeContentValue(out, entry.value)
		}
	default:
		writeContentBytes(out, 11, []byte(fmt.Sprintf("%T:%v", value, value)))
	}
}

func writeContentBytes(out *bytes.Buffer, tag byte, value []byte) {
	out.WriteByte(tag)
	_, _ = out.WriteString(strconv.Itoa(len(value)))
	out.WriteByte(':')
	out.Write(value)
}
