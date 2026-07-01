package mysql

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	// Each product gets its own scheme; routing is by scheme alone (no server
	// probing). MySQL/MariaDB, Vitess, and PlanetScale all speak the MySQL wire
	// protocol but diverge for change data capture, so the CDC schemes map to
	// three distinct backends.
	registry.RegisterSource(
		[]string{"mysql", "mysql+pymysql", "mariadb"},
		func() interface{} { return NewMySQLSource() },
	)
	registry.RegisterSource(
		[]string{"vitess", "planetscale"},
		func() interface{} { return NewVitessSource() },
	)
	registry.RegisterSource(
		[]string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"},
		func() interface{} { return NewMySQLCDCSource() },
	)
	registry.RegisterSource(
		[]string{"vitess+cdc"},
		func() interface{} { return NewVitessCDCSource() },
	)
	registry.RegisterSource(
		[]string{"planetscale+cdc"},
		func() interface{} { return NewPlanetScaleCDCSource() },
	)
}
