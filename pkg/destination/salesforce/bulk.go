package salesforce

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

const (
	loadMethodREST = "rest"
	loadMethodBulk = "bulk"

	// bulkMaxJobBytes flushes a job well under Salesforce's 150 MB upload cap,
	// leaving room for CSV quoting.
	bulkMaxJobBytes = 50 << 20

	bulkJobTimeout = 2 * time.Hour

	// bulkNull clears a field; an empty cell leaves it unchanged.
	bulkNull = "#N/A"

	bulkNullLiteralCode = "BULK_NULL_LITERAL"
)

// bulkPollWait is the wait between job status checks.
func bulkPollWait(attempt int) time.Duration {
	if attempt >= 4 {
		return 10 * time.Second
	}
	return time.Second << attempt
}

var bulkOperations = map[string]string{
	"create": "insert",
	"update": "update",
	"upsert": "upsert",
	"delete": "delete",
}

// bulkJobs buffers shaped records per action until they are flushed as one
// Bulk API 2.0 job each.
type bulkJobs struct {
	byAction map[string]*bulkBuffer
}

type bulkBuffer struct {
	headers map[string]struct{}
	rows    []map[string]string
	size    int
}

// useBulk switches the shaper to Bulk API 2.0, the default unless the URI sets
// load_method=rest.
func (d *SalesforceDestination) useBulk(sh *shaper) error {
	if d.loadMethod != loadMethodBulk {
		return nil
	}
	if sh.failFast() {
		return fmt.Errorf("salesforce: --reject-mode fail_fast needs load_method=rest on the destination URI; the default bulk load reports rejected records only after each job finishes, so use fail or skip with it")
	}
	sh.bulk = &bulkJobs{byAction: map[string]*bulkBuffer{}}
	return nil
}

func (d *SalesforceDestination) bufferBulk(ctx context.Context, sh *shaper, action string, records []sfRecord, rejects *rejectionLog) error {
	buf := sh.bulk.byAction[action]
	if buf == nil {
		buf = &bulkBuffer{headers: map[string]struct{}{}}
		sh.bulk.byAction[action] = buf
	}
	for _, r := range records {
		row := bulkCells(r)
		if field := literalBulkNull(r); field != "" {
			rejects.add([]rejection{{
				code:       bulkNullLiteralCode,
				message:    fmt.Sprintf("%s holds the text %q, which the Bulk API reads as \"clear this field\"; use load_method=rest to write it as text", field, bulkNull),
				fields:     []string{field},
				identifier: bulkRowKey(sh, row),
			}})
			continue
		}
		for k, v := range row {
			buf.headers[k] = struct{}{}
			buf.size += len(k) + len(v) + 2
		}
		buf.rows = append(buf.rows, row)
	}
	if buf.size < bulkMaxJobBytes {
		return nil
	}
	delete(sh.bulk.byAction, action)
	return d.runBulkJob(ctx, sh, action, buf, rejects)
}

// flushBulk runs a job for every action still buffered. Deletes run last so a
// mirror sweep never precedes the writes it depends on.
func (d *SalesforceDestination) flushBulk(ctx context.Context, sh *shaper, rejects *rejectionLog) error {
	if sh.bulk == nil {
		return nil
	}
	for _, action := range []string{"create", "update", "upsert", "delete"} {
		buf := sh.bulk.byAction[action]
		if buf == nil || len(buf.rows) == 0 {
			continue
		}
		delete(sh.bulk.byAction, action)
		if err := d.runBulkJob(ctx, sh, action, buf, rejects); err != nil {
			return err
		}
	}
	return nil
}

// bulkCells flattens a record into CSV cells: nested relationship objects become
// "Account.Ext_Id__c" (or typed "Contact:Who.Ext_Id__c") columns, and null (or empty) values become #N/A so they
// clear the field just as a JSON null does over REST.
func bulkCells(r sfRecord) map[string]string {
	out := make(map[string]string, len(r))
	for k, v := range r {
		if k == "attributes" {
			continue
		}
		if nested, ok := v.(map[string]interface{}); ok {
			prefix := k
			if attrs, ok := nested["attributes"].(map[string]string); ok && attrs["type"] != "" {
				prefix = attrs["type"] + ":" + k
			}
			for f, fv := range nested {
				if f != "attributes" {
					out[prefix+"."+f] = bulkCell(fv)
				}
			}
			continue
		}
		out[k] = bulkCell(v)
	}
	return out
}

// literalBulkNull returns the field whose text is exactly bulkNull; bulk has no
// escape for it, so sending it would clear the field instead.
func literalBulkNull(r sfRecord) string {
	for k, v := range r {
		if nested, ok := v.(map[string]interface{}); ok {
			for f, fv := range nested {
				if fv == bulkNull {
					return k + "." + f
				}
			}
			continue
		}
		if v == bulkNull {
			return k
		}
	}
	return ""
}

func bulkCell(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return bulkNull
	case string:
		if x == "" {
			return bulkNull
		}
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

func (b *bulkBuffer) csv() ([]byte, error) {
	headers := make([]string, 0, len(b.headers))
	for h := range b.headers {
		headers = append(headers, h)
	}
	slices.Sort(headers)

	var out bytes.Buffer
	w := csv.NewWriter(&out)
	if err := w.Write(headers); err != nil {
		return nil, err
	}
	line := make([]string, len(headers))
	for _, row := range b.rows {
		for i, h := range headers {
			line[i] = row[h]
		}
		if err := w.Write(line); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return out.Bytes(), w.Error()
}

type bulkJobInfo struct {
	ID                     string `json:"id"`
	State                  string `json:"state"`
	ErrorMessage           string `json:"errorMessage"`
	NumberRecordsProcessed int64  `json:"numberRecordsProcessed"`
	NumberRecordsFailed    int64  `json:"numberRecordsFailed"`
}

// runBulkJob creates a job, uploads the CSV, waits for Salesforce to process it,
// then records the written ids and turns failed rows into rejections. A job that
// fails as a whole (bad header, permissions, limits) aborts the run.
func (d *SalesforceDestination) runBulkJob(ctx context.Context, sh *shaper, action string, buf *bulkBuffer, rejects *rejectionLog) error {
	data, err := buf.csv()
	if err != nil {
		return fmt.Errorf("salesforce bulk: failed to encode CSV: %w", err)
	}
	body := map[string]string{
		"object":      sh.sobject,
		"operation":   bulkOperations[action],
		"contentType": "CSV",
		"lineEnding":  "LF",
	}
	if action == "upsert" {
		body["externalIdFieldName"] = sh.idField
	}

	var job bulkJobInfo
	if err := d.bulkRequest(ctx, "POST", "", body, &job); err != nil {
		return fmt.Errorf("salesforce bulk %s %s: failed to create job: %w", body["operation"], sh.sobject, err)
	}
	config.Debug("[SALESFORCE DEST] bulk %s job %s: uploading %d %s record(s) (%d bytes)", body["operation"], job.ID, len(buf.rows), sh.sobject, len(data))

	if err := d.uploadBulkJob(ctx, job.ID, data); err != nil {
		d.abortBulkJob(ctx, job.ID)
		return fmt.Errorf("salesforce bulk job %s: %w", job.ID, err)
	}

	final, err := d.waitBulkJob(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("salesforce bulk job %s: %w", job.ID, err)
	}
	if final.State != "JobComplete" {
		return fmt.Errorf("salesforce bulk %s %s job %s %s: %s", body["operation"], sh.sobject, job.ID, strings.ToLower(final.State), final.ErrorMessage)
	}
	config.Debug("[SALESFORCE DEST] bulk job %s complete: %d processed, %d failed", job.ID, final.NumberRecordsProcessed, final.NumberRecordsFailed)

	if sh.writtenIDs != nil && action != "delete" {
		err := d.bulkResults(ctx, job.ID, "successfulResults", func(row map[string]string) {
			if id := row["sf__Id"]; id != "" {
				sh.writtenIDs.Store(id, struct{}{})
			}
		})
		if err != nil {
			return err
		}
	}
	if final.NumberRecordsFailed == 0 {
		return nil
	}

	var failed []rejection
	err = d.bulkResults(ctx, job.ID, "failedResults", func(row map[string]string) {
		rej := parseBulkError(row["sf__Error"])
		if action == "delete" && rej.code == entityDeletedCode {
			return
		}
		rej.identifier = bulkRowKey(sh, row)
		failed = append(failed, rej)
	})
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		output.Warnf("Warning: salesforce rejected %d of %d %s record(s) in bulk job %s; first error: %s\n", len(failed), len(buf.rows), sh.sobject, job.ID, failed[0].message)
		rejects.add(failed)
	}
	return orgLimitError(sh, failed)
}

func (d *SalesforceDestination) uploadBulkJob(ctx context.Context, jobID string, data []byte) error {
	resp, err := d.client.R(ctx).
		SetHeader("Content-Type", "text/csv").
		SetBody(data).
		Put(d.dataPath("/jobs/ingest/" + jobID + "/batches"))
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	if resp.StatusCode() != 201 && resp.StatusCode() != 200 {
		return fmt.Errorf("upload failed: %w", parseAPIError(resp))
	}
	if err := d.bulkRequest(ctx, "PATCH", jobID, map[string]string{"state": "UploadComplete"}, nil); err != nil {
		return fmt.Errorf("failed to close job: %w", err)
	}
	return nil
}

// abortBulkJob is best-effort: a job left open times out on its own.
func (d *SalesforceDestination) abortBulkJob(ctx context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := d.bulkRequest(ctx, "PATCH", jobID, map[string]string{"state": "Aborted"}, nil); err != nil {
		config.Debug("[SALESFORCE DEST] failed to abort bulk job %s: %v", jobID, err)
	}
}

func (d *SalesforceDestination) waitBulkJob(ctx context.Context, jobID string) (bulkJobInfo, error) {
	deadline := time.Now().Add(bulkJobTimeout)
	for attempt := 0; ; attempt++ {
		var job bulkJobInfo
		if err := d.bulkRequest(ctx, "GET", jobID, nil, &job); err != nil {
			return job, fmt.Errorf("failed to read job status: %w", err)
		}
		switch job.State {
		case "JobComplete", "Failed", "Aborted":
			return job, nil
		}
		if time.Now().After(deadline) {
			return job, fmt.Errorf("still %s after %s; check Setup > Bulk Data Load Jobs for its outcome", job.State, bulkJobTimeout)
		}
		select {
		case <-ctx.Done():
			d.abortBulkJob(ctx, jobID)
			return job, ctx.Err()
		case <-time.After(bulkPollWait(attempt)):
		}
	}
}

// bulkRequest sends a JSON request to /jobs/ingest[/jobID].
func (d *SalesforceDestination) bulkRequest(ctx context.Context, method, jobID string, body, result interface{}) error {
	path := d.dataPath("/jobs/ingest")
	if jobID != "" {
		path += "/" + jobID
	}
	req := d.client.R(ctx)
	if body != nil {
		req = req.SetBody(body)
	}
	var resp *httpclient.Response
	var err error
	switch method {
	case "POST":
		resp, err = req.Post(path)
	case "PATCH":
		resp, err = req.Patch(path)
	default:
		resp, err = req.Get(path)
	}
	if err != nil {
		return err
	}
	if resp.StatusCode() != 200 && resp.StatusCode() != 201 {
		return parseAPIError(resp)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(resp.Body(), result)
}

// bulkResults streams a job's result CSV, following Sforce-Locator pages.
func (d *SalesforceDestination) bulkResults(ctx context.Context, jobID, kind string, fn func(row map[string]string)) error {
	locator := ""
	for {
		req := d.client.R(ctx).SetHeader("Accept", "text/csv")
		if locator != "" {
			req = req.SetQueryParam("locator", locator)
		}
		resp, err := req.Get(d.dataPath("/jobs/ingest/" + jobID + "/" + kind + "/"))
		if err != nil {
			return fmt.Errorf("salesforce bulk job %s: failed to fetch %s: %w", jobID, kind, err)
		}
		if resp.StatusCode() != 200 {
			return fmt.Errorf("salesforce bulk job %s: failed to fetch %s: %w", jobID, kind, parseAPIError(resp))
		}
		if err := readCSVRows(resp.Body(), fn); err != nil {
			return fmt.Errorf("salesforce bulk job %s: failed to parse %s: %w", jobID, kind, err)
		}
		locator = resp.Header().Get("Sforce-Locator")
		if locator == "" || locator == "null" {
			return nil
		}
	}
}

func readCSVRows(data []byte, fn func(row map[string]string)) error {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		row := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		fn(row)
	}
}

// parseBulkError splits an sf__Error value such as
// "INVALID_FIELD:Foreign key external ID: x not found:Ext_Id__c --" into its parts.
func parseBulkError(raw string) rejection {
	code, rest, ok := strings.Cut(raw, ":")
	if !ok {
		return rejection{code: "BULK_ERROR", message: raw}
	}
	rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "--"))
	rej := rejection{code: code, message: rest}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		rej.message = rest[:i]
		for _, f := range strings.Split(rest[i+1:], ",") {
			if f = strings.TrimSpace(f); f != "" {
				rej.fields = append(rej.fields, f)
			}
		}
	}
	if rej.message == "" {
		rej.message = raw
	}
	return rej
}

// bulkRowKey names a failed row the way REST rejects do. Result rows are not in
// upload order, so the echoed match column is the only reliable link.
func bulkRowKey(sh *shaper, row map[string]string) string {
	for _, field := range []string{sh.idField, recordIDField} {
		if field == "" {
			continue
		}
		if v := row[field]; v != "" && v != bulkNull {
			return field + "=" + v
		}
	}
	return labelKey(sh.labelColumns, func(c string) string {
		if v := row[c]; v != bulkNull {
			return v
		}
		return ""
	})
}
