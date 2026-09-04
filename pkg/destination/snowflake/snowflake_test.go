package snowflake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	pqgo "github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	pqfile "github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	pqschema "github.com/apache/arrow-go/v18/parquet/schema"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMergeSQL(t *testing.T) {
	t.Run("non_cdc", func(t *testing.T) {
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id", "name", "updated_at"}, "", nil)

		assert.Contains(t, sql, `MERGE INTO "TARGET_SCHEMA"."TARGET_TBL" AS target`)
		assert.Contains(t, sql, `FROM "STAGING_SCHEMA"."STAGING_TBL"`)
		assert.Contains(t, sql, "ORDER BY (SELECT NULL)")
		assert.Contains(t, sql, `ON target."ID" = source."ID"`)
		assert.Contains(t, sql, "WHEN MATCHED THEN")
		assert.Contains(t, sql, `target."NAME" = source."NAME"`)
		assert.NotContains(t, sql, `UPDATE SET target."ID" = source."ID"`)
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN")
		assert.Contains(t, sql, `INSERT ("ID", "NAME", "UPDATED_AT")`)
		assert.Contains(t, sql, `VALUES (source."ID", source."NAME", source."UPDATED_AT")`)
		assert.NotContains(t, sql, "_CDC_DELETED")
	})

	t.Run("non_cdc_with_incremental_key", func(t *testing.T) {
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id", "name", "updated_at"}, "updated_at", nil)

		assert.Contains(t, sql, `ORDER BY "UPDATED_AT" DESC`)
		assert.NotContains(t, sql, "ORDER BY (SELECT NULL)")
	})

	t.Run("incremental predicate", func(t *testing.T) {
		sql := buildMergeSQLWithPredicate(
			"staging_schema.staging_tbl",
			"target_schema.target_tbl",
			[]string{"id"},
			[]string{"id", "event_date"},
			"",
			nil,
			"target.\"EVENT_DATE\" >= DATEADD(day, -7, CURRENT_DATE())",
		)

		assert.Contains(t, sql, `ON target."ID" = source."ID" AND (target."EVENT_DATE" >= DATEADD(day, -7, CURRENT_DATE()))`)
	})

	t.Run("non_cdc_all_pk_columns", func(t *testing.T) {
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id"}, "", nil)
		assert.NotContains(t, sql, "WHEN MATCHED THEN")
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN")
	})

	t.Run("cdc", func(t *testing.T) {
		columns := []string{"id", "name", "value", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil)

		// Composed source: data columns from the latest non-deleted change,
		// CDC columns from the latest change overall.
		assert.Contains(t, sql, `SELECT la."ID", act."NAME", act."VALUE", la."_CDC_LSN", la."_CDC_DELETED", la."_CDC_SYNCED_AT", act."_CDC_LSN" IS NOT NULL AS "__INGESTR_HAS_ACTIVE", act."_CDC_LSN" AS "__INGESTR_ACTIVE_LSN"`)
		assert.Contains(t, sql, `ORDER BY "_CDC_LSN" DESC, "_CDC_DELETED" DESC`)
		assert.Contains(t, sql, `WHERE "_CDC_DELETED" = false`)
		assert.Contains(t, sql, `WHEN MATCHED AND (target."_CDC_LSN" IS NULL OR source."_CDC_LSN" > target."_CDC_LSN" OR (source."_CDC_LSN" = target."_CDC_LSN" AND source."_CDC_DELETED" = true AND COALESCE(target."_CDC_DELETED", false) = false)) AND (source."_CDC_DELETED" = false OR (source."__INGESTR_HAS_ACTIVE" AND (target."_CDC_LSN" IS NULL OR source."__INGESTR_ACTIVE_LSN" >= target."_CDC_LSN"))) THEN`)
		assert.Contains(t, sql, `WHEN MATCHED AND (target."_CDC_LSN" IS NULL OR source."_CDC_LSN" > target."_CDC_LSN" OR (source."_CDC_LSN" = target."_CDC_LSN" AND source."_CDC_DELETED" = true AND COALESCE(target."_CDC_DELETED", false) = false)) AND source."_CDC_DELETED" = true THEN`)
		assert.Contains(t, sql, `target."_CDC_DELETED" = true, target."_CDC_LSN" = source."_CDC_LSN", target."_CDC_SYNCED_AT" = source."_CDC_SYNCED_AT"`)
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN\n")
		assert.NotContains(t, sql, `WHEN NOT MATCHED AND (source."_CDC_DELETED" = false OR source."__INGESTR_HAS_ACTIVE") THEN`)
	})

	t.Run("cdc_uppercased_columns", func(t *testing.T) {
		// The naming layer commonly uppercases columns for Snowflake; CDC
		// detection must be case-insensitive.
		columns := []string{"ID", "NAME", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"ID"}, columns, "", nil)

		assert.Contains(t, sql, `"__INGESTR_HAS_ACTIVE"`)
		assert.Contains(t, sql, `AND source."_CDC_DELETED" = true THEN`)
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN\n")
	})

	t.Run("cdc_resume_uppercased_columns", func(t *testing.T) {
		// On an incremental/resume run the schema is read back from Snowflake, which folds
		// unquoted identifiers to upper case, while the staging-only _cdc_unchanged_cols is
		// appended from the source schema in lower case. CDC detection must stay case-insensitive
		// so the unchanged-column preservation (IFF/ARRAY_CONTAINS) is still emitted.
		columns := []string{"ID", "NAME", "CONFIG_DATA", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT", "_cdc_unchanged_cols"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil)

		assert.Contains(t, sql, `AND (source."_CDC_DELETED" = false OR (source."__INGESTR_HAS_ACTIVE" AND (target."_CDC_LSN" IS NULL OR source."__INGESTR_ACTIVE_LSN" >= target."_CDC_LSN"))) THEN`)
		assert.Contains(t, sql, `"CONFIG_DATA" = IFF(COALESCE(ARRAY_CONTAINS(TO_VARIANT('config_data'), TRY_PARSE_JSON(LOWER(source."_CDC_UNCHANGED_COLS"))), TRUE), target."CONFIG_DATA", source."CONFIG_DATA")`)
		// staging-only column must not be persisted on the destination
		assert.Contains(t, sql, "INSERT (\"ID\", \"NAME\", \"CONFIG_DATA\", \"_CDC_LSN\", \"_CDC_DELETED\", \"_CDC_SYNCED_AT\")\n")
		assert.NotContains(t, sql, `INSERT ("ID", "NAME", "CONFIG_DATA", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT", "_CDC_UNCHANGED_COLS")`)
	})

	t.Run("cdc_without_unchanged_cols_column", func(t *testing.T) {
		// Sources that materialize full change rows (e.g. SQL Server CDC) emit
		// no _cdc_unchanged_cols; the merge must not reference it.
		columns := []string{"id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil)

		assert.NotContains(t, sql, "_CDC_UNCHANGED_COLS")
		assert.Contains(t, sql, `target."NAME" = source."NAME"`)
	})

	t.Run("cdc_only_pk_and_metadata", func(t *testing.T) {
		columns := []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil)

		assert.Contains(t, sql, `AND (source."_CDC_DELETED" = false OR (source."__INGESTR_HAS_ACTIVE" AND (target."_CDC_LSN" IS NULL OR source."__INGESTR_ACTIVE_LSN" >= target."_CDC_LSN"))) THEN`)
		assert.Contains(t, sql, `target."_CDC_LSN" = source."_CDC_LSN"`)
		assert.NotContains(t, sql, `target."NAME" = source."NAME"`)
		assert.Contains(t, sql, `AND source."_CDC_DELETED" = true THEN`)
		assert.Contains(t, sql, "WHEN NOT MATCHED THEN\n")
	})

	t.Run("cdc_incremental_predicate", func(t *testing.T) {
		columns := []string{"id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}
		predicate := `target."ID" > 100`
		sql := buildMergeSQLWithPredicate("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil, predicate)

		assert.Contains(t, sql, "ON target.\"ID\" = source.\"ID\"\n")
		assert.NotContains(t, sql, `ON target."ID" = source."ID" AND (`+predicate+`)`)
		assert.Contains(t, sql, `WHEN MATCHED AND (`+predicate+`) AND (target."_CDC_LSN" IS NULL`)
	})

	t.Run("non_cdc_internal_alias_collision", func(t *testing.T) {
		columns := []string{"id", "__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2"}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil)

		assert.Contains(t, sql, `AS "__BRUIN_DEDUP_RN_3"`)
		assert.Contains(t, sql, `WHERE "__BRUIN_DEDUP_RN_3" = 1`)
	})

	t.Run("cdc_internal_alias_collision", func(t *testing.T) {
		columns := []string{
			"id", "payload",
			"__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2",
			`"__ingestr_has_active"`, `"__ingestr_has_active_2"`,
			`"__ingestr_active_lsn"`, `"__ingestr_active_lsn_2"`,
			"_cdc_lsn", "_cdc_deleted", "_cdc_synced_at",
		}
		sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, columns, "", nil)

		assert.Contains(t, sql, `AS "__BRUIN_DEDUP_RN_3"`)
		assert.Contains(t, sql, `WHERE "__BRUIN_DEDUP_RN_3" = 1`)
		assert.Contains(t, sql, `AS "__INGESTR_HAS_ACTIVE_3"`)
		assert.Contains(t, sql, `source."__INGESTR_HAS_ACTIVE_3"`)
		assert.Contains(t, sql, `act."_CDC_LSN" AS "__INGESTR_ACTIVE_LSN_3"`)
		assert.Contains(t, sql, `source."__INGESTR_ACTIVE_LSN_3" >= target."_CDC_LSN"`)
		assert.Contains(t, sql, `target."__ingestr_has_active" = source."__ingestr_has_active"`)
		assert.Contains(t, sql, `target."__ingestr_has_active_2" = source."__ingestr_has_active_2"`)
		assert.Contains(t, sql, `target."__ingestr_active_lsn" = source."__ingestr_active_lsn"`)
		assert.Contains(t, sql, `target."__ingestr_active_lsn_2" = source."__ingestr_active_lsn_2"`)
	})
}

func TestParseSchemaTable(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSchema string
		wantTable  string
	}{
		{
			name:       "schema and table",
			input:      "my_schema.my_table",
			wantSchema: "MY_SCHEMA",
			wantTable:  "MY_TABLE",
		},
		{
			name:       "table only defaults to PUBLIC",
			input:      "my_table",
			wantSchema: "PUBLIC",
			wantTable:  "MY_TABLE",
		},
		{
			name:       "already uppercase",
			input:      "ZENDESK.GROUPS",
			wantSchema: "ZENDESK",
			wantTable:  "GROUPS",
		},
		{
			name:       "three-part database.schema.table uses middle as schema",
			input:      "a.b.c",
			wantSchema: "B",
			wantTable:  "C",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := parseSchemaTable(tt.input)
			assert.Equal(t, tt.wantSchema, schema)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple name",
			input: "my_table",
			want:  `"MY_TABLE"`,
		},
		{
			name:  "already quoted",
			input: `"MY_TABLE"`,
			want:  `"MY_TABLE"`,
		},
		{
			name:  "uppercase",
			input: "GROUPS",
			want:  `"GROUPS"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteIdentifier(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRelaxColumnNullabilitySQL(t *testing.T) {
	var _ destination.NullabilityRelaxer = (*Dialect)(nil)

	d := &Dialect{}
	got := d.RelaxColumnNullabilitySQL("bruin_facebook.insights", "spend")
	assert.Equal(t, `ALTER TABLE bruin_facebook.insights ALTER COLUMN "SPEND" DROP NOT NULL`, got)
}

func TestImplicitTableStageName(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantStage string
	}{
		{
			name:      "standard schema.table produces valid implicit stage reference",
			table:     "zendesk.groups",
			wantStage: `"ZENDESK".%"GROUPS"`,
		},
		{
			name:      "table only defaults to PUBLIC schema",
			table:     "my_table",
			wantStage: `"PUBLIC".%"MY_TABLE"`,
		},
		{
			name:      "uppercase input",
			table:     "THIS_DOES_NOT_EXIST4.TEST",
			wantStage: `"THIS_DOES_NOT_EXIST4".%"TEST"`,
		},
		{
			name:      "underscores in names",
			table:     "my_schema.my_long_table_name",
			wantStage: `"MY_SCHEMA".%"MY_LONG_TABLE_NAME"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemaName, tableName := parseSchemaTable(tt.table)
			stageName := fmt.Sprintf(`%s.%%%s`, quoteIdentifier(schemaName), quoteIdentifier(tableName))
			assert.Equal(t, tt.wantStage, stageName)
		})
	}
}

func TestBuildCopyIntoSQLUsesParquetLogicalTypes(t *testing.T) {
	got := buildCopyIntoSQL(`"PUBLIC"."EVENTS"`, `"PUBLIC".%"EVENTS"`, "123456789", nil)
	want := `COPY INTO "PUBLIC"."EVENTS" FROM @"PUBLIC".%"EVENTS"/123456789/ FILE_FORMAT = (TYPE = PARQUET USE_LOGICAL_TYPE = TRUE) MATCH_BY_COLUMN_NAME = CASE_INSENSITIVE PURGE = TRUE`
	assert.Equal(t, want, got)
}

func TestBuildCopyIntoSQLWithFilesList(t *testing.T) {
	got := buildCopyIntoSQL(`"PUBLIC"."EVENTS"`, `"PUBLIC".%"EVENTS"`, "123456789", []string{"batch_0_1.parquet", "batch_1_1.parquet"})
	want := `COPY INTO "PUBLIC"."EVENTS" FROM @"PUBLIC".%"EVENTS"/123456789/ FILES = ('batch_0_1.parquet', 'batch_1_1.parquet') FILE_FORMAT = (TYPE = PARQUET USE_LOGICAL_TYPE = TRUE) MATCH_BY_COLUMN_NAME = CASE_INSENSITIVE PURGE = TRUE`
	assert.Equal(t, want, got)
}

func TestCopyFilesInBatchesCapsFilesClause(t *testing.T) {
	var executed []string
	capture := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		executed = append(executed, actualSQL)
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(capture))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("COPY INTO").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("COPY INTO").WillReturnResult(sqlmock.NewResult(0, 0))

	files := make([]string, maxFilesPerCopy+1)
	for i := range files {
		files[i] = fmt.Sprintf("batch_%d.parquet", i)
	}

	dest := &SnowflakeDestination{db: db}
	copied, err := dest.copyFilesInBatches(t.Context(), `"PUBLIC"."EVENTS"`, `"PUBLIC".%"EVENTS"`, "123456789", files)
	require.NoError(t, err)
	assert.Equal(t, len(files), copied)
	require.NoError(t, mock.ExpectationsWereMet())

	// Snowflake rejects a FILES clause with more than maxFilesPerCopy names.
	require.Len(t, executed, 2)
	assert.Equal(t, maxFilesPerCopy, strings.Count(executed[0], ".parquet'"))
	assert.Equal(t, 1, strings.Count(executed[1], ".parquet'"))
}

func TestSnowflakeParquetWriterTimestampLogicalTypes(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "created_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true},
		{Name: "synced_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
	}, nil)

	builder := array.NewRecordBuilder(mem, arrowSchema)
	builder.Field(0).(*array.TimestampBuilder).Append(arrow.Timestamp(1717245296789123))
	builder.Field(1).(*array.TimestampBuilder).Append(arrow.Timestamp(1717245296789123))
	record := builder.NewRecordBatch()
	builder.Release()
	defer record.Release()

	var buf bytes.Buffer
	writerProps, arrowProps := snowflakeParquetWriterProperties()
	writer, err := pqarrow.NewFileWriter(record.Schema(), &buf, writerProps, arrowProps)
	require.NoError(t, err)
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())

	reader, err := pqfile.NewParquetReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	createdAt := reader.MetaData().Schema.Column(0)
	assert.Equal(t, pqgo.Types.Int64, createdAt.PhysicalType())
	createdAtLogical, ok := createdAt.LogicalType().(pqschema.TimestampLogicalType)
	require.True(t, ok, "created_at logical type = %T", createdAt.LogicalType())
	assert.Equal(t, pqschema.TimeUnitMicros, createdAtLogical.TimeUnit())
	assert.False(t, createdAtLogical.IsAdjustedToUTC())

	syncedAt := reader.MetaData().Schema.Column(1)
	assert.Equal(t, pqgo.Types.Int64, syncedAt.PhysicalType())
	syncedAtLogical, ok := syncedAt.LogicalType().(pqschema.TimestampLogicalType)
	require.True(t, ok, "synced_at logical type = %T", syncedAt.LogicalType())
	assert.Equal(t, pqschema.TimeUnitMicros, syncedAtLogical.TimeUnit())
	assert.True(t, syncedAtLogical.IsAdjustedToUTC())
}

// The staged files are only useful if Snowflake can decode them, so assert the
// codec that actually lands in the footer and read a value back through it.
func TestSnowflakeParquetWriterCodec(t *testing.T) {
	roundTrip := func(t *testing.T) compress.Compression {
		t.Helper()
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer mem.AssertSize(t, 0)

		builder := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil))
		builder.Field(0).(*array.Int64Builder).Append(4242)
		record := builder.NewRecordBatch()
		builder.Release()
		defer record.Release()

		var buf bytes.Buffer
		writerProps, arrowProps := snowflakeParquetWriterProperties()
		writer, err := pqarrow.NewFileWriter(record.Schema(), &buf, writerProps, arrowProps)
		require.NoError(t, err)
		require.NoError(t, writer.Write(record))
		require.NoError(t, writer.Close())

		reader, err := pqfile.NewParquetReader(bytes.NewReader(buf.Bytes()))
		require.NoError(t, err)
		defer func() { _ = reader.Close() }()

		chunk, err := reader.MetaData().RowGroup(0).ColumnChunk(0)
		require.NoError(t, err)

		col, err := reader.RowGroup(0).Column(0)
		require.NoError(t, err)
		values := make([]int64, 1)
		read, _, err := col.(*pqfile.Int64ColumnChunkReader).ReadBatch(1, values, nil, nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), read)
		assert.Equal(t, int64(4242), values[0], "value must survive the round trip")

		return chunk.Compression()
	}

	t.Run("default is zstd", func(t *testing.T) {
		assert.Equal(t, compress.Codecs.Zstd, roundTrip(t))
	})

	t.Run("env override", func(t *testing.T) {
		t.Setenv("INGESTR_SNOWFLAKE_PARQUET_CODEC", "SNAPPY")
		assert.Equal(t, compress.Codecs.Snappy, roundTrip(t))
	})

	t.Run("unrecognized override falls back to zstd", func(t *testing.T) {
		t.Setenv("INGESTR_SNOWFLAKE_PARQUET_CODEC", "lz4")
		assert.Equal(t, compress.Codecs.Zstd, roundTrip(t))
	})
}

func TestSnowflakeTargetFileBytes(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int64
	}{
		{name: "unset", env: "", want: 32 << 20},
		{name: "override", env: "8", want: 8 << 20},
		{name: "zero flushes per batch", env: "0", want: 0},
		{name: "negative falls back", env: "-1", want: 32 << 20},
		{name: "unparseable falls back", env: "big", want: 32 << 20},
		// Without the clamp the shift overflows to a negative size, which
		// panics the buffer preallocation.
		{name: "absurd value is clamped", env: "999999999999999", want: 1024 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", tt.env)
			assert.Equal(t, tt.want, snowflakeTargetFileBytes())
		})
	}
}

// A multi-table write runs one WriteParallel per table concurrently, so files
// have to close early once those uploaders collectively hold more than the
// shared budget -- but only for uploaders actually holding something, or the
// wide loads the budget exists for would be pushed into tiny files.
func TestUploaderShouldFlush(t *testing.T) {
	withBufferedBytes := func(t *testing.T, n int64) {
		t.Helper()
		bufferedBytes.Store(n)
		t.Cleanup(func() { bufferedBytes.Store(0) })
	}
	buffered := func(n int) *bytes.Buffer { return bytes.NewBuffer(make([]byte, n)) }

	t.Run("under the target and under budget", func(t *testing.T) {
		withBufferedBytes(t, 0)
		w := &snowflakeFileUploader{targetBytes: 32 << 20, buf: buffered(4 << 20)}
		assert.False(t, w.shouldFlush())
	})

	t.Run("at the target", func(t *testing.T) {
		withBufferedBytes(t, 0)
		w := &snowflakeFileUploader{targetBytes: 32 << 20, buf: buffered(32 << 20)}
		assert.True(t, w.shouldFlush())
	})

	t.Run("over budget closes early", func(t *testing.T) {
		withBufferedBytes(t, uploaderBudgetBytes)
		w := &snowflakeFileUploader{targetBytes: 32 << 20, buf: buffered(4 << 20)}
		assert.True(t, w.shouldFlush())
	})

	t.Run("over budget still respects the adaptive floor", func(t *testing.T) {
		withBufferedBytes(t, uploaderBudgetBytes)
		w := &snowflakeFileUploader{targetBytes: 32 << 20, buf: buffered(minAdaptiveFlushBytes - 1)}
		assert.False(t, w.shouldFlush(), "an idle uploader must not be pushed into sub-MiB files")
	})

	t.Run("idle uploader holds nothing", func(t *testing.T) {
		withBufferedBytes(t, uploaderBudgetBytes)
		w := &snowflakeFileUploader{targetBytes: 32 << 20}
		assert.False(t, w.shouldFlush())
	})

	t.Run("zero target flushes every batch", func(t *testing.T) {
		withBufferedBytes(t, 0)
		w := &snowflakeFileUploader{targetBytes: 0}
		assert.True(t, w.shouldFlush())
	})
}

func TestGetTableSchemaPreservesSnowflakeTypeMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"name", "type", "null?"}).
		AddRow("ID", "NUMBER(38,0)", "N").
		AddRow("BUDGET", "NUMBER(20,2)", "Y").
		AddRow("NAME", "VARCHAR(255)", "Y")
	mock.ExpectQuery("DESCRIBE TABLE").WillReturnRows(rows)

	dest := &SnowflakeDestination{db: db}
	got, err := dest.GetTableSchema(context.Background(), "public.campaigns")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Columns, 3)

	assert.Equal(t, schema.TypeDecimal, got.Columns[0].DataType)
	assert.Equal(t, 38, got.Columns[0].Precision)
	assert.Equal(t, 0, got.Columns[0].Scale)
	assert.False(t, got.Columns[0].Nullable)

	assert.Equal(t, schema.TypeDecimal, got.Columns[1].DataType)
	assert.Equal(t, 20, got.Columns[1].Precision)
	assert.Equal(t, 2, got.Columns[1].Scale)
	assert.True(t, got.Columns[1].Nullable)

	assert.Equal(t, schema.TypeString, got.Columns[2].DataType)
	assert.Equal(t, 255, got.Columns[2].MaxLength)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMapSnowflakeTypeToColumn(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  schema.DataType
		precision int
		scale     int
		maxLength int
	}{
		{name: "bare_number", input: "NUMBER", wantType: schema.TypeDecimal, precision: 38},
		{name: "number_with_precision_scale", input: "NUMBER(20,2)", wantType: schema.TypeDecimal, precision: 20, scale: 2},
		{name: "decimal_with_precision_only", input: "DECIMAL(18)", wantType: schema.TypeDecimal, precision: 18},
		{name: "numeric_with_spaces", input: "NUMERIC( 12, 4 )", wantType: schema.TypeDecimal, precision: 12, scale: 4},
		{name: "varchar_length", input: "VARCHAR(1024)", wantType: schema.TypeString, maxLength: 1024},
		{name: "timestamp_precision", input: "TIMESTAMP_NTZ(9)", wantType: schema.TypeTimestamp},
		{name: "timestamp_tz_precision", input: "TIMESTAMP_TZ(9)", wantType: schema.TypeTimestampTZ},
		{name: "time_precision", input: "TIME(9)", wantType: schema.TypeTime},
		{name: "binary_length", input: "BINARY(8388608)", wantType: schema.TypeBinary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapSnowflakeTypeToColumn(tt.input)
			assert.Equal(t, tt.wantType, got.DataType)
			assert.Equal(t, tt.precision, got.Precision)
			assert.Equal(t, tt.scale, got.Scale)
			assert.Equal(t, tt.maxLength, got.MaxLength)
		})
	}
}

func TestEmptyDecimalBatchAlignsToSnowflakeDescribedScale(t *testing.T) {
	decimalType := &arrow.Decimal128Type{Precision: 20, Scale: 2}
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "BUDGET", Type: decimalType, Nullable: true},
	}, nil)

	builder := array.NewDecimal128Builder(memory.DefaultAllocator, decimalType)
	decimalArray := builder.NewArray()
	builder.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{decimalArray}, 0)
	decimalArray.Release()
	defer record.Release()

	targetCol := mapSnowflakeTypeToColumn("NUMBER(20,2)")
	targetCol.Name = "BUDGET"
	targetCol.Nullable = true
	targetSchema := (&schema.TableSchema{Columns: []schema.Column{targetCol}}).ToArrowSchema()

	got, err := databuffer.CastRecordToSchema(record, targetSchema, true)
	require.NoError(t, err)
	defer got.Release()

	assert.Equal(t, int64(0), got.NumRows())
	assert.True(t, got.Schema().Equal(targetSchema))
}

func TestMapDataTypeToSnowflake(t *testing.T) {
	tests := []struct {
		name string
		col  schema.Column
		want string
	}{
		{name: "boolean", col: schema.Column{DataType: schema.TypeBoolean}, want: "BOOLEAN"},
		{name: "int16", col: schema.Column{DataType: schema.TypeInt16}, want: "SMALLINT"},
		{name: "int32", col: schema.Column{DataType: schema.TypeInt32}, want: "INTEGER"},
		{name: "int64", col: schema.Column{DataType: schema.TypeInt64}, want: "BIGINT"},
		{name: "float32", col: schema.Column{DataType: schema.TypeFloat32}, want: "FLOAT"},
		{name: "float64", col: schema.Column{DataType: schema.TypeFloat64}, want: "DOUBLE"},
		{name: "string", col: schema.Column{DataType: schema.TypeString}, want: "VARCHAR"},
		{name: "string with length", col: schema.Column{DataType: schema.TypeString, MaxLength: 100}, want: "VARCHAR(100)"},
		{name: "decimal default", col: schema.Column{DataType: schema.TypeDecimal}, want: "NUMBER(38,0)"},
		{name: "decimal with precision", col: schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2}, want: "NUMBER(10,2)"},
		{name: "date", col: schema.Column{DataType: schema.TypeDate}, want: "DATE"},
		{name: "time", col: schema.Column{DataType: schema.TypeTime}, want: "TIME"},
		{name: "timestamp", col: schema.Column{DataType: schema.TypeTimestamp}, want: "TIMESTAMP_NTZ"},
		{name: "timestamp_tz", col: schema.Column{DataType: schema.TypeTimestampTZ}, want: "TIMESTAMP_TZ"},
		{name: "json", col: schema.Column{DataType: schema.TypeJSON}, want: "VARIANT"},
		{name: "uuid", col: schema.Column{DataType: schema.TypeUUID}, want: "VARCHAR(36)"},
		{name: "binary", col: schema.Column{DataType: schema.TypeBinary}, want: "BINARY"},
		{name: "array", col: schema.Column{DataType: schema.TypeArray}, want: "ARRAY"},
		{name: "interval", col: schema.Column{DataType: schema.TypeInterval}, want: "VARCHAR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapDataTypeToSnowflake(tt.col)
			assert.Equal(t, tt.want, got)
		})
	}
}

func newSingleRowRecordBatch(mem memory.Allocator) arrow.RecordBatch {
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	builder := array.NewRecordBuilder(mem, arrowSchema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(1)
	return builder.NewRecordBatch()
}

func TestWriteParallelNonStagingCopiesByPrefix(t *testing.T) {
	t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", "0")
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("PUT file://batch_").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`COPY INTO .* FROM .*/ FILE_FORMAT =`).WillReturnResult(sqlmock.NewResult(0, 0))

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	close(records)

	dest := &SnowflakeDestination{db: db}
	require.NoError(t, dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:       "public.events",
		Parallelism: 1,
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func newSingleRowRecordBatchWithColumns(mem memory.Allocator, names ...string) arrow.RecordBatch {
	fields := make([]arrow.Field, len(names))
	for i, name := range names {
		fields[i] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64}
	}
	builder := array.NewRecordBuilder(mem, arrow.NewSchema(fields, nil))
	defer builder.Release()
	for i := range names {
		builder.Field(i).(*array.Int64Builder).Append(int64(i))
	}
	return builder.NewRecordBatch()
}

// A retargeting schema aligner can change the Arrow schema mid-stream. The
// parquet writer rejects a record whose schema differs from the one it was
// created with, so the uploader has to roll over to a new file instead of
// failing the whole write.
func TestWriteParallelRollsOverFileOnSchemaChange(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("PUT file://batch_").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("PUT file://batch_").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("COPY INTO").WillReturnResult(sqlmock.NewResult(0, 0))

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatchWithColumns(mem, "id")}
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatchWithColumns(mem, "id", "extra")}
	close(records)

	dest := &SnowflakeDestination{db: db}
	require.NoError(t, dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:       "public.events",
		Parallelism: 1,
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func newIncompressibleRecordBatch(mem memory.Allocator, totalBytes int) arrow.RecordBatch {
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "payload", Type: arrow.BinaryTypes.Binary},
	}, nil)
	builder := array.NewRecordBuilder(mem, arrowSchema)
	defer builder.Release()
	rng := rand.New(rand.NewPCG(1, 2))
	const rowBytes = 64 << 10
	for written := 0; written < totalBytes; written += rowBytes {
		payload := make([]byte, rowBytes)
		for i := range payload {
			payload[i] = byte(rng.Uint32())
		}
		builder.Field(0).(*array.BinaryBuilder).Append(payload)
	}
	return builder.NewRecordBatch()
}

// When the source stalls, an uploader holding at least minAdaptiveFlushBytes
// uploads what it has instead of idling until the size target is reached. The
// second batch is only sent after the first PUT is observed, so the test
// deadlocks (and times out) if the adaptive flush does not fire.
func TestWriteParallelAdaptiveFlushUploadsWhileSourceIsIdle(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	firstPut := make(chan struct{})
	var signalOnce sync.Once
	capture := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if strings.HasPrefix(actualSQL, "PUT ") {
			signalOnce.Do(func() { close(firstPut) })
		}
		return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(capture))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("PUT file://batch_0_1.parquet").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("PUT file://batch_0_2.parquet").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("COPY INTO").WillReturnResult(sqlmock.NewResult(0, 0))

	records := make(chan source.RecordBatchResult)
	go func() {
		defer close(records)
		records <- source.RecordBatchResult{Batch: newIncompressibleRecordBatch(mem, 2*minAdaptiveFlushBytes)}
		select {
		case <-firstPut:
		case <-time.After(10 * time.Second):
			t.Error("uploader did not flush while the source was idle")
			return
		}
		records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	}()

	dest := &SnowflakeDestination{db: db}
	require.NoError(t, dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:       "public.events",
		Parallelism: 1,
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

// A failed PUT aborts the write and leaves queued batches for the caller's
// cancellation and drain path. No COPY may publish the partial load.
func TestWriteParallelAbortsOnUploadFailure(t *testing.T) {
	t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", "0") // one PUT per batch
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	var mu sync.Mutex
	statements := map[string]bool{}
	capture := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		mu.Lock()
		defer mu.Unlock()
		statements[actualSQL] = true
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(capture))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	putErr := errors.New("stage is full")
	mock.ExpectExec("PUT").WillReturnError(putErr)
	// sqlmock stops consulting the matcher once every expectation is
	// fulfilled, so leave surplus ones: without them a statement issued after
	// the failure would never be recorded and the assertions below could not
	// fail.
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < 4; i++ {
		mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))
	}

	const batches = 8
	records := make(chan source.RecordBatchResult, batches)
	defer func() {
		for result := range records {
			result.Batch.Release()
		}
	}()
	for i := 0; i < batches; i++ {
		records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	}
	close(records)

	dest := &SnowflakeDestination{db: db}
	err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:        "public.events",
		Parallelism:  1,
		StagingTable: true,
	})
	require.ErrorIs(t, err, putErr)
	assert.Contains(t, err.Error(), "snowflake parallel write failed")
	assert.Len(t, records, batches-1, "failed workers must leave remaining records for the caller")

	mu.Lock()
	defer mu.Unlock()
	for stmt := range statements {
		assert.NotContains(t, stmt, "COPY INTO", "no COPY may run after the write failed")
	}
	assert.Len(t, statements, 1, "no further statements after the failing PUT: %v", statements)
	assert.Zero(t, bufferedBytes.Load(), "a failed write must give back its buffer budget")
}

func TestWriteParallelReturnsOnFailureWhileSourceIsOpen(t *testing.T) {
	for _, parallelism := range []int{1, 4} {
		for _, tc := range []struct {
			name       string
			query      string
			fileSizeMB string
			batchBytes int
		}{
			{name: "PUT", query: "PUT", fileSizeMB: "0"},
			{name: "adaptive PUT", query: "PUT", fileSizeMB: "32", batchBytes: 2 * minAdaptiveFlushBytes},
			{name: "COPY", query: "COPY INTO", fileSizeMB: "0"},
			{name: "source", fileSizeMB: "0"},
		} {
			t.Run(fmt.Sprintf("%s/workers=%d", tc.name, parallelism), func(t *testing.T) {
				t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", tc.fileSizeMB)
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)

				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer func() { _ = db.Close() }()

				wantErr := errors.New("write failed")
				if tc.query == "COPY INTO" {
					mock.ExpectExec("PUT").WillReturnResult(sqlmock.NewResult(0, 0))
				}
				if tc.query != "" {
					mock.ExpectExec(tc.query).WillReturnError(wantErr)
				}

				var batch arrow.RecordBatch
				if tc.batchBytes > 0 {
					batch = newIncompressibleRecordBatch(mem, tc.batchBytes)
				} else {
					batch = newSingleRowRecordBatch(mem)
				}
				result := source.RecordBatchResult{Batch: batch}
				if tc.query == "" {
					result.Err = wantErr
				}
				records := make(chan source.RecordBatchResult, 1)
				records <- result

				done := make(chan struct{})
				var writeErr error
				go func() {
					dest := &SnowflakeDestination{db: db}
					writeErr = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
						Table:        "public.events",
						Parallelism:  parallelism,
						StagingTable: true,
					})
					close(done)
				}()
				defer func() {
					close(records)
					<-done
					for result := range records {
						if result.Batch != nil {
							result.Batch.Release()
						}
					}
				}()

				select {
				case <-done:
					require.ErrorIs(t, writeErr, wantErr)
				case <-time.After(5 * time.Second):
					t.Fatal("WriteParallel waited for source EOF after a failure")
				}
				assert.Zero(t, bufferedBytes.Load(), "failed workers must release their buffer budget")
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestWriteParallelReturnsOnCancellationWhileSourceIsOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	records := make(chan source.RecordBatchResult)
	done := make(chan struct{})
	var writeErr error
	go func() {
		dest := &SnowflakeDestination{}
		writeErr = dest.WriteParallel(ctx, records, destination.WriteOptions{
			Table:       "public.events",
			Parallelism: 4,
		})
		close(done)
	}()
	defer func() {
		close(records)
		<-done
	}()

	select {
	case records <- source.RecordBatchResult{}:
	case <-time.After(5 * time.Second):
		t.Fatal("WriteParallel did not start consuming records")
	}
	cancel()

	select {
	case <-done:
		require.ErrorIs(t, writeErr, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("WriteParallel waited for source EOF after cancellation")
	}
}

func TestWriteParallelCancelsOverlappedCopyOnUploadFailure(t *testing.T) {
	t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", "0")
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.MatchExpectationsInOrder(false)

	putErr := errors.New("stage is full")
	mock.ExpectExec("PUT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("PUT").WillDelayFor(100 * time.Millisecond).WillReturnError(putErr)
	mock.ExpectExec("COPY INTO").WillDelayFor(5 * time.Second).WillReturnResult(sqlmock.NewResult(0, 0))

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	close(records)

	dest := &SnowflakeDestination{db: db}
	start := time.Now()
	err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:        "public.events",
		Parallelism:  2,
		StagingTable: true,
	})
	require.ErrorIs(t, err, putErr)
	assert.Less(t, time.Since(start), time.Second, "upload failure should cancel the in-flight COPY")
	require.NoError(t, mock.ExpectationsWereMet())
}

// The schema-change roll-over flushes mid-stream, so a PUT can fail while the
// triggering record is still held. It has to be released like every other
// batch, and the write has to abort.
func TestWriteParallelAbortsWhenSchemaChangeFlushFails(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	putErr := errors.New("stage is full")
	mock.ExpectExec("PUT").WillReturnError(putErr)

	// Left at the default file size target so the only flush is the one the
	// schema change forces.
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatchWithColumns(mem, "id")}
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatchWithColumns(mem, "id", "extra")}
	close(records)

	dest := &SnowflakeDestination{db: db}
	err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:        "public.events",
		Parallelism:  1,
		StagingTable: true,
	})
	require.ErrorIs(t, err, putErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

// The shared budget is only meaningful if every uploader keeps bufferedBytes
// in step with its own buffer, including when the buffer goes away.
func TestUploaderAccountsBufferedBytes(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	require.Zero(t, bufferedBytes.Load(), "another test leaked buffered bytes")
	t.Cleanup(func() { bufferedBytes.Store(0) })

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectExec("PUT").WillReturnResult(sqlmock.NewResult(0, 0))

	var rows atomic.Int64
	writerProps, arrowProps := snowflakeParquetWriterProperties()
	newUploader := func() *snowflakeFileUploader {
		return &snowflakeFileUploader{
			dest:        &SnowflakeDestination{db: db},
			ctx:         t.Context(),
			stageName:   `"PUBLIC".%"EVENTS"`,
			loadID:      "123456789",
			targetBytes: 32 << 20,
			writerProps: writerProps,
			arrowProps:  arrowProps,
			rowsLoaded:  &rows,
		}
	}

	appendOne := func(w *snowflakeFileUploader) {
		t.Helper()
		record := newSingleRowRecordBatch(mem)
		require.NoError(t, w.append(record))
		record.Release()
	}

	uploaded := newUploader()
	appendOne(uploaded)
	require.Positive(t, bufferedBytes.Load())
	assert.Equal(t, uploaded.pendingBytes(), bufferedBytes.Load())

	// A second uploader's bytes add to the same total, the way concurrent
	// per-table writes do.
	discarded := newUploader()
	appendOne(discarded)
	assert.Equal(t, uploaded.pendingBytes()+discarded.pendingBytes(), bufferedBytes.Load())

	_, err = uploaded.flush()
	require.NoError(t, err)
	assert.Equal(t, discarded.pendingBytes(), bufferedBytes.Load(), "a flushed file releases its share")

	discarded.discard()
	assert.Zero(t, bufferedBytes.Load(), "a discarded file releases its share")
	require.NoError(t, mock.ExpectationsWereMet())
}

// discard drops the open file instead of uploading it, so a worker that gives
// up after a failure cannot leave a partial file on the stage.
func TestUploaderDiscardDropsOpenFileWithoutUploading(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var rows atomic.Int64
	writerProps, arrowProps := snowflakeParquetWriterProperties()
	w := &snowflakeFileUploader{
		dest:        &SnowflakeDestination{db: db},
		ctx:         t.Context(),
		stageName:   `"PUBLIC".%"EVENTS"`,
		loadID:      "123456789",
		targetBytes: 32 << 20,
		writerProps: writerProps,
		arrowProps:  arrowProps,
		rowsLoaded:  &rows,
	}

	record := newSingleRowRecordBatch(mem)
	require.NoError(t, w.append(record))
	record.Release()
	w.pendingRows = 1

	w.discard()
	assert.Zero(t, w.pendingBytes())
	assert.Zero(t, w.pendingRows)

	// No PUT expectation is registered, so any upload here fails the mock.
	fileName, err := w.flush()
	require.NoError(t, err)
	assert.Empty(t, fileName)
	assert.Zero(t, rows.Load(), "discarded rows must not be counted as loaded")
	require.NoError(t, mock.ExpectationsWereMet())
}

// The final flush at end-of-stream is the one an uploader does not follow with
// anything else, so a failure there is where buffered bytes are most likely to
// stay counted against the shared budget forever.
func TestWriteParallelReleasesBudgetWhenFinalFlushFails(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	require.Zero(t, bufferedBytes.Load(), "another test leaked buffered bytes")
	t.Cleanup(func() { bufferedBytes.Store(0) })

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	putErr := errors.New("stage is full")
	mock.ExpectExec("PUT").WillReturnError(putErr)

	// Left at the default size target, so the batch is still buffered when the
	// records channel closes and the only PUT is the end-of-stream flush.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	close(records)

	dest := &SnowflakeDestination{db: db}
	err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:        "public.events",
		Parallelism:  1,
		StagingTable: true,
	})
	require.ErrorIs(t, err, putErr)
	assert.Zero(t, bufferedBytes.Load())
	require.NoError(t, mock.ExpectationsWereMet())
}

// A source error must surface from WriteParallel rather than being swallowed
// into a silently short write, and a batch attached to that error result has
// to be released like any other -- see
// pkg/destination/error_batch_release_test.go for the same contract across the
// other destinations.
func TestWriteParallelReturnsSourceError(t *testing.T) {
	t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", "0")
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.MatchExpectationsInOrder(false)
	mock.ExpectExec("PUT").WillReturnResult(sqlmock.NewResult(0, 0))

	sourceErr := errors.New("source read failed")
	records := make(chan source.RecordBatchResult, 3)
	defer func() {
		for result := range records {
			result.Batch.Release()
		}
	}()
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem), Err: sourceErr}
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	close(records)

	dest := &SnowflakeDestination{db: db}
	err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:        "public.events",
		Parallelism:  1,
		StagingTable: true,
	})
	require.ErrorIs(t, err, sourceErr)
	assert.Len(t, records, 2, "failed workers must leave remaining records for the caller")
}

// With StagingTable set, COPY runs while uploads are still going, naming the
// files that finished since the previous statement. How many files each COPY
// picks up is a race, so this asserts only the invariant: every uploaded file
// is named by exactly one COPY.
func TestWriteParallelStagingLoadsEveryFileExactlyOnce(t *testing.T) {
	t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", "0") // one file per batch
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	// sqlmock invokes the matcher once per candidate expectation, so the same
	// statement can be offered several times; collect them as a set.
	var mu sync.Mutex
	puts := map[string]bool{}
	copies := map[string]bool{}
	capture := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(actualSQL, "PUT "):
			puts[actualSQL] = true
		case strings.HasPrefix(actualSQL, "COPY INTO"):
			copies[actualSQL] = true
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(capture))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.MatchExpectationsInOrder(false)

	// One PUT per batch and at most one COPY per PUT, plus headroom so an
	// unexpected extra statement fails on its own assertion rather than on a
	// confusing "all expectations were already fulfilled".
	const batches = 60
	for i := 0; i < batches+8; i++ {
		mock.ExpectExec("PUT").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("COPY INTO").WillReturnResult(sqlmock.NewResult(0, 0))
	}

	records := make(chan source.RecordBatchResult, batches)
	for i := 0; i < batches; i++ {
		records <- source.RecordBatchResult{Batch: newSingleRowRecordBatch(mem)}
	}
	close(records)

	dest := &SnowflakeDestination{db: db}
	require.NoError(t, dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:        "public.events",
		Parallelism:  4,
		StagingTable: true,
	}))

	fileRE := regexp.MustCompile(`batch_\d+_\d+\.parquet`)
	uploaded := map[string]bool{}
	for put := range puts {
		name := fileRE.FindString(put)
		require.NotEmpty(t, name)
		uploaded[name] = true
	}
	require.Len(t, uploaded, batches)

	loaded := map[string]int{}
	for c := range copies {
		require.Contains(t, c, "FILES = (", "overlapped COPY must name its files")
		for _, name := range fileRE.FindAllString(c, -1) {
			loaded[name]++
		}
	}
	for name := range uploaded {
		assert.Equal(t, 1, loaded[name], "file %s loaded %d times", name, loaded[name])
	}
	assert.Len(t, loaded, batches, "COPY named a file that was never uploaded")
}

// TestMultiTableWriteDoesNotDeadlockUnderConnectionPressure reproduces the
// exact multi-table merge deadlock reported in production: several tables
// are written concurrently (via multitable.Write, using the real Router)
// through the same *SnowflakeDestination/*sql.DB, with combined worker
// parallelism (numTables * Parallelism) exceeding the connection pool size,
// and records interleaved across tables the way a real multi-table CDC
// source would emit them.
//
// With the old behavior (one connection pinned per worker for its entire
// lifetime, acquired before the worker ever looks at its channel), this
// deadlocks: a minority of workers monopolize the pool and then idle waiting
// for more data, while the Router's single goroutine blocks forever trying
// to deliver to a starved table whose workers can never acquire a
// connection. Acquiring a connection only for the duration of each PUT (this
// fix) lets the shared pool be time-shared safely regardless of numTables *
// Parallelism, so the write completes instead of hanging.
func TestMultiTableWriteDoesNotDeadlockUnderConnectionPressure(t *testing.T) {
	t.Setenv("INGESTR_SNOWFLAKE_FILE_SIZE_MB", "0") // one PUT per batch so expectations are deterministic
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.MatchExpectationsInOrder(false)

	const maxOpenConns = 2
	db.SetMaxOpenConns(maxOpenConns)

	const numTables = 3
	const parallelismPerTable = 3 // numTables * parallelismPerTable (9) > maxOpenConns (2)
	const batchesPerTable = 12    // > router's per-table channel buffer (8), forces backpressure

	tableConfigs := make(map[string]destination.TableWriteConfig, numTables)
	tableNames := make([]string, 0, numTables)
	for i := 0; i < numTables; i++ {
		name := fmt.Sprintf("table_%d", i)
		tableNames = append(tableNames, name)
		tableConfigs[name] = destination.TableWriteConfig{DestTable: fmt.Sprintf("public.%s", name)}

		for j := 0; j < batchesPerTable; j++ {
			mock.ExpectExec("PUT file://batch_").WillReturnResult(sqlmock.NewResult(0, 0))
		}
		mock.ExpectExec("COPY INTO").WillReturnResult(sqlmock.NewResult(0, 0))
	}

	dest := &SnowflakeDestination{db: db}

	// Feed batches interleaved round-robin across tables through a single,
	// unbuffered shared channel, mimicking how a real multi-table source
	// streams records for many tables through one channel.
	records := make(chan source.RecordBatchResult)
	go func() {
		defer close(records)
		for j := 0; j < batchesPerTable; j++ {
			for _, name := range tableNames {
				records <- source.RecordBatchResult{TableName: name, Batch: newSingleRowRecordBatch(memory.DefaultAllocator)}
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- multitable.Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
			TableConfigs: tableConfigs,
			Parallelism:  parallelismPerTable,
		})
	}()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("multitable.Write deadlocked: workers likely held pooled connections beyond a single batch's upload")
	}

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchAlterColumnTypesSQL(t *testing.T) {
	d := &Dialect{}
	sql := d.BatchAlterColumnTypesSQL(`"DB"."PUBLIC"."USERS"`, []schema.Column{
		{Name: "age", DataType: schema.TypeString},
		{Name: "score", DataType: schema.TypeFloat64},
	})
	assert.Equal(
		t,
		`ALTER TABLE "DB"."PUBLIC"."USERS" ALTER COLUMN "AGE" SET DATA TYPE VARCHAR, COLUMN "SCORE" SET DATA TYPE DOUBLE`,
		sql,
	)
	assert.Empty(t, d.BatchAlterColumnTypesSQL(`"T"`, nil))
}

func TestBuildMergeSQL_CastsMismatchedSourceColumns(t *testing.T) {
	// staging DATE is TIMESTAMP_TZ, target DATE was widened to TIMESTAMP_NTZ;
	// Snowflake's MERGE won't implicitly cast, so source must be cast to target type.
	castMap := map[string]string{"DATE": "TIMESTAMP_NTZ"}
	sql := buildMergeSQL("staging_schema.staging_tbl", "target_schema.target_tbl", []string{"id"}, []string{"id", "date"}, "", castMap)

	assert.Contains(t, sql, `target."DATE" = CAST(source."DATE" AS TIMESTAMP_NTZ)`)
	assert.Contains(t, sql, `VALUES (source."ID", CAST(source."DATE" AS TIMESTAMP_NTZ))`)
	// Columns not in the cast map stay uncast.
	assert.NotContains(t, sql, `CAST(source."ID"`)
}

func TestParseSnowflakeAlterColumnTypesSQL_Single(t *testing.T) {
	table, changes, ok := parseSnowflakeAlterColumnTypesSQL(`ALTER TABLE "DB"."PUBLIC"."USERS" ALTER COLUMN "AGE" SET DATA TYPE VARCHAR`)
	require.True(t, ok)
	assert.Equal(t, `"DB"."PUBLIC"."USERS"`, table)
	require.Len(t, changes, 1)
	assert.Equal(t, "AGE", changes[0].column)
	assert.Equal(t, "VARCHAR", changes[0].newType)
}

func TestParseSnowflakeAlterColumnTypesSQL_MultiClause(t *testing.T) {
	table, changes, ok := parseSnowflakeAlterColumnTypesSQL(
		`ALTER TABLE "DB"."PUBLIC"."USERS" ALTER COLUMN "AGE" SET DATA TYPE VARCHAR, COLUMN "AMOUNT" SET DATA TYPE NUMBER(38,0)`,
	)
	require.True(t, ok)
	assert.Equal(t, `"DB"."PUBLIC"."USERS"`, table)
	require.Len(t, changes, 2)
	assert.Equal(t, "AGE", changes[0].column)
	assert.Equal(t, "VARCHAR", changes[0].newType)
	assert.Equal(t, "AMOUNT", changes[1].column)
	assert.Equal(t, "NUMBER(38,0)", changes[1].newType)
}

func TestParseSnowflakeAlterColumnTypesSQL_Invalid(t *testing.T) {
	for _, sql := range []string{
		`ALTER TABLE "DB"."PUBLIC"."USERS" ADD COLUMN "AGE" VARCHAR`,
		`CREATE OR REPLACE TABLE "DB"."PUBLIC"."USERS" AS SELECT 1`,
		`SELECT 1`,
	} {
		if _, _, ok := parseSnowflakeAlterColumnTypesSQL(sql); ok {
			t.Errorf("expected parse to fail for %q", sql)
		}
	}
}

func TestIsSnowflakeAlterTypeRewriteCandidate(t *testing.T) {
	alter := `ALTER TABLE "DB"."PUBLIC"."USERS" ALTER COLUMN "AGE" SET DATA TYPE VARCHAR`
	incompatible := errors.New("002108 (22000): SQL compilation error: cannot change column AGE from type NUMBER(38,0) to VARCHAR(16777216)")

	assert.True(t, isSnowflakeAlterTypeRewriteCandidate(alter, incompatible))
	assert.False(t, isSnowflakeAlterTypeRewriteCandidate(alter, nil))
	assert.False(t, isSnowflakeAlterTypeRewriteCandidate(alter, errors.New("some other error")))
	assert.False(t, isSnowflakeAlterTypeRewriteCandidate(`SELECT 1`, incompatible))
}

func TestBuildSnowflakeAlterColumnTypeRewriteSQL(t *testing.T) {
	sql, err := buildSnowflakeAlterColumnTypeRewriteSQL(
		`"DB"."PUBLIC"."USERS"`,
		[]string{"ID", "AGE", "NAME"},
		map[string]string{"AGE": "VARCHAR"},
		"",
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		`CREATE OR REPLACE TABLE "DB"."PUBLIC"."USERS" AS SELECT "ID", CAST("AGE" AS VARCHAR) AS "AGE", "NAME" FROM "DB"."PUBLIC"."USERS"`,
		sql,
	)
}

func TestBuildSnowflakeAlterColumnTypeRewriteSQL_MultiColumn(t *testing.T) {
	sql, err := buildSnowflakeAlterColumnTypeRewriteSQL(
		`"DB"."PUBLIC"."USERS"`,
		[]string{"ID", "AGE", "SCORE"},
		map[string]string{"AGE": "VARCHAR", "SCORE": "DOUBLE"},
		"",
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		`CREATE OR REPLACE TABLE "DB"."PUBLIC"."USERS" AS SELECT "ID", CAST("AGE" AS VARCHAR) AS "AGE", CAST("SCORE" AS DOUBLE) AS "SCORE" FROM "DB"."PUBLIC"."USERS"`,
		sql,
	)
}

func TestBuildSnowflakeAlterColumnTypeRewriteSQL_PreservesClustering(t *testing.T) {
	sql, err := buildSnowflakeAlterColumnTypeRewriteSQL(
		`"DB"."PUBLIC"."USERS"`,
		[]string{"ID", "AGE"},
		map[string]string{"AGE": "VARCHAR"},
		"CLUSTER BY (ID)",
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		`CREATE OR REPLACE TABLE "DB"."PUBLIC"."USERS" CLUSTER BY (ID) AS SELECT "ID", CAST("AGE" AS VARCHAR) AS "AGE" FROM "DB"."PUBLIC"."USERS"`,
		sql,
	)
}

func TestBuildSnowflakeAlterColumnTypeRewriteSQL_ColumnMissing(t *testing.T) {
	_, err := buildSnowflakeAlterColumnTypeRewriteSQL(
		`"DB"."PUBLIC"."USERS"`,
		[]string{"ID", "NAME"},
		map[string]string{"AGE": "VARCHAR"},
		"",
	)
	require.Error(t, err)
}

func TestClusterByClauseFor(t *testing.T) {
	assert.Equal(t, "", clusterByClauseFor(""))
	assert.Equal(t, "", clusterByClauseFor("   "))
	assert.Equal(t, "CLUSTER BY (C1, C2)", clusterByClauseFor("LINEAR(C1, C2)"))
	assert.Equal(t, "CLUSTER BY (TO_DATE(TS))", clusterByClauseFor("LINEAR(TO_DATE(TS))"))
	// Already-bare expression (no LINEAR wrapper) is wrapped as-is.
	assert.Equal(t, "CLUSTER BY (C1)", clusterByClauseFor("C1"))
}
