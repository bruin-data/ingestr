# Benchmarks

The first use case for onnx-gomlx (that prompted us to start the project) was to allow serving (inference) and
fine-tuning of sentence encoder models for KnightAnalytics using XLA.

So the benchmarks use that sentence-encoder model as reference. There are two variations:

1. Benchmarks the [_KnightsAnalytics/all-MiniLM-L6-v2_](https://huggingface.co/KnightsAnalytics/all-MiniLM-L6-v2) fine-tuned
   sentence encoder model with random sentences truncated to 128 tokens, sampled from 
   [HuggingFaceFW/fineweb](https://huggingface.co/datasets/HuggingFaceFW/fineweb). 
   See `internal/benchmarks/knights_sbert_test.go`.
2. Benchmarks the same [_KnightsAnalytics/all-MiniLM-L6-v2_](https://huggingface.co/KnightsAnalytics/all-MiniLM-L6-v2) 
   model with a small fixed list of titles (~13 tokens). This is the `internal/benchmarks/rob_sentences.go` test.

The benchmarks cover both GoMLX+XLA/PJRT execution and ORT (Microsoft ONNX Runtime), both in CPU and GPU versions. 
It also include throughput measure, if using parallelization. 

The benchmark include **full** model benchmarking (`Full` suffix) or partial model benchmarking -- this was used 
during development of **onnx-gomlx** to identify slowness.

## Glossary

* **ORT**: ONNX Runtime, the Microsoft runtime to execute ONNX models. Supports CPU and GPUs
* **XLA/PJRT**: A Google library to JIT-compile ML models and then execute them.  Supports CPU and various accelerators (GPU, ROCm, TPU,etc)

## Practical considerations

* If using Intel CPUs (or any heterogeneous CPU set up), make sure to exclude the slow cores (E-cores in intel).
  In Linux, with Intel 12K900, use for instance `sudo chcpu -d 16-23` or `taskset 0xFFFF ./benchmarks.test...`.
  See running benchmarks examples below.
* For tests that use ONNXRuntime you must set ORT_SO_PATH to point to your ORT `.so` file (either CPU or the CUDA one).
* There is lots of variance. Literally, even the weather may impact it: I suppose the CPU/GPU can be
  temperature-throttled differently.  

## Running benchmarks

The benchmark code is in `internal/benchmarks`, and the examples below assume you are in that subdirectory.

Notice the ORT `.so` files are installed in `~/.local/lib` for these examples.

You have to set the flag `--bench_duration=10s` (or some other amount of time). If you leave it at the default 0, 
it won't run any benchmark test. These are not Go's benchmarks, but rather built as tests.

Example of commands to run benchmarks in an Intel 12K900:

* Running KnightsAnalystics SBert with GoMLX/XLA + ORT CPU:

```
go test -c . && GOMLX_BACKEND=xla:cpu ORT_SO_PATH=/home/janpf/.local/lib/libonnxruntime.so taskset 0xFFFF ./benchmarks.test -test.run=BenchKnights.*Full -test.v --bench_duration=10s
```

* Running KnightsAnalystics SBert with ORT GPU:

```
go test -c . && GOMLX_BACKEND=xla:cuda ORT_SO_PATH=/home/janpf/.local/lib/libonnxruntime_gpu  .so taskset 0xFFFF ./benchmarks.test -test.run=BenchKnights.*Full -test.v --bench_duration=10s
```

* Experimenting with XLA_FLAGS:

```
$ go test -c . && GOMLX_BACKEND=xla:cpu XLA_FLAGS='--xla_cpu_enable_fast_math=true --xla_cpu_fast_math_honor_nans=false --xla_cpu_fast_math_honor_infs=false --xla_cpu_fast_math_honor_division=false --xla_cpu_fast_math_honor_functions=false --xla_cpu_enable_concurrency_optimized_scheduler=true' ORT_SO_PATH=/home/janpf/lib/libonnxruntime.so taskset 0xFFFF ./benchmarks.test -test.run=BenchKnights.*FullXLA -test.v --bench_duration=10s
```

* Running RobSentences benchmark -- which includes parallelization on CPU:

```
$ go test -c . && GOMLX_BACKEND=xla:cpu ORT_SO_PATH=/home/janpf/.local/lib/libonnxruntime.so ./benchmarks.test -test.run=BenchRob -test.v --bench_duration=10s
```

