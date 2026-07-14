## Project Overview

**ingestr** is a Go-based data ingestion CLI that transfers data between databases and formats. It uses Apache Arrow for in-memory data representation and ADBC (Arrow Database Connectivity) for database interactions.

- Go module: `github.com/bruin-data/ingestr`
- Binary: `ingestr` (built to `bin/ingestr`)
- Entry point: `main.go` → `cmd/`

## Build and Test Commands

```bash
# Build the application
make build                    # Builds to bin/ingestr

# Run unit tests
make test                     # All unit tests with race detection
go test -short ./...          # Unit tests only

# Run integration tests (gated by the `integration` build tag)
make test-integration
go test -tags integration -v -run TestPostgresToPostgres ./tests/integration/...

# Clean build artifacts
make clean

# Build and run
make run ARGS="ingest --source-uri=postgres://... --dest-uri=sqlite://... --source-table=users"

# Direct execution
go run . ingest --source-uri=<uri> --dest-uri=<uri> --source-table=<table>

# Format and lint
make format                   # gci + gofumpt + go vet + golangci-lint
make lint                     # Linters only

# After changing dependencies (go.mod/go.sum), refresh the license audit lock
make licenses-audit-update    # CI runs `make licenses-audit` (--check) and fails if the lock is stale
```

Always run `make format`, `make lint` and `make test` when you are done with making your changes, ensure they pass.

When your change touches `go.mod`/`go.sum`, also run `make licenses-audit-update` and commit any resulting lock changes — CI's `make licenses-audit` will fail otherwise. (A version bump within the same license set may produce no diff, which is fine.)

## Architecture Overview

### Core Components

1. **Pipeline** (`pkg/pipeline/pipeline.go`): Orchestrates the complete ingestion workflow
   - Connects to source and destination using URI registry
   - Fetches schema from source
   - Auto-detects primary keys if not provided
   - Selects and validates ingestion strategy
   - Executes the strategy with an IngestionJob

2. **URI Registry** (`internal/uri/registry.go`): Central registry pattern for source/destination discovery
   - Maps URI schemes (postgres, duckdb, bigquery, etc.) to constructor functions
   - Provides `GetSource(uri)` and `GetDestination(uri)` methods
   - `DefaultRegistry` is initialized at package init time with all supported connectors

3. **Sources** (`pkg/source/`): Data extraction layer
   - All sources implement the `Source` interface with `Connect`, `GetSchema`, `Read`, `Close`
   - Return data as `<-chan RecordBatchResult` streaming Arrow record batches
   - Two main patterns:
     - **ADBC-based**: Generic ADBC source with pluggable Dialect interface (DuckDB, Snowflake, BigQuery)
     - **Native driver**: Direct database driver usage (Postgres via pgx, MySQL, MSSQL)

4. **Destinations** (`pkg/destination/`): Data loading layer
   - All destinations implement the `Destination` interface
   - Key methods: `PrepareTable`, `Write`, `WriteParallel`, `SwapTable`
   - Support transaction handling via `Transaction` interface
   - Consume Arrow record batches from sources

5. **Strategies** (`pkg/strategy/`): Write pattern implementations
   - Registry-based pattern with `Register()` and `Get()` functions
   - Each strategy implements the `WriteStrategy` interface
   - Current strategies: `replace` (drop/recreate), `merge` (upsert by primary key)
   - Strategy validation occurs after primary key auto-detection

6. **Schema** (`pkg/schema/schema.go`): Internal type system and Arrow conversion
   - Defines `TableSchema` with columns, primary keys, schema name
   - `Column` type with DataType enum, precision, scale, nullability
   - Converts between database types, internal types, and Arrow types

### ADBC Dialect System

The ADBC source uses a Dialect interface to abstract database-specific behavior:

- **Dialect**: Base interface for driver management, SQL templates, type mapping
- **DatasetAwareDialect**: For databases like BigQuery that embed schema in query paths
- **DatasetConnector**: For databases requiring `dataset_id` in connection string (BigQuery)
- **SchemaProvider**: Optional interface for native API schema fetching (faster than SQL)

Database-specific dialects in `pkg/source/{database}/dialect.go`:
- `duckdb/dialect.go`
- `snowflake/dialect.go`, `snowflake/mapper.go`
- `bigquery/dialect.go`, `bigquery/mapper.go`
- Each also has `mapper.go` for database-specific type mapping logic

### Key Patterns

**Configuration Flow**:
1. CLI flags parsed in `cmd/ingest.go`
2. Config struct (`internal/config/config.go`) populated with defaults and user input
3. Config validation (required fields, strategy validation)
4. Pipeline created with config and executed

**Data Flow**:
1. `Source.Read()` returns `<-chan RecordBatchResult` with Arrow record batches
2. Strategy executes its pattern (e.g., staging table, merge, swap)
3. `Destination.Write()` or `WriteParallel()` consumes the channel
4. Arrow format enables zero-copy transfer where possible

**Primary Key Handling**:
- Sources attempt to detect PKs during `GetSchema()`
- Pipeline auto-populates `config.PrimaryKeys` if empty and source provides them
- Strategy validation checks for required PKs after auto-detection
- This allows merge strategy to work without explicit PK specification

## Important Notes

### Code Standards
Do NOT write comments everywhere. If the code is self-explanatory, do not write comments.

### BigQuery Destination
Before changing `pkg/destination/bigquery/` or BigQuery-affecting behavior in the replace/merge strategies, read the `bigquery-destination` skill (`.claude/skills/bigquery-destination/SKILL.md`) — it documents the design (write path, dedup, swap selection, partition/cluster change handling). Update it in the same change if you alter that behavior.

### Type Mapping
Each source must map its native types to the `schema.DataType` enum. The ADBC dialect system delegates this via `MapDataType(dbType string)`. Native sources implement mapping directly (e.g., `pkg/source/postgres/mapper.go`).

### Timestamp Convention
All timestamps in the Arrow layer use **microseconds** as the standard unit. This is enforced throughout the codebase:

- **Schema layer** (`pkg/schema/schema.go`): `TypeTimestamp` and `TypeTimestampTZ` map to `arrow.TimestampType{Unit: arrow.Microsecond}`
- **Sources**: Must convert native timestamp values to microseconds using `time.Time.UnixMicro()` or equivalent
- **Schema inference** (`pkg/schemainfer/merge.go`): Merges timestamp types to microseconds
- **Destinations**: Can assume incoming timestamps are in microseconds

When implementing a new source with timestamps:
```go
// Correct: convert to microseconds
case time.Time:
    b.Append(arrow.Timestamp(v.UnixMicro()))

// Correct: convert milliseconds to microseconds
case primitive.DateTime: // MongoDB stores as milliseconds
    b.Append(arrow.Timestamp(int64(v) * 1000))
```

This convention exists because:
1. Most databases use microseconds internally (PostgreSQL, BigQuery)
2. Sufficient precision for virtually all use cases
3. BigQuery's Storage Write API expects microseconds regardless of Arrow schema unit metadata

### ADBC Driver Management
ADBC drivers are installed via the native `github.com/columnar-tech/dbc` client. The `pkg/source/adbc` package handles this automatically. Drivers are cached and only installed once.

### Integration Tests
Located in `tests/integration/`. Tests use testcontainers for PostgreSQL and create temporary files for SQLite/DuckDB. Integration tests are gated by the `integration` build tag — run them with `go test -tags integration ./tests/integration/...` or `make test-integration`. Plain `go test ./...` will not pick them up.

### Error Handling
- Config validation returns `*ValidationError` with field name and message
- Pipeline wraps errors with context (e.g., `"failed to connect to source: ..."`)
- ADBC source provides debug logging via `config.Debug()` when `--debug` flag is set
- Always use `fmt.Errorf` with `%w` for error wrapping to preserve error chains

### URI Formats
The tool accepts various URI schemes:
- PostgreSQL: `postgres://`, `postgresql://`, `postgresql+psycopg2://`
- MySQL: `mysql://`, `mysql+pymysql://`, `mariadb://`
- MSSQL: `mssql://`, `sqlserver://`, `mssql+pyodbc://`
- MongoDB: `mongodb://`, `mongodb+srv://`
- DuckDB: `duckdb:///path/to/db.db`
- Snowflake: `snowflake://user:pass@account/database/schema`
- BigQuery: `bigquery://project/dataset`
- SQLite: `sqlite:///path/to/db.db`
- CSV: `csv://path/to/file.csv`
- Parquet: `parquet://path/to/file.parquet`

### Adding New Sources
1. Implement the `Source` interface or create an ADBC Dialect
2. Register in `internal/uri/registry.go` `init()` with URI schemes
3. If using ADBC: implement Dialect with SQL templates and type mapper
4. If native driver: handle connection, schema fetching, and batch reading directly

**Schema-less sources** (like MongoDB): Implement `HasKnownSchema() bool` returning `false`. The pipeline will automatically use schema inference (`pkg/schemainfer/`) to derive the schema from the first batch of data. The source should still emit proper Arrow types — use `pkg/schema.JSONArrowType` for nested documents and arrays.

### Adding New Strategies
1. Implement `WriteStrategy` interface in `pkg/strategy/`
2. Register in `pkg/strategy/strategy.go` `init()` function
3. Implement validation for required config (e.g., primary keys for merge)
4. Execute pattern using `IngestionJob` (has source, destination, schema, config)
