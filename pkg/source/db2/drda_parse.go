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
	"unicode/utf16"

	"github.com/bruin-data/ingestr/internal/config"
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

// parseSECCHKRM inspects a SECCHKRM reply's SVRCOD/SECCHKCD fields. DRDA
// defines SVRCOD 0 as success; any nonzero value (4=WARNING, 8=ERROR,
// 16=SEVERE, ...) means the security check failed, with SECCHKCD giving the
// specific reason code. Every call site previously treated any SECCHKRM as
// "authenticated" without looking at either field - a real IBM i host
// confirmed via packet capture to return SVRCOD=16 on every connection in
// this investigation, while the client silently proceeded to ACCRDB, which
// the server then dropped because the session was never actually
// authenticated.
func parseSECCHKRM(obj []byte) error {
	fields := parseReplyObject(obj)
	v, ok := fields[cpSVRCOD]
	if !ok || len(v) < 2 {
		return nil
	}
	svrcod := binary.BigEndian.Uint16(v[:2])
	if svrcod == 0 {
		return nil
	}
	secchkcd := -1
	if sv, ok := fields[cpSECCHKCD]; ok && len(sv) >= 1 {
		secchkcd = int(sv[0])
	}
	return fmt.Errorf("authentication rejected by server (SECCHKRM SVRCOD=%d, SECCHKCD=%d): the negotiated security mechanism's credentials were not accepted", svrcod, secchkcd)
}

// parseSQLCard returns the remaining bytes after the SQLCARD, the SQLCODE it
// carried (0 when the object is the empty/0xff "no SQLCARD" sentinel), and an
// error for a negative SQLCODE. Callers that need to recognize SQLCODE +100
// ("no more rows") - a valid, non-error end-of-cursor signal some DB2 flavors
// send instead of, or in addition to, ENDQRYRM (reported on issue #3) - must
// inspect the returned code themselves; a positive SQLCODE is never an error.
//
// SQLCODE is a binary integer field, so like every other binary integer this
// client reads off the wire, its byte order follows the connection's
// negotiated TYPDEFNAM/platform, not a hardcoded assumption. Reported on
// issue #3 against a real IBM i server: a SQLCODE of +100 (0x00000064,
// big-endian) was previously always read as little-endian regardless of
// platform, producing 0x64000000 = 1677721600 - which meant the SQLCODE +100
// end-of-cursor path could never fire on IBM i, and a real negative SQLCODE
// (an actual server error) would have been misread the same way.
func parseSQLCard(obj []byte, platform db2Platform) ([]byte, int32, error) {
	if len(obj) == 0 {
		return nil, 0, nil
	}
	if obj[0] == 0xff {
		return obj[1:], 0, nil
	}
	if len(obj) < 54 {
		return nil, 0, fmt.Errorf("short SQLCARD: %d bytes", len(obj))
	}
	if obj[0] != 0 {
		return nil, 0, fmt.Errorf("unexpected SQLCARD group flag: 0x%x", obj[0])
	}

	sqlCode := int32(numericByteOrderFor(platform).Uint32(obj[1:5]))
	// SQLSTATE is DRDA protocol-level metadata, not job-CCSID column data -
	// the same category as RDBNAM/PRDID/SQLNAME, which this client already
	// reads as CCSID 500 regardless of platform (see characterEncodingFor's
	// own doc comment). The message text a few lines down already decodes
	// as cp500 unconditionally; SQLSTATE previously didn't, reading it as
	// raw ASCII bytes instead - inconsistent with that, and not yet
	// confirmed against a real non-ASCII capture, so logged as raw hex by
	// the caller for the next real SQLCARD to confirm.
	state := strings.TrimRight(decodeString(obj[5:10], "cp500"), "\x00 ")
	rest := obj[54:]

	_, rest = parseVarchar(rest)
	messageMain, rest := parseVarchar(rest)
	messageSecondary, rest := parseVarchar(rest)
	message := messageMain
	if message == "" {
		message = messageSecondary
	}

	if sqlCode < 0 {
		return rest, sqlCode, db2SQLError{
			Code:    sqlCode,
			State:   state,
			Message: decodeString([]byte(message), "cp500"),
		}
	}
	return rest, sqlCode, nil
}

// characterEncodingFor returns the encoding character data actually comes
// back in for a given platform. IBM i returns job CCSID (37) for
// CHAR/VARCHAR data and SQLDARD column names regardless of what ACCRDB's
// TYPDEFOVR requests (confirmed on issue #3: SYSCOLUMNS/SYSTABLES text
// arrived as EBCDIC despite TYPDEFOVR asking for CCSID 1208/UTF-8). Other
// platforms keep this client's existing UTF-8 assumption, which nothing
// in this investigation has contradicted.
func characterEncodingFor(platform db2Platform) string {
	if platform == db2PlatformIBMi {
		return "cp37"
	}
	return "utf-8"
}

func parseSQLDARD(obj []byte, platform db2Platform) ([]db2Column, error) {
	if len(obj) == 0 {
		return nil, nil
	}

	// This client always requests RTNSQLDA=0xf1 ("with names") on both
	// PRPSQLSTT and DSCSQLSTT (see packPRPSQLSTT/packDSCSQLSTT), so every
	// SQLDARD this client receives includes a name block per column.
	// Previously derived from obj[0] (the SQLCAGRP null indicator, a
	// field with no relation to column naming) - confirmed wrong on issue
	// #3 against a real IBM i capture where obj[0] was 0xff (SQLCAGRP
	// null) yet every column clearly carried SQLNAME/SQLLABEL data, which
	// the old hasName=false path skipped over entirely (reading a flat
	// 29-byte block per column instead of the real variable-length name
	// block), producing a parse that silently read 0 usable columns.
	hasName := true
	rest, _, err := parseSQLCard(obj, platform)
	if err != nil || len(rest) == 0 {
		return nil, err
	}

	// SQLDHGRP: a 24-byte header (1-byte null indicator + 23 bytes this
	// client doesn't otherwise use) - confirmed on issue #3 against five
	// real captures of varying column counts (1/4/5/32/72), which all
	// placed SQLNUMROWS at a fixed absolute offset of 0x18 (24) from the
	// start of the SQLDARD content. An earlier fix assumed a 13-byte
	// header based on a single, incompletely-decoded capture; the
	// corrected 24-byte figure (23 bytes to skip from here, since rest
	// already starts 1 byte into the object) is cross-checked against
	// every capture, not just one.
	config.Debug("[DB2] parseSQLDARD: header bytes before SQLNUMROWS: %x", rest[:min(len(rest), 24)])
	if rest[0] == 0x00 {
		if len(rest) < 23 {
			return nil, nil
		}
		rest = rest[23:]
	} else {
		rest = rest[1:]
	}

	if len(rest) < 4 {
		return nil, nil
	}
	// SQLNUMROWS is a 4-byte count, not 2 - confirmed on issue #3 against
	// four real captures (00000001, 00000004, 00000005, 00000020 for 1/4/
	// 5/32 columns respectively), read in the platform's numeric byte
	// order like every other binary numeric field on the wire (see
	// numericByteOrderFor's own doc comment for why TYPDEFNAM governs
	// this).
	count := int(numericByteOrderFor(platform).Uint32(rest[:4]))
	rest = rest[4:]
	config.Debug("[DB2] parseSQLDARD: SQLNUMROWS=%d", count)

	columns := make([]db2Column, 0, count)
	for i := 0; i < count; i++ {
		if len(rest) == 0 {
			break
		}
		col, next, err := parseDB2Column(rest, hasName, platform)
		if err != nil {
			config.Debug("[DB2] parseSQLDARD: column %d parse error: %v, remaining bytes=%x", i, err, rest)
			if len(columns) > 0 {
				break
			}
			return nil, err
		}
		config.Debug("[DB2] parseSQLDARD: column %d name=%q sqltype=%d length=%d precision=%d scale=%d", i, col.Name, col.SQLType, col.Length, col.Precision, col.Scale)
		columns = append(columns, col)
		rest = next
	}
	return columns, nil
}

func parseDB2Column(b []byte, hasName bool, platform db2Platform) (db2Column, []byte, error) {
	if len(b) < 16 {
		return db2Column{}, nil, fmt.Errorf("short SQLDARD column: %d bytes", len(b))
	}

	// Confirmed on issue #3 against a real capture: precision/scale/
	// length/sqlType are binary numeric fields subject to the same
	// TYPDEFNAM-driven byte order as everything else on the wire (see
	// numericByteOrderFor) - previously hardcoded little-endian
	// regardless of platform, which on IBM i read a 128-byte VARCHAR's
	// length (wire bytes 00 00 00 00 00 00 00 80) as an astronomically
	// large little-endian value, and SQLTYPE 448/VARCHAR (wire bytes 01
	// c0) as 0xc001 (49153), neither of which is a valid SQL type.
	order := numericByteOrderFor(platform)
	precision := int(order.Uint16(b[:2]))
	scale := int(order.Uint16(b[2:4]))
	length := int(order.Uint64(b[4:12]))
	sqlType := int(order.Uint16(b[12:14]))
	b = b[16:]

	name := ""
	if hasName {
		// Confirmed on issue #3 by offline-parsing two full real captures
		// (4 and 72 columns, 280 and 5909 bytes) against this exact layout
		// and consuming every byte with nothing left over, across 76
		// columns spanning VARCHAR/CHAR/DECIMAL/NUMERIC/INTEGER: 9 reserved
		// bytes (null indicator, SQLUNNAMED, 6 more), then SQLNAME, then
		// SQLLABEL, then SQLCOMMENTS (each a VCM-then-VCS pair - parseName
		// already implements that framing correctly, prefer-first-non-
		// empty), then 2 more reserved bytes, then a 2-byte 0xFF 0xFF
		// trailer. The previous "6 bytes + conditional 1-or-3-byte skip"
		// and "7-byte tail" were both wrong (based on one partially
		// decoded capture), which misaligned every column after the
		// first.
		if len(b) < 9 {
			return db2Column{}, nil, fmt.Errorf("short SQLDARD column name block")
		}
		b = b[9:]

		sqlName, next := parseName(b)
		label, next := parseName(next)
		_, next = parseName(next)
		if len(next) < 2 {
			return db2Column{}, nil, fmt.Errorf("short SQLDARD column comments tail")
		}
		next = next[2:]
		b = next
		// Confirmed on issue #3: the previous "prefer label, fall back to
		// SQLNAME" order reported the column's LABEL text as its name,
		// masked on queries where SQLNAME and SQLLABEL happen to be
		// identical. SQLNAME is the real column identifier.
		if sqlName != "" {
			name = sqlName
		} else {
			name = label
		}
		// parseName/parseVarchar return the raw bytes as a Go string with
		// no decoding (see their own docs) - decode here, once. Confirmed
		// on issue #3: SQLNAME/SQLLABEL are DRDA protocol-level metadata
		// (like RDBNAM/PKGNAMCSN elsewhere in this client), encoded in
		// cp500 regardless of platform - not the platform's data CCSID
		// (characterEncodingFor), which is what the actual column DATA
		// arrives in, a different thing.
		name = decodeString([]byte(name), "cp500")
		if len(b) >= 2 {
			b = b[2:]
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

// sdaDescriptor is a parsed SDA (Self-Defining type) triplet: it defines a
// Local IDentifier (LID) a column's own type byte in the N-GDA triplet can
// reference instead of a literal FD:OCA primitive type. Confirmed on issue
// #3 via a real IBM i host's QRYDSC for a query with VARGRAPHIC columns -
// SDA triplets there defined LIDs 0x80/0x81 (base type 0x11/0x91, CCSID
// 1200, 2 bytes/char), which the N-GDA triplet's column entries then used
// directly as their type byte. The LID's own parity (even/odd) follows
// this client's existing non-null/nullable convention for every other
// FD:OCA type pair (0x38/0x39 etc.) - confirmed against the same capture
// (0x80, even, NOT NULL column; 0x81, odd, nullable column).
type sdaDescriptor struct {
	lid          byte
	baseType     byte
	ccsid        int
	bytesPerChar int
	// isLOB is set when the SDA's own maxLength field has its high bit set -
	// the same "data carried via EXTDTA, not inline" signal this client
	// already relies on for the literal FD:OCA LOB type codes (see the
	// "& 0x7fff" mask in readDRDAField's drdaTypeNLobBytes/NLobCSBCS case).
	// Confirmed on issue #3 for a DBCLOB column resolved through an SDA LID:
	// base_type 0xd1, maxLength 0x8008, N-GDA param bytes 0x8009 - both
	// carrying the same high bit. A column like this must not be resolved
	// to VARGRAPHIC just because it shares VARGRAPHIC's CCSID/bytesPerChar.
	isLOB bool
}

// parseSDATriplet decodes an SDA triplet's body: LID(1) baseType(1)
// reserved(2) CCSID(2) bytesPerChar(1) flag(1) maxLength(2) - 10 bytes
// after the triplet-type byte, confirmed byte-for-byte against the real
// capture on issue #3.
func parseSDATriplet(triplet []byte) (sdaDescriptor, bool) {
	body := triplet[1:]
	if len(body) < 10 {
		return sdaDescriptor{}, false
	}
	maxLength := binary.BigEndian.Uint16(body[8:10])
	return sdaDescriptor{
		lid:          body[0],
		baseType:     body[1],
		ccsid:        int(binary.BigEndian.Uint16(body[4:6])),
		bytesPerChar: int(body[6]),
		isLOB:        maxLength&0x8000 != 0,
	}, true
}

// resolveSDALID maps an SDA-defined LID to a type this client already
// knows how to decode, using only what's actually been confirmed: CCSID
// 1200/13488 with 2 bytes/char is GRAPHIC/VARGRAPHIC data, decoded via the
// existing drdaTypeVarGraph/NVarGraph path (variable-length framing, UTF-
// 16BE content) with nullability from the LID's own parity. Anything else
// is left as the bare LID value, which readDRDAField's "unsupported DRDA
// field type" error (with column index) will surface clearly rather than
// silently mis-decode - there's no confirmed mapping for it yet.
func resolveSDALID(desc sdaDescriptor) byte {
	if (desc.ccsid == 1200 || desc.ccsid == 13488) && desc.bytesPerChar == 2 {
		if desc.lid%2 == 1 {
			return drdaTypeNVarGraph
		}
		return drdaTypeVarGraph
	}
	return desc.lid
}

func parseQRYDSC(obj []byte) ([]drdaField, error) {
	if len(obj) == 0 {
		return nil, fmt.Errorf("empty QRYDSC")
	}
	sdaLIDs := make(map[byte]sdaDescriptor)
	for len(obj) > 0 {
		ln := int(obj[0])
		if ln < 1 || ln > len(obj) {
			return nil, fmt.Errorf("invalid QRYDSC triplet length: %d", ln)
		}
		triplet := obj[1:ln]
		obj = obj[ln:]
		if len(triplet) == 0 {
			continue
		}
		switch triplet[0] {
		case fdocaTripletTypeNGDA:
			if len(triplet) < 2 || triplet[1] != fdocaSQLDTAGRPLID {
				return nil, fmt.Errorf("unsupported QRYDSC N-GDA triplet: %s", hex.EncodeToString(triplet))
			}
			body := triplet[2:]
			fields := make([]drdaField, 0, len(body)/3)
			for i := 0; i+3 <= len(body); i += 3 {
				typeCode := body[i]
				if desc, ok := sdaLIDs[typeCode]; ok {
					if desc.isLOB {
						return nil, fmt.Errorf("column %d: LOB data via SDA LID 0x%x (base_type 0x%x, ccsid %d) is not supported - exclude this column from the query", i/3, typeCode, desc.baseType, desc.ccsid)
					}
					resolved := resolveSDALID(desc)
					config.Debug("[DB2] QRYDSC: column type 0x%x resolved via SDA LID to 0x%x (ccsid=%d, bytes_per_char=%d)", typeCode, resolved, desc.ccsid, desc.bytesPerChar)
					typeCode = resolved
				}
				fields = append(fields, drdaField{
					typeCode: typeCode,
					params:   append([]byte(nil), body[i+1:i+3]...),
				})
			}
			config.Debug("[DB2] QRYDSC N-GDA triplet: %d column(s), raw bytes=%x", len(fields), triplet)
			return fields, nil
		case fdocaTripletTypeMDD:
			// Metadata descriptor - precedes the N-GDA triplet for result
			// sets with GRAPHIC/LOB columns, carries nothing this client
			// needs. Skip and keep looking for the N-GDA triplet.
			config.Debug("[DB2] QRYDSC: skipping MDD triplet, raw bytes=%x", triplet)
			continue
		case fdocaTripletTypeSDA:
			if desc, ok := parseSDATriplet(triplet); ok {
				sdaLIDs[desc.lid] = desc
				config.Debug("[DB2] QRYDSC: SDA LID=0x%x base_type=0x%x ccsid=%d bytes_per_char=%d", desc.lid, desc.baseType, desc.ccsid, desc.bytesPerChar)
			} else {
				config.Debug("[DB2] QRYDSC: SDA triplet too short to parse, raw bytes=%x", triplet)
			}
			continue
		default:
			// Any other FD:OCA triplet type (RLO, CPT, ...): skip rather
			// than fail outright, since no per-column type information
			// this client needs lives outside the N-GDA/SDA triplets.
			config.Debug("[DB2] QRYDSC: skipping triplet type 0x%x, raw bytes=%x", triplet[0], triplet)
			continue
		}
	}
	return nil, fmt.Errorf("QRYDSC has no N-GDA triplet")
}

func parseQRYDTA(obj []byte, fields []drdaField, platform db2Platform) ([][]any, error) {
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
			v, err := readDRDAField(stream, field, platform)
			if err != nil {
				return nil, fmt.Errorf("column %d (DRDA type 0x%x): %w", i, field.typeCode, err)
			}
			row[i] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// decodeUTF16BE decodes GRAPHIC/VARGRAPHIC column data (CCSID 1200/13488),
// which IBM i sends as big-endian UTF-16 - a different, double-byte-per-
// character encoding from every other character type this client decodes
// (CHAR/VARCHAR's single-byte cp37/cp500). An odd byte count is malformed
// input for this encoding, not something to silently truncate.
func decodeUTF16BE(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("odd-length UTF-16BE graphic data: %d bytes", len(data))
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(data[i*2 : i*2+2])
	}
	return string(utf16.Decode(units)), nil
}

func readDRDAField(stream *bytes.Reader, field drdaField, platform db2Platform) (any, error) {
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
	case drdaTypeChar, drdaTypeNChar, drdaTypeMix, drdaTypeNMix:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return strings.TrimRight(decodeString(data, characterEncodingFor(platform)), " "), nil
	case drdaTypeGraphic, drdaTypeNGraphic:
		// GRAPHIC is DBCS data - CCSID 1200/13488, i.e. big-endian UTF-16 -
		// a fundamentally different (double-byte) encoding from CHAR's
		// single-byte cp37/cp500, not just a different code page. Reported
		// on issue #3 as the cause of an "unsupported QRYDSC descriptor"
		// failure (actually an unrelated FD:OCA framing issue, fixed
		// separately in parseQRYDSC) on tables with GRAPHIC/VARGRAPHIC
		// columns.
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		decoded, err := decodeUTF16BE(data)
		if err != nil {
			return nil, err
		}
		return strings.TrimRight(decoded, " "), nil
	case drdaTypeVarchar, drdaTypeNVarchar, drdaTypeLong, drdaTypeNLong,
		drdaTypeVarMix, drdaTypeNVarMix, drdaTypeLongMix, drdaTypeNLongMix:
		lnBytes, err := readBytes(stream, 2)
		if err != nil {
			return nil, err
		}
		ln := int(binary.BigEndian.Uint16(lnBytes))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return decodeString(data, characterEncodingFor(platform)), nil
	case drdaTypeVarGraph, drdaTypeNVarGraph:
		// See drdaTypeGraphic above - same UTF-16BE encoding, variable-
		// length framing (2-byte inline length prefix) instead of fixed.
		// Confirmed on issue #3 against a real IBM i capture: the inline
		// length is a character count, not a byte count (JTOpen's "maxLength
		// is in bytes" note describes the column's declared maximum, not
		// this per-value prefix) - a 43-character value's prefix read as 43
		// while the actual payload was 86 bytes (43 * 2 bytes/char for
		// UTF-16BE), which decodeUTF16BE's odd-length check caught.
		lnBytes, err := readBytes(stream, 2)
		if err != nil {
			return nil, err
		}
		ln := int(binary.BigEndian.Uint16(lnBytes))
		data, err := readBytes(stream, ln*2)
		if err != nil {
			return nil, err
		}
		return decodeUTF16BE(data)
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
		return signedInt(data, numericByteOrderFor(platform)), nil
	case drdaTypeFloat4, drdaTypeNFloat4:
		data, err := readBytes(stream, 4)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(numericByteOrderFor(platform).Uint32(data)), nil
	case drdaTypeFloat8, drdaTypeNFloat8:
		data, err := readBytes(stream, 8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(numericByteOrderFor(platform).Uint64(data)), nil
	case drdaTypeDecimal, drdaTypeNDecimal:
		precision := int(field.params[0])
		scale := int(field.params[1])
		ln := (precision + 2) / 2
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return decodePackedDecimal(data, scale)
	case drdaTypeZoned, drdaTypeNZoned:
		// IBM i reports NUMERIC columns as zoned decimal, confirmed on
		// issue #3 against a real QRYDSC/SQLDARD pair: the 2-byte length
		// field is precision-byte then scale-byte (same convention as
		// drdaTypeDecimal above), and the wire length is the precision
		// itself - one byte per digit, unlike packed decimal's two digits
		// per byte.
		precision := int(field.params[0])
		scale := int(field.params[1])
		data, err := readBytes(stream, precision)
		if err != nil {
			return nil, err
		}
		return decodeZonedDecimal(data, scale)
	case drdaTypeDate, drdaTypeNDate:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		decoded := decodeString(data, characterEncodingFor(platform))
		t, err := time.Parse("2006-01-02", decoded)
		if err != nil {
			return decoded, nil
		}
		return t, nil
	case drdaTypeTime, drdaTypeNTime:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		return normalizeDb2Time(decodeString(data, characterEncodingFor(platform))), nil
	case drdaTypeTimestamp, drdaTypeNTimestamp:
		ln := int(binary.BigEndian.Uint16(field.params))
		data, err := readBytes(stream, ln)
		if err != nil {
			return nil, err
		}
		decoded := decodeString(data, characterEncodingFor(platform))
		value := normalizeDb2Timestamp(decoded)
		t, err := time.Parse("2006-01-02-15.04.05.000000", value)
		if err != nil {
			return nil, fmt.Errorf("invalid Db2 timestamp %q: %w", strings.TrimSpace(decoded), err)
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
		drdaTypeNDecFloat, drdaTypeNLobBytes, drdaTypeNLobCSBCS, drdaTypeNZoned:
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

// numericByteOrderFor returns the byte order binary numeric fields
// (INTEGER/SMALLINT/BIGINT/FLOAT) are actually sent in. DRDA's TYPDEFNAM
// governs this: IBM i's own native one (QTDSQL400) is big-endian - this
// client assumed little-endian (QTDSQLX86/LUW's convention) unconditionally
// everywhere, confirmed wrong on issue #3 via real IBM i data (COLUMN_COUNT/
// ROW_LENGTH came back byte-reversed, verified against the same row read
// via ODBC). Packed decimal (decodePackedDecimal) is unaffected - it's a
// sequence of BCD digit nibbles in natural reading order, not a binary
// integer, so there's no "endianness" to it.
func numericByteOrderFor(platform db2Platform) binary.ByteOrder {
	if platform == db2PlatformIBMi {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func signedInt(data []byte, order binary.ByteOrder) int64 {
	switch len(data) {
	case 1:
		return int64(int8(data[0]))
	case 2:
		return int64(int16(order.Uint16(data)))
	case 4:
		return int64(int32(order.Uint32(data)))
	case 8:
		return int64(order.Uint64(data))
	default:
		if order == binary.BigEndian {
			var v int64
			for _, b := range data {
				v = (v << 8) | int64(b)
			}
			return v
		}
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

// decodeZonedDecimal decodes IBM i's wire format for NUMERIC columns,
// confirmed on issue #3 against a real capture (bytes f0 f0 f1, precision
// 3 scale 0, decodes to 1): one byte per digit (the low nibble; the high
// nibble is a zone, always 0xF, except on the last byte, whose high
// nibble is the sign instead) - a fundamentally different layout from
// drdaTypeDecimal's packed BCD (two digits per byte, sign in the low
// nibble of the last byte). The sign nibble values (0xf/0xc/0xa/0xe
// positive, 0xd/0xb negative) match the same convention decodePackedDecimal
// already uses, just in the opposite nibble position of the last byte.
func decodeZonedDecimal(data []byte, scale int) (decimal.Decimal, error) {
	if len(data) == 0 {
		return decimal.Zero, nil
	}
	digits := make([]byte, len(data))
	for i, b := range data {
		d := b & 0x0f
		if d > 9 {
			return decimal.Zero, fmt.Errorf("invalid zoned decimal digit nibble: %x", hex.EncodeToString(data))
		}
		digits[i] = '0' + d
	}
	trimmed := strings.TrimLeft(string(digits), "0")
	if trimmed == "" {
		trimmed = "0"
	}
	v, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, err
	}
	v = v.Shift(int32(-scale))
	signNibble := data[len(data)-1] >> 4
	if signNibble == 0x0d || signNibble == 0x0b {
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

// parseVarchar returns the raw bytes of a length-prefixed field wrapped in
// a Go string with no character decoding applied - callers that care what
// encoding those bytes are actually in (e.g. SQLDARD column names) decode
// them separately via decodeString, since that depends on the platform,
// not on anything this generic framing helper knows.
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
