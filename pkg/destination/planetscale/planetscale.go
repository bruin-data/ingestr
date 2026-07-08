package planetscale

import "github.com/bruin-data/ingestr/pkg/destination/mysql"

type Destination struct {
	*mysql.MySQLDestination
}

func NewDestination() *Destination {
	return &Destination{
		MySQLDestination: mysql.NewVitessCompatibleDestination("ps_mysql"),
	}
}

func (d *Destination) Schemes() []string {
	return []string{"ps_mysql"}
}
