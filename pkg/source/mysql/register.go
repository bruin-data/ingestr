package mysql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	// The dispatchers probe the server at connect time and route to the MySQL or
	// Vitess backend. Vitess speaks the MySQL wire protocol but needs VStream for
	// CDC, so the two implementations are kept fully separate behind one scheme.
	registry.RegisterSource(
		[]string{"mysql", "mysql+pymysql", "mariadb"},
		func() interface{} { return newMySQLSourceDispatcher() },
	)
	registry.RegisterSource(
		[]string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"},
		func() interface{} { return newMySQLCDCDispatcher() },
	)
}
