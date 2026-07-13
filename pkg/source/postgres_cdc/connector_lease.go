package postgres_cdc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const connectorLeaseReleaseTimeout = 5 * time.Second

type postgresCDCLease struct {
	mu            sync.Mutex
	conn          *pgx.Conn
	keys          []int64
	sharedKeys    []int64
	onRelease     func()
	done          chan struct{}
	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
	lossErr       error
	releasing     bool
	released      bool
	releaseDone   chan struct{}
	releaseErr    error
}

func (s *PostgresCDCSource) AcquireConnectorLease(ctx context.Context, opts source.ConnectorLeaseOptions) (source.ConnectorLease, error) {
	if opts.ConnectorID == "" {
		return nil, fmt.Errorf("connector ID is empty")
	}
	if s.queryPool == nil {
		return nil, fmt.Errorf("postgres CDC source is not connected")
	}

	s.connectorLeaseMu.Lock()
	defer s.connectorLeaseMu.Unlock()
	if s.connectorPreparing {
		return nil, fmt.Errorf("cannot acquire PostgreSQL connector lease while managed publication %q preparation is in progress", s.cdcConfig.Publication)
	}
	if s.connectorLease != nil {
		return nil, fmt.Errorf("connector lease is already held by this source")
	}
	s.legacySlots = make(map[string]bool)

	conn, err := pgx.ConnectConfig(ctx, s.queryPool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("failed to open PostgreSQL session for connector lease: %w", err)
	}
	keys := []int64{connectorLeaseKey("connector:" + opts.ConnectorID)}
	for _, connectorID := range priorConnectorIDs(opts.PreviousConnectorID, opts.PreviousConnectorIDs) {
		if connectorID != opts.ConnectorID {
			keys = append(keys, connectorLeaseKey("connector:"+connectorID))
		}
	}
	slotName := s.cdcConfig.SlotName
	type slotCandidate struct {
		name        string
		suffix      string
		unambiguous bool
		key         int64
	}
	var legacySlots []slotCandidate
	if slotName == "" {
		previousSuffixes := priorSlotSuffixes(opts.PreviousSlotSuffix, opts.PreviousSlotSuffixes)
		if opts.SourceTable == "" {
			slotName = generateMultiTableSlotName(s.cdcConfig.Publication, opts.SlotSuffix)
			for _, suffix := range previousSuffixes {
				name := generateMultiTableSlotName(s.cdcConfig.Publication, suffix)
				legacySlots = append(legacySlots, slotCandidate{name: name, suffix: suffix, unambiguous: true})
			}
			if opts.LegacySlotSuffix != "" {
				name := generateLegacyMultiTableSlotName(s.cdcConfig.Publication, opts.LegacySlotSuffix)
				legacySlots = append(legacySlots, slotCandidate{name: name, suffix: opts.LegacySlotSuffix, unambiguous: legacySlotNameUnambiguous(name, opts.LegacySlotSuffix)})
			}
		} else {
			slotName = generateSlotName(opts.SourceTable, s.cdcConfig.Publication, opts.SlotSuffix)
			for _, suffix := range previousSuffixes {
				name := generateSlotName(opts.SourceTable, s.cdcConfig.Publication, suffix)
				legacySlots = append(legacySlots, slotCandidate{name: name, suffix: suffix, unambiguous: true})
			}
			if opts.LegacySlotSuffix != "" {
				name := generateLegacySlotName(opts.SourceTable, s.cdcConfig.Publication, opts.LegacySlotSuffix)
				legacySlots = append(legacySlots, slotCandidate{name: name, suffix: opts.LegacySlotSuffix, unambiguous: legacySlotNameUnambiguous(name, opts.LegacySlotSuffix)})
			}
		}
	}
	slotKey := connectorLeaseKey("slot:" + slotName)
	keys = append(keys, slotKey)
	legacyByKey := make(map[int64]string)
	seenSlots := map[string]bool{"": true, slotName: true}
	filteredLegacySlots := legacySlots[:0]
	for _, candidate := range legacySlots {
		if seenSlots[candidate.name] {
			continue
		}
		seenSlots[candidate.name] = true
		candidate.key = connectorLeaseKey("slot:" + candidate.name)
		legacyByKey[candidate.key] = candidate.name
		keys = append(keys, candidate.key)
		filteredLegacySlots = append(filteredLegacySlots, candidate)
	}
	legacySlots = filteredLegacySlots
	for _, key := range keys {
		var acquired bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
			_ = conn.Close(context.Background())
			return nil, fmt.Errorf("failed to acquire PostgreSQL connector advisory lock: %w", err)
		}
		if !acquired {
			_ = conn.Close(context.Background())
			if key == slotKey {
				return nil, fmt.Errorf("replication slot %s is already used by another connector", slotName)
			}
			if legacyName := legacyByKey[key]; legacyName != "" {
				return nil, fmt.Errorf("legacy replication slot %s is already used by another connector", legacyName)
			}
			return nil, fmt.Errorf("connector %s is already running", opts.ConnectorID)
		}
	}
	for _, candidate := range legacySlots {
		var active bool
		err := conn.QueryRow(ctx, "SELECT active FROM pg_replication_slots WHERE slot_name = $1 AND slot_type = 'logical'", candidate.name).Scan(&active)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			_ = conn.Close(context.Background())
			return nil, fmt.Errorf("failed to inspect legacy replication slot %s: %w", candidate.name, err)
		}
		if err == nil && !candidate.unambiguous {
			_ = conn.Close(context.Background())
			return nil, fmt.Errorf("legacy replication slot %s has an ambiguous truncated name that does not retain connector suffix %s; configure an explicit slot or complete the migration manually", candidate.name, candidate.suffix)
		}
		if err == nil && active {
			_ = conn.Close(context.Background())
			return nil, fmt.Errorf("legacy replication slot %s is active; stop the older connector before upgrading", candidate.name)
		}
		if err == nil && candidate.unambiguous {
			s.legacySlots[candidate.name] = false
		}
	}
	publicationKey := int64(0)
	if s.managedPublication {
		publicationKey = publicationMigrationLeaseKey(s.connectorIdentity.Database, s.cdcConfig.Publication)
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock_shared($1)", publicationKey); err != nil {
			_ = conn.Close(context.Background())
			return nil, fmt.Errorf("failed to acquire PostgreSQL publication lease: %w", err)
		}
	}

	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	lease := &postgresCDCLease{
		conn: conn, keys: keys, done: make(chan struct{}),
		monitorCancel: monitorCancel, monitorDone: make(chan struct{}),
	}
	if publicationKey != 0 {
		lease.sharedKeys = []int64{publicationKey}
	}
	lease.onRelease = func() {
		s.connectorLeaseMu.Lock()
		if s.connectorLease == lease {
			s.connectorLease = nil
		}
		s.connectorLeaseMu.Unlock()
	}
	s.connectorLease = lease
	go lease.monitor(monitorCtx)
	return lease, nil
}

func (l *postgresCDCLease) Done() <-chan struct{} {
	return l.done
}

func (l *postgresCDCLease) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lossErr
}

func (l *postgresCDCLease) monitor(ctx context.Context) {
	defer close(l.monitorDone)
	for {
		_, err := l.conn.WaitForNotification(ctx)
		if err == nil {
			continue
		}
		l.mu.Lock()
		if !l.releasing && !l.released {
			l.lossErr = fmt.Errorf("PostgreSQL connector lease session was lost: %w", err)
			close(l.done)
		}
		l.mu.Unlock()
		return
	}
}

func (l *postgresCDCLease) Release() error {
	l.mu.Lock()
	if l.released {
		err := l.releaseErr
		l.mu.Unlock()
		return err
	}
	if l.releasing {
		done := l.releaseDone
		l.mu.Unlock()
		<-done
		l.mu.Lock()
		err := l.releaseErr
		l.mu.Unlock()
		return err
	}
	l.releasing = true
	l.releaseDone = make(chan struct{})
	conn := l.conn
	cancel := l.monitorCancel
	monitorDone := l.monitorDone
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if monitorDone != nil {
		<-monitorDone
	}

	l.mu.Lock()
	lossErr := l.lossErr
	l.mu.Unlock()
	if l.onRelease != nil {
		defer l.onRelease()
	}
	if conn == nil {
		return l.finishRelease(lossErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectorLeaseReleaseTimeout)
	defer cancel()
	var unlockErr error
	for i := len(l.sharedKeys) - 1; i >= 0; i-- {
		var unlocked bool
		if err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock_shared($1)", l.sharedKeys[i]).Scan(&unlocked); err != nil {
			unlockErr = err
			break
		}
		if !unlocked {
			unlockErr = fmt.Errorf("shared lock was not held by its lease session")
			break
		}
	}
	for i := len(l.keys) - 1; i >= 0; i-- {
		if unlockErr != nil {
			break
		}
		var unlocked bool
		if err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", l.keys[i]).Scan(&unlocked); err != nil {
			unlockErr = err
			break
		}
		if !unlocked {
			unlockErr = fmt.Errorf("lock was not held by its lease session")
			break
		}
	}
	closeErr := conn.Close(ctx)
	if lossErr != nil {
		return l.finishRelease(lossErr)
	}
	if unlockErr != nil {
		return l.finishRelease(fmt.Errorf("failed to unlock PostgreSQL connector advisory lock: %w", unlockErr))
	}
	if closeErr != nil {
		return l.finishRelease(fmt.Errorf("failed to close PostgreSQL connector lease session: %w", closeErr))
	}
	return l.finishRelease(nil)
}

func (l *postgresCDCLease) finishRelease(err error) error {
	l.mu.Lock()
	l.conn = nil
	l.released = true
	l.releaseErr = err
	close(l.releaseDone)
	l.mu.Unlock()
	return err
}

func resolvedConnectorIdentity(systemID, host string, port uint16, database string, cfg CDCConfig) source.ConnectorIdentity {
	previousDatabaseIdentity := strings.Join([]string{
		"postgres", net.JoinHostPort(strings.ToLower(host), strconv.Itoa(int(port))), database,
	}, "\x00")
	databaseIdentity := strings.Join([]string{"postgres", systemID, database}, "\x00")
	return source.ConnectorIdentity{
		Database: databaseIdentity,
		Connector: strings.Join([]string{
			databaseIdentity, cfg.Publication, cfg.SlotName,
		}, "\x00"),
		PreviousDatabase: previousDatabaseIdentity,
		PreviousConnector: strings.Join([]string{
			previousDatabaseIdentity, cfg.Publication, cfg.SlotName,
		}, "\x00"),
	}
}

func connectorLeaseKey(connectorID string) int64 {
	sum := sha256.Sum256([]byte("ingestr:postgres_cdc:" + connectorID))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

func priorConnectorIDs(singular string, plural []string) []string {
	return dedupeMigrationCandidates(plural, singular)
}

func priorSlotSuffixes(singular string, plural []string) []string {
	return dedupeMigrationCandidates(plural, singular)
}

func dedupeMigrationCandidates(plural []string, singular string) []string {
	candidates := append([]string(nil), plural...)
	candidates = append(candidates, singular)
	seen := map[string]bool{"": true}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	return result
}

func publicationLeaseKey(database, publication string) int64 {
	return connectorLeaseKey("publication:" + database + ":" + publication)
}

func resolveResumeSlotCandidates(ctx context.Context, pool *pgxpool.Pool, current string, candidates ...string) (string, bool, bool, error) {
	seen := map[string]bool{"": true, current: true}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		resolved, exists, legacy, err := resolveResumeSlot(ctx, pool, current, candidate)
		if err != nil || exists {
			return resolved, exists, legacy, err
		}
	}
	return resolveResumeSlot(ctx, pool, current, "")
}

func publicationMigrationLeaseKey(database, publication string) int64 {
	return connectorLeaseKey("publication-migration:" + database + ":" + publication)
}
