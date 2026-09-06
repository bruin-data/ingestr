# GoMLX compatibility forks

## ONNX importer

`onnx-gomlx/` is the Apache-2.0-licensed `github.com/gomlx/onnx-gomlx`
v0.5.6 source carried from the GLiNER pure-Go proof of concept. Its LICENSE is
retained. Only these upstream source changes are present:

- `internal/onnxgomlx/ops.go`: allow ONNX Flatten's valid `axis == rank`.
- `internal/onnxgomlx/graph.go`: rank-one, axis-zero, reduction-none
  ScatterElements, including negative indices.

Regression tests live in `pkg/transformer/gliner/compat_test.go`. Remove the
replace directive and this fork when an upstream release covers both cases.
The batch-one LSTM adaptation and constant retention setting live in ingestr's
GLiNER runtime, not this fork. No weights or native runtime are vendored.

## Compute backend

`compute/` is `github.com/gomlx/compute` v0.1.7, with its Apache-2.0 license
retained. Real GLiNER inference under `-race` exposed invalid stored-`uintptr`
arithmetic in its small-matrix kernels (`checkptr` fails in `nosimd_small.go`).
The default kernels now use typed slice indexing, not disabled pointer checks:

- Replace `nosimd_small.go` with the upstream `nosimd_small_safe.go` implementation,
  enabled for ordinary builds; include `batchStart` in bounds checks and retain
  half-precision conversion in the column fringe.
- Convert `nosimd_small_transposed.go` to equivalent typed indexing, retaining
  its accumulation order and including `batchStart` in bounds checks.
- Use upstream's typed `packLHS` in the large non-SIMD kernel instead of
  `unsafePackLHS`, which has the same stored-pointer problem.
- Regenerate the three half-precision variants with the upstream alternates
  generator; remove the now-redundant `*_safe*` files and generation directive.

`TestSmallMatmulTypedSlices` checks both layouts without model assets, including
under `-race`. `TestModelReferenceParity` checks the complete FP32 model with
race/pointer instrumentation when explicitly enabled. Experimental SIMD kernels
are not part of this integration; no experimental Go build flags are needed.
Remove this fork when an upstream release supplies equivalent safe kernels.
