package mssql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mssql", "sqlserver", "mssql+pyodbc", "azuresql", "azure-sql"},
		func() interface{} { return NewMSSQLSource() },
	)
}
