package mssql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mssql", "sqlserver", "mssql+pyodbc"},
		func() interface{} { return NewMSSQLSource() },
	)
}
