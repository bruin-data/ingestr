package starrocks

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
)

var errJSONBodyLimit = errors.New("JSON body exceeds the Stream Load byte limit")

type jsonBatchEncoder struct {
	record  arrow.RecordBatch
	keys    [][]byte
	columns []int
	initErr error
	nextRow int64
	pending bool
	body    boundedJSONBuffer
	row     boundedJSONBuffer
}

func newJSONBatchEncoder(record arrow.RecordBatch, limit int) *jsonBatchEncoder {
	encoder := &jsonBatchEncoder{
		record: record,
		body:   boundedJSONBuffer{limit: limit},
		row:    boundedJSONBuffer{limit: max(0, limit-2)},
	}
	if record.NumRows() == 0 {
		return encoder
	}
	fields := record.Schema().Fields()
	lastColumn := make(map[string]int, len(fields))
	for i, field := range fields {
		lastColumn[field.Name] = i
	}
	for i, field := range fields {
		if lastColumn[field.Name] == i {
			encoder.columns = append(encoder.columns, i)
		}
	}
	slices.SortFunc(encoder.columns, func(a, b int) int {
		return strings.Compare(fields[a].Name, fields[b].Name)
	})
	encodedKeys := boundedJSONBuffer{limit: encoder.row.limit}
	offsets := make([]int, len(encoder.columns)+1)
	for i, column := range encoder.columns {
		encodedKeys.writeQuoted(fields[column].Name)
		if encodedKeys.err != nil {
			encoder.initErr = fmt.Errorf("column names cannot fit in a single row within the %d-byte Stream Load limit: %w", limit, encodedKeys.err)
			return encoder
		}
		offsets[i+1] = len(encodedKeys.data)
	}
	encoder.keys = make([][]byte, len(encoder.columns))
	for i := range encoder.keys {
		encoder.keys[i] = encodedKeys.data[offsets[i]:offsets[i+1]]
	}
	return encoder
}

func (e *jsonBatchEncoder) binaryColumns() []string {
	var names []string
	for _, field := range e.record.Schema().Fields() {
		if id := field.Type.ID(); id == arrow.BINARY || id == arrow.LARGE_BINARY {
			names = append(names, field.Name)
		}
	}
	return names
}

// next keeps at most one body and one row in memory. A row that does not fit
// in the current body is kept for the next body without encoding it again.
// The returned body remains valid until the next call.
func (e *jsonBatchEncoder) next(ctx context.Context) ([]byte, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if e.initErr != nil {
		return nil, 0, e.initErr
	}
	if e.nextRow == e.record.NumRows() {
		return nil, 0, nil
	}
	e.body.reset()
	e.body.writeByte('[')
	var rows int64
	for e.nextRow < e.record.NumRows() {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if !e.pending {
			e.row.reset()
			e.row.writeByte('{')
			for i, key := range e.keys {
				if i > 0 {
					e.row.writeByte(',')
				}
				e.row.write(key)
				e.row.writeByte(':')
				e.row.writeValue(e.record.Column(e.columns[i]), int(e.nextRow), false)
			}
			e.row.writeByte('}')
			if e.row.err != nil {
				if errors.Is(e.row.err, errJSONBodyLimit) {
					return nil, 0, fmt.Errorf("a single row (row %d) exceeds the %d-byte Stream Load limit", e.nextRow+1, e.body.limit)
				}
				return nil, 0, e.row.err
			}
		}
		separator := 0
		if rows > 0 {
			separator = 1
		}
		if len(e.row.data)+separator+1 > e.body.limit-len(e.body.data) {
			e.pending = true
			break
		}
		if rows > 0 {
			e.body.writeByte(',')
		}
		e.body.write(e.row.data)
		e.pending = false
		e.nextRow++
		rows++
	}
	e.body.writeByte(']')
	return e.body.data, rows, e.body.err
}

type boundedJSONBuffer struct {
	data  []byte
	limit int
	err   error
}

func (b *boundedJSONBuffer) reset() {
	b.data = b.data[:0]
	b.err = nil
}

func (b *boundedJSONBuffer) reserve(n int) bool {
	if b.err != nil {
		return false
	}
	if n > b.limit-len(b.data) {
		b.err = errJSONBodyLimit
		return false
	}
	if len(b.data)+n > cap(b.data) {
		capacity := min(b.limit, max(len(b.data)+n, max(1024, cap(b.data)*2)))
		data := make([]byte, len(b.data), capacity)
		copy(data, b.data)
		b.data = data
	}
	return true
}

func (b *boundedJSONBuffer) write(data []byte) {
	if b.reserve(len(data)) {
		b.data = append(b.data, data...)
	}
}

func (b *boundedJSONBuffer) writeByte(data byte) {
	if b.reserve(1) {
		b.data = append(b.data, data)
	}
}

func (b *boundedJSONBuffer) writeString(data string) {
	if b.reserve(len(data)) {
		b.data = append(b.data, data...)
	}
}

// writeQuoted writes JSON string escapes directly into the bounded buffer,
// including invalid UTF-8 replacement and HTML escaping used by encoding/json.
func (b *boundedJSONBuffer) writeQuoted(value string) {
	if !b.reserve(len(value) + 2) {
		return
	}
	b.writeByte('"')
	start := 0
	for i := 0; i < len(value) && b.err == nil; {
		c := value[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(value[i:])
			if (r != utf8.RuneError || size != 1) && r != '\u2028' && r != '\u2029' {
				i += size
				continue
			}
			b.writeString(value[start:i])
			switch r {
			case utf8.RuneError:
				b.writeString(`\ufffd`)
			case '\u2028':
				b.writeString(`\u2028`)
			default:
				b.writeString(`\u2029`)
			}
			i += size
			start = i
			continue
		}
		if c >= 0x20 && c != '\\' && c != '"' && c != '<' && c != '>' && c != '&' {
			i++
			continue
		}
		b.writeString(value[start:i])
		switch c {
		case '\\', '"':
			b.writeByte('\\')
			b.writeByte(c)
		case '\n':
			b.writeString(`\n`)
		case '\r':
			b.writeString(`\r`)
		case '\t':
			b.writeString(`\t`)
		case '\b':
			b.writeString(`\b`)
		case '\f':
			b.writeString(`\f`)
		default:
			const hexDigits = "0123456789abcdef"
			b.writeString(`\u00`)
			b.writeByte(hexDigits[c>>4])
			b.writeByte(hexDigits[c&0xf])
		}
		i++
		start = i
	}
	b.writeString(value[start:])
	b.writeByte('"')
}

func (b *boundedJSONBuffer) writeBinary(value []byte, nested bool) {
	length := hex.EncodedLen(len(value))
	if nested {
		length = base64.StdEncoding.EncodedLen(len(value))
	}
	if !b.reserve(length + 2) {
		return
	}
	b.writeByte('"')
	start := len(b.data)
	b.data = b.data[:start+length]
	if nested {
		base64.StdEncoding.Encode(b.data[start:], value)
	} else {
		hex.Encode(b.data[start:], value)
	}
	b.writeByte('"')
}

func (b *boundedJSONBuffer) writeValue(arr arrow.Array, row int, nested bool) {
	if b.err != nil {
		return
	}
	if arr.IsNull(row) {
		b.writeString("null")
		return
	}
	switch a := arr.(type) {
	case *array.String:
		b.writeQuoted(a.Value(row))
	case *array.LargeString:
		b.writeQuoted(a.Value(row))
	case *array.Binary:
		b.writeBinary(a.Value(row), nested)
	case *array.LargeBinary:
		b.writeBinary(a.Value(row), nested)
	case *array.List:
		start, end := a.ValueOffsets(row)
		b.writeList(a.ListValues(), int(start), int(end))
	case *array.LargeList:
		start, end := a.ValueOffsets(row)
		b.writeList(a.ListValues(), int(start), int(end))
	case array.ExtensionArray:
		if a.DataType().(arrow.ExtensionType).ExtensionName() == schema.JSONExtensionName {
			value := extractValue(a.Storage(), row)
			if text, ok := value.(string); ok && json.Valid([]byte(text)) {
				b.writeRawJSON(text)
			} else {
				b.writeGoValue(value)
			}
		} else {
			b.writeValue(a.Storage(), row, nested)
		}
	default:
		b.writeGoValue(extractValue(arr, row))
	}
}

// writeRawJSON compacts a validated JSON value and escapes HTML characters
// without allocating another copy of the value.
func (b *boundedJSONBuffer) writeRawJSON(value string) {
	quoted, escaped, start := false, false, 0
	for i := 0; i < len(value) && b.err == nil; i++ {
		c := value[i]
		if !quoted && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			b.writeString(value[start:i])
			start = i + 1
			continue
		}
		if c == '"' && !escaped {
			quoted = !quoted
		}
		escaped = c == '\\' && !escaped
		if c == '<' || c == '>' || c == '&' {
			b.writeString(value[start:i])
			switch c {
			case '<':
				b.writeString(`\u003c`)
			case '>':
				b.writeString(`\u003e`)
			case '&':
				b.writeString(`\u0026`)
			}
			start = i + 1
		} else if c == 0xe2 && i+2 < len(value) && value[i+1] == 0x80 && (value[i+2] == 0xa8 || value[i+2] == 0xa9) {
			b.writeString(value[start:i])
			if value[i+2] == 0xa8 {
				b.writeString(`\u2028`)
			} else {
				b.writeString(`\u2029`)
			}
			i += 2
			start = i + 1
		}
	}
	b.writeString(value[start:])
}

func (b *boundedJSONBuffer) writeList(values arrow.Array, start, end int) {
	b.writeByte('[')
	for i := start; i < end && b.err == nil; i++ {
		if i > start {
			b.writeByte(',')
		}
		b.writeValue(values, i, true)
	}
	b.writeByte(']')
}

func (b *boundedJSONBuffer) writeGoValue(value interface{}) {
	if s, ok := value.(string); ok {
		b.writeQuoted(s)
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		b.err = err
		return
	}
	b.write(data)
}
