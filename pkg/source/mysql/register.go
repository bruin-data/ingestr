package mysql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mysql", "mysql+pymysql", "mariadb"},
		func() interface{} { return NewMySQLSource() },
	)
	registry.RegisterSource(
		[]string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"},
		func() interface{} { return NewMySQLCDCSource() },
	)
}
