package trino

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
)

const (
	transactionIDHeader        = "X-Trino-Transaction-Id"
	startedTransactionIDHeader = "X-Trino-Started-Transaction-Id"
	clearTransactionIDHeader   = "X-Trino-Clear-Transaction-Id"
	noTransactionID            = "NONE"
)

type transactionToken uint64

type transactionContextKey struct{}

type transactionRegistry struct {
	mu      sync.RWMutex
	next    transactionToken
	entries map[transactionToken]string
}

func newTransactionRegistry() *transactionRegistry {
	return &transactionRegistry{entries: make(map[transactionToken]string)}
}

func (r *transactionRegistry) begin(ctx context.Context) (context.Context, transactionToken) {
	r.mu.Lock()
	r.next++
	token := r.next
	r.entries[token] = noTransactionID
	r.mu.Unlock()
	return r.withToken(ctx, token), token
}

func (r *transactionRegistry) withToken(ctx context.Context, token transactionToken) context.Context {
	return context.WithValue(ctx, transactionContextKey{}, token)
}

func (r *transactionRegistry) transactionID(token transactionToken) (string, bool) {
	r.mu.RLock()
	id, ok := r.entries[token]
	r.mu.RUnlock()
	return id, ok
}

func (r *transactionRegistry) setTransactionID(token transactionToken, id string) {
	r.mu.Lock()
	if _, ok := r.entries[token]; ok {
		r.entries[token] = id
	}
	r.mu.Unlock()
}

func (r *transactionRegistry) remove(token transactionToken) {
	r.mu.Lock()
	delete(r.entries, token)
	r.mu.Unlock()
}

type transactionRoundTripper struct {
	base     http.RoundTripper
	registry *transactionRegistry
}

// trino-go-client does not propagate Trino transaction headers. The context
// token keeps concurrent logical transactions isolated on the shared client.
func (t *transactionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token, transactional := req.Context().Value(transactionContextKey{}).(transactionToken)
	if transactional {
		if id, ok := t.registry.transactionID(token); ok {
			req = req.Clone(req.Context())
			req.Header.Set(transactionIDHeader, id)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil || !transactional {
		return resp, err
	}
	if id := resp.Header.Get(startedTransactionIDHeader); id != "" {
		t.registry.setTransactionID(token, id)
	}
	if resp.Header.Get(clearTransactionIDHeader) != "" {
		t.registry.remove(token)
	}
	return resp, nil
}

type trinoTransaction struct {
	mu       sync.Mutex
	conn     *sql.Conn
	registry *transactionRegistry
	token    transactionToken
	done     bool
}

func (t *trinoTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return sql.ErrTxDone
	}
	_, err := t.conn.ExecContext(t.registry.withToken(ctx, t.token), query, args...)
	if err != nil {
		config.LogFailedQuery(query, err)
	}
	return err
}

func (t *trinoTransaction) Commit(ctx context.Context) error {
	return t.finish(ctx, "COMMIT")
}

func (t *trinoTransaction) Rollback(ctx context.Context) error {
	return t.finish(ctx, "ROLLBACK")
}

func (t *trinoTransaction) finish(ctx context.Context, query string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return sql.ErrTxDone
	}

	_, execErr := t.conn.ExecContext(t.registry.withToken(ctx, t.token), query)
	t.done = true
	t.registry.remove(t.token)
	closeErr := t.conn.Close()
	if execErr != nil {
		config.LogFailedQuery(query, execErr)
		return execErr
	}
	if closeErr != nil {
		return fmt.Errorf("failed to release Trino transaction connection: %w", closeErr)
	}
	return nil
}

func (d *TrinoDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	if d.db == nil {
		return nil, errors.New("trino destination is not connected")
	}
	if d.transactions == nil {
		return nil, errors.New("trino transaction tracking is not configured")
	}

	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire Trino transaction connection: %w", err)
	}
	txCtx, token := d.transactions.begin(ctx)
	if _, err := conn.ExecContext(txCtx, "START TRANSACTION READ WRITE"); err != nil {
		d.transactions.remove(token)
		_ = conn.Close()
		return nil, fmt.Errorf("failed to start Trino transaction: %w", err)
	}
	if id, ok := d.transactions.transactionID(token); !ok || id == noTransactionID {
		d.transactions.remove(token)
		_ = conn.Close()
		return nil, errors.New("failed to start Trino transaction: server did not return a transaction ID")
	}

	return &trinoTransaction{
		conn:     conn,
		registry: d.transactions,
		token:    token,
	}, nil
}
