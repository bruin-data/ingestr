[![Documentation](https://img.shields.io/badge/docs-gomlx.github.io-blue.svg)](https://gomlx.github.io/)
[![Sponsor GoMLX](https://img.shields.io/badge/Sponsor-GoMLX-white?logo=github&style=flat-square)](https://github.com/gomlx/gomlx/blob/main/README.md#-support-the-project)


# Compute Backends API

Package `compute` provides a modular API for defining and executing multidimensional computation graphs with pluggable backends.

It defines `shapes` (tensor shapes) and `dtypes` (data types) and the top-level `compute` package defines a `Backend` API (a series of interfaces), that
can be used to define a computation graph, JIT-compile it, transfer buffers (raw values) to/from the backend, and execute compiled computations.

It powers [GoMLX](https://github.com/gomlx/gomlx), the machine learning framework for Go, but can be used directly also. With the caveat that the `compute.Backend` doesn't aim to be ergonomic, but instead "correct" and "minimal". For a more convenient API for complex computation, and auto-differentiation, use GoMLX instead.

## Available Backends

The `compute.Backend` API is currently implemented by:

- Package `gobackend`: a native Go implementation, hence very portable
  (including it runs in WASM). It covers 80% of the API (some ops are still
  missing). We are working on SIMD versions: AVX512 and AVX2 using
  `simd/archsimd` for now, only for _matmul_ (with huge performance gains).
- Package
  [`compute/xla`](https://github.com/gomlx/go-xla/tree/main/compute/xla): an
  [XLA (PJRT)](https://openxla.org/) based implementation, the same used by Jax
  and TensorFlow. It uses CGO (it's a C++ library), but it supports GPUs and
  TPUs, as well as a fast CPU, proper JIT compilation. Limited to static shapes
  though.
- The project [go-darwinml](https://github.com/gomlx/go-darwinml/) is an
  **experimental** support to Apple's CoreML, with accelerate for GPU (Metal)
  and CPU (arm64).

## Using the `compute.Backend` interface


## Roadmap

**Short term:**

- [x] Add basic dynamic shapes support. (experimental, see `./docs/DynamicShapes.md`)
- [x] Add initial SIMD implementation for the Go backend (AVX512 and AVX2, using plain `simd/archsimd`).

**Future:**

We are exploring support (Backend implementations) for:

* Integrate more SIMD using go-highway for the Go backend.
* ONNX Runtime: dynamically generate an ONNX proto and use ORT to execute it;
* [llama.cpp](https://github.com/ggml-org/llama.cpp): using [github.com/hybridgroup/yzma](https://github.com/hybridgroup/yzma) a "pure-go" binding;
* [WebNN](https://learn.microsoft.com/en-us/windows/ai/directml/webnn-overview) or WebGL.

## Implementing your own backend

It's conceptually simple:

- Inherit from `notimplemented`, and return an empty capabilities.
- Implement the transferring of buffers to/from your backend.
- Implement the operations that you need.
- Make sure you pass the "compliance" tests in `support/backendtest`, by calling
  the function `backendtest.RunAll(t *testing.T, b compute.Backend)`, or running
  the individual tests. Example:

```
func TestCompliance(t *testing.T) {
	backendtest.RunAll(t, myBackend)
}
```

Consider using GoMLX tests against your Backend to test that they are working --
just set the environment variable `GOMLX_BACKEND` to your new backend, and you
can run arbitrary tests. Also, once you have enough ops implemented, you can use
some of the example models to benchmark your backend against some of the others.

## Environment Variables

- `GOMLX_BACKEND`: defines the backend engine to use (if using `backends.New()`). The value is formatted as "<backend_name>[:<backend_config>]",
  with the config part being optional. Examples:
  - `GOMLX_BACKEND=go`: Use the "Go backend", the pure Go implementation that is very portable but slow.
  - `GOMLX_BACKEND="xla:cpu"`: Use XLA (the faster backend, only runs on Linux now) for CPU
  - `GOMLX_BACKEND="xla:cuda"`: Use XLA for for Nvidia CUDA
  - `GOMLX_BACKEND="xla:/path/to/my/pjrt_plugin.so"`: Use XLA with an arbitrary PJRT. PJRT is a plugin system for XLA to support different hardware.
    One can install PJRTs build for NVIDIA GPUs (there is an installation script for that), there is also one for ROCm (not tested by the author),
    for TPU (Google Cloud) and reports of PJRTs being built to even new accelerators (e.g.: [TensTorrent XLA](https://github.com/tenstorrent/tt-xla))
- For the native Go backend:
  - `GOMLX_SIMD_AVX512`: set to `0` or `false` to disable AVX512 SIMD implementation in the native Go backend. The default is enabled if AVX512 is present.
  - `GOMLX_SIMD_AVX2`: set to `0` or `false` to disable AVX2 SIMD implementation in the native Go backend. The default is enabled if AVX2 is present.
  - `GOMLX_FUSION`: if set to `0`, `false` to disable fused operations in the native Go backend. The default is enabled.
- For the [XLA backend](https://github.com/gomlx/go-xla/tree/main/compute/xla)
  - `PJRT_PLUGIN_LIBRARY_PATH`: the underlying XLA backend uses this variable as an extra directory to search for plugin locations.
    It searches for the systems library paths (`$LD_LIBRARY_PATH`, `/etc/ld.so.conf`), the default `/usr/local/lib/gomlx/pjrt` and `$PJRT_PLUGIN_LIBRARY_PATH` if set.
  - `GOMLX_NO_AUTO_INSTALL`: if set to `1`, GoMLX will not automatically install PJRTs when running on a system without them.
  - `XLA_FLAGS`: optional controls for XLA backend. It should be set to a semicolon (";") separated list of options. If you set to `--help` 
    the backend will print out some help for all options. There is also a description on the page [XLA Flags Guidance](https://openxla.org/xla/flags_guidance).

## WebAssembly (WASM) Support

The "go" backend is well supported in WASM environment.

It passes all compliance tests:

```go
GOOS=js GOARCH=wasm go test  -exec wasmbrowsertest ./... -count=1 -v
```
