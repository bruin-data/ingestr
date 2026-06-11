package db2

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type dssPacket struct {
	dssType       byte
	chained       bool
	correlationID uint16
	codePoint     uint16
	object        []byte
	moreData      bool
}

func packDSSObject(codePoint uint16, body []byte) []byte {
	out := make([]byte, 4+len(body))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(out)))
	binary.BigEndian.PutUint16(out[2:4], codePoint)
	copy(out[4:], body)
	return out
}

func packBinary(codePoint uint16, value []byte) []byte {
	out := make([]byte, 4+len(value))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(out)))
	binary.BigEndian.PutUint16(out[2:4], codePoint)
	copy(out[4:], value)
	return out
}

func packUint(codePoint uint16, value int, size int) []byte {
	buf := make([]byte, size)
	switch size {
	case 1:
		buf[0] = byte(value)
	case 2:
		binary.BigEndian.PutUint16(buf, uint16(value))
	case 4:
		binary.BigEndian.PutUint32(buf, uint32(value))
	case 8:
		binary.BigEndian.PutUint64(buf, uint64(value))
	default:
		panic("unsupported integer size")
	}
	return packBinary(codePoint, buf)
}

func packString(codePoint uint16, value string, encoding string) ([]byte, error) {
	encoded, err := encodeString(value, encoding)
	if err != nil {
		return nil, err
	}
	return packBinary(codePoint, encoded), nil
}

func packNullString(value string) []byte {
	if value == "" {
		return []byte{0xff}
	}
	raw := []byte(value)
	out := make([]byte, 5+len(raw))
	out[0] = 0
	binary.BigEndian.PutUint32(out[1:5], uint32(len(raw)))
	copy(out[5:], raw)
	return out
}

func encodeString(value string, encoding string) ([]byte, error) {
	switch strings.ToLower(encoding) {
	case "cp500":
		return encodeCP500(value)
	case "utf-8", "utf8":
		return []byte(value), nil
	default:
		return nil, fmt.Errorf("unsupported encoding: %s", encoding)
	}
}

func decodeString(value []byte, encoding string) string {
	switch strings.ToLower(encoding) {
	case "cp500":
		return decodeCP500(value)
	}
	return string(value)
}

func writeRequestDSS(w io.Writer, object []byte, correlationID uint16, nextDSSHasSameID bool, lastPacket bool) (uint16, error) {
	if len(object) < 4 {
		return correlationID, fmt.Errorf("invalid DSS object")
	}

	codePoint := binary.BigEndian.Uint16(object[2:4])
	flag := byte(0x01)
	if codePoint == cpSQLSTT || codePoint == cpSQLATTR || codePoint == cpSQLDTA || codePoint == cpEXTDTA {
		flag = 0x03
	}
	if !lastPacket {
		flag |= 0x40
	}
	nextID := correlationID + 1
	if nextDSSHasSameID {
		flag |= 0x10
		nextID = correlationID
	}

	packet := bytes.NewBuffer(make([]byte, 0, len(object)+6))
	if err := binary.Write(packet, binary.BigEndian, uint16(len(object)+6)); err != nil {
		return correlationID, err
	}
	packet.WriteByte(0xD0)
	packet.WriteByte(flag)
	if err := binary.Write(packet, binary.BigEndian, correlationID); err != nil {
		return correlationID, err
	}
	packet.Write(object)

	_, err := w.Write(packet.Bytes())
	return nextID, err
}

func readDSS(r io.Reader) (dssPacket, error) {
	header := make([]byte, 6)
	if _, err := io.ReadFull(r, header); err != nil {
		return dssPacket{}, err
	}
	if header[2] != 0xD0 {
		return dssPacket{}, fmt.Errorf("invalid DSS header: %s", hex.EncodeToString(header))
	}

	dssLen := binary.BigEndian.Uint16(header[0:2])
	p := dssPacket{
		dssType:       header[3] & 0x0f,
		chained:       header[3]&0x40 != 0,
		correlationID: binary.BigEndian.Uint16(header[4:6]),
	}

	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return dssPacket{}, err
	}
	objLen := binary.BigEndian.Uint16(lenBuf)
	cpBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, cpBuf); err != nil {
		return dssPacket{}, err
	}
	p.codePoint = binary.BigEndian.Uint16(cpBuf)

	if dssLen == 0xffff {
		if p.codePoint != cpQRYDTA {
			return dssPacket{}, fmt.Errorf("extended DSS for unexpected codepoint: 0x%x", p.codePoint)
		}
		chunkLen := int(objLen) - 15
		if chunkLen < 0 {
			return dssPacket{}, fmt.Errorf("invalid extended DSS object length: %d", objLen)
		}
		object := make([]byte, chunkLen)
		if _, err := io.ReadFull(r, object); err != nil {
			return dssPacket{}, err
		}

		nextLenBuf := make([]byte, 2)
		if _, err := io.ReadFull(r, nextLenBuf); err != nil {
			return dssPacket{}, err
		}
		nextLen := binary.BigEndian.Uint16(nextLenBuf)
		if nextLen < 2 {
			return dssPacket{}, fmt.Errorf("invalid extended DSS continuation length: %d", nextLen)
		}
		extra := make([]byte, int(nextLen)-2)
		if _, err := io.ReadFull(r, extra); err != nil {
			return dssPacket{}, err
		}
		object = append(object, extra...)
		p.moreData = nextLen == 0x7ffe
		p.object = object
		return p, nil
	}

	if objLen < 4 {
		return dssPacket{}, fmt.Errorf("invalid DSS object length: %d", objLen)
	}
	object := make([]byte, int(objLen)-4)
	if _, err := io.ReadFull(r, object); err != nil {
		return dssPacket{}, err
	}
	if len(object) != int(dssLen)-10 || int(objLen) != int(dssLen)-6 {
		return dssPacket{}, fmt.Errorf("invalid DSS length: dss=%d object=%d body=%d", dssLen, objLen, len(object))
	}
	p.object = object
	return p, nil
}
