package adbctest

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func RunWithIsolatedDriverPath(m *testing.M, name string) int {
	dir, err := os.MkdirTemp("", "ingestr-adbc-drivers-"+sanitizeName(name)+"-")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "create isolated ADBC driver path: %v\n", err)
		return 1
	}

	previous, hadPrevious := os.LookupEnv("ADBC_DRIVER_PATH")
	if err := os.Setenv("ADBC_DRIVER_PATH", dir); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "set ADBC_DRIVER_PATH: %v\n", err)
		_ = os.RemoveAll(dir)
		return 1
	}

	code := m.Run()

	if hadPrevious {
		_ = os.Setenv("ADBC_DRIVER_PATH", previous)
	} else {
		_ = os.Unsetenv("ADBC_DRIVER_PATH")
	}
	_ = os.RemoveAll(dir)

	return code
}

func sanitizeName(name string) string {
	return strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(name)
}
