//go:build stress

package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/stretchr/testify/require"
)

type mysqlCDCFailingStreamer struct {
	err error
}

func (s mysqlCDCFailingStreamer) GetEvent(context.Context) (*replication.BinlogEvent, error) {
	return nil, s.err
}

func TestMySQLCDC_StressReaderErrorIsNotTimeout(t *testing.T) {
	readerErr := errors.New("replication stream closed")
	_, err, eventCtxErr := readMySQLCDCEvent(context.Background(), mysqlCDCFailingStreamer{err: readerErr})

	require.ErrorIs(t, err, readerErr)
	require.NoError(t, eventCtxErr, "an immediate binlog-reader failure must not be classified as a timeout")
}
