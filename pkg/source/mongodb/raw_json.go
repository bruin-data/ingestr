package mongodb

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/bson"
)

type rawBSONJSONField struct {
	key   string
	value bson.RawValue
}

func rawBSONValueAsJSONString(val bson.RawValue) (string, bool) {
	var buf bytes.Buffer
	if !appendRawBSONJSONValue(&buf, val) {
		return "", false
	}
	return buf.String(), true
}

func appendRawBSONDocumentJSON(buf *bytes.Buffer, doc bson.Raw) bool {
	elements, err := doc.Elements()
	if err != nil {
		return false
	}

	fields := make([]rawBSONJSONField, 0, len(elements))
	for _, elem := range elements {
		key := elem.Key()
		replaced := false
		for i := range fields {
			if fields[i].key == key {
				fields[i].value = elem.Value()
				replaced = true
				break
			}
		}
		if !replaced {
			fields = append(fields, rawBSONJSONField{key: key, value: elem.Value()})
		}
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].key < fields[j].key
	})

	buf.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		appendJSONString(buf, field.key)
		buf.WriteByte(':')
		if !appendRawBSONJSONValue(buf, field.value) {
			return false
		}
	}
	buf.WriteByte('}')
	return true
}

func appendRawBSONArrayJSON(buf *bytes.Buffer, arr bson.Raw) bool {
	values, err := arr.Values()
	if err != nil {
		return false
	}

	buf.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			buf.WriteByte(',')
		}
		if !appendRawBSONJSONValue(buf, value) {
			return false
		}
	}
	buf.WriteByte(']')
	return true
}

func appendRawBSONJSONValue(buf *bytes.Buffer, val bson.RawValue) bool {
	switch val.Type {
	case bson.TypeDouble:
		v, ok := val.DoubleOK()
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		appendJSONFloat64(buf, v)
		return true
	case bson.TypeString:
		v, ok := val.StringValueOK()
		if !ok {
			return false
		}
		appendJSONString(buf, v)
		return true
	case bson.TypeEmbeddedDocument:
		doc, ok := val.DocumentOK()
		return ok && appendRawBSONDocumentJSON(buf, doc)
	case bson.TypeArray:
		arr, ok := val.ArrayOK()
		return ok && appendRawBSONArrayJSON(buf, arr)
	case bson.TypeBinary:
		_, data, ok := val.BinaryOK()
		if !ok {
			return false
		}
		appendJSONString(buf, base64.StdEncoding.EncodeToString(data))
		return true
	case bson.TypeObjectID:
		v, ok := val.ObjectIDOK()
		if !ok {
			return false
		}
		appendJSONString(buf, v.Hex())
		return true
	case bson.TypeBoolean:
		v, ok := val.BooleanOK()
		if !ok {
			return false
		}
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return true
	case bson.TypeDateTime:
		v, ok := val.DateTimeOK()
		return ok && appendJSONTime(buf, time.UnixMilli(v))
	case bson.TypeRegex:
		pattern, _, ok := val.RegexOK()
		if !ok {
			return false
		}
		appendJSONString(buf, pattern)
		return true
	case bson.TypeJavaScript:
		v, ok := val.JavaScriptOK()
		if !ok {
			return false
		}
		appendJSONString(buf, v)
		return true
	case bson.TypeSymbol:
		v, ok := val.SymbolOK()
		if !ok {
			return false
		}
		appendJSONString(buf, v)
		return true
	case bson.TypeInt32:
		v, ok := val.Int32OK()
		if !ok {
			return false
		}
		buf.WriteString(strconv.FormatInt(int64(v), 10))
		return true
	case bson.TypeTimestamp:
		t, _, ok := val.TimestampOK()
		return ok && appendJSONTime(buf, time.Unix(int64(t), 0))
	case bson.TypeInt64:
		v, ok := val.Int64OK()
		if !ok {
			return false
		}
		buf.WriteString(strconv.FormatInt(v, 10))
		return true
	case bson.TypeDecimal128:
		v, ok := val.Decimal128OK()
		if !ok {
			return false
		}
		appendJSONString(buf, v.String())
		return true
	case bson.TypeUndefined, bson.TypeMinKey, bson.TypeMaxKey:
		buf.WriteString("{}")
		return true
	case bson.TypeNull:
		buf.WriteString("null")
		return true
	default:
		return appendRawJSONValue(buf, convertRawBSONValue(val))
	}
}

func appendJSONString(buf *bytes.Buffer, value string) {
	if stringNeedsNoEscaping(value) {
		buf.WriteByte('"')
		buf.WriteString(value)
		buf.WriteByte('"')
		return
	}
	if !utf8.ValidString(value) {
		_ = appendRawJSONValue(buf, value)
		return
	}

	start := 0
	buf.WriteByte('"')
	for i, c := range value {
		if c == utf8.RuneError {
			continue
		}
		if c < 0x20 || c == '\\' || c == '"' || c == '\u2028' || c == '\u2029' {
			buf.WriteString(value[start:i])
			switch c {
			case '\\', '"':
				buf.WriteByte('\\')
				buf.WriteRune(c)
			case '\b':
				buf.WriteString(`\b`)
			case '\f':
				buf.WriteString(`\f`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				switch c {
				case '\u2028':
					buf.WriteString(`\u2028`)
				case '\u2029':
					buf.WriteString(`\u2029`)
				default:
					buf.WriteString(`\u00`)
					buf.WriteByte("0123456789abcdef"[byte(c)>>4])
					buf.WriteByte("0123456789abcdef"[byte(c)&0xF])
				}
			}
			start = i + utf8.RuneLen(c)
		}
	}
	buf.WriteString(value[start:])
	buf.WriteByte('"')
}

func stringNeedsNoEscaping(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x20 || c == '\\' || c == '"' {
			return false
		}
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func appendJSONFloat64(buf *bytes.Buffer, value float64) {
	format := byte('f')
	abs := math.Abs(value)
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}

	out := strconv.AppendFloat(nil, value, format, -1, 64)
	if format == 'e' {
		n := len(out)
		if n >= 4 && out[n-4] == 'e' && out[n-3] == '-' && out[n-2] == '0' {
			out[n-2] = out[n-1]
			out = out[:n-1]
		} else if n >= 4 && out[n-4] == 'e' && out[n-3] == '+' && out[n-2] == '0' {
			out[n-2] = out[n-1]
			out = out[:n-1]
		}
	}
	buf.Write(out)
}

func appendJSONTime(buf *bytes.Buffer, value time.Time) bool {
	start := buf.Len()
	buf.WriteByte('"')
	formatted := value.AppendFormat(buf.AvailableBuffer(), time.RFC3339Nano)
	if !strictRFC3339Time(formatted) {
		buf.Truncate(start)
		return appendRawJSONValue(buf, value)
	}

	buf.Write(formatted)
	buf.WriteByte('"')
	return true
}

func strictRFC3339Time(value []byte) bool {
	if len(value) <= len("9999") || value[len("9999")] != '-' {
		return false
	}
	if value[len(value)-1] == 'Z' {
		return true
	}

	c := value[len(value)-len("Z07:00")]
	if '0' <= c && c <= '9' {
		return false
	}
	zoneHour := 10*(value[len(value)-len("07:00")]-'0') + (value[len(value)-len("7:00")] - '0')
	return zoneHour < 24
}

func appendRawJSONValue(buf *bytes.Buffer, value any) bool {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return false
	}
	out := encoded.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	buf.Write(out)
	return true
}
