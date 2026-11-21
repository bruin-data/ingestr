//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestStarRocksDestinationCorrectness(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	dsn, baseURI := sharedStarRocks(t)
	waitForStarRocksBackend(t, dsn)

	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	appDB := "sr_dest_" + strings.ToLower(uniqueSuffix())
	execEventually(t, db, "CREATE DATABASE IF NOT EXISTS "+appDB)
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + appDB + " FORCE") })
	execEventually(t, db, "CREATE TABLE IF NOT EXISTS "+appDB+".ready (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num'='1')")

	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.Path = "/" + appDB
	destURIWithDB := u.String()

	t.Run("unqualified dest-table resolves URI database across replace and merge", func(t *testing.T) {
		srcPath := filepath.Join(t.TempDir(), "src.duckdb")
		src := openDuckDBForTest(t, srcPath)
		execDuckDB(t, src, "CREATE TABLE items (id BIGINT PRIMARY KEY, label VARCHAR(50))")
		execDuckDB(t, src, "INSERT INTO items VALUES (1, 'café'), (2, 'naïve')")
		require.NoError(t, src.Close())
		sourceURI := fmt.Sprintf("duckdb:///%s", srcPath)

		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "main.items",
			DestURI:             destURIWithDB,
			DestTable:           "unqualified_items",
			IncrementalStrategy: config.StrategyReplace,
		}).Run(ctx))

		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+appDB+".unqualified_items").Scan(&count))
		require.Equal(t, 2, count)

		var stagingLeftovers int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME LIKE '%staging%'", appDB).Scan(&stagingLeftovers))
		require.Zero(t, stagingLeftovers)

		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "main.items",
			DestURI:             destURIWithDB,
			DestTable:           "unqualified_items",
			IncrementalStrategy: config.StrategyMerge,
			PrimaryKeys:         []string{"id", "ID"},
		}).Run(ctx))

		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+appDB+".unqualified_items").Scan(&count))
		require.Equal(t, 2, count)
	})

	t.Run("append with source primary keys preserves duplicate rows instead of upserting", func(t *testing.T) {
		srcPath := filepath.Join(t.TempDir(), "append_src.duckdb")
		src := openDuckDBForTest(t, srcPath)
		execDuckDB(t, src, "CREATE TABLE events (id BIGINT PRIMARY KEY, payload VARCHAR)")
		execDuckDB(t, src, "INSERT INTO events VALUES (1, 'first'), (2, 'first')")
		require.NoError(t, src.Close())
		sourceURI := fmt.Sprintf("duckdb:///%s", srcPath)

		for range 2 {
			require.NoError(t, pipeline.New(&config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "main.events",
				DestURI:             destURIWithDB,
				DestTable:           "append_events",
				IncrementalStrategy: config.StrategyAppend,
				PrimaryKeys:         []string{"id"},
			}).Run(ctx))
		}

		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+appDB+".append_events").Scan(&count))
		require.Equal(t, 4, count)
	})

	t.Run("replace swaps schema primary keys and supports >65KB UTF-8 strings", func(t *testing.T) {
		src1Path := filepath.Join(t.TempDir(), "r1.duckdb")
		src1 := openDuckDBForTest(t, src1Path)
		execDuckDB(t, src1, "CREATE TABLE docs (id BIGINT PRIMARY KEY, legacy_col VARCHAR)")
		execDuckDB(t, src1, "INSERT INTO docs VALUES (1, 'old')")
		require.NoError(t, src1.Close())

		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", src1Path),
			SourceTable:         "main.docs",
			DestURI:             destURIWithDB,
			DestTable:           "docs",
			IncrementalStrategy: config.StrategyReplace,
		}).Run(ctx))

		largeBody := strings.Repeat("🌟", 20000)
		src2Path := filepath.Join(t.TempDir(), "r2.duckdb")
		src2 := openDuckDBForTest(t, src2Path)
		execDuckDB(t, src2, "CREATE TABLE docs (tenant INT, id BIGINT, body VARCHAR, PRIMARY KEY (tenant, id))")
		_, err := src2.Exec("INSERT INTO docs VALUES (10, 1, ?)", largeBody)
		require.NoError(t, err)
		require.NoError(t, src2.Close())

		require.NoError(t, pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", src2Path),
			SourceTable:         "main.docs",
			DestURI:             destURIWithDB,
			DestTable:           "docs",
			IncrementalStrategy: config.StrategyReplace,
		}).Run(ctx))

		var gotBody string
		require.NoError(t, db.QueryRow("SELECT body FROM "+appDB+".docs WHERE tenant = 10 AND id = 1").Scan(&gotBody))
		require.Equal(t, largeBody, gotBody)

		var tblName, createDDL string
		require.NoError(t, db.QueryRow("SHOW CREATE TABLE "+appDB+".docs").Scan(&tblName, &createDDL))
		require.NotContains(t, createDDL, "legacy_col")
		require.Contains(t, createDDL, "PRIMARY KEY(`tenant`, `id`)")
	})

	t.Run("exotic column names with binary and fixed-size binary round-trip accurately", func(t *testing.T) {
		dest := starrocks.NewStarRocksDestination()
		require.NoError(t, dest.Connect(ctx, destURIWithDB))
		defer func() { _ = dest.Close(ctx) }()

		tblSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "$price", DataType: schema.TypeDecimal, Precision: 12, Scale: 2, Nullable: true},
				{Name: "a.b.c", DataType: schema.TypeString, Nullable: true},
				{Name: "items[0]", DataType: schema.TypeString, Nullable: true},
				{Name: "a,b", DataType: schema.TypeString, Nullable: true},
				{Name: "_c0", DataType: schema.TypeBinary, Nullable: true},
				{Name: "fixed_bin", DataType: schema.TypeBinary, Nullable: true},
			},
		}
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:       "exotic_cols",
			Schema:      tblSchema,
			PrimaryKeys: []string{"id"},
			DropFirst:   true,
		}))

		pool := memory.NewGoAllocator()
		idB := array.NewInt64Builder(pool)
		idB.AppendValues([]int64{1, 2, 3}, nil)
		idArr := idB.NewArray()
		idB.Release()
		defer idArr.Release()

		priceB := array.NewStringBuilder(pool)
		priceB.AppendValues([]string{"19.95", "0.05", ""}, []bool{true, true, false})
		priceArr := priceB.NewArray()
		priceB.Release()
		defer priceArr.Release()

		dotB := array.NewStringBuilder(pool)
		dotB.AppendValues([]string{"dot1", "dot2", "dot3"}, nil)
		dotArr := dotB.NewArray()
		dotB.Release()
		defer dotArr.Release()

		idxB := array.NewStringBuilder(pool)
		idxB.AppendValues([]string{"idx1", "idx2", "idx3"}, nil)
		idxArr := idxB.NewArray()
		idxB.Release()
		defer idxArr.Release()

		commaB := array.NewStringBuilder(pool)
		commaB.AppendValues([]string{"c1", "c2", "c3"}, nil)
		commaArr := commaB.NewArray()
		commaB.Release()
		defer commaArr.Release()

		binB := array.NewBinaryBuilder(pool, arrow.BinaryTypes.Binary)
		binB.Append([]byte{})
		binB.Append([]byte{0x00, 0xFF, 0xFE})
		binB.AppendNull()
		binArr := binB.NewArray()
		binB.Release()
		defer binArr.Release()

		fsbB := array.NewFixedSizeBinaryBuilder(pool, &arrow.FixedSizeBinaryType{ByteWidth: 2})
		fsbB.Append([]byte{0xBE, 0xEF})
		fsbB.Append([]byte{0xCA, 0xFE})
		fsbB.AppendNull()
		fsbArr := fsbB.NewArray()
		fsbB.Release()
		defer fsbArr.Release()

		arrowSc := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "$price", Type: arrow.BinaryTypes.String},
			{Name: "a.b.c", Type: arrow.BinaryTypes.String},
			{Name: "items[0]", Type: arrow.BinaryTypes.String},
			{Name: "a,b", Type: arrow.BinaryTypes.String},
			{Name: "_c0", Type: arrow.BinaryTypes.Binary},
			{Name: "fixed_bin", Type: &arrow.FixedSizeBinaryType{ByteWidth: 2}},
		}, nil)
		batch := array.NewRecordBatch(arrowSc, []arrow.Array{idArr, priceArr, dotArr, idxArr, commaArr, binArr, fsbArr}, 3)
		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: batch}
		close(ch)

		require.NoError(t, dest.Write(ctx, ch, destination.WriteOptions{Table: "exotic_cols", Schema: tblSchema}))

		var price, dot, idxVal, comma, hexFixed string
		var binNull bool
		var binLen int
		require.NoError(t, db.QueryRow("SELECT CAST(`$price` AS STRING), `a.b.c`, `items[0]`, `a,b`, `_c0` IS NULL, COALESCE(LENGTH(`_c0`), -1), HEX(`fixed_bin`) FROM "+appDB+".exotic_cols WHERE id = 1").Scan(&price, &dot, &idxVal, &comma, &binNull, &binLen, &hexFixed))
		require.Equal(t, "19.95", price)
		require.Equal(t, "dot1", dot)
		require.Equal(t, "idx1", idxVal)
		require.Equal(t, "c1", comma)
		require.False(t, binNull, "empty VARBINARY must not become SQL NULL")
		require.Zero(t, binLen)
		require.Equal(t, "BEEF", hexFixed)

		var hexBin2 string
		require.NoError(t, db.QueryRow("SELECT HEX(`_c0`), HEX(`fixed_bin`) FROM "+appDB+".exotic_cols WHERE id = 2").Scan(&hexBin2, &hexFixed))
		require.Equal(t, "00FFFE", hexBin2)
		require.Equal(t, "CAFE", hexFixed)

		var null3, fixedNull3 bool
		require.NoError(t, db.QueryRow("SELECT `_c0` IS NULL, `fixed_bin` IS NULL FROM "+appDB+".exotic_cols WHERE id = 3").Scan(&null3, &fixedNull3))
		require.True(t, null3)
		require.True(t, fixedNull3)
	})

	t.Run("batched multi-column widenings run in one schema change and parallel abort force-drops staging", func(t *testing.T) {
		dest := starrocks.NewStarRocksDestination()
		require.NoError(t, dest.Connect(ctx, destURIWithDB))
		defer func() { _ = dest.Close(ctx) }()

		initSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "c1", DataType: schema.TypeInt16, Nullable: false},
				{Name: "c2", DataType: schema.TypeString, MaxLength: 10, Nullable: true},
			},
		}
		execEventually(t, db, "CREATE TABLE "+appDB+`.batch_evolve (
			id BIGINT NOT NULL,
			c1 SMALLINT NOT NULL,
			c2 VARCHAR(40) NULL
		) PRIMARY KEY (id) DISTRIBUTED BY HASH (id) PROPERTIES ('replication_num' = '1')`)

		oldC1 := schema.Column{Name: "c1", DataType: schema.TypeInt16, Nullable: false}
		oldC2 := schema.Column{Name: "c2", DataType: schema.TypeString, MaxLength: 40, Nullable: true}
		_, err := dest.ApplySchemaEvolution(ctx, "batch_evolve", &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{Type: schemaevolution.ChangeWidenType, ColumnName: "c1", OldColumn: &oldC1, NewColumn: schema.Column{Name: "c1", DataType: schema.TypeInt64, Nullable: true}},
				{Type: schemaevolution.ChangeRelaxNullability, ColumnName: "c1", OldColumn: &oldC1, NewColumn: schema.Column{Name: "c1", DataType: schema.TypeInt64, Nullable: true}},
				{Type: schemaevolution.ChangeWidenType, ColumnName: "c2", OldColumn: &oldC2, NewColumn: schema.Column{Name: "c2", DataType: schema.TypeString, MaxLength: 200, Nullable: true}},
			},
		})
		require.NoError(t, err)

		rows, err := db.Query(fmt.Sprintf("SHOW ALTER TABLE COLUMN FROM `%s` WHERE TableName = 'batch_evolve'", appDB))
		require.NoError(t, err)
		jobCount := 0
		for rows.Next() {
			jobCount++
		}
		require.NoError(t, rows.Close())
		require.Equal(t, 1, jobCount, "widen+nullability relaxation alongside another widening must execute in a single StarRocks schema change job")

		var c1Nullable string
		require.NoError(t, db.QueryRow("SELECT IS_NULLABLE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'batch_evolve' AND COLUMN_NAME = 'c1'", appDB).Scan(&c1Nullable))
		require.Equal(t, "YES", c1Nullable)

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:     "abort_staging",
			Schema:    initSchema,
			DropFirst: true,
		}))
		pool := memory.NewGoAllocator()
		ch := make(chan source.RecordBatchResult, 6)
		for i := range 4 {
			idB := array.NewInt64Builder(pool)
			idB.Append(int64(i + 1))
			idArr := idB.NewArray()
			idB.Release()
			c1B := array.NewInt16Builder(pool)
			c1B.Append(int16(i))
			c1Arr := c1B.NewArray()
			c1B.Release()
			c2B := array.NewStringBuilder(pool)
			c2B.Append("ok")
			c2Arr := c2B.NewArray()
			c2B.Release()
			sc := arrow.NewSchema([]arrow.Field{
				{Name: "id", Type: arrow.PrimitiveTypes.Int64},
				{Name: "c1", Type: arrow.PrimitiveTypes.Int16},
				{Name: "c2", Type: arrow.BinaryTypes.String},
			}, nil)
			rec := array.NewRecordBatch(sc, []arrow.Array{idArr, c1Arr, c2Arr}, 1)
			idArr.Release()
			c1Arr.Release()
			c2Arr.Release()
			ch <- source.RecordBatchResult{Batch: rec}
		}
		ch <- source.RecordBatchResult{Err: errors.New("upstream failed mid-stream")}
		close(ch)

		err = dest.WriteParallel(ctx, ch, destination.WriteOptions{
			Table:        "abort_staging",
			Schema:       initSchema,
			StagingTable: true,
			Parallelism:  4,
		})
		require.ErrorContains(t, err, "upstream failed mid-stream")
		require.NoError(t, dest.DropTable(ctx, "abort_staging"))
	})

	t.Run("merge rejects existing duplicate-key target table", func(t *testing.T) {
		_, err := db.Exec("CREATE TABLE " + appDB + `.dup_target (id BIGINT, name VARCHAR(100))
			DUPLICATE KEY(id) DISTRIBUTED BY RANDOM PROPERTIES ('replication_num'='1')`)
		require.NoError(t, err)

		srcPath := filepath.Join(t.TempDir(), "m.duckdb")
		src := openDuckDBForTest(t, srcPath)
		execDuckDB(t, src, "CREATE TABLE src (id BIGINT PRIMARY KEY, name VARCHAR(100))")
		execDuckDB(t, src, "INSERT INTO src VALUES (1, 'a')")
		require.NoError(t, src.Close())

		err = pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
			SourceTable:         "main.src",
			DestURI:             destURIWithDB,
			DestTable:           "dup_target",
			IncrementalStrategy: config.StrategyMerge,
			PrimaryKeys:         []string{"id"},
		}).Run(ctx)
		require.ErrorContains(t, err, "requires PRIMARY KEY (id)")
	})

	t.Run("append rejects existing primary-key target table without overwriting rows", func(t *testing.T) {
		execEventually(t, db, "CREATE TABLE "+appDB+`.pk_append_target (id BIGINT NOT NULL, name VARCHAR(100))
			PRIMARY KEY(id) DISTRIBUTED BY HASH(id) PROPERTIES ('replication_num'='1')`)
		_, err := db.Exec("INSERT INTO " + appDB + ".pk_append_target VALUES (1, 'original')")
		require.NoError(t, err)

		srcPath := filepath.Join(t.TempDir(), "a.duckdb")
		src := openDuckDBForTest(t, srcPath)
		execDuckDB(t, src, "CREATE TABLE src (id BIGINT PRIMARY KEY, name VARCHAR(100))")
		execDuckDB(t, src, "INSERT INTO src VALUES (1, 'overwritten')")
		require.NoError(t, src.Close())

		err = pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
			SourceTable:         "main.src",
			DestURI:             destURIWithDB,
			DestTable:           "pk_append_target",
			IncrementalStrategy: config.StrategyAppend,
			PrimaryKeys:         []string{"id"},
		}).Run(ctx)
		require.ErrorContains(t, err, "requires a DUPLICATE KEY table; found PRIMARY_KEYS")

		var current string
		require.NoError(t, db.QueryRow("SELECT name FROM "+appDB+".pk_append_target WHERE id = 1").Scan(&current))
		require.Equal(t, "original", current)
	})
}
