package benchmarks

// Benchmark results for this file using:
//
// - GoMLX: v0.15.4rc / GoPJRT: 0.4.10rc
// - ONNX Runtime v1.20.1
// - Command used:
//	go test . -test.bench=.
//
// - cpu: 12th Gen Intel(R) Core(TM) i9-12900K: GOMAXPROC=24, 12 cores (4P, 8E), 24 hyperthread cores.
// - results: https://docs.google.com/spreadsheets/d/1ikpJH6rVVHq8ES-IA8U4lkKH4XsTSpRyZewXwGTgits/edit?gid=0#gid=0

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/chewxy/math32"
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/exceptions"
	"github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/support/testutil"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	"github.com/janpfeifer/must"
	ort "github.com/yalue/onnxruntime_go"
	"k8s.io/klog/v2"
)

func init() {
	klog.InitFlags(nil)
}

var (
	DefaultDeviceNum = compute.DeviceNum(0)

	TestShapes = []shapes.Shape{
		//shapes.Make(dtypes.Float32, 1, 1),
		//shapes.Make(dtypes.Float32, 10, 10),
		//shapes.Make(dtypes.Float32, 100, 100),
		shapes.Make(dtypes.Float32, 1000, 1000),
	}
	SmallTestPrograms = [][2]string{
		{"add1.onnx", "f(x)=x+1"},
		{"add1div2.onnx", "f(x)=(x+1)/2"},
		{"sqrt_add1div2.onnx", "f(x)=Sqrt((x+1)/2)"},
		{"mean_norm_sqrt_add1div2.onnx", "f(x)=Sqrt((x+1)/2) - ReduceMean(Sqrt((x+1)/2))"},
	}
	numPrograms = len(SmallTestPrograms)

	// testGoPrograms is a Go version of the SmallTestPrograms, as a per-element function.
	testGoPrograms = []goVectorFunc{
		parallelizeGoVectorFunc(func(inputs, outputs []float32) {
			for ii, v := range inputs {
				outputs[ii] = v + 1
			}
		}),
		parallelizeGoVectorFunc(func(inputs, outputs []float32) {
			for ii, v := range inputs {
				outputs[ii] = (v + 1) * 0.5
			}
		}),
		parallelizeGoVectorFunc(func(inputs, outputs []float32) {
			for ii, v := range inputs {
				outputs[ii] = math32.Sqrt((v + 1) * 0.5)
			}
		}),
	}

	benchmarkNameSuffix = "|GOMAXPROCS"
)

// goVectorFunc defines the signature for functions that process slices.
type goVectorFunc func(inputs, outputs []float32)

// sliceMap executes the given function sequentially for every element on in and returns a mapped slice.
func sliceMap[In, Out any](in []In, fn func(e In) Out) (out []Out) {
	out = make([]Out, len(in))
	for ii, e := range in {
		out[ii] = fn(e)
	}
	return
}

// parallelizeGoVectorFunc takes a goVectorFunc and parallelizes its execution if the input size is large enough.
func parallelizeGoVectorFunc(fn goVectorFunc) goVectorFunc {
	return func(inputs, outputs []float32) {
		numInputs := len(inputs)
		if numInputs < 100_000 { // Threshold for parallelization. Tune this value.
			fn(inputs, outputs)
			return
		}

		numCPU := runtime.NumCPU()
		chunkSize := numInputs / numCPU
		var wg sync.WaitGroup
		wg.Add(numCPU)
		for i := range numCPU {
			start := i * chunkSize
			end := (i + 1) * chunkSize
			if i == numCPU-1 {
				end = numInputs // Handle any remainder
			}
			go func(start, end int) {
				defer wg.Done()
				fn(inputs[start:end], outputs[start:end])
			}(start, end)
		}
		wg.Wait()
	}
}

// BenchmarkSmallXLAExec executes SmallTestPrograms on XLA using the normal Exec method.
// We try not to count the time for tensor transfers in and out.
func BenchmarkSmallXLAExec(b *testing.B) {
	// Check conversion.
	backend := testutil.BuildTestBackend()
	execs := make([]*graph.Exec, numPrograms)
	for progIdx, program := range SmallTestPrograms {
		model := must.M1(parser.ParseFile(program[0]))
		execs[progIdx] = graph.MustNewExec(backend, func(x *graph.Node) *graph.Node {
			g := x.Graph()
			outputs := model.CallGraph(nil, g, map[string]*graph.Node{"X": x})
			return outputs[0]
		})
	}

	// Pre-allocate tensors.
	numShapes := len(TestShapes)
	inputTensors := make([]*tensors.Tensor, numShapes)
	outputTensors := make([]*tensors.Tensor, numShapes)
	for shapeIdx, s := range TestShapes {
		inputTensors[shapeIdx] = tensors.FromShape(s)
		outputTensors[shapeIdx] = tensors.FromShape(s)
	}

	// Run tests for each shape/program combination.
	for shapeIdx, s := range TestShapes {
		for progIdx, program := range SmallTestPrograms {
			exec := execs[progIdx]
			b.Run(fmt.Sprintf("shape=%s/%s%s", s, program[1], benchmarkNameSuffix),
				func(b *testing.B) {
					// Set input to the value of v.
					x := inputTensors[shapeIdx]
					tensors.MutableFlatData[float32](x, func(flat []float32) {
						for ii := range flat {
							flat[ii] = float32(shapeIdx*numPrograms + progIdx + 1)
						}
					})

					for range 10 {
						tmpOutput := exec.MustExec1(x)
						tmpOutput.FinalizeAll()
					}

					b.ResetTimer()
					for b.Loop() {
						tmpOutput := exec.MustExec1(x)
						tmpOutput.FinalizeAll()
					}
				})
		}
	}
}

// BenchmarkSmallXLADirect benchmarks SmallTestPrograms using direct GoMLX execution.
// We try not to count the time for tensor transfers in and out.
func BenchmarkSmallXLADirect(b *testing.B) {
	// Create executables.
	backend := testutil.BuildTestBackend()
	numShapes := len(TestShapes)
	graphPerShapePerProgram := make([][]*graph.Graph, numShapes)
	inputTensors := make([]*tensors.Tensor, numShapes)
	outputTensors := make([]*tensors.Tensor, numShapes)
	for shapeIdx, s := range TestShapes {
		graphPerShapePerProgram[shapeIdx] = make([]*graph.Graph, numPrograms)
		for progIdx, program := range SmallTestPrograms {
			model := must.M1(parser.ParseFile(program[0]))
			g := graph.NewGraph(backend, fmt.Sprintf("Graph #%d", shapeIdx))
			x := graph.Parameter(g, "X", s)
			y := model.CallGraph(nil, g, map[string]*graph.Node{"X": x})[0]
			g.Compile(y)
			graphPerShapePerProgram[shapeIdx][progIdx] = g
			inputTensors[shapeIdx] = tensors.FromShape(s)
			outputTensors[shapeIdx] = tensors.FromShape(s)
		}
	}

	// Run tests for each shape/program combination.
	for shapeIdx, s := range TestShapes {
		for progIdx, program := range SmallTestPrograms {
			b.Run(fmt.Sprintf("shape=%s/%s%s", s, program[1], benchmarkNameSuffix),
				func(b *testing.B) {
					// Set input to the value of v.
					x := inputTensors[shapeIdx]
					tensors.MutableFlatData[float32](x, func(flat []float32) {
						for ii := range flat {
							flat[ii] = float32(shapeIdx*numPrograms + progIdx + 1)
						}
					})
					xBuf, err := x.Buffer(backend, DefaultDeviceNum)
					if err != nil {
						klog.Errorf("Failed to get buffer from tensor: %+v", err)
						b.FailNow()
					}
					g := graphPerShapePerProgram[shapeIdx][progIdx]

					// WarmUp:
					for range 10 {
						tmpOutput := g.RunWithBuffers([]compute.Buffer{xBuf}, []bool{false}, DefaultDeviceNum)[0]
						tmpOutput.FinalizeAll()
					}

					// Run test:
					b.ResetTimer()
					for b.Loop() {
						tmpOutput := g.RunWithBuffers([]compute.Buffer{xBuf}, []bool{false}, DefaultDeviceNum)[0]
						tmpOutput.FinalizeAll()
					}
				})
		}
	}
}

// BenchmarkSmallORT benchmarks the SmallTestPrograms using ONNX Runtime.
// We try not to count the time for tensor transfers in and out.
func BenchmarkSmallORT(b *testing.B) {
	ortPath := os.Getenv("ORT_SO_PATH")
	if ortPath == "" {
		exceptions.Panicf("Please set environment ORT_SO_PATH with the path to your ONNX Runtime dynamic linked library")
	}
	ort.SetSharedLibraryPath(ortPath)
	must.M(ort.InitializeEnvironment())
	defer func() { _ = ort.DestroyEnvironment() }()

	// Create a session for each tensor shape:
	numShapes := len(TestShapes)
	sessions := make([][]*ort.AdvancedSession, numShapes)
	inputsPerShape := make([]*ort.Tensor[float32], 0, numShapes)
	outputsPerShape := make([]*ort.Tensor[float32], 0, numShapes)
	for shapeIdx, s := range TestShapes {
		inputData := make([]float32, s.Size())
		dims64 := sliceMap(s.Dimensions, func(dim int) int64 { return int64(dim) })
		inputShape := ort.NewShape(dims64...)
		inputTensor := must.M1(ort.NewTensor(inputShape, inputData))
		inputsPerShape = append(inputsPerShape, inputTensor)
		outputTensor := must.M1(ort.NewEmptyTensor[float32](inputShape))
		outputsPerShape = append(outputsPerShape, outputTensor)

		sessions[shapeIdx] = make([]*ort.AdvancedSession, numPrograms)
		for progIdx, program := range SmallTestPrograms {
			sessions[shapeIdx][progIdx] = must.M1(ort.NewAdvancedSession(
				program[0],
				[]string{"X"},
				[]string{"Y"},
				[]ort.Value{inputTensor},
				[]ort.Value{outputTensor},
				nil))
		}
	}

	// Run tests for each shape/program combination.
	for shapeIdx, s := range TestShapes {
		for progIdx, program := range SmallTestPrograms {
			b.Run(fmt.Sprintf("shape=%s/%s%s", s, program[1], benchmarkNameSuffix),
				func(b *testing.B) {
					// Set input to the value of v.
					input := inputsPerShape[shapeIdx]
					data := input.GetData()
					for ii := range data {
						data[ii] = float32(shapeIdx*numPrograms + progIdx + 1)
					}
					session := sessions[shapeIdx][progIdx]

					// WarmUp:
					for range 10 {
						must.M(session.Run())
					}

					b.ResetTimer()
					for b.Loop() {
						must.M(session.Run())
					}
				})
		}
	}
}

func BenchmarkPureGo(b *testing.B) {
	// Pre-allocate tensors.
	numShapes := len(TestShapes)
	inputTensors := make([]*tensors.Tensor, numShapes)
	outputTensors := make([]*tensors.Tensor, numShapes)
	for shapeIdx, s := range TestShapes {
		inputTensors[shapeIdx] = tensors.FromShape(s)
		outputTensors[shapeIdx] = tensors.FromShape(s)
	}

	// Run tests for each shape/program combination.
	for shapeIdx, s := range TestShapes {
		x := inputTensors[shapeIdx]
		y := outputTensors[shapeIdx]
		for progIdx, program := range SmallTestPrograms {
			b.Run(fmt.Sprintf("shape=%s/%s%s", s, program[1], benchmarkNameSuffix),
				func(b *testing.B) {
					// Set value:
					tensors.MutableFlatData[float32](x, func(flat []float32) {
						for ii := range flat {
							flat[ii] = float32(shapeIdx*numPrograms + progIdx + 1)
						}
					})
					testProgram := testGoPrograms[progIdx]

					// Warm-up:
					for range 10 {
						tensors.ConstFlatData[float32](x, func(inputs []float32) {
							tensors.MutableFlatData[float32](y, func(outputs []float32) {
								testProgram(inputs, outputs)
							})
						})
					}

					// Benchmark
					b.ResetTimer()
					for b.Loop() {
						tensors.ConstFlatData[float32](x, func(inputs []float32) {
							tensors.MutableFlatData[float32](y, func(outputs []float32) {
								testProgram(inputs, outputs)
							})
						})
					}
				})
		}
	}
}
