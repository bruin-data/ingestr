package db2

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeSignonReply builds a reply datastream matching what IBM i's Sign-on
// Server sends back for an attribute exchange: a 20-byte common header,
// then a fixed-layout prefix (RC at 20, padding, server version, padding,
// server level at 40), then the variable parameter section starting at
// offset 42 carrying the server seed (CP 0x1103) and password level
// (CP 0x1119) this client actually reads.
func fakeSignonReply(passwordLevel byte, serverSeed []byte) []byte {
	const prefixEnd = 42
	seedEntryLen := 6 + len(serverSeed)
	pwdLevelEntryLen := 7
	total := prefixEnd + seedEntryLen + pwdLevelEntryLen

	reply := make([]byte, total)
	binary.BigEndian.PutUint32(reply[0:4], uint32(total))
	binary.BigEndian.PutUint32(reply[20:24], 0) // RC = 0 (success)

	offset := prefixEnd
	binary.BigEndian.PutUint32(reply[offset:offset+4], uint32(seedEntryLen))
	binary.BigEndian.PutUint16(reply[offset+4:offset+6], signonCPSeed)
	copy(reply[offset+6:offset+6+len(serverSeed)], serverSeed)
	offset += seedEntryLen

	binary.BigEndian.PutUint32(reply[offset:offset+4], uint32(pwdLevelEntryLen))
	binary.BigEndian.PutUint16(reply[offset+4:offset+6], signonCPPasswordLevel)
	reply[offset+6] = passwordLevel

	return reply
}

func TestExchangeSignonAttributesParsesServerSeedAndPasswordLevel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	serverSeed := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		req := make([]byte, 52)
		if _, err := readFull(conn, req); err != nil {
			return
		}
		if binary.BigEndian.Uint16(req[6:8]) != signonServerID {
			t.Errorf("request serverID = 0x%x, want 0x%x", binary.BigEndian.Uint16(req[6:8]), signonServerID)
		}
		if binary.BigEndian.Uint16(req[18:20]) != signonReqRepIDExchangeAttributes {
			t.Errorf("request reqRepID = 0x%x, want 0x%x", binary.BigEndian.Uint16(req[18:20]), signonReqRepIDExchangeAttributes)
		}

		_, _ = conn.Write(fakeSignonReply(3, serverSeed))
	}()

	attrs, err := exchangeSignonAttributesAt(context.Background(), ln.Addr().String(), time.Second, nil)
	require.NoError(t, err)

	<-serverDone

	require.Equal(t, 3, attrs.passwordLevel)
	require.Equal(t, serverSeed, attrs.serverSeed)
	require.Len(t, attrs.clientSeed, 8)
}

func TestExchangeSignonAttributesFailsOnNonZeroRC(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		req := make([]byte, 52)
		if _, err := readFull(conn, req); err != nil {
			return
		}
		reply := make([]byte, 24)
		binary.BigEndian.PutUint32(reply[0:4], 24)
		binary.BigEndian.PutUint32(reply[20:24], 8) // RC != 0
		_, _ = conn.Write(reply)
	}()

	_, err = exchangeSignonAttributesAt(context.Background(), ln.Addr().String(), time.Second, nil)
	require.ErrorContains(t, err, "RC=8")
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
