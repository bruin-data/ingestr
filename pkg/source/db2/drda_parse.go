package db2

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type db2Rows struct {
	Columns []db2Column
	Rows    [][]any
}

type db2Column struct {
	Name      string
	SQLType   int
	Length    int
	Precision int
	Scale     int
	Nullable  bool
}

type drdaField struct {
	typeCode byte
	params   []byte
}

type db2SQLError struct {
	Code    int32
	State   string
	Message string
}

func (e db2SQLError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "Db2 returned an error"
	}
	return fmt.Sprintf("%s (SQLCODE=%d SQLSTATE=%s)", message, e.Code, e.State)
}

func parseReplyObject(obj []byte) map[uint16][]byte {
	out := make(map[uint16][]byte)
	for i := 0; i+4 <= len(obj); {
		ln := int(binary.BigEndian.Uint16(obj[i : i+2]))
		if ln < 4 || i+ln > len(obj) {
			break
		}
		cp := binary.BigEndian.Uint16(obj[i+2 : i+4])
		out[cp] = obj[i+4 : i+ln]
		i += ln
	}
	return out
}

func parseSQLCard(obj []byte) ([]byte, error) {
	if len(obj) == 0 {
		return nil, nil
	}
	if obj[0] == 0xff {
		return obj[1:], nil
	}
	if len(obj) < 54 {
		return nil, fmt.Errorf("short SQLCARD: %d bytes", len(obj))
	}
	if obj[0] != 0 {
		return nil, fmt.Errorf("unexpected SQLCARD group flag: 0x%x", obj[0])
	}

	sqlCode := int32(binary.LittleEndian.Uint32(obj[1:5]))
	state := strings.TrimRight(string(obj[5:10]), "\x00 ")
	rest := obj[54:]

	_, rest = parseVarchar(rest)
	messageMain, rest := parseVarchar(rest)
	messageSecondary, rest := parseVarchar(rest)
	message := messageMain
	if message == "" {
		message = messageSecondary
	}

	if sqlCode < 0 {
		return rest, db2SQLError{
			Code:    sqlCode,
			State:   state,
			Message: decodeString([]byte(message), "cp500"),
		}
	}
	return rest, nil
}

func parseSQLDARD(obj []byte) ([]db2Column, error) {
	if len(obj) == 0 {
		return nil, nil
	}

	hasName := obj[0] == 0x00
	rest, err := parseSQLCard(obj)
	if err != nil || len(rest) == 0 {
		return nil, err
	}

	if rest[0] == 0x00 {
		if len(rest) < 13 {
			return nil, nil
		}
		rest = rest[13:]
		_, rest = parseVarchar(rest)
		_, rest = parseName(rest)
	} else {
		rest = rest[1:]
	}

	if len(rest) < 2 {
		return nil, nil
	}
	count := int(binary.LittleEndian.Uint16(rest[:2]))
	rest = rest[2:]

	columns := make([]db2Column, 0, count)
	for i := 0; i < count; i++ {
		if len(rest) == 0 {
			break
		}
		col, next, err := parseDB2Column(rest, hasName)
		if err != nil {
			if len(columns) > 0 {
				break
			}
			return nil, err
		}
		columns = append(columns, col)
		rest = next
	}
	return columns, nil
}

func parseDB2Column(b []byte, hasName bool) (db2Column, []byte, error) {
	if len(b) < 16 {
		return db2Column{}, nil, fmt.Errorf("short SQLDARD column: %d bytes", len(b))
	}

	precision := int(binary.LittleEndian.Uint16(b[:2]))
	scale := int(binary.LittleEndian.Uint16(b[2:4]))
	length := int(binary.LittleEndian.Uint64(b[4:12]))
	sqlType := int(binary.LittleEndian.Uint16(b[12:14]))
	b = b[16:]

	name := ""
	if hasName {
		if len(b) < 9 {
			return db2Column{}, nil, fmt.Errorf("short SQLDARD column name block")
		}
		b = b[6:]
		if b[0] == 0x00 {
			b = b[3:]
		} else {
			b = b[1:]
		}

		sqlName, next := parseName(b)
		label, next := parseName(next)
		_, next = parseName(next)
		b = next
		if label != "" {
			name = label
		} else {
			name = sqlName
		}
		if len(b) >= 7 {
			b = b[7:]
		} else {
			b = nil
		}
	} else {
		if len(b) < 29 {
			return db2Column{}, nil, fmt.Errorf("short SQLDARD unnamed column block")
		}
		b = b[29:]
	}

	return db2Column{
		Name:      strings.TrimRight(name, " "),
		SQLType:   sqlType,
		Length:    length,
		Precision: precision,
		Scale:     scale,
		Nullable:  nullableSQLType(sqlType),
	}, b, nil
}

func parseQRYDSC(obj []byte) ([]drdaField, error) {
	if len(obj) == 0 {
		return nil, fmt.Errorf("empty QRYDSC")
	}
	ln := int(obj[0])
	if ln < 1 || ln > len(obj) {
		return nil, fmt.Errorf("invalid QRYDSC length: %d", ln)
	}
	body := obj[1:ln]
	if len(body) < 2 || body[0] != 0x76 || body[1] != 0xd0 {
		return nil, fmt.Errorf("unsupported QRYDSC descriptor: %s", hex.EncodeToString(body))
	}
	body = body[2:]
	fields := make([]drdaField, 0, len(body)/3)
	for i := 0; i+3 <= len(body); i += 3 {
		fields = append(fields, drdaField{
			typeCode: body[i],
			params:   append([]byte(nil), body[i+1:i+3]...),
		})
	}
	return fields, nil
}

func parseQRYDTA(obj []byte, fields []drdaField) ([][]any, error) {
	if len(fields) == 0 {
		return nil, nil
	}

	stream := bytes.NewReader(obj)
	rows := make([][]any, 0)
	for {
		marker := make([]byte, 2)
		if _, err := io.ReadFull(stream, marker); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		if marker[0] != 0xff {
			break
		}

		row := make([]any, len(fields))
		for i, field := range fields {
			v, err := readDRDAField(stream, field)
			if err != nil {
				return nil, err
			}
			row[i] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func readDRDAField(stream *bytes.Reader, field drdaField) (any, error) {
	if nullableDRDAType(field.typeCode) {
		b, err := stream.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == 0xff {
			return nil, nil
		}
	}

	switch field.typeCode {
	case drdaTypeChar, drdaTypeNChar, drdaTypeGraphic, drdaTypeNGraphic,
		drdaTypeMix, drdaTypeNMix:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return strings.TrimRight(string(data), " "), nil
	case drdaTypeVarchar, drdaTypeNVarchar, drdaTypeLong, drdaTypeNLong,
		drdaTypeVarGraph, drdaTypeNVarGraph, drdaTypeVarMix, drdaTypeNVarMix,
		drdaTypeLongMix, drdaTypeNLongMix:
		lnBytes, err := readBytes(stream, 2)
		if err != nil {
			return nil, err
		}
		ln := int(binary.BigEndian.Uint16(lnBytes))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	case drdaTypeVarByte, drdaTypeNVarByte:
		lnBytes, err := readBytes(stream, 4)
		if err != nil {
			return nil, err
		}
		ln := int(binary.BigEndian.Uint32(lnBytes))
		return readBytes(stream, ln)
	case drdaTypeNRowID:
		ln := int(binary.BigEndian.Uint16(field.params))
		return readBytes(stream, ln)
	case drdaTypeInteger, drdaTypeNInteger, drdaTypeSmall, drdaTypeNSmall,
		drdaTypeOneByte, drdaTypeNOneByte, drdaTypeInteger8, drdaTypeNInteger8:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return signedLittleEndian(data), nil
	case drdaTypeFloat4, drdaTypeNFloat4:
		data, err := readBytes(stream, 4)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(data)), nil
	case drdaTypeFloat8, drdaTypeNFloat8:
		data, err := readBytes(stream, 8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(data)), nil
	case drdaTypeDecimal, drdaTypeNDecimal:
		precision := int(field.params[0])
		scale := int(field.params[1])
		ln := (precision + 2) / 2
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return decodePackedDecimal(data, scale)
	case drdaTypeDate, drdaTypeNDate:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse("2006-01-02", string(data))
		if err != nil {
			return string(data), nil
		}
		return t, nil
	case drdaTypeTime, drdaTypeNTime:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return normalizeDb2Time(string(data)), nil
	case drdaTypeTimestamp, drdaTypeNTimestamp:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		value := normalizeDb2Timestamp(string(data))
		t, err := time.Parse("2006-01-02-15.04.05.000000", value)
		if err != nil {
			return strings.TrimSpace(string(data)), nil
		}
		return t, nil
	case drdaTypeBoolean, drdaTypeNBoolean:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return len(data) > 0 && data[len(data)-1] != 0, nil
	case drdaTypeDecFloat, drdaTypeNDecFloat:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return hex.EncodeToString(data), nil
	case drdaTypeNLobBytes, drdaTypeNLobCSBCS:
		ln := int(binary.BigEndian.Uint16(field.params)) & 0x7fff
		if _, err := readBytes(stream, ln); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported DRDA field type: 0x%x", field.typeCode)
	}
}

func nullableDRDAType(t byte) bool {
	switch t {
	case drdaTypeNInteger, drdaTypeNSmall, drdaTypeNOneByte, drdaTypeNFloat8, drdaTypeNFloat4,
		drdaTypeNDecimal, drdaTypeNInteger8, drdaTypeNRowID, drdaTypeNDate,
		drdaTypeNTime, drdaTypeNTimestamp, drdaTypeNVarByte, drdaTypeNChar,
		drdaTypeNVarchar, drdaTypeNLong, drdaTypeNGraphic, drdaTypeNVarGraph,
		drdaTypeNMix, drdaTypeNVarMix, drdaTypeNLongMix, drdaTypeNBoolean,
		drdaTypeNDecFloat, drdaTypeNLobBytes, drdaTypeNLobCSBCS:
		return true
	default:
		return false
	}
}

func readBytes(r *bytes.Reader, n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative read length: %d", n)
	}
	out := make([]byte, n)
	_, err := io.ReadFull(r, out)
	return out, err
}

func signedLittleEndian(data []byte) int64 {
	switch len(data) {
	case 1:
		return int64(int8(data[0]))
	case 2:
		return int64(int16(binary.LittleEndian.Uint16(data)))
	case 4:
		return int64(int32(binary.LittleEndian.Uint32(data)))
	case 8:
		return int64(binary.LittleEndian.Uint64(data))
	default:
		var v int64
		for i := len(data) - 1; i >= 0; i-- {
			v = (v << 8) | int64(data[i])
		}
		return v
	}
}

func decodePackedDecimal(data []byte, scale int) (decimal.Decimal, error) {
	if len(data) == 0 {
		return decimal.Zero, nil
	}
	encoded := hex.EncodeToString(data)
	signNibble := encoded[len(encoded)-1]
	digits := strings.TrimLeft(encoded[:len(encoded)-1], "0")
	if digits == "" {
		digits = "0"
	}
	i, ok := new(strings.Builder), true
	for _, r := range digits {
		if r < '0' || r > '9' {
			ok = false
			break
		}
		i.WriteRune(r)
	}
	if !ok {
		return decimal.Zero, fmt.Errorf("invalid packed decimal: %s", encoded)
	}
	v, err := decimal.NewFromString(i.String())
	if err != nil {
		return decimal.Zero, err
	}
	v = v.Shift(int32(-scale))
	if signNibble == 'd' || signNibble == 'b' {
		v = v.Neg()
	}
	return v, nil
}

func normalizeDb2Timestamp(value string) string {
	value = strings.TrimSpace(value)
	const baseLen = len("2006-01-02-15.04.05")
	if len(value) == baseLen {
		return value + ".000000"
	}
	if len(value) > baseLen && value[baseLen] == '.' {
		fraction := value[baseLen+1:]
		if len(fraction) > 6 {
			fraction = fraction[:6]
		}
		if len(fraction) < 6 {
			fraction += strings.Repeat("0", 6-len(fraction))
		}
		return value[:baseLen+1] + fraction
	}
	return value
}

func normalizeDb2Time(value string) string {
	value = strings.TrimSpace(value)
	const baseLen = len("15.04.05")
	if len(value) < baseLen {
		return strings.ReplaceAll(value, ".", ":")
	}

	base := strings.ReplaceAll(value[:baseLen], ".", ":")
	if len(value) == baseLen {
		return base
	}
	if value[baseLen] != '.' {
		return strings.ReplaceAll(value, ".", ":")
	}

	fraction := value[baseLen+1:]
	if len(fraction) > 6 {
		fraction = fraction[:6]
	}
	if len(fraction) < 6 {
		fraction += strings.Repeat("0", 6-len(fraction))
	}
	return base + "." + fraction
}

func parseVarchar(b []byte) (string, []byte) {
	if len(b) < 2 {
		return "", nil
	}
	ln := int(binary.BigEndian.Uint16(b[:2]))
	if len(b) < 2+ln {
		return "", nil
	}
	return string(b[2 : 2+ln]), b[2+ln:]
}

func parseName(b []byte) (string, []byte) {
	first, rest := parseVarchar(b)
	second, rest := parseVarchar(rest)
	if first != "" {
		return first, rest
	}
	return second, rest
}

func parseDiagnostic(obj []byte) string {
	fields := parseReplyObject(obj)
	if v := fields[cpSRVDGN]; len(v) > 0 {
		return decodeString(v, "cp500")
	}
	return ""
}
