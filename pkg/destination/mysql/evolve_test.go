package mysql

import (
	"cmp"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestSchemaEvolutionWidensUnboundedPrimaryKeyToIndexableLength(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).
		WithArgs("app", "attachments").
		WillReturnRows(sqlmock.NewRows([]string{
			"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "COLUMN_TYPE",
		}).AddRow("narrow_key", "varchar", "NO", nil, nil, 100, "varchar(100)").
			AddRow("wide_key", "varchar", "NO", nil, nil, 1000, "varchar(1000)").
			AddRow("note", "varchar", "YES", nil, nil, 100, "varchar(100)"))
	mock.ExpectQuery(`CONSTRAINT_NAME = 'PRIMARY'`).
		WithArgs("app", "attachments").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("narrow_key").AddRow("wide_key"))
	existing, err := dest.GetTableSchema(t.Context(), "app.attachments")
	require.NoError(t, err)
	require.Equal(t, []string{"narrow_key", "wide_key"}, existing.PrimaryKeys)
	require.True(t, existing.Columns[0].IsPrimaryKey)
	require.False(t, existing.Columns[2].IsPrimaryKey)

	incoming := &schema.TableSchema{Columns: []schema.Column{
		{Name: "narrow_key", DataType: schema.TypeString, Nullable: true},
		{Name: "wide_key", DataType: schema.TypeString, Nullable: true},
		{Name: "note", DataType: schema.TypeString, Nullable: true},
	}}
	comparison, err := schemaevolution.Compare(incoming, existing, &schemaevolution.CompareOptions{
		NormalizeSourceColumn: dest.NormalizeSchemaEvolutionSourceColumn,
	})
	require.NoError(t, err)
	statements, _, err := destination.RenderEvolution(&Dialect{}, "app.attachments", comparison)
	require.NoError(t, err)
	require.Equal(t, []string{
		"ALTER TABLE app.attachments MODIFY COLUMN `narrow_key` VARCHAR(768) NOT NULL",
		"ALTER TABLE app.attachments MODIFY COLUMN `note` TEXT NULL",
	}, statements)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizeSchemaEvolutionSourceColumn(t *testing.T) {
	str := func(length int, key bool) schema.Column {
		return schema.Column{Name: "k", DataType: schema.TypeString, MaxLength: length, IsPrimaryKey: key}
	}
	for _, tc := range []struct {
		name         string
		source, dest schema.Column
		want         int
	}{
		{"unbounded source widens a narrow key to the indexable maximum", str(0, false), str(100, true), maxPrimaryKeyStringLength},
		{"unbounded source keeps a key that is already wider", str(0, false), str(1000, true), 1000},
		{"bounded source keeps its length", str(200, false), str(100, true), 200},
		{"bounded source over the limit keeps its length", str(1000, false), str(100, true), 1000},
		{"non-key columns are left unbounded", str(0, false), str(100, false), 0},
		{"unbounded destination keys are left alone", str(0, false), str(0, true), 0},
		{"numeric destination keys are left alone", str(0, false), schema.Column{Name: "k", DataType: schema.TypeInt64, IsPrimaryKey: true}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (&MySQLDestination{}).NormalizeSchemaEvolutionSourceColumn(tc.source, tc.dest)
			require.Equal(t, tc.want, got.MaxLength)
		})
	}

	numeric := schema.Column{Name: "k", DataType: schema.TypeInt64}
	require.Equal(t, numeric, (&MySQLDestination{}).NormalizeSchemaEvolutionSourceColumn(numeric, str(100, true)))
}

func TestBoundKeyColumns(t *testing.T) {
	str := func(name string, length int) schema.Column {
		return schema.Column{Name: name, DataType: schema.TypeString, MaxLength: length}
	}
	bigint := schema.Column{Name: "version", DataType: schema.TypeInt64}
	for _, tc := range []struct {
		name    string
		columns []schema.Column
		keys    []string
		want    []int
	}{
		{"unbounded single key gets the whole budget", []schema.Column{str("k", 0)}, []string{"k"}, []int{768}},
		{"explicit single key is kept", []schema.Column{str("k", 200)}, []string{"k"}, []int{200}},
		{"explicit single key over the limit is kept", []schema.Column{str("k", 1000)}, []string{"k"}, []int{1000}},
		{"unbounded composite keys split the budget", []schema.Column{str("a", 0), str("b", 0)}, []string{"a", "b"}, []int{384, 384}},
		{"unbounded keys share what explicit keys leave", []schema.Column{str("a", 0), str("b", 600)}, []string{"a", "b"}, []int{168, 600}},
		{"bigint keys reserve 8 bytes", []schema.Column{str("a", 0), bigint}, []string{"a", "version"}, []int{766, 0}},
		{"uuid keys reserve 144 bytes", []schema.Column{{Name: "id", DataType: schema.TypeUUID}, str("slug", 0)}, []string{"id", "slug"}, []int{0, 732}},
		{"decimal keys reserve their storage size", []schema.Column{{Name: "id", DataType: schema.TypeDecimal, Precision: 38}, str("slug", 0)}, []string{"id", "slug"}, []int{0, 763}},
		{"interval keys reserve their VARCHAR(255)", []schema.Column{{Name: "span", DataType: schema.TypeInterval}, str("slug", 0)}, []string{"span", "slug"}, []int{0, 513}},
		{"several non-string keys add up", []schema.Column{{Name: "at", DataType: schema.TypeTimestamp}, bigint, str("slug", 0)}, []string{"at", "version", "slug"}, []int{0, 0, 764}},
		{"key names match case-insensitively", []schema.Column{str("tenant", 0), str("external_id", 0)}, []string{"TENANT", "External_Id"}, []int{384, 384}},
		{"non-key columns stay unbounded", []schema.Column{str("k", 0), str("note", 0)}, []string{"k"}, []int{768, 0}},
		{"tables without string keys are unchanged", []schema.Column{bigint, str("note", 0)}, []string{"version"}, []int{0, 0}},
		{
			"mixed composite key",
			[]schema.Column{str("tenant", 0), str("external_id", 0), str("region", 100), bigint, str("note", 0)},
			[]string{"tenant", "EXTERNAL_ID", "region", "version"},
			[]int{333, 333, 100, 0, 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]schema.Column(nil), tc.columns...)
			got := boundKeyColumns(tc.columns, tc.keys, utf8mb4BytesPerChar)
			lengths := make([]int, len(got))
			for i, col := range got {
				lengths[i] = col.MaxLength
			}
			require.Equal(t, tc.want, lengths)
			require.Equal(t, original, tc.columns)
		})
	}
}

func TestBoundKeyColumnsUsesTheTableCharset(t *testing.T) {
	str := func(name string, length int) schema.Column {
		return schema.Column{Name: name, DataType: schema.TypeString, MaxLength: length}
	}
	for _, tc := range []struct {
		name      string
		columns   []schema.Column
		keys      []string
		charBytes int
		want      []int
	}{
		{"latin1 single key", []schema.Column{str("k", 0)}, []string{"k"}, 1, []int{3072}},
		{"utf8mb3 single key", []schema.Column{str("k", 0)}, []string{"k"}, 3, []int{1024}},
		{"latin1 composite key with a bigint", []schema.Column{str("a", 0), str("b", 0), {Name: "v", DataType: schema.TypeInt64}}, []string{"a", "b", "v"}, 1, []int{1532, 1532, 0}},
		{"latin1 key with a uuid", []schema.Column{{Name: "id", DataType: schema.TypeUUID}, str("slug", 0)}, []string{"id", "slug"}, 1, []int{0, 3036}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundKeyColumns(tc.columns, tc.keys, tc.charBytes)
			lengths := make([]int, len(got))
			for i, col := range got {
				lengths[i] = col.MaxLength
			}
			require.Equal(t, tc.want, lengths)
		})
	}
}

func TestCreateCharBytes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db, database: "app"}
	unboundedKey := []schema.Column{{Name: "k", DataType: schema.TypeString}}

	require.Equal(t, utf8mb4BytesPerChar, dest.createCharBytes(t.Context(), "app.items", []schema.Column{{Name: "k", DataType: schema.TypeString, MaxLength: 100}}, []string{"k"}), "bounded keys need no lookup")
	require.Equal(t, utf8mb4BytesPerChar, dest.createCharBytes(t.Context(), "app.items", unboundedKey, nil), "tables without keys need no lookup")

	mock.ExpectQuery(`information_schema\.SCHEMATA`).WithArgs("analytics").WillReturnRows(sqlmock.NewRows([]string{"MAXLEN"}).AddRow(1))
	require.Equal(t, 1, dest.createCharBytes(t.Context(), "analytics.items", unboundedKey, []string{"k"}))

	mock.ExpectQuery(`information_schema\.SCHEMATA`).WithArgs("app").WillReturnRows(sqlmock.NewRows([]string{"MAXLEN"}).AddRow(3))
	require.Equal(t, 3, dest.createCharBytes(t.Context(), "items", unboundedKey, []string{"k"}), "unqualified tables use the connection database")

	mock.ExpectQuery(`information_schema\.SCHEMATA`).WillReturnError(errors.New("access denied"))
	require.Equal(t, utf8mb4BytesPerChar, dest.createCharBytes(t.Context(), "app.items", unboundedKey, []string{"k"}), "lookup errors assume utf8mb4")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestKeyBytesMatchesCreatedType(t *testing.T) {
	decimal := func(precision, scale int) schema.Column {
		return schema.Column{DataType: schema.TypeDecimal, Precision: precision, Scale: scale}
	}
	for _, tc := range []struct {
		col      schema.Column
		mysql    string
		keyBytes int
	}{
		{schema.Column{DataType: schema.TypeBoolean}, "BOOLEAN", 1},
		{schema.Column{DataType: schema.TypeInt8}, "TINYINT", 1},
		{schema.Column{DataType: schema.TypeInt16}, "SMALLINT", 2},
		{schema.Column{DataType: schema.TypeInt32}, "INT", 4},
		{schema.Column{DataType: schema.TypeInt64}, "BIGINT", 8},
		{schema.Column{DataType: schema.TypeFloat32}, "FLOAT", 4},
		{schema.Column{DataType: schema.TypeFloat64}, "DOUBLE", 8},
		{schema.Column{DataType: schema.TypeDate}, "DATE", 3},
		{schema.Column{DataType: schema.TypeTime}, "TIME(6)", 6},
		{schema.Column{DataType: schema.TypeTimestamp}, "DATETIME(6)", 8},
		{schema.Column{DataType: schema.TypeTimestampTZ}, "TIMESTAMP(6)", 7},
		{schema.Column{DataType: schema.TypeUUID}, "CHAR(36)", 144},
		{schema.Column{DataType: schema.TypeInterval}, "VARCHAR(255)", 1020},
		{decimal(0, 0), "DECIMAL(38,9)", 17},
		{decimal(38, 0), "DECIMAL(38,0)", 17},
		{decimal(18, 0), "DECIMAL(18,0)", 8},
		{decimal(10, 2), "DECIMAL(10,2)", 5},
		{decimal(65, 30), "DECIMAL(65,30)", 30},
		{decimal(5, -1), "DECIMAL(5,0)", 3},
	} {
		t.Run(tc.mysql, func(t *testing.T) {
			require.Equal(t, tc.mysql, MapDataTypeToMySQL(tc.col))
			require.Equal(t, tc.keyBytes, keyBytes(tc.col, utf8mb4BytesPerChar))
		})
	}

	require.Equal(t, 16, keyBytes(schema.Column{DataType: schema.TypeBinary, MaxLength: 16}, utf8mb4BytesPerChar), "VARBINARY(16) read from an existing table")
	require.Equal(t, 3, keyBytes(decimal(5, 10), utf8mb4BytesPerChar), "scale is clamped to the precision")
	require.Equal(t, 36, keyBytes(schema.Column{DataType: schema.TypeUUID}, 1), "CHAR(36) in a latin1 table")
	require.Equal(t, 765, keyBytes(schema.Column{DataType: schema.TypeInterval}, 3), "VARCHAR(255) in a utf8mb3 table")
}

func TestStringKeyBudget(t *testing.T) {
	for _, tc := range []struct{ otherKeyBytes, charBytes, want int }{
		{0, 4, 768},
		{8, 4, 766},
		{17, 4, 763},
		{144, 4, 732},
		{0, 3, 1024},
		{0, 1, 3072},
		{8, 1, 3064},
		{1000, 4, 518},
	} {
		require.Equal(t, tc.want, stringKeyBudget(tc.otherKeyBytes, tc.charBytes), "%d other key bytes, %d bytes per char", tc.otherKeyBytes, tc.charBytes)
	}
}

func TestFairShares(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		requests, floors, want []int
		available              int
	}{
		{"requests that fit are kept", []int{200, 500}, []int{100, 100}, []int{200, 500}, 768},
		{"equal requests split the budget", []int{768, 768}, []int{100, 100}, []int{384, 384}, 768},
		{"unbounded request takes what is left", []int{200, 0}, []int{100, 100}, []int{200, 568}, 768},
		{"single unbounded request takes the budget", []int{0}, []int{100}, []int{768}, 768},
		{"single request over the budget is capped", []int{1000}, []int{100}, []int{768}, 768},
		{"columns never shrink below their floor", []int{500, 100, 500}, []int{400, 50, 50}, []int{500, 100, 168}, 768},
		{"floors over the budget are kept as they are", []int{768, 768}, []int{700, 100}, []int{700, 100}, 768},
		{"no budget left keeps the floors", []int{500}, []int{100}, []int{100}, 50},
		{"requests below their floor keep the floor", []int{50}, []int{100}, []int{100}, 768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, fairShares(tc.requests, tc.floors, tc.available))
		})
	}
}

type keyTestColumn struct {
	name      string
	dataType  string
	length    int
	key       bool
	charBytes int
}

func (c keyTestColumn) schema() schema.Column {
	col := schema.Column{Name: c.name, DataType: schema.TypeString, MaxLength: c.length, IsPrimaryKey: c.key, Nullable: !c.key}
	switch c.dataType {
	case "bigint":
		col = schema.Column{Name: c.name, DataType: schema.TypeInt64, IsPrimaryKey: c.key, Nullable: !c.key}
	case "decimal":
		col = schema.Column{Name: c.name, DataType: schema.TypeDecimal, Precision: c.length, IsPrimaryKey: c.key, Nullable: !c.key}
	}
	return col
}

func expectMySQLTable(mock sqlmock.Sqlmock, columns []keyTestColumn, tableCharBytes int) {
	rows := sqlmock.NewRows([]string{
		"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "COLUMN_TYPE",
	})
	keys := sqlmock.NewRows([]string{"COLUMN_NAME"})
	for _, col := range columns {
		nullable := "YES"
		if col.key {
			nullable = "NO"
			keys.AddRow(col.name)
		}
		switch col.dataType {
		case "bigint":
			rows.AddRow(col.name, "bigint", nullable, 19, 0, nil, "bigint")
			continue
		case "decimal":
			rows.AddRow(col.name, "decimal", nullable, col.length, 0, nil, fmt.Sprintf("decimal(%d,0)", col.length))
			continue
		}
		rows.AddRow(col.name, "varchar", nullable, nil, nil, col.length, fmt.Sprintf("varchar(%d)", col.length))
	}
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).WillReturnRows(rows)
	mock.ExpectQuery(`KEY_COLUMN_USAGE`).WillReturnRows(keys)

	hasKey := false
	charsets := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_MAXLEN", "TABLE_MAXLEN"})
	for _, col := range columns {
		hasKey = hasKey || col.key
		charBytes := 0
		if col.dataType == "varchar" {
			charBytes = cmp.Or(col.charBytes, utf8mb4BytesPerChar)
		}
		charsets.AddRow(col.name, charBytes, cmp.Or(tableCharBytes, utf8mb4BytesPerChar))
	}
	if hasKey {
		mock.ExpectQuery(`information_schema\.CHARACTER_SETS`).WillReturnRows(charsets)
	}
}

func TestSchemaEvolutionFitsStringKeysIntoTheKeyLimit(t *testing.T) {
	varchar := func(name string, length int, key bool) keyTestColumn {
		return keyTestColumn{name: name, dataType: "varchar", length: length, key: key}
	}
	bigint := func(name string, key bool) keyTestColumn {
		return keyTestColumn{name: name, dataType: "bigint", key: key}
	}
	source := func(name string, length int) schema.Column {
		return schema.Column{Name: name, DataType: schema.TypeString, MaxLength: length, Nullable: true}
	}
	modify := func(name, colType string) string {
		return "ALTER TABLE app.items MODIFY COLUMN `" + name + "` " + colType
	}

	for _, tc := range []struct {
		name           string
		dest           []keyTestColumn
		tableCharBytes int
		source         []schema.Column
		overrides      string
		want           []string
	}{
		{
			name:   "unbounded source widens a single key to the limit",
			dest:   []keyTestColumn{varchar("k", 100, true)},
			source: []schema.Column{source("k", 0)},
			want:   []string{modify("k", "VARCHAR(768) NOT NULL")},
		},
		{
			name:   "source length within the limit is kept",
			dest:   []keyTestColumn{varchar("k", 100, true)},
			source: []schema.Column{source("k", 500)},
			want:   []string{modify("k", "VARCHAR(500) NOT NULL")},
		},
		{
			name:   "source length over the limit is capped",
			dest:   []keyTestColumn{varchar("k", 100, true)},
			source: []schema.Column{source("k", 1000)},
			want:   []string{modify("k", "VARCHAR(768) NOT NULL")},
		},
		{
			name:      "explicit override over the limit is applied as given",
			dest:      []keyTestColumn{varchar("k", 100, true)},
			source:    []schema.Column{source("k", 0)},
			overrides: "k:varchar(1000)",
			want:      []string{modify("k", "VARCHAR(1000) NOT NULL")},
		},
		{
			name:      "unsized override on a string key widens to the limit",
			dest:      []keyTestColumn{varchar("k", 100, true)},
			source:    []schema.Column{source("k", 0)},
			overrides: "k:string",
			want:      []string{modify("k", "VARCHAR(768) NOT NULL")},
		},
		{
			name:      "unsized override on a numeric key becomes a bounded string",
			dest:      []keyTestColumn{bigint("id", true)},
			source:    []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}},
			overrides: "id:string",
			want:      []string{modify("id", "VARCHAR(768) NOT NULL")},
		},
		{
			name:      "sized override on a numeric key keeps its length",
			dest:      []keyTestColumn{bigint("id", true)},
			source:    []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}},
			overrides: "id:varchar(50)",
			want:      []string{modify("id", "VARCHAR(50) NOT NULL")},
		},
		{
			name:   "unbounded composite keys split the budget",
			dest:   []keyTestColumn{varchar("tenant", 100, true), varchar("external_id", 100, true)},
			source: []schema.Column{source("tenant", 0), source("external_id", 0)},
			want:   []string{modify("tenant", "VARCHAR(384) NOT NULL"), modify("external_id", "VARCHAR(384) NOT NULL")},
		},
		{
			name:   "composite source lengths that fit are kept",
			dest:   []keyTestColumn{varchar("tenant", 100, true), varchar("external_id", 100, true)},
			source: []schema.Column{source("tenant", 200), source("external_id", 500)},
			want:   []string{modify("tenant", "VARCHAR(200) NOT NULL"), modify("external_id", "VARCHAR(500) NOT NULL")},
		},
		{
			name:      "explicit override on a composite key is not reduced",
			dest:      []keyTestColumn{varchar("k", 100, true), varchar("j", 100, true)},
			source:    []schema.Column{source("k", 0), source("j", 100)},
			overrides: "k:varchar(768)",
			want:      []string{modify("k", "VARCHAR(768) NOT NULL")},
		},
		{
			name:      "unsized override on a composite key takes what is left",
			dest:      []keyTestColumn{varchar("k", 100, true), varchar("j", 100, true)},
			source:    []schema.Column{source("k", 0), source("j", 100)},
			overrides: "k:string",
			want:      []string{modify("k", "VARCHAR(668) NOT NULL")},
		},
		{
			name:      "explicit override is budgeted before source widenings",
			dest:      []keyTestColumn{varchar("k", 100, true), varchar("j", 100, true)},
			source:    []schema.Column{source("k", 0), source("j", 0)},
			overrides: "k:varchar(600)",
			want:      []string{modify("k", "VARCHAR(600) NOT NULL"), modify("j", "VARCHAR(168) NOT NULL")},
		},
		{
			name:   "numeric key columns reserve part of the budget",
			dest:   []keyTestColumn{bigint("version", true), varchar("k", 100, true)},
			source: []schema.Column{{Name: "version", DataType: schema.TypeInt64, Nullable: true}, source("k", 0)},
			want:   []string{modify("k", "VARCHAR(766) NOT NULL")},
		},
		{
			name: "decimal key columns reserve their storage size",
			dest: []keyTestColumn{{name: "id", dataType: "decimal", length: 38, key: true}, varchar("slug", 100, true)},
			source: []schema.Column{
				{Name: "id", DataType: schema.TypeDecimal, Precision: 38, Nullable: true},
				source("slug", 0),
			},
			want: []string{modify("slug", "VARCHAR(763) NOT NULL")},
		},
		{
			name:   "key columns never shrink below their current width",
			dest:   []keyTestColumn{varchar("a", 400, true), varchar("b", 50, true), varchar("c", 50, true)},
			source: []schema.Column{source("a", 500), source("b", 100), source("c", 0)},
			want: []string{
				modify("a", "VARCHAR(500) NOT NULL"),
				modify("b", "VARCHAR(100) NOT NULL"),
				modify("c", "VARCHAR(168) NOT NULL"),
			},
		},
		{
			name:   "a full composite key is not widened",
			dest:   []keyTestColumn{varchar("k", 384, true), varchar("j", 384, true)},
			source: []schema.Column{source("k", 0), source("j", 0)},
		},
		{
			name:   "non-key columns are not capped",
			dest:   []keyTestColumn{varchar("k", 100, true), varchar("note", 100, false)},
			source: []schema.Column{source("k", 0), source("note", 0)},
			want:   []string{modify("k", "VARCHAR(768) NOT NULL"), modify("note", "TEXT NULL")},
		},
		{
			name:           "latin1 keys are not capped at the utf8mb4 width",
			dest:           []keyTestColumn{{name: "k", dataType: "varchar", length: 100, key: true, charBytes: 1}},
			tableCharBytes: 1,
			source:         []schema.Column{source("k", 1000)},
			want:           []string{modify("k", "VARCHAR(1000) NOT NULL")},
		},
		{
			name:           "latin1 keys are capped at the latin1 width",
			dest:           []keyTestColumn{{name: "k", dataType: "varchar", length: 100, key: true, charBytes: 1}},
			tableCharBytes: 1,
			source:         []schema.Column{source("k", 4000)},
			want:           []string{modify("k", "VARCHAR(3072) NOT NULL")},
		},
		{
			name: "latin1 composite keys share the latin1 budget",
			dest: []keyTestColumn{
				{name: "k", dataType: "varchar", length: 100, key: true, charBytes: 1},
				{name: "j", dataType: "varchar", length: 100, key: true, charBytes: 1},
			},
			tableCharBytes: 1,
			source:         []schema.Column{source("k", 2000), source("j", 2000)},
			want:           []string{modify("k", "VARCHAR(1536) NOT NULL"), modify("j", "VARCHAR(1536) NOT NULL")},
		},
		{
			name:           "utf8mb3 tables use 3 bytes per character",
			dest:           []keyTestColumn{{name: "k", dataType: "varchar", length: 100, key: true, charBytes: 3}},
			tableCharBytes: 3,
			source:         []schema.Column{source("k", 2000)},
			want:           []string{modify("k", "VARCHAR(1024) NOT NULL")},
		},
		{
			name: "unchanged key columns count with their own charset",
			dest: []keyTestColumn{
				{name: "k", dataType: "varchar", length: 100, key: true},
				{name: "j", dataType: "varchar", length: 1000, key: true, charBytes: 1},
			},
			source: []schema.Column{source("k", 0), source("j", 1000)},
			want:   []string{modify("k", "VARCHAR(518) NOT NULL")},
		},
		{
			name: "widened columns are budgeted with the table charset",
			dest: []keyTestColumn{
				{name: "k", dataType: "varchar", length: 100, key: true, charBytes: 1},
				{name: "j", dataType: "varchar", length: 100, key: true, charBytes: 1},
			},
			tableCharBytes: 4,
			source:         []schema.Column{source("k", 2000), source("j", 100)},
			want:           []string{modify("k", "VARCHAR(743) NOT NULL")},
		},
		{
			name:   "tables without a primary key are not capped",
			dest:   []keyTestColumn{varchar("k", 100, false)},
			source: []schema.Column{source("k", 1000)},
			want:   []string{modify("k", "VARCHAR(1000) NULL")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			dest := &MySQLDestination{db: db}
			expectMySQLTable(mock, tc.dest, tc.tableCharBytes)

			existing := &schema.TableSchema{}
			for _, col := range tc.dest {
				existing.Columns = append(existing.Columns, col.schema())
				if col.key {
					existing.PrimaryKeys = append(existing.PrimaryKeys, col.name)
				}
			}
			overrides, err := schemaevolution.ParseColumnOverrides(tc.overrides)
			require.NoError(t, err)
			comparison, err := schemaevolution.Compare(&schema.TableSchema{Columns: tc.source}, existing, &schemaevolution.CompareOptions{
				Overrides:             overrides,
				NormalizeSourceColumn: dest.NormalizeSchemaEvolutionSourceColumn,
			})
			require.NoError(t, err)

			fitted, err := dest.fitKeyWidenings(t.Context(), "app.items", comparison)
			require.NoError(t, err)
			statements, _, err := destination.RenderEvolution(&Dialect{}, "app.items", fitted)
			require.NoError(t, err)
			require.Equal(t, tc.want, statements)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFitKeyWideningsSkipsLookupWithoutStringWidenings(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db}

	added := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeAddColumn, ColumnName: "note", NewColumn: schema.Column{Name: "note", DataType: schema.TypeString},
	}}}
	got, err := dest.fitKeyWidenings(t.Context(), "app.items", added)
	require.NoError(t, err)
	require.Same(t, added, got)

	got, err = dest.fitKeyWidenings(t.Context(), "app.items", nil)
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFitKeyWideningsAssumesUTF8MB4WhenCharsetsCannotBeRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).WillReturnRows(sqlmock.NewRows([]string{
		"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "COLUMN_TYPE",
	}).AddRow("k", "varchar", "NO", nil, nil, 100, "varchar(100)"))
	mock.ExpectQuery(`KEY_COLUMN_USAGE`).WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("k"))
	mock.ExpectQuery(`information_schema\.CHARACTER_SETS`).WillReturnError(errors.New("access denied"))

	old := schema.Column{Name: "k", DataType: schema.TypeString, MaxLength: 100}
	got, err := dest.fitKeyWidenings(t.Context(), "app.items", &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "k", OldColumn: &old,
		NewColumn: schema.Column{Name: "k", DataType: schema.TypeString, MaxLength: 3000},
	}}})
	require.NoError(t, err)
	require.Equal(t, maxPrimaryKeyStringLength, got.Changes[0].NewColumn.MaxLength)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFitKeyWideningsReturnsLookupErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &MySQLDestination{db: db}
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).WillReturnError(errors.New("connection lost"))

	old := schema.Column{Name: "k", DataType: schema.TypeString, MaxLength: 100}
	_, err = dest.fitKeyWidenings(t.Context(), "app.items", &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "k", OldColumn: &old,
		NewColumn: schema.Column{Name: "k", DataType: schema.TypeString, MaxLength: 768},
	}}})
	require.ErrorContains(t, err, "connection lost")
}
