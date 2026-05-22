package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CDCMode represents the CDC operation mode
type CDCMode string

const (
	ModeBatch  CDCMode = "batch"  // Run once, exit when caught up
	ModeStream CDCMode = "stream" // Continuous streaming mode
)

type CDCConfig struct {
	Publication   string
	SlotName      string
	Mode          CDCMode
	ResumeFromLSN string // If set, skip snapshot and resume streaming from this LSN
	DestSchema    string // If set, prepend this schema to destination table names (e.g. "dataset" for BigQuery)
}

type PostgresCDCSource struct {
	queryPool *pgxpool.Pool  // Regular connection pool for queries
	replConn  *pgconn.PgConn // Replication connection
	uri       string
	cdcConfig CDCConfig
}

func NewPostgresCDCSource() *PostgresCDCSource {
	return &PostgresCDCSource{}
}

func (s *PostgresCDCSource) Schemes() []string {
	return []string{"postgres+cdc", "postgresql+cdc"}
}

func (s *PostgresCDCSource) Connect(ctx context.Context, uri string) error {
	cdcConfig, normalizedURI, err := parseURIConfig(uri)
	if err != nil {
		return fmt.Errorf("failed to parse CDC config: %w", err)
	}

	// Create query pool for regular SQL operations
	pgConfig, err := pgxpool.ParseConfig(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	queryPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := queryPool.Ping(ctx); err != nil {
		queryPool.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	// Auto-create publication if not specified
	if cdcConfig.Publication == "" {
		cdcConfig.Publication = "ingestr_publication"
		if err := ensurePublicationExists(ctx, queryPool, cdcConfig.Publication); err != nil {
			queryPool.Close()
			return fmt.Errorf("failed to create publication: %w", err)
		}
	}

	// Create replication connection
	replConnStr := buildReplicationConnString(normalizedURI)
	replConn, err := pgconn.Connect(ctx, replConnStr)
	if err != nil {
		queryPool.Close()
		return fmt.Errorf("failed to create replication connection: %w", err)
	}

	s.queryPool = queryPool
	s.replConn = replConn
	s.uri = uri
	s.cdcConfig = cdcConfig

	return nil
}

func (s *PostgresCDCSource) Close(ctx context.Context) error {
	var errs []error

	if s.replConn != nil {
		if err := s.replConn.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close replication connection: %w", err))
		}
	}

	if s.queryPool != nil {
		s.queryPool.Close()
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (s *PostgresCDCSource) HandlesIncrementality() bool {
	return false
}

func (s *PostgresCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	return NewCDCTable(s, req)
}

func parseURIConfig(uri string) (CDCConfig, string, error) {
	cfg := CDCConfig{
		Mode: ModeBatch,
	}

	// Remove scheme suffix (+cdc)
	normalizedURI := uri
	if strings.HasPrefix(uri, "postgres+cdc://") {
		normalizedURI = "postgres://" + strings.TrimPrefix(uri, "postgres+cdc://")
	} else if strings.HasPrefix(uri, "postgresql+cdc://") {
		normalizedURI = "postgresql://" + strings.TrimPrefix(uri, "postgresql+cdc://")
	}

	parsed, err := url.Parse(normalizedURI)
	if err != nil {
		return cfg, "", fmt.Errorf("failed to parse URI: %w", err)
	}

	query := parsed.Query()

	cfg.Publication = query.Get("publication")
	cfg.SlotName = query.Get("slot")
	cfg.DestSchema = query.Get("dest_schema")

	if mode := query.Get("mode"); mode != "" {
		switch mode {
		case "batch":
			cfg.Mode = ModeBatch
		case "stream":
			cfg.Mode = ModeStream
		default:
			return cfg, "", fmt.Errorf("invalid mode: %s (must be 'batch' or 'stream')", mode)
		}
	}

	// Remove CDC-specific params from connection string
	query.Del("publication")
	query.Del("slot")
	query.Del("mode")
	query.Del("dest_schema")
	parsed.RawQuery = query.Encode()

	return cfg, parsed.String(), nil
}

func ensurePublicationExists(ctx context.Context, pool *pgxpool.Pool, pubName string) error {
	var exists bool
	err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_publication WHERE pubname = $1)", pubName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check publication existence: %w", err)
	}

	if exists {
		config.Debug("[CDC] Publication %s already exists", pubName)
		return nil
	}

	config.Debug("[CDC] Creating publication %s FOR ALL TABLES", pubName)
	// Use pgx.Identifier to safely quote the publication name - DDL statements don't support parameter placeholders
	pubIdent := pgx.Identifier{pubName}.Sanitize()
	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", pubIdent))
	if err != nil {
		// Handle race condition: another instance may have created the publication concurrently
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == pgErrDuplicate || pgErr.Code == pgErrAlreadyExists) {
			config.Debug("[CDC] Publication %s was created by another instance (concurrent race)", pubName)
			return nil
		}
		return fmt.Errorf("failed to create publication: %w", err)
	}

	config.Debug("[CDC] Publication %s created successfully", pubName)
	return nil
}

func buildReplicationConnString(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return uri + "?replication=database"
	}

	query := parsed.Query()
	query.Set("replication", "database")
	parsed.RawQuery = query.Encode()

	config.Debug("[CDC] Replication connection string: %s", parsed.String())

	return parsed.String()
}

// MultiTableSource interface implementation

// IsMultiTable returns true if this CDC source should operate in multi-table mode.
// Returns true when no specific table is requested (source-table flag not provided).
func (s *PostgresCDCSource) IsMultiTable() bool {
	// CDC sources are multi-table by default - they capture changes from all tables in the publication
	return true
}

// GetTables returns all tables in the publication with their schemas.
func (s *PostgresCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	// Check if this is a "FOR ALL TABLES" publication
	var pubAllTables bool
	err := s.queryPool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", s.cdcConfig.Publication).Scan(&pubAllTables)
	if err != nil {
		return nil, fmt.Errorf("failed to check publication type: %w", err)
	}

	config.Debug("[CDC] Publication %s puballtables=%v", s.cdcConfig.Publication, pubAllTables)

	var query string
	if pubAllTables {
		// For "FOR ALL TABLES" publications, query all user tables directly
		query = `
			SELECT schemaname, tablename
			FROM pg_tables
			WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
			ORDER BY schemaname, tablename
		`
	} else {
		// For specific table publications, use pg_publication_tables
		query = `
			SELECT schemaname, tablename
			FROM pg_publication_tables
			WHERE pubname = $1
			ORDER BY schemaname, tablename
		`
	}

	var rows pgx.Rows
	if pubAllTables {
		rows, err = s.queryPool.Query(ctx, query)
	} else {
		rows, err = s.queryPool.Query(ctx, query, s.cdcConfig.Publication)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query publication tables: %w", err)
	}
	defer func() { rows.Close() }()

	var tables []source.SourceTableInfo
	for rows.Next() {
		var schemaName, tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		fullName := tableName
		if schemaName != "public" {
			fullName = schemaName + "." + tableName
		}

		// Get schema for this table
		tableSchema, err := getTableSchema(ctx, s.queryPool, fullName)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for table %s: %w", fullName, err)
		}

		// Add CDC metadata columns
		tableSchema = addCDCColumns(tableSchema)

		tables = append(tables, source.SourceTableInfo{
			Name:        fullName,
			Schema:      tableSchema,
			PrimaryKeys: tableSchema.PrimaryKeys,
			DestSchema:  s.cdcConfig.DestSchema,
		})

		config.Debug("[CDC] Found table in publication: %s (PKs: %v)", fullName, tableSchema.PrimaryKeys)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(tables) == 0 {
		config.Debug("[CDC] No tables found. pubAllTables=%v, query used: %s", pubAllTables, query)
		return nil, fmt.Errorf("no tables found in publication %s", s.cdcConfig.Publication)
	}

	config.Debug("[CDC] Found %d tables in publication %s", len(tables), s.cdcConfig.Publication)
	return tables, nil
}

// ReadAll reads CDC changes from all tables in the publication.
func (s *PostgresCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	tables, err := s.GetTables(ctx)
	if err != nil {
		return nil, err
	}

	reader := NewMultiTableCDCReader(s, tables, s.cdcConfig, opts.CDCResumeLSNs, opts.CDCSlotSuffix)
	return reader.Read(ctx, opts)
}

const (
	pgErrAlreadyExists = "42710"
	pgErrDuplicate     = "23505"
)

var (
	_ source.Source           = (*PostgresCDCSource)(nil)
	_ source.MultiTableSource = (*PostgresCDCSource)(nil)
)
