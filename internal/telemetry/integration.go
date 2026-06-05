//go:build integration

package telemetry

import "os"

func init() {
	_ = os.Setenv("INGESTR_DISABLE_TELEMETRY", "true")
	_ = os.Setenv("DISABLE_TELEMETRY", "true")
}
