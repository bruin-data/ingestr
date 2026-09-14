package snowflake

import (
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	sf "github.com/snowflakedb/gosnowflake"
)

var driverLogOnce sync.Once

// ConfigureDriverLogging raises the gosnowflake logger above error level so the
// driver stops writing every failed query to stderr.
//
// The driver logs the error before returning it, so queries ingestr issues
// speculatively — probing a destination table that does not exist yet, for
// instance — surface as level=error lines even though ingestr handles them.
// ingestr reports the failures it cares about itself.
//
// A Snowflake client config file still wins: the driver applies easy logging
// when it builds a connection, which happens after this runs.
func ConfigureDriverLogging() {
	driverLogOnce.Do(func() {
		level := "fatal"
		if config.DebugMode {
			level = "debug"
		}
		if err := sf.GetLogger().SetLogLevel(level); err != nil {
			config.Debug("failed to set gosnowflake log level to %s: %v", level, err)
		}
	})
}
