package matmul_test

import (
	"testing"

	"github.com/gomlx/compute/internal/gobackend/dot"
	"github.com/gomlx/compute/internal/gobackend/dot/matmul"
	"github.com/gomlx/compute/support/backendtest"
)

func TestNoSIMD(t *testing.T) {
	defer func() {
		dot.ResetTestRegistrations()
		matmul.ForceSmallVariant = false
		matmul.ForceLargeVariant = false
	}()
	dot.ResetTestRegistrations()
	matmul.RegisterNoSIMDForTests()

	t.Run("Small", func(t *testing.T) {
		matmul.ForceSmallVariant = true
		matmul.ForceLargeVariant = false
		backendtest.TestDotGeneral(t, backend)
	})

	t.Run("Large", func(t *testing.T) {
		matmul.ForceSmallVariant = false
		matmul.ForceLargeVariant = true
		backendtest.TestDotGeneral(t, backend)
	})
}
