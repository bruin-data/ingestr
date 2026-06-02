package cassandrautil

import (
	"fmt"
	"strings"
)

func ResolveTableName(defaultKeyspace, table string) (string, string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return "", "", fmt.Errorf("table name is required")
	}

	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		keyspace := normalizeIdentifierSegment(parts[0])
		tableName := normalizeIdentifierSegment(parts[1])
		if keyspace == "" || tableName == "" {
			return "", "", fmt.Errorf("invalid Cassandra table name %q", table)
		}
		return keyspace, tableName, nil
	}

	keyspace := normalizeIdentifierSegment(defaultKeyspace)
	tableName := normalizeIdentifierSegment(table)
	if keyspace == "" {
		return "", "", fmt.Errorf("cassandra keyspace is required: include it in the URI path or use keyspace.table")
	}
	if tableName == "" {
		return "", "", fmt.Errorf("invalid Cassandra table name %q", table)
	}
	return keyspace, tableName, nil
}

func QuoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func QuoteTable(defaultKeyspace, table string) (string, error) {
	keyspace, tableName, err := ResolveTableName(defaultKeyspace, table)
	if err != nil {
		return "", err
	}
	return QuoteIdentifier(keyspace) + "." + QuoteIdentifier(tableName), nil
}

func normalizeIdentifierSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if len(segment) >= 2 && strings.HasPrefix(segment, `"`) && strings.HasSuffix(segment, `"`) {
		return strings.ReplaceAll(segment[1:len(segment)-1], `""`, `"`)
	}
	return strings.ToLower(segment)
}
