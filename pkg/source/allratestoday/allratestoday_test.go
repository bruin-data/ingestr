package allratestoday

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnect_RejectsWrongScheme(t *testing.T) {
	s := New()
	require.Error(t, s.Connect(context.Background(), "frankfurter://"))
	require.NoError(t, s.Connect(context.Background(), "allratestoday://"))
	require.NoError(t, s.Close(context.Background()))
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantTable string
		wantBank  string
		wantErr   bool
	}{
		{name: "bank list", in: "central_banks", wantTable: "central_banks"},
		{name: "rates with bank", in: "central_bank_rates:rbi", wantTable: "central_bank_rates", wantBank: "rbi"},
		{name: "bank is lowercased", in: "central_bank_rates: ECB ", wantTable: "central_bank_rates", wantBank: "ecb"},
		{name: "rates without bank", in: "central_bank_rates", wantErr: true},
		{name: "rates with empty bank", in: "central_bank_rates:", wantErr: true},
		{name: "bank with path characters", in: "central_bank_rates:../latest", wantErr: true},
		{name: "unknown table", in: "latest", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, bank, err := parseTableName(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTable, table)
			assert.Equal(t, tt.wantBank, bank)
		})
	}
}

func newTestSource(t *testing.T, handler http.HandlerFunc) *Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	s := New()
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(server.URL+"/"),
		ingestrhttp.WithDisableRetry(),
	)
	t.Cleanup(func() { _ = s.client.Close() })
	return s
}

func readAll(t *testing.T, s *Source, table string) (int64, error) {
	t.Helper()
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: table})
	require.NoError(t, err)

	results, err := tbl.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	var rows int64
	var readErr error
	for res := range results {
		if res.Err != nil {
			readErr = res.Err
			continue
		}
		rows += res.Batch.NumRows()
		res.Batch.Release()
	}
	return rows, readErr
}

func TestReadCentralBankRates(t *testing.T) {
	var gotPath string
	s := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bank":"rbi","rate_date":"2026-10-23","rates":[
			{"base":"USD","quote":"INR","type":"reference","value":94.9432},
			{"base":"EUR","quote":"INR","type":"reference","value":106.4547},
			{"base":"GBP","quote":"INR","type":"reference","value":null}
		]}`))
	})

	rows, err := readAll(t, s, "central_bank_rates:rbi")
	require.NoError(t, err)
	assert.Equal(t, "/central-bank/rbi", gotPath)
	assert.Equal(t, int64(2), rows, "rows without a value are skipped")
}

func TestReadCentralBankRates_APIError(t *testing.T) {
	s := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"No published table available for this source"}`))
	})

	_, err := readAll(t, s, "central_bank_rates:zzz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Contains(t, err.Error(), "No published table")
}

func TestReadCentralBanks(t *testing.T) {
	var gotPath string
	s := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sources":[
			{"code":"rbi","name":"Reserve Bank of India","latest":"2026-10-23","stale":false,"cadence":"daily"},
			{"code":"ecb","name":"European Central Bank","latest":"2026-10-23","stale":false,"cadence":"daily"},
			{"code":"new","name":"Not published yet"}
		]}`))
	})

	rows, err := readAll(t, s, "central_banks")
	require.NoError(t, err)
	assert.Equal(t, "/central-banks", gotPath)
	assert.Equal(t, int64(3), rows)
}

func TestGetTable_Strategies(t *testing.T) {
	s := New()

	banks, err := s.GetTable(context.Background(), source.TableRequest{Name: "central_banks"})
	require.NoError(t, err)
	assert.Equal(t, []string{"code"}, banks.PrimaryKeys())
	assert.Equal(t, config.StrategyReplace, banks.Strategy())

	rates, err := s.GetTable(context.Background(), source.TableRequest{Name: "central_bank_rates:ecb"})
	require.NoError(t, err)
	assert.Equal(t, []string{"bank", "date", "base", "quote", "rate_type"}, rates.PrimaryKeys())
	assert.Equal(t, config.StrategyDeleteInsert, rates.Strategy())
	assert.Equal(t, "date", rates.IncrementalKey())
	assert.Equal(t, "date", rates.(source.PartitionedTable).PartitionBy())
	assert.True(t, rates.(source.IntervalIgnoringTable).IgnoresInterval())
}
