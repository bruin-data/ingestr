package mysql

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestUriToDSN(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantDSN      string
		wantDatabase string
		wantErr      bool
	}{
		{
			name:         "basic mysql uri",
			uri:          "mysql://user:pass@localhost:3306/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?allowNativePasswords=true&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mysql uri with default port",
			uri:          "mysql://user:pass@localhost/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?allowNativePasswords=true&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mysql uri without password",
			uri:          "mysql://user@localhost:3306/testdb",
			wantDSN:      "user@tcp(localhost:3306)/testdb?allowNativePasswords=true&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mariadb scheme",
			uri:          "mariadb://user:pass@localhost:3306/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?allowNativePasswords=true&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "mysql+pymysql scheme",
			uri:          "mysql+pymysql://user:pass@localhost:3306/testdb",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?allowNativePasswords=true&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:         "uri with query parameters",
			uri:          "mysql://user:pass@localhost:3306/testdb?charset=utf8mb4",
			wantDSN:      "user:pass@tcp(localhost:3306)/testdb?allowNativePasswords=true&charset=utf8mb4&parseTime=true",
			wantDatabase: "testdb",
			wantErr:      false,
		},
		{
			name:    "invalid scheme",
			uri:     "postgres://user:pass@localhost:5432/testdb",
			wantErr: true,
		},
		{
			name:    "invalid uri format",
			uri:     "not-a-valid-uri",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDSN, gotDatabase, err := uriToDSN(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantDSN, gotDSN)
			assert.Equal(t, tt.wantDatabase, gotDatabase)
		})
	}
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "simple table name",
			table: "users",
			want:  "`users`",
		},
		{
			name:  "schema qualified table",
			table: "mydb.users",
			want:  "`mydb`.`users`",
		},
		{
			name:  "table with special chars",
			table: "my-table",
			want:  "`my-table`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteTable(tt.table)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQuoteColumns(t *testing.T) {
	tests := []struct {
		name    string
		columns []string
		want    []string
	}{
		{
			name:    "single column",
			columns: []string{"id"},
			want:    []string{"`id`"},
		},
		{
			name:    "multiple columns",
			columns: []string{"id", "name", "email"},
			want:    []string{"`id`", "`name`", "`email`"},
		},
		{
			name:    "empty list",
			columns: []string{},
			want:    []string{},
		},
		{
			name:    "columns with special chars",
			columns: []string{"user-id", "user_name"},
			want:    []string{"`user-id`", "`user_name`"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteColumns(tt.columns)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterColumns(t *testing.T) {
	tests := []struct {
		name    string
		columns []string
		exclude []string
		want    []string
	}{
		{
			name:    "exclude one column",
			columns: []string{"id", "name", "email"},
			exclude: []string{"id"},
			want:    []string{"name", "email"},
		},
		{
			name:    "exclude multiple columns",
			columns: []string{"id", "name", "email", "age"},
			exclude: []string{"id", "age"},
			want:    []string{"name", "email"},
		},
		{
			name:    "exclude nothing",
			columns: []string{"id", "name", "email"},
			exclude: []string{},
			want:    []string{"id", "name", "email"},
		},
		{
			name:    "exclude non-existent column",
			columns: []string{"id", "name", "email"},
			exclude: []string{"age"},
			want:    []string{"id", "name", "email"},
		},
		{
			name:    "case insensitive exclusion",
			columns: []string{"ID", "Name", "Email"},
			exclude: []string{"id", "email"},
			want:    []string{"Name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterColumns(tt.columns, tt.exclude)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildJoinCondition(t *testing.T) {
	tests := []struct {
		name        string
		keys        []string
		targetAlias string
		sourceAlias string
		want        string
	}{
		{
			name:        "single primary key",
			keys:        []string{"id"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`id` = source.`id`",
		},
		{
			name:        "composite primary key",
			keys:        []string{"user_id", "post_id"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`user_id` = source.`user_id` AND target.`post_id` = source.`post_id`",
		},
		{
			name:        "different aliases",
			keys:        []string{"id"},
			targetAlias: "t",
			sourceAlias: "s",
			want:        "t.`id` = s.`id`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildJoinCondition(tt.keys, tt.targetAlias, tt.sourceAlias)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildUpdateSet(t *testing.T) {
	tests := []struct {
		name        string
		columns     []string
		targetAlias string
		sourceAlias string
		want        string
	}{
		{
			name:        "single column",
			columns:     []string{"name"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`name` = source.`name`",
		},
		{
			name:        "multiple columns",
			columns:     []string{"name", "email", "age"},
			targetAlias: "target",
			sourceAlias: "source",
			want:        "target.`name` = source.`name`, target.`email` = source.`email`, target.`age` = source.`age`",
		},
		{
			name:        "different aliases",
			columns:     []string{"name"},
			targetAlias: "t",
			sourceAlias: "s",
			want:        "t.`name` = s.`name`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildUpdateSet(tt.columns, tt.targetAlias, tt.sourceAlias)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractTableName(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "simple table name",
			table: "users",
			want:  "users",
		},
		{
			name:  "schema qualified table",
			table: "mydb.users",
			want:  "users",
		},
		{
			name:  "multiple dots",
			table: "catalog.schema.table",
			want:  "table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTableName(tt.table)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	tests := []struct {
		name        string
		table       string
		columns     []schema.Column
		primaryKeys []string
		want        string
	}{
		{
			name:  "simple table without primary key",
			table: "users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
			},
			primaryKeys: nil,
			want:        "CREATE TABLE IF NOT EXISTS `users` (\n  `id` BIGINT,\n  `name` TEXT\n)",
		},
		{
			name:  "table with primary key",
			table: "users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
			},
			primaryKeys: []string{"id"},
			want:        "CREATE TABLE IF NOT EXISTS `users` (\n  `id` BIGINT,\n  `name` TEXT,\n  PRIMARY KEY (`id`)\n)",
		},
		{
			name:  "table with composite primary key",
			table: "user_posts",
			columns: []schema.Column{
				{Name: "user_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "post_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "created_at", DataType: schema.TypeTimestamp, Nullable: true},
			},
			primaryKeys: []string{"user_id", "post_id"},
			want:        "CREATE TABLE IF NOT EXISTS `user_posts` (\n  `user_id` BIGINT,\n  `post_id` BIGINT,\n  `created_at` DATETIME(6),\n  PRIMARY KEY (`user_id`, `post_id`)\n)",
		},
		{
			name:  "schema qualified table",
			table: "mydb.users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			},
			primaryKeys: nil,
			want:        "CREATE TABLE IF NOT EXISTS `mydb`.`users` (\n  `id` BIGINT\n)",
		},
		{
			name:  "table with various types",
			table: "test_table",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
				{Name: "active", DataType: schema.TypeBoolean, Nullable: false},
				{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
				{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2, Nullable: true},
				{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
			},
			primaryKeys: []string{"id"},
			want:        "CREATE TABLE IF NOT EXISTS `test_table` (\n  `id` BIGINT,\n  `name` TEXT,\n  `active` BOOLEAN,\n  `score` DOUBLE,\n  `amount` DECIMAL(10,2),\n  `created_at` TIMESTAMP(6),\n  PRIMARY KEY (`id`)\n)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCreateTableSQL(tt.table, tt.columns, tt.primaryKeys)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMySQLDestination_Schemes(t *testing.T) {
	dest := NewMySQLDestination()
	schemes := dest.Schemes()
	expected := []string{"mysql", "mysql+pymysql", "mariadb"}
	assert.Equal(t, expected, schemes)
}

func TestMySQLDestination_StrategySupport(t *testing.T) {
	dest := NewMySQLDestination()

	assert.True(t, dest.SupportsReplaceStrategy())
	assert.True(t, dest.SupportsAppendStrategy())
	assert.True(t, dest.SupportsMergeStrategy())
	assert.True(t, dest.SupportsDeleteInsertStrategy())
	assert.True(t, dest.SupportsAtomicSwap())
}

func TestDeleteInsertLockName(t *testing.T) {
	first := deleteInsertLockName("analytics.orders")
	second := deleteInsertLockName("analytics.orders")
	other := deleteInsertLockName("analytics.customers")

	assert.Equal(t, first, second)
	assert.NotEqual(t, first, other)
	assert.LessOrEqual(t, len(first), 64)
	assert.Contains(t, first, "ingestr_di_")
}
