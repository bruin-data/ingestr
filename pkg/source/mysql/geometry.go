package mysql

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// mysqlGeometryToWKT converts a value in MySQL's internal geometry format (a
// 4-byte little-endian SRID followed by WKB), as returned by both the driver
// and the binlog, to WKT. Coordinates are written in stored X Y order, which
// for geographic SRSs is longitude latitude. The SRID is not included.
func mysqlGeometryToWKT(value []byte) (string, error) {
	if len(value) < 4 {
		return "", fmt.Errorf("MySQL geometry value is %d bytes; expected a 4-byte SRID followed by WKB", len(value))
	}
	r := &wkbReader{data: value[4:]}
	var b strings.Builder
	if err := r.writeGeometry(&b, 0); err != nil {
		return "", fmt.Errorf("invalid MySQL geometry value: %w", err)
	}
	if len(r.data) != 0 {
		return "", fmt.Errorf("invalid MySQL geometry value: %d trailing bytes", len(r.data))
	}
	return b.String(), nil
}

const (
	wkbPoint              = 1
	wkbLineString         = 2
	wkbPolygon            = 3
	wkbMultiPoint         = 4
	wkbMultiLineString    = 5
	wkbMultiPolygon       = 6
	wkbGeometryCollection = 7
)

var wkbTypeNames = map[uint32]string{
	wkbPoint:              "POINT",
	wkbLineString:         "LINESTRING",
	wkbPolygon:            "POLYGON",
	wkbMultiPoint:         "MULTIPOINT",
	wkbMultiLineString:    "MULTILINESTRING",
	wkbMultiPolygon:       "MULTIPOLYGON",
	wkbGeometryCollection: "GEOMETRYCOLLECTION",
}

type wkbReader struct {
	data  []byte
	order binary.ByteOrder
}

func (r *wkbReader) take(n int) ([]byte, error) {
	if len(r.data) < n {
		return nil, fmt.Errorf("truncated WKB")
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out, nil
}

func (r *wkbReader) uint32() (uint32, error) {
	buf, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return r.order.Uint32(buf), nil
}

// count reads an element count and rejects counts that cannot fit in the
// remaining bytes, so corrupt input cannot trigger large allocations.
func (r *wkbReader) count(minElementBytes int) (int, error) {
	n, err := r.uint32()
	if err != nil {
		return 0, err
	}
	if uint64(n)*uint64(minElementBytes) > uint64(len(r.data)) {
		return 0, fmt.Errorf("truncated WKB")
	}
	return int(n), nil
}

func (r *wkbReader) header() (uint32, error) {
	orderByte, err := r.take(1)
	if err != nil {
		return 0, err
	}
	switch orderByte[0] {
	case 0:
		r.order = binary.BigEndian
	case 1:
		r.order = binary.LittleEndian
	default:
		return 0, fmt.Errorf("unknown WKB byte order %d", orderByte[0])
	}
	geomType, err := r.uint32()
	if err != nil {
		return 0, err
	}
	if _, ok := wkbTypeNames[geomType]; !ok {
		return 0, fmt.Errorf("unsupported WKB geometry type %d", geomType)
	}
	return geomType, nil
}

// writeGeometry writes one WKB geometry. Members of a MULTI* geometry must
// have the matching single type (want) and are written without a type tag.
func (r *wkbReader) writeGeometry(b *strings.Builder, want uint32) error {
	geomType, err := r.header()
	if err != nil {
		return err
	}
	if want == 0 {
		b.WriteString(wkbTypeNames[geomType])
	} else if geomType != want {
		return fmt.Errorf("expected a %s member, got %s", wkbTypeNames[want], wkbTypeNames[geomType])
	}
	switch geomType {
	case wkbPoint:
		b.WriteByte('(')
		if err := r.writePoint(b); err != nil {
			return err
		}
		b.WriteByte(')')
		return nil
	case wkbLineString:
		return r.writePointList(b)
	case wkbPolygon:
		return r.writeRings(b)
	case wkbMultiPoint, wkbMultiLineString, wkbMultiPolygon, wkbGeometryCollection:
		n, err := r.count(5)
		if err != nil {
			return err
		}
		if n == 0 {
			b.WriteString(" EMPTY")
			return nil
		}
		b.WriteByte('(')
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			var member uint32
			if geomType != wkbGeometryCollection {
				member = geomType - 3
			}
			if err := r.writeGeometry(b, member); err != nil {
				return err
			}
		}
		b.WriteByte(')')
		return nil
	}
	return nil
}

func (r *wkbReader) writeRings(b *strings.Builder) error {
	n, err := r.count(4)
	if err != nil {
		return err
	}
	if n == 0 {
		b.WriteString(" EMPTY")
		return nil
	}
	b.WriteByte('(')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := r.writePointList(b); err != nil {
			return err
		}
	}
	b.WriteByte(')')
	return nil
}

func (r *wkbReader) writePointList(b *strings.Builder) error {
	n, err := r.count(16)
	if err != nil {
		return err
	}
	if n == 0 {
		b.WriteString(" EMPTY")
		return nil
	}
	b.WriteByte('(')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := r.writePoint(b); err != nil {
			return err
		}
	}
	b.WriteByte(')')
	return nil
}

func (r *wkbReader) writePoint(b *strings.Builder) error {
	buf, err := r.take(16)
	if err != nil {
		return err
	}
	b.WriteString(formatWKTCoordinate(math.Float64frombits(r.order.Uint64(buf[:8]))))
	b.WriteByte(' ')
	b.WriteString(formatWKTCoordinate(math.Float64frombits(r.order.Uint64(buf[8:]))))
	return nil
}

func formatWKTCoordinate(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
