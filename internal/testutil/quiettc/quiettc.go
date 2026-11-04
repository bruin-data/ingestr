// Package quiettc silences testcontainers' Docker lifecycle logs ("Creating
// container for image…", "Container started") in the importing test binary when
// output.QuietEnv is set (the Makefile test targets). Import it for side effects
// from a test file in any package that starts containers:
//
//	import _ "github.com/bruin-data/ingestr/internal/testutil/quiettc"
//
// It is a leaf package (no ingestr dependencies beyond internal/output) so it can
// be imported from in-package tests without creating import cycles.
package quiettc

import (
	"github.com/bruin-data/ingestr/internal/output"
	tclog "github.com/testcontainers/testcontainers-go/log"
)

type quietLogger struct{}

func (quietLogger) Printf(string, ...any) {}

func init() {
	if output.Quiet() {
		tclog.SetDefault(quietLogger{})
	}
}
