package mysql

import (
	"context"

	"github.com/bruin-data/ingestr/internal/config"
)

// VitessSource reads from a Vitess keyspace over vtgate's MySQL protocol. It is
// identical to MySQLSource except that it runs in the OLAP workload, which lifts
// the per-query row-count cap Vitess enforces in its default OLTP workload. The
// dispatcher selects this backend when it detects a Vitess server.
type VitessSource struct {
	*MySQLSource
}

func NewVitessSource() *VitessSource {
	return &VitessSource{MySQLSource: NewMySQLSource()}
}

func (s *VitessSource) Connect(ctx context.Context, uri string) error {
	if err := s.MySQLSource.Connect(ctx, uri); err != nil {
		return err
	}
	config.Debug("[SOURCE] Vitess source: enabling OLAP workload")
	s.sessionInit = []string{"SET workload = 'OLAP'"}
	return nil
}
