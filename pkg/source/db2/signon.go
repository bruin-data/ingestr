package db2

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// IBM i's Sign-on Server is a separate, non-DRDA proprietary protocol
// (historically TCP port 8476, 9476 under SSL) that a client must use to
// learn two things before it can authenticate over DRDA/DDM with SECMEC 8
// (a password-substitute mechanism): the server's password level (which of
// two substitute algorithms to use) and a server-generated random seed the
// substitute is keyed with. None of this is part of the open DRDA/DDM
// specification; the wire format here is ported from IBM's own JTOpen
// (jt400) client - AS400ImplRemote.signonConnect, SignonExchangeAttributeReq/
// Rep, PortMapper, commit 613335c in https://github.com/IBM/JTOpen.
const (
	signonPort     = 8476
	signonPortSSL  = 9476
	signonServerID = 0xE009

	signonReqRepIDExchangeAttributes = 0x7003

	signonCPClientVersion = 0x1101
	signonCPClientLevel   = 0x1102
	signonCPSeed          = 0x1103
	signonCPPasswordLevel = 0x1119

	// signonClientLevel is the highest datastream level JTOpen itself
	// currently claims (password-level-4 support included); the server's
	// actual password level comes back independently in the reply and is
	// what this client acts on, so overclaiming support here doesn't risk
	// anything behaving differently than it otherwise would.
	signonClientLevel = 10
)

// signonAttributes is what the Sign-on Server's attribute-exchange reply
// gives a client: the password level governing which substitute-password
// algorithm SECCHK's SECMEC 8 credential must use, and the two seeds
// (server-generated, client-generated) that algorithm is keyed with.
type signonAttributes struct {
	passwordLevel int
	serverSeed    []byte
	clientSeed    []byte
}

// exchangeSignonAttributes connects to IBM i's Sign-on Server and performs
// the "exchange client/server attributes" request/reply - the only part of
// that protocol this client needs, since it exists solely to compute a
// SECMEC 8 credential for the DRDA/DDM connection on a different port
// (446) entirely. The connection here is closed before returning in either
// case; nothing further is needed from the Sign-on Server itself.
//
// host/port override the normal target (the DRDA host, on the well-known
// Sign-on Server port) - port 0 means "use the well-known default for
// useSSL". Needed for setups where only the DRDA port is reachable at the
// main host and the Sign-on Server has to be reached some other way
// (reported on issue #3: a port-forwarding proxy that only forwards 446).
func exchangeSignonAttributes(ctx context.Context, host string, port int, useSSL bool, timeout time.Duration) (signonAttributes, error) {
	if port == 0 {
		port = signonPort
		if useSSL {
			port = signonPortSSL
		}
	}
	return exchangeSignonAttributesAt(ctx, fmt.Sprintf("%s:%d", host, port), timeout, func(conn net.Conn) (net.Conn, error) {
		if !useSSL {
			return conn, nil
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("Sign-on Server TLS handshake: %w", err)
		}
		return tlsConn, nil
	})
}

// exchangeSignonAttributesAt is exchangeSignonAttributes with the dial
// address and TLS wrapping factored out, so tests can point it at a local
// fake listener instead of the hardcoded well-known Sign-on Server port.
func exchangeSignonAttributesAt(ctx context.Context, addr string, timeout time.Duration, wrap func(net.Conn) (net.Conn, error)) (signonAttributes, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return signonAttributes{}, fmt.Errorf("connecting to Sign-on Server: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if wrap != nil {
		wrapped, err := wrap(conn)
		if err != nil {
			return signonAttributes{}, err
		}
		conn = wrapped
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	clientSeed := make([]byte, 8)
	if _, err := rand.Read(clientSeed); err != nil {
		return signonAttributes{}, fmt.Errorf("generating Sign-on Server client seed: %w", err)
	}

	if _, err := conn.Write(packSignonExchangeAttributesRequest(clientSeed)); err != nil {
		return signonAttributes{}, fmt.Errorf("sending Sign-on Server attribute exchange: %w", err)
	}

	reply, err := readSignonReply(conn)
	if err != nil {
		return signonAttributes{}, fmt.Errorf("reading Sign-on Server attribute exchange reply: %w", err)
	}

	rc := binary.BigEndian.Uint32(reply[20:24])
	if rc != 0 {
		return signonAttributes{}, fmt.Errorf("Sign-on Server rejected attribute exchange (RC=%d)", rc)
	}

	fields := parseSignonReplyFields(reply)
	serverSeed, ok := fields[signonCPSeed]
	if !ok || len(serverSeed) < 8 {
		return signonAttributes{}, fmt.Errorf("Sign-on Server reply has no server seed")
	}
	passwordLevel := 0
	if v, ok := fields[signonCPPasswordLevel]; ok && len(v) >= 1 {
		passwordLevel = int(v[0])
	}

	return signonAttributes{
		passwordLevel: passwordLevel,
		serverSeed:    serverSeed[:8],
		clientSeed:    clientSeed,
	}, nil
}

// packSignonExchangeAttributesRequest builds the "exchange client/server
// attributes" request datastream: a 20-byte common Client Access header
// (length, headerID=0, serverID=0xE009 for Sign-on, csInstance=0,
// correlation=0, templateLen=0, reqRepID=0x7003) followed by three LL/CP/
// data parameters - client version, client datastream level, and the
// client's random seed. Ported from SignonExchangeAttributeReq.
func packSignonExchangeAttributesRequest(clientSeed []byte) []byte {
	buf := make([]byte, 52)
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(buf)))
	// buf[4:6] headerID = 0
	binary.BigEndian.PutUint16(buf[6:8], signonServerID)
	// buf[8:12] csInstance = 0, buf[12:16] correlation = 0, buf[16:18] templateLen = 0
	binary.BigEndian.PutUint16(buf[18:20], signonReqRepIDExchangeAttributes)

	binary.BigEndian.PutUint32(buf[20:24], 10) // LL
	binary.BigEndian.PutUint16(buf[24:26], signonCPClientVersion)
	binary.BigEndian.PutUint32(buf[26:30], 1) // client version

	binary.BigEndian.PutUint32(buf[30:34], 8) // LL
	binary.BigEndian.PutUint16(buf[34:36], signonCPClientLevel)
	binary.BigEndian.PutUint16(buf[36:38], signonClientLevel)

	binary.BigEndian.PutUint32(buf[38:42], 14) // LL
	binary.BigEndian.PutUint16(buf[42:44], signonCPSeed)
	copy(buf[44:52], clientSeed)

	return buf
}

// readSignonReply reads a Sign-on Server reply datastream: a 4-byte
// big-endian total length at the very start of the same 20-byte common
// header used by the request, followed by that many bytes total.
func readSignonReply(conn net.Conn) ([]byte, error) {
	header := make([]byte, 20)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	total := binary.BigEndian.Uint32(header[0:4])
	if total < 20 {
		return nil, fmt.Errorf("invalid Sign-on Server reply length: %d", total)
	}
	reply := make([]byte, total)
	copy(reply, header)
	if total > 20 {
		if _, err := io.ReadFull(conn, reply[20:]); err != nil {
			return nil, err
		}
	}
	return reply, nil
}

// parseSignonReplyFields decodes the Sign-on Server reply's variable
// parameter section: 4-byte LL, 2-byte CP, then (LL-6) bytes of data,
// starting at offset 42 - a fixed-layout prefix (RC, server version,
// server level, ...) occupies bytes 20-41 ahead of it. This is the same
// LL/CP/data shape as DRDA's own parseReplyObject, just with a 4-byte LL
// instead of DRDA's 2-byte LL and a different starting offset.
func parseSignonReplyFields(reply []byte) map[uint16][]byte {
	out := make(map[uint16][]byte)
	for i := 42; i+6 <= len(reply); {
		ln := int(binary.BigEndian.Uint32(reply[i : i+4]))
		if ln < 6 || i+ln > len(reply) {
			break
		}
		cp := binary.BigEndian.Uint16(reply[i+4 : i+6])
		out[cp] = reply[i+6 : i+ln]
		i += ln
	}
	return out
}
