package trino

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestAcquireManagedCDCRunLease_ContendedCancels(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

	mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."cdc_lease_`).
		WillReturnError(errors.New("Table already exists"))
	mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."cdc_lease_`).
		WillReturnRows(sqlmock.NewRows([]string{"owner", "age"}).AddRow("live-tok", int64(2)))

	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	_, err = d.acquireManagedCDCRunLeaseWithTimings(ctx, "conn-busy", 30*time.Second, 10*time.Second, 50*time.Millisecond, 100*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAcquireManagedCDCRunLease_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("validation errors", func(t *testing.T) {
		t.Parallel()
		_, err := NewTrinoDestination().AcquireManagedCDCRunLease(t.Context(), "conn-1")
		require.ErrorContains(t, err, "not connected")

		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		_, err = (&TrinoDestination{db: db, catalog: "iceberg"}).AcquireManagedCDCRunLease(t.Context(), "")
		require.ErrorContains(t, err, "empty")

		_, err = (&TrinoDestination{db: db}).AcquireManagedCDCRunLease(t.Context(), "conn-1")
		require.ErrorContains(t, err, "catalog")
	})

	t.Run("direct tryAcquire and Release", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		lease := &trinoCDCRunLease{
			d:           d,
			catalog:     "iceberg",
			schema:      "_bruin_staging",
			name:        "cdc_lease_abc",
			token:       "tok-1",
			ttl:         30 * time.Second,
			heartbeatIv: 10 * time.Second,
			checkIv:     50 * time.Millisecond,
			pollIv:      10 * time.Millisecond,
			done:        make(chan struct{}),
			stop:        make(chan struct{}),
			stopped:     make(chan struct{}),
		}

		mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE "iceberg"."_bruin_staging"."cdc_lease_abc" AS SELECT CAST('tok-1' AS VARCHAR) AS "owner", CURRENT_TIMESTAMP AS "heartbeat"`)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("tok-1"))

		acquired, err := lease.tryAcquire(t.Context())
		require.NoError(t, err)
		require.True(t, acquired)

		lease.lastHeartbeat.Store(time.Now().UnixNano())
		close(lease.stopped) // simulate exited monitor for direct Release test
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("tok-1"))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "iceberg"."_bruin_staging"."cdc_lease_abc" RENAME TO "iceberg"."_bruin_staging"."cdc_lease_abc_released_tok-1"`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc_released_tok-1"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("tok-1"))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."cdc_lease_abc_released_tok-1"`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		require.NoError(t, lease.Release())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("takeover of expired lease and rollback on live move", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		lease := &trinoCDCRunLease{
			d:       d,
			catalog: "iceberg",
			schema:  "_bruin_staging",
			name:    "cdc_lease_abc",
			token:   "tok-2",
			ttl:     30 * time.Second,
		}

		// Case 1: expired holder -> moved table still expired -> dropped so next try creates
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."cdc_lease_abc"`).
			WillReturnError(errors.New("Table already exists"))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."cdc_lease_abc" GROUP BY "owner"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner", "age"}).AddRow("old-tok", int64(45)))
		mock.ExpectExec(`ALTER TABLE "iceberg"."_bruin_staging"."cdc_lease_abc" RENAME TO "iceberg"."_bruin_staging"."cdc_lease_abc_expired_tok-2"`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."cdc_lease_abc_expired_tok-2" GROUP BY "owner"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner", "age"}).AddRow("old-tok", int64(46)))
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."cdc_lease_abc_expired_tok-2"`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		acquired, err := lease.tryAcquire(t.Context())
		require.NoError(t, err)
		require.False(t, acquired)

		// Case 2: expired check raced with live renewal -> moved table is live -> restored!
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."cdc_lease_abc"`).
			WillReturnError(errors.New("Table already exists"))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."cdc_lease_abc" GROUP BY "owner"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner", "age"}).AddRow("old-tok", int64(45)))
		mock.ExpectExec(`ALTER TABLE "iceberg"."_bruin_staging"."cdc_lease_abc" RENAME TO "iceberg"."_bruin_staging"."cdc_lease_abc_expired_tok-2"`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."cdc_lease_abc_expired_tok-2" GROUP BY "owner"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner", "age"}).AddRow("tok-3", int64(1)))
		mock.ExpectExec(`ALTER TABLE "iceberg"."_bruin_staging"."cdc_lease_abc_expired_tok-2" RENAME TO "iceberg"."_bruin_staging"."cdc_lease_abc"`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		acquired, err = lease.tryAcquire(t.Context())
		require.NoError(t, err)
		require.False(t, acquired)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Done marks lease lost when renewal is overdue", func(t *testing.T) {
		t.Parallel()
		lease := &trinoCDCRunLease{
			ttl:  100 * time.Millisecond,
			done: make(chan struct{}),
		}
		lease.lastHeartbeat.Store(time.Now().Add(-time.Second).UnixNano())

		select {
		case <-lease.Done():
		default:
			t.Fatal("expected overdue lease Done() to be closed")
		}
		require.ErrorIs(t, lease.Err(), source.ErrConnectorLeaseLost)
	})

	t.Run("monitor detects stolen lease", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		lease := &trinoCDCRunLease{
			d:           d,
			catalog:     "iceberg",
			schema:      "_bruin_staging",
			name:        "cdc_lease_abc",
			token:       "tok-1",
			ttl:         30 * time.Second,
			heartbeatIv: 10 * time.Second,
			checkIv:     10 * time.Millisecond,
			done:        make(chan struct{}),
			stop:        make(chan struct{}),
			stopped:     make(chan struct{}),
		}
		lease.lastHeartbeat.Store(time.Now().UnixNano())

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("other-runner"))

		go lease.monitor()

		select {
		case <-lease.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for lease loss")
		}
		<-lease.stopped
		require.ErrorIs(t, lease.Err(), source.ErrConnectorLeaseLost)
		require.ErrorContains(t, lease.Err(), "taken over")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Release fails if lease was taken over", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		lease := &trinoCDCRunLease{
			d:       d,
			catalog: "iceberg",
			schema:  "_bruin_staging",
			name:    "cdc_lease_abc",
			token:   "tok-1",
			stop:    make(chan struct{}),
			stopped: make(chan struct{}),
		}
		close(lease.stopped)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("other-runner"))

		err = lease.Release()
		require.ErrorContains(t, err, "release Trino CDC run lease")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("monitor heartbeats and releases cleanly", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		lease := &trinoCDCRunLease{
			d:           d,
			catalog:     "iceberg",
			schema:      "_bruin_staging",
			name:        "cdc_lease_abc",
			token:       "tok-1",
			ttl:         30 * time.Second,
			heartbeatIv: 10 * time.Millisecond,
			checkIv:     150 * time.Millisecond,
			done:        make(chan struct{}),
			stop:        make(chan struct{}),
			stopped:     make(chan struct{}),
		}
		lease.lastHeartbeat.Store(time.Now().Add(-100 * time.Millisecond).UnixNano())

		heartbeated := make(chan struct{})
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("tok-1"))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "iceberg"."_bruin_staging"."cdc_lease_abc" ("owner", "heartbeat") SELECT 'tok-1', CURRENT_TIMESTAMP WHERE (SELECT MIN("owner") = 'tok-1' AND MAX("owner") = 'tok-1' FROM "iceberg"."_bruin_staging"."cdc_lease_abc")`)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("tok-1"))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "iceberg"."_bruin_staging"."cdc_lease_abc" RENAME TO "iceberg"."_bruin_staging"."cdc_lease_abc_released_tok-1"`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM "iceberg"."_bruin_staging"."cdc_lease_abc_released_tok-1"`)).
			WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow("tok-1"))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."cdc_lease_abc_released_tok-1"`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		go lease.monitor()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if time.Since(time.Unix(0, lease.lastHeartbeat.Load())) < 50*time.Millisecond {
				close(heartbeated)
				break
			}
			time.Sleep(time.Millisecond)
		}
		select {
		case <-heartbeated:
		default:
			t.Fatal("timed out waiting for heartbeat renewal")
		}
		require.NoError(t, lease.Release())
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
