# 2026/08/17

- Removed `internal/protos`, and instead use `github.com/gomlx/compute-onnx/support/protos`, which is identical
  (This is because protos register globally their definitions, and we cannot have 2 definitions of the same proto)

# v0.5.0: Updates for GoMLX v0.28.0

# v0.4.2: Updated dependencies.

- Updated dependency to GoMLX v0.27.3 and go-huggingface v0.3.5.

# v0.4.1: API adjustment

- Updated dependency to GoMLX v0.27.1
- Package `onnx`:
  - Added `WithBaseDir()` to set the directory where to read external data files from -- if using the default 
    external data reader.
  - Added `ExternalDataReader` interface
  - Added `WithExternalDataReader()` to configure an specialized external data reader.
  - Configuration methods now return a `Model` so configuration calls can be cascaded.
- Package `onnx/parser`: 
  - `Parse(data []bytes) (*Model, error)`
  - `ParseFile(filePath string) (*Model, error)`
  - `ParseFromReader(reader io.Reader) (*Model, error)`
- Package `internal/onnxgomlx`:
  - Implemented changes in `onnx` API (see above).
  - Split default `ExternalDataReader` into package `internal/onnxgomlx/filesreader`. 

# v0.4.0: More ops and fused ops; Added support for model [Florence-2](https://huggingface.co/microsoft/Florence-2-large); `onnx` package refactored.

- Package `onnx`: split implementation into `internal/onnxgomlx/...`, `onnx` is now just a public API.
  - **API change**: it's a small change but `onnx.Model` is now an interface (it was a pointer to an object)
    and the constructors reside in `onnx/parser`.
- `Mod` operator: Supports both fmod=1 (C-style, sign follows dividend) and fmod=0 (Python-style, sign follows divisor) with broadcasting and dtype promotion
- `onnxImplicitFloatPromotion` for float-only ops (Sqrt, Exp, etc.).
- `Concat` dtype alignment: When dtype promotion is enabled, all Concat operands are cast to the first operand's dtype, preserving Int64 for shape/index tensors.
- `isVariableConstant` loosening: Float variables with "const" in the name are now accepted as materializable constants (needed when Concat dtype promotion casts Float32 constants to Int64).
- Sub-graph name shadowing fix: convertSubGraph now saves and restores parent entries in nodeOutputToNode / variableNameToValue instead of unconditionally deleting them on cleanup.
- `convertIf()` rework: Uses GoMLX's native If with closures instead of the Where-based approach.
- Added support for [Florence2 model](https://huggingface.co/microsoft/Florence-2-large); 
- Updated GoMLX dependency to v0.27.0.

# v0.3.5: New ops for various models support (Gemma, Snowflake,CLAP, etc); Many fixes and improvements.

- Added quantized fusion patterns for dense layers, QKV projections, and scaled dot-product attention (SDPA). (by @ajroetker)
- Fixed `isZeroInitializer` to handle tensors with zero-sized dimensions (e.g., `[batchSize, 0]`).
- Add ONNX operators for Gemma and Snowflake Arctic models: `ReduceL2`, `SimplifiedLayerNormalization` (RMSNorm),
  `RotaryEmbedding`, `MultiHeadAttention`.
- Simplified optional DType auto-promotion.
- Added Resize operation for "CLAP" model support.
- New Einsum op support (2 operands).
- Various bug fixes across multiple ops and sub-graph handling.
- Added ReduceMax, ReduceMin, ReduceSum, ReduceProd, and NonZero ONNX operations.
- Optional auto-promotion of dtypes in case of mismatches: this is an error
  in ONNX specification, but some Pytorch models do this. See `Model.AllowDTypePromotion()`.
- Added support for reading variables from external data files (due to proto 2/4 Gb size-limit)
- Several fixes (see logs)

# v0.3.4: Updated for GoMLX 0.26.0

- Using new github.com/gomlx/go-xla library.
- GoMLX now auto-installs XLA PJRT plugins (it can be disabled)

# v0.3.3: Basic Quantization support (@ajroetker); Updated for GoMLX 0.25.0

- Updates for GoMLX 0.25.0
- MatMulInteger quantization operation for integer matrix multiplication on quantized values. (by @ajroetker)
  - Y = (A - a_zero_point) * (B - b_zero_point)
- Clean ups (by @siherrmann)

# v0.3.2: New ops (2025/11/18) 

- Thanks to @timakey11 for this release the contribution!
- New Operators: 
  - `LayerNormalization`: Added support for the LayerNormalization operator, a key component in
    many modern neural network architectures.
  - `Split`: Implemented the Split operator, allowing tensors to be split into multiple outputs.
  - `If`: Added support for the If control flow operator, enabling conditional execution within the
    graph.
- Subgraph support: used by the `If` operator.
- New Models that we can now run :
  - https://huggingface.co/mirth/chonky_modernbert_base_1
  - https://huggingface.co/mixedbread-ai/mxbai-rerank-base-v1

# v0.3.1: Updated test dependencies

* Updated test dependencies: including go-huggingface and github.com/daulet/tokenizers to their latest versions.
* Added InceptionV3 model benchmark.
* Added support for `Pad` and `AveragePool` ONNX ops.

# v0.3.0: Updated to GoMLX v0.24.0

* Updated dependencies to GoMLX v0.24.0, and its improved directory organization.
  Since GoMLX changed the API (package directories changed), we bump the version here as well.
* Updates to README.md.

# v0.2.5 2025/08/22

* Updated dependencies to GoMLX v0.22.1
* Added Conv, MaxPool, BatchNormalization, and AverageGlobalPool operations.
* Added `Sin` and `Cos`
* Added `ScatterND` and `Trilu`
* Updated `Slice` to fully match ONNX spec

# v0.2.4 2025/06/12 

* Added support for other ONNX dtypes that require conversion during reading.
  Also added conversion when saving values back to the ONNX proto.
* Updated dependencies to GoMLX.
* Added `onnxtests.py` to help test/explore individual ONNX ops using ONNXRuntime. 
* New ops: `DequantizeLinear`, `DynamicQuantizeLinear`.

# v0.2.3 2025/05/31

* Added Save/Check values of outputs for internal/benchmarks: allows it to be
  used as a functional test during the development of GoMLX Go backend.
* Updated dependencies to latest GoMLX v0.19.5

# v0.2.2 2025/05/22

* Added Min and Max operators.
* Updated dependency to GoMLX v0.19.3.

# v0.2.1 2025/05/01

* Updated to GoMLX v0.19.1
* Included default GoMLX backends by default.

# v0.2.0 2025/02/02

* Updated to GoMLX v0.17.0
* Added bitwise operators.
* Added parallel benchmarks.
* Added benchmarks documentation.

# v0.1.5 🎄 2024/12/19 🎄

* Added `internal/bechmarks` package: See progress in https://docs.google.com/spreadsheets/d/1ikpJH6rVVHq8ES-IA8U4lkKH4XsTSpRyZewXwGTgits/edit?gid=1753191050#gid=1753191050
  * Benchmark ONNX models with XLA, ONNX Runtime (ORT), CPU and GPU
  * Very simple models
  * KnightsAnalytics/all-MiniLM-L6-v2
  * Slices (parts of) KnightsAnalytics/all-MiniLM-L6-v2
* Updated dependencies to GoMLX 0.16.1 with lots of accelerations.

# v0.1.4 - 2024/11/28

* Added Flatten op support.

# v0.1.3 - 2024/11/21

* Added ScopeToONNX (originally ContextToONNX) to save variables back to the ONNX model (in memory).
* Refactored internal/togomlx to inside onnx/ subdirectory.
* Added Model.Write and Model.SaveToFile.

# v0.1.2 - 2024/11/17

* Added LSTM op support, with a small example. 

# v0.1.1 - 2024/11/15

* Assume some variables are constant during constant-expression evaluation.
* Improved pretty-printing of attributes: include their values for small values.
* New ops: Range, Tile, CumSum, Not, Tanh, GatherElements, several standard unary and binary operators.
* Fixed ops: Where.

# v0.1.0

* First working version – for a few models.
* Constant-expression evaluation during a model build: needed for parameters that are fed dynamically 
  to ONNX but require static values in GoMLX/XLA.
