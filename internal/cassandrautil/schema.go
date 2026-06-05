package cassandrautil

import (
	"context"
	"fmt"
	"sort"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func GetTableSchema(ctx context.Context, session *gocql.Session, defaultKeyspace, table string) (*schema.TableSchema, error) {
	keyspace, tableName, err := ResolveTableName(defaultKeyspace, table)
	if err != nil {
		return nil, err
	}

	iter := session.Query(`
		SELECT column_name, kind, position, type
		FROM system_schema.columns
		WHERE keyspace_name = ? AND table_name = ?
	`, keyspace, tableName).IterContext(ctx)

	type colMeta struct {
		columnName string
		kind       string
		position   int
		cqlType    string
	}
	var metas []colMeta
	for {
		var meta colMeta
		if !iter.Scan(&meta.columnName, &meta.kind, &meta.position, &meta.cqlType) {
			break
		}
		metas = append(metas, meta)
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to query Cassandra schema: %w", err)
	}
	if len(metas) == 0 {
		return nil, nil
	}

	sort.SliceStable(metas, func(i, j int) bool {
		leftRank := columnKindRank(metas[i].kind)
		rightRank := columnKindRank(metas[j].kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if metas[i].position != metas[j].position {
			return metas[i].position < metas[j].position
		}
		return metas[i].columnName < metas[j].columnName
	})

	var columns []schema.Column
	var primaryKeys []string
	for _, meta := range metas {
		dt, precision, scale, arrayType := MapCassandraToDataType(meta.cqlType)
		isPK := meta.kind == "partition_key" || meta.kind == "clustering"
		col := schema.Column{
			Name:         meta.columnName,
			DataType:     dt,
			Nullable:     !isPK,
			Precision:    precision,
			Scale:        scale,
			ArrayType:    arrayType,
			IsPrimaryKey: isPK,
		}
		columns = append(columns, col)
		if isPK {
			primaryKeys = append(primaryKeys, meta.columnName)
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      keyspace,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func columnKindRank(kind string) int {
	switch kind {
	case "partition_key":
		return 0
	case "clustering":
		return 1
	case "static":
		return 2
	default:
		return 3
	}
}
