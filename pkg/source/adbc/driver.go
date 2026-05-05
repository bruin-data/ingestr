package adbc

import (
	"database/sql"
	"sync"

	"github.com/apache/arrow-adbc/go/adbc/drivermgr"
	"github.com/apache/arrow-adbc/go/adbc/sqldriver"
)

// ADBCDriverName is the registered SQL driver name for ADBC via driver manager.
// All ADBC-based sources use this single driver registration, with the specific
// database driver specified in the connection string (e.g., "driver=duckdb;...").
const ADBCDriverName = "adbc_generic"

var driverOnce sync.Once

func init() {
	driverOnce.Do(func() {
		sql.Register(ADBCDriverName, sqldriver.Driver{Driver: &drivermgr.Driver{}})
	})
}
