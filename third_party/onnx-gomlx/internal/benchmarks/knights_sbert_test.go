package benchmarks

import (
	"flag"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	dtok "github.com/daulet/tokenizers"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/exceptions"
	"github.com/gomlx/go-huggingface/hub"
	"github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/gomlx/support/testutil"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	"github.com/janpfeifer/go-benchmarks"
	"github.com/janpfeifer/must"
	"github.com/parquet-go/parquet-go"
	ort "github.com/yalue/onnxruntime_go"
	"google.golang.org/protobuf/proto"
)

var (
	// HuggingFace authentication token read from the environment.
	// It can be created in https://huggingface.co
	// Some files may require it for downloading.
	hfAuthToken = os.Getenv("HF_TOKEN")

	KnightsAnalyticsSBertID = "KnightsAnalytics/all-MiniLM-L6-v2"
	FineWebID               = "HuggingFaceFW/fineweb"
	FineWebSampleFile       = "sample/10BT/000_00000.parquet"

	// Benchmark hyperparameters.
	BatchSizes     = []int{1, 16, 32, 64} // {1, 16, 64}
	SequenceLength = 128                  // Shouldn't be changed, since the tokenizer is hard-coded to pad to 128.
	NumSentences   = 128                  // 10_000

	flagBenchDuration = flag.Duration("bench_duration", 0, "Benchmark duration, typically use 10 seconds. If left as 0, benchmark tests are disabled")
	flagPrintXLAGraph = flag.Bool("xla_graph", false, "Prints XLA graph")
	flagExcludePadded = flag.Bool("exclude_padded", false, "Exclude sentences with less than 128 tokens")
	flagVerbose       = flag.Bool("verbose", false, "Prints verbose information")

	// Save embeddings to a file, one per example, named /tmp/embeddings_%03d.bin
	flagSaveEmbeddings  = flag.Bool("save_embeddings", false, "Save embeddings to file, one per example, named embeddings-%03d.bin")
	flagCheckEmbeddings = flag.Bool("check_embeddings", false, "Check embeddings generated match the ones loaded from files")
)

// tokenizedSentence stores the tokenized input for models of a sentence.
type tokenizedSentence struct {
	Encoding [3][]int64 // IDs, Masks, tokenTypeIDs
}

// fineWebEntry: inspection of fields in a parquet file done with tool in
// github.com/xitongsys/parquet-go/tool/parquet-tools.
//
// The parquet annotations are described in: https://pkg.go.dev/github.com/parquet-go/parquet-go#SchemaOf
type fineWebEntry struct {
	Text  string  `parquet:"text,snappy"`
	ID    string  `parquet:"id,snappy"`
	Dump  string  `parquet:"dump,snappy"`
	URL   string  `parquet:"url,snappy"`
	Score float64 `parquet:"language_score"`
}

// trimString returns s trimmed to at most maxLength runes. If trimmed, it appends "…" at the end.
func trimString(s string, maxLength int) string {
	if utf8.RuneCountInString(s) <= maxLength {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLength-1]) + "…"
}

func padOrTrim[T any](n int, values []T, padding T) []T {
	if len(values) >= n {
		return values[:n]
	}
	newValues := make([]T, n)
	copy(newValues, values)
	for ii := len(values); ii < n; ii++ {
		newValues[ii] = padding
	}
	return newValues
}

// sampleFineWeb returns the first n tokenized sentences from a 2Gb sample of the FineWeb dataset.
//
// The modelID is used to download the tokenization model.
//
// sequenceLen is the length of each sentence in number of tokens.
// If the original sentence is longer, it is truncated.
// If it is shorter, it is padded.
func sampleFineWeb(modelID string, n, sequenceLen int) []tokenizedSentence {
	results := make([]tokenizedSentence, n)

	// Download repo file.
	repo := hub.New(FineWebID).WithType(hub.RepoTypeDataset).WithAuth(hfAuthToken)
	localSampleFile := must.M1(repo.DownloadFile(FineWebSampleFile))

	// Parquet reading using parquet-go: it's somewhat cumbersome (to open the file, it needs its size!?), but it works.
	schema := parquet.SchemaOf(&fineWebEntry{})
	fSize := must.M1(os.Stat(localSampleFile)).Size()
	fReader := must.M1(os.Open(localSampleFile))
	fParquet := must.M1(parquet.OpenFile(fReader, fSize))
	reader := parquet.NewGenericReader[fineWebEntry](fParquet, schema)
	defer func() { _ = reader.Close() }()

	// Create tokenizer: it is configured by the "tokenizer.json" to a max_length of 128, with padding.
	repoTokenizer := hub.New(modelID).WithAuth(hfAuthToken)
	localFile := must.M1(repoTokenizer.DownloadFile("tokenizer.json"))
	tokenizer := must.M1(dtok.FromFile(localFile))
	defer func() { _ = tokenizer.Close() }()

	// Read a batch at a time and tokenize.
	const maxBatchSize = 32
	current := 0
	for current < n {
		batchSize := min(maxBatchSize, n-current)
		rows := make([]fineWebEntry, batchSize)
		numRead := must.M1(reader.Read(rows))
		if numRead == 0 {
			break
		}
		for _, row := range rows {
			encoding := tokenizer.EncodeWithOptions(row.Text, false,
				dtok.WithReturnTypeIDs(),
				dtok.WithReturnAttentionMask(),
			)

			if *flagExcludePadded {
				var countMasked int
				for _, id := range encoding.AttentionMask {
					if id == 0 {
						countMasked++
					}
				}
				if countMasked > 0 {
					continue
				}
			}

			results[current].Encoding[0] = padOrTrim(sequenceLen,
				sliceMap(encoding.IDs, func(id uint32) int64 { return int64(id) }),
				0)
			results[current].Encoding[1] = padOrTrim(sequenceLen,
				sliceMap(encoding.AttentionMask, func(id uint32) int64 { return int64(id) }),
				0)
			results[current].Encoding[2] = padOrTrim(sequenceLen,
				sliceMap(encoding.TypeIDs, func(id uint32) int64 { return int64(id) }),
				0)
			current++
		}
	}
	if current < n {
		exceptions.Panicf("requested %d sentences to sample, got only %d", n, current)
	}
	return results
}

var (
	tokenizedExamples     []tokenizedSentence
	tokenizedExamplesOnce sync.Once
)

func initTokenizedExamples() {
	tokenizedExamplesOnce.Do(func() {
		fmt.Printf("Tokenizing %d sentences of length %d...\n", NumSentences, SequenceLength)
		tokenizedExamples = sampleFineWeb(KnightsAnalyticsSBertID, NumSentences, SequenceLength)
		fmt.Printf("\tfinished tokenizing.\n")
	})
}

func benchmarkONNXModelWithXLA(withHeader bool, name, onnxModelPath string, batchSize int,
	targetNodeNames ...string) {
	initTokenizedExamples()
	if NumSentences < batchSize {
		exceptions.Panicf("batchSize(%d) must be <= to the number of sentences sampled (%d)", batchSize, NumSentences)
	}

	// Build model
	backend := testutil.BuildTestBackend()
	onnxModel := must.M1(parser.ParseFile(onnxModelPath))
	store := model.NewStore()
	must.M(onnxModel.VariablesToScope(store.RootScope()))
	exec := model.MustNewExec(backend, store, func(scope *model.Scope, tokenIDs, attentionMask, tokenTypeIDs *graph.Node) *graph.Node {
		//fmt.Printf("Exec inputs (tokens, mask, types): %s, %s, %s\n", tokenIDs.Shape(), attentionMask.Shape(), tokenTypeIDs.Shape())
		g := tokenIDs.Graph()
		outputs := onnxModel.CallGraph(scope, g,
			map[string]*graph.Node{
				"input_ids":      tokenIDs,
				"attention_mask": attentionMask,
				"token_type_ids": tokenTypeIDs,
			}, targetNodeNames...)
		if *flagPrintXLAGraph {
			fmt.Printf("Graph:\n%s\n", g)
		}
		return outputs[0]
	})
	defer exec.Finalize()

	// Create input tensors:
	var inputTensors [3]*tensors.Tensor // tokenIDs, attentionMask, tokenTypeIDs
	for ii := range inputTensors {
		inputTensors[ii] = tensors.FromShape(shapes.Make(dtypes.Int64, batchSize, SequenceLength))
	}

	runIdx := 0
	sentenceIdx := 0
	testFn := benchmarks.NamedFunction{
		Name: fmt.Sprintf("XLA/%s/BatchSize=%2d:", name, batchSize),
		Func: func() {
			// Create the batch for each input tensor.
			for inputIdx, t := range inputTensors {
				tensors.MutableFlatData[int64](t, func(flat []int64) {
					for exampleIdx := range batchSize {
						sample := tokenizedExamples[sentenceIdx+exampleIdx]
						copy(flat[exampleIdx*SequenceLength:], sample.Encoding[inputIdx])
					}
				})
			}

			// Execute program.
			//start := time.Now()
			output := exec.MustCall(inputTensors[0], inputTensors[1], inputTensors[2])[0]
			tensors.ConstFlatData(output, func(flat []float32) {
				if runIdx == 0 {
					fmt.Printf("\t> Last value of result: %v\n", flat[len(flat)-1])
				}
			})
			output.FinalizeAll()

			//elapsed := time.Since(start)
			//if elapsed > 200*time.Microsecond {
			//	fmt.Printf("runIdx=%d, sentenceIdx=%d: elapsed=%s\n", runIdx, sentenceIdx, elapsed)
			//}

			// Next batch.
			runIdx++
			sentenceIdx += batchSize
			if sentenceIdx+batchSize >= NumSentences {
				sentenceIdx = 0
			}
		},
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	numWarmUps := 2 * (len(tokenizedExamples) + batchSize - 1) / batchSize
	benchmarks.New(testFn).
		WithWarmUps(numWarmUps).
		WithDuration(*flagBenchDuration).
		WithHeader(withHeader).
		Done()
}

// ortInitFn will execute only once.
var (
	ortInitFn = sync.OnceFunc(func() {
		ortPath := os.Getenv("ORT_SO_PATH")
		if ortPath == "" {
			exceptions.Panicf("Please set environment ORT_SO_PATH with the path to your ONNX Runtime dynamic linked library")
		}
		if strings.Index(ortPath, "gpu") != -1 {
			ortIsCUDA = true
		}
		ort.SetSharedLibraryPath(ortPath)
		must.M(ort.InitializeEnvironment())
		// Since we may run this function multiple times, we never destroy the environment.
		//defer func() { _ = ort.DestroyEnvironment() }()
	})
	ortIsCUDA bool
)

func benchmarkONNXModelWithORT(withHeader bool,
	name, onnxModelPath string, batchSize int,
	outputNodeName string, outputNodeShape shapes.Shape) {
	ortInitFn()

	// Tokenize examples from FineWeb (or from testSentences)
	initTokenizedExamples()
	if NumSentences < batchSize {
		exceptions.Panicf("batchSize(%d) must be >= to the number of sentences sampled (%d)", batchSize, NumSentences)
	}

	// Create input and output tensors.
	var inputTensors [3]*ort.Tensor[int64]
	inputShape := ort.NewShape(int64(batchSize), int64(SequenceLength))
	for ii := range inputTensors {
		inputTensors[ii] = must.M1(ort.NewEmptyTensor[int64](inputShape))
	}
	outputShape := ort.NewShape(sliceMap(outputNodeShape.Dimensions, func(dim int) int64 { return int64(dim) })...)
	outputTensor := must.M1(ort.NewEmptyTensor[float32](outputShape))

	// Create session with ONNX program.
	var options *ort.SessionOptions
	if ortIsCUDA {
		options = must.M1(ort.NewSessionOptions())
		cudaOptions := must.M1(ort.NewCUDAProviderOptions())
		// must.M(cudaOptions.Update(map[string]string{"device_id": "0"}))
		must.M(options.AppendExecutionProviderCUDA(cudaOptions))
	}
	session := must.M1(ort.NewAdvancedSession(
		onnxModelPath,
		[]string{"input_ids", "attention_mask", "token_type_ids"},
		[]string{outputNodeName},
		sliceMap(inputTensors[:], func(t *ort.Tensor[int64]) ort.Value { return t }),
		[]ort.Value{outputTensor}, options))
	defer func() { must.M(session.Destroy()) }()

	sentenceIdx := 0
	runIdx := 0
	testFn := benchmarks.NamedFunction{
		Name: fmt.Sprintf("ORT/%s/BatchSize=%2d:", name, batchSize),
		Func: func() {
			// Create a batch for each input tensor.
			for inputIdx, t := range inputTensors {
				flat := t.GetData()
				for batchIdx := range batchSize {
					sample := tokenizedExamples[sentenceIdx+batchIdx]
					copy(flat[batchIdx*SequenceLength:], sample.Encoding[inputIdx])
				}
			}

			// Execute program.
			must.M(session.Run())

			flat := outputTensor.GetData()
			if runIdx == 0 {
				fmt.Printf("\t> Last value of result: %v\n", flat[len(flat)-1])
			}

			// Next batch.
			sentenceIdx += batchSize
			if sentenceIdx+batchSize >= NumSentences {
				sentenceIdx = 0
			}
			runIdx++
		},
	}

	benchmarks.New(testFn).
		WithWarmUps(10).
		WithDuration(*flagBenchDuration).
		WithHeader(withHeader).
		Done()
}

func TestBenchKnightsSBertFullORT(t *testing.T) {
	if testing.Short() || *flagBenchDuration == 0 {
		t.SkipNow()
	}
	repo := hub.New(KnightsAnalyticsSBertID).WithAuth(hfAuthToken)
	onnxModelPath := must.M1(repo.DownloadFile("model.onnx"))
	for ii, batchSize := range BatchSizes {
		benchmarkONNXModelWithORT(ii == 0, "Full", onnxModelPath, batchSize,
			"last_hidden_state", shapes.Make(dtypes.Float32, batchSize, SequenceLength, 384))
	}
}

func TestBenchKnightsSBertFullXLA(t *testing.T) {
	if testing.Short() || *flagBenchDuration == 0 {
		t.SkipNow()
	}
	repo := hub.New(KnightsAnalyticsSBertID).WithAuth(hfAuthToken)
	onnxModelPath := must.M1(repo.DownloadFile("model.onnx"))
	for _, batchSize := range BatchSizes {
		benchmarkONNXModelWithXLA(false, "Full", onnxModelPath, batchSize)
	}
}

func recursivelyTagNode(allNodes, usedNodes map[string]*protos.NodeProto, outputName string) {
	if _, found := usedNodes[outputName]; found {
		return
	}
	node := allNodes[outputName]
	if node == nil {
		// Likely node is a variable or an input, simply ignore.
		return
	}
	usedNodes[outputName] = node
	for _, inputNode := range node.Input {
		recursivelyTagNode(allNodes, usedNodes, inputNode)
	}
}

// saveONNXModelWithOutput reads an ONNX model from fromPath, changes its output to
// the node named newOutputNode, and then saves the modified model to toPath.
func saveONNXModelWithOutput(fromPath, toPath, newOutputNode string) (shapePerBatchSize map[int]shapes.Shape) {
	onnxModel := must.M1(onnxgomlx.ReadFile(fromPath))

	// Find the output shape for each batchSize.
	shapePerBatchSize = make(map[int]shapes.Shape, len(BatchSizes))
	backend := testutil.BuildTestBackend()
	store := model.NewStore()
	must.M(onnxModel.VariablesToScope(store.RootScope()))
	for _, batchSize := range BatchSizes {
		g := graph.NewGraph(backend, fmt.Sprintf("batchSize=%d", batchSize))
		var inputs [3]*graph.Node
		inputsNames := []string{"token_ids", "attention_mask", "token_type_ids"}
		for ii := range inputs {
			inputs[ii] = graph.Parameter(g, inputsNames[ii], shapes.Make(dtypes.Int64, batchSize, SequenceLength))
		}
		output := onnxModel.CallGraph(store.RootScope(), g,
			map[string]*graph.Node{
				"input_ids":      inputs[0],
				"attention_mask": inputs[1],
				"token_type_ids": inputs[2],
			}, newOutputNode)[0]
		shapePerBatchSize[batchSize] = output.Shape().Clone()
		g.Finalize()
		fmt.Printf("\tbatch size %d: shape %s\n", batchSize, output.Shape())
	}

	// Change output in model proto.
	graphProto := onnxModel.Proto.Graph
	newOutput := &protos.ValueInfoProto{
		Name: newOutputNode,
	}
	graphProto.Output = []*protos.ValueInfoProto{newOutput}

	// Mark nodes that are needed to generate the target output node.
	allNodes := make(map[string]*protos.NodeProto, 2*len(graphProto.Node))
	for _, node := range graphProto.Node {
		for _, outputName := range node.Output {
			allNodes[outputName] = node
		}
	}
	usedNodes := make(map[string]*protos.NodeProto, len(allNodes))
	recursivelyTagNode(allNodes, usedNodes, newOutputNode)
	fmt.Printf("\t%d nodes kept out of %d.\n", len(usedNodes), len(graphProto.Node))
	graphProto.Node = make([]*protos.NodeProto, 0, len(usedNodes))
	for _, node := range usedNodes {
		graphProto.Node = append(graphProto.Node, node)
	}

	// Save model
	contents := must.M1(proto.Marshal(&onnxModel.Proto))
	must.M(os.WriteFile(toPath, contents, 0644))

	return
}

// ModelSlicesOutputs points to intermediary outputs in the KnightsAnalytics/all-MiniLM-L6-v2 model.
var ModelSlicesOutputs = [][2]string{
	// Format: <output node name>, <short name>
	//{"/embeddings/Add_output_0", "embeddingGather"},
	//{"/embeddings/LayerNorm/Add_1_output_0", "embeddingsLayerNorm"},

	//{"/embeddings/LayerNorm/ReduceMean_output_0", "ReduceMean0"},
	//{"/embeddings/LayerNorm/Sub_output_0", "LayerNorm0Shifted"},

	//{"/embeddings/LayerNorm/Pow_output_0", "LayerNorm0Squares"},
	//{"/embeddings/LayerNorm/ReduceMean_1_output_0", "LayerNorm0SquaresMean"},
	//{"/embeddings/LayerNorm/Add_output_0", "LayerNorm0SquaresMeanEpsilon"},
	//{"/embeddings/LayerNorm/Sqrt_output_0", "layerNorm0Scale"},
	//{"/embeddings/LayerNorm/Div_output_0", "layerNorm0ScaleNormalized"},
	//{"/embeddings/LayerNorm/Mul_output_0", "layerNorm0Scaled"},

	//{"/embeddings/LayerNorm/Add_1_output_0", "attentionLayer0.PreValueMul"},
	{"/encoder/layer.0/attention/self/value/MatMul_output_0", "attentionLayer0.ValueMul"},

	//{"/encoder/layer.0/attention/self/Reshape_3_output_0", "attentionLayer0"},
	//{"/encoder/layer.0/attention/output/Add_output_0", "attentionLayer0"},

	//{"/encoder/layer.1/attention/self/Reshape_3_output_0", "attentionLayer1"},
}

func TestBenchKnightsSBertSliceXLA(t *testing.T) {
	if testing.Short() || *flagBenchDuration == 0 {
		t.SkipNow()
	}
	repo := hub.New(KnightsAnalyticsSBertID).WithAuth(hfAuthToken)
	onnxModelPath := must.M1(repo.DownloadFile("model.onnx"))
	for _, modelSlice := range ModelSlicesOutputs {
		name := modelSlice[1]
		outputNodeName := modelSlice[0]
		for ii, batchSize := range BatchSizes {
			displayHeader := ii == 0
			benchmarkONNXModelWithXLA(displayHeader, name, onnxModelPath, batchSize, outputNodeName)
		}
	}
}

func TestBenchKnightsSBertSliceORT(t *testing.T) {
	if testing.Short() || *flagBenchDuration == 0 {
		t.SkipNow()
	}
	repo := hub.New(KnightsAnalyticsSBertID).WithAuth(hfAuthToken)
	onnxModelPath := must.M1(repo.DownloadFile("model.onnx"))

	for _, modelSlice := range ModelSlicesOutputs {
		name := modelSlice[1]
		outputNodeName := modelSlice[0]
		editedModelPath := path.Join(t.TempDir(), name) + ".onnx"
		shapesPerBatchSize := saveONNXModelWithOutput(onnxModelPath, editedModelPath, outputNodeName)
		_ = shapesPerBatchSize
		for ii, batchSize := range BatchSizes {
			displayHeader := ii == 0
			benchmarkONNXModelWithORT(displayHeader, name, editedModelPath, batchSize,
				outputNodeName, shapesPerBatchSize[batchSize])
		}
	}
}
