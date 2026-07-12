package iceberg

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestManagedCDCStateWriteLoadFencePruneAndRetry(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	ctx := context.Background()
	table := "managed_cdc.state"
	stateSchema := icebergCDCStateTestSchema()
	recordedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC).UnixMicro()
	records := [][]any{
		{"run-old", "2", "connector-a", "public.orders", "lake.orders", "run", int64(1), "complete", "0/10", recordedAt},
		{"run-new", "2", "connector-a", "public.orders", "lake.orders", "run", int64(2), "complete", "0/20", recordedAt + 1},
		{"checkpoint-new", "2", "connector-a", "public.orders", "lake.orders", "checkpoint", int64(2), "complete", "0/20", recordedAt + 2},
		{"checkpoint-new", "2", "connector-b", "public.orders", "lake.orders_b", "checkpoint", int64(2), "complete", "0/30", recordedAt + 3},
	}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: stateSchema, PrimaryKeys: []string{"connector_id", "event_id"},
	}))
	writeState := func() {
		batches, err := buildRecordBatches(icebergArrowSchema(stateSchema), records)
		require.NoError(t, err)
		require.NoError(t, dest.WriteCDCState(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: table, Schema: stateSchema, CDCExpectedIncarnation: "target-incarnation-must-not-fence-state-writes",
		}))
	}
	writeState()
	firstSnapshots := icebergSnapshotCount(t, dest, table)
	writeState()
	require.Equal(t, firstSnapshots, icebergSnapshotCount(t, dest, table))

	entries, err := dest.LoadCDCState(ctx, table, "connector-a")
	require.NoError(t, err)
	require.Len(t, entries, 3)
	positions := make(map[string]string, len(entries))
	for _, entry := range entries {
		positions[entry.EventID] = entry.Position
		require.False(t, entry.RecordedAt.IsZero())
	}
	require.Equal(t, "0/20", positions["checkpoint-new"])

	fence, err := dest.LoadCDCStateFence(ctx, table, "connector-a")
	require.NoError(t, err)
	require.EqualValues(t, 2, fence.Generation)
	require.Equal(t, []string{"run-new"}, fence.RunEventIDs)

	require.NoError(t, dest.DeleteCDCStateEvents(ctx, table, "connector-a", []string{"checkpoint-new", "run-old"}))
	entries, err = dest.LoadCDCState(ctx, table, "connector-a")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "run-new", entries[0].EventID)
	otherEntries, err := dest.LoadCDCState(ctx, table, "connector-b")
	require.NoError(t, err)
	require.Len(t, otherEntries, 1)

	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Empty(t, tbl.Properties()[tableCDCResumeStateKey])
	require.Empty(t, tbl.CurrentSnapshot().Summary.Properties[snapshotCDCResumeLSNKey])
	require.Empty(t, tbl.CurrentSnapshot().Summary.Properties[snapshotCDCResetKey])
	require.Equal(t, icebergCDCStatePruneBatch, dest.CDCStatePruneBatchSize())
}

func TestManagedCDCTargetClaimIsPermanentIdempotentAndVisible(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	ctx := context.Background()
	table := "managed_cdc.targets"
	claimSchema := icebergCDCClaimTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: claimSchema, PrimaryKeys: []string{"destination_table"},
	}))

	claim := destination.CDCTargetClaim{
		DestinationTable: "lake.orders",
		ConnectorID:      "connector-a",
		SourceTable:      "public.orders",
	}
	require.NoError(t, dest.ClaimCDCTarget(ctx, table, claim))
	canonical, err := dest.CanonicalCDCTarget(ctx, claim.DestinationTable)
	require.NoError(t, err)
	claimLock, err := dest.catalog.LoadTable(ctx, icebergCDCTargetClaimLockIdentifier(icebergcatalog.ToIdentifier(table), canonical))
	require.NoError(t, err)
	require.Equal(t, canonical, claimLock.Properties()[cdcTargetClaimLockTargetKey])
	require.Len(t, canonical, 64)
	require.Empty(t, claimLock.Properties()[managedTableProperty])
	require.Empty(t, claimLock.Properties()[managedTableExpiresAt])
	require.Empty(t, claimLock.Properties()[managedTableExpiresAfterMS])
	firstSnapshots := icebergSnapshotCount(t, dest, table)
	claimTable, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Empty(t, claimTable.CurrentSnapshot().Summary.Properties[snapshotCDCResetKey])
	require.NoError(t, dest.ClaimCDCTarget(ctx, table, claim))
	require.Equal(t, firstSnapshots, icebergSnapshotCount(t, dest, table))
	claimRows := readTableRows(t, dest, table)
	require.Len(t, claimRows.Rows, 1)
	require.Equal(t, canonical, claimRows.Value(claimRows.Rows[0], "destination_table"))
	require.Len(t, claimRows.Value(claimRows.Rows[0], "destination_table"), 64)

	claim.ConnectorID = "connector-b"
	err = dest.ClaimCDCTarget(ctx, table, claim)
	require.ErrorContains(t, err, "already claimed")
	require.Len(t, readTableRows(t, dest, table).Rows, 1)

	claim.DestinationTable = "lake.customers"
	claim.SourceTable = "public.customers"
	require.NoError(t, dest.ClaimCDCTarget(ctx, table, claim))
	require.Len(t, readTableRows(t, dest, table).Rows, 2)
}

func TestManagedCDCTargetConcurrentClaimHasOnePermanentOwner(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	ctx := context.Background()
	table := "managed_cdc.concurrent_targets"
	claimSchema := icebergCDCClaimTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: claimSchema, PrimaryKeys: []string{"destination_table"},
	}))
	barrier := &cdcClaimCreateBarrierCatalog{
		Catalog: dest.catalog,
		ready:   make(chan struct{}, 8),
		release: make(chan struct{}),
	}
	dest.catalog = barrier

	errs := make(chan error, 2)
	for _, connectorID := range []string{"connector-a", "connector-b"} {
		go func() {
			errs <- dest.ClaimCDCTarget(ctx, table, destination.CDCTargetClaim{
				DestinationTable: "lake.orders",
				ConnectorID:      connectorID,
				SourceTable:      "public.orders",
			})
		}()
	}
	<-barrier.ready
	<-barrier.ready
	close(barrier.release)
	first, second := <-errs, <-errs
	require.True(t, first == nil || second == nil)
	require.False(t, first == nil && second == nil)
	if first != nil {
		require.ErrorContains(t, first, "already claimed")
	}
	if second != nil {
		require.ErrorContains(t, second, "already claimed")
	}
	require.Len(t, readTableRows(t, dest, table).Rows, 1)
}

func TestManagedCDCTargetClaimRetryRepairsMissingVisibleRow(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	ctx := context.Background()
	table := "managed_cdc.retry_targets"
	claimSchema := icebergCDCClaimTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: claimSchema, PrimaryKeys: []string{"destination_table"},
	}))
	claim := destination.CDCTargetClaim{
		DestinationTable: "lake.orders",
		ConnectorID:      "connector-a",
		SourceTable:      "public.orders",
	}
	ownerID, err := claim.OwnerID()
	require.NoError(t, err)
	canonical, err := dest.CanonicalCDCTarget(ctx, claim.DestinationTable)
	require.NoError(t, err)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.NoError(t, dest.ensureCDCTargetClaimLock(ctx, tbl.Identifier(), canonical, ownerID, ""))
	tbl, err = dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Empty(t, tbl.Properties()[cdcTargetClaimPropertyPrefix+destination.CDCTargetKeyDigest(canonical)])
	require.Empty(t, readTableRows(t, dest, table).Rows)

	require.NoError(t, dest.ClaimCDCTarget(ctx, table, claim))
	require.Len(t, readTableRows(t, dest, table).Rows, 1)
	tbl, err = dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.Equal(t, ownerID, tbl.Properties()[cdcTargetClaimPropertyPrefix+destination.CDCTargetKeyDigest(canonical)])
	claim.ConnectorID = "connector-b"
	require.ErrorContains(t, dest.ClaimCDCTarget(ctx, table, claim), "already claimed")
}

func TestManagedCDCTargetIncarnationAndConditionalTruncate(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	ctx := context.Background()
	table := "managed_cdc.orders"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}, {int64(2)}})
	require.NoError(t, dest.CommitWriteToken(ctx, table, "legacy-checkpoint", "0/900"))

	incarnation, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEmpty(t, incarnation)
	require.NoError(t, dest.ValidateManagedCDCTarget(ctx, table))

	err = dest.TruncateCDCTableIfIncarnation(ctx, table, "replaced-table")
	require.ErrorContains(t, err, "incarnation changed")
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))

	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, table, incarnation))
	require.Zero(t, icebergRowCount(ctx, t, dest, table))
	resume, err := dest.GetMaxCDCLSN(ctx, table)
	require.NoError(t, err)
	require.Empty(t, resume)
	stable, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, incarnation, stable)

	require.NoError(t, dest.catalog.DropTable(ctx, icebergcatalog.ToIdentifier(table)))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))
	recreated, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEqual(t, incarnation, recreated)
	err = dest.TruncateCDCTableIfIncarnation(ctx, table, incarnation)
	require.ErrorContains(t, err, "incarnation changed")
}

func TestIcebergCatalogIdentityIgnoresRotatedCredentials(t *testing.T) {
	left := mustIcebergCatalogIdentity(t, "postgres://old-user:old@catalog/db?password=old&SSLPASSWORD=ssl-old&Api_Token=token-old&sslmode=require")
	right := mustIcebergCatalogIdentity(t, "postgres://new-user:new@catalog/db?password=new&sslpassword=ssl-new&api_token=token-new&sslmode=require")
	require.Equal(t, left, right)
	require.NotContains(t, left, "old")
	require.NotContains(t, left, "user")
	require.NotContains(t, strings.ToLower(left), "password")
	require.NotContains(t, strings.ToLower(left), "token")
}

func TestIcebergCatalogIdentityCanonicalizesEquivalentLocations(t *testing.T) {
	for _, tt := range []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "rest endpoint case default port path and query order",
			left:  "HTTPS://CATALOG.EXAMPLE.COM.:443/api/../iceberg/?region=us-east-1&tenant=prod",
			right: "https://catalog.example.com/iceberg?tenant=prod&region=us-east-1",
		},
		{
			name:  "postgres default port and credentials",
			left:  "postgres://first:old@CATALOG.EXAMPLE.COM:5432/catalog/?sslmode=require&password=old",
			right: "postgres://second:new@catalog.example.com/catalog?password=new&sslmode=require",
		},
		{
			name:  "object warehouse case and trailing path",
			left:  "s3://WAREHOUSE/prod/./",
			right: "s3://warehouse/prod",
		},
		{
			name:  "file URI and local path spelling",
			left:  "file:/var/lib/iceberg/warehouse/",
			right: "/var/lib/iceberg/warehouse",
		},
		{
			name:  "localhost file authority",
			left:  "file://LOCALHOST./var/lib/iceberg/warehouse",
			right: "file:///var/lib/iceberg/warehouse",
		},
		{
			name:  "local warehouse path",
			left:  "/var/lib/iceberg/warehouse/./",
			right: "/var/lib/iceberg/warehouse",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, mustIcebergCatalogIdentity(t, tt.left), mustIcebergCatalogIdentity(t, tt.right))
		})
	}
}

func TestIcebergCatalogIdentityCanonicalizesKeywordDSN(t *testing.T) {
	left := mustIcebergCatalogIdentity(t, "host=CATALOG.EXAMPLE.COM. port=5432 dbname='iceberg catalog' user=old password='old secret' sslpassword=ssl-old jwt=old-jwt bearer=old-bearer cookie=old-cookie session=old-session sslmode=REQUIRE connect_timeout=5")
	right := mustIcebergCatalogIdentity(t, "connect_timeout=30 sslmode=verify-full session=new-session cookie=new-cookie bearer=new-bearer jwt=new-jwt password=new user=new dbname='iceberg catalog' host=catalog.example.com")

	require.Equal(t, left, right)
	require.Equal(t, "keyword-dsn:?dbname=iceberg+catalog&host=catalog.example.com", left)
	for _, secret := range []string{"old", "secret", "password", "user", "sslpassword", "jwt", "bearer", "cookie", "session"} {
		require.NotContains(t, strings.ToLower(left), secret)
	}
}

func TestIcebergCatalogIdentityCanonicalizesRelativeFileLocations(t *testing.T) {
	absolute, err := filepath.Abs("catalog.db")
	require.NoError(t, err)
	want := "file://" + filepath.ToSlash(absolute)

	require.Equal(t, want, mustIcebergCatalogIdentity(t, "file:catalog.db"))
	require.Equal(t, want, mustIcebergCatalogIdentity(t, "file:./catalog.db"))
	require.Equal(t, want, mustIcebergCatalogIdentity(t, "./catalog.db"))
	require.Equal(t, want, mustIcebergCatalogIdentity(t, want))
}

func TestIcebergCatalogIdentityRejectsNonDurableFileLocations(t *testing.T) {
	for _, raw := range []string{
		"file::memory:",
		"file:catalog.db?mode=memory",
		"file:///tmp/catalog.db?mode=memory",
		"file:///tmp/catalog.db?MODE=MEMORY",
	} {
		_, err := icebergCatalogIdentityURI(raw)
		require.ErrorContains(t, err, "in-memory")
		require.NotContains(t, err.Error(), raw)
	}
}

func TestIcebergCatalogIdentityIgnoresTransportOnlySSLMode(t *testing.T) {
	left := mustIcebergCatalogIdentity(t, "postgres://catalog.example.com/iceberg?sslmode=require")
	right := mustIcebergCatalogIdentity(t, "postgres://catalog.example.com/iceberg?sslmode=verify-full")
	require.Equal(t, left, right)
}

func TestIcebergCatalogIdentityRejectsAmbientServiceDSN(t *testing.T) {
	for _, raw := range []string{
		"service=warehouse",
		"service=warehouse host=catalog.example.com dbname=iceberg",
		"servicefile=/etc/postgresql/service.conf host=catalog.example.com dbname=iceberg",
		"postgres://catalog.example.com/iceberg?service=warehouse",
		"postgres://catalog.example.com/iceberg?servicefile=%2Fetc%2Fpostgresql%2Fservice.conf",
	} {
		_, err := icebergCatalogIdentityURI(raw)
		require.ErrorContains(t, err, "service-based")
		require.NotContains(t, err.Error(), raw)
	}
}

func TestIcebergCatalogIdentityDistinguishesKeywordDSNLocation(t *testing.T) {
	base := mustIcebergCatalogIdentity(t, "host=catalog.example.com dbname=iceberg user=old password=secret")
	differentHost := mustIcebergCatalogIdentity(t, "host=other.example.com dbname=iceberg user=new password=rotated")
	differentDatabase := mustIcebergCatalogIdentity(t, "host=catalog.example.com dbname=other user=new password=rotated")

	require.NotEqual(t, base, differentHost)
	require.NotEqual(t, base, differentDatabase)
}

func TestIcebergCatalogIdentityRejectsUnsafeDSNsWithoutLeakingThem(t *testing.T) {
	for _, raw := range []string{
		"host=catalog.example.com password='unterminated-secret",
		"user:opaque-secret@tcp(catalog.example.com:3306)/iceberg",
		"postgres:host=catalog.example.com password=opaque-secret",
		"catalog.example.com/ambiguous-secret",
	} {
		_, err := icebergCatalogIdentityURI(raw)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		require.NotContains(t, err.Error(), raw)
	}
}

func TestIcebergCatalogIdentityIgnoresSignedAndAuthQueryParameters(t *testing.T) {
	for _, tt := range []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "AWS SigV4",
			left: "https://catalog.example.com/iceberg?tenant=prod" +
				"&X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=old%2F20260714%2Fus-east-1%2Fexecute-api%2Faws4_request" +
				"&X-Amz-Date=20260714T000000Z&X-Amz-Expires=60&X-Amz-Security-Token=old-token" +
				"&X-Amz-SignedHeaders=host&X-Amz-Signature=old-signature",
			right: "https://catalog.example.com/iceberg?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
				"&X-Amz-Credential=new%2F20260715%2Fus-east-1%2Fexecute-api%2Faws4_request" +
				"&X-Amz-Date=20260715T000000Z&X-Amz-Expires=120&X-Amz-Security-Token=new-token" +
				"&X-Amz-SignedHeaders=host&X-Amz-Signature=new-signature&tenant=prod",
		},
		{
			name:  "legacy AWS signature",
			left:  "https://catalog.example.com/iceberg?tenant=prod&AWSAccessKeyId=old&Expires=1&Signature=old",
			right: "https://catalog.example.com/iceberg?Signature=new&Expires=2&AWSAccessKeyId=new&tenant=prod",
		},
		{
			name:  "Azure SAS",
			left:  "https://account.blob.core.windows.net/warehouse?tenant=prod&sv=2024-11-04&sp=rl&se=2026-07-14&sr=c&sig=old",
			right: "https://account.blob.core.windows.net/warehouse?sig=new&sr=c&se=2026-07-15&sp=rl&sv=2025-01-05&tenant=prod",
		},
		{
			name:  "Google signed URL",
			left:  "https://storage.googleapis.com/warehouse?tenant=prod&X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=old&X-Goog-Date=20260714T000000Z&X-Goog-Signature=old",
			right: "https://storage.googleapis.com/warehouse?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=new&X-Goog-Date=20260715T000000Z&X-Goog-Signature=new&tenant=prod",
		},
		{
			name:  "OAuth and generic auth",
			left:  "https://catalog.example.com/iceberg?tenant=prod&oauth_client_id=old&client_secret=old&Authorization=Bearer+old",
			right: "https://catalog.example.com/iceberg?Authorization=Bearer+new&client_secret=new&oauth_client_id=new&tenant=prod",
		},
		{
			name:  "session and bearer credentials",
			left:  "https://catalog.example.com/iceberg?tenant=prod&jwt=old-jwt&bearer=old-bearer&cookie=old-cookie&session=old-session",
			right: "https://catalog.example.com/iceberg?session=new-session&cookie=new-cookie&bearer=new-bearer&jwt=new-jwt&tenant=prod",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			left := mustIcebergCatalogIdentity(t, tt.left)
			right := mustIcebergCatalogIdentity(t, tt.right)
			require.Equal(t, left, right)
			require.Equal(t, "https://"+strings.TrimPrefix(strings.Split(left, "?")[0], "https://")+"?tenant=prod", left)
		})
	}
}

func TestIcebergCatalogIdentityPreservesLocationQueryParameters(t *testing.T) {
	base := mustIcebergCatalogIdentity(t, "https://catalog.example.com/iceberg?tenant=prod&region=us-east-1")
	differentTenant := mustIcebergCatalogIdentity(t, "https://catalog.example.com/iceberg?tenant=staging&region=us-east-1")
	differentRegion := mustIcebergCatalogIdentity(t, "https://catalog.example.com/iceberg?tenant=prod&region=eu-west-1")

	require.NotEqual(t, base, differentTenant)
	require.NotEqual(t, base, differentRegion)
}

func TestManagedCDCCatalogCapabilityValidation(t *testing.T) {
	for _, supported := range []icebergcatalog.Type{
		icebergcatalog.REST,
		icebergcatalog.Hive,
		icebergcatalog.Glue,
		icebergcatalog.DynamoDB,
		icebergcatalog.SQL,
	} {
		require.True(t, supportsAtomicCDCTargetClaims(supported), supported)
	}
	for _, unsupported := range []icebergcatalog.Type{icebergcatalog.Hadoop, "custom"} {
		require.False(t, supportsAtomicCDCTargetClaims(unsupported), unsupported)
	}

	sqlDestination := newSQLiteManagedCDCDestination(t)
	require.NoError(t, sqlDestination.ValidateManagedCDCState())
	hadoopDestination := newHadoopDestination(t)
	err := hadoopDestination.ValidateManagedCDCState()
	require.ErrorContains(t, err, "atomic table creation")
	require.ErrorContains(t, err, "hadoop")
}

func TestCanonicalCDCTargetUsesEncodedCatalogNamespace(t *testing.T) {
	dest := newHadoopDestination(t)
	canonical, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.Equal(t, destination.CDCTargetKeyDigest(
		strings.ToLower(strings.TrimSpace(string(dest.catalog.CatalogType()))),
		icebergCatalogIdentityName(dest.catalog.CatalogType(), dest.cfg),
		icebergRESTCatalogIdentityPrefix(dest.catalog.CatalogType(), dest.cfg.Properties.Get("prefix", "")),
		mustIcebergCatalogIdentity(t, dest.cfg.Properties.Get("uri", "")),
		mustIcebergCatalogIdentity(t, dest.cfg.Properties.Get("warehouse", "")),
		strings.TrimSpace(dest.cfg.Properties.Get("glue.id", "")),
		strings.ToLower(strings.TrimSpace(dest.cfg.Properties.Get("glue.region", ""))),
		"",
		"lake",
		"orders",
	), canonical)
	require.Len(t, canonical, 64)
	differentTable, err := dest.CanonicalCDCTarget(context.Background(), "lake.customers")
	require.NoError(t, err)
	require.NotEqual(t, canonical, differentTable)

	dest.cfg.Properties["uri"] = "postgres://first:secret@catalog/db?SSLPASSWORD=first"
	firstCredentials, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	dest.cfg.Properties["uri"] = "postgres://second:rotated@catalog/db?sslpassword=second"
	rotatedCredentials, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.Equal(t, firstCredentials, rotatedCredentials)
	dest.cfg.Properties["uri"] = "postgres://second:rotated@different-catalog/db?sslpassword=second"
	differentCatalog, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.NotEqual(t, rotatedCredentials, differentCatalog)
}

func TestCanonicalCDCTargetIncludesRESTPrefix(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	dest.catalog = catalogTypeOverride{Catalog: dest.catalog, catalogType: icebergcatalog.REST}
	dest.cfg.Properties["prefix"] = "/tenant/./prod/"
	first, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)

	dest.cfg.Properties["prefix"] = "tenant/prod"
	equivalent, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.Equal(t, first, equivalent)

	dest.cfg.Properties["prefix"] = "tenant/staging"
	different, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.NotEqual(t, first, different)
}

func TestCanonicalCDCTargetIgnoresNonRoutingCatalogName(t *testing.T) {
	for _, catalogType := range []icebergcatalog.Type{icebergcatalog.REST, icebergcatalog.Hive, icebergcatalog.Glue} {
		t.Run(string(catalogType), func(t *testing.T) {
			dest := newSQLiteManagedCDCDestination(t)
			dest.catalog = catalogTypeOverride{Catalog: dest.catalog, catalogType: catalogType}
			if catalogType == icebergcatalog.Glue {
				dest.cfg.Properties["glue.id"] = "123456789012"
				dest.cfg.Properties["glue.region"] = "us-east-1"
				dest.cfg.Properties["glue.endpoint"] = "https://glue.us-east-1.amazonaws.com"
			}
			dest.cfg.CatalogNameExplicit = true
			dest.cfg.CatalogName = "alpha"
			alpha, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
			require.NoError(t, err)
			dest.cfg.CatalogName = "beta"
			beta, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
			require.NoError(t, err)
			require.Equal(t, alpha, beta)
		})
	}
}

func TestCanonicalCDCTargetRequiresExplicitGlueRoutingIdentity(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	dest.catalog = catalogTypeOverride{Catalog: dest.catalog, catalogType: icebergcatalog.Glue}

	_, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.ErrorContains(t, err, "glue.id")
	dest.cfg.Properties["glue.id"] = "123456789012"
	_, err = dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.ErrorContains(t, err, "glue.region")
	dest.cfg.Properties["glue.region"] = "US-EAST-1"
	_, err = dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.ErrorContains(t, err, "glue.endpoint")
	dest.cfg.Properties["glue.endpoint"] = "https://glue.us-east-1.amazonaws.com"
	_, err = dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.NoError(t, err)
}

func TestCanonicalCDCTargetIncludesExplicitGlueEndpointAndRejectsAmbientEndpoint(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	dest.catalog = catalogTypeOverride{Catalog: dest.catalog, catalogType: icebergcatalog.Glue}
	dest.cfg.Properties["glue.id"] = "123456789012"
	dest.cfg.Properties["glue.region"] = "us-east-1"

	t.Setenv("AWS_ENDPOINT_URL_GLUE", "https://ambient-secret.example")
	_, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.ErrorContains(t, err, "explicit glue.endpoint")
	require.NotContains(t, err.Error(), "ambient-secret")

	dest.cfg.Properties["glue.endpoint"] = "HTTPS://GLUE.EXAMPLE.COM.:443/api/../catalog/"
	first, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.NoError(t, err)
	dest.cfg.Properties["glue.endpoint"] = "https://glue.example.com/catalog"
	equivalent, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.NoError(t, err)
	require.Equal(t, first, equivalent)
	dest.cfg.Properties["glue.endpoint"] = "https://other.example.com/catalog"
	different, err := dest.CanonicalCDCTarget(t.Context(), "lake.orders")
	require.NoError(t, err)
	require.NotEqual(t, first, different)
}

func TestCanonicalCDCTargetUsesEffectiveSQLCatalogName(t *testing.T) {
	dest := newSQLiteManagedCDCDestination(t)
	dest.cfg.CatalogName = "ingestr"
	dest.cfg.CatalogNameExplicit = false
	legacyDefault, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)

	dest.cfg.CatalogName = "sql"
	dest.cfg.CatalogNameExplicit = true
	explicitDefault, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.Equal(t, legacyDefault, explicitDefault)

	dest.cfg.CatalogName = "alpha"
	alpha, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	dest.cfg.CatalogName = "beta"
	beta, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.NotEqual(t, alpha, beta)
}

func mustIcebergCatalogIdentity(t *testing.T, raw string) string {
	t.Helper()
	identity, err := icebergCatalogIdentityURI(raw)
	require.NoError(t, err)
	return identity
}

func TestCanonicalCDCTargetNormalizesCatalogEndpointAndWarehouse(t *testing.T) {
	dest := newHadoopDestination(t)
	dest.cfg.Properties["uri"] = "HTTPS://CATALOG.EXAMPLE.COM.:443/api/../iceberg/?tenant=prod&X-Amz-Signature=old&X-Amz-Date=20260714T000000Z"
	dest.cfg.Properties["warehouse"] = "s3://WAREHOUSE/prod/./?X-Amz-Signature=old&X-Amz-Date=20260714T000000Z"
	first, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)

	dest.cfg.Properties["uri"] = "https://catalog.example.com/iceberg?X-Amz-Date=20260715T000000Z&X-Amz-Signature=new&tenant=prod"
	dest.cfg.Properties["warehouse"] = "s3://warehouse/prod?X-Amz-Date=20260715T000000Z&X-Amz-Signature=new"
	equivalent, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.Equal(t, first, equivalent)

	dest.cfg.Properties["warehouse"] = "s3://warehouse/other"
	differentWarehouse, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.NotEqual(t, first, differentWarehouse)

	dest.cfg.Properties["warehouse"] = "s3://warehouse/prod"
	dest.cfg.Properties["uri"] = "https://other-catalog.example.com/iceberg?tenant=prod"
	differentCatalog, err := dest.CanonicalCDCTarget(context.Background(), "lake.orders")
	require.NoError(t, err)
	require.NotEqual(t, first, differentCatalog)
}

func newSQLiteManagedCDCDestination(t *testing.T) *Destination {
	t.Helper()
	root := t.TempDir()
	dest := NewDestination()
	require.NoError(t, dest.Connect(
		context.Background(),
		"iceberg+sqlite://"+filepath.Join(root, "catalog.db")+"?warehouse_path="+url.QueryEscape(filepath.Join(root, "warehouse")),
	))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	return dest
}

func icebergCDCStateTestSchema() *schema.TableSchema {
	return &schema.TableSchema{Columns: []schema.Column{
		{Name: "event_id", DataType: schema.TypeString, Nullable: false},
		{Name: "state_version", DataType: schema.TypeString, Nullable: false},
		{Name: "connector_id", DataType: schema.TypeString, Nullable: false},
		{Name: "source_table", DataType: schema.TypeString, Nullable: false},
		{Name: "destination_table", DataType: schema.TypeString, Nullable: false},
		{Name: "state_kind", DataType: schema.TypeString, Nullable: false},
		{Name: "state_generation", DataType: schema.TypeInt64, Nullable: false},
		{Name: "state_status", DataType: schema.TypeString, Nullable: false},
		{Name: "_cdc_lsn", DataType: schema.TypeString, Nullable: false},
		{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}}
}

func icebergCDCClaimTestSchema() *schema.TableSchema {
	return &schema.TableSchema{Columns: []schema.Column{
		{Name: "destination_table", DataType: schema.TypeString, Nullable: false},
		{Name: "connector_id", DataType: schema.TypeString, Nullable: false},
		{Name: "claimed_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}}
}

type cdcClaimCreateBarrierCatalog struct {
	icebergcatalog.Catalog
	ready   chan struct{}
	release chan struct{}
}

type catalogTypeOverride struct {
	icebergcatalog.Catalog
	catalogType icebergcatalog.Type
}

func (c catalogTypeOverride) CatalogType() icebergcatalog.Type {
	return c.catalogType
}

func (c catalogTypeOverride) Close() error {
	if closer, ok := c.Catalog.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (c *cdcClaimCreateBarrierCatalog) CreateTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	tableSchema *iceberggo.Schema,
	opts ...icebergcatalog.CreateTableOpt,
) (*icebergtable.Table, error) {
	if len(ident) > 0 && strings.HasPrefix(ident[len(ident)-1], cdcTargetClaimLockPrefix) {
		c.ready <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.release:
		}
	}
	return c.Catalog.CreateTable(ctx, ident, tableSchema, opts...)
}
