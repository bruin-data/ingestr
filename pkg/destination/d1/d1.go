package d1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	apiBaseURL    = "https://api.cloudflare.com/client/v4"
	maxParameters = 100
	maxSQLBytes   = 100000
	maxRetries    = 5
)

type D1Destination struct {
	endpoint string
	token    string
	client   *http.Client
	limiter  *rate.Limiter
}

type statement struct {
	SQL    string        `json:"sql"`
	Params []interface{} `json:"params,omitempty"`
}

type queryResult struct {
	Success bool                     `json:"success"`
	Results []map[string]interface{} `json:"results"`
	Error   string                   `json:"error"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type queryResponse struct {
	Success bool          `json:"success"`
	Errors  []apiError    `json:"errors"`
	Result  []queryResult `json:"result"`
}

func NewD1Destination() *D1Destination {
	return &D1Destination{
		client:  &http.Client{Timeout: 45 * time.Second},
		limiter: rate.NewLimiter(4, 1),
	}
}

func parseURI(raw string) (accountID, databaseID, token string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid D1 URI")
	}
	if u.Scheme != "d1" && u.Scheme != "cloudflare-d1" {
		return "", "", "", fmt.Errorf("D1 URI must start with d1:// or cloudflare-d1://")
	}
	if u.User != nil || u.Port() != "" || u.Fragment != "" || u.Opaque != "" {
		return "", "", "", fmt.Errorf("D1 URI must have the form d1://<account_id>/<database_id>?api_token=<token>")
	}
	accountID, databaseID = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	validID := func(id string) bool {
		if id == "" {
			return false
		}
		for _, c := range id {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
		return true
	}
	if !validID(accountID) || !validID(databaseID) {
		return "", "", "", fmt.Errorf("D1 URI requires an account ID and database ID, not a database name or SQL schema")
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid D1 URI query parameters")
	}
	for name, values := range params {
		if name != "api_token" || len(values) != 1 {
			return "", "", "", fmt.Errorf("D1 URI accepts only one api_token query parameter")
		}
	}
	token = params.Get("api_token")
	if strings.TrimSpace(token) == "" {
		return "", "", "", fmt.Errorf("api_token is required in D1 URI")
	}
	return accountID, databaseID, token, nil
}

func (d *D1Destination) Connect(ctx context.Context, raw string) error {
	accountID, databaseID, token, err := parseURI(raw)
	if err != nil {
		return err
	}
	d.endpoint = apiBaseURL + "/accounts/" + accountID + "/d1/database/" + databaseID + "/query"
	d.token = token
	if _, err := d.query(ctx, "SELECT 1"); err != nil {
		return fmt.Errorf("failed to connect to Cloudflare D1: %w", err)
	}
	return nil
}

func (d *D1Destination) Close(context.Context) error {
	if d.client != nil {
		d.client.CloseIdleConnections()
	}
	return nil
}

func (d *D1Destination) query(ctx context.Context, sql string, params ...interface{}) ([]queryResult, error) {
	return d.send(ctx, []statement{{SQL: sql, Params: params}})
}

func (d *D1Destination) executeBatch(ctx context.Context, statements []statement) error {
	if len(statements) == 0 {
		return nil
	}
	_, err := d.send(ctx, statements)
	return err
}

func (d *D1Destination) send(ctx context.Context, statements []statement) ([]queryResult, error) {
	if d.endpoint == "" || d.client == nil {
		return nil, fmt.Errorf("D1 destination is not connected")
	}
	for _, stmt := range statements {
		if len(stmt.Params) > maxParameters {
			return nil, fmt.Errorf("D1 query exceeds %d bound parameters", maxParameters)
		}
		if len(stmt.SQL) > maxSQLBytes {
			return nil, fmt.Errorf("D1 query exceeds %d bytes of SQL", maxSQLBytes)
		}
	}
	var payload interface{} = struct {
		Batch []statement `json:"batch"`
	}{statements}
	if len(statements) == 1 {
		payload = statements[0]
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode D1 query: %w", err)
	}
	for attempt := 0; ; attempt++ {
		if d.limiter != nil {
			if err := d.limiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("waiting for D1 rate limit: %w", err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create D1 request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+d.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := d.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("D1 request failed (write outcome may be unknown): %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read D1 response (write outcome may be unknown): %w", readErr)
		}
		// A 429 rejects the request before execution. Other failures can follow a
		// committed write, so replaying them could duplicate rows.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		var result queryResponse
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		decodeErr := decoder.Decode(&result)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success || len(result.Errors) > 0 {
			var messages []string
			for _, e := range result.Errors {
				messages = append(messages, fmt.Sprintf("%d: %s", e.Code, e.Message))
			}
			if len(messages) == 0 {
				messages = append(messages, "request was not successful")
			}
			message := strings.Join(messages, "; ")
			if d.token != "" {
				message = strings.ReplaceAll(message, d.token, "[REDACTED]")
			}
			return nil, fmt.Errorf("D1 API error (HTTP %d): %s", resp.StatusCode, message)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("invalid D1 response: %w", decodeErr)
		}
		if len(result.Result) < len(statements) {
			return nil, fmt.Errorf("D1 returned %d query results for %d statements", len(result.Result), len(statements))
		}
		for i, item := range result.Result {
			if !item.Success {
				message := item.Error
				if d.token != "" {
					message = strings.ReplaceAll(message, d.token, "[REDACTED]")
				}
				return nil, fmt.Errorf("D1 query %d failed: %s", i+1, message)
			}
		}
		return result.Result, nil
	}
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 && seconds <= 86400 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		if delay := time.Until(date); delay > 0 {
			return delay
		}
	}
	return time.Second << attempt
}
