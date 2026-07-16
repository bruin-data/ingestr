package postgres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCopyDataWriteCoalescingConnFlushesCopyFramesTogether(t *testing.T) {
	underlying := &recordingConn{}
	conn := &copyDataWriteCoalescingConn{Conn: underlying, bufferLimit: 1024}
	first := copyDataFrame([]byte("first"))
	second := copyDataFrame([]byte("second"))

	n, err := conn.Write(first)
	require.NoError(t, err)
	require.Equal(t, len(first), n)
	n, err = conn.Write(second)
	require.NoError(t, err)
	require.Equal(t, len(second), n)
	require.Empty(t, underlying.writes)

	query := []byte{'Q', 0, 0, 0, 4}
	n, err = conn.Write(query)
	require.NoError(t, err)
	require.Equal(t, len(query), n)
	require.Equal(t, [][]byte{append(append([]byte{}, first...), second...), query}, underlying.writes)
}

func TestCopyDataWriteCoalescingConnFlushesAtLimit(t *testing.T) {
	underlying := &recordingConn{}
	first := copyDataFrame([]byte("one"))
	second := copyDataFrame([]byte("two"))
	conn := &copyDataWriteCoalescingConn{Conn: underlying, bufferLimit: len(first) + len(second)}

	_, err := conn.Write(first)
	require.NoError(t, err)
	require.Empty(t, underlying.writes)
	_, err = conn.Write(second)
	require.NoError(t, err)
	require.Equal(t, [][]byte{append(append([]byte{}, first...), second...)}, underlying.writes)
}

func TestCopyDataWriteCoalescingConnDoesNotBufferMalformedFrame(t *testing.T) {
	underlying := &recordingConn{}
	conn := &copyDataWriteCoalescingConn{Conn: underlying, bufferLimit: 1024}
	frame := copyDataFrame([]byte("valid"))
	malformed := []byte{'d', 0, 0, 0, 99, 1}

	_, err := conn.Write(frame)
	require.NoError(t, err)
	_, err = conn.Write(malformed)
	require.NoError(t, err)
	require.Equal(t, [][]byte{frame, malformed}, underlying.writes)
}

func TestCopyDataWriteCoalescingConnRetainsFlushError(t *testing.T) {
	wantErr := errors.New("write failed")
	underlying := &recordingConn{writeErr: wantErr}
	frame := copyDataFrame([]byte("data"))
	conn := &copyDataWriteCoalescingConn{Conn: underlying, bufferLimit: len(frame)}

	_, err := conn.Write(frame)
	require.ErrorIs(t, err, wantErr)
	_, err = conn.Write(frame)
	require.ErrorIs(t, err, wantErr)
}

func copyDataFrame(payload []byte) []byte {
	frame := make([]byte, 5, len(payload)+5)
	frame[0] = 'd'
	binary.BigEndian.PutUint32(frame[1:], uint32(len(payload)+4))
	return append(frame, payload...)
}

type recordingConn struct {
	writes   [][]byte
	writeErr error
}

func (c *recordingConn) Read([]byte) (int, error)         { return 0, nil }
func (c *recordingConn) Close() error                     { return nil }
func (c *recordingConn) LocalAddr() net.Addr              { return nil }
func (c *recordingConn) RemoteAddr() net.Addr             { return nil }
func (c *recordingConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }

func (c *recordingConn) Write(data []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	c.writes = append(c.writes, bytes.Clone(data))
	return len(data), nil
}
