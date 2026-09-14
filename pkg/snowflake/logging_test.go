package snowflake

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	sf "github.com/snowflakedb/gosnowflake"
)

// Mirrors the driver's own call in connection.go queryContextInternal.
func TestConfigureDriverLoggingSilencesQueryErrors(t *testing.T) {
	var buf bytes.Buffer
	sf.GetLogger().SetOutput(&buf)
	t.Cleanup(func() { sf.GetLogger().SetOutput(os.Stderr) })

	ConfigureDriverLogging()

	if level := sf.GetLogger().GetLogLevel(); level != "fatal" {
		t.Fatalf("expected driver log level fatal, got %q", level)
	}

	sf.GetLogger().WithContext(context.Background()).Errorf("error: %v", errors.New("Table 'DB.SCHEMA.T' does not exist or not authorized"))

	if buf.Len() != 0 {
		t.Fatalf("expected driver error to be suppressed, got %q", buf.String())
	}
}
