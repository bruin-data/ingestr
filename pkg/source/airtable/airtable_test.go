package airtable

import (
	"encoding/json"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantToken  string
		wantBaseID string
		wantErr    bool
	}{
		{
			name:      "valid URI",
			uri:       "airtable://?access_token=patXXXXXXXXXXXXXX",
			wantToken: "patXXXXXXXXXXXXXX",
		},
		{
			name:    "missing access_token",
			uri:     "airtable://",
			wantErr: true,
		},
		{
			name:    "empty access_token",
			uri:     "airtable://?access_token=",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://?access_token=abc",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "",
			wantErr: true,
		},
		{
			name:      "access_token with extra params",
			uri:       "airtable://?access_token=patABC123&extra=ignored",
			wantToken: "patABC123",
		},
		{
			name:       "with base_id",
			uri:        "airtable://?access_token=patABC123&base_id=appXYZ",
			wantToken:  "patABC123",
			wantBaseID: "appXYZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToken, gotBaseID, err := parseURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotToken != tt.wantToken {
				t.Errorf("parseURI() token = %v, want %v", gotToken, tt.wantToken)
			}
			if gotBaseID != tt.wantBaseID {
				t.Errorf("parseURI() baseID = %v, want %v", gotBaseID, tt.wantBaseID)
			}
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		defaultBaseID string
		wantBase      string
		wantTable     string
		wantErr       bool
	}{
		{
			name:      "valid base_id/table_name",
			input:     "appABC123/tblXYZ456",
			wantBase:  "appABC123",
			wantTable: "tblXYZ456",
		},
		{
			name:      "valid with table name",
			input:     "appABC123/My Table",
			wantBase:  "appABC123",
			wantTable: "My Table",
		},
		{
			name:    "missing table and no default base_id",
			input:   "appABC123",
			wantErr: true,
		},
		{
			name:          "table only with default base_id from URI",
			input:         "tblXYZ456",
			defaultBaseID: "appABC123",
			wantBase:      "appABC123",
			wantTable:     "tblXYZ456",
		},
		{
			name:          "table-form precedence: table base_id wins over URI base_id",
			input:         "appFromTable/tblXYZ",
			defaultBaseID: "appFromURI",
			wantBase:      "appFromTable",
			wantTable:     "tblXYZ",
		},
		{
			name:    "empty base",
			input:   "/tblXYZ",
			wantErr: true,
		},
		{
			name:    "empty table",
			input:   "appABC123/",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:      "table with slash in name",
			input:     "appABC/some/path",
			wantBase:  "appABC",
			wantTable: "some/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := parseTableName(tt.input, tt.defaultBaseID)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTableName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if ref.baseID != tt.wantBase {
				t.Errorf("baseID = %v, want %v", ref.baseID, tt.wantBase)
			}
			if ref.tableName != tt.wantTable {
				t.Errorf("tableName = %v, want %v", ref.tableName, tt.wantTable)
			}
		})
	}
}

func TestFlattenRecords(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"id":"rec123","createdTime":"2024-01-01T00:00:00.000Z","fields":{"Name":"Alice","Age":30}}`),
		json.RawMessage(`{"id":"rec456","createdTime":"2024-01-02T00:00:00.000Z","fields":{"Name":"Bob","Email":"bob@example.com"}}`),
	}

	items, err := flattenRecords(raw)
	if err != nil {
		t.Fatalf("flattenRecords() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0]["id"] != "rec123" {
		t.Errorf("expected id=rec123, got %v", items[0]["id"])
	}
	if items[0]["createdTime"] != "2024-01-01T00:00:00.000Z" {
		t.Errorf("expected createdTime, got %v", items[0]["createdTime"])
	}
	if items[0]["fields__Name"] != "Alice" {
		t.Errorf("expected fields__Name=Alice, got %v", items[0]["fields__Name"])
	}
	if items[1]["fields__Email"] != "bob@example.com" {
		t.Errorf("expected fields__Email=bob@example.com, got %v", items[1]["fields__Email"])
	}
}

func TestFlattenRecordsPreservesCase(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"id":"rec1","fields":{"CamelCase":"val","UPPER":"val2","lower":"val3"}}`),
	}

	items, err := flattenRecords(raw)
	if err != nil {
		t.Fatalf("flattenRecords() error = %v", err)
	}

	if _, ok := items[0]["fields__CamelCase"]; !ok {
		t.Error("expected fields__CamelCase to exist")
	}
	if _, ok := items[0]["fields__UPPER"]; !ok {
		t.Error("expected fields__UPPER to exist")
	}
	if _, ok := items[0]["fields__lower"]; !ok {
		t.Error("expected fields__lower to exist")
	}
}

func TestFlattenRecordsUseNumber(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"id":"rec1","fields":{"BigID":9007199254740993,"Score":3.14}}`),
	}

	items, err := flattenRecords(raw)
	if err != nil {
		t.Fatalf("flattenRecords() error = %v", err)
	}

	bigID, ok := items[0]["fields__BigID"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number for BigID, got %T", items[0]["fields__BigID"])
	}
	if bigID.String() != "9007199254740993" {
		t.Errorf("expected 9007199254740993, got %s", bigID.String())
	}

	score, ok := items[0]["fields__Score"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number for Score, got %T", items[0]["fields__Score"])
	}
	if score.String() != "3.14" {
		t.Errorf("expected 3.14, got %s", score.String())
	}
}

func TestFlattenRecordsEmptyFields(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"id":"rec1","createdTime":"2024-01-01T00:00:00.000Z","fields":{}}`),
	}

	items, err := flattenRecords(raw)
	if err != nil {
		t.Fatalf("flattenRecords() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["id"] != "rec1" {
		t.Errorf("expected id=rec1, got %v", items[0]["id"])
	}
}

func TestFlattenRecordsInvalidJSON(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{invalid}`),
	}

	_, err := flattenRecords(raw)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
