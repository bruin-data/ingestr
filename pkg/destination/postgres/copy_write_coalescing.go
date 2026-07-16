package postgres

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresCopyWriteBufferSize = 1024 * 1024

type copyDataWriteCoalescingConn struct {
	net.Conn

	mu          sync.Mutex
	buffer      []byte
	bufferLimit int
	writeErr    error
}

func configureCopyDataWriteCoalescing(config *pgxpool.Config) {
	afterNetConnect := config.ConnConfig.AfterNetConnect
	config.ConnConfig.AfterNetConnect = func(ctx context.Context, connectConfig *pgconn.Config, conn net.Conn) (net.Conn, error) {
		if afterNetConnect != nil {
			var err error
			conn, err = afterNetConnect(ctx, connectConfig, conn)
			if err != nil {
				return nil, err
			}
		}
		return &copyDataWriteCoalescingConn{
			Conn:        conn,
			bufferLimit: postgresCopyWriteBufferSize,
		}, nil
	}
}

func (c *copyDataWriteCoalescingConn) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if !isPostgresCopyDataFrame(data) {
		if err := c.flushLocked(); err != nil {
			return 0, err
		}
		return c.Conn.Write(data)
	}

	if len(c.buffer) > 0 && len(c.buffer)+len(data) > c.bufferLimit {
		if err := c.flushLocked(); err != nil {
			return 0, err
		}
	}
	if len(data) >= c.bufferLimit {
		if err := c.writeAllLocked(data); err != nil {
			return 0, err
		}
		return len(data), nil
	}

	if c.buffer == nil {
		c.buffer = make([]byte, 0, c.bufferLimit)
	}
	c.buffer = append(c.buffer, data...)
	if len(c.buffer) >= c.bufferLimit {
		if err := c.flushLocked(); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (c *copyDataWriteCoalescingConn) flushLocked() error {
	if len(c.buffer) == 0 {
		return nil
	}
	err := c.writeAllLocked(c.buffer)
	c.buffer = c.buffer[:0]
	return err
}

func (c *copyDataWriteCoalescingConn) writeAllLocked(data []byte) error {
	for len(data) > 0 {
		n, err := c.Conn.Write(data)
		data = data[n:]
		if err != nil {
			c.writeErr = err
			return err
		}
		if n == 0 {
			c.writeErr = io.ErrShortWrite
			return c.writeErr
		}
	}
	return nil
}

func isPostgresCopyDataFrame(data []byte) bool {
	return len(data) >= 5 && data[0] == 'd' && int(binary.BigEndian.Uint32(data[1:5])) == len(data)-1
}
