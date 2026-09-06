package benchmarks

import (
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/go-huggingface/hub"
	. "github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/gomlx/support/testutil"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	"github.com/janpfeifer/go-benchmarks"
	"github.com/janpfeifer/must"
	ort "github.com/yalue/onnxruntime_go"
)

func TestBenchInceptionV3(t *testing.T) {
	if testing.Short() {
		fmt.Printf("Skipping InceptionV3 benchmark test: --short is set\n")
		t.SkipNow()
	}
	if *flagBenchDuration == 0 {
		fmt.Printf("Skipping InceptionV3 benchmark test: --bench_duration is not set\n")
		t.SkipNow()
	}
	t.Run("ONNX-GoMLX", benchGoMLXInceptionV3)
	t.Run("ONNX-ORT", benchORTInceptionV3)
}

var (
	inceptionV3RepoID        = "recursionerr/nsfw_01"
	inceptionV3ModelFileName = "inception_v3.onnx"
	inceptionV3BatchSizes    = []int{1, 16, 32, 64} // {1, 16, 64}
)

func downloadInceptionV3Model() string {
	fmt.Printf("HuggingFace repository:  %s\n", inceptionV3RepoID)
	repo := hub.New(inceptionV3RepoID)
	if !repo.HasFile(inceptionV3ModelFileName) {
		fmt.Printf("Could not find %q for repo %q", inceptionV3ModelFileName, repo)
	}
	fmt.Printf("HuggingFace file:        %s\n", inceptionV3ModelFileName)
	onnxModelPath := must.M1(repo.DownloadFile(inceptionV3ModelFileName))
	fmt.Printf("Locally downloaded file: %s\n", onnxModelPath)

	// Calculate and print sha256 hash of the model file
	fileContent := must.M1(os.ReadFile(onnxModelPath))
	hash := sha256.Sum256(fileContent)
	fmt.Printf("File SHA256:             %x\n", hash)
	return onnxModelPath
}

func benchGoMLXInceptionV3(t *testing.T) {
	onnxModelPath := downloadInceptionV3Model()
	onnxModel := must.M1(parser.ParseFile(onnxModelPath))
	if *flagVerbose {
		fmt.Printf("Model details:\n%s\n", onnxModel)
	}
	backend := testutil.BuildTestBackend()
	store := model.NewStore()
	must.M(onnxModel.VariablesToScope(store.RootScope()))
	inputsNames, _ := onnxModel.Inputs()
	inputName := inputsNames[0]
	outputsNames, _ := onnxModel.Outputs()
	outputName := outputsNames[0]
	for batchIdx, batchSize := range inceptionV3BatchSizes {
		//t.Run(fmt.Sprintf("batchSize=%02d", batchSize), func(t *testing.T) {
		exec := model.MustNewExec(backend, store, func(scope *model.Scope, images *Node) *Node {
			g := images.Graph()
			outputs := onnxModel.CallGraph(scope, g,
				map[string]*Node{
					inputName: images,
				}, outputName)
			if *flagPrintXLAGraph {
				fmt.Printf("Graph:\n%s\n", g)
			}
			return outputs[0]
		})

		// Create random images
		r := rand.New(rand.NewPCG(42, 0))
		inputImages := tensors.FromShape(shapes.Make(dtypes.Float32, batchSize, 299, 299, 3))
		tensors.MutableFlatData[float32](inputImages, func(flat []float32) {
			for i := range flat {
				flat[i] = r.Float32()
			}
		})

		benchFn := benchmarks.NamedFunction{
			Name: fmt.Sprintf("%s/batchSize=%02d", t.Name(), batchSize),
			Func: func() {
				output, err := exec.Call1(inputImages)
				if err != nil {
					t.Fatalf("InceptionV3 execution failed: %+v", err)
				}
				// Force transfer to local memory: this should be part of the cost.
				tensors.ConstFlatData(output, func(flat []float32) {
					_ = flat[0]
				})
				output.FinalizeAll()
			},
		}

		runtime.LockOSThread()
		benchmarks.New(benchFn).
			WithWarmUps(128).
			WithDuration(*flagBenchDuration).
			WithHeader(batchIdx == 0).
			Done()
		runtime.UnlockOSThread()
		exec.Finalize()
	}
}

func benchORTInceptionV3(t *testing.T) {
	onnxModelPath := downloadInceptionV3Model()
	ortInitFn()
	var options *ort.SessionOptions
	if ortIsCUDA {
		options = must.M1(ort.NewSessionOptions())
		cudaOptions := must.M1(ort.NewCUDAProviderOptions())
		// must.M(cudaOptions.Update(map[string]string{"device_id": "0"}))
		must.M(options.AppendExecutionProviderCUDA(cudaOptions))
	}
	session := must.M1(ort.NewDynamicAdvancedSession(
		onnxModelPath,
		[]string{"input_1:0"}, []string{"Identity:0"},
		options))
	defer func() {
		err := session.Destroy()
		if err != nil {
			fmt.Printf("Error destroying session: %v\n", err)
		}
	}()

	for batchIdx, batchSize := range inceptionV3BatchSizes {
		inputShape := ort.NewShape(int64(batchSize), int64(299), int64(299), int64(3))
		images := must.M1(ort.NewEmptyTensor[float32](inputShape))
		r := rand.New(rand.NewPCG(42, 0))
		{
			flat := images.GetData()
			for i := range flat {
				flat[i] = r.Float32()
			}
		}
		outputShape := ort.NewShape(int64(batchSize), int64(5))
		outputTensor := must.M1(ort.NewEmptyTensor[float32](outputShape))

		benchFn := benchmarks.NamedFunction{
			Name: fmt.Sprintf("%s/batchSize=%02d", t.Name(), batchSize),
			Func: func() {
				must.M(session.Run(
					[]ort.Value{images},
					[]ort.Value{outputTensor},
				))
				{
					// Force transfer to local memory: this should be part of the cost.
					flat := outputTensor.GetData()
					_ = flat[0]
				}
			},
		}
		runtime.LockOSThread()
		benchmarks.New(benchFn).
			WithWarmUps(128).
			WithDuration(*flagBenchDuration).
			WithHeader(batchIdx == 0).
			Done()
		runtime.UnlockOSThread()
	}
}
