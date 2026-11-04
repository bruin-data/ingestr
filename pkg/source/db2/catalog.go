package db2

import "fmt"

// db2ColumnsQuery returns the system-catalog query used to fetch column
// metadata for a table. DB2 for i exposes its SQL catalog under QSYS2
// instead of the SYSCAT schema LUW (and z/OS) use, with LENGTH and
// NUMERIC_SCALE already broken out the same way SYSCAT.COLUMNS does, so
// both branches return the same five-column shape that getSchema expects.
func db2ColumnsQuery(platform db2Platform, schemaName, tableName string) string {
	if platform == db2PlatformIBMi {
		return fmt.Sprintf(`
			SELECT
				COLUMN_NAME,
				DATA_TYPE,
				IS_NULLABLE,
				LENGTH,
				NUMERIC_SCALE
			FROM QSYS2.SYSCOLUMNS
			WHERE TABLE_SCHEMA = %s AND TABLE_NAME = %s
			ORDER BY ORDINAL_POSITION
		`, quoteLiteral(schemaName), quoteLiteral(tableName))
	}

	return fmt.Sprintf(`
		SELECT
			COLNAME,
			TYPENAME,
			NULLS,
			LENGTH,
			SCALE
		FROM SYSCAT.COLUMNS
		WHERE TABSCHEMA = %s AND TABNAME = %s
		ORDER BY COLNO
	`, quoteLiteral(schemaName), quoteLiteral(tableName))
}

// db2PrimaryKeysQuery returns the system-catalog query used to fetch
// primary key column names for a table. QSYS2.SYSCST/SYSKEYCST are IBM
// i's equivalent of SYSCAT.TABCONST/KEYCOLUSE.
func db2PrimaryKeysQuery(platform db2Platform, schemaName, tableName string) string {
	if platform == db2PlatformIBMi {
		return fmt.Sprintf(`
			SELECT k.COLUMN_NAME
			FROM QSYS2.SYSKEYCST k
			JOIN QSYS2.SYSCST c
				ON c.CONSTRAINT_NAME = k.CONSTRAINT_NAME
				AND c.TABLE_SCHEMA = k.TABLE_SCHEMA
				AND c.TABLE_NAME = k.TABLE_NAME
			WHERE c.CONSTRAINT_TYPE = 'PRIMARY KEY'
				AND k.TABLE_SCHEMA = %s
				AND k.TABLE_NAME = %s
			ORDER BY k.ORDINAL_POSITION
		`, quoteLiteral(schemaName), quoteLiteral(tableName))
	}

	return fmt.Sprintf(`
		SELECT k.COLNAME
		FROM SYSCAT.KEYCOLUSE k
		JOIN SYSCAT.TABCONST c
			ON c.CONSTNAME = k.CONSTNAME
			AND c.TABSCHEMA = k.TABSCHEMA
			AND c.TABNAME = k.TABNAME
		WHERE c.TYPE = 'P'
			AND k.TABSCHEMA = %s
			AND k.TABNAME = %s
		ORDER BY k.COLSEQ
	`, quoteLiteral(schemaName), quoteLiteral(tableName))
}
