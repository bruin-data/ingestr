package athena

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
)

func (d *AthenaDestination) queryAll(ctx context.Context, query, database string) ([]string, [][]string, error) {
	execID, err := d.startQuery(ctx, query, database)
	if err != nil {
		return nil, nil, err
	}
	if err := d.waitForQuery(ctx, execID); err != nil {
		return nil, nil, err
	}

	var (
		nextToken  *string
		colNames   []string
		allRows    [][]string
		skipHeader = true
	)

	for {
		out, err := d.client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(execID),
			NextToken:        nextToken,
			MaxResults:       aws.Int32(1000),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get query results: %w", err)
		}

		if out.ResultSet != nil && out.ResultSet.ResultSetMetadata != nil && len(colNames) == 0 {
			for _, c := range out.ResultSet.ResultSetMetadata.ColumnInfo {
				if c.Name != nil {
					colNames = append(colNames, *c.Name)
				}
			}
		}

		if out.ResultSet != nil {
			for i, row := range out.ResultSet.Rows {
				if skipHeader && i == 0 {
					continue
				}
				values := make([]string, len(row.Data))
				for j, d := range row.Data {
					if d.VarCharValue != nil {
						values[j] = *d.VarCharValue
					}
				}
				allRows = append(allRows, values)
			}
		}

		skipHeader = false
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}

	return colNames, allRows, nil
}

func (d *AthenaDestination) ensureIcebergTable(ctx context.Context, database, table string) error {
	qualified, err := formatQualifiedTableHive(database, table)
	if err != nil {
		return err
	}
	q := fmt.Sprintf("SHOW CREATE TABLE %s", qualified)
	_, rows, err := d.queryAll(ctx, q, database)
	if err != nil {
		if isAthenaTableNotFound(err) {
			return fmt.Errorf("%w: %s.%s", errTableNotFound, database, table)
		}
		return err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return fmt.Errorf("failed to read SHOW CREATE TABLE output for %s.%s", database, table)
	}

	var lines []string
	for _, r := range rows {
		if len(r) > 0 && r[0] != "" {
			lines = append(lines, r[0])
		}
	}
	stmt := strings.ToLower(strings.Join(lines, "\n"))
	// Prefer a strong signal: the Iceberg table_type property in the DDL.
	if !strings.Contains(stmt, "table_type") || !strings.Contains(stmt, "iceberg") {
		return fmt.Errorf("athena destination requires Iceberg tables only: %s.%s is not an Iceberg table", database, table)
	}
	return nil
}

func isAthenaTableNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "table_not_found") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "unknown table") ||
		strings.Contains(s, "hive_metastoreerror") && strings.Contains(s, "not found")
}
