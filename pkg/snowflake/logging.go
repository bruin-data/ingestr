package snowflake

import (
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	sf "github.com/snowflakedb/gosnowflake/v2"
)

var driverLogOnce sync.Once

// ConfigureDriverLogging raises the gosnowflake logger above error level so the
// driver stops writing to stderr the failed queries ingestr issues speculatively
// and handles itself. --debug lowers it to debug instead.
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
