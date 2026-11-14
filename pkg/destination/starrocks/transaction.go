package starrocks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"
)

type streamLoadTransaction struct {
	loader    *streamLoader
	database  string
	table     string
	label     string
	options   streamLoadOptions
	id        int64
	prepared  bool
	committed bool
	closed    bool
	// mu guards failed and node: snapshot windows send concurrent loads into one transaction.
	mu     sync.Mutex
	failed error
	// node is the BE the FE routed this transaction to. Loads go there directly, so a busy FE
	// that is slow to answer Expect: 100-continue never receives a body it would only redirect.
	// Other operations still go through the FE: after prepare the BE no longer knows the
	// transaction and only the FE can commit it.
	node string
	// Time spent preparing, committing and waiting for visibility.
	prepareTime, commitTime, publishTime time.Duration
}

type streamLoadTransactionResponse struct {
	TxnID                int64  `json:"TxnId"`
	Status               string `json:"Status"`
	Message              string `json:"Message"`
	ErrorURL             string `json:"ErrorURL"`
	NumberLoadedRows     *int64 `json:"NumberLoadedRows"`
	NumberTotalRows      *int64 `json:"NumberTotalRows"`
	NumberFilteredRows   *int64 `json:"NumberFilteredRows"`
	NumberUnselectedRows *int64 `json:"NumberUnselectedRows"`
}

type streamLoadTransactionError struct {
	operation string
	status    string
	message   string
	cause     error
}

func (e *streamLoadTransactionError) Error() string {
	return fmt.Sprintf("StarRocks transaction %s failed (%s): %s", e.operation, e.status, e.message)
}

func (e *streamLoadTransactionError) Unwrap() error { return e.cause }

func isStreamLoadVersionPressure(err error) bool {
	var transactionError *streamLoadTransactionError
	return errors.As(err, &transactionError) && strings.Contains(strings.ToLower(transactionError.message), "too many versions")
}

func (s *streamLoader) beginTransaction(ctx context.Context, database, table, label string, opts streamLoadOptions) (*streamLoadTransaction, error) {
	tx := &streamLoadTransaction{loader: s, database: database, table: table, label: label, options: opts}
	response, err := tx.request(ctx, "begin", nil)
	if err != nil {
		return tx, err
	}
	if response.TxnID <= 0 {
		return tx, fmt.Errorf("StarRocks transaction begin returned no transaction ID")
	}
	tx.id = response.TxnID
	return tx, nil
}

func (tx *streamLoadTransaction) load(ctx context.Context, body []byte) error {
	tx.mu.Lock()
	failed, open := tx.failed, tx.id > 0 && !tx.prepared && !tx.closed
	tx.mu.Unlock()
	if failed != nil {
		return failed
	}
	if !open {
		return fmt.Errorf("StarRocks transaction %s is not open for loading", tx.label)
	}
	_, err := tx.request(ctx, "load", body)
	if err != nil {
		tx.mu.Lock()
		if tx.failed == nil {
			tx.failed = err
		}
		tx.mu.Unlock()
	}
	return err
}

// commit prepares and commits the transaction and waits until it is visible.
func (tx *streamLoadTransaction) commit(ctx context.Context) error {
	uncertain, err := tx.submit(ctx)
	if err != nil {
		return err
	}
	return tx.awaitVisible(ctx, uncertain)
}

// submit prepares and commits the transaction without waiting for it to become
// visible. A commit whose outcome is unknown, such as after lost responses,
// returns its error as uncertain for awaitVisible to resolve.
func (tx *streamLoadTransaction) submit(ctx context.Context) (uncertain, err error) {
	tx.mu.Lock()
	failed := tx.failed
	tx.mu.Unlock()
	if failed != nil {
		return nil, failed
	}
	if tx.id <= 0 || tx.closed {
		return nil, fmt.Errorf("StarRocks transaction %s is not open for committing", tx.label)
	}
	if !tx.prepared {
		start := time.Now()
		response, err := tx.request(ctx, "prepare", nil)
		tx.prepareTime += time.Since(start)
		if err != nil {
			tx.failed = err
			return nil, err
		}
		tx.prepared = true
		if err := response.checkRows(); err != nil {
			tx.failed = err
			return nil, err
		}
	}
	var commitErr error
	start := time.Now()
	defer func() { tx.commitTime += time.Since(start) }()
	if !tx.committed {
		// Commit is idempotent at the FE. A lost response must never cause
		// another begin or load against this label.
		for attempt := range 3 {
			_, commitErr = tx.request(ctx, "commit", nil)
			if commitErr == nil {
				tx.committed = true
				break
			}
			if ctx.Err() != nil || isStreamLoadVersionPressure(commitErr) {
				return nil, commitErr
			}
			if attempt < 2 {
				timer := time.NewTimer(time.Duration(attempt+1) * 200 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	return commitErr, nil
}

// awaitVisible waits until a submitted transaction is visible, which also
// resolves a commit whose outcome submit could not confirm.
func (tx *streamLoadTransaction) awaitVisible(ctx context.Context, uncertain error) error {
	start := time.Now()
	defer func() { tx.publishTime += time.Since(start) }()
	commitErr := uncertain
	if tx.loader.waitForPublish == nil {
		return fmt.Errorf("StarRocks transaction %d visibility cannot be verified: %w", tx.id, errors.Join(commitErr, errors.New("no visibility checker")))
	}
	if err := tx.loader.waitForPublish(ctx, tx.database, tx.id); err != nil {
		return fmt.Errorf("StarRocks transaction %d commit visibility unresolved: %w", tx.id, errors.Join(commitErr, err))
	}
	tx.committed = true
	tx.closed = true
	return nil
}

func (tx *streamLoadTransaction) rollback(ctx context.Context) error {
	if tx.committed {
		return fmt.Errorf("StarRocks transaction %d is already committed", tx.id)
	}
	if tx.closed {
		return nil
	}
	if _, err := tx.request(ctx, "rollback", nil); err != nil {
		return err
	}
	tx.closed = true
	return nil
}

// confirmAborted permits replay only when the transaction cannot publish rows.
func (tx *streamLoadTransaction) confirmAborted(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tx == nil || tx.id <= 0 || tx.committed {
		return false, nil
	}
	if tx.closed {
		return true, nil
	}
	if tx.loader.transactionState == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	state, err := tx.loader.transactionState(ctx, tx.database, tx.id)
	if err != nil {
		return false, fmt.Errorf("verify StarRocks transaction %d abort: %w", tx.id, err)
	}
	if state != "ABORTED" {
		return false, nil
	}
	tx.closed = true
	return true, nil
}

func (response streamLoadTransactionResponse) checkRows() error {
	if response.NumberLoadedRows == nil || response.NumberTotalRows == nil || response.NumberFilteredRows == nil || response.NumberUnselectedRows == nil {
		return fmt.Errorf("StarRocks transaction prepare returned incomplete row counts")
	}
	loaded, total := *response.NumberLoadedRows, *response.NumberTotalRows
	filtered, unselected := *response.NumberFilteredRows, *response.NumberUnselectedRows
	if loaded < 0 || total < 0 || filtered != 0 || unselected != 0 || loaded != total {
		return fmt.Errorf("StarRocks transaction did not load every row: loaded=%d total=%d filtered=%d unselected=%d", loaded, total, filtered, unselected)
	}
	return nil
}

func (tx *streamLoadTransaction) loadEndpoint() string {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.node != "" {
		return tx.node
	}
	return tx.loader.endpoint
}

func (tx *streamLoadTransaction) setNode(node string) {
	tx.mu.Lock()
	tx.node = node
	tx.mu.Unlock()
}

func (tx *streamLoadTransaction) request(ctx context.Context, operation string, body []byte) (streamLoadTransactionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	method := http.MethodPost
	if operation == "load" {
		method = http.MethodPut
	}
	url := tx.loader.endpoint + "/api/transaction/" + operation
	if operation == "load" {
		url = tx.loadEndpoint() + "/api/transaction/load"
	}
	var response streamLoadTransactionResponse
	for hop := 0; hop < 4; hop++ {
		request, err := tx.loader.newRequest(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return response, err
		}
		request.Header.Set("Expect", "100-continue")
		request.Header.Set("db", tx.database)
		request.Header.Set("table", tx.table)
		request.Header.Set("label", tx.label)
		if operation == "begin" {
			request.Header.Set("timeout", "300")
			request.Header.Set("idle_transaction_timeout", "120")
		}
		if operation == "load" {
			request.Header.Set("format", "json")
			request.Header.Set("strip_outer_array", "true")
			// StarRocks applies the JSON size guard to the cumulative
			// transaction size; each request is bounded by the encoder.
			request.Header.Set("ignore_json_size", "true")
			request.Header.Set("strict_mode", "true")
			request.Header.Set("max_filter_ratio", "0")
			if tx.options.columns != "" {
				request.Header.Set("columns", tx.options.columns)
			}
			if tx.options.jsonpaths != "" {
				request.Header.Set("jsonpaths", tx.options.jsonpaths)
			}
			if tx.options.mergeCondition != "" {
				request.Header.Set("merge_condition", tx.options.mergeCondition)
			}
		}
		result, err := tx.loader.client.Do(request)
		if err != nil {
			return response, &streamLoadTransactionError{operation: operation, status: "transport", message: err.Error(), cause: err}
		}
		if isRedirect(result.StatusCode) {
			location := result.Header.Get("Location")
			_ = result.Body.Close()
			if location == "" {
				return response, fmt.Errorf("StarRocks transaction %s redirect without a Location header", operation)
			}
			next, err := neturl.Parse(location)
			if err != nil {
				return response, fmt.Errorf("parse transaction redirect: %w", err)
			}
			target := request.URL.ResolveReference(next)
			if operation == "begin" || operation == "load" {
				tx.setNode(target.Scheme + "://" + target.Host)
			}
			url = target.String()
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(result.Body, 1<<20))
		_ = result.Body.Close()
		if readErr != nil {
			return response, &streamLoadTransactionError{operation: operation, status: "transport", message: readErr.Error(), cause: readErr}
		}
		if result.StatusCode != http.StatusOK {
			return response, &streamLoadTransactionError{operation: operation, status: result.Status, message: string(data)}
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return response, fmt.Errorf("parse StarRocks transaction %s response: %w", operation, err)
		}
		if response.Status != "OK" {
			message := response.Message
			if response.ErrorURL != "" {
				if details := tx.loader.errorLog(ctx, response.ErrorURL, url); details != "" {
					message += ": " + details
				}
			}
			return response, &streamLoadTransactionError{operation: operation, status: response.Status, message: message}
		}
		if tx.id > 0 && response.TxnID > 0 && tx.id != response.TxnID {
			return response, fmt.Errorf("StarRocks transaction %s returned transaction ID %d, expected %d", operation, response.TxnID, tx.id)
		}
		return response, nil
	}
	return response, fmt.Errorf("StarRocks transaction %s exceeded redirect limit", operation)
}
