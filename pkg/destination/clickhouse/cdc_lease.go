package clickhouse

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	cdcLeaseDatabase    = "_bruin_staging"
	cdcLeaseTTL         = 30 * time.Second
	cdcLeaseHeartbeat   = 5 * time.Second
	cdcLeaseCheck       = time.Second
	cdcLeaseCommentHead = "ingestr-cdc-lease:"
)

type clickHouseCDCRunLease struct {
	d         *ClickHouseDestination
	db        string
	name      string
	comment   string
	token     string
	ttl       time.Duration
	heartbeat time.Duration
	check     time.Duration

	lastHeartbeat atomic.Int64
	stopping      atomic.Bool

	done        chan struct{}
	stop        chan struct{}
	stopped     chan struct{}
	doneOnce    sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	err         error
	releaseErr  error
}

func (d *ClickHouseDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	return d.acquireManagedCDCRunLease(ctx, connectorID, cdcLeaseTTL, cdcLeaseHeartbeat, cdcLeaseCheck)
}

func (d *ClickHouseDestination) acquireManagedCDCRunLease(ctx context.Context, connectorID string, ttl, heartbeatInterval, checkInterval time.Duration) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	sum := sha256.Sum256([]byte(connectorID))
	tokenBytes := make([]byte, 12)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	lease := &clickHouseCDCRunLease{
		d:         d,
		db:        cdcLeaseDatabase,
		name:      "cdc_lease_" + hex.EncodeToString(sum[:10]),
		comment:   cdcLeaseCommentHead + token,
		token:     token,
		ttl:       ttl,
		heartbeat: heartbeatInterval,
		check:     checkInterval,
		done:      make(chan struct{}),
		stop:      make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	if err := d.ensureDatabaseExists(ctx, lease.db); err != nil {
		return nil, fmt.Errorf("ensure ClickHouse CDC lease database: %w", err)
	}

	deadline := time.Now().Add(ttl + 15*time.Second)
	for {
		acquired, tookOver, err := lease.tryAcquire(ctx)
		if err != nil {
			return nil, err
		}
		if acquired {
			lease.lastHeartbeat.Store(time.Now().UnixNano())
			go lease.monitor()
			return lease, nil
		}
		if tookOver {
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another MySQL CDC run already owns connector %s (ClickHouse lease %s.%s)", connectorID, lease.db, lease.name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(minDuration(2*time.Second, checkInterval)):
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b || a <= 0 {
		return b
	}
	return a
}

func (l *clickHouseCDCRunLease) ref() string {
	return quoteIdentifier(l.db) + "." + quoteIdentifier(l.name)
}

func (l *clickHouseCDCRunLease) tableRef(name string) string {
	return quoteIdentifier(l.db) + "." + quoteIdentifier(name)
}

func (l *clickHouseCDCRunLease) leaseEngine() string {
	if strings.EqualFold(l.d.engineType, "replicated_merge_tree") || strings.EqualFold(l.d.engineType, "shared_merge_tree") {
		engine, _ := validateEngineType(l.d.engineType, false)
		return engine
	}
	return "MergeTree()"
}

func (l *clickHouseCDCRunLease) tryAcquire(ctx context.Context) (acquired bool, tookOver bool, err error) {
	parts := []string{
		fmt.Sprintf("CREATE TABLE %s%s (`owner` String, `heartbeat` DateTime64(6, 'UTC')) ENGINE = %s ORDER BY (`owner`)",
			l.ref(), onClusterClause(l.d.cluster), l.leaseEngine()),
	}
	if extra := buildEngineSettingsClause(l.d.engineSettings); extra != "" {
		parts = append(parts, extra)
	}
	parts = append(parts, "COMMENT "+sqlString(l.comment))

	createSQL := strings.Join(parts, " ")
	if err := l.d.conn.Exec(ctx, createSQL); err == nil {
		if hbErr := l.recordHeartbeat(ctx); hbErr != nil {
			return false, false, fmt.Errorf("record ClickHouse CDC lease heartbeat: %w", hbErr)
		}
		owner, ownerErr := l.owner(ctx)
		if ownerErr != nil {
			return false, false, ownerErr
		}
		return owner == l.comment, false, nil
	} else if !isTableExistsError(err) {
		return false, false, fmt.Errorf("acquire ClickHouse CDC run lease: %w", err)
	}

	staleOwner, err := l.staleHolder(ctx)
	if err != nil || staleOwner == "" {
		return false, false, err
	}

	dead := l.name + "_expired_" + l.token
	renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s%s", l.ref(), l.tableRef(dead), onClusterClause(l.d.cluster))
	if err := l.d.conn.Exec(ctx, renameSQL); err != nil {
		return false, false, nil
	}
	if moved, err := l.commentOf(ctx, dead); err != nil || moved != staleOwner {
		restoreSQL := fmt.Sprintf("RENAME TABLE %s TO %s%s", l.tableRef(dead), l.ref(), onClusterClause(l.d.cluster))
		if restoreErr := l.d.conn.Exec(ctx, restoreSQL); restoreErr != nil {
			config.Debug("[CLICKHOUSE] Could not restore CDC run lease %s moved during takeover: %v", l.ref(), restoreErr)
		}
		return false, false, nil
	}

	config.Debug("[CLICKHOUSE] Took over expired CDC run lease %s", l.ref())
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s%s", l.tableRef(dead), onClusterClause(l.d.cluster))
	if dropErr := l.d.conn.Exec(ctx, dropSQL); dropErr != nil {
		config.Debug("[CLICKHOUSE] Could not drop expired CDC run lease %s: %v", l.tableRef(dead), dropErr)
	}
	return false, true, nil
}

func (l *clickHouseCDCRunLease) commentOf(ctx context.Context, table string) (string, error) {
	var comment string
	err := l.d.conn.QueryRow(ctx, "SELECT comment FROM system.tables WHERE database = ? AND name = ?", l.db, table).Scan(&comment)
	return comment, err
}

func (l *clickHouseCDCRunLease) owner(ctx context.Context) (string, error) {
	comment, err := l.commentOf(ctx, l.name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		return "", fmt.Errorf("read ClickHouse CDC lease owner: %w", err)
	}
	return comment, nil
}

func (l *clickHouseCDCRunLease) staleHolder(ctx context.Context) (string, error) {
	owner, err := l.owner(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	holder := strings.TrimPrefix(owner, cdcLeaseCommentHead)
	var count uint64
	var ageSec int64
	query := fmt.Sprintf("SELECT count(), toInt64(dateDiff('second', max(heartbeat), now64(6))) FROM %s WHERE owner = ?", l.ref())
	if err := l.d.conn.QueryRow(ctx, query, holder).Scan(&count, &ageSec); err != nil {
		if isMissingTableOrDB(err) {
			return "", nil
		}
		return "", fmt.Errorf("read ClickHouse CDC lease heartbeat: %w", err)
	}
	if count == 0 {
		err = l.d.conn.QueryRow(
			ctx,
			"SELECT toInt64(dateDiff('second', metadata_modification_time, now64(6))) FROM system.tables WHERE database = ? AND name = ?",
			l.db, l.name,
		).Scan(&ageSec)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", nil
			}
			return "", fmt.Errorf("read ClickHouse CDC lease creation time: %w", err)
		}
	}
	if time.Duration(ageSec)*time.Second > l.ttl {
		return owner, nil
	}
	return "", nil
}

func (l *clickHouseCDCRunLease) recordHeartbeat(ctx context.Context) error {
	insertSQL := fmt.Sprintf("INSERT INTO %s (`owner`, `heartbeat`) VALUES (%s, now64(6))", l.ref(), sqlString(l.token))
	return l.d.conn.Exec(ctx, insertSQL)
}

func (l *clickHouseCDCRunLease) monitor() {
	defer close(l.stopped)
	ticker := time.NewTicker(l.check)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
		}
		if l.renewalOverdue() {
			l.markLost(errors.New("the lease could not be renewed in time"))
			return
		}
		var lost error
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		owner, err := l.owner(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			lost = errors.New("the lease table no longer exists")
		case err != nil:
			config.Debug("[CLICKHOUSE] CDC lease check failed: %v", err)
		case owner != l.comment:
			lost = errors.New("the lease was taken over by another run")
		case time.Since(time.Unix(0, l.lastHeartbeat.Load())) >= l.heartbeat:
			if err := l.recordHeartbeat(ctx); err != nil {
				config.Debug("[CLICKHOUSE] CDC lease heartbeat failed: %v", err)
			} else {
				l.lastHeartbeat.Store(time.Now().UnixNano())
			}
		}
		cancel()
		if lost == nil && l.renewalOverdue() {
			lost = errors.New("the lease could not be renewed in time")
		}
		if lost != nil {
			l.markLost(lost)
			return
		}
	}
}

func (l *clickHouseCDCRunLease) markLost(cause error) {
	l.mu.Lock()
	l.err = errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("ClickHouse CDC run lease lost: %w", cause))
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.done) })
}

func (l *clickHouseCDCRunLease) renewalOverdue() bool {
	return time.Since(time.Unix(0, l.lastHeartbeat.Load())) > l.ttl/2
}

func (l *clickHouseCDCRunLease) Done() <-chan struct{} {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
	return l.done
}

func (l *clickHouseCDCRunLease) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *clickHouseCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.stopping.Store(true)
		close(l.stop)
		<-l.stopped
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		owner, err := l.owner(ctx)
		if err != nil || owner != l.comment {
			if err == nil || errors.Is(err, sql.ErrNoRows) {
				err = errors.New("lease was not owned during release")
			}
			l.releaseErr = fmt.Errorf("release ClickHouse CDC run lease: %w", err)
			return
		}

		released := l.name + "_released_" + l.token
		renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s%s", l.ref(), l.tableRef(released), onClusterClause(l.d.cluster))
		if err := l.d.conn.Exec(ctx, renameSQL); err != nil {
			l.releaseErr = fmt.Errorf("release ClickHouse CDC run lease: %w", err)
			return
		}
		if moved, err := l.commentOf(ctx, released); err != nil || moved != l.comment {
			restoreSQL := fmt.Sprintf("RENAME TABLE %s TO %s%s", l.tableRef(released), l.ref(), onClusterClause(l.d.cluster))
			_ = l.d.conn.Exec(ctx, restoreSQL)
			l.releaseErr = errors.New("release ClickHouse CDC run lease: the lease was taken over before release")
			return
		}
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s%s", l.tableRef(released), onClusterClause(l.d.cluster))
		if err := l.d.conn.Exec(ctx, dropSQL); err != nil {
			l.releaseErr = fmt.Errorf("drop released ClickHouse CDC run lease: %w", err)
		}
	})
	return l.releaseErr
}
