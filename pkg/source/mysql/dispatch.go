package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

// The mysql and mysql+cdc schemes serve both MySQL/MariaDB and Vitess. Vitess
// speaks the MySQL wire protocol via vtgate but differs fundamentally for change
// data capture (no standard binlog; changes come from the VStream gRPC API). To
// keep the two implementations fully decoupled, each scheme registers a thin
// dispatcher that probes the server once on Connect (SELECT @@version) and then
// forwards every call to the appropriate backend.

// mysqlSourceDispatcher selects between MySQLSource and VitessSource for plain
// (non-CDC) reads.
type mysqlSourceDispatcher struct {
	backend source.Source
}

func newMySQLSourceDispatcher() *mysqlSourceDispatcher {
	return &mysqlSourceDispatcher{}
}

func (d *mysqlSourceDispatcher) Schemes() []string {
	return []string{"mysql", "mysql+pymysql", "mariadb"}
}

func (d *mysqlSourceDispatcher) Connect(ctx context.Context, uri string) error {
	dsn, _, err := uriToDSN(uri)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL URI: %w", err)
	}
	if vitessServer(ctx, dsn) {
		config.Debug("[SOURCE] detected Vitess server; using Vitess source")
		d.backend = NewVitessSource()
	} else {
		d.backend = NewMySQLSource()
	}
	return d.backend.Connect(ctx, uri)
}

func (d *mysqlSourceDispatcher) Close(ctx context.Context) error {
	if d.backend == nil {
		return nil
	}
	return d.backend.Close(ctx)
}

func (d *mysqlSourceDispatcher) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	return d.backend.GetTable(ctx, req)
}

func (d *mysqlSourceDispatcher) HandlesIncrementality() bool {
	return d.backend.HandlesIncrementality()
}

// mysqlCDCDispatcher selects between MySQLCDCSource (binlog) and VitessCDCSource
// (VStream) for change data capture.
type mysqlCDCDispatcher struct {
	backend source.MultiTableSource
}

func newMySQLCDCDispatcher() *mysqlCDCDispatcher {
	return &mysqlCDCDispatcher{}
}

func (d *mysqlCDCDispatcher) Schemes() []string {
	return []string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"}
}

func (d *mysqlCDCDispatcher) Connect(ctx context.Context, uri string) error {
	// parseMySQLCDCURI strips the +cdc scheme and CDC/Vitess/PlanetScale-only
	// query params, yielding a clean MySQL URI usable for the version probe.
	cfg, normalizedURI, connInfo, err := parseMySQLCDCURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL CDC URI: %w", err)
	}

	// PlanetScale's MySQL endpoint identifies as Vitess, so it must be selected
	// before the @@version probe below would route it to VStream (which cannot
	// reach PlanetScale's CDC API).
	if usePlanetScaleCDC(cfg, connInfo.Host) {
		config.Debug("[SOURCE] using PlanetScale psdbconnect CDC source")
		d.backend = NewPlanetScaleCDCSource()
		return d.backend.Connect(ctx, uri)
	}

	dsn, _, err := uriToDSN(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL URI: %w", err)
	}
	if vitessServer(ctx, dsn) {
		config.Debug("[SOURCE] detected Vitess server; using Vitess CDC (VStream) source")
		d.backend = NewVitessCDCSource()
	} else {
		d.backend = NewMySQLCDCSource()
	}
	return d.backend.Connect(ctx, uri)
}

// usePlanetScaleCDC reports whether CDC should use PlanetScale's psdbconnect API
// instead of vtgate VStream. An explicit cdc_backend override wins; otherwise a
// service token or a *.psdb.cloud host selects PlanetScale.
func usePlanetScaleCDC(cfg MySQLCDCConfig, host string) bool {
	switch cfg.CDCBackend {
	case "planetscale", "psdb", "psdbconnect":
		return true
	case "vstream", "vitess":
		return false
	}
	if cfg.PSDBToken != "" || cfg.PSDBTokenName != "" {
		return true
	}
	return strings.HasSuffix(strings.ToLower(host), ".psdb.cloud")
}

func (d *mysqlCDCDispatcher) Close(ctx context.Context) error {
	if d.backend == nil {
		return nil
	}
	return d.backend.Close(ctx)
}

func (d *mysqlCDCDispatcher) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	return d.backend.GetTable(ctx, req)
}

func (d *mysqlCDCDispatcher) HandlesIncrementality() bool {
	return d.backend.HandlesIncrementality()
}

func (d *mysqlCDCDispatcher) IsMultiTable() bool {
	return d.backend.IsMultiTable()
}

func (d *mysqlCDCDispatcher) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	return d.backend.GetTables(ctx)
}

func (d *mysqlCDCDispatcher) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	return d.backend.ReadAll(ctx, opts)
}

// vitessServer opens a short-lived connection and reports whether the server
// identifies as Vitess. On any probe error it returns false, so the dispatcher
// falls back to the MySQL backend (matching prior behavior, where undetected
// servers were treated as plain MySQL).
func vitessServer(ctx context.Context, dsn string) bool {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		config.Debug("[SOURCE] Vitess probe: failed to open connection: %v", err)
		return false
	}
	defer func() { _ = db.Close() }()

	isVitess, err := isVitessServer(ctx, db)
	if err != nil {
		config.Debug("[SOURCE] Vitess probe: failed to detect server version: %v", err)
		return false
	}
	return isVitess
}

var (
	_ source.Source           = (*mysqlSourceDispatcher)(nil)
	_ source.Source           = (*mysqlCDCDispatcher)(nil)
	_ source.MultiTableSource = (*mysqlCDCDispatcher)(nil)
)
