package postgres_cdc

import (
	"reflect"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestMigrationCandidatesAreDeduplicatedInPriorityOrder(t *testing.T) {
	want := []string{"current-source-old-dest", "old-source-new-dest", "old-source-old-dest"}
	got := priorConnectorIDs("current-source-old-dest", []string{
		"current-source-old-dest", "old-source-new-dest", "old-source-old-dest", "old-source-new-dest", "",
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("priorConnectorIDs() = %#v, want %#v", got, want)
	}
	if got := priorSlotSuffixes("only-legacy", nil); !reflect.DeepEqual(got, []string{"only-legacy"}) {
		t.Fatalf("singular compatibility suffix was lost: %#v", got)
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
	base := resolvedConnectorIdentity("system-1", "DB.EXAMPLE", 5432, "orders", CDCConfig{Publication: "orders_pub"})
	alias := resolvedConnectorIdentity("system-1", "db.example", 5432, "orders", CDCConfig{Publication: "orders_pub"})
	if base != alias {
		t.Fatalf("host case changed resolved identity: %#v != %#v", base, alias)
	}
	otherDatabase := resolvedConnectorIdentity("system-1", "db.example", 5432, "customers", CDCConfig{Publication: "orders_pub"})
	if base.Database == otherDatabase.Database || base.Connector == otherDatabase.Connector {
		t.Fatal("different effective databases shared a resolved identity")
	}
	otherSlot := resolvedConnectorIdentity("system-1", "db.example", 5432, "orders", CDCConfig{Publication: "orders_pub", SlotName: "other"})
	if base.Database != otherSlot.Database || base.Connector == otherSlot.Connector {
		t.Fatal("slot must scope connector identity without changing database identity")
	}
	otherAlias := resolvedConnectorIdentity("system-1", "localhost", 15432, "orders", CDCConfig{Publication: "orders_pub"})
	if base.Database != otherAlias.Database || base.Connector != otherAlias.Connector {
		t.Fatal("host aliases for the same server changed primary connector identity")
	}
	if base.PreviousDatabase == otherAlias.PreviousDatabase || base.PreviousConnector == otherAlias.PreviousConnector {
		t.Fatal("previous host-derived identity must remain available for migration")
	}
	var _ source.ConnectorIdentityProvider = NewPostgresCDCSource()
}
