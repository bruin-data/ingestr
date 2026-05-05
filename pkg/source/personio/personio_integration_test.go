package personio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePersonioURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantID     string
		wantSecret string
		wantErr    bool
	}{
		{
			name:       "valid URI",
			uri:        "personio://?client_id=my-id&client_secret=my-secret",
			wantID:     "my-id",
			wantSecret: "my-secret",
		},
		{
			name:       "valid URI without leading question mark",
			uri:        "personio://client_id=my-id&client_secret=my-secret",
			wantID:     "my-id",
			wantSecret: "my-secret",
		},
		{
			name:    "missing scheme",
			uri:     "http://example.com",
			wantErr: true,
		},
		{
			name:    "missing client_id",
			uri:     "personio://?client_secret=my-secret",
			wantErr: true,
		},
		{
			name:    "missing client_secret",
			uri:     "personio://?client_id=my-id",
			wantErr: true,
		},
		{
			name:    "empty URI body",
			uri:     "personio://",
			wantErr: true,
		},
		{
			name:    "empty query mark only",
			uri:     "personio://?",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, secret, err := parsePersonioURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantSecret, secret)
		})
	}
}

func TestPersonioSource_GetTable(t *testing.T) {
	s := NewPersonioSource()

	for tableName, meta := range supportedTables {
		t.Run(tableName, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tableName})
			require.NoError(t, err)
			assert.NotNil(t, table)
			assert.Equal(t, tableName, table.Name())
			assert.False(t, table.HasKnownSchema())
			assert.Equal(t, meta.primaryKeys, table.PrimaryKeys())
			assert.Equal(t, meta.incrementalKey, table.IncrementalKey())
			assert.Equal(t, meta.strategy, table.Strategy())
		})
	}

	t.Run("unsupported table", func(t *testing.T) {
		_, err := s.GetTable(context.Background(), source.TableRequest{Name: "nonexistent"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported table")
	})
}

func newTestPersonioSource(serverURL string) *PersonioSource {
	s := &PersonioSource{
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		token:        "test-token",
	}
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(serverURL+"/"),
		gonghttp.WithTimeout(10*time.Second),
		gonghttp.WithAuth(gonghttp.NewBearerAuth("test-token")),
		gonghttp.WithDisableRetry(),
	)
	return s
}

func collectBatches(t *testing.T, ch <-chan source.RecordBatchResult) []source.RecordBatchResult {
	t.Helper()
	var results []source.RecordBatchResult
	for r := range ch {
		results = append(results, r)
	}
	return results
}

func TestPersonioSource_ReadEmployees(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "company/employees")

		call := int(requestCount.Add(1))
		w.Header().Set("Content-Type", "application/json")

		switch call {
		case 1:
			assert.Equal(t, "200", r.URL.Query().Get("limit"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": []map[string]interface{}{
					{
						"type": "Employee",
						"attributes": map[string]interface{}{
							"id": map[string]interface{}{
								"label":        "ID",
								"value":        float64(1),
								"type":         "integer",
								"universal_id": "id",
							},
							"first_name": map[string]interface{}{
								"label":        "First name",
								"value":        "John",
								"type":         "standard",
								"universal_id": "first_name",
							},
							"last_name": map[string]interface{}{
								"label":        "Last name",
								"value":        "Doe",
								"type":         "standard",
								"universal_id": "last_name",
							},
							"email": map[string]interface{}{
								"label":        "Email",
								"value":        "john@example.com",
								"type":         "standard",
								"universal_id": "email",
							},
						},
					},
					{
						"type": "Employee",
						"attributes": map[string]interface{}{
							"id": map[string]interface{}{
								"label":        "ID",
								"value":        float64(2),
								"type":         "integer",
								"universal_id": "id",
							},
							"first_name": map[string]interface{}{
								"label":        "First name",
								"value":        "Jane",
								"type":         "standard",
								"universal_id": "first_name",
							},
							"last_name": map[string]interface{}{
								"label":        "Last name",
								"value":        "Smith",
								"type":         "standard",
								"universal_id": "last_name",
							},
							"email": map[string]interface{}{
								"label":        "Email",
								"value":        "jane@example.com",
								"type":         "standard",
								"universal_id": "email",
							},
						},
					},
				},
				"metadata": map[string]interface{}{
					"total_elements": 2,
					"current_page":   1,
					"total_pages":    1,
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    []map[string]interface{}{},
			})
		}
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "employees"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	require.NotNil(t, batches[0].Batch)

	batch := batches[0].Batch
	assert.Equal(t, int64(2), batch.NumRows())

	fieldNames := make(map[string]bool)
	for i := 0; i < int(batch.NumCols()); i++ {
		fieldNames[batch.ColumnName(i)] = true
	}
	assert.True(t, fieldNames["id"])
	assert.True(t, fieldNames["first_name"])
	assert.True(t, fieldNames["last_name"])
	assert.True(t, fieldNames["email"])
}

func TestPersonioSource_ReadEmployeesWithIncrementalStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2024-06-01T00:00:00", r.URL.Query().Get("updated_since"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": []map[string]interface{}{
				{
					"type": "Employee",
					"attributes": map[string]interface{}{
						"id": map[string]interface{}{
							"label":        "ID",
							"value":        float64(1),
							"universal_id": "id",
						},
					},
				},
			},
			"metadata": map[string]interface{}{
				"total_elements": 1,
				"current_page":   1,
				"total_pages":    1,
			},
		})
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "employees"})
	require.NoError(t, err)

	intervalStart := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &intervalStart,
	})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadAbsenceTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "company/time-off-types")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": []map[string]interface{}{
				{
					"type": "TimeOffType",
					"attributes": map[string]interface{}{
						"id":   float64(1),
						"name": "Vacation",
					},
				},
				{
					"type": "TimeOffType",
					"attributes": map[string]interface{}{
						"id":   float64(2),
						"name": "Sick Leave",
					},
				},
			},
			"metadata": map[string]interface{}{
				"total_elements": 2,
				"current_page":   1,
				"total_pages":    1,
			},
		})
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "absence_types"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadAbsences(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "company/time-offs")
		call := int(requestCount.Add(1))

		w.Header().Set("Content-Type", "application/json")

		switch call {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": []map[string]interface{}{
					{
						"type": "TimeOff",
						"attributes": map[string]interface{}{
							"id":         float64(100),
							"status":     "approved",
							"start_date": "2024-01-15",
							"end_date":   "2024-01-20",
						},
					},
				},
				"metadata": map[string]interface{}{
					"total_elements": 1,
					"current_page":   1,
					"total_pages":    1,
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    []map[string]interface{}{},
			})
		}
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "absences"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadAttendances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "company/attendances")
		assert.Equal(t, "2024-01-01", r.URL.Query().Get("start_date"))
		assert.Equal(t, "2024-01-31", r.URL.Query().Get("end_date"))
		assert.Equal(t, "true", r.URL.Query().Get("includePending"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": []map[string]interface{}{
				{
					"id": float64(501),
					"attributes": map[string]interface{}{
						"employee":   float64(1),
						"date":       "2024-01-15",
						"start_time": "09:00",
						"end_time":   "17:00",
						"break":      60,
					},
				},
			},
			"metadata": map[string]interface{}{
				"total_elements": 1,
				"current_page":   1,
				"total_pages":    1,
			},
		})
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "attendances"})
	require.NoError(t, err)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadAttendancesMissingDates(t *testing.T) {
	s := newTestPersonioSource("http://unused")

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "attendances"})
	require.NoError(t, err)

	t.Run("missing start_date", func(t *testing.T) {
		results, err := table.Read(context.Background(), source.ReadOptions{})
		require.NoError(t, err)
		batches := collectBatches(t, results)
		require.Len(t, batches, 1)
		assert.Error(t, batches[0].Err)
		assert.Contains(t, batches[0].Err.Error(), "start_date is required")
	})

	t.Run("missing end_date", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		results, err := table.Read(context.Background(), source.ReadOptions{
			IntervalStart: &start,
		})
		require.NoError(t, err)
		batches := collectBatches(t, results)
		require.Len(t, batches, 1)
		assert.Error(t, batches[0].Err)
		assert.Contains(t, batches[0].Err.Error(), "end_date is required")
	})
}

func TestPersonioSource_ReadProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "company/attendances/projects")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": []map[string]interface{}{
				{
					"id": float64(10),
					"attributes": map[string]interface{}{
						"name":   "Project Alpha",
						"active": true,
					},
				},
				{
					"id": float64(11),
					"attributes": map[string]interface{}{
						"name":   "Project Beta",
						"active": false,
					},
				},
			},
		})
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "projects"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadDocumentCategories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "company/document-categories")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": []map[string]interface{}{
				{
					"id": float64(1),
					"attributes": map[string]interface{}{
						"name": "Contracts",
					},
				},
			},
		})
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "document_categories"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadCustomReportsList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "company/custom-reports/reports")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": []map[string]interface{}{
				{
					"type": "CustomReport",
					"attributes": map[string]interface{}{
						"id":   "rpt-1",
						"name": "Headcount Report",
					},
				},
			},
		})
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "custom_reports_list"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())
}

func TestPersonioSource_ReadEmployeesAbsencesBalance(t *testing.T) {
	var employeeListCalls atomic.Int32
	var balanceCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/company/employees", "/company/employees/":
			call := int(employeeListCalls.Add(1))
			switch call {
			case 1:
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"data": []map[string]interface{}{
						{
							"type": "Employee",
							"attributes": map[string]interface{}{
								"id": map[string]interface{}{
									"label":        "ID",
									"value":        float64(1),
									"universal_id": "id",
								},
							},
						},
						{
							"type": "Employee",
							"attributes": map[string]interface{}{
								"id": map[string]interface{}{
									"label":        "ID",
									"value":        float64(2),
									"universal_id": "id",
								},
							},
						},
					},
					"metadata": map[string]interface{}{
						"total_elements": 2,
						"current_page":   1,
						"total_pages":    1,
					},
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"data":    []map[string]interface{}{},
				})
			}

		case "/company/employees/1/absences/balance", "/company/employees/2/absences/balance":
			balanceCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": []map[string]interface{}{
					{
						"id":       float64(10),
						"category": "Vacation",
						"balance":  float64(15),
					},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "employees_absences_balance"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.NotEmpty(t, batches)

	totalRows := int64(0)
	for _, b := range batches {
		require.NoError(t, b.Err)
		totalRows += b.Batch.NumRows()
	}
	assert.Equal(t, int64(2), totalRows)
	assert.Equal(t, int32(2), balanceCalls.Load())
}

func TestPersonioSource_Pagination(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(requestCount.Add(1))
		w.Header().Set("Content-Type", "application/json")

		switch call {
		case 1:
			assert.Equal(t, "0", r.URL.Query().Get("offset"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": []map[string]interface{}{
					{
						"type": "TimeOffType",
						"attributes": map[string]interface{}{
							"id":   float64(1),
							"name": "Vacation",
						},
					},
					{
						"type": "TimeOffType",
						"attributes": map[string]interface{}{
							"id":   float64(2),
							"name": "Sick Leave",
						},
					},
				},
				"metadata": map[string]interface{}{
					"total_elements": 3,
					"current_page":   1,
					"total_pages":    2,
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": []map[string]interface{}{
					{
						"type": "TimeOffType",
						"attributes": map[string]interface{}{
							"id":   float64(3),
							"name": "Personal",
						},
					},
				},
				"metadata": map[string]interface{}{
					"total_elements": 3,
					"current_page":   2,
					"total_pages":    2,
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    []map[string]interface{}{},
			})
		}
	}))
	defer server.Close()

	s := newTestPersonioSource(server.URL)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "absence_types"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{PageSize: 2})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 2)
	require.NoError(t, batches[0].Err)
	require.NoError(t, batches[1].Err)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestFlattenEmployeeAttributes(t *testing.T) {
	t.Run("flattens attributes with universal_id", func(t *testing.T) {
		items := []map[string]interface{}{
			{
				"type": "Employee",
				"attributes": map[string]interface{}{
					"id": map[string]interface{}{
						"label":        "ID",
						"value":        float64(1),
						"type":         "integer",
						"universal_id": "id",
					},
					"first_name": map[string]interface{}{
						"label":        "First name",
						"value":        "John",
						"type":         "standard",
						"universal_id": "first_name",
					},
					"email": map[string]interface{}{
						"label":        "Email",
						"value":        "john@example.com",
						"type":         "standard",
						"universal_id": "email",
					},
				},
			},
		}

		result := flattenEmployeeAttributes(items)
		require.Len(t, result, 1)
		assert.Equal(t, float64(1), result[0]["id"])
		assert.Equal(t, "John", result[0]["first_name"])
		assert.Equal(t, "john@example.com", result[0]["email"])
	})

	t.Run("falls back to label when universal_id is empty", func(t *testing.T) {
		items := []map[string]interface{}{
			{
				"type": "Employee",
				"attributes": map[string]interface{}{
					"some_key": map[string]interface{}{
						"label": "My Custom Field",
						"value": "custom_value",
					},
				},
			},
		}

		result := flattenEmployeeAttributes(items)
		require.Len(t, result, 1)
		assert.Equal(t, "custom_value", result[0]["my_custom_field"])
	})

	t.Run("skips attributes without label or universal_id", func(t *testing.T) {
		items := []map[string]interface{}{
			{
				"type": "Employee",
				"attributes": map[string]interface{}{
					"nameless": map[string]interface{}{
						"value": "should be skipped",
					},
					"valid": map[string]interface{}{
						"label":        "Valid",
						"value":        "kept",
						"universal_id": "valid",
					},
				},
			},
		}

		result := flattenEmployeeAttributes(items)
		require.Len(t, result, 1)
		assert.Equal(t, "kept", result[0]["valid"])
		_, hasNameless := result[0][""]
		assert.False(t, hasNameless)
	})

	t.Run("multiple employees", func(t *testing.T) {
		items := []map[string]interface{}{
			{
				"type": "Employee",
				"attributes": map[string]interface{}{
					"id": map[string]interface{}{"value": float64(1), "universal_id": "id"},
				},
			},
			{
				"type": "Employee",
				"attributes": map[string]interface{}{
					"id": map[string]interface{}{"value": float64(2), "universal_id": "id"},
				},
			},
		}

		result := flattenEmployeeAttributes(items)
		require.Len(t, result, 2)
		assert.Equal(t, float64(1), result[0]["id"])
		assert.Equal(t, float64(2), result[1]["id"])
	})

	t.Run("skips items without attributes key", func(t *testing.T) {
		items := []map[string]interface{}{
			{"not_attributes": "something"},
		}
		result := flattenEmployeeAttributes(items)
		assert.Empty(t, result)
	})
}

func TestExtractAttributes(t *testing.T) {
	items := []map[string]interface{}{
		{
			"type": "TimeOffType",
			"attributes": map[string]interface{}{
				"id":   float64(1),
				"name": "Vacation",
			},
		},
		{
			"type": "TimeOffType",
			"attributes": map[string]interface{}{
				"id":   float64(2),
				"name": "Sick Leave",
			},
		},
	}

	result := extractAttributes(items)
	require.Len(t, result, 2)
	assert.Equal(t, float64(1), result[0]["id"])
	assert.Equal(t, "Vacation", result[0]["name"])
	assert.Equal(t, float64(2), result[1]["id"])
	assert.Equal(t, "Sick Leave", result[1]["name"])
}

func TestExtractAttributesAddID(t *testing.T) {
	items := []map[string]interface{}{
		{
			"id": float64(99),
			"attributes": map[string]interface{}{
				"name":   "Project Alpha",
				"active": true,
			},
		},
	}

	result := extractAttributesAddID(items)
	require.Len(t, result, 1)
	assert.Equal(t, float64(99), result[0]["id"])
	assert.Equal(t, "Project Alpha", result[0]["name"])
	assert.Equal(t, true, result[0]["active"])
}

func TestConvertCustomReportItems(t *testing.T) {
	items := []map[string]interface{}{
		{
			"type": "CustomReportRow",
			"attributes": map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"id": "item-1",
						"attributes": []interface{}{
							map[string]interface{}{
								"attribute_id": "department",
								"value":        "Engineering",
							},
							map[string]interface{}{
								"attribute_id": "headcount",
								"value":        float64(50),
							},
						},
					},
				},
			},
		},
	}

	result := convertCustomReportItems(items, "rpt-123")
	require.Len(t, result, 1)
	assert.Equal(t, "rpt-123", result[0]["report_id"])
	assert.Equal(t, "item-1", result[0]["item_id"])
	assert.Equal(t, "Engineering", result[0]["department"])
	assert.Equal(t, float64(50), result[0]["headcount"])
}

func TestConvertCustomReportItems_Empty(t *testing.T) {
	result := convertCustomReportItems([]map[string]interface{}{}, "rpt-1")
	assert.Empty(t, result)
}

func TestToTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{name: "time.Time", input: now},
		{name: "*time.Time", input: &now},
		{name: "nil", input: nil, wantErr: true},
		{name: "nil pointer", input: (*time.Time)(nil), wantErr: true},
		{name: "string", input: "not a time", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toTime(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, now.Unix(), result.Unix())
		})
	}
}
