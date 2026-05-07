package smartsheet

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const smartsheetBaseURL = "https://api.smartsheet.com/2.0"

type SmartsheetSource struct {
	client      *gonghttp.Client
	accessToken string
}

type sheetResponse struct {
	Name    string        `json:"name"`
	Columns []sheetColumn `json:"columns"`
	Rows    []sheetRow    `json:"rows"`
}

type sheetColumn struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type sheetRow struct {
	ID    int64       `json:"id"`
	Cells []sheetCell `json:"cells"`
}

type sheetCell struct {
	ColumnID int64       `json:"columnId"`
	Value    interface{} `json:"value"`
}

func NewSmartsheetSource() *SmartsheetSource {
	return &SmartsheetSource{}
}

func (s *SmartsheetSource) Schemes() []string {
	return []string{"smartsheet"}
}

func (s *SmartsheetSource) Connect(ctx context.Context, uri string) error {
	accessToken, err := parseSmartsheetURI(uri)
	if err != nil {
		return err
	}

	s.accessToken = accessToken

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(smartsheetBaseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithDebug(config.DebugMode),
	)

	config.Debug("[SMARTSHEET] Connected successfully")
	return nil
}

func parseSmartsheetURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "smartsheet://") {
		return "", fmt.Errorf("invalid smartsheet URI: must start with smartsheet://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse smartsheet URI: %w", err)
	}

	accessToken := parsed.Query().Get("access_token")
	if accessToken == "" {
		return "", fmt.Errorf("access_token is required to connect to Smartsheet")
	}

	return accessToken, nil
}

func (s *SmartsheetSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *SmartsheetSource) HandlesIncrementality() bool {
	return false
}

func (s *SmartsheetSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.IncrementalKey != "" {
		return nil, fmt.Errorf("incremental loads are not yet supported for Smartsheet")
	}

	sheetID := req.Name
	if sheetID == "" {
		return nil, fmt.Errorf("sheet ID is required as --source-table for smartsheet source")
	}

	return &source.DynamicSourceTable{
		TableName:           sheetID,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("smartsheet source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, sheetID, opts)
		},
	}, nil
}

func (s *SmartsheetSource) read(ctx context.Context, sheetID string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	sheetIDInt, err := strconv.ParseInt(sheetID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid sheet_id: %w", err)
	}

	// Fetch the full sheet from API
	path := fmt.Sprintf("/sheets/%d", sheetIDInt)
	var sheet sheetResponse
	resp, err := s.client.R(ctx).
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", s.accessToken)).
		SetResult(&sheet).
		Get(path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sheet data: %w", err)
	}
	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("API error: status %d - %s", resp.StatusCode(), resp.String())
	}

	// Build schema columns from sheet column types
	columns := []schema.Column{
		{Name: "_row_id", DataType: schema.TypeInt64, Nullable: false},
	}
	for _, col := range sheet.Columns {
		columns = append(columns, schema.Column{
			Name:     col.Title,
			DataType: mapColumnType(col.Type),
			Nullable: true,
		})
	}

	columnByID := make(map[int64]string, len(sheet.Columns))
	for _, col := range sheet.Columns {
		columnByID[col.ID] = col.Title
	}

	var items []map[string]interface{}
	for _, row := range sheet.Rows {
		rowData := map[string]interface{}{
			"_row_id": row.ID,
		}
		for _, cell := range row.Cells {
			if title, ok := columnByID[cell.ColumnID]; ok {
				rowData[title] = cell.Value
			}
		}
		items = append(items, rowData)
	}

	if opts.Limit > 0 && len(items) > opts.Limit {
		items = items[:opts.Limit]
	}

	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, columns, opts.ExcludeColumns)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}

		config.Debug("[SMARTSHEET] Sheet %s: %d rows read", sheetID, len(items))
	}()

	return results, nil
}

func mapColumnType(colType string) schema.DataType {
	switch colType {
	case "TEXT_NUMBER", "CONTACT_LIST", "MULTI_CONTACT_LIST",
		"PICKLIST", "MULTI_PICKLIST",
		"DURATION", "PREDECESSOR":
		return schema.TypeString
	case "DATE":
		return schema.TypeDate
	case "DATETIME", "ABSTRACT_DATETIME":
		return schema.TypeTimestamp
	case "CHECKBOX":
		return schema.TypeBoolean
	default:
		return schema.TypeString
	}
}

var _ source.Source = (*SmartsheetSource)(nil)
