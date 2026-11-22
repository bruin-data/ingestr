package db2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDb2ColumnsQueryUsesSyscatByDefault(t *testing.T) {
	query := db2ColumnsQuery(db2PlatformAuto, "ANALYTICS", "ORDERS")
	require.Contains(t, query, "SYSCAT.COLUMNS")
	require.Contains(t, query, "TABSCHEMA = 'ANALYTICS'")
	require.Contains(t, query, "TABNAME = 'ORDERS'")
}

func TestDb2ColumnsQueryUsesIBMiCatalog(t *testing.T) {
	query := db2ColumnsQuery(db2PlatformIBMi, "MYSCHEMA", "MYTABLE")
	require.Contains(t, query, "QSYS2.SYSCOLUMNS")
	require.Contains(t, query, "TABLE_SCHEMA = 'MYSCHEMA'")
	require.Contains(t, query, "TABLE_NAME = 'MYTABLE'")
	require.NotContains(t, query, "SYSCAT")
}

func TestDb2PrimaryKeysQueryUsesSyscatByDefault(t *testing.T) {
	query := db2PrimaryKeysQuery(db2PlatformLUW, "ANALYTICS", "ORDERS")
	require.Contains(t, query, "SYSCAT.KEYCOLUSE")
	require.Contains(t, query, "SYSCAT.TABCONST")
}

func TestDb2PrimaryKeysQueryUsesIBMiCatalog(t *testing.T) {
	query := db2PrimaryKeysQuery(db2PlatformIBMi, "MYSCHEMA", "MYTABLE")
	require.Contains(t, query, "QSYS2.SYSKEYCST")
	require.Contains(t, query, "QSYS2.SYSCST")
	require.Contains(t, query, "'PRIMARY KEY'")
	require.NotContains(t, query, "SYSCAT")
}
