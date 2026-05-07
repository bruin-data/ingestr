package mssql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"mssql", "sqlserver", "mssql+pyodbc"},
		func() interface{} { return NewMSSQLDestination() },
	)
}
