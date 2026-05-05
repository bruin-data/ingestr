package mysql

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mysql", "mysql+pymysql", "mariadb"},
		func() interface{} { return NewMySQLSource() },
	)
}
