package mysql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"mysql", "mysql+pymysql", "mariadb"},
		func() interface{} { return NewMySQLDestination() },
	)
}
