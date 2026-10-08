package db2

import (
	"bytes"
	"context"
	"encoding/binary"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadDRDAFieldNonNullableDecimal(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte{0x12, 0x34, 0x5c}), drdaField{
		typeCode: drdaTypeDecimal,
		params:   []byte{5, 2},
	})

	require.NoError(t, err)
	require.Equal(t, "123.45", value.(interface{ String() string }).String())
}

func TestReadDRDAFieldTimestampTruncatesToMicroseconds(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("2024-01-02-03.04.05.123456789012")), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len("2024-01-02-03.04.05.123456789012")),
	})

	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC), value)
}

func TestReadDRDAFieldTimestampPadsToMicroseconds(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("2024-01-02-03.04.05.1")), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len("2024-01-02-03.04.05.1")),
	})

	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 1, 2, 3, 4, 5, 100000000, time.UTC), value)
}

func TestReadDRDAFieldTimestampReturnsErrorForMalformedValue(t *testing.T) {
	raw := "not-a-timestamp"
	value, err := readDRDAField(bytes.NewReader([]byte(raw)), drdaField{
		typeCode: drdaTypeTimestamp,
		params:   lengthParams(len(raw)),
	})

	require.ErrorContains(t, err, "invalid Db2 timestamp")
	require.Nil(t, value)
}

func TestReadDRDAFieldTimeNormalizesSeparators(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("03.04.05")), drdaField{
		typeCode: drdaTypeTime,
		params:   lengthParams(len("03.04.05")),
	})

	require.NoError(t, err)
	require.Equal(t, "03:04:05", value)
}

func TestReadDRDAFieldTimePreservesFraction(t *testing.T) {
	value, err := readDRDAField(bytes.NewReader([]byte("03.04.05.1")), drdaField{
		typeCode: drdaTypeTime,
		params:   lengthParams(len("03.04.05.1")),
	})

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

	_, err := client.packSECCHK(secmecEncryptedUserPassword, []byte("short-token"))

	require.ErrorContains(t, err, "credential encryption failed for user")
	require.ErrorContains(t, err, "expected 32-byte security token")
}

func TestCP500EncodingUsesCodePage500(t *testing.T) {
	encoded, err := encodeString("[]{}@#$", "cp500")

	require.NoError(t, err)
	require.Equal(t, []byte{0x4a, 0x5a, 0xc0, 0xd0, 0x7c, 0x7b, 0x5b}, encoded)
	require.Equal(t, "[]{}@#$", decodeString(encoded, "cp500"))
}

func TestPackPKGNAMCSNUsesCatalogBytes(t *testing.T) {
	client := &db2Client{database: padDatabaseName("[")}

	packet, err := client.packPKGNAMCSN(db2PackageSectionQuery)

	require.NoError(t, err)
	require.Equal(t, byte('['), packet[4])
	require.Equal(t, []byte(db2PackageToken), packet[4+18+18+18:4+18+18+18+8])
	require.Equal(t, []byte{0x00, db2PackageSectionQuery}, packet[len(packet)-2:])
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

func TestSetClientVariablesSkipsLUWStatementsOnIBMi(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	client := &db2Client{conn: clientConn, timeout: time.Second, platform: db2PlatformIBMi}

	var codePoints []uint16
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		drainOneRequest(t, serverConn, &codePoints) // CCSID EXCSAT
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

	require.Equal(t, []uint16{cpEXCSAT, cpRDBCMM}, codePoints)
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

	require.ErrorContains(t, err, "invalid QRYDSC length: 0")
}

func TestPackDSSObjectRejectsOversizedBody(t *testing.T) {
	_, err := packDSSObject(cpSQLSTT, make([]byte, maxDSSObjectBodyLength+1))

	require.ErrorContains(t, err, "body too large")
}

func TestPackSQLSTTRejectsOversizedQuery(t *testing.T) {
	_, err := packSQLSTT(strings.Repeat("x", maxDSSObjectBodyLength))

	require.ErrorContains(t, err, "body too large")
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
