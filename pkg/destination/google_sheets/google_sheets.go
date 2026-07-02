package google_sheets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const maxRowsPerRequest = 5000

type GoogleSheetsDestination struct {
	client        *sheets.Service
	mu            sync.Mutex
	schema        *schema.TableSchema
	spreadsheetID string
	sheetName     string
}

func NewGoogleSheetsDestination() *GoogleSheetsDestination {
	return &GoogleSheetsDestination{}
}

func (d *GoogleSheetsDestination) Schemes() []string {
	return []string{"gsheets"}
}

func (d *GoogleSheetsDestination) Connect(ctx context.Context, uri string) error {
	credsJSON, err := parseURI(uri)
	if err != nil {
		return err
	}

	opts := []option.ClientOption{
		option.WithScopes("https://www.googleapis.com/auth/spreadsheets"),
	}
	// Credentials are optional: when none are provided, the client falls back
	// to Application Default Credentials (e.g. the gcloud ADC file on the machine).
	if len(credsJSON) > 0 {
		credType, err := detectCredentialType(credsJSON)
		if err != nil {
			return fmt.Errorf("failed to detect credential type: %w", err)
		}
		opts = append(opts, option.WithAuthCredentialsJSON(credType, credsJSON))
	}

	client, err := sheets.NewService(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create Google Sheets service: %w", err)
	}

	d.client = client

	config.Debug("[GSHEETS] Connected successfully")
	return nil
}

func (d *GoogleSheetsDestination) Close(ctx context.Context) error {
	return nil
}

func (d *GoogleSheetsDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	spreadsheetID, sheetName, err := parseTableName(opts.Table)
	if err != nil {
		return err
	}
	d.spreadsheetID = spreadsheetID
	d.sheetName = sheetName
	d.schema = opts.Schema

	if err := d.ensureSheetExists(ctx); err != nil {
		return err
	}

	if opts.DropFirst {
		if err := d.clearSheet(ctx); err != nil {
			return err
		}
		return d.writeHeader(ctx)
	}

	// Append mode: only write the header row when the sheet has no data yet.
	empty, err := d.sheetIsEmpty(ctx)
	if err != nil {
		return err
	}
	if empty {
		return d.writeHeader(ctx)
	}
	return nil
}

func (d *GoogleSheetsDestination) ensureSheetExists(ctx context.Context) error {
	spreadsheet, err := d.client.Spreadsheets.Get(d.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to fetch spreadsheet %s: %w", d.spreadsheetID, err)
	}

	for _, s := range spreadsheet.Sheets {
		if s.Properties != nil && s.Properties.Title == d.sheetName {
			return nil
		}
	}

	config.Debug("[GSHEETS] creating sheet %q in spreadsheet %s", d.sheetName, d.spreadsheetID)
	_, err = d.client.Spreadsheets.BatchUpdate(d.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				AddSheet: &sheets.AddSheetRequest{
					Properties: &sheets.SheetProperties{Title: d.sheetName},
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to create sheet %q: %w", d.sheetName, err)
	}
	return nil
}

func (d *GoogleSheetsDestination) clearSheet(ctx context.Context) error {
	_, err := d.client.Spreadsheets.Values.Clear(d.spreadsheetID, quoteSheetTitle(d.sheetName), &sheets.ClearValuesRequest{}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to clear sheet %q: %w", d.sheetName, err)
	}
	return nil
}

func (d *GoogleSheetsDestination) sheetIsEmpty(ctx context.Context) (bool, error) {
	resp, err := d.client.Spreadsheets.Values.Get(d.spreadsheetID, quoteSheetTitle(d.sheetName)).Context(ctx).Do()
	if err != nil {
		return false, fmt.Errorf("failed to read sheet %q: %w", d.sheetName, err)
	}
	return len(resp.Values) == 0, nil
}

func (d *GoogleSheetsDestination) writeHeader(ctx context.Context) error {
	if d.schema == nil {
		return nil
	}
	headers := d.schema.ColumnNames()
	if len(headers) == 0 {
		return nil
	}

	row := make([]interface{}, len(headers))
	for i, h := range headers {
		row[i] = h
	}

	_, err := d.client.Spreadsheets.Values.Update(d.spreadsheetID, quoteSheetTitle(d.sheetName)+"!A1", &sheets.ValueRange{
		Values: [][]interface{}{row},
	}).ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to write header to sheet %q: %w", d.sheetName, err)
	}
	return nil
}

func (d *GoogleSheetsDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[GSHEETS] Starting write to %s.%s", d.spreadsheetID, d.sheetName)

	for result := range records {
		if result.Err != nil {
			return result.Err
		}

		batchNum++
		startBatch := time.Now()

		rows, err := d.writeRecordBatch(ctx, result.Batch)
		if err != nil {
			result.Batch.Release()
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		config.Debug("[GSHEETS] Batch %d: %d rows in %v (total: %d)", batchNum, rows, time.Since(startBatch), totalRows)

		result.Batch.Release()
	}

	config.Debug("[GSHEETS] Total: %d rows written in %v", totalRows, time.Since(startTime))
	return nil
}

// WriteParallel writes sequentially: the Sheets append API appends after the
// last populated row, so concurrent appends to the same sheet would race.
func (d *GoogleSheetsDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.Write(ctx, records, opts)
}

func (d *GoogleSheetsDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	numRows := int(record.NumRows())
	numCols := int(record.NumCols())
	if numRows == 0 {
		return 0, nil
	}

	values := make([][]interface{}, numRows)
	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		row := make([]interface{}, numCols)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			row[colIdx] = valueToCell(record.Column(colIdx), rowIdx)
		}
		values[rowIdx] = row
	}

	for start := 0; start < numRows; start += maxRowsPerRequest {
		end := start + maxRowsPerRequest
		if end > numRows {
			end = numRows
		}
		_, err := d.client.Spreadsheets.Values.Append(d.spreadsheetID, quoteSheetTitle(d.sheetName), &sheets.ValueRange{
			Values: values[start:end],
		}).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
		if err != nil {
			return int64(start), fmt.Errorf("failed to append rows to sheet %q: %w", d.sheetName, err)
		}
	}

	return int64(numRows), nil
}

func (d *GoogleSheetsDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	// Data is written directly to the target sheet; there is no staging to swap.
	config.Debug("[GSHEETS] SwapTable called (no-op for Google Sheets)")
	return nil
}

func (d *GoogleSheetsDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (d *GoogleSheetsDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return &gsheetsTransaction{}, nil
}

type gsheetsTransaction struct{}

func (t *gsheetsTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (t *gsheetsTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *gsheetsTransaction) Rollback(ctx context.Context) error {
	return nil
}

func (d *GoogleSheetsDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return fmt.Errorf("merge strategy is not supported for Google Sheets destination")
}

func (d *GoogleSheetsDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return fmt.Errorf("delete+insert strategy is not supported for Google Sheets destination")
}

func (d *GoogleSheetsDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("scd2 strategy is not supported for Google Sheets destination")
}

func (d *GoogleSheetsDestination) DropTable(ctx context.Context, table string) error {
	spreadsheetID, sheetName, err := parseTableName(table)
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	spreadsheet, err := d.client.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to fetch spreadsheet %s: %w", spreadsheetID, err)
	}

	sheetID := int64(-1)
	for _, s := range spreadsheet.Sheets {
		if s.Properties != nil && s.Properties.Title == sheetName {
			sheetID = s.Properties.SheetId
			break
		}
	}
	if sheetID < 0 {
		// Sheet (tab) does not exist; nothing to drop.
		return nil
	}

	// The Sheets API refuses to delete the last remaining sheet in a
	// spreadsheet, so in that case clear its values instead.
	if len(spreadsheet.Sheets) <= 1 {
		_, err = d.client.Spreadsheets.Values.Clear(spreadsheetID, quoteSheetTitle(sheetName), &sheets.ClearValuesRequest{}).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to clear sheet %q: %w", sheetName, err)
		}
		return nil
	}

	_, err = d.client.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				DeleteSheet: &sheets.DeleteSheetRequest{
					SheetId:         sheetID,
					ForceSendFields: []string{"SheetId"},
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to delete sheet %q: %w", sheetName, err)
	}
	return nil
}

func (d *GoogleSheetsDestination) SupportsReplaceStrategy() bool { return true }

func (d *GoogleSheetsDestination) SupportsAppendStrategy() bool { return true }

func (d *GoogleSheetsDestination) SupportsMergeStrategy() bool { return false }

func (d *GoogleSheetsDestination) SupportsDeleteInsertStrategy() bool { return false }

func (d *GoogleSheetsDestination) SupportsSCD2Strategy() bool { return false }

func (d *GoogleSheetsDestination) SupportsAtomicSwap() bool { return false }

func (d *GoogleSheetsDestination) GetScheme() string { return "gsheets" }

func (d *GoogleSheetsDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func parseTableName(table string) (string, string, error) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid table name %q: expected format spreadsheet_id.sheet_name (e.g. fkdUQ2bjdNfUq2CA.Sheet1)", table)
	}
	return parts[0], parts[1], nil
}

func quoteSheetTitle(title string) string {
	return "'" + strings.ReplaceAll(title, "'", "''") + "'"
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

	// No credentials provided: fall back to Application Default Credentials.
	return nil, nil
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

func valueToCell(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return ""
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return a.Value(idx)
	case *array.Uint16:
		return a.Value(idx)
	case *array.Uint32:
		return a.Value(idx)
	case *array.Uint64:
		return a.Value(idx)
	case *array.Float32:
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.Decimal128:
		return a.Value(idx).ToFloat64(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return string(a.Value(idx))
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time32:
		return a.Value(idx).ToTime(a.DataType().(*arrow.Time32Type).Unit).Format("15:04:05.999999")
	case *array.Time64:
		return a.Value(idx).ToTime(a.DataType().(*arrow.Time64Type).Unit).Format("15:04:05.999999")
	case *array.Timestamp:
		return a.Value(idx).ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.999999")
	case array.ExtensionArray:
		return valueToCell(a.Storage(), idx)
	default:
		return fmt.Sprintf("%v", arr.GetOneForMarshal(idx))
	}
}

var _ destination.Destination = (*GoogleSheetsDestination)(nil)
