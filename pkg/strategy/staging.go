package strategy

import (
	"fmt"
	"strings"
	"time"
)

func GenerateStagingTableName(targetTable, suffix, stagingDataset string) string {
	parts := strings.SplitN(targetTable, ".", 2)
	tableName := targetTable
	schemaPrefix := ""

	if len(parts) == 2 {
		schemaPrefix = parts[0]
		tableName = parts[1]
	}

	if stagingDataset != "" {
		schemaPrefix = stagingDataset
	}

	if schemaPrefix != "" {
		return fmt.Sprintf("%s.%s_%s_%d", schemaPrefix, tableName, suffix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s_%d", tableName, suffix, time.Now().UnixNano())
}
