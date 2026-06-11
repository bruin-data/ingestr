package db2

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
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

func writeTestDSS(conn net.Conn, codePoint uint16, object []byte, chained bool) error {
	body := packDSSObject(codePoint, object)
	packet := make([]byte, 6+len(body))
	binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
	packet[2] = 0xd0
	packet[3] = 0x01
	if chained {
		packet[3] |= 0x40
	}
	binary.BigEndian.PutUint16(packet[4:6], 1)
	copy(packet[6:], body)

	_, err := conn.Write(packet)
	return err
}
