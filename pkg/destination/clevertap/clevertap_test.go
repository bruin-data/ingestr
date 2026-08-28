package clevertap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type uploadBody struct {
	D []map[string]interface{} `json:"d"`
}

// newUploadServer captures every /1/upload payload and replies with success.
func newUploadServer(t *testing.T) (*httptest.Server, *[]uploadBody) {
	t.Helper()
	var mu sync.Mutex
	var bodies []uploadBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, uploadEndpoint, r.URL.Path)
		require.Equal(t, "acc-123", r.Header.Get("X-CleverTap-Account-Id"))
		require.Equal(t, "pass-xyz", r.Header.Get("X-CleverTap-Passcode"))

		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body uploadBody
		require.NoError(t, json.Unmarshal(raw, &body))

		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","processed":`+strconv.Itoa(len(body.D))+`,"unprocessed":[]}`)
	}))
	t.Cleanup(server.Close)
	return server, &bodies
}

func connectTestDestination(t *testing.T, serverURL string) *CleverTapDestination {
	t.Helper()
	uri := "clevertap://?account_id=acc-123&passcode=pass-xyz&region=eu1&endpoint=" + url.QueryEscape(serverURL)
	d := NewCleverTapDestination()
	require.NoError(t, d.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, d.Close(context.Background())) })
	return d
}

func profileBatch() arrow.RecordBatch {
	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "age", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"hasan@x.com", "ali@x.com"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"hasan", "ali"}, nil)
	b.Field(2).(*array.Int64Builder).AppendValues([]int64{25, 30}, nil)
	return b.NewRecordBatch()
}

func TestWriteProfiles(t *testing.T) {
	server, bodies := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: profileBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=email"}))

	require.Len(t, *bodies, 1)
	recs := (*bodies)[0].D
	require.Len(t, recs, 2)
	assert.Equal(t, "profile", recs[0]["type"])
	assert.Equal(t, "hasan@x.com", recs[0]["identity"])
	assert.Equal(t, map[string]interface{}{"name": "hasan", "age": float64(25)}, recs[0]["profileData"])
	assert.NotContains(t, recs[0], "evtName")
}

func eventBatch() arrow.RecordBatch {
	s := arrow.NewSchema([]arrow.Field{
		{Name: "user_id", Type: arrow.BinaryTypes.String},
		{Name: "amount", Type: arrow.PrimitiveTypes.Float64},
		{Name: "purchased_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond}},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"u-42"}, nil)
	b.Field(1).(*array.Float64Builder).AppendValues([]float64{49.9}, nil)
	ts := arrow.Timestamp(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC).UnixMicro())
	b.Field(2).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{ts}, nil)
	return b.NewRecordBatch()
}

func TestWriteEvents(t *testing.T) {
	server, bodies := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: eventBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table: "events?identity_column=user_id&ts=purchased_at&event_name=Charged",
	}))

	require.Len(t, *bodies, 1)
	rec := (*bodies)[0].D[0]
	assert.Equal(t, "event", rec["type"])
	assert.Equal(t, "u-42", rec["identity"])
	assert.Equal(t, "Charged", rec["evtName"])
	assert.Equal(t, float64(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC).Unix()), rec["ts"])
	assert.Equal(t, map[string]interface{}{"amount": 49.9}, rec["evtData"])
}

func TestWriteWithLiteralIdentity(t *testing.T) {
	server, bodies := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: profileBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity=vip-user"}))

	require.Len(t, *bodies, 1)
	recs := (*bodies)[0].D
	require.Len(t, recs, 2)
	assert.Equal(t, "vip-user", recs[0]["identity"])
	assert.Equal(t, "vip-user", recs[1]["identity"])
	assert.Equal(t, map[string]interface{}{"email": "hasan@x.com", "name": "hasan", "age": float64(25)}, recs[0]["profileData"])
}

func TestLiteralIdentityWithIDType(t *testing.T) {
	server, bodies := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: eventBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table: "events?identity=device-1&id_type=objectId&ts=purchased_at&event_name=Charged",
	}))

	rec := (*bodies)[0].D[0]
	assert.Equal(t, "device-1", rec["objectId"])
	assert.NotContains(t, rec, "identity")
}

func TestIdentityRequired(t *testing.T) {
	server, _ := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles"})
	require.ErrorContains(t, err, "set identity_column=<column> or identity=<constant>")
}

func TestIdentityAndIDValueMutuallyExclusive(t *testing.T) {
	server, _ := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=email&identity=x"})
	require.ErrorContains(t, err, "not both")
}

func TestEventTSSecondUnit(t *testing.T) {
	server, bodies := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "user_id", Type: arrow.BinaryTypes.String},
		{Name: "purchased_at", Type: &arrow.TimestampType{Unit: arrow.Second}},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"u-1"}, nil)
	secs := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC).Unix()
	b.Field(1).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{arrow.Timestamp(secs)}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table: "events?identity_column=user_id&ts=purchased_at&event_name=Charged",
	}))

	rec := (*bodies)[0].D[0]
	assert.Equal(t, float64(secs), rec["ts"])
}

func TestMissingIdentityColumnFailsFast(t *testing.T) {
	server, _ := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: profileBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=missing_col"})
	require.ErrorContains(t, err, "identity column \"missing_col\" not found")
}

func TestMissingTSColumnFailsFast(t *testing.T) {
	server, _ := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: eventBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{
		Table: "events?identity_column=user_id&ts=purchsed_at&event_name=Charged",
	})
	require.ErrorContains(t, err, "ts column \"purchsed_at\" not found")
}

func TestEventsRequireEventName(t *testing.T) {
	server, _ := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "events?identity_column=user_id"})
	require.ErrorContains(t, err, "event_name")
}

func TestProfilesRejectTS(t *testing.T) {
	server, _ := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=email&ts=updated_at"})
	require.ErrorContains(t, err, "ts is only supported for events")
}

func TestRejectedRecordsFailByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"partial","processed":1,"unprocessed":[{"status":"fail","code":513,"error":"Invalid identity","record":{"identity":"ali@x.com","type":"profile"}}]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: profileBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=email"})
	require.ErrorContains(t, err, "clevertap rejected 1 profile record(s)")
	require.ErrorContains(t, err, "code 513")
	require.ErrorContains(t, err, "ali@x.com")
}

func TestRejectedRecordsSkippedWithOnErrorSkip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","processed":1,"unprocessed":[{"status":"fail","code":513,"error":"Invalid identity"}]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: profileBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=email&on_error=skip"}))
}

func TestNullIdentityRowsSkipped(t *testing.T) {
	server, bodies := newUploadServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com", ""}, []bool{true, false})
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"A", "B"}, nil)
	batch := b.NewRecordBatch()

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "profiles?identity_column=email"}))

	require.Len(t, *bodies, 1)
	assert.Len(t, (*bodies)[0].D, 1)
}

func TestStrategySupport(t *testing.T) {
	d := NewCleverTapDestination()
	assert.True(t, d.SupportsAppendStrategy())
	assert.True(t, d.SupportsReplaceStrategy())
	assert.False(t, d.SupportsMergeStrategy())
	assert.False(t, d.SupportsDeleteInsertStrategy())
	assert.False(t, d.SupportsSCD2Strategy())
	assert.False(t, d.SupportsAtomicSwap())
}

func TestInvalidURI(t *testing.T) {
	d := NewCleverTapDestination()
	require.ErrorContains(t, d.Connect(context.Background(), "clevertap://?passcode=x"), "account_id is required")
	require.ErrorContains(t, d.Connect(context.Background(), "clevertap://?account_id=x"), "passcode is required")
	require.ErrorContains(t, d.Connect(context.Background(), "clevertap://?account_id=x&passcode=y&region=zz"), "invalid region")
}
