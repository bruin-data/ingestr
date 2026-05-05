package google_sheets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type GoogleSheetsSource struct {
	client *sheets.Service
}

func NewGoogleSheetsSource() *GoogleSheetsSource {
	return &GoogleSheetsSource{}
}

func (s *GoogleSheetsSource) Schemes() []string {
	return []string{"gsheets"}
}

func (s *GoogleSheetsSource) HandlesIncrementality() bool {
	return false
}

func (s *GoogleSheetsSource) Connect(ctx context.Context, uri string) error {
	credsJSON, err := parseURI(uri)
	if err != nil {
		return err
	}

	credType, err := detectCredentialType(credsJSON)
	if err != nil {
		return fmt.Errorf("failed to detect credential type: %w", err)
	}

	client, err := sheets.NewService(
		ctx,
		option.WithAuthCredentialsJSON(credType, credsJSON),
		option.WithScopes("https://www.googleapis.com/auth/spreadsheets.readonly"),
	)
	if err != nil {
		return fmt.Errorf("failed to create Google Sheets service: %w", err)
	}

	s.client = client

	config.Debug("[GSHEETS] Connected successfully")
	return nil
}

func detectCredentialType(credsJSON []byte) (option.CredentialsType, error) {
	var raw struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(credsJSON, &raw); err != nil {
		return "", fmt.Errorf("invalid credentials JSON: %w", err)
	}

	switch raw.Type {
	case "service_account":
		return option.ServiceAccount, nil
	case "authorized_user":
		return option.AuthorizedUser, nil
	default:
		return "", fmt.Errorf("unsupported credential type %q: expected service_account or authorized_user", raw.Type)
	}
}

func parseURI(uri string) ([]byte, error) {
	if !strings.HasPrefix(uri, "gsheets://") {
		return nil, fmt.Errorf("invalid gsheets URI: must start with gsheets://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gsheets URI: %w", err)
	}

	query := parsed.Query()

	if credsPath := query.Get("credentials_path"); credsPath != "" {
		data, err := os.ReadFile(credsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read credentials file %s: %w", credsPath, err)
		}
		return data, nil
	}

	if credsB64 := query.Get("credentials_base64"); credsB64 != "" {
		data, err := base64.StdEncoding.DecodeString(credsB64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode credentials_base64: %w", err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("gsheets URI requires credentials_path or credentials_base64 parameter")
}

func (s *GoogleSheetsSource) Close(ctx context.Context) error {
	return nil
}

func (s *GoogleSheetsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.IncrementalKey != "" {
		return nil, fmt.Errorf("incremental loads are not supported for Google Sheets")
	}

	tableName := req.Name
	spreadsheetID, sheetName, err := parseTableName(tableName)
	if err != nil {
		return nil, err
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("google sheets source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, spreadsheetID, sheetName, opts)
		},
	}, nil
}

func parseTableName(table string) (string, string, error) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid table name %q: expected format spreadsheet_id.sheet_name (e.g. fkdUQ2bjdNfUq2CA.Sheet1)", table)
	}
	return parts[0], parts[1], nil
}

func (s *GoogleSheetsSource) read(ctx context.Context, spreadsheetID, sheetName string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)

		err := s.readSheet(ctx, spreadsheetID, sheetName, opts, results)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *GoogleSheetsSource) readSheet(ctx context.Context, spreadsheetID, sheetName string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GSHEETS] reading %s.%s", spreadsheetID, sheetName)

	resp, err := s.client.Spreadsheets.Values.Get(spreadsheetID, sheetName).
		ValueRenderOption("UNFORMATTED_VALUE").
		DateTimeRenderOption("SERIAL_NUMBER").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to fetch sheet data: %w", err)
	}

	if len(resp.Values) == 0 {
		config.Debug("[GSHEETS] sheet %s.%s is empty", spreadsheetID, sheetName)
		return nil
	}

	headers := make([]string, len(resp.Values[0]))
	seen := make(map[string]int, len(resp.Values[0]))
	for i, h := range resp.Values[0] {
		str := fmt.Sprintf("%v", h)
		if h == nil || str == "" {
			str = fmt.Sprintf("column_%d", i)
		}
		if count, exists := seen[str]; exists {
			seen[str] = count + 1
			renamed := fmt.Sprintf("%s_%d", str, count+1)
			fmt.Printf("Warning: duplicate header %q found in sheet %s, renaming to %q\n", str, sheetName, renamed)
			str = renamed
		} else {
			seen[str] = 1
		}
		seen[str] = 1
		headers[i] = str
	}

	rows := resp.Values[1:]
	items := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		item := make(map[string]interface{}, len(headers))
		for i, header := range headers {
			if i < len(row) {
				item[header] = row[i]
			} else {
				item[header] = nil
			}
		}
		items = append(items, item)
	}

	if opts.Limit > 0 && len(items) > opts.Limit {
		items = items[:opts.Limit]
	}

	if len(items) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}

	config.Debug("[GSHEETS] sheet %s.%s: %d rows read", spreadsheetID, sheetName, len(items))
	return nil
}

var _ source.Source = (*GoogleSheetsSource)(nil)
