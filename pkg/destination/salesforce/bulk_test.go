package salesforce

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBulkUploadFailureAbortsJob(t *testing.T) {
	for _, stage := range []string{"upload", "close"} {
		for _, abortFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/abortFails=%t", stage, abortFails), func(t *testing.T) {
				var cap capture
				d := newBulkDest(t, &cap, &fakeBulk{}, func(w http.ResponseWriter, r *http.Request, body string) bool {
					fail := stage == "upload" && r.Method == http.MethodPut ||
						stage == "close" && strings.Contains(body, "UploadComplete")
					if fail || abortFails && strings.Contains(body, "Aborted") {
						message := "failed " + stage
						if !fail {
							message = "cleanup failed"
						}
						w.WriteHeader(http.StatusBadRequest)
						_, _ = fmt.Fprintf(w, `[{"errorCode":"INVALID_REQUEST","message":%q}]`, message)
						return true
					}
					return false
				})
				opts := writeOpts("Contact", "append", nil)
				opts.RejectMode = "skip"
				err := d.Write(context.Background(), stringBatch(t, map[string][]string{"LastName": {"Ada"}}, []string{"LastName"}), opts)
				wantError := "upload failed"
				if stage == "close" {
					wantError = "failed to close job"
				}
				if err == nil || !strings.Contains(err.Error(), wantError) || strings.Contains(err.Error(), "cleanup failed") {
					t.Fatalf("Write = %v, want original %s failure even under skip", err, stage)
				}
				want := []string{"POST", "PUT", `PATCH {"state":"Aborted"}`}
				if stage == "close" {
					want = []string{"POST", "PUT", `PATCH {"state":"UploadComplete"}`, `PATCH {"state":"Aborted"}`}
				}
				assertBulkRequests(t, &cap, want)
			})
		}
	}
}

func TestBulkCancellationAbortsJob(t *testing.T) {
	for _, stage := range []string{"upload", "close", "status"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var cap capture
			d := newBulkDest(t, &cap, &fakeBulk{}, func(w http.ResponseWriter, r *http.Request, body string) bool {
				atStatus := r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/jobs/ingest/750J0")
				if stage == "upload" && r.Method == http.MethodPut ||
					stage == "close" && strings.Contains(body, "UploadComplete") ||
					stage == "status" && atStatus {
					cancel()
					<-r.Context().Done()
					return true
				}
				return false
			})
			err := d.Write(ctx, stringBatch(t, map[string][]string{"LastName": {"Ada"}}, []string{"LastName"}), writeOpts("Contact", "append", nil))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Write = %v, want context.Canceled", err)
			}
			want := []string{"POST", "PUT"}
			if stage != "upload" {
				want = append(want, `PATCH {"state":"UploadComplete"}`)
			}
			if stage == "status" {
				want = append(want, "GET")
			}
			want = append(want, `PATCH {"state":"Aborted"}`)
			assertBulkRequests(t, &cap, want)
		})
	}
}

func assertBulkRequests(t *testing.T, cap *capture, want []string) {
	t.Helper()
	var got []string
	for _, req := range cap.all() {
		if strings.Contains(req.path, "/jobs/ingest") {
			path := "/jobs/ingest/750J0"
			switch req.method {
			case http.MethodPost:
				path = "/jobs/ingest"
			case http.MethodPut:
				path += "/batches"
			}
			if !strings.HasSuffix(req.path, path) {
				t.Fatalf("%s path = %s, want suffix %s", req.method, req.path, path)
			}
			step := req.method
			if req.method == http.MethodPatch {
				step += " " + strings.TrimSpace(req.body)
			}
			got = append(got, step)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Bulk requests = %v, want %v", got, want)
	}
}

func TestBulkPollsUntilTerminalState(t *testing.T) {
	for _, state := range []string{"JobComplete", "Failed", "Aborted"} {
		t.Run(state, func(t *testing.T) {
			var cap capture
			polls := 0
			d := newBulkDest(t, &cap, &fakeBulk{}, func(w http.ResponseWriter, r *http.Request, _ string) bool {
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/jobs/ingest/750J0") {
					return false
				}
				states := []string{"UploadComplete", "InProgress", state}
				if polls >= len(states) {
					t.Error("polled after terminal state")
					w.WriteHeader(http.StatusBadRequest)
					return true
				}
				_, _ = fmt.Fprintf(w, `{"state":%q,"errorMessage":"job stopped"}`, states[polls])
				polls++
				return true
			})
			opts := writeOpts("Contact", "append", nil)
			opts.RejectMode = "skip"
			err := d.Write(context.Background(), stringBatch(t, map[string][]string{"LastName": {"Ada"}}, []string{"LastName"}), opts)
			if state == "JobComplete" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), strings.ToLower(state)+": job stopped") {
				t.Fatalf("Write = %v, want terminal %s error even under skip", err, state)
			}
			assertBulkRequests(t, &cap, []string{"POST", "PUT", `PATCH {"state":"UploadComplete"}`, "GET", "GET", "GET"})
		})
	}
}

func TestBulkUploadsTypedArrowCells(t *testing.T) {
	for _, writeNulls := range []bool{false, true} {
		t.Run(fmt.Sprintf("writeNulls=%t", writeNulls), func(t *testing.T) {
			var cap capture
			fb := &fakeBulk{}
			d := newBulkDest(t, &cap, fb, nil)
			sch := arrow.NewSchema([]arrow.Field{
				{Name: "LastName", Type: arrow.BinaryTypes.String},
				{Name: "Active__c", Type: arrow.FixedWidthTypes.Boolean},
				{Name: "Count__c", Type: arrow.PrimitiveTypes.Int64},
				{Name: "Unsigned__c", Type: arrow.PrimitiveTypes.Uint64},
				{Name: "Amount__c", Type: &arrow.Decimal128Type{Precision: 25, Scale: 4}},
				{Name: "Ratio__c", Type: arrow.PrimitiveTypes.Float64},
				{Name: "Date__c", Type: arrow.FixedWidthTypes.Date32},
				{Name: "Time__c", Type: &arrow.TimestampType{Unit: arrow.Microsecond}},
			}, nil)
			b := array.NewRecordBuilder(memory.DefaultAllocator, sch)
			defer b.Release()
			b.Field(0).(*array.StringBuilder).AppendValues([]string{"Ada", "Grace", "Linus", "Ken"}, nil)
			b.Field(1).(*array.BooleanBuilder).AppendValues([]bool{true, false, true, false}, nil)
			b.Field(2).(*array.Int64Builder).AppendValues([]int64{-9007199254740993, 0, 42, 1}, nil)
			b.Field(3).(*array.Uint64Builder).AppendValues([]uint64{math.MaxUint64, 0, 42, 1}, nil)
			amount, err := decimal128.FromString("12345678901234567890.1234", 25, 4)
			if err != nil {
				t.Fatal(err)
			}
			b.Field(4).(*array.Decimal128Builder).Append(amount)
			b.Field(4).AppendNulls(3)
			b.Field(5).(*array.Float64Builder).AppendValues([]float64{-123.125, math.NaN(), math.Inf(1), math.Inf(-1)}, nil)
			b.Field(6).(*array.Date32Builder).Append(arrow.Date32(19675))
			b.Field(6).AppendNulls(3)
			b.Field(7).(*array.TimestampBuilder).Append(arrow.Timestamp(1_700_000_000_123_456))
			b.Field(7).AppendNulls(3)
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
			close(records)
			opts := writeOpts("Contact", "append", nil)
			opts.WriteNulls = writeNulls
			if err := d.WriteParallel(context.Background(), records, opts); err != nil {
				t.Fatal(err)
			}
			null := ""
			if writeNulls {
				null = "#N/A"
			}
			want := "Active__c,Amount__c,Count__c,Date__c,LastName,Ratio__c,Time__c,Unsigned__c\n" +
				"true,12345678901234567890.1234,-9007199254740993,2023-11-14,Ada,-123.125,2023-11-14T22:13:20.123Z,18446744073709551615\n" +
				fmt.Sprintf("false,%s,0,%s,Grace,%s,%s,0\ntrue,%s,42,%s,Linus,%s,%s,42\nfalse,%s,1,%s,Ken,%s,%s,1\n", null, null, null, null, null, null, null, null, null, null, null, null)
			if len(fb.uploads) != 1 || fb.uploads[0] != want {
				t.Fatalf("uploads = %q, want [%q]", fb.uploads, want)
			}
		})
	}
}
