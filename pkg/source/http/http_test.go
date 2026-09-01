package http

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		table    string
		expected fileFormat
	}{
		{"csv from table hint", "https://example.com/data", "my_data#csv", formatCSV},
		{"csv_headless from table hint", "https://example.com/data", "my_data#csv_headless", formatCSVHeadless},
		{"json from table hint", "https://example.com/api", "my_data#json", formatJSON},
		{"jsonl from table hint", "https://example.com/api", "my_data#jsonl", formatJSONL},
		{"ndjson from table hint", "https://example.com/api", "my_data#ndjson", formatJSONL},
		{"parquet from table hint", "https://example.com/data", "my_data#parquet", formatParquet},
		{"csv from url extension", "https://example.com/data.csv", "my_data", formatCSV},
		{"json from url extension", "https://example.com/data.json", "my_data", formatJSON},
		{"jsonl from url extension", "https://example.com/data.jsonl", "my_data", formatJSONL},
		{"ndjson from url extension", "https://example.com/data.ndjson", "my_data", formatJSONL},
		{"parquet from url extension", "https://example.com/data.parquet", "my_data", formatParquet},
		{"csv url with query params", "https://example.com/data.csv?token=abc", "my_data", formatCSV},
		{"unknown format", "https://example.com/data", "my_data", formatUnknown},
		{"hint overrides url", "https://example.com/data.csv", "my_data#json", formatJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := detectFormat(tt.url, tt.table, "")
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectFormatEncoding(t *testing.T) {
	tests := []struct {
		name             string
		table            string
		expectedFormat   fileFormat
		expectedEncoding string
	}{
		{"encoding only", "my_data#encoding=windows-1252", formatUnknown, "windows-1252"},
		{"format then encoding", "my_data#csv,encoding=windows-1252", formatCSV, "windows-1252"},
		{"encoding then format", "my_data#encoding=windows-1252,csv", formatCSV, "windows-1252"},
		{"space after comma", "my_data#csv, encoding=windows-1252", formatCSV, "windows-1252"},
		{"space before format", "my_data#encoding=windows-1252, csv", formatCSV, "windows-1252"},
		{"mixed-case encoding value preserved", "my_data#csv,encoding=Windows-1252", formatCSV, "Windows-1252"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, encoding := detectFormat("https://example.com/data", tt.table, "")
			assert.Equal(t, tt.expectedFormat, format)
			assert.Equal(t, tt.expectedEncoding, encoding)
		})
	}
}

func TestCleanTableName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my_data", "my_data"},
		{"my_data#csv", "my_data"},
		{"my_data#json", "my_data"},
		{"table#parquet", "table"},
		{"no_hint", "no_hint"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanTableName(tt.input))
		})
	}
}

func TestParseColumnNames(t *testing.T) {
	tests := []struct {
		name     string
		columns  string
		numCols  int
		expected []string
	}{
		{"empty columns", "", 3, []string{"unknown_col_0", "unknown_col_1", "unknown_col_2"}},
		{"with names and types", "id:bigint,name:text,value:double", 3, []string{"id", "name", "value"}},
		{"names only no types", "id,name,value", 3, []string{"id", "name", "value"}},
		{"fewer columns than data", "id:bigint,name:text", 3, []string{"id", "name", "unknown_col_2"}},
		{"more columns than data", "id:bigint,name:text,value:double,extra:int", 3, []string{"id", "name", "value"}},
		{"with spaces", " id : bigint , name : text ", 2, []string{"id", "name"}},
		{"3-part picks source name", "first_name:string:fname,email::eml", 2, []string{"fname", "eml"}},
		{"mixed 2 and 3 part", "id:bigint,first_name:string:fname", 2, []string{"id", "fname"}},
		{"decimal type with rename", "amount:decimal(10,2):raw_amount,name:text", 2, []string{"raw_amount", "name"}},
		{"decimal type without rename", "id:bigint,amount:decimal(10,2)", 2, []string{"id", "amount"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseColumnNames(tt.columns, tt.numCols)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInferCSVValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected interface{}
	}{
		{"empty", "", nil},
		{"whitespace", "   ", nil},
		{"NaN", "NaN", nil},
		{"nan", "nan", nil},
		{"NA", "NA", nil},
		{"N/A", "N/A", nil},
		{"null", "null", nil},
		{"None", "None", nil},
		{"none", "none", nil},
		{"true", "true", true},
		{"True", "True", true},
		{"TRUE", "TRUE", true},
		{"false", "false", false},
		{"False", "False", false},
		{"FALSE", "FALSE", false},
		{"zero", "0", int64(0)},
		{"positive int", "42", int64(42)},
		{"negative int", "-10", int64(-10)},
		{"large int", "9999999999", int64(9999999999)},
		{"float", "3.14", 3.14},
		{"negative float", "-0.5", -0.5},
		{"scientific", "1.5e3", 1500.0},
		{"plain string", "hello", "hello"},
		{"string with spaces", "  hello world  ", "hello world"},
		{"date-like stays string", "2024-01-15", "2024-01-15"},
		{"mixed alphanumeric", "abc123", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, inferCSVValue(tt.input))
		})
	}
}

// TestHTTPByteCap proves the MaxBatchBytes cap: with the cap off the whole JSON
// array lands in one batch; with a small cap the same rows split across many
// batches with no row lost.
func TestHTTPByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The HTTP JSON source does a single unpaginated fetch per read, so it
		// always returns the full 50-row array.
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": i, "blob": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		// The generic HTTP JSON reader with the byte cap parses a top-level array.
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := NewHTTPSource()
		if err := s.Connect(context.Background(), srv.URL); err != nil {
			t.Fatal(err)
		}
		results, err := s.read(context.Background(), "data#json", source.ReadOptions{MaxBatchBytes: max})
		if err != nil {
			t.Fatal(err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if res.Batch == nil {
				continue
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offB, offR := run(0)
	onB, onR := run(4096)

	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d want 50", offR, onR)
	}
}

func TestHTTPSourceStreamsBeforeResponseCompletes(t *testing.T) {
	firstWritten := make(chan struct{})
	finish := make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "id,name\n1,first\n")
		w.(http.Flusher).Flush()
		close(firstWritten)
		<-finish
		_, _ = io.WriteString(w, "2,second\n")
	}))
	defer srv.Close()

	s := connectedSource(t, srv.URL)
	results, err := s.read(context.Background(), "data", source.ReadOptions{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	<-firstWritten

	select {
	case result := <-results:
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		assert.EqualValues(t, 1, result.Batch.NumRows())
		result.Batch.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("first record batch was not emitted while the response remained open")
	}

	close(finish)
	rows, err := collectRows(results)
	assert.NoError(t, err)
	assert.EqualValues(t, 1, rows)
}

func TestHTTPSourceAuthHeadersRedirectAndMetadata(t *testing.T) {
	const (
		token  = "top-secret-token"
		apiKey = "top-secret-key"
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/download", http.StatusFound)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-API-Key") != apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("ETag", `"version-1"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		_, _ = io.WriteString(w, "{\"id\":1}\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	uri := srv.URL + "/start#ingestr:bearer_token=" + url.QueryEscape(token) + "&header.X-API-Key=" + url.QueryEscape(apiKey)
	s := connectedSource(t, uri)
	rows, err := readRows(t, s, "data", source.ReadOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, 1, rows)

	metadata := s.Metadata()
	assert.Equal(t, srv.URL+"/download", metadata.FinalURL)
	assert.Equal(t, `"version-1"`, metadata.ETag)
	assert.Equal(t, "Wed, 21 Oct 2015 07:28:00 GMT", metadata.LastModified)
	assert.Equal(t, "application/x-ndjson", metadata.ContentType)
	assert.Greater(t, metadata.ContentLength, int64(0))
	assert.NotContains(t, displayURL(s.target), token)
	assert.NotContains(t, displayURL(s.target), apiKey)
}

func TestHTTPSourceStripsConfiguredHeadersOnCrossOriginRedirect(t *testing.T) {
	var gotAuthorization, gotSecret string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotSecret = r.Header.Get("X-Secret")
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "id\n1\n")
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/data.csv", http.StatusFound)
	}))
	defer redirect.Close()

	s := connectedSource(t, redirect.URL+"#ingestr:bearer_token=secret&header.X-Secret=secret")
	_, err := readRows(t, s, "data", source.ReadOptions{})
	assert.NoError(t, err)
	assert.Empty(t, gotAuthorization)
	assert.Empty(t, gotSecret)
}

func TestHTTPSourceResumesInterruptedResponse(t *testing.T) {
	data := []byte("id,name\n1,alpha\n2,beta\n3,gamma\n")
	const cut = 17
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := requests.Add(1)
		w.Header().Set("ETag", `"stable"`)
		w.Header().Set("Content-Type", "text/csv")
		if request == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:cut])
			return
		}

		assert.Equal(t, fmt.Sprintf("bytes=%d-", cut), r.Header.Get("Range"))
		assert.Equal(t, `"stable"`, r.Header.Get("If-Range"))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(data)-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-cut))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[cut:])
	}))
	defer srv.Close()

	s := connectedSource(t, srv.URL+"/data.csv#ingestr:retries=0")
	rows, err := readRows(t, s, "data", source.ReadOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, 3, rows)
	assert.EqualValues(t, 2, requests.Load())
}

func TestHTTPSourceRejectsChangedOrMissingResumeValidator(t *testing.T) {
	data := []byte("id,name\n1,alpha\n2,beta\n")
	const cut = 14
	tests := []struct {
		name          string
		initialHeader http.Header
		resumeHeader  http.Header
		errorContains string
	}{
		{
			name:          "missing ETag",
			initialHeader: http.Header{"ETag": {`"stable"`}},
			resumeHeader:  http.Header{},
			errorContains: "ETag was missing or changed",
		},
		{
			name:          "changed Last-Modified",
			initialHeader: http.Header{"Last-Modified": {"Wed, 21 Oct 2015 07:28:00 GMT"}},
			resumeHeader:  http.Header{"Last-Modified": {"Thu, 22 Oct 2015 07:28:00 GMT"}},
			errorContains: "Last-Modified was missing or changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := requests.Add(1)
				w.Header().Set("Content-Type", "text/csv")
				if request == 1 {
					for name, values := range tt.initialHeader {
						w.Header()[name] = values
					}
					w.Header().Set("Content-Length", strconv.Itoa(len(data)))
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(data[:cut])
					return
				}

				for name, values := range tt.resumeHeader {
					w.Header()[name] = values
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(data)-1, len(data)))
				w.Header().Set("Content-Length", strconv.Itoa(len(data)-cut))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(data[cut:])
			}))
			defer srv.Close()

			s := connectedSource(t, srv.URL+"/data.csv#ingestr:retries=0")
			_, err := readRows(t, s, "data", source.ReadOptions{})
			assert.ErrorContains(t, err, tt.errorContains)
			assert.EqualValues(t, 2, requests.Load())
		})
	}
}

func TestHTTPSourceDoesNotFakeResumeWithoutValidator(t *testing.T) {
	data := []byte("id\n1\n2\n")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data[:5])
	}))
	defer srv.Close()

	s := connectedSource(t, srv.URL+"/data.csv#ingestr:retries=0")
	_, err := readRows(t, s, "data", source.ReadOptions{})
	assert.ErrorContains(t, err, "expected")
	assert.EqualValues(t, 1, requests.Load())
}

func TestHTTPSourceConditionalRequest(t *testing.T) {
	const etag = `"current"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, etag, r.Header.Get("If-None-Match"))
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	s := connectedSource(t, srv.URL+"/data.csv#ingestr:if_none_match="+url.QueryEscape(etag)+"&retries=0")
	_, err := readRows(t, s, "data", source.ReadOptions{})
	assert.ErrorIs(t, err, ErrNotModified)
}

func TestHTTPSourceChecksum(t *testing.T) {
	data := []byte("id\n1\n")
	digest := sha256.Sum256(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	t.Run("valid", func(t *testing.T) {
		s := connectedSource(t, fmt.Sprintf("%s/data#ingestr:checksum=sha256:%x&retries=0", srv.URL, digest))
		rows, err := readRows(t, s, "data", source.ReadOptions{})
		assert.NoError(t, err)
		assert.EqualValues(t, 1, rows)
	})

	t.Run("mismatch", func(t *testing.T) {
		s := connectedSource(t, srv.URL+"/data#ingestr:checksum=sha256:"+strings.Repeat("0", 64)+"&retries=0")
		_, err := readRows(t, s, "data", source.ReadOptions{})
		assert.ErrorContains(t, err, "checksum mismatch")
	})
}

func TestHTTPSourceRetryCancellation(t *testing.T) {
	requestReceived := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived <- struct{}{}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	s := connectedSource(t, srv.URL+"/data.csv#ingestr:retries=3")
	results, err := s.read(ctx, "data", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-requestReceived
	cancel()
	_, err = collectRows(results)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHTTPSourceRetriesTransientStatus(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "id\n1\n")
	}))
	defer srv.Close()

	s := connectedSource(t, srv.URL+"/data.csv#ingestr:retries=1")
	rows, err := readRows(t, s, "data", source.ReadOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, 1, rows)
	assert.EqualValues(t, 2, requests.Load())
}

func TestHTTPSourceStreamsGzip(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = io.WriteString(writer, "id\n1\n2\n")
	assert.NoError(t, writer.Close())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer srv.Close()

	s := connectedSource(t, srv.URL+"/data.csv.gz#ingestr:retries=0")
	rows, err := readRows(t, s, "data", source.ReadOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, 2, rows)
}

func TestHTTPSourceUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		suffix   string
		expected string
	}{
		{"default user-agent", "", "ingestr/1.0 (https://github.com/bruin-data/ingestr)"},
		{"custom user-agent", "&header.User-Agent=my-agent", "my-agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUA atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUA.Store(r.Header.Get("User-Agent"))
				w.Header().Set("Content-Type", "text/csv")
				_, _ = io.WriteString(w, "id\n1\n")
			}))
			defer srv.Close()

			s := connectedSource(t, srv.URL+"/data.csv#ingestr:retries=0"+tt.suffix)
			_, err := readRows(t, s, "data", source.ReadOptions{})
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, gotUA.Load())
		})
	}
}

func TestParseSourceURIAuthentication(t *testing.T) {
	target, opts, err := parseSourceURI("https://user:password@example.com/data.csv?signature=keep#ingestr:header.X-Value=a%26b%23c&retries=0")
	assert.NoError(t, err)
	assert.Nil(t, target.User)
	assert.Equal(t, "signature=keep", target.RawQuery)
	assert.Equal(t, basicAuth("user", "password"), opts.headers.Get("Authorization"))
	assert.Equal(t, "a&b#c", opts.headers.Get("X-Value"))
	assert.Equal(t, 0, opts.retries)
}

func connectedSource(t *testing.T, uri string) *HTTPSource {
	t.Helper()
	s := NewHTTPSource()
	if err := s.Connect(context.Background(), uri); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

func readRows(t *testing.T, s *HTTPSource, table string, opts source.ReadOptions) (int64, error) {
	t.Helper()
	results, err := s.read(context.Background(), table, opts)
	if err != nil {
		return 0, err
	}
	return collectRows(results)
}

func collectRows(results <-chan source.RecordBatchResult) (int64, error) {
	var rows int64
	for result := range results {
		if result.Err != nil {
			return rows, result.Err
		}
		if result.Batch != nil {
			rows += result.Batch.NumRows()
			result.Batch.Release()
		}
	}
	return rows, nil
}
