package parquet

import (
	"os"
	"testing"

	"github.com/bruin-data/ingestr/internal/adbctest"
)

func TestMain(m *testing.M) {
	os.Exit(adbctest.RunWithIsolatedDriverPath(m, "source-parquet"))
}
