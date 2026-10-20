package db2

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

func TestReadDRDAFieldDecodesIntegerBigEndianOnIBMi(t *testing.T) {
	// 00 00 00 11 (big-endian 17) - the real COLUMN_COUNT value from
	// issue #3, which this client previously decoded as little-endian
	// and reported as 285212672 (verified wrong against the same row
	// read via ODBC).
	value, err := readDRDAField(bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x11}), drdaField{
		typeCode: drdaTypeInteger,
		params:   lengthParams(4),
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, int64(17), value)
}

func TestReadDRDAFieldDecodesIntegerLittleEndianOnOtherPlatforms(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0x11, 0x00, 0x00, 0x00}), drdaField{
		typeCode: drdaTypeInteger,
		params:   lengthParams(4),
	}, db2PlatformLUW)

	require.NoError(t, err)
	require.Equal(t, int64(17), value, "non-IBM-i platforms keep the existing little-endian (QTDSQLX86) convention")
}

func TestReadDRDAFieldDecodesFloat8BigEndianOnIBMi(t *testing.T) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(3.5))

	value, err := readDRDAField(bytes.NewReader(buf[:]), drdaField{
		typeCode: drdaTypeFloat8,
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, 3.5, value)
}

func TestReadDRDAFieldNonNullableDecimal(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0x12, 0x34, 0x5c}), drdaField{
		typeCode: drdaTypeDecimal,
		params:   []byte{5, 2},
	}, db2PlatformAuto)

	require.NoError(t, err)
	require.Equal(t, "123.45", value.(interface{ String() string }).String())
}

// TestReadDRDAFieldDecodesZonedDecimal pins the exact byte example from
// issue #3: IBM i reports NUMERIC columns as zoned decimal (DRDA type
// 0x10), one byte per digit (low nibble), with the sign in the high
// nibble of the last byte - bytes f0 f0 f1 (precision 3, scale 0) decode
// to 1.
func TestReadDRDAFieldDecodesZonedDecimal(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0xf0, 0xf0, 0xf1}), drdaField{
		typeCode: drdaTypeZoned,
		params:   []byte{3, 0},
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "1", value.(interface{ String() string }).String())
}

// TestReadDRDAFieldDecodesNegativeZonedDecimal exercises the sign
// convention issue #3 documented (0xd/0xb negative) but didn't have a
// real captured negative value for - the digit bytes are the same
// pattern as the confirmed positive case above, only the last byte's
// sign nibble differs.
func TestReadDRDAFieldDecodesNegativeZonedDecimal(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0xf0, 0xf0, 0xd1}), drdaField{
		typeCode: drdaTypeZoned,
		params:   []byte{3, 0},
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "-1", value.(interface{ String() string }).String())
}

// TestReadDRDAFieldDecodesZonedDecimalWithScale confirms the scale byte
// (field.params[1]) is applied the same way as drdaTypeDecimal's.
func TestReadDRDAFieldDecodesZonedDecimalWithScale(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0xf1, 0xf2, 0xf3, 0xf4, 0xf5}), drdaField{
		typeCode: drdaTypeZoned,
		params:   []byte{5, 2},
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "123.45", value.(interface{ String() string }).String())
}

func TestReadDRDAFieldTimestampTruncatesToMicroseconds(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("2024-01-02-03.04.05.123456789012")), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len("2024-01-02-03.04.05.123456789012")),
	}, db2PlatformAuto)

	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC), value)
}

func TestReadDRDAFieldTimestampPadsToMicroseconds(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("2024-01-02-03.04.05.1")), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len("2024-01-02-03.04.05.1")),
	}, db2PlatformAuto)

	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 1, 2, 3, 4, 5, 100000000, time.UTC), value)
}

func TestReadDRDAFieldTimestampReturnsErrorForMalformedValue(t *testing.T) {
	raw := "not-a-timestamp"
	value, err := readDRDAField(bytes.NewReader([]byte(raw)), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len(raw)),
	}, db2PlatformAuto)

	require.ErrorContains(t, err, "invalid Db2 timestamp")
	require.Nil(t, value)
}

func TestReadDRDAFieldDecodesTimestampAsCP37OnIBMi(t *testing.T) {
	// The exact bytes from a real IBM i host's QRYDTA reply on issue #3 -
	// cp37-encoded "2026-09-11-11.39.16.546000", sent raw with no
	// conversion to CCSID 1208/UTF-8 despite TYPDEFOVR asking for it (the
	// same server behavior v30 fixed for CHAR/VARCHAR, but the datetime
	// path still read it as if it were already UTF-8).
	raw := []byte{
		0xf2, 0xf0, 0xf2, 0xf6, 0x60, 0xf0, 0xf9, 0x60, 0xf1, 0xf1, 0x60,
		0xf1, 0xf1, 0x4b, 0xf3, 0xf9, 0x4b, 0xf1, 0xf6, 0x4b, 0xf5, 0xf4,
		0xf6, 0xf0, 0xf0, 0xf0,
	}

	value, err := readDRDAField(bytes.NewReader(raw), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len(raw)),
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 9, 11, 11, 39, 16, 546000000, time.UTC), value)
}

func TestReadDRDAFieldTimeNormalizesSeparators(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("03.04.05")), drdaField{
		typeCode: drdaTypeTime,
		params:   lengthParams(len("03.04.05")),
	}, db2PlatformAuto)

	require.NoError(t, err)
	require.Equal(t, "03:04:05", value)
}

func TestReadDRDAFieldTimePreservesFraction(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("03.04.05.1")), drdaField{
		typeCode: drdaTypeTime,
		params:   lengthParams(len("03.04.05.1")),
	}, db2PlatformAuto)

	require.NoError(t, err)
	require.Equal(t, "03:04:05.100000", value)
}

func TestResolveSecMec(t *testing.T) {
	cases := []struct {
		name   string
		secmec int
		sectkn []byte
		want   int
	}{
		{"explicit encrypted with valid token passes through", secmecEncryptedUserPassword, make([]byte, 32), secmecEncryptedUserPassword},
		{"explicit plaintext passes through", secmecUserPassword, nil, secmecUserPassword},
		{"omitted with valid token defaults to encrypted", 0, make([]byte, 32), secmecEncryptedUserPassword},
		{"omitted with no token falls back to plaintext", 0, nil, secmecUserPassword},
		{"claimed encrypted but no token falls back to plaintext", secmecEncryptedUserPassword, nil, secmecUserPassword},
		{"claimed encrypted but wrong-length token falls back to plaintext", secmecEncryptedUserPassword, []byte("short-token"), secmecUserPassword},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveSecMec(tc.secmec, tc.sectkn))
		})
	}
}

func TestPackSECCHKReturnsEncryptionError(t *testing.T) {
	client := &db2Client{
		database: "TESTDB",
		user:     "db2inst1",
		password: "password",
		private:  big.NewInt(2),
	}

	_, err := client.packSECCHK(secmecEncryptedUserPassword, []byte("short-token"), nil)

	require.ErrorContains(t, err, "credential encryption failed for user")
	require.ErrorContains(t, err, "expected 32-byte security token")
}

func TestPackSECCHKPadsUSRIDToTenBytesForSubstitutePasswordAuth(t *testing.T) {
	client := &db2Client{database: padDatabaseName("SIGLPAR"), user: "user", password: "pass"}

	packet, err := client.packSECCHK(secmecEUSRIDPWD, nil, []byte("01234567890123456789"))
	require.NoError(t, err)

	fields := parseReplyObject(packet[4:])
	require.Equal(t, []byte("USER      "), cp500ToASCIIForTest(t, fields[cpUSRID]),
		"jt400's DDMSECCHKRequestDataStream always sends exactly 10 bytes for USRID, uppercase and blank-padded")
}

func TestPackSECCHKPadsUSRIDForPlainTextAuthToo(t *testing.T) {
	client := &db2Client{database: padDatabaseName("SIGLPAR"), user: "ab", password: "pass"}

	packet, err := client.packSECCHK(secmecUserPassword, nil, nil)
	require.NoError(t, err)

	fields := parseReplyObject(packet[4:])
	require.Equal(t, []byte("AB        "), cp500ToASCIIForTest(t, fields[cpUSRID]))
}

func cp500ToASCIIForTest(t *testing.T, cp500Bytes []byte) []byte {
	t.Helper()
	decoded := decodeString(cp500Bytes, "cp500")
	return []byte(decoded)
}

func TestParseSECCHKRMReturnsErrorForNonZeroSVRCOD(t *testing.T) {
	// SVRCOD=16 (SEVERE ERROR), SECCHKCD=15 - the exact bytes a real IBM i
	// host returns on issue #3 for every connection attempted so far, which
	// every prior build silently ignored and treated as "authenticated".
	object := append(packUint(cpSVRCOD, 16, 2), packUint(cpSECCHKCD, 15, 1)...)

	err := parseSECCHKRM(object)

	require.ErrorContains(t, err, "SVRCOD=16")
	require.ErrorContains(t, err, "SECCHKCD=15")
}

func TestParseSECCHKRMReturnsNilForZeroSVRCOD(t *testing.T) {
	object := packUint(cpSVRCOD, 0, 2)

	require.NoError(t, parseSECCHKRM(object))
}

func TestParseSECCHKRMReturnsNilWhenSVRCODAbsent(t *testing.T) {
	require.NoError(t, parseSECCHKRM(nil))
}

// packSECMECMenu builds a single SECMEC field (LL+CP+data) carrying
// multiple 2-byte values concatenated - the documented DRDA shape for an
// ACCSECRD reply when the proposed mechanism isn't supported: the server
// lists what it does support instead of confirming one value.
func packSECMECMenu(values ...int) []byte {
	data := make([]byte, 0, len(values)*2)
	for _, v := range values {
		data = append(data, byte(v>>8), byte(v))
	}
	field := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(field[0:2], uint16(len(field)))
	binary.BigEndian.PutUint16(field[2:4], cpSECMEC)
	copy(field[4:], data)
	return field
}

func TestHandshakeChoosesSECMEC8FromMenuReply(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	drdaServerSeed := []byte{9, 10, 11, 12, 13, 14, 15, 16}
	client := &db2Client{
		conn:           clientConn,
		database:       padDatabaseName("SIGLPAR"),
		user:           "user",
		password:       "pass",
		timeout:        time.Second,
		private:        private,
		signonExchange: stubSignonExchange(3),
	}

	var dedicatedACCSECSent bool
	var secchkObject []byte
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		if _, err := readDSS(serverConn); err != nil { // EXCSAT
			return
		}
		if _, err := readDSS(serverConn); err != nil { // ACCSEC
			return
		}
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // re-proposed ACCSEC (secmec=3)
			return
		}
		// A menu reply listing 7, 9, 10, 8 in that order - NOT starting
		// with 8. The old bug (reading only the first 2 bytes of this
		// field) would resolve this to 7, an unsupported mechanism this
		// client has no credential path for, and skip the SECMEC 8 flow
		// entirely.
		menu := packSECMECMenu(7, secmecEncryptedUserPassword, 10, secmecEUSRIDPWD)
		if writeTestDSS(serverConn, cpACCSECRD, menu, false) != nil {
			return
		}

		dedicated, err := readDSS(serverConn) // dedicated ACCSEC for secmec=8
		if err != nil {
			return
		}
		if binary.BigEndian.Uint16(parseReplyObject(dedicated.object)[cpSECMEC]) == secmecEUSRIDPWD {
			dedicatedACCSECSent = true
		}
		accsecrd := append(packUint(cpSECMEC, secmecEUSRIDPWD, 2), packBinary(cpSECTKN, drdaServerSeed)...)
		if writeTestDSS(serverConn, cpACCSECRD, accsecrd, false) != nil {
			return
		}

		packet, err := readDSS(serverConn) // SECCHK
		if err != nil {
			return
		}
		secchkObject = packet.object
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	_ = serverConn.Close()
	<-done

	require.True(t, dedicatedACCSECSent, "client must pick SECMEC 8 out of the full menu, not just its first listed value")
	fields := parseReplyObject(secchkObject)
	require.NotEqual(t, []byte("pass"), fields[cpPASSWORD], "must not fall back to plain-text credentials when 8 is buried in the menu")
	require.Len(t, fields[cpPASSWORD], 20, "must send a real SHA-1 substitute, not a plain-text password")
}

func TestHandshakeSendsSubstituteCredentialForSECMEC8(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	drdaServerSeed := []byte{9, 10, 11, 12, 13, 14, 15, 16}
	client := &db2Client{
		conn:           clientConn,
		database:       padDatabaseName("SIGLPAR"),
		user:           "user",
		password:       "pass",
		timeout:        time.Second,
		private:        private,
		signonExchange: stubSignonExchange(3),
	}

	var capturedClientSeed []byte
	var secchkObject []byte
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		if _, err := readDSS(serverConn); err != nil { // EXCSAT
			return
		}
		if _, err := readDSS(serverConn); err != nil { // ACCSEC
			return
		}
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // re-proposed ACCSEC (secmec=3)
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEUSRIDPWD, 2), false) != nil {
			return
		}

		// Dedicated ACCSEC/ACCSECRD round for SECMEC 8's own seed pair
		// (not the Sign-on Server's) - confirmed necessary on issue #3.
		dedicatedACCSEC, err := readDSS(serverConn)
		if err != nil {
			return
		}
		capturedClientSeed = parseReplyObject(dedicatedACCSEC.object)[cpSECTKN]
		accsecrd := append(packUint(cpSECMEC, secmecEUSRIDPWD, 2), packBinary(cpSECTKN, drdaServerSeed)...)
		if writeTestDSS(serverConn, cpACCSECRD, accsecrd, false) != nil {
			return
		}

		packet, err := readDSS(serverConn) // SECCHK
		if err != nil {
			return
		}
		secchkObject = packet.object
		// Closing without a reply is fine - this test only cares about
		// what the client sent, not the rest of the handshake.
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	_ = serverConn.Close()
	<-done

	require.Len(t, capturedClientSeed, 8, "ACCSEC re-proposing SECMEC 8 must carry an 8-byte client seed in SECTKN")
	fields := parseReplyObject(secchkObject)
	wantCredential := substitutePasswordSHA1("USER", "pass", drdaServerSeed, capturedClientSeed)
	require.Equal(t, wantCredential, fields[cpPASSWORD], "PASSWORD field must carry the SECMEC 8 substitute computed from the DRDA ACCSEC/ACCSECRD seed pair, not the plain-text password or the Sign-on Server's seeds")
	require.NotEqual(t, []byte("pass"), fields[cpPASSWORD])
}

func TestHandshakeFailsWhenSECCHKRMReportsAuthenticationFailure(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	client := &db2Client{
		conn:           clientConn,
		database:       padDatabaseName("SIGLPAR"),
		user:           "user",
		password:       "pass",
		timeout:        time.Second,
		private:        private,
		platform:       db2PlatformIBMi,
		signonExchange: stubSignonExchange(3),
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		if _, err := readDSS(serverConn); err != nil { // EXCSAT
			return
		}
		if _, err := readDSS(serverConn); err != nil { // ACCSEC
			return
		}
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // re-proposed ACCSEC (secmec=3)
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEUSRIDPWD, 2), false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // dedicated ACCSEC for SECMEC 8's seed pair
			return
		}
		accsecrd := append(packUint(cpSECMEC, secmecEUSRIDPWD, 2), packBinary(cpSECTKN, []byte{1, 2, 3, 4, 5, 6, 7, 8})...)
		if writeTestDSS(serverConn, cpACCSECRD, accsecrd, false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // SECCHK
			return
		}
		// SVRCOD=16, SECCHKCD=15 - the real server's response, reproduced
		// byte-for-byte from the issue #3 packet capture.
		object := append(packUint(cpSVRCOD, 16, 2), packUint(cpSECCHKCD, 15, 1)...)
		_ = writeTestDSS(serverConn, cpSECCHKRM, object, false)
		// No ACCRDB should ever be sent - the handshake must fail here.
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}

	select {
	case err := <-done:
		require.ErrorContains(t, err, "SVRCOD=16")
		require.ErrorContains(t, err, "SECCHKCD=15")
	case <-time.After(time.Second):
		t.Fatal("handshake did not return in time")
	}
}

func TestSubstitutePasswordSHA1MatchesIndependentVector(t *testing.T) {
	// Computed independently in Python (hashlib.sha1), not derived from
	// this Go implementation, so this catches transcription bugs in the
	// port rather than just re-confirming whatever the Go code already
	// does. See AS400ImplRemote.generateShaToken/generateShaSubstitute.
	serverSeed := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	clientSeed := []byte{9, 10, 11, 12, 13, 14, 15, 16}

	got := substitutePasswordSHA1("TESTUSER", "Secret123", serverSeed, clientSeed)

	require.Equal(t, "2de5412589a9505bc92a57e38ec409dabf368cdf", hex.EncodeToString(got))
}

func TestSubstitutePasswordSHA1PadsAndUppercasesUserID(t *testing.T) {
	serverSeed := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	clientSeed := []byte{9, 10, 11, 12, 13, 14, 15, 16}

	short := substitutePasswordSHA1("ABC", "Secret123", serverSeed, clientSeed)
	padded := substitutePasswordSHA1("ABC       ", "Secret123", serverSeed, clientSeed)

	require.Equal(t, padded, short, "user ID must be blank-padded to 10 chars before hashing")
}

func TestSubstitutePasswordSHA512Is64Bytes(t *testing.T) {
	serverSeed := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	clientSeed := []byte{9, 10, 11, 12, 13, 14, 15, 16}

	got := substitutePasswordSHA512("TESTUSER", "Secret123", serverSeed, clientSeed)

	require.Len(t, got, 64)
}

func TestCP500EncodingUsesCodePage500(t *testing.T) {
	encoded, err := encodeString("[]{}@#$", "cp500")

	require.NoError(t, err)
	require.Equal(t, []byte{0x4a, 0x5a, 0xc0, 0xd0, 0x7c, 0x7b, 0x5b}, encoded)
	require.Equal(t, "[]{}@#$", decodeString(encoded, "cp500"))
}

func TestCP37EncodingDiffersFromCP500AtTheSevenKnownPositions(t *testing.T) {
	// Values from IBM's own CCSID 37 vs 500 comparison, not derived from
	// this package's own tables, so this catches a transcription bug in
	// the port rather than just re-confirming whatever the code already
	// does. Letters and digits are identical on both pages; these 7 byte
	// positions are the only ones that differ.
	cases := []struct {
		b        byte
		cp37Char string
		cp500Str string
	}{
		{0x4A, "¢", "["},
		{0x4F, "|", "!"},
		{0x5A, "!", "]"},
		{0x5F, "¬", "^"},
		{0xB0, "^", "¢"},
		{0xBA, "[", "¬"},
		{0xBB, "]", "|"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.cp37Char, decodeCP37([]byte{tc.b}), "cp37 byte 0x%x", tc.b)
		require.Equal(t, tc.cp500Str, decodeCP500([]byte{tc.b}), "cp500 byte 0x%x", tc.b)
	}
}

func TestCP37EncodingRoundTripsBracketsAndPipe(t *testing.T) {
	encoded, err := encodeString("COL[1]|TEST!X^Y", "cp37")
	require.NoError(t, err)

	require.Equal(t, "COL[1]|TEST!X^Y", decodeString(encoded, "cp37"))
}

func TestReadDRDAFieldDecodesVarcharAsCP37OnIBMi(t *testing.T) {
	raw, err := encodeCP37("COL[1]")
	require.NoError(t, err)
	lnBytes := []byte{byte(len(raw) >> 8), byte(len(raw))}

	value, err := readDRDAField(bytes.NewReader(append(lnBytes, raw...)), drdaField{
		typeCode: drdaTypeVarchar,
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "COL[1]", value)
}

func TestReadDRDAFieldDecodesVarcharAsUTF8OnOtherPlatforms(t *testing.T) {
	raw := []byte("COL[1]")
	lnBytes := []byte{byte(len(raw) >> 8), byte(len(raw))}

	value, err := readDRDAField(bytes.NewReader(append(lnBytes, raw...)), drdaField{
		typeCode: drdaTypeVarchar,
	}, db2PlatformLUW)

	require.NoError(t, err)
	require.Equal(t, "COL[1]", value, "non-IBM-i platforms keep the existing UTF-8 passthrough")
}

func TestReadDRDAFieldDecodesFixedGraphicAsUTF16BE(t *testing.T) {
	raw := encodeUTF16BEForTest("Hi")

	value, err := readDRDAField(bytes.NewReader(raw), drdaField{
		typeCode: drdaTypeGraphic,
		params:   lengthParams(len(raw)),
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "Hi", value)
}

func TestReadDRDAFieldDecodesVarGraphicAsUTF16BE(t *testing.T) {
	raw := encodeUTF16BEForTest("TABLE_TEXT")
	charCount := len(raw) / 2
	lnBytes := []byte{byte(charCount >> 8), byte(charCount)}

	value, err := readDRDAField(bytes.NewReader(append(lnBytes, raw...)), drdaField{
		typeCode: drdaTypeVarGraph,
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "TABLE_TEXT", value)
}

// TestReadDRDAFieldDecodesVarGraphicLengthPrefixAsCharacterCount pins the
// exact case reported on issue #3: a 43-character VARGRAPHIC value whose
// inline length prefix is 43 (a character count), not 86 (the actual byte
// count of the UTF-16BE payload). Reading it as a byte count truncated the
// payload and tripped decodeUTF16BE's odd-length check.
func TestReadDRDAFieldDecodesVarGraphicLengthPrefixAsCharacterCount(t *testing.T) {
	raw := encodeUTF16BEForTest(strings.Repeat("x", 43))
	require.Len(t, raw, 86)

	value, err := readDRDAField(bytes.NewReader(append([]byte{0x00, 0x2b}, raw...)), drdaField{
		typeCode: drdaTypeVarGraph,
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, strings.Repeat("x", 43), value)
}

func TestReadDRDAFieldRejectsOddLengthGraphicData(t *testing.T) {
	_, err := readDRDAField(bytes.NewReader([]byte{0x00, 0x48, 0x00}), drdaField{
		typeCode: drdaTypeGraphic,
		params:   lengthParams(3),
	}, db2PlatformIBMi)

	require.ErrorContains(t, err, "odd-length UTF-16BE")
}

func encodeUTF16BEForTest(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

func TestPackPKGNAMCSNUsesCatalogBytes(t *testing.T) {
	client := &db2Client{database: padDatabaseName("[")}

	packet, err := client.packPKGNAMCSN(db2PackageSectionQuery)

	require.NoError(t, err)
	// PKGNAMCSN is text, same as every other DRDA character field - it
	// travels cp500-encoded, not raw ASCII (a real IBM i host rejects the
	// prepare outright if it isn't).
	require.Equal(t, "[", decodeString(packet[4:5], "cp500"))
	require.Equal(t, db2PackageToken, decodeString(packet[4+18+18+18:4+18+18+18+8], "cp500"))
	require.Equal(t, []byte{0x00, db2PackageSectionQuery}, packet[len(packet)-2:])
}

func TestPackACCRDBUsesQTDSQL400OnIBMi(t *testing.T) {
	packet := packACCRDB(padDatabaseName("RDBNAME2"), db2PlatformIBMi, []byte("12345678"))

	fields := parseReplyObject(packet[4:])
	typdefnam := decodeString(fields[cpTYPDEFNAM], "cp500")
	require.Equal(t, "QTDSQL400", typdefnam,
		"QTDSQLX86 is an Intel/x86-oriented type definition; DRDA has a dedicated AS/400 value that IBM i's own server expects")
}

func TestPackACCRDBUsesQTDSQLX86OnOtherPlatforms(t *testing.T) {
	packet := packACCRDB(padDatabaseName("SAMPLE"), db2PlatformLUW, []byte("12345678"))

	fields := parseReplyObject(packet[4:])
	typdefnam := decodeString(fields[cpTYPDEFNAM], "cp500")
	require.Equal(t, "QTDSQLX86", typdefnam,
		"non-IBM-i platforms keep the existing typedef - no evidence they need QTDSQL400")
}

func TestPackACCRDBUsesQSQPRDIDOnIBMi(t *testing.T) {
	packet := packACCRDB(padDatabaseName("RDBNAME2"), db2PlatformIBMi, []byte("12345678"))

	fields := parseReplyObject(packet[4:])
	prdid := decodeString(fields[cpPRDID], "cp500")
	require.Equal(t, "QSQ08060", prdid,
		"SQL11014 is the LUW/CLI product family - IBM i's documented own family prefix is QSQ, and every other ACCRDB field has been ruled out already")
}

func TestPackACCRDBUsesSQLPRDIDOnOtherPlatforms(t *testing.T) {
	packet := packACCRDB(padDatabaseName("SAMPLE"), db2PlatformLUW, []byte("12345678"))

	fields := parseReplyObject(packet[4:])
	prdid := decodeString(fields[cpPRDID], "cp500")
	require.Equal(t, "SQL11014", prdid,
		"non-IBM-i platforms keep the existing PRDID - no evidence they need the QSQ family")
}

func TestPackPKGNAMCSNUsesQSQCLIPKGCOnIBMi(t *testing.T) {
	client := &db2Client{database: padDatabaseName("SIGLPAR"), platform: db2PlatformIBMi}

	packet, err := client.packPKGNAMCSN(db2PackageSectionQuery)

	require.NoError(t, err)
	collection := decodeString(packet[4+18:4+18+18], "cp500")
	pkgName := decodeString(packet[4+18+18:4+18+18+18], "cp500")
	require.Equal(t, "QGPL", strings.TrimRight(collection, " "),
		"NULLID isn't a real package collection on IBM i (confirmed empty via QSYS2.SYSPACKAGE) - QGPL is what IBM's own ODBC driver defaults to there")
	require.Equal(t, "QSQCLIPKGC", strings.TrimRight(pkgName, " "),
		"SYSSH200 doesn't exist under QGPL either - QSQCLIPKGC is IBM i's real pre-bound system CLI package for cursor operations, confirmed via QSYS2.SYSPACKAGE")
	token := packet[4+18+18+18 : 4+18+18+18+8]
	require.Equal(t, []byte{0xd8, 0xe2, 0xd8, 0xc3, 0xd3, 0xc9, 0x40, 0x40}, token,
		"must match QSYS2.SYSPACKAGE's real CONSISTENCY_TOKEN for QGPL/QSQCLIPKGC (hex D8E2D8C3D3C94040) - a pre-bound package rejects any other token")
	section := packet[len(packet)-2:]
	require.Equal(t, []byte{0x00, 0x01}, section,
		"db2PackageSectionQuery (64) should be remapped to 1 on IBM i - 64 still got EOF on PRPSQLSTT even with the right package/token")
}

func TestPackPKGNAMCSNUsesResolvedRDBNameOnIBMi(t *testing.T) {
	client := &db2Client{
		database: padDatabaseName("*LOCAL"),
		rdbName:  "RDBNAME2",
		platform: db2PlatformIBMi,
	}

	packet, err := client.packPKGNAMCSN(db2PackageSectionQuery)

	require.NoError(t, err)
	database := decodeString(packet[4:4+18], "cp500")
	require.Equal(t, "RDBNAME2", strings.TrimRight(database, " "),
		"*LOCAL is a symbolic alias ACCSEC/ACCRDB resolve themselves - PKGNAMCSN needs the real RDB directory name (confirmed via QSYS2.SYSPACKAGE's PACKAGE_CATALOG column) or the server can't find the package")
}

func TestPackPKGNAMCSNFallsBackToDatabaseWhenRDBNameUnresolved(t *testing.T) {
	client := &db2Client{database: padDatabaseName("*LOCAL"), platform: db2PlatformIBMi}

	packet, err := client.packPKGNAMCSN(db2PackageSectionQuery)

	require.NoError(t, err)
	database := decodeString(packet[4:4+18], "cp500")
	require.Equal(t, "*LOCAL", strings.TrimRight(database, " "),
		"without a resolved RDB name, falling back to the raw database value keeps today's (known-broken-on-alias) behavior instead of crashing")
}

func TestPackPKGNAMCSNKeepsNonQuerySectionsUnchangedOnIBMi(t *testing.T) {
	client := &db2Client{database: padDatabaseName("SIGLPAR"), platform: db2PlatformIBMi}

	packet, err := client.packPKGNAMCSN(db2PackageSectionSet)

	require.NoError(t, err)
	section := packet[len(packet)-2:]
	require.Equal(t, []byte{0x00, byte(db2PackageSectionSet)}, section,
		"only the query section (64) is remapped on IBM i - other section numbers should pass through untouched")
}

// buildSQLCard builds a minimal valid SQLCARD object: group flag 0,
// little-endian SQLCODE, blank-padded SQLSTATE, 44 bytes of filler up to
// the fixed 54-byte header parseSQLCard requires, then an empty varchar,
// the message (cp500-encoded), and an empty secondary message varchar.
func buildSQLCard(t *testing.T, sqlCode int32, state, message string) []byte {
	t.Helper()
	buf := make([]byte, 54)
	buf[0] = 0x00
	binary.LittleEndian.PutUint32(buf[1:5], uint32(sqlCode))
	copy(buf[5:10], []byte(fmt.Sprintf("%-5s", state)))

	msgBytes, err := encodeString(message, "cp500")
	require.NoError(t, err)

	buf = append(buf, 0x00, 0x00) // empty leading varchar
	buf = append(buf, byte(len(msgBytes)>>8), byte(len(msgBytes)))
	buf = append(buf, msgBytes...)
	buf = append(buf, 0x00, 0x00) // empty secondary message varchar
	return buf
}

func TestParseResponseSQLCARDReplacesBareSQLERRRMDiagnostic(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{conn: clientConn, timeout: time.Second}

	done := make(chan error, 1)
	go func() {
		done <- client.parseResponse(context.Background(), db2StreamHandler{})
	}()

	// SRVDGN carries a bare diagnostic code with no further structure -
	// confirmed on issue #3 ("preparing statement: 23765", no SQLCODE/
	// SQLSTATE/message at all). A SQLCARD arriving in the same chain has
	// real structure and should win.
	srvdgn, err := encodeString("23765", "cp500")
	require.NoError(t, err)
	require.NoError(t, writeTestDSS(serverConn, cpSQLERRRM, packBinary(cpSRVDGN, srvdgn), true))
	require.NoError(t, writeTestDSS(serverConn, cpSQLCARD, buildSQLCard(t, -204, "42704", "real diagnostic"), false))

	select {
	case err := <-done:
		require.ErrorContains(t, err, "real diagnostic")
		require.ErrorContains(t, err, "SQLCODE=-204")
		require.NotContains(t, err.Error(), "23765")
	case <-time.After(time.Second):
		t.Fatal("parseResponse did not return in time")
	}
}

func TestParseResponseStreamsRowsBeforeResponseEnds(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:    clientConn,
		timeout: time.Second,
	}

	rowsCh := make(chan [][]any, 1)
	done := make(chan error, 1)

	go func() {
		done <- client.parseResponse(context.Background(), db2StreamHandler{
			Rows: func(rows [][]any) error {
				rowsCh <- rows
				return nil
			},
		})
	}()

	require.NoError(t, writeTestDSS(serverConn, cpQRYDSC, []byte{0x06, 0x76, 0xd0, drdaTypeInteger, 0x00, 0x04}, true))
	require.NoError(t, writeTestDSS(serverConn, cpQRYDTA, []byte{0xff, 0x00, 0x2a, 0x00, 0x00, 0x00}, true))

	select {
	case rows := <-rowsCh:
		require.Len(t, rows, 1)
		require.Equal(t, int64(42), rows[0][0])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected rows to be streamed before terminal response packet")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("parseResponse returned before terminal response packet")
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, writeTestDSS(serverConn, cpENDQRYRM, nil, false))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected parser to finish after terminal response packet")
	}
}

func TestHandshakeRenegotiatesACCSECWhenDowngrading(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	client := &db2Client{
		conn:     clientConn,
		database: padDatabaseName("SIGLPAR"),
		user:     "user",
		password: "pass",
		timeout:  time.Second,
		private:  private,
	}

	var codePoints []uint16
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		// EXCSAT, then ACCSEC (two separate on-wire DSS packets).
		p, err := readDSS(serverConn)
		if err != nil {
			return
		}
		codePoints = append(codePoints, p.codePoint)
		p, err = readDSS(serverConn)
		if err != nil {
			return
		}
		codePoints = append(codePoints, p.codePoint)

		// Reply exactly like DBHPROD2: claims SECMEC 9, no token.
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		// The client should re-propose ACCSEC with the downgraded
		// mechanism next, not jump straight to SECCHK.
		p, err = readDSS(serverConn)
		if err != nil {
			return
		}
		codePoints = append(codePoints, p.codePoint)
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	_ = serverConn.Close()

	select {
	case err := <-done:
		require.Error(t, err, "handshake should fail once the test closes the connection")
	case <-time.After(time.Second):
		t.Fatal("handshake did not return after connection closed")
	}

	require.Equal(t, []uint16{cpEXCSAT, cpACCSEC, cpACCSEC}, codePoints)
}

func TestHandshakeDetectsIBMiFromSecmecDowngradeWhenPRDIDMissing(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	client := &db2Client{
		conn:           clientConn,
		database:       padDatabaseName("SIGLPAR"),
		user:           "user",
		password:       "pass",
		timeout:        time.Second,
		private:        private,
		signonExchange: stubSignonExchange(3),
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		// EXCSAT, then ACCSEC.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		// Claims SECMEC 9, no token - same quirk as DBHPROD2.
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		// Re-proposed ACCSEC; confirm with the undocumented SECMEC 8.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEUSRIDPWD, 2), false) != nil {
			return
		}

		// Dedicated ACCSEC/ACCSECRD round for SECMEC 8's own seed pair.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		accsecrd := append(packUint(cpSECMEC, secmecEUSRIDPWD, 2), packBinary(cpSECTKN, []byte{1, 2, 3, 4, 5, 6, 7, 8})...)
		if writeTestDSS(serverConn, cpACCSECRD, accsecrd, false) != nil {
			return
		}

		// SECCHK and ACCRDB are now two separate round trips.
		if _, err := readDSS(serverConn); err != nil { // SECCHK
			return
		}
		if writeTestDSS(serverConn, cpSECCHKRM, nil, false) != nil {
			return
		}
		if _, err := readDSS(serverConn); err != nil { // ACCRDB
			return
		}
		// ACCRDBRM with no PRDID at all - the real server's quirk reported
		// on issue #3: PRDID-based detection has nothing to match, so the
		// SECMEC downgrade pattern is the only signal left.
		_ = writeTestDSS(serverConn, cpACCRDBRM, nil, false)
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("handshake did not return in time")
	}

	require.Equal(t, db2PlatformIBMi, client.Platform(), "SECMEC 9->8 downgrade should be detected as IBM i when PRDID can't resolve it")
}

func TestHandshakeFailsLoudlyWhenACCRDBRMNeverArrives(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	client := &db2Client{
		conn:           clientConn,
		database:       padDatabaseName("SIGLPAR"),
		user:           "user",
		password:       "pass",
		timeout:        time.Second,
		private:        private,
		platform:       db2PlatformIBMi,
		signonExchange: stubSignonExchange(3),
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		// EXCSAT, then ACCSEC.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		// Re-proposed ACCSEC; confirm with the undocumented SECMEC 8.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEUSRIDPWD, 2), false) != nil {
			return
		}

		// Dedicated ACCSEC/ACCSECRD round for SECMEC 8's own seed pair.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		accsecrd := append(packUint(cpSECMEC, secmecEUSRIDPWD, 2), packBinary(cpSECTKN, []byte{1, 2, 3, 4, 5, 6, 7, 8})...)
		if writeTestDSS(serverConn, cpACCSECRD, accsecrd, false) != nil {
			return
		}

		// SECCHK and ACCRDB are two separate round trips. The server
		// replies to SECCHK with SECCHKRM, then reads ACCRDB and sends
		// nothing at all - reproducing the real packet capture from
		// issue #3 (no ACCRDBRM, no error). Closing the pipe afterwards
		// simulates that silence surfacing as an EOF to the client.
		if _, err := readDSS(serverConn); err != nil { // SECCHK
			return
		}
		if writeTestDSS(serverConn, cpSECCHKRM, nil, false) != nil {
			return
		}
		if _, err := readDSS(serverConn); err != nil { // ACCRDB
			return
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	// The server script reads ACCRDB and sends nothing back - closing the
	// pipe here simulates that silence surfacing as an EOF, same as a
	// real connection that just never responds eventually would.
	_ = serverConn.Close()

	select {
	case err := <-done:
		require.Error(t, err, "splitting SECCHK/ACCRDB into separate round trips means a missing ACCRDBRM now surfaces as a real error instead of a silent success")
		require.False(t, client.sawACCRDBRM)
	case <-time.After(time.Second):
		t.Fatal("handshake did not return after connection closed")
	}

	require.False(t, client.sawACCRDBRM, "no ACCRDBRM was ever sent by the fake server, so the flag must reflect that")
}

func TestNewCorrelationTokenIsRandomPerCall(t *testing.T) {
	a, err := newCorrelationToken()
	require.NoError(t, err)
	b, err := newCorrelationToken()
	require.NoError(t, err)

	require.Len(t, a, 8)
	require.Len(t, b, 8)
	require.NotEqual(t, a, b, "CRRTKN must correlate one specific unit of work - reusing the same bytes across connections is exactly the bug issue #3's capture pointed at")
}

func TestHandshakeSendsRawDatabaseAliasInACCRDB(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	private, err := newPrivateKey()
	require.NoError(t, err)

	client := &db2Client{
		conn:           clientConn,
		database:       padDatabaseName("*LOCAL"),
		rdbName:        "RDBNAME2",
		platform:       db2PlatformIBMi,
		user:           "user",
		password:       "pass",
		timeout:        time.Second,
		private:        private,
		signonExchange: stubSignonExchange(3),
	}

	var accrdbRDBNAM string
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		if _, err := readDSS(serverConn); err != nil { // EXCSAT
			return
		}
		if _, err := readDSS(serverConn); err != nil { // ACCSEC
			return
		}
		if writeTestDSS(serverConn, cpEXCSATRD, nil, true) != nil {
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEncryptedUserPassword, 2), false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // re-proposed ACCSEC
			return
		}
		if writeTestDSS(serverConn, cpACCSECRD, packUint(cpSECMEC, secmecEUSRIDPWD, 2), false) != nil {
			return
		}

		// Dedicated ACCSEC/ACCSECRD round for SECMEC 8's own seed pair.
		if _, err := readDSS(serverConn); err != nil {
			return
		}
		accsecrd := append(packUint(cpSECMEC, secmecEUSRIDPWD, 2), packBinary(cpSECTKN, []byte{1, 2, 3, 4, 5, 6, 7, 8})...)
		if writeTestDSS(serverConn, cpACCSECRD, accsecrd, false) != nil {
			return
		}

		if _, err := readDSS(serverConn); err != nil { // SECCHK
			return
		}
		if writeTestDSS(serverConn, cpSECCHKRM, nil, false) != nil {
			return
		}
		accrdb, err := readDSS(serverConn)
		if err != nil {
			return
		}
		fields := parseReplyObject(accrdb.object)
		accrdbRDBNAM = strings.TrimSpace(decodeString(fields[cpRDBNAM], "cp500"))
		_ = writeTestDSS(serverConn, cpACCRDBRM, nil, false)
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.handshake(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("handshake did not return in time")
	}

	require.Equal(t, "*LOCAL", accrdbRDBNAM,
		"ACCRDB's own RDBNAM reverted to the raw alias - using the resolved name (v22) still never got ACCRDBRM back in two separate captures, while ACCSEC/SECCHK consistently succeed sending *LOCAL as-is on this server")
}

// minimalSQLDARD builds the shortest SQLDARD object parseSQLDARD accepts:
// the 0xff shortcut byte (skips parseSQLCard's normal 54-byte-minimum
// structure entirely), a non-zero marker byte, a 1-column count, and that
// one column's minimum-length unnamed-column block. Column name and type
// content don't matter for tests only checking whether handler.Columns
// fires at all.
// sqlDARDHeader builds the 24-byte SQLDHGRP header + 4-byte SQLNUMROWS
// confirmed on issue #3 against five real captures (1/4/5/32/72 columns).
func sqlDARDHeader(numRows uint32) []byte {
	obj := []byte{0xff} // SQLCAGRP: null
	obj = append(obj,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 10 bytes
		0x55,       // constant
		0x00, 0x02, // 2 bytes (varies across captures)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 10 bytes
	) // SQLDHGRP: present, 24 bytes total including the leading null indicator
	n := make([]byte, 4)
	binary.BigEndian.PutUint32(n, numRows)
	return append(obj, n...)
}

// sqlDARDColumn builds one column descriptor using the layout confirmed on
// issue #3 by offline-parsing two full real captures (4 and 72 columns)
// against it and consuming every byte with nothing left over, across 76
// columns spanning VARCHAR/CHAR/DECIMAL/NUMERIC/INTEGER: precision(2)
// scale(2) length(8) sqlType(2) ccsid(2), 9 reserved zero bytes, SQLNAME
// (cp500 VCM+VCS), SQLLABEL (cp500 VCM+VCS), SQLCOMMENTS (empty VCM+VCS),
// 2 more reserved zero bytes, then a 2-byte 0xFF 0xFF trailer.
func sqlDARDColumn(t *testing.T, length uint64, sqlType uint16, ccsid uint16, sqlName, sqlLabel string) []byte {
	t.Helper()
	name, err := encodeCP500(sqlName)
	require.NoError(t, err)
	label, err := encodeCP500(sqlLabel)
	require.NoError(t, err)

	buf := make([]byte, 0, 16+9+4+len(name)+4+len(label)+6+2)
	buf = append(buf, 0x00, 0x00) // precision
	buf = append(buf, 0x00, 0x00) // scale
	lengthBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lengthBytes, length)
	buf = append(buf, lengthBytes...)
	typeBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(typeBytes, sqlType)
	buf = append(buf, typeBytes...)
	ccsidBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(ccsidBytes, ccsid)
	buf = append(buf, ccsidBytes...)
	buf = append(buf, make([]byte, 9)...) // null indicator, SQLUNNAMED, 6 reserved
	buf = append(buf, 0x00, 0x00)         // SQLNAME VCM: empty
	buf = append(buf, byte(len(name)>>8), byte(len(name)))
	buf = append(buf, name...)
	buf = append(buf, 0x00, 0x00) // SQLLABEL VCM: empty
	buf = append(buf, byte(len(label)>>8), byte(len(label)))
	buf = append(buf, label...)
	buf = append(buf, 0x00, 0x00, 0x00, 0x00) // SQLCOMMENTS VCM+VCS: both empty
	buf = append(buf, 0x00, 0x00)             // 2 more reserved bytes
	buf = append(buf, 0xff, 0xff)             // trailing null-group indicators
	return buf
}

// TestParseSQLDARDDecodesRealIBMiCapture pins the exact SQLDARD shape
// reported on issue #3: a single-column PRPSQLSTT reply (TABLE_NAME,
// VARCHAR(128), CCSID 37) that the parser previously read as 0 columns,
// then misaligned once SQLNUMROWS itself was fixed.
func TestParseSQLDARDDecodesRealIBMiCapture(t *testing.T) {
	obj := sqlDARDHeader(1)
	obj = append(obj, sqlDARDColumn(t, 128, 448, 37, "TABLE_NAME", "TABLE_NAME")...)

	columns, err := parseSQLDARD(obj, db2PlatformIBMi)

	require.NoError(t, err)
	require.Len(t, columns, 1)
	require.Equal(t, "TABLE_NAME", columns[0].Name)
	require.Equal(t, db2SQLTypeVarchar, columns[0].SQLType)
	require.Equal(t, 128, columns[0].Length)
	require.Equal(t, 0, columns[0].Precision)
	require.Equal(t, 0, columns[0].Scale)
}

// TestParseSQLDARDKeepsColumnsAlignedPastTheFirst pins the exact bug
// reported on issue #3: once SQLNUMROWS parsed correctly, column 1 (and
// every column after it) was still misaligned by a few bytes because the
// column descriptor's own name-block skip lengths were wrong - confirmed
// via the real symptom (column 1 decoded as SQLTYPE 9472/0x2500, length
// 2147598336/0x8001C000, name ""). Also pins that SQLNAME (not SQLLABEL)
// is the reported column name - the old "prefer label" order was masked
// on the real capture only because that column's name and label happened
// to be identical.
func TestParseSQLDARDKeepsColumnsAlignedPastTheFirst(t *testing.T) {
	obj := sqlDARDHeader(2)
	obj = append(obj, sqlDARDColumn(t, 128, 448, 37, "TABLE_NAME", "Table Name")...)
	obj = append(obj, sqlDARDColumn(t, 4, 496, 37, "COLUMN_COUNT", "Column Count")...)

	columns, err := parseSQLDARD(obj, db2PlatformIBMi)

	require.NoError(t, err)
	require.Len(t, columns, 2)
	require.Equal(t, "TABLE_NAME", columns[0].Name)
	require.Equal(t, db2SQLTypeVarchar, columns[0].SQLType)
	require.Equal(t, 128, columns[0].Length)
	require.Equal(t, "COLUMN_COUNT", columns[1].Name, "column 1 must stay aligned and report SQLNAME, not SQLLABEL")
	require.Equal(t, db2SQLTypeInteger, columns[1].SQLType)
	require.Equal(t, 4, columns[1].Length)
}

func minimalSQLDARD() []byte {
	obj := []byte{0xff, 0x01, 0x01, 0x00}
	return append(obj, make([]byte, 45)...) // 16-byte column header + 29-byte unnamed-column block
}

func TestStreamSkipsDSCSQLSTTWhenPrepareReturnsSQLDARD(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:     clientConn,
		database: padDatabaseName("SIGLPAR"),
		timeout:  time.Second,
	}

	var codePoints []uint16
	var sawColumns bool
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		for {
			p, err := readDSS(serverConn)
			require.NoError(t, err)
			codePoints = append(codePoints, p.codePoint)
			if !p.chained {
				break
			}
		}
		// PRPSQLSTT's reply carries a real SQLDARD this time.
		require.NoError(t, writeTestDSS(serverConn, cpSQLDARD, minimalSQLDARD(), false))

		for {
			p, err := readDSS(serverConn)
			require.NoError(t, err)
			codePoints = append(codePoints, p.codePoint)
			if !p.chained {
				break
			}
		}
		require.NoError(t, writeTestDSS(serverConn, cpRDBCMM, nil, false))
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.Stream(context.Background(), "SELECT 1 FROM QSYS2.SYSTABLES", db2StreamHandler{
			Columns: func([]db2Column) error {
				sawColumns = true
				return nil
			},
		})
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stream did not return in time")
	}

	require.True(t, sawColumns, "SQLDARD during PRPSQLSTT should still reach the caller's Columns handler")
	require.Equal(t, []uint16{cpPRPSQLSTT, cpSQLSTT, cpOPNQRY}, codePoints,
		"DSCSQLSTT must not be sent when PRPSQLSTT already returned a real SQLDARD")
}

func TestStreamSendsPrepareAndOpenAsSeparateRoundTrips(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:     clientConn,
		database: padDatabaseName("SIGLPAR"),
		timeout:  time.Second,
	}

	var codePoints []uint16
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		// PRPSQLSTT + SQLSTT must be fully drained and replied to before
		// OPNQRY is ever sent - if Stream still chained them together,
		// this second drain would see OPNQRY mixed into the first one.
		// This handler has no Columns callback (the catalog-driven table
		// path's own shape), so DSCSQLSTT must not fire even though
		// drainOneRequest's generic RDBCMM reply carries no SQLDARD - see
		// TestStreamSendsDSCSQLSTTForCustomQueriesWhenPrepareOmitsSQLDARD
		// for the case where a caller that actually wants column metadata
		// triggers it.
		drainOneRequest(t, serverConn, &codePoints)
		drainOneRequest(t, serverConn, &codePoints)
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.Stream(context.Background(), "SELECT 1 FROM QSYS2.SYSTABLES", db2StreamHandler{})
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stream did not return in time")
	}

	require.Equal(t, []uint16{cpPRPSQLSTT, cpSQLSTT, cpOPNQRY}, codePoints,
		"DSCSQLSTT must not be sent when the caller has no Columns callback to use it")
}

func TestStreamSendsDSCSQLSTTForCustomQueriesWhenPrepareOmitsSQLDARD(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:     clientConn,
		database: padDatabaseName("SIGLPAR"),
		timeout:  time.Second,
	}

	var codePoints []uint16
	var sawColumns bool
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		drainOneRequest(t, serverConn, &codePoints) // PRPSQLSTT+SQLSTT, generic reply (no SQLDARD)

		dscSQLSTT, err := readDSS(serverConn)
		require.NoError(t, err)
		codePoints = append(codePoints, dscSQLSTT.codePoint)
		require.NoError(t, writeTestDSS(serverConn, cpSQLDARD, minimalSQLDARD(), false))

		drainOneRequest(t, serverConn, &codePoints) // OPNQRY
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.Stream(context.Background(), "SELECT TABLE_NAME FROM QSYS2.SYSTABLES", db2StreamHandler{
			Columns: func([]db2Column) error {
				sawColumns = true
				return nil
			},
		})
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stream did not return in time")
	}

	require.True(t, sawColumns, "DSCSQLSTT's own SQLDARD must still reach the caller's Columns handler")
	require.Equal(t, []uint16{cpPRPSQLSTT, cpSQLSTT, cpDSCSQLSTT, cpOPNQRY}, codePoints,
		"DSCSQLSTT must be sent when the caller needs column metadata and PRPSQLSTT didn't provide it")
}

func TestPackDSCSQLSTTDoesNotSendRTNSQLDA(t *testing.T) {
	// packDSCSQLSTT started as a copy of packPRPSQLSTT, which carries
	// RTNSQLDA (cpRTNSQLDA, 0x2116) to ask PRPSQLSTT to also return a
	// SQLDARD inline - a flag that doesn't apply to DSCSQLSTT, whose whole
	// purpose is already "return a SQLDARD for this statement". Reported
	// on issue #3: every DSCSQLSTT sent by this client got back a
	// PRMNSPRM ("parameter not supported") reply instead of a SQLDARD.
	client := &db2Client{database: padDatabaseName("SIGLPAR")}

	body, err := client.packDSCSQLSTT()

	require.NoError(t, err)
	require.NotContains(t, string(body), string([]byte{0x21, 0x16}),
		"DSCSQLSTT must not carry RTNSQLDA (cpRTNSQLDA, 0x2116)")
}

func TestStreamSurfacesPRMNSPRMFromDSCSQLSTT(t *testing.T) {
	// Confirmed on issue #3: a DSCSQLSTT round trip can get back a bare
	// PRMNSPRM reply (0x1251, "parameter not supported") instead of a
	// SQLDARD, and previously this client had no constant for 0x1251, so
	// it fell through parseResponse's default case completely silently -
	// Stream returned success with zero columns ever having arrived. This
	// must now surface as a real, labeled error.
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:     clientConn,
		database: padDatabaseName("SIGLPAR"),
		timeout:  time.Second,
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		var codePoints []uint16
		drainOneRequest(t, serverConn, &codePoints) // PRPSQLSTT+SQLSTT, generic reply (no SQLDARD)

		dscSQLSTT, err := readDSS(serverConn)
		require.NoError(t, err)
		codePoints = append(codePoints, dscSQLSTT.codePoint)
		// SVRCOD (2 bytes, value 8) + the rejected parameter's own
		// codepoint (cpRTNSQLDA, 0x2116) as a nested, value-less field -
		// same length-prefixed codepoint framing parseReplyObject already
		// handles elsewhere.
		prmnsprm := []byte{0x00, 0x06, 0x11, 0x49, 0x00, 0x08, 0x00, 0x04, 0x21, 0x16}
		require.NoError(t, writeTestDSS(serverConn, cpPRMNSPRM, prmnsprm, false))
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.Stream(context.Background(), "SELECT TABLE_NAME FROM QSYS2.SYSTABLES", db2StreamHandler{
			Columns: func([]db2Column) error { return nil },
		})
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	select {
	case err := <-done:
		require.ErrorContains(t, err, "PRMNSPRM")
	case <-time.After(time.Second):
		t.Fatal("Stream did not return in time")
	}
}

// drainOneRequest reads DSS packets from conn until a non-chained one is
// seen, recording each codepoint, then writes back a single innocuous
// reply so the caller's parseResponse completes without error.
func drainOneRequest(t *testing.T, conn net.Conn, codePoints *[]uint16) {
	t.Helper()
	for {
		p, err := readDSS(conn)
		require.NoError(t, err)
		*codePoints = append(*codePoints, p.codePoint)
		if !p.chained {
			break
		}
	}
	require.NoError(t, writeTestDSS(conn, cpRDBCMM, nil, false))
}

func TestSetClientVariablesSkipsEntirelyOnIBMi(t *testing.T) {
	// No server goroutine at all: a real IBM i host drops the connection
	// on the very first thing this function used to send (the post-auth
	// CCSID renegotiation EXCSAT), so on IBM i it must return immediately
	// without writing anything to the wire. net.Pipe's Write blocks until
	// something Reads, so if setClientVariables wrote anything here, this
	// test would hang until the timeout instead of completing.
	_, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{conn: clientConn, timeout: time.Second, platform: db2PlatformIBMi}

	done := make(chan error, 1)
	go func() {
		done <- client.setClientVariables(context.Background())
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("setClientVariables should return immediately on IBM i without writing to the connection")
	}
}

func TestSetClientVariablesSendsLUWStatementsOnLUW(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{conn: clientConn, timeout: time.Second, platform: db2PlatformLUW}

	var codePoints []uint16
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		drainOneRequest(t, serverConn, &codePoints) // CCSID EXCSAT
		drainOneRequest(t, serverConn, &codePoints) // SET CLIENT WRKSTNNAME
		drainOneRequest(t, serverConn, &codePoints) // SET CURRENT LOCALE
		drainOneRequest(t, serverConn, &codePoints) // commit
	}()

	done := make(chan error, 1)
	go func() {
		done <- client.setClientVariables(context.Background())
	}()

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server script did not complete in time")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("setClientVariables did not return in time")
	}

	require.Equal(t, []uint16{
		cpEXCSAT,
		cpEXCSQLSET, cpSQLSTT,
		cpEXCSQLSET, cpSQLSTT,
		cpRDBCMM,
	}, codePoints)
}

func TestParseResponseDetectsIBMiPlatformFromPRDID(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:    clientConn,
		timeout: time.Second,
	}

	done := make(chan error, 1)
	go func() {
		done <- client.parseResponse(context.Background(), db2StreamHandler{})
	}()

	prdid, err := packString(cpPRDID, "QSQ07030", "cp500")
	require.NoError(t, err)
	require.NoError(t, writeTestDSS(serverConn, cpACCRDBRM, prdid, false))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected parseResponse to finish")
	}

	require.Equal(t, db2PlatformIBMi, client.Platform())
}

func TestParseResponseAutoDetectsRDBNameFromACCRDBRM(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{conn: clientConn, timeout: time.Second}

	done := make(chan error, 1)
	go func() {
		done <- client.parseResponse(context.Background(), db2StreamHandler{})
	}()

	prdid, err := packString(cpPRDID, "QSQ07030", "cp500")
	require.NoError(t, err)
	rdbnam, err := packString(cpRDBNAM, "RDBNAME2", "cp500")
	require.NoError(t, err)
	object := append(append([]byte{}, prdid...), rdbnam...)
	require.NoError(t, writeTestDSS(serverConn, cpACCRDBRM, object, false))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected parseResponse to finish")
	}

	require.Equal(t, "RDBNAME2", client.rdbName,
		"database (e.g. *LOCAL) is a symbolic alias - PKGNAMCSN needs the real RDB name the server echoes back in ACCRDBRM")
}

func TestParseResponseKeepsRDBNameOverrideFromACCRDBRM(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{conn: clientConn, timeout: time.Second, rdbName: "MANUALOVERRIDE"}

	done := make(chan error, 1)
	go func() {
		done <- client.parseResponse(context.Background(), db2StreamHandler{})
	}()

	rdbnam, err := packString(cpRDBNAM, "RDBNAME2", "cp500")
	require.NoError(t, err)
	require.NoError(t, writeTestDSS(serverConn, cpACCRDBRM, rdbnam, false))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected parseResponse to finish")
	}

	require.Equal(t, "MANUALOVERRIDE", client.rdbName,
		"an explicit rdb_name override must take precedence over whatever ACCRDBRM echoes back")
}

func TestParseResponseHonorsPlatformOverride(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{
		conn:     clientConn,
		timeout:  time.Second,
		platform: db2PlatformLUW,
	}

	done := make(chan error, 1)
	go func() {
		done <- client.parseResponse(context.Background(), db2StreamHandler{})
	}()

	prdid, err := packString(cpPRDID, "QSQ07030", "cp500")
	require.NoError(t, err)
	require.NoError(t, writeTestDSS(serverConn, cpACCRDBRM, prdid, false))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected parseResponse to finish")
	}

	require.Equal(t, db2PlatformLUW, client.Platform(), "explicit override must not be clobbered by auto-detection")
}

func TestRefreshDeadlineZeroTimeoutClearsDeadline(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	require.NoError(t, clientConn.SetDeadline(time.Now().Add(-time.Second)))

	client := &db2Client{
		conn:    clientConn,
		timeout: 0,
	}
	client.refreshDeadline(context.Background())

	writeDone := make(chan error, 1)
	go func() {
		_, err := serverConn.Write([]byte{0x2a})
		writeDone <- err
	}()

	buf := make([]byte, 1)
	_, err := clientConn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, byte(0x2a), buf[0])
	require.NoError(t, <-writeDone)
}

func TestParseQRYDSCRejectsZeroLength(t *testing.T) {
	_, err := parseQRYDSC([]byte{0})

	require.ErrorContains(t, err, "invalid QRYDSC triplet length: 0")
}

func TestParseQRYDTAIncludesColumnContextOnUnsupportedType(t *testing.T) {
	fields := []drdaField{
		{typeCode: drdaTypeInteger, params: []byte{0, 4}},
		{typeCode: 0x99}, // not a recognized DRDA field type
	}
	row := []byte{0xff, 0x00, 0x2a, 0x00, 0x00, 0x00}

	_, err := parseQRYDTA(row, fields, db2PlatformAuto)

	require.ErrorContains(t, err, "column 1")
	require.ErrorContains(t, err, "0x99")
}

func TestParseQRYDSCResolvesVARGRAPHICThroughSDALIDs(t *testing.T) {
	// The exact QRYDSC triplet bytes from a real IBM i host's reply on
	// issue #3, for a query with two VARGRAPHIC CCSID 1200 columns
	// (TABLE_TEXT NOT NULL, LONG_COMMENT nullable). The N-GDA triplet's
	// own column entries use LIDs 0x80/0x81 defined by the two preceding
	// SDA triplets, not a literal FD:OCA primitive type - which the
	// parser previously had no way to resolve, surfacing as "unsupported
	// DRDA field type: 0x80".
	obj := []byte{
		0x07, 0x78, 0x00, 0x05, 0x01, 0x01, 0x38, // MDD
		0x0c, 0x70, 0x80, 0x11, 0x00, 0x00, 0x04, 0xb0, 0x02, 0x01, 0x3f, 0xff, // SDA: LID 0x80, CCSID 1200, 2 bytes/char
		0x07, 0x78, 0x00, 0x05, 0x01, 0x01, 0x3b, // MDD
		0x0c, 0x70, 0x81, 0x91, 0x00, 0x00, 0x04, 0xb0, 0x02, 0x01, 0x3f, 0xff, // SDA: LID 0x81, CCSID 1200, 2 bytes/char
		0x07, 0x78, 0x00, 0x05, 0x02, 0x01, 0xd0, // MDD (row layout)
		0x09, 0x76, 0xd0, 0x80, 0x00, 0x32, 0x81, 0x07, 0xd0, // N-GDA: column(LID=0x80, len=50), column(LID=0x81, len=2000)
	}

	fields, err := parseQRYDSC(obj)

	require.NoError(t, err)
	require.Len(t, fields, 2)
	require.Equal(t, drdaTypeVarGraph, fields[0].typeCode, "LID 0x80 is even (NOT NULL column) - resolves to the non-null VARGRAPH type")
	require.Equal(t, []byte{0x00, 0x32}, fields[0].params)
	require.Equal(t, drdaTypeNVarGraph, fields[1].typeCode, "LID 0x81 is odd (nullable column) - resolves to the nullable VARGRAPH type")
	require.Equal(t, []byte{0x07, 0xd0}, fields[1].params)
}

func TestParseQRYDSCFailsClearlyForLOBResolvedThroughSDALID(t *testing.T) {
	// The exact SDA + N-GDA bytes from a real IBM i host's QRYDSC reply on
	// issue #3 for a DBCLOB column (MQT_DEFINITION): base_type 0xd1, CCSID
	// 13488, 2 bytes/char - the same CCSID/bytes-per-char combination as
	// VARGRAPHIC, which a resolver keying only on those two fields
	// misresolved as VARGRAPHIC. The SDA's own maxLength (0x8008) and the
	// N-GDA entry's own param bytes (0x8009) both have the high bit set -
	// this client's own existing convention (see drdaTypeNLobBytes/
	// NLobCSBCS) for "data carried via EXTDTA, not inline" - so this must
	// be rejected clearly instead of being decoded as if it were inline
	// UTF-16BE text.
	obj := []byte{
		0x0c, 0x70, 0x82, 0xd1, 0x00, 0x00, 0x34, 0xb0, 0x02, 0x01, 0x80, 0x08, // SDA: LID 0x82, base_type 0xd1, CCSID 13488, 2 bytes/char, maxLength high bit set
		0x06, 0x76, 0xd0, 0x82, 0x80, 0x09, // N-GDA: column(LID=0x82, params=0x8009)
	}

	_, err := parseQRYDSC(obj)

	require.ErrorContains(t, err, "LOB")
	require.ErrorContains(t, err, "not supported")
}

func TestParseQRYDSCSkipsLeadingMDDTriplet(t *testing.T) {
	// The exact MDD triplet bytes from a real IBM i host's QRYDSC reply on
	// issue #3 (07 78 00 05 01 01 38 - 7 bytes, matching FD:OCA's
	// documented MDD_TRIPLET_SIZE), which earlier code misread as the
	// entire descriptor and rejected outright as "unsupported QRYDSC
	// descriptor: 780005010138" - that hex is this MDD triplet's body,
	// not a malformed N-GDA triplet. Followed by a minimal, otherwise
	// normal N-GDA triplet (one INTEGER column) to confirm parsing
	// continues correctly past it.
	mdd := []byte{0x07, 0x78, 0x00, 0x05, 0x01, 0x01, 0x38}
	ngda := []byte{0x06, 0x76, 0xd0, drdaTypeInteger, 0x00, 0x04}
	obj := append(append([]byte{}, mdd...), ngda...)

	fields, err := parseQRYDSC(obj)

	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Equal(t, drdaTypeInteger, fields[0].typeCode)
	require.Equal(t, []byte{0x00, 0x04}, fields[0].params)
}

// TestReadDSSReassemblesExtendedQRYDTASegments pins the off-by-4 reported
// on issue #3 for a real 72-column table's QRYDTA: the object's own
// extended-length field, plus the two segments' real data, reconciled
// exactly (32753 + (3449-2) = 36200). The old chunkLen formula
// (objLen-15) read 4 bytes too many into the first segment, eating 4
// bytes of the second segment's own 2-byte continuation-length header
// plus 2 bytes of its real data, and misreading what was left as a
// continuation length of 0.
func TestReadDSSReassemblesExtendedQRYDTASegments(t *testing.T) {
	data1 := []byte{0xff, 0x01, 0x02, 0x03, 0x04}
	data2 := []byte{0x05, 0x06, 0x07}
	total := uint32(len(data1) + len(data2))

	chunk := make([]byte, 0, 4+len(data1))
	extLenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(extLenBuf, total)
	chunk = append(chunk, extLenBuf...)
	chunk = append(chunk, data1...)

	buf := &bytes.Buffer{}
	buf.Write([]byte{0xff, 0xff, 0xd0, 0x01, 0x00, 0x01}) // 6-byte DSS header, dssLen=0xffff
	objLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(objLenBuf, uint16(len(chunk)+19))
	buf.Write(objLenBuf)
	cpBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(cpBuf, cpQRYDTA)
	buf.Write(cpBuf)
	buf.Write(chunk)
	nextLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(nextLenBuf, uint16(len(data2)+2))
	buf.Write(nextLenBuf)
	buf.Write(data2)

	p, err := readDSS(buf)

	require.NoError(t, err)
	require.Equal(t, append(append([]byte{}, data1...), data2...), p.object)
	require.False(t, p.moreData)
}

// TestReadDSSReassemblesThreeOrMoreExtendedQRYDTASegments pins the hang
// reported on issue #3 for QRYDTA objects spanning 3+ segments. An
// intermediate continuation segment's 2-byte header has its high bit set
// ("more follows") with the segment's own length (including the header)
// in the low 15 bits - treating that raw header as a literal byte count,
// unmasked, made the old code try to read far more bytes than were ever
// sent, blocking until the connection timed out. The one capture
// available before this fix (TestReadDSSReassemblesExtendedQRYDTASegments
// above) only ever exercised a final segment, whose high bit is clear by
// definition, so it couldn't have caught this.
func TestReadDSSReassemblesThreeOrMoreExtendedQRYDTASegments(t *testing.T) {
	data1 := []byte{0xff, 0x01, 0x02, 0x03, 0x04}
	data2 := []byte{0x05, 0x06, 0x07}
	data3 := []byte{0x08, 0x09}
	total := uint32(len(data1) + len(data2) + len(data3))

	chunk := make([]byte, 0, 4+len(data1))
	extLenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(extLenBuf, total)
	chunk = append(chunk, extLenBuf...)
	chunk = append(chunk, data1...)

	buf := &bytes.Buffer{}
	buf.Write([]byte{0xff, 0xff, 0xd0, 0x01, 0x00, 0x01}) // 6-byte DSS header, dssLen=0xffff
	objLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(objLenBuf, uint16(len(chunk)+19))
	buf.Write(objLenBuf)
	cpBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(cpBuf, cpQRYDTA)
	buf.Write(cpBuf)
	buf.Write(chunk)

	// Intermediate segment: high bit (0x8000, "more follows") set, low 15
	// bits are this segment's own length including its 2-byte header.
	seg2LenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(seg2LenBuf, uint16(len(data2)+2)|0x8000)
	buf.Write(seg2LenBuf)
	buf.Write(data2)

	// Final segment: high bit clear.
	seg3LenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(seg3LenBuf, uint16(len(data3)+2))
	buf.Write(seg3LenBuf)
	buf.Write(data3)

	p, err := readDSS(buf)

	require.NoError(t, err)
	want := append(append(append([]byte{}, data1...), data2...), data3...)
	require.Equal(t, want, p.object)
	require.False(t, p.moreData)
}

func TestPackDSSObjectRejectsOversizedBody(t *testing.T) {
	_, err := packDSSObject(cpSQLSTT, make([]byte, maxDSSObjectBodyLength+1))

	require.ErrorContains(t, err, "body too large")
}

func TestPackSQLSTTRejectsOversizedQuery(t *testing.T) {
	_, err := packSQLSTT(strings.Repeat("x", maxDSSObjectBodyLength))

	require.ErrorContains(t, err, "body too large")
}

// stubSignonExchange stands in for a real Sign-on Server connection in
// handshake tests that aren't themselves testing the Sign-on Server
// protocol (that's signon_test.go's job) but still exercise the SECMEC 8
// path, which now only consults the Sign-on Server for the password level
// (deciding SHA-1 vs SHA-512) - the seed pair the substitute actually uses
// comes from a dedicated ACCSEC/ACCSECRD round on the DRDA connection
// itself (see handshake), so the seeds returned here are unused filler.
func stubSignonExchange(passwordLevel int) func(context.Context, string, int, bool, time.Duration) (signonAttributes, error) {
	return func(context.Context, string, int, bool, time.Duration) (signonAttributes, error) {
		return signonAttributes{
			passwordLevel: passwordLevel,
			serverSeed:    []byte{0, 0, 0, 0, 0, 0, 0, 0},
			clientSeed:    []byte{0, 0, 0, 0, 0, 0, 0, 0},
		}, nil
	}
}

func writeTestDSS(conn net.Conn, codePoint uint16, object []byte, chained bool) error {
	body, err := packDSSObject(codePoint, object)
	if err != nil {
		return err
	}
	packet := make([]byte, 6+len(body))
	binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
	packet[2] = 0xd0
	packet[3] = 0x01
	if chained {
		packet[3] |= 0x40
	}
	binary.BigEndian.PutUint16(packet[4:6], 1)
	copy(packet[6:], body)

	_, err = conn.Write(packet)
	return err
}

func lengthParams(length int) []byte {
	out := make([]byte, 2)
	binary.BigEndian.PutUint16(out, uint16(length))
	return out
}
