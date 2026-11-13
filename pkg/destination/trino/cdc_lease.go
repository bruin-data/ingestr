package trino

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	trinoCDCLeaseSchema     = "_bruin_staging"
	trinoCDCLeaseTTL        = 30 * time.Second
	trinoCDCLeaseHeartbeat  = 10 * time.Second
	trinoCDCLeaseCheck      = 2 * time.Second
	trinoCDCLeasePoll       = 2 * time.Second
	trinoCDCTargetLockPoll  = 200 * time.Millisecond
	trinoCDCTargetLockLabel = "target lock"
)

type trinoCDCRunLease struct {
	d          *TrinoDestination
	catalog    string
	schema     string
	name       string
	token      string
	label      string
	notRunLoss bool

	ttl         time.Duration
	heartbeatIv time.Duration
	checkIv     time.Duration
	pollIv      time.Duration

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

func (d *TrinoDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	return d.acquireManagedCDCRunLeaseWithTimings(
		ctx,
		connectorID,
		trinoCDCLeaseTTL,
		trinoCDCLeaseHeartbeat,
		trinoCDCLeaseCheck,
		trinoCDCLeasePoll,
	)
}

func (d *TrinoDestination) acquireManagedCDCRunLeaseWithTimings(
	ctx context.Context,
	connectorID string,
	ttl, heartbeatIv, checkIv, pollIv time.Duration,
) (source.ConnectorLease, error) {
	if d.db == nil {
		return nil, errors.New("trino destination is not connected")
	}
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	if d.catalog == "" {
		return nil, errors.New("trino managed CDC run lease requires a catalog")
	}

	sum := sha256.Sum256([]byte(connectorID))
	return d.acquireTrinoTableLease(
		ctx,
		d.catalog,
		"cdc_lease_"+hex.EncodeToString(sum[:10]),
		"CDC run lease",
		false,
		fmt.Sprintf("another CDC run already owns connector %s", connectorID),
		ttl,
		heartbeatIv,
		checkIv,
		pollIv,
	)
}

type trinoTargetLockSpec struct {
	canonicalKey string
	catalog      string
	table        string
}

func (d *TrinoDestination) acquireCDCTargetLocks(
	ctx context.Context,
	tables ...string,
) (func() error, func() error, error) {
	return d.acquireCDCTargetLocksWithTimings(
		ctx,
		trinoCDCLeaseTTL,
		trinoCDCLeaseHeartbeat,
		trinoCDCLeaseCheck,
		trinoCDCTargetLockPoll,
		tables...,
	)
}

func (d *TrinoDestination) acquireCDCTargetLocksWithTimings(
	ctx context.Context,
	ttl, heartbeatIv, checkIv, pollIv time.Duration,
	tables ...string,
) (func() error, func() error, error) {
	if d.db == nil {
		return nil, nil, errors.New("trino destination is not connected")
	}
	seen := make(map[string]struct{}, len(tables))
	specs := make([]trinoTargetLockSpec, 0, len(tables))
	for _, table := range tables {
		catalog, schemaName, tableName, err := d.resolveTableName(table)
		if err != nil {
			return nil, nil, err
		}
		key := destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		specs = append(specs, trinoTargetLockSpec{
			canonicalKey: key,
			catalog:      catalog,
			table:        table,
		})
	}
	slices.SortFunc(specs, func(a, b trinoTargetLockSpec) int {
		return strings.Compare(a.canonicalKey, b.canonicalKey)
	})

	acquired := make([]*trinoCDCRunLease, 0, len(specs))
	releaseAll := func() error {
		var errs []error
		for i := len(acquired) - 1; i >= 0; i-- {
			if err := acquired[i].Release(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	for _, spec := range specs {
		leaseIface, err := d.acquireTrinoTableLease(
			ctx,
			spec.catalog,
			"cdc_tlock_"+destination.CDCTargetKeyDigest(spec.canonicalKey),
			trinoCDCTargetLockLabel,
			true,
			fmt.Sprintf("another operation is mutating Trino target %s", spec.table),
			ttl,
			heartbeatIv,
			checkIv,
			pollIv,
		)
		if err != nil {
			_ = releaseAll()
			return nil, nil, err
		}
		acquired = append(acquired, leaseIface.(*trinoCDCRunLease))
	}

	verifyHeld := func() error {
		for _, l := range acquired {
			select {
			case <-l.Done():
				return l.Err()
			default:
			}
		}
		return nil
	}
	return releaseAll, verifyHeld, nil
}

func (d *TrinoDestination) acquireTrinoTableLease(
	ctx context.Context,
	catalog, name, label string,
	notRunLoss bool,
	timeoutReason string,
	ttl, heartbeatIv, checkIv, pollIv time.Duration,
) (source.ConnectorLease, error) {
	var token string
	if d.newLeaseToken != nil {
		var err error
		token, err = d.newLeaseToken()
		if err != nil {
			return nil, fmt.Errorf("generate Trino %s token: %w", label, err)
		}
	} else {
		tokenBytes := make([]byte, 12)
		if _, err := rand.Read(tokenBytes); err != nil {
			return nil, fmt.Errorf("generate Trino %s token: %w", label, err)
		}
		token = hex.EncodeToString(tokenBytes)
	}
	lease := &trinoCDCRunLease{
		d:           d,
		catalog:     catalog,
		schema:      trinoCDCLeaseSchema,
		name:        name,
		token:       token,
		label:       label,
		notRunLoss:  notRunLoss,
		ttl:         ttl,
		heartbeatIv: heartbeatIv,
		checkIv:     checkIv,
		pollIv:      pollIv,
		done:        make(chan struct{}),
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
	}

	if err := d.ensureSchemaExists(ctx, lease.catalog, lease.schema); err != nil {
		return nil, fmt.Errorf("ensure Trino %s schema: %w", label, err)
	}

	deadline := time.Now().Add(lease.ttl + 15*time.Second)
	for {
		acquired, err := lease.tryAcquire(ctx)
		if err != nil {
			return nil, err
		}
		if acquired {
			lease.lastHeartbeat.Store(time.Now().UnixNano())
			go lease.monitor()
			return lease, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s (Trino lease %s)", timeoutReason, lease.fqn())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lease.pollIv):
		}
	}
}

func (l *trinoCDCRunLease) fqn() string {
	return qualifyTrinoTable(l.catalog, l.schema, l.name)
}

func (l *trinoCDCRunLease) tryAcquire(ctx context.Context) (bool, error) {
	createSQL := fmt.Sprintf(
		`CREATE TABLE %s AS SELECT CAST(%s AS VARCHAR) AS %s, CURRENT_TIMESTAMP AS %s`,
		l.fqn(),
		escapeString(l.token),
		quoteIdentifier("owner"),
		quoteIdentifier("heartbeat"),
	)
	_, err := l.d.db.ExecContext(ctx, createSQL)
	if err == nil {
		owner, err := l.owner(ctx)
		if err != nil {
			if isTrinoMissingObject(err) {
				return false, nil
			}
			return false, err
		}
		return owner == l.token, nil
	}
	if !isTrinoAlreadyExists(err) {
		if isTrinoCommitConflict(err) {
			return false, nil
		}
		config.LogFailedQuery(createSQL, err)
		return false, fmt.Errorf("acquire Trino CDC run lease: %w", err)
	}

	staleOwner, err := l.staleHolder(ctx)
	if err != nil || staleOwner == "" {
		return false, err
	}

	deadFQN := qualifyTrinoTable(l.catalog, l.schema, l.name+"_expired_"+l.token)
	renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", l.fqn(), deadFQN)
	if _, err := l.d.db.ExecContext(ctx, renameSQL); err != nil {
		return false, nil
	}

	movedOwner, movedAge, err := l.holderStatus(ctx, deadFQN)
	if err != nil || movedOwner != staleOwner || movedAge <= int64(l.ttl.Seconds()) {
		restoreSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", deadFQN, l.fqn())
		if _, restoreErr := l.d.db.ExecContext(context.WithoutCancel(ctx), restoreSQL); restoreErr != nil {
			config.Debug("[TRINO] Could not restore CDC run lease %s moved during takeover: %v", l.fqn(), restoreErr)
		}
		return false, nil
	}

	config.Debug("[TRINO] Took over expired CDC run lease %s", l.fqn())
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, _ = l.d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+deadFQN)
	return false, nil
}

func (l *trinoCDCRunLease) staleHolder(ctx context.Context) (string, error) {
	owner, ageSeconds, err := l.holderStatus(ctx, l.fqn())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isTrinoMissingObject(err) {
			return "", nil
		}
		return "", err
	}
	if ageSeconds > int64(l.ttl.Seconds()) {
		return owner, nil
	}
	return "", nil
}

func (l *trinoCDCRunLease) holderStatus(ctx context.Context, tableFQN string) (string, int64, error) {
	query := fmt.Sprintf(
		`SELECT %s, CAST(date_diff('second', MAX(%s), CURRENT_TIMESTAMP) AS BIGINT) FROM %s GROUP BY %s`,
		quoteIdentifier("owner"),
		quoteIdentifier("heartbeat"),
		tableFQN,
		quoteIdentifier("owner"),
	)
	var owner string
	var ageSeconds sql.NullInt64
	err := l.d.db.QueryRowContext(ctx, query).Scan(&owner, &ageSeconds)
	if err != nil {
		return "", 0, err
	}
	if !ageSeconds.Valid {
		return "", 0, sql.ErrNoRows
	}
	return owner, ageSeconds.Int64, nil
}

func (l *trinoCDCRunLease) owner(ctx context.Context) (string, error) {
	return l.ownerOf(ctx, l.fqn())
}

func (l *trinoCDCRunLease) ownerOf(ctx context.Context, tableFQN string) (string, error) {
	query := fmt.Sprintf("SELECT MAX(%s) FROM %s", quoteIdentifier("owner"), tableFQN)
	var owner sql.NullString
	err := l.d.db.QueryRowContext(ctx, query).Scan(&owner)
	if err != nil {
		return "", err
	}
	if !owner.Valid || owner.String == "" {
		return "", sql.ErrNoRows
	}
	return owner.String, nil
}

func (l *trinoCDCRunLease) heartbeat(ctx context.Context) error {
	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s) VALUES (%s, CURRENT_TIMESTAMP)",
		l.fqn(),
		quoteIdentifier("owner"),
		quoteIdentifier("heartbeat"),
		escapeString(l.token),
	)
	_, err := l.d.db.ExecContext(ctx, query)
	return err
}

func (l *trinoCDCRunLease) monitor() {
	defer close(l.stopped)
	ticker := time.NewTicker(l.checkIv)
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
		case errors.Is(err, sql.ErrNoRows) || isTrinoMissingObject(err):
			lost = errors.New("the lease table no longer exists")
		case err != nil:
			config.Debug("[TRINO] CDC lease check failed: %v", err)
		case owner != l.token:
			lost = errors.New("the lease was taken over by another run")
		case time.Since(time.Unix(0, l.lastHeartbeat.Load())) >= l.heartbeatIv:
			if err := l.heartbeat(ctx); err != nil {
				config.Debug("[TRINO] CDC lease heartbeat failed: %v", err)
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

func (l *trinoCDCRunLease) markLost(cause error) {
	l.mu.Lock()
	if l.notRunLoss {
		l.err = fmt.Errorf("trino %s %s lost: %w", l.label, l.fqn(), cause)
	} else {
		l.err = errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("trino CDC run lease lost: %w", cause))
	}
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.done) })
}

func (l *trinoCDCRunLease) renewalOverdue() bool {
	return time.Since(time.Unix(0, l.lastHeartbeat.Load())) > l.ttl/2
}

func (l *trinoCDCRunLease) Done() <-chan struct{} {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
	return l.done
}

func (l *trinoCDCRunLease) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *trinoCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.stopping.Store(true)
		close(l.stop)
		<-l.stopped
		leaseDesc := "CDC run lease"
		if l.notRunLoss && l.label != "" {
			leaseDesc = l.label
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		owner, err := l.owner(ctx)
		if err != nil || owner != l.token {
			if err == nil || errors.Is(err, sql.ErrNoRows) || isTrinoMissingObject(err) {
				err = errors.New("lease was not owned during release")
			}
			l.releaseErr = fmt.Errorf("release Trino %s: %w", leaseDesc, err)
			return
		}
		releasedFQN := qualifyTrinoTable(l.catalog, l.schema, l.name+"_released_"+l.token)
		if _, err := l.d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", l.fqn(), releasedFQN)); err != nil {
			l.releaseErr = fmt.Errorf("release Trino %s: %w", leaseDesc, err)
			return
		}
		if moved, err := l.ownerOf(ctx, releasedFQN); err != nil || moved != l.token {
			_, _ = l.d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", releasedFQN, l.fqn()))
			l.releaseErr = fmt.Errorf("release Trino %s: the lease was taken over before release", leaseDesc)
			return
		}
		if _, err := l.d.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+releasedFQN); err != nil {
			l.releaseErr = fmt.Errorf("drop released Trino %s: %w", leaseDesc, err)
		}
	})
	return l.releaseErr
}
