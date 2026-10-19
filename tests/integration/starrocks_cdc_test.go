//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	starrocks "github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestStarRocksCDC(t *testing.T) {
	if testing.Short() || !containerWanted("starrocks") {
		t.Skip("Docker CDC integration")
	}
	t.Parallel()
	ctx := t.Context()
	dsn, uri := os.Getenv("INGESTR_TEST_STARROCKS_DSN"), os.Getenv("INGESTR_TEST_STARROCKS_URI")
	if dsn == "" || uri == "" {
		dsn, uri = sharedStarRocks(t)
	}
	waitForStarRocksBackend(t, dsn)
	parent, pgSourceURI := t, os.Getenv("INGESTR_TEST_POSTGRES_CDC_URI")
	pgSource := func(t *testing.T) string {
		t.Helper()
		if pgSourceURI == "" {
			container, u := setupPostgresCDCContainer(t, ctx)
			pgSourceURI = u
			parent.Cleanup(func() { _ = container.Terminate(context.Background()) })
		}
		return pgSourceURI
	}
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	database := "cdc_" + strings.ToLower(uniqueSuffix())
	execEventually(t, db, "CREATE DATABASE "+database)
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE " + database) })
	// A fresh backend is alive before it reports disk capacity; table
	// creation fails until it does.
	execEventually(t, db, "CREATE TABLE "+database+".ready_probe (k INT) DUPLICATE KEY(k) DISTRIBUTED BY HASH(k) BUCKETS 1 PROPERTIES ('replication_num' = '1')")
	execEventually(t, db, "DROP TABLE "+database+".ready_probe")
	dest := starrocks.NewStarRocksDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	exec := func(t *testing.T, query string) { t.Helper(); require.NoError(t, dest.Exec(ctx, query)) }

	t.Run("merge ordering and TOAST", func(t *testing.T) {
		target, stage := database+".target", database+".stage"
		columns := []schema.Column{{Name: "tenant", DataType: schema.TypeInt64}, {Name: "id", DataType: schema.TypeInt64}, {Name: "payload", DataType: schema.TypeString}, {Name: "__ingestr_rank", DataType: schema.TypeString}, {Name: "__ingestr_target", DataType: schema.TypeString}, {Name: "_cdc_lsn", DataType: schema.TypeString}, {Name: "_cdc_deleted", DataType: schema.TypeBoolean}, {Name: "_cdc_synced_at", DataType: schema.TypeTimestamp}}
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: []string{"tenant", "id"}}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: stage, Schema: &schema.TableSchema{Columns: append(columns, schema.Column{Name: "_cdc_unchanged_cols", DataType: schema.TypeString})}}))
		exec(t, "INSERT INTO "+target+" VALUES (1,1,'current','rank','origin','030',false,'2026-01-01'),(1,2,'deleted','r','o','030',true,'2026-01-01'),(1,3,'legacy','r','o',NULL,false,'2026-01-01'),(1,4,'keep','r','o','010',false,'2026-01-01'),(1,5,'old','r','o','010',false,'2026-01-01'),(1,6,'clear','r','o','010',false,'2026-01-01'),(1,7,'old','r','o','010',false,'2026-01-01')")
		exec(t, "INSERT INTO "+target+" VALUES (1,8,'before','r','o','010',false,'2026-01-01'),(1,9,'original','r','o','010',false,'2026-01-01')")
		exec(t, "INSERT INTO "+stage+` VALUES
   (1,1,'stale','x','x','020',false,'2026-01-02','[]'),
   (1,1,NULL,NULL,NULL,'025',true,'2026-01-02','[]'),
   (1,1,'unknown','x','x',NULL,false,'2026-01-02','[]'),
   (1,2,'resurrection','x','x','030',false,'2026-01-02','[]'),
   (1,3,'first','newrank','neworigin','010',false,'2026-01-02','[]'),
   (1,4,NULL,NULL,NULL,'010',true,'2026-01-02','[]'),
   (1,5,'new','r2','o2','020',false,'2026-01-02','[]'),
   (1,5,NULL,'r3','o3','030',false,'2026-01-03','["payload"]'),
   (1,5,NULL,'r4','o4','040',false,'2026-01-04','["payload"]'),
   (1,6,NULL,'r2','o2','020',false,'2026-01-02','[]'),
   (1,6,NULL,'r3','o3','030',false,'2026-01-03','["payload"]'),
   (1,7,'new-before-delete','r2','o2','020',false,'2026-01-02','[]'),
   (1,7,NULL,NULL,NULL,'030',true,'2026-01-03','[]'),
   (1,8,'same-position-update','r2','o2','010',false,'2026-01-02','[]'),
   (1,8,NULL,NULL,NULL,'010',true,'2026-01-02','[]'),
   (1,9,'same-position-replay','r2','o2','010',false,'2026-01-02','[]'),
   (2,1,'other tenant','r','o','010',false,'2026-01-01','[]'),
   (2,2,'insert-delete','r','o','010',false,'2026-01-01','[]'),
   (2,2,NULL,NULL,NULL,'010',true,'2026-01-01','[]'),
   (2,3,NULL,NULL,NULL,'010',true,'2026-01-01','[]')`)
		opts := destination.MergeOptions{TargetTable: target, StagingTable: stage, PrimaryKeys: []string{"tenant", "id"}, Columns: []string{"tenant", "id", "payload", "__ingestr_rank", "__ingestr_target", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"}}
		for range 2 {
			require.NoError(t, dest.MergeTable(ctx, opts))
			rows, err := db.Query("SELECT tenant,id,COALESCE(payload,'<null>'),_cdc_lsn,_cdc_deleted FROM " + target + " ORDER BY tenant,id")
			require.NoError(t, err)
			var actual []string
			for rows.Next() {
				var tenant, id int
				var payload, lsn string
				var deleted bool
				require.NoError(t, rows.Scan(&tenant, &id, &payload, &lsn, &deleted))
				actual = append(actual, fmt.Sprintf("%d/%d:%s:%s:%t", tenant, id, payload, lsn, deleted))
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			require.Equal(t, []string{"1/1:current:030:false", "1/2:deleted:030:true", "1/3:first:010:false", "1/4:keep:010:true", "1/5:new:040:false", "1/6:<null>:030:false", "1/7:new-before-delete:030:true", "1/8:same-position-update:010:true", "1/9:original:010:false", "2/1:other tenant:010:false", "2/2:insert-delete:010:true", "2/3:<null>:010:true"}, actual)
		}
		var rank, origin string
		require.NoError(t, db.QueryRow("SELECT __ingestr_rank,__ingestr_target FROM "+target+" WHERE tenant=1 AND id=5").Scan(&rank, &origin))
		require.Equal(t, []string{"r4", "o4"}, []string{rank, origin})
		opts.IncrementalPredicate = "id > 0"
		require.ErrorContains(t, dest.MergeTable(ctx, opts), "incremental predicates")
		opts.IncrementalPredicate = ""
		id, exists, err := dest.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		require.NoError(t, dest.TruncateCDCTable(ctx, target))
		after, _, err := dest.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.Equal(t, id, after)
		exec(t, "DROP TABLE "+target)
		exec(t, "CREATE TABLE "+target+" LIKE "+stage)
		require.ErrorContains(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, PrimaryKeys: []string{"tenant", "id"}, RequirePrimaryKeyMatch: true}), "requires PRIMARY KEY")
		exec(t, "DROP TABLE "+target)
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: opts.PrimaryKeys}))
		opts.CDCExpectedIncarnation = id
		require.ErrorContains(t, dest.MergeTable(ctx, opts), "incarnation changed")
	})

	t.Run("claims and checkpoint log", func(t *testing.T) {
		claims := database + ".claims"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: claims, Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "destination_table", DataType: schema.TypeString}, {Name: "connector_id", DataType: schema.TypeString}, {Name: "claimed_at", DataType: schema.TypeTimestamp}}}, PrimaryKeys: []string{"destination_table"}}))
		results := make([]error, 8)
		var wg sync.WaitGroup
		longTarget := database + "." + strings.Repeat("long_name_", 12)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = dest.ClaimCDCTarget(ctx, claims, destination.CDCTargetClaim{DestinationTable: longTarget, ConnectorID: fmt.Sprint(i), SourceTable: "public.items"})
			}()
		}
		wg.Wait()
		winners := 0
		for i, err := range results {
			if err == nil {
				winners++
				require.NoError(t, dest.ClaimCDCTarget(ctx, claims, destination.CDCTargetClaim{DestinationTable: longTarget, ConnectorID: fmt.Sprint(i), SourceTable: "public.items"}))
			} else {
				require.ErrorContains(t, err, "already claimed")
			}
		}
		require.Equal(t, 1, winners)
		state := database + ".state"
		stateCols := []schema.Column{{Name: "connector_id", DataType: schema.TypeString}, {Name: "event_id", DataType: schema.TypeString}, {Name: "source_table", DataType: schema.TypeString}, {Name: "destination_table", DataType: schema.TypeString}, {Name: "state_kind", DataType: schema.TypeString}, {Name: "state_generation", DataType: schema.TypeInt64}, {Name: "state_status", DataType: schema.TypeString}, {Name: "_cdc_lsn", DataType: schema.TypeString}}
		require.NoError(t, dest.PrepareCDCStateTable(ctx, state, &schema.TableSchema{Columns: stateCols}))
		connector, event := strings.Repeat("c", 100), strings.Repeat("e", 160)
		for range 2 {
			require.NoError(t, dest.Exec(ctx, "INSERT INTO "+state+" VALUES (?,?,'public.items','items','run',1,'started','010')", connector, event))
		}
		entries, err := dest.LoadCDCState(ctx, state, connector)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		fence, err := dest.LoadCDCStateFence(ctx, state, connector)
		require.NoError(t, err)
		require.Equal(t, []string{event}, fence.RunEventIDs)
		require.NoError(t, dest.DeleteCDCStateEvents(ctx, state, "other", []string{event}))
		entries, err = dest.LoadCDCState(ctx, state, connector)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.NoError(t, dest.DeleteCDCStateEvents(ctx, state, connector, []string{event}))
		entries, err = dest.LoadCDCState(ctx, state, connector)
		require.NoError(t, err)
		require.Empty(t, entries)
	})

	t.Run("schema evolution", func(t *testing.T) {
		table := database + ".evolution"
		exec(t, "CREATE TABLE "+table+" (id BIGINT NOT NULL, value INT NOT NULL) PRIMARY KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num'='1')")
		exec(t, "INSERT INTO "+table+" VALUES (1,100)")
		old := schema.Column{Name: "value", DataType: schema.TypeInt32}
		_, err := dest.ApplySchemaEvolution(ctx, table, &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{Type: schemaevolution.ChangeWidenType, ColumnName: "value", OldColumn: &old, NewColumn: schema.Column{Name: "value", DataType: schema.TypeInt64}}, {Type: schemaevolution.ChangeRelaxNullability, ColumnName: "value", OldColumn: &old}, {Type: schemaevolution.ChangeAddColumn, ColumnName: "extra", NewColumn: schema.Column{Name: "extra", DataType: schema.TypeString}}}})
		require.NoError(t, err)
		exec(t, "INSERT INTO "+table+" VALUES (2,3000000000,'new'),(3,NULL,NULL)")
		actual, err := dest.GetTableSchema(ctx, table)
		require.NoError(t, err)
		require.Equal(t, []string{"id"}, actual.PrimaryKeys)
		require.Equal(t, schema.TypeInt64, actual.Columns[1].DataType)
		require.True(t, actual.Columns[1].Nullable)

		keyless := database + ".evolution_keyless"
		exec(t, "CREATE TABLE "+keyless+" (id INT NOT NULL, label VARCHAR(10)) DISTRIBUTED BY RANDOM PROPERTIES ('replication_num'='1')")
		oldID, oldLabel := schema.Column{Name: "id", DataType: schema.TypeInt32}, schema.Column{Name: "label", DataType: schema.TypeString, MaxLength: 10, Nullable: true}
		_, err = dest.ApplySchemaEvolution(ctx, keyless, &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{Type: schemaevolution.ChangeWidenType, ColumnName: "id", OldColumn: &oldID, NewColumn: schema.Column{Name: "id", DataType: schema.TypeInt64}}, {Type: schemaevolution.ChangeRelaxNullability, ColumnName: "id", OldColumn: &oldID}, {Type: schemaevolution.ChangeWidenType, ColumnName: "label", OldColumn: &oldLabel, NewColumn: schema.Column{Name: "label", DataType: schema.TypeString, MaxLength: 100, Nullable: true}}}})
		require.NoError(t, err)
		exec(t, "INSERT INTO "+keyless+" VALUES (3000000000,'"+strings.Repeat("x", 50)+"'),(NULL,NULL)")
	})

	t.Run("MySQL CDC run lease and fenced swaps", func(t *testing.T) {
		connector := "lease-" + database
		first, err := dest.AcquireManagedCDCRunLease(ctx, connector)
		require.NoError(t, err)
		waitCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		_, err = dest.AcquireManagedCDCRunLease(waitCtx, connector)
		cancel()
		require.Error(t, err, "a second run must not acquire a held lease")
		require.NoError(t, first.Release())
		second, err := dest.AcquireManagedCDCRunLease(ctx, connector)
		require.NoError(t, err)
		select {
		case <-second.Done():
			t.Fatalf("lease lost unexpectedly: %v", second.Err())
		case <-time.After(7 * time.Second):
		}
		require.NoError(t, second.Release())

		columns := []schema.Column{{Name: "id", DataType: schema.TypeInt64}, {Name: "v", DataType: schema.TypeString, Nullable: true}}
		target, staged := database+".swap_target", database+"_staging.swap_stage"
		t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + database + "_staging FORCE") })
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: []string{"id"}}))
		exec(t, "INSERT INTO "+target+" VALUES (1, 'old')")
		user := "sr_reader_" + strings.ToLower(uniqueSuffix())
		exec(t, "CREATE USER IF NOT EXISTS '"+user+"'@'%' IDENTIFIED BY 'pw'")
		t.Cleanup(func() { _, _ = db.Exec("DROP USER IF EXISTS '" + user + "'@'%'") })
		exec(t, "GRANT SELECT ON TABLE "+target+" TO '"+user+"'@'%'")
		grants := func() string {
			t.Helper()
			rows, err := db.Query("SHOW GRANTS FOR '" + user + "'@'%'")
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			cols, _ := rows.Columns()
			var all []string
			for rows.Next() {
				values := make([]sql.NullString, len(cols))
				ptrs := make([]any, len(cols))
				for i := range values {
					ptrs[i] = &values[i]
				}
				require.NoError(t, rows.Scan(ptrs...))
				for _, v := range values {
					all = append(all, v.String)
				}
			}
			return strings.Join(all, "\n")
		}
		require.Contains(t, grants(), "swap_target")
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: staged, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: []string{"id"}, DropFirst: true}))
		exec(t, "INSERT INTO "+staged+" VALUES (2, 'replaced')")
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{StagingTable: staged, TargetTable: target}))
		var n int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM "+target+" WHERE id = 2").Scan(&n))
		require.Equal(t, 1, n)
		require.Contains(t, grants(), "swap_target", "a same-layout replace must keep the target's grants")

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: staged, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: []string{"id"}, DropFirst: true}))
		exec(t, "INSERT INTO "+staged+" VALUES (3, 'fenced')")
		before, exists, err := dest.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		stagingInc, resultInc, err := dest.CDCConditionalSwapIncarnations(ctx, target, staged)
		require.NoError(t, err)
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{StagingTable: staged, TargetTable: target, CDCExpectedIncarnation: before, CDCExpectedStagingIncarnation: stagingInc, CDCExpectedResultIncarnation: resultInc}))
		after, _, err := dest.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.Equal(t, resultInc, after)
		require.NoError(t, db.QueryRow("SELECT count(*) FROM "+target+" WHERE id = 3").Scan(&n))
		require.Equal(t, 1, n)
		var leftovers int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME LIKE '\\_\\_ingestr\\_swap\\_%'", database).Scan(&leftovers))
		require.Zero(t, leftovers, "the planner's cross-database copy must not be left in the target database")

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: staged, Schema: &schema.TableSchema{Columns: columns}, PrimaryKeys: []string{"id"}, DropFirst: true}))
		_, _, err = dest.CDCConditionalSwapIncarnations(ctx, target, staged)
		require.NoError(t, err)
		err = dest.SwapTable(ctx, destination.SwapOptions{StagingTable: staged, TargetTable: target, CDCExpectedIncarnation: before, CDCExpectedStagingIncarnation: "x", CDCExpectedResultIncarnation: "y"})
		require.Error(t, err, "a stale expected target incarnation must fence the swap")
		require.NoError(t, db.QueryRow("SELECT count(*) FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME LIKE '\\_\\_ingestr\\_swap\\_%'", database).Scan(&leftovers))
		require.Zero(t, leftovers, "a fenced swap must drop the planner's copy")
	})

	t.Run("Postgres snapshot resume stream and refresh", func(t *testing.T) {
		sourceSchema := database
		pg, sourceURI, pgExec := setupStarRocksCDCPostgres(t, ctx, pgSource(t), sourceSchema)
		pgExec(fmt.Sprintf(`CREATE TABLE %[1]s.items(id BIGINT PRIMARY KEY, payload TEXT, n BIGINT, active BOOLEAN, amount NUMERIC(18,4), doc JSONB, ts TIMESTAMP);
   ALTER TABLE %[1]s.items ALTER COLUMN payload SET STORAGE EXTERNAL;
   ALTER TABLE %[1]s.items ALTER COLUMN doc SET STORAGE EXTERNAL;
   INSERT INTO %[1]s.items SELECT g,repeat('abcdefghij',500),g,g%%2=0,g/7.0,jsonb_build_object('n',g,'large',repeat('json-value',500)),'2026-01-02 03:04:05.123456' FROM generate_series(1,300) g;
   CREATE TABLE %[1]s.composite(tenant BIGINT,id BIGINT,value TEXT,PRIMARY KEY(tenant,id));
   INSERT INTO %[1]s.composite VALUES (1,1,'a'),(2,1,'b');
   CREATE TABLE %[1]s.empty(id BIGINT PRIMARY KEY,value TEXT);
   CREATE TABLE %[1]s.keyless(value TEXT,n BIGINT);
   ALTER TABLE %[1]s.keyless REPLICA IDENTITY FULL;
   INSERT INTO %[1]s.keyless VALUES ('same',1),('same',1);
   CREATE PUBLICATION %[1]s FOR TABLE %[1]s.items,%[1]s.composite,%[1]s.empty,%[1]s.keyless;`, sourceSchema))
		cfg := config.DefaultConfig()
		cfg.CDCTableNaming = config.TableNamingTable
		cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + database + "&dest_schema=" + database + "&slot=" + database
		cfg.DestURI = uri
		cfg.StagingDataset = database + "_staging"
		t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + cfg.StagingDataset) })
		cfg.PageSize = 37
		cfg.IncrementalStrategy = config.StrategyMerge
		cfg.Progress = config.ProgressLog
		run := func() { t.Helper(); require.NoError(t, pipeline.New(cfg).Run(ctx)) }
		count := func(table string) int {
			t.Helper()
			var n int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+database+"."+table+" WHERE NOT _cdc_deleted").Scan(&n))
			return n
		}
		run()
		require.Equal(t, 300, count("items"))
		require.Equal(t, 2, count("composite"))
		require.Zero(t, count("empty"))
		require.Equal(t, 2, count("keyless"))
		pgExec(fmt.Sprintf(`UPDATE %[1]s.items SET n=n+1000,active=NOT active WHERE id<=100;
   UPDATE %[1]s.items SET n=n+1000 WHERE id<=100;
   DELETE FROM %[1]s.items WHERE id BETWEEN 101 AND 120;
   UPDATE %[1]s.items SET id=400,payload='moved',doc='{"n":300}' WHERE id=300;
   INSERT INTO %[1]s.items VALUES (301,'new',301,true,1.2345,'{"n":301}','2026-02-01');
   UPDATE %[1]s.composite SET value='changed' WHERE tenant=2;
   DELETE FROM %[1]s.composite WHERE tenant=1;
   INSERT INTO %[1]s.empty VALUES (1,'first');`, sourceSchema))
		pgExec("BEGIN; UPDATE " + sourceSchema + ".items SET payload='rolled back' WHERE id=1; ROLLBACK")
		run()
		require.Equal(t, 281, count("items"))
		require.Equal(t, 1, count("composite"))
		require.Equal(t, 1, count("empty"))
		var preserved int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM "+database+".items WHERE id<=100 AND length(payload)=5000 AND n=id+2000 AND active=(id%2<>0) AND length(get_json_string(doc,'$.large'))=5000").Scan(&preserved))
		require.Equal(t, 100, preserved)
		var amount, ts string
		var jsonN int
		require.NoError(t, db.QueryRow("SELECT CAST(amount AS STRING),CAST(ts AS STRING),get_json_int(doc,'$.n') FROM "+database+".items WHERE id=1").Scan(&amount, &ts, &jsonN))
		require.Equal(t, "0.1429", amount)
		require.Equal(t, "2026-01-02 03:04:05.123456", ts)
		require.Equal(t, 1, jsonN)
		run()
		require.Equal(t, 281, count("items"))
		require.Equal(t, 2, count("keyless"))
		pgExec("UPDATE " + sourceSchema + ".keyless SET n=2")
		run()
		var logRows, deletedRows int
		require.NoError(t, db.QueryRow("SELECT COUNT(*),SUM(CASE WHEN _cdc_deleted THEN 1 ELSE 0 END) FROM "+database+".keyless").Scan(&logRows, &deletedRows))
		require.Equal(t, 6, logRows)
		require.Equal(t, 2, deletedRows)
		pgExec("ALTER TABLE " + sourceSchema + ".items ADD COLUMN extra TEXT; UPDATE " + sourceSchema + ".items SET extra='added',payload=NULL WHERE id=1")
		run()
		var extra string
		var nullPayload bool
		require.NoError(t, db.QueryRow("SELECT extra,payload IS NULL FROM "+database+".items WHERE id=1").Scan(&extra, &nullPayload))
		require.Equal(t, "added", extra)
		require.True(t, nullPayload)
		pgExec("TRUNCATE " + sourceSchema + ".composite; INSERT INTO " + sourceSchema + ".composite VALUES(3,1,'after truncate')")
		run()
		require.Equal(t, 1, count("composite"))
		var tenant int
		require.NoError(t, db.QueryRow("SELECT tenant FROM "+database+".composite WHERE NOT _cdc_deleted").Scan(&tenant))
		require.Equal(t, 3, tenant)
		cfg.Stream = true
		cfg.FlushInterval = 200 * time.Millisecond
		cfg.FlushRecords = 1
		streamCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- pipeline.New(cfg).Run(streamCtx) }()
		stopped := false
		t.Cleanup(func() {
			cancel()
			if !stopped {
				select {
				case <-done:
				case <-time.After(30 * time.Second):
					t.Error("stream did not stop during cleanup")
				}
			}
		})
		pgExec("INSERT INTO " + sourceSchema + ".empty VALUES(2,'streamed')")
		require.Eventually(t, func() bool {
			var n int
			return db.QueryRow("SELECT COUNT(*) FROM "+database+".empty WHERE id=2 AND NOT _cdc_deleted").Scan(&n) == nil && n == 1
		}, 60*time.Second, 200*time.Millisecond)
		cancel()
		select {
		case <-done:
			stopped = true
		case <-time.After(30 * time.Second):
			t.Fatal("stream did not stop")
		}
		cfg.Stream = false
		run()
		require.Equal(t, 2, count("empty"))
		pgExec("ALTER TABLE " + sourceSchema + ".composite ADD COLUMN newkey BIGINT NOT NULL DEFAULT 7; ALTER TABLE " + sourceSchema + ".composite DROP CONSTRAINT composite_pkey; ALTER TABLE " + sourceSchema + ".composite ADD PRIMARY KEY(tenant,newkey); ALTER TABLE " + sourceSchema + ".composite DROP COLUMN value")
		pgExec("ALTER TABLE " + sourceSchema + ".keyless ADD COLUMN extra TEXT DEFAULT 'refreshed'")
		cfg.FullRefresh = true
		run()
		cfg.FullRefresh = false
		require.Equal(t, 281, count("items"))
		require.Equal(t, 2, count("keyless"))
		refreshed, err := dest.GetTableSchema(ctx, database+".composite")
		require.NoError(t, err)
		require.Equal(t, []string{"tenant", "newkey"}, refreshed.PrimaryKeys)
		require.NotContains(t, refreshed.ColumnNames(), "value")
		require.NotContains(t, refreshed.ColumnNames(), "_cdc_unchanged_cols")
		pgExec("UPDATE " + sourceSchema + ".composite SET id=42 WHERE tenant=3")
		run()
		var refreshedID int
		require.NoError(t, db.QueryRow("SELECT id FROM "+database+".composite WHERE tenant=3 AND newkey=7 AND NOT _cdc_deleted").Scan(&refreshedID))
		require.Equal(t, 42, refreshedID)
		cfg.SourceTable = sourceSchema + ".empty"
		cfg.DestTable = database + ".single"
		cfg.SourceURI = strings.Replace(cfg.SourceURI, "&slot="+database, "&slot="+database+"_single", 1)
		t.Cleanup(func() {
			_, _ = pg.Exec(context.Background(), "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1 AND NOT active", database+"_single")
		})
		cfg.FullRefresh = true
		run()
		cfg.FullRefresh = false
		require.Equal(t, 2, count("single"))
		pgExec("UPDATE " + sourceSchema + ".empty SET value='single resumed' WHERE id=1; DELETE FROM " + sourceSchema + ".empty WHERE id=2")
		run()
		require.Equal(t, 1, count("single"))
		var singleValue string
		require.NoError(t, db.QueryRow("SELECT value FROM "+database+".single WHERE id=1").Scan(&singleValue))
		require.Equal(t, "single resumed", singleValue)
	})
	t.Run("Postgres mid-stream re-snapshot keeps rows readable", func(t *testing.T) {
		name := database + "_soft"
		pg, sourceURI, pgExec := setupStarRocksCDCPostgres(t, ctx, pgSource(t), name)
		const rows = 20000
		pgExec(fmt.Sprintf(`CREATE TABLE %[1]s.soft_items(id BIGINT PRIMARY KEY, payload TEXT, dropme TEXT, n BIGINT);
   INSERT INTO %[1]s.soft_items SELECT g, 'p' || g, 'd' || g, g FROM generate_series(1,%[2]d) g;
   CREATE PUBLICATION %[1]s FOR TABLE %[1]s.soft_items;`, name, rows))
		cfg := config.DefaultConfig()
		cfg.CDCTableNaming = config.TableNamingTable
		cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + name + "&dest_schema=" + database + "&slot=" + name + "&discover_interval=1s"
		cfg.DestURI = uri
		cfg.StagingDataset = name + "_staging"
		t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + cfg.StagingDataset) })
		cfg.PageSize = 1000
		cfg.IncrementalStrategy = config.StrategyMerge
		cfg.Progress = config.ProgressLog
		table := database + ".soft_items"
		liveCount := func() int {
			t.Helper()
			var n int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE NOT _cdc_deleted").Scan(&n))
			return n
		}
		require.NoError(t, pipeline.New(cfg).Run(ctx))
		pgExec("DELETE FROM " + name + ".soft_items WHERE id = 1")
		require.NoError(t, pipeline.New(cfg).Run(ctx))
		require.Equal(t, rows-1, liveCount())
		// A row the new snapshot cannot contain; only the retire sweep removes it.
		_, err = db.Exec("INSERT INTO " + table + " (id, payload, n, _cdc_lsn, _cdc_deleted) VALUES (999999, 'ghost', 0, '00000000/00000001', false)")
		require.NoError(t, err)

		cfg.Stream = true
		cfg.FlushInterval = 200 * time.Millisecond
		cfg.FlushRecords = 2000
		streamCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- pipeline.New(cfg).Run(streamCtx) }()
		stopped := false
		stop := func() {
			cancel()
			if stopped {
				return
			}
			select {
			case <-done:
				stopped = true
			case <-time.After(30 * time.Second):
				t.Error("stream did not stop")
			}
		}
		t.Cleanup(stop)
		pgExec("UPDATE " + name + ".soft_items SET n = -2 WHERE id = 2")
		require.Eventually(t, func() bool {
			var n int
			return db.QueryRow("SELECT n FROM "+table+" WHERE id = 2").Scan(&n) == nil && n == -2
		}, 60*time.Second, 100*time.Millisecond)

		var (
			pollMu  sync.Mutex
			minLive = rows
			samples int
		)
		pollCtx, stopPoll := context.WithCancel(ctx)
		pollDone := make(chan struct{})
		go func() {
			defer close(pollDone)
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-pollCtx.Done():
					return
				case <-ticker.C:
				}
				var n int
				if db.QueryRowContext(pollCtx, "SELECT COUNT(*) FROM "+table+" WHERE NOT _cdc_deleted").Scan(&n) != nil {
					continue
				}
				pollMu.Lock()
				minLive = min(minLive, n)
				samples++
				pollMu.Unlock()
			}
		}()

		pgExec("ALTER TABLE " + name + ".soft_items DROP COLUMN dropme; UPDATE " + name + ".soft_items SET n = -3 WHERE id = 3")
		var walText string
		require.NoError(t, pg.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&walText))
		wal, err := pglogrepl.ParseLSN(walText)
		require.NoError(t, err)
		afterDDL := fmt.Sprintf("%08X/%08X", uint32(wal>>32), uint32(wal))
		require.Eventually(t, func() bool {
			var stale int
			return db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE NOT _cdc_deleted AND (_cdc_lsn IS NULL OR _cdc_lsn < ?)", afterDDL).Scan(&stale) == nil && stale == 0
		}, 180*time.Second, 200*time.Millisecond)
		stopPoll()
		<-pollDone
		stop()

		pollMu.Lock()
		t.Logf("live-row count minimum during re-snapshot: %d over %d samples", minLive, samples)
		require.GreaterOrEqual(t, minLive, rows-1)
		require.Greater(t, samples, 5)
		pollMu.Unlock()

		var tombstoneLSN string
		var tombstoneDeleted bool
		require.NoError(t, db.QueryRow("SELECT _cdc_lsn, _cdc_deleted FROM "+table+" WHERE id = 1").Scan(&tombstoneLSN, &tombstoneDeleted))
		require.True(t, tombstoneDeleted)
		require.Less(t, tombstoneLSN, afterDDL)
		var ghostLSN, ghostPayload string
		var ghostDeleted bool
		require.NoError(t, db.QueryRow("SELECT _cdc_lsn, _cdc_deleted, payload FROM "+table+" WHERE id = 999999").Scan(&ghostLSN, &ghostDeleted, &ghostPayload))
		require.True(t, ghostDeleted)
		require.GreaterOrEqual(t, ghostLSN, afterDDL)
		require.Equal(t, "ghost", ghostPayload)

		var sourceCount, sourceIDs, sourceN int64
		require.NoError(t, pg.QueryRow(ctx, "SELECT COUNT(*), SUM(id), SUM(n) FROM "+name+".soft_items").Scan(&sourceCount, &sourceIDs, &sourceN))
		var destCount, destIDs, destN int64
		require.NoError(t, db.QueryRow("SELECT COUNT(*), SUM(id), SUM(n) FROM "+table+" WHERE NOT _cdc_deleted").Scan(&destCount, &destIDs, &destN))
		require.Equal(t, []int64{sourceCount, sourceIDs, sourceN}, []int64{destCount, destIDs, destN})
		var payload string
		require.NoError(t, db.QueryRow("SELECT payload FROM "+table+" WHERE id = 3 AND n = -3 AND NOT _cdc_deleted").Scan(&payload))
		require.Equal(t, "p3", payload)
	})
	t.Run("Postgres varchar widened to text mid-stream", func(t *testing.T) {
		name := database + "_widen"
		_, sourceURI, pgExec := setupStarRocksCDCPostgres(t, ctx, pgSource(t), name)
		pgExec(fmt.Sprintf(`CREATE TABLE %[1]s.t(id INT PRIMARY KEY, name VARCHAR(5));
   INSERT INTO %[1]s.t SELECT g, 'n' || g FROM generate_series(1,100) g;
   CREATE PUBLICATION %[1]s FOR TABLE %[1]s.t;`, name))
		cfg := config.DefaultConfig()
		cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + name + "&slot=" + name
		cfg.SourceTable = name + ".t"
		cfg.DestURI = uri
		cfg.DestTable = database + ".widen"
		cfg.IncrementalStrategy = config.StrategyMerge
		cfg.Progress = config.ProgressLog
		require.NoError(t, pipeline.New(cfg).Run(ctx))

		cfg.Stream = true
		cfg.FlushInterval = 200 * time.Millisecond
		streamCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- pipeline.New(cfg).Run(streamCtx) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Error("stream did not stop")
			}
		})
		pgExec("ALTER TABLE " + name + ".t ALTER COLUMN name TYPE TEXT; INSERT INTO " + name + ".t VALUES (1000, repeat('z', 500))")
		require.Eventually(t, func() bool {
			select {
			case err := <-done:
				t.Fatalf("stream stopped: %v", err)
			default:
			}
			var n int
			return db.QueryRow("SELECT length(name) FROM "+database+".widen WHERE id = 1000 AND NOT _cdc_deleted").Scan(&n) == nil && n == 500
		}, 90*time.Second, 200*time.Millisecond)
		widened, err := dest.GetTableSchema(ctx, database+".widen")
		require.NoError(t, err)
		for _, col := range widened.Columns {
			if col.Name == "name" {
				require.GreaterOrEqual(t, col.MaxLength, 2000)
			}
		}
		var live int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+database+".widen WHERE NOT _cdc_deleted").Scan(&live))
		require.Equal(t, 101, live)
	})

	t.Run("Postgres snapshot values match WAL values", func(t *testing.T) {
		name := database + "_types"
		_, sourceURI, pgExec := setupStarRocksCDCPostgres(t, ctx, pgSource(t), name)
		columns := "t, ttz, iv, inet_h, bits, vb, money_c, r, tr, mr, pt, bx, tsv, c, t24, tarr, inarr, barr, far"
		pgExec(fmt.Sprintf(`CREATE TABLE %[1]s.t(id INT PRIMARY KEY, t TIME, ttz TIMETZ, iv INTERVAL, inet_h INET, bits BIT(4), vb VARBIT, money_c MONEY, r INT4RANGE, tr TSTZRANGE, mr INT4MULTIRANGE, pt POINT, bx BOX, tsv TSVECTOR, c CHAR(3), t24 TIME, tarr TIME[], inarr INET[], barr BIT(2)[], far TSTZRANGE);
   INSERT INTO %[1]s.t VALUES (1,'13:14:15.123456','10:00:00+02','1 day 02:03:04','10.0.0.1',B'1010',B'101','12.34','[1,5)','[2026-01-01,2026-02-01)','{[1,3),[5,7)}','(1,2)','((0,0),(1,1))','a fat cat','ab','24:00:00','{13:14:15.5,24:00:00}','{10.0.0.1}','{10,01}','[10000-01-01,10001-01-01)');
   CREATE PUBLICATION %[1]s FOR TABLE %[1]s.t;`, name))
		cfg := config.DefaultConfig()
		cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + name + "&slot=" + name
		cfg.SourceTable = name + ".t"
		cfg.DestURI = uri
		cfg.DestTable = database + ".types"
		cfg.IncrementalStrategy = config.StrategyMerge
		cfg.Progress = config.ProgressLog
		require.NoError(t, pipeline.New(cfg).Run(ctx))
		pgExec(fmt.Sprintf("INSERT INTO %[1]s.t SELECT 2, %[2]s FROM %[1]s.t WHERE id = 1", name, columns))
		require.NoError(t, pipeline.New(cfg).Run(ctx))
		var snapshot, wal string
		query := "SELECT concat_ws('|', " + strings.NewReplacer("tarr", "array_join(tarr, ',')", "inarr", "array_join(inarr, ',')", "barr", "array_join(barr, ',')").Replace(columns) + ") FROM " + database + ".types WHERE id = ?"
		require.NoError(t, db.QueryRow(query, 1).Scan(&snapshot))
		require.NoError(t, db.QueryRow(query, 2).Scan(&wal))
		require.Equal(t, wal, snapshot)
		require.Contains(t, snapshot, "13:14:15.123456|10:00:00+02|1 day 02:03:04|10.0.0.1|1010|101|$12.34|[1,5)|")
		require.Contains(t, snapshot, "|13:14:15.500000,00:00:00.000000|10.0.0.1|10,01|")
	})
}

// setupStarRocksCDCPostgres creates schema name on the CDC source and drops it,
// with the publication and inactive slot of the same name, on cleanup.
func setupStarRocksCDCPostgres(t *testing.T, ctx context.Context, sourceURI, name string) (*pgxpool.Pool, string, func(string)) {
	t.Helper()
	pg, err := pgxpool.New(ctx, sourceURI)
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	pgExec := func(query string) { t.Helper(); _, err := pg.Exec(ctx, query); require.NoError(t, err) }
	pgExec("CREATE SCHEMA " + name)
	t.Cleanup(func() {
		_, _ = pg.Exec(context.Background(), "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1 AND NOT active", name)
		_, _ = pg.Exec(context.Background(), "DROP PUBLICATION IF EXISTS "+name)
		_, _ = pg.Exec(context.Background(), "DROP SCHEMA "+name+" CASCADE")
	})
	return pg, sourceURI, pgExec
}
