package mysql

// VitessSource reads from a Vitess keyspace over vtgate's MySQL protocol. It is
// identical to MySQLSource except that it runs in the OLAP workload, which lifts
// the per-query row-count cap Vitess enforces in its default OLTP workload. It
// serves both the vitess:// and planetscale:// schemes (PlanetScale is managed
// Vitess); TLS for PlanetScale is enabled in uriToDSN.
type VitessSource struct {
	*MySQLSource
}

func NewVitessSource() *VitessSource {
	src := NewMySQLSource()
	src.vitessBackend = true
	return &VitessSource{MySQLSource: src}
}

func (s *VitessSource) Schemes() []string {
	return []string{"vitess", "planetscale"}
}
