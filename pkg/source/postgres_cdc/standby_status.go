package postgres_cdc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
)

const standbyWriteTimeout = 5 * time.Second

func sendStandbyStatusUpdate(ctx context.Context, conn *pgconn.PgConn, status pglogrepl.StandbyStatusUpdate) error {
	if conn == nil {
		return fmt.Errorf("replication connection is not open")
	}
	return withWriteDeadline(ctx, conn.Conn(), standbyWriteTimeout, func() error {
		return pglogrepl.SendStandbyStatusUpdate(ctx, conn, status)
	})
}

func withWriteDeadline(ctx context.Context, conn net.Conn, timeout time.Duration, send func() error) error {
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set standby status write deadline: %w", err)
	}
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.SetWriteDeadline(time.Now())
		close(cancelDone)
	})
	err := send()
	if !stopCancel() {
		<-cancelDone
	}
	_ = conn.SetWriteDeadline(time.Time{})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	return nil
}
