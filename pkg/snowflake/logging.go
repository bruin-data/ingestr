package snowflake

import (
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	sf "github.com/snowflakedb/gosnowflake"
)

var driverLogOnce sync.Once

// ConfigureDriverLogging raises the gosnowflake logger above error level so the
// driver stops writing to stderr the failed queries ingestr issues speculatively
// and handles itself. --debug lowers it to debug instead.
//
// "OFF" would not work: the driver logs through WithContext().Errorf, a logrus
// Entry gated only by the inner level, not by the flag "OFF" clears.
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
