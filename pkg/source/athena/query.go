package athena

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

func filterColumns(columns []schema.Column, excludeColumns []string) []schema.Column {
	if len(excludeColumns) == 0 {
		return columns
	}

	exclude := make(map[string]bool, len(excludeColumns))
	for _, c := range excludeColumns {
		exclude[strings.ToLower(c)] = true
	}

	filtered := make([]schema.Column, 0, len(columns))
	for _, col := range columns {
		if !exclude[strings.ToLower(col.Name)] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func buildArrowSchema(columns []schema.Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     schema.DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quoteQualifiedTable(database, table string) string {
	return quoteIdent(database) + "." + quoteIdent(table)
}

func buildSelectQuery(database, table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteIdent(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteQualifiedTable(database, table))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= '%s'", quoteIdent(opts.IncrementalKey), opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= '%s'", quoteIdent(opts.IncrementalKey), opts.IntervalEnd.Format("2006-01-02 15:04:05")))
		}
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	return query
}

func (s *AthenaSource) queryAll(ctx context.Context, query, database string) ([]string, [][]string, error) {
	execID, err := s.startQuery(ctx, query, database)
	if err != nil {
		return nil, nil, err
	}
	if err := s.waitForQuery(ctx, execID); err != nil {
		return nil, nil, err
	}

	var (
		nextToken  *string
		colNames   []string
		allRows    [][]string
		skipHeader = true
	)

	for {
		out, err := s.client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
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

func (s *AthenaSource) streamQuery(
	ctx context.Context,
	query string,
	database string,
	arrowSchema *arrow.Schema,
	columns []schema.Column,
	batchSize int,
	out chan<- source.RecordBatchResult,
) error {
	execID, err := s.startQuery(ctx, query, database)
	if err != nil {
		return err
	}
	if err := s.waitForQuery(ctx, execID); err != nil {
		return err
	}
	return s.streamQueryFromExecID(ctx, execID, arrowSchema, columns, batchSize, out)
}

func (s *AthenaSource) streamQueryFromExecID(
	ctx context.Context,
	execID string,
	arrowSchema *arrow.Schema,
	columns []schema.Column,
	batchSize int,
	out chan<- source.RecordBatchResult,
) error {
	mem := memory.NewGoAllocator()

	newBuilders := func() []array.Builder {
		builders := make([]array.Builder, len(columns))
		for i, field := range arrowSchema.Fields() {
			builders[i] = array.NewBuilder(mem, field.Type)
		}
		return builders
	}

	flush := func(builders []array.Builder, rowCount int64) {
		if rowCount == 0 {
			for _, b := range builders {
				b.Release()
			}
			return
		}

		arrays := make([]arrow.Array, len(builders))
		for i, b := range builders {
			arrays[i] = b.NewArray()
		}
		record := array.NewRecordBatch(arrowSchema, arrays, rowCount)
		for _, arr := range arrays {
			arr.Release()
		}
		for _, b := range builders {
			b.Release()
		}
		out <- source.RecordBatchResult{Batch: record}
	}

	var (
		nextToken  *string
		skipHeader = true
		builders   = newBuilders()
		rowCount   int64
	)

	for {
		resp, err := s.client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(execID),
			NextToken:        nextToken,
			MaxResults:       aws.Int32(1000),
		})
		if err != nil {
			for _, b := range builders {
				b.Release()
			}
			return fmt.Errorf("failed to get query results: %w", err)
		}

		if resp.ResultSet != nil {
			for i, row := range resp.ResultSet.Rows {
				if skipHeader && i == 0 {
					continue
				}
				if len(row.Data) < len(columns) {
					// pad short rows
					padded := make([]types.Datum, len(columns))
					copy(padded, row.Data)
					row.Data = padded
				}

				for colIdx := range columns {
					var strPtr *string
					if colIdx < len(row.Data) {
						d := row.Data[colIdx]
						strPtr = d.VarCharValue
					}

					if err := appendFromString(builders[colIdx], strPtr, columns[colIdx]); err != nil {
						for _, b := range builders {
							b.Release()
						}
						return fmt.Errorf("failed to append value for column %s: %w", columns[colIdx].Name, err)
					}
				}
				rowCount++

				if batchSize > 0 && rowCount >= int64(batchSize) {
					flush(builders, rowCount)
					builders = newBuilders()
					rowCount = 0
				}
			}
		}

		skipHeader = false
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		nextToken = resp.NextToken
	}

	flush(builders, rowCount)
	return nil
}

func appendFromString(builder array.Builder, val *string, col schema.Column) error {
	if val == nil {
		builder.AppendNull()
		return nil
	}

	s := strings.TrimSpace(*val)
	if s == "" || strings.EqualFold(s, "null") {
		builder.AppendNull()
		return nil
	}

	switch b := builder.(type) {
	case *array.BooleanBuilder:
		switch strings.ToLower(s) {
		case "true", "1":
			b.Append(true)
		case "false", "0":
			b.Append(false)
		default:
			b.AppendNull()
		}
	case *array.Int16Builder:
		i, err := strconv.ParseInt(s, 10, 16)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(int16(i))
		}
	case *array.Int32Builder:
		i, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(int32(i))
		}
	case *array.Int64Builder:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(i)
		}
	case *array.Float32Builder:
		f, err := strconv.ParseFloat(s, 32)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(float32(f))
		}
	case *array.Float64Builder:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(f)
		}
	case *array.StringBuilder:
		b.Append(s)
	case *array.BinaryBuilder:
		if strings.HasPrefix(s, "0x") {
			raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(raw)
			}
		} else {
			b.Append([]byte(s))
		}
	case *array.Date32Builder:
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(arrow.Date32FromTime(t))
		}
	case *array.Time64Builder:
		t, err := parseTimeOfDay(s)
		if err != nil {
			b.AppendNull()
		} else {
			micros := int64(t.Hour())*3600000000 + int64(t.Minute())*60000000 + int64(t.Second())*1000000 + int64(t.Nanosecond())/1000
			b.Append(arrow.Time64(micros))
		}
	case *array.TimestampBuilder:
		t, err := parseTimestamp(s)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(arrow.Timestamp(t.UnixMicro()))
		}
	case *array.Decimal128Builder:
		p := int32(col.Precision)
		if p == 0 {
			p = 38
		}
		dec, err := decimal128.FromString(s, p, int32(col.Scale))
		if err != nil {
			// best-effort: scale manually for high precision values
			bf := new(big.Float)
			if _, ok := bf.SetString(s); ok {
				scale := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(col.Scale)), nil))
				bf.Mul(bf, scale)
				bi, _ := bf.Int(nil)
				b.Append(decimal128.FromBigInt(bi))
			} else {
				b.AppendNull()
			}
		} else {
			b.Append(dec)
		}
	case *array.ListBuilder:
		elems, err := parseArrayLiteral(s)
		if err != nil {
			b.AppendNull()
			return nil
		}
		b.Append(true)
		vb := b.ValueBuilder()
		elemCol := schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale}
		for _, e := range elems {
			ev := e
			if err := appendFromString(vb, &ev, elemCol); err != nil {
				return err
			}
		}
	default:
		builder.AppendNull()
	}

	return nil
}

func parseTimeOfDay(s string) (time.Time, error) {
	layouts := []string{
		"15:04:05.999999999",
		"15:04:05.999999",
		"15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid time")
}

func parseTimestamp(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid timestamp")
}

func parseArrayLiteral(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "null") {
		return nil, errors.New("null array")
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}, nil
	}

	var (
		out      []string
		current  strings.Builder
		inQuotes bool
		escaped  bool
	)

	flush := func() {
		tok := strings.TrimSpace(current.String())
		current.Reset()
		if len(tok) >= 2 && tok[0] == '"' && tok[len(tok)-1] == '"' {
			tok = tok[1 : len(tok)-1]
		}
		out = append(out, tok)
	}

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuotes = !inQuotes
			current.WriteRune(r)
			continue
		}
		if r == ',' && !inQuotes {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return out, nil
}
