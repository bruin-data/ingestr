package vitess

import "github.com/bruin-data/ingestr/pkg/destination/mysql"

type Destination struct {
	*mysql.MySQLDestination
}

func NewDestination() *Destination {
	return &Destination{
		MySQLDestination: mysql.NewVitessCompatibleDestination("vitess"),
	}
}

func (d *Destination) Schemes() []string {
	return []string{"vitess"}
}
