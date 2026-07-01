package mysql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"mysql", "mysql+pymysql", "mariadb", "vitess", "planetscale"},
		func() interface{} { return NewMySQLDestination() },
	)
}
