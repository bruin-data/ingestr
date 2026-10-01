package mssql

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	mssqldb "github.com/microsoft/go-mssqldb"
)

const (
	errIndexEntryTooLong     = 1946
	errBulkCopyColumnTooLong = 4815
)

var (
	bulkCopyColumnIDPattern = regexp.MustCompile(`colid (\d+)`)
	indexEntryLimitPattern  = regexp.MustCompile(`maximum length of (\d+) bytes`)
	indexNamePattern        = regexp.MustCompile(`for the index '([^']+)'`)
)

// explainWriteError prefixes SQL Server errors whose raw text does not say
// which value is too long. columns maps bulk-copy colids to names when known.
func explainWriteError(err error, columns []string) error {
	var sqlErr mssqldb.Error
	if err == nil || !errors.As(err, &sqlErr) {
		return err
	}
	for _, item := range append([]mssqldb.Error{sqlErr}, sqlErr.All...) {
		switch item.Number {
		case errIndexEntryTooLong:
			limit := indexEntryLimitBytes(item.Message)
			index := ""
			if match := indexNamePattern.FindStringSubmatch(item.Message); match != nil {
				index = fmt.Sprintf(" for index %q", match[1])
			}
			return fmt.Errorf("key value is too long%s: SQL Server index keys hold at most %d bytes (%d NVARCHAR characters), regardless of the declared column length: %w", index, limit, limit/2, err)
		case errBulkCopyColumnTooLong:
			if column := bulkCopyColumnName(item.Message, columns); column != "" {
				return fmt.Errorf("a value in column %q is longer than the destination column allows: %w", column, err)
			}
			return fmt.Errorf("a value is longer than its destination column allows: %w", err)
		}
	}
	return err
}

func bulkCopyColumnName(message string, columns []string) string {
	match := bulkCopyColumnIDPattern.FindStringSubmatch(message)
	if match == nil {
		return ""
	}
	id, err := strconv.Atoi(match[1])
	if err != nil || id < 1 || id > len(columns) {
		return ""
	}
	return columns[id-1]
}

func indexEntryLimitBytes(message string) int {
	if match := indexEntryLimitPattern.FindStringSubmatch(message); match != nil {
		if limit, err := strconv.Atoi(match[1]); err == nil && limit > 0 {
			return limit
		}
	}
	return 2 * maxPrimaryKeyStringLength
}
