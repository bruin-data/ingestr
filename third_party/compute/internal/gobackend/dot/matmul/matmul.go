// package matmul provides the base implementations of matrx multiply for DotGeneral. It includes a no-SIMD and various
// SIMD variations, support for "packing" for large matrices, and support for "non-transposed" ([N,K]x[K,M] -> [N,M])
// and "transposed" ([N,K]x[M,K]->[N,M]) layouts.
package matmul

import (
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/support/envutil"
	"k8s.io/klog/v2"
)

const (
	// EnabledEnv is the environment variable that controls whether the default matmul
	// implementations are enabled.
	// It's on by default, and can be disabled by setting it to false.
	EnabledEnv = "GOMLX_DOT_MATMUL"
)

// Block/packs parameters for current architecture.
type CacheParams struct {
	// LHSL1KernelRows (or Mr), the number of lhs kernel rows going to registers.
	// Set to 2, 4, or multiples of 4.
	LHSL1KernelRows int

	// RHSL1KernelCols (or Nr), the number of rhs kernel columns going to registers.
	// For SIMD it will typically be large enough for one or two SIMD vector loads.
	RHSL1KernelCols int

	// PanelContractingSize (or Kc) is the largest size of the RHS and LHF panels (used when packing)
	// on the contracting dimension.
	// Selected to make the panel fit into L2/L3 caches.
	PanelContractingSize int

	// LHSPanelCrossSize (or Mc) is the "cross" size of the LHS and Output panels (used when packing).
	// Selected to make the panel fit into L2/L3 caches.
	LHSPanelCrossSize int // Mc: L2 rows

	// RHSPanelCrossSize (or Mc) is the "cross" size of the RHS and Output panels (used when packing).
	// Selected to make the panel fit into L2/L3 caches.
	RHSPanelCrossSize int // Nc: L3 cols
}

var (
	// Used for tests only.
	ForceSmallVariant = false
	ForceLargeVariant = false
)

const (
	PriorityNoSIMD = gobackend.PriorityTyped
	PriorityAVX2   = gobackend.PriorityArch
	PriorityAVX512 = gobackend.PriorityArch + 1
)

func init() {
	avx512Enabled, err := envutil.ReadBool(envutil.SIMD_AVX512_Env, true)
	if err != nil {
		klog.Fatalf("Invalid value for %q: %+v", envutil.SIMD_AVX512_Env, err)
	}
	_ = avx512Enabled
}
