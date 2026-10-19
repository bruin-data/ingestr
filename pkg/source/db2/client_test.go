package db2

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

func TestReadDRDAFieldNonNullableDecimal(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0x12, 0x34, 0x5c}), drdaField{
		typeCode: drdaTypeDecimal,
		params:   []byte{5, 2},
	}, db2PlatformAuto)

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
	lnBytes := []byte{byte(len(raw) >> 8), byte(len(raw))}

	value, err := readDRDAField(bytes.NewReader(append(lnBytes, raw...)), drdaField{
		typeCode: drdaTypeVarGraph,
	}, db2PlatformIBMi)

	require.NoError(t, err)
	require.Equal(t, "TABLE_TEXT", value)
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

	require.Equal(t, []uint16{cpPRPSQLSTT, cpSQLSTT, cpOPNQRY}, codePoints)
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
