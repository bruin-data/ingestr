# ONNX-GoMLX: Inference and fine-tuning ONNX models with GoMLX

[![GoDev](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/gomlx/onnx-gomlx?tab=doc)
[![Slack](https://img.shields.io/badge/Slack-GoMLX-purple.svg?logo=slack)](https://app.slack.com/client/T029RQSE6/C08TX33BX6U)

## 📖 Overview
ONNX-GoMLX converts [ONNX models](https://onnx.ai/) (`.onnx` suffix) to 
[GoMLX (an accelerated machine learning framework for Go](https://github.com/gomlx/gomlx).

One can also fine-tune models with GoMLX and then save back its weights to the ONNX model.

Future plans also include creating an ONNX backend for GoMLX: so one can execute or export GoMLX models using ONNX.

The main use cases so far are:

1. **Fine-tuning**: import an inference-only ONNX model to GoMLX and use its auto-differentiation and training loop to
   fine-tune models. It allows saving the fine-tuned model as a GoMLX checkpoint or exporting the fine-tuned weights
   back to the ONNX model. This can also be used to expand / combine models.
2. **Inference**: use an ONNX file using Go and not having to include [ONNX Runtime](https://onnxruntime.ai/) (or Python)
   -- at the cost of including XLA/PJRT (the current only backend for GoMLX). It also allows one to extend the
   model with extra ML pre-/post-processing using GoMLX (image transformations, normalization, combining models,
   building ensembles, etc.). This may be interesting for large/expensive models, or large throughput on large
   batches.
    * Notice if you want to simply get a pure Go inference of ONNX models, see 
      [github.com/AdvancedClimateSystems/gonnx](https://github.com/AdvancedClimateSystems/gonnx) or
      [github.com/oramasearch/onnx-go](https://github.com/oramasearch/onnx-go). They will be slower (~8x based on a SentenceEncoder model, BERT based using `gonnx` vs `ONNXRuntime`) than 
      the XLA inference (or `onnxruntime`) for large projects, but for many use cases it doesn't matter, and they
      are a much smaller pure Go dependency. Only for CPU (no GPU support).

## Coverage of ONNX Ops Set

There are at least some 20 or so models that are working so far, but the list is growing.

* [Sentence Encoding all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)
has been working perfectly, see example below.
* [ONNX-GoMLX demo/development notebook](https://github.com/gomlx/onnx-gomlx/blob/main/onnx-go.ipynb): both serve as a functional test and to demo what it can do.

But **not all operations ("ops") are converted yet**. 
If you try it and find some operation that is not converted, please let us know (create an "issue") we will be happy to try to convert them.
Generally, all the required scaffolding and tooling are already there, and converting ops has been straightforward.

## 🎓 Example

We download (and cache) the [all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)  
using [github.com/gomlx/go-huggingface](https://github.com/gomlx/go-huggingface).

The tokens in the example are hardcoded for simplicity. See [github.com/gomlx/go-huggingface](https://github.com/gomlx/go-huggingface) for tokenization for various models.

```go
import (
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/gomlx/go-huggingface/hub"
	"github.com/gomlx/gomlx/ml/model"
)

...

// Download and cache the ONNX model from HuggingFace.
hfAuthToken := os.Getenv("HF_TOKEN")
hfModelID := "sentence-transformers/all-MiniLM-L6-v2"
repo := hub.New(modelID).WithAuth(hfAuthToken)
modelPath := must.M1(repo.DownloadFile("onnx/model.onnx"))

// Parse ONNX model.
onnxModel := must.M1(onnx.ReadFile(modelPath))

// Convert ONNX variables (model weights) to GoMLX model.Store -- which stores variables and can be checkpointed (saved):
store := model.NewStore()
must.M(onnxModel.VariablesToScope(store.RootScope()))

// Execute it with GoMLX/XLA:
sentences := []string{
    "This is an example sentence",
    "Each sentence is converted"}
//... tokenize ...
inputIDs := [][]int64{
    {101, 2023, 2003, 2019, 2742, 6251,  102},
    { 101, 2169, 6251, 2003, 4991,  102,    0}}
tokenTypeIDs := [][]int64{
    {0, 0, 0, 0, 0, 0, 0},
    {0, 0, 0, 0, 0, 0, 0}}
attentionMask := [][]int64{
    {1, 1, 1, 1, 1, 1, 1},
    {1, 1, 1, 1, 1, 1, 0}}
var embeddings []*tensors.Tensor
embeddings = model.MustCallOnceN( // Execute a GoMLX computation graph with a store
	backends.New(),  // GoMLX backend to use (defaults to XLA) 
	store, // Store stores the model variables/weights and optional hyperparameters.
	func (scope *model.Scope, inputs []*Node) []*Node {
		// Convert ONNX model (in `onnxModel`) to a GoMLX computation graph. It returns a slice of values (with only one for this model)
		return onnxModel.CallGraph(scope, inputs[0].Graph(), map[string]*Node{
			"input_ids": inputs[0],
			"attention_mask": inputs[1],
			"token_type_ids": inputs[2]}, targetOutputs...)
	}, 
	inputIDs, attentionMask, tokenTypeIDs)  // Inputs to the GoMLX function.
fmt.Printf("Embeddings: %s", embeddings)
```

The output looks like:

```
Embeddings: [2][7][384]float32{
 {{-0.0886, -0.0368, 0.0180, ..., 0.0261, 0.0912, -0.0152},
  {-0.0200, -0.0014, -0.0177, ..., 0.0204, 0.0522, 0.1991},
  {-0.0196, -0.0336, -0.0319, ..., 0.0203, 0.0709, 0.0644},
  ...,
  {-0.0253, 0.0408, 0.0125, ..., -0.0270, 0.0377, 0.1133},
  {-0.0140, -0.0275, 0.0796, ..., -0.0748, 0.0774, -0.0657},
  {0.0318, -0.0032, -0.0210, ..., 0.0387, 0.0191, -0.0059}},
 {{-0.0886, -0.0368, 0.0180, ..., 0.0261, 0.0912, -0.0152},
  {0.0304, 0.0531, -0.0238, ..., -0.1011, 0.0218, 0.0473},
  {-0.0027, -0.0508, 0.0805, ..., -0.0777, 0.0881, -0.0560},
  ...,
  {0.0928, 0.0165, -0.0976, ..., 0.0449, 0.0390, -0.0182},
  {0.0231, 0.0090, -0.0213, ..., 0.0232, 0.0191, -0.0066},
  {-0.0213, 0.0019, 0.0043, ..., 0.0561, 0.0170, 0.0256}}}
```

## Fine-Tuning

1. Extract the ONNX model's weight to GoMLX `model.Store`: see `Model.VariablesToScope()`.
2. Use `Model.CallGraph()` in your GoMLX model function (see example just above).
3. Train model as usual in GoMLX.
4. Depending on how you are going to use the model:
   1. Save the model as a GoMLX checkpoint, as usual.
   2. Save the model by updating the ONNX model: after training use `Model.ScopeToONNX()` to copy the updated variable  
      values from GoMLX `model.Scope` back to the ONNX model (in-memory), and then use `Model.Write()` or 
      `Model.SaveToFile()` to save the updated ONNX model to disk.

## Benchmarks

We have some GoMLX/XLA and ONNX Runtime (Microsoft) benchmarks in [this spreadsheet](https://docs.google.com/spreadsheets/d/1ikpJH6rVVHq8ES-IA8U4lkKH4XsTSpRyZewXwGTgits/edit?usp=sharing), 
tested on the sentence encoder model we were interested in. This was used during development and reflects
how it improves performance—the numbers on the bottom of the sheets are the currently accurate.

See [docs/benchmarks.md](docs/benchmarks.md) for more information.
   
## 🤝 Collaborating

Collaboration is very welcome: either in the form of code or simply with ideas with real applicability. Don't
hesitate to start a discussion or issue in the repository.

You can find the author and other interested parties in the [Slack channel #gomlx](https://app.slack.com/client/T029RQSE6/C08TX33BX6U) (you can [join the slack server here](https://invite.slack.golangbridge.org/)).

If you are interested, we have two notebooks we use to compare results. They are a good starting point for anyone curious:

* [Go Version](https://github.com/gomlx/onnx-gomlx/blob/main/onnx-go.ipynb)
* [ONNX Python Version](https://github.com/gomlx/onnx-gomlx)

## 🥳 Thanks

* This project was born from brainstorming with the talented folks at [KnightAnalytics](https://www.knightsanalytics.com/).
  Without their insight and enthusiasm this wouldn't have gotten off the ground.
* [ONNX models](https://onnx.ai/) is such a nice open source standard to communicate models across different implementations.
* [OpenXLA/XLA](https://github.com/openxla/xla) the open-source backend engine by Google that powers this implementation.
* Sources of inspiration:
  * https://github.com/knights-analytics/onnx-gomlx
  * https://github.com/oramasearch/onnx-go
