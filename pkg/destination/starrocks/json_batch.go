package starrocks

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
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
	// validated marks JSON columns whose source guarantees valid JSON text.
	validated []bool
	initErr   error
	nextRow   int64
	pending   bool
	body      boundedJSONBuffer
	row       boundedJSONBuffer
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
	encoder.validated = make([]bool, len(encoder.columns))
	for i, column := range encoder.columns {
		encoder.keys[i] = encodedKeys.data[offsets[i]:offsets[i+1]]
		encoder.validated[i] = validatedJSONField(fields[column])
	}
	return encoder
}

func validatedJSONField(field arrow.Field) bool {
	extension, ok := field.Type.(arrow.ExtensionType)
	if !ok || extension.ExtensionName() != schema.JSONExtensionName {
		return false
	}
	value, ok := field.Metadata.GetValue(schema.JSONValidatedMetadataKey)
	return ok && value == "true"
}

func isBinaryArrowType(dt arrow.DataType) bool {
	if ext, ok := dt.(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			return false
		}
		return isBinaryArrowType(ext.StorageType())
	}
	switch dt.ID() {
	case arrow.BINARY, arrow.LARGE_BINARY, arrow.FIXED_SIZE_BINARY:
		return true
	default:
		return false
	}
}

func isSimpleStreamLoadField(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

func newStreamLoadBatchEncoder(record arrow.RecordBatch, limit int) (*jsonBatchEncoder, streamLoadOptions) {
	if record.NumRows() == 0 {
		return newJSONBatchEncoder(record, limit), streamLoadOptions{}
	}
	fields := record.Schema().Fields()
	lastColumn := make(map[string]int, len(fields))
	for i, field := range fields {
		lastColumn[field.Name] = i
	}
	allSimple := true
	var survivingBinary []string
	for i, field := range fields {
		if lastColumn[field.Name] != i {
			continue
		}
		if strings.Contains(field.Name, "`") {
			return &jsonBatchEncoder{
				record:  record,
				initErr: fmt.Errorf("StarRocks cannot load column %q: column names containing a backtick are not supported", field.Name),
			}, streamLoadOptions{}
		}
		if !isSimpleStreamLoadField(field.Name) {
			allSimple = false
		}
		if isBinaryArrowType(field.Type) {
			survivingBinary = append(survivingBinary, field.Name)
		}
	}
	if allSimple {
		return newJSONBatchEncoder(record, limit), streamLoadOptions{
			columns: columnsHeader(record.Schema(), survivingBinary),
		}
	}

	encoder := &jsonBatchEncoder{
		record: record,
		body:   boundedJSONBuffer{limit: limit},
		row:    boundedJSONBuffer{limit: max(0, limit-2)},
	}
	for i, field := range fields {
		if lastColumn[field.Name] == i {
			encoder.columns = append(encoder.columns, i)
		}
	}
	slices.SortFunc(encoder.columns, func(a, b int) int {
		return strings.Compare(fields[a].Name, fields[b].Name)
	})
	prefix := "_c"
	for {
		conflict := false
		for _, col := range encoder.columns {
			if strings.HasPrefix(strings.ToLower(fields[col].Name), prefix) {
				conflict = true
				break
			}
		}
		if !conflict {
			break
		}
		prefix += "_"
	}

	encodedKeys := boundedJSONBuffer{limit: encoder.row.limit}
	offsets := make([]int, len(encoder.columns)+1)
	jsonPathParts := make([]string, len(encoder.columns))
	slotDecls := make([]string, len(encoder.columns))
	colMappings := make([]string, len(encoder.columns))
	for i, col := range encoder.columns {
		slot := prefix + strconv.Itoa(i)
		encodedKeys.writeQuoted(slot)
		if encodedKeys.err != nil {
			encoder.initErr = fmt.Errorf("column names cannot fit in a single row within the %d-byte Stream Load limit: %w", limit, encodedKeys.err)
			return encoder, streamLoadOptions{}
		}
		offsets[i+1] = len(encodedKeys.data)
		jsonPathParts[i] = `"$.` + slot + `"`
		slotDecls[i] = slot
		quoted := quoteColumn(fields[col].Name)
		if isBinaryArrowType(fields[col].Type) {
			colMappings[i] = fmt.Sprintf("%s=to_binary(%s, 'hex')", quoted, slot)
		} else {
			colMappings[i] = fmt.Sprintf("%s=%s", quoted, slot)
		}
	}
	encoder.keys = make([][]byte, len(encoder.columns))
	encoder.validated = make([]bool, len(encoder.columns))
	for i, col := range encoder.columns {
		encoder.keys[i] = encodedKeys.data[offsets[i]:offsets[i+1]]
		encoder.validated[i] = validatedJSONField(fields[col])
	}
	return encoder, streamLoadOptions{
		columns:   strings.Join(append(slotDecls, colMappings...), ", "),
		jsonpaths: "[" + strings.Join(jsonPathParts, ",") + "]",
	}
}

func (e *jsonBatchEncoder) binaryColumns() []string {
	var names []string
	for _, field := range e.record.Schema().Fields() {
		if isBinaryArrowType(field.Type) {
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
	rows, err := e.appendTo(ctx, &e.body)
	if err != nil {
		return nil, 0, err
	}
	e.body.writeByte(']')
	return e.body.data, rows, e.body.err
}

// appendTo fills an open JSON array, allowing successive Arrow pages to share
// a request without allocating and copying an encoded body for every page.
func (e *jsonBatchEncoder) appendTo(ctx context.Context, body *boundedJSONBuffer) (int64, error) {
	if e.initErr != nil {
		return 0, e.initErr
	}
	var rows int64
	for e.nextRow < e.record.NumRows() {
		if err := ctx.Err(); err != nil {
			return 0, err
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
				if e.validated[i] {
					e.row.writeValidatedJSON(e.record.Column(e.columns[i]), int(e.nextRow))
				} else {
					e.row.writeValue(e.record.Column(e.columns[i]), int(e.nextRow), false)
				}
			}
			e.row.writeByte('}')
			if e.row.err != nil {
				if errors.Is(e.row.err, errJSONBodyLimit) {
					return 0, fmt.Errorf("a single row (row %d) exceeds the %d-byte Stream Load limit", e.nextRow+1, body.limit)
				}
				return 0, e.row.err
			}
		}
		separator := 0
		if len(body.data) > 1 {
			separator = 1
		}
		if len(e.row.data)+separator+1 > body.limit-len(body.data) {
			e.pending = true
			break
		}
		if separator != 0 {
			body.writeByte(',')
		}
		body.write(e.row.data)
		e.pending = false
		e.nextRow++
		rows++
	}
	return rows, body.err
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
	case *array.FixedSizeBinary:
		b.writeBinary(a.Value(row), nested)
	case *array.List:
		start, end := a.ValueOffsets(row)
		b.writeList(a.ListValues(), int(start), int(end))
	case *array.LargeList:
		start, end := a.ValueOffsets(row)
		b.writeList(a.ListValues(), int(start), int(end))
	case *array.FixedSizeList:
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

// writeValidatedJSON writes a JSON extension value that the source has
// already validated, without validating it again.
func (b *boundedJSONBuffer) writeValidatedJSON(arr arrow.Array, row int) {
	if b.err != nil {
		return
	}
	if arr.IsNull(row) {
		b.writeString("null")
		return
	}
	extension, ok := arr.(array.ExtensionArray)
	if !ok {
		b.writeValue(arr, row, false)
		return
	}
	switch storage := extension.Storage().(type) {
	case *array.String:
		b.writeRawJSON(storage.Value(row))
	case *array.LargeString:
		b.writeRawJSON(storage.Value(row))
	default:
		b.writeValue(arr, row, false)
	}
}

// rawJSONSpecial marks the bytes writeRawJSON must inspect: whitespace,
// quotes, backslashes, HTML characters and the lead byte of U+2028/U+2029.
var rawJSONSpecial = func() (table [256]bool) {
	for _, c := range []byte{' ', '\t', '\n', '\r', '"', '\\', '<', '>', '&', 0xe2} {
		table[c] = true
	}
	return table
}()

// writeRawJSON compacts a validated JSON value and escapes HTML characters
// without allocating another copy of the value, matching
// json.Marshal(json.RawMessage(value)).
func (b *boundedJSONBuffer) writeRawJSON(value string) {
	if b.err != nil {
		return
	}
	const hexDigits = "0123456789abcdef"
	// Whitespace may compact to fit even when the input exceeds the limit.
	if !b.reserve(min(len(value), b.limit-len(b.data))) {
		return
	}
	quoted, escaped, start := false, false, 0
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !rawJSONSpecial[c] {
			escaped = false
			continue
		}
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == '<' || c == '>' || c == '&':
			b.writeString(value[start:i])
			if !b.reserve(6) {
				return
			}
			b.data = append(b.data, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			start = i + 1
		case c == 0xe2:
			if i+2 < len(value) && value[i+1] == 0x80 && (value[i+2] == 0xa8 || value[i+2] == 0xa9) {
				b.writeString(value[start:i])
				if !b.reserve(6) {
					return
				}
				b.data = append(b.data, '\\', 'u', '2', '0', '2', '8'+value[i+2]-0xa8)
				i += 2
				start = i + 1
			}
		case !quoted:
			b.writeString(value[start:i])
			if b.err != nil {
				return
			}
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
