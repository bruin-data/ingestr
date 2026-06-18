package mssql_cdc

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mssql+cdc", "sqlserver+cdc", "azuresql+cdc", "azure-sql+cdc"},
		func() interface{} { return NewMSSQLCDCSource() },
	)
}
