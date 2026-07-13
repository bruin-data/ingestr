package postgres_cdc

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAcquireConnectorLeaseRejectsConcurrentPublicationPreparation(t *testing.T) {
	src := NewPostgresCDCSource()
	src.queryPool = &pgxpool.Pool{}
	src.cdcConfig.Publication = defaultPublicationName
	src.connectorPreparing = true

	done := make(chan error, 1)
	go func() {
		_, err := src.AcquireConnectorLease(context.Background(), source.ConnectorLeaseOptions{ConnectorID: "connector"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "preparation is in progress") {
			t.Fatalf("AcquireConnectorLease() error = %v, want preparation-in-progress error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connector lease acquisition blocked during publication preparation")
	}
}

func TestConnectorLeaseKeyIsStableAndConnectorScoped(t *testing.T) {
	first := connectorLeaseKey("connector-a")
	if first != connectorLeaseKey("connector-a") {
		t.Fatal("connector lease key is not stable")
	}
	if first == connectorLeaseKey("connector-b") {
		t.Fatal("different connectors received the same lease key")
	}
}

func TestPostgresCDCLeaseReleaseIsIdempotent(t *testing.T) {
	lease := &postgresCDCLease{}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresCDCLeaseReleaseIsConcurrentlyIdempotent(t *testing.T) {
	lease := &postgresCDCLease{}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- lease.Release()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicationLeaseKeyIsDatabaseScoped(t *testing.T) {
	if publicationLeaseKey("database_a", "ingestr_publication") == publicationLeaseKey("database_b", "ingestr_publication") {
		t.Fatal("same publication name in different databases received the same lease key")
	}
}

func TestResolvedConnectorIdentityUsesEffectiveDatabaseAndCDCConfig(t *testing.T) {
	base := resolvedConnectorIdentity("system-1", "orders", CDCConfig{Publication: "orders_pub"})
	otherDatabase := resolvedConnectorIdentity("system-1", "customers", CDCConfig{Publication: "orders_pub"})
	if base.Database == otherDatabase.Database || base.Connector == otherDatabase.Connector {
		t.Fatal("different effective databases shared a resolved identity")
	}
	otherSlot := resolvedConnectorIdentity("system-1", "orders", CDCConfig{Publication: "orders_pub", SlotName: "other"})
	if base.Database != otherSlot.Database || base.Connector == otherSlot.Connector {
		t.Fatal("slot must scope connector identity without changing database identity")
	}
	otherSystem := resolvedConnectorIdentity("system-2", "orders", CDCConfig{Publication: "orders_pub"})
	if base.Database == otherSystem.Database || base.Connector == otherSystem.Connector {
		t.Fatal("different server system IDs shared a resolved identity")
	}
	var _ source.ConnectorIdentityProvider = NewPostgresCDCSource()
}
