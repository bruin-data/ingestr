- 2026-08-28:
  - Fixed `gobackend.DataEqual` to check `reflect.Type.Comparable()` before equality check to support deduplication of nodes with non-comparable structs (containing slices).
  - Fixed `gobackend.DotGeneral` de-normalization reshape to delegate to `DynamicReshape` when handling dynamic matrices/batches.

- 2026-08-27:
  - Added `DynamicIota` and `DynamicPad` backend operations to `DynamicOps` interface (with `OpTypeDynamicIota` and `OpTypeDynamicPad`), implemented in the Go backend (`gobackend`) and ONNX backend (`compute-onnx`), along with `shapeinference` and backend compliance tests.
  - Added `DynamicBroadcastInDim` backend operation to `DynamicOps` interface (with `OpTypeDynamicBroadcastInDim`), implemented in the Go backend (`gobackend`) and ONNX backend (`compute-onnx`), along with `shapeinference` and backend compliance tests.
  - Added `CumSum` backend operator to `StandardOps` supporting `Exclusive` and `Reverse` options (with Go backend implementation).

- 2026-08-02:
  - Fix dynamic output shape materialization in `gobackend` for `binaryOps`, `Concatenate`, and `Reduce` operations so `Buffer.RawShape` is always materialized/concrete.
  - Implement symbolic dynamic axis naming for `Concatenate` (`=term1+term2`) with support for parsing/resolving composite symbolic names in `shapes.Resolve`.
  - Add backend compliance tests for `binaryOps`, `Concatenate`, and `Reduce` with dynamic shapes under `support/backendtest`.

- 2026-07-28:
  - Updated FusedDense to take a `DenseConfig` options parameter, which now includes layout information of the weights.
  - Renamed `AxesLayout` -> `AttentionAxesLayout` (since it's for attention only).

# Initial release

- Moved GoMLX's `backends/simplego` to `gobackend`.
- Removed all dependencies to `stretchr/testify` and `gomlx`, to trim as much as possible external dependencies.
- Moved `gobackend` generic tests to `support/backendtest`, so they can be used by other backends.
- Package `gobackend`:
  - Fixed definition of `Bitcast` when casting to a larger target dtype: the rank is shrinked by 1.
- Package `support`:
  - The following packages were moved from `github.com/gomlx/gomlx/pkg/support/...` to `support/...`: `xslices`, `xsync`, `sets` and `humanize`.

- Package `shapes`: added initial support for dynamic shapes (see `./docs/DynamicShapes.md` for overall idea):
  - Add `Shape.Resolve(AxisBindings) (Shape, error)` method.
  - Add `Shape.IsDynamic()` method.
  - Add `DynamicDim` type.

- New ops:
  - `SchedulingBarrier` and `OptimizationBarrier`, both implemented in the Go backend.