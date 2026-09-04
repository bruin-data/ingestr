package postgres_cdc

import (
	"context"
	"time"
)

// Periodic checks also cover publications that stop sending all row events.
// Relation and logical-message checks catch changes before decoding new row
// shapes or acknowledging a batch barrier or stream heartbeat.
type publicationGuard struct {
	check    func(context.Context) error
	last     time.Time
	interval time.Duration
}

func newPublicationGuard(src *PostgresCDCSource) publicationGuard {
	guard := publicationGuard{interval: 10 * time.Second}
	if src.queryPool != nil {
		guard.check = src.validatePublicationShape
	}
	return guard
}

func (g *publicationGuard) validate(ctx context.Context, force bool) error {
	if g.check == nil || !force && time.Since(g.last) < g.interval {
		return nil
	}
	if err := g.check(ctx); err != nil {
		return err
	}
	g.last = time.Now()
	return nil
}

func publicationCheckRequired(data []byte) bool {
	return len(data) > 0 && (data[0] == msgTypeRelation || data[0] == 'M')
}
