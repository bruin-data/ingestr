package mssql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mssql", "sqlserver", "mssql+pyodbc", "azuresql", "azure-sql"},
		func() interface{} { return NewMSSQLSource() },
	)
	registry.RegisterSource(
		[]string{"mssql+ct", "sqlserver+ct", "azuresql+ct", "azure-sql+ct"},
		func() interface{} { return NewMSSQLChangeTrackingSource() },
	)
}
