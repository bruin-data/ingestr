// Package gliner implements opt-in, batch-one GLiNER PII Edge inference in Go.
package gliner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
	"github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	"google.golang.org/protobuf/proto"
)

var constantRetention sync.Once

type Runtime struct {
	mu      sync.Mutex
	tok     *hftokenizer.Tokenizer
	om      onnx.Model
	store   *model.Store
	backend compute.Backend
}

// New is the only entry point that downloads assets or initializes model state.
func New(ctx context.Context) (r *Runtime, err error) {
	dir, err := modelDirectory(ctx)
	if err != nil {
		return nil, err
	}
	r = &Runtime{}
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("GLiNER initialization failed")
		}
		if err != nil {
			r.Close()
			r = nil
		}
	}()
	cfg, err := api.ParseConfigFile(filepath.Join(dir, "tokenizer_config.json"))
	if err != nil {
		return r, fmt.Errorf("GLiNER tokenizer configuration failed")
	}
	r.tok, err = hftokenizer.NewFromFile(cfg, filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return r, fmt.Errorf("GLiNER tokenizer initialization failed")
	}
	if err = r.tok.With(api.EncodeOptions{AddSpecialTokens: false}); err != nil {
		return r, fmt.Errorf("GLiNER tokenizer options failed")
	}
	data, err := os.ReadFile(filepath.Join(dir, "onnx/model.onnx"))
	if err != nil {
		return r, fmt.Errorf("read GLiNER weights: %w", err)
	}
	var p protos.ModelProto
	if err := proto.Unmarshal(data, &p); err != nil {
		return r, fmt.Errorf("GLiNER model decoding failed")
	}
	for _, node := range p.Graph.Node {
		// All steps are valid for unpadded batch one. Omitting lengths avoids
		// GoMLX's LSTM mask broadcasting issue; never use this for padded batches.
		if node.OpType == "LSTM" && len(node.Input) > 4 {
			node.Input[4] = ""
		}
	}
	data, err = proto.Marshal(&p)
	if err != nil {
		return r, fmt.Errorf("GLiNER model adaptation failed")
	}
	r.om, err = parser.Parse(data)
	if err != nil {
		return r, fmt.Errorf("GLiNER model import failed")
	}
	constantRetention.Do(func() { graph.MinConstValueSizeToKeep = 1 << 20 })
	r.store = model.NewStore()
	if err := r.om.VariablesToScope(r.store.RootScope()); err != nil {
		return r, fmt.Errorf("GLiNER weight initialization failed")
	}
	r.backend, err = gobackend.New("")
	if err != nil {
		return r, fmt.Errorf("GLiNER Go backend initialization failed")
	}
	return r, nil
}

func (r *Runtime) Mask(text string) (result string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Library errors/panics may contain tensors or input tokens. Never expose them.
	defer func() {
		if recover() != nil {
			result, err = "", fmt.Errorf("GLiNER inference failed")
		}
	}()
	if r.backend == nil {
		return "", fmt.Errorf("GLiNER runtime is closed")
	}
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	inputs, words, err := tokenize(r.tok, text)
	if err != nil {
		return "", err
	}
	logits, err := r.infer(inputs)
	if err != nil {
		return "", fmt.Errorf("GLiNER inference failed")
	}
	_, result, err = decode(text, words, logits, 0.3)
	return result, err
}

// Called under mu. Constants and compiled graphs belong to exactly one text;
// only the tokenizer, parsed model, weight Store, and backend survive the call.
func (r *Runtime) infer(inputs map[string][][]int64) ([]float32, error) {
	r.om.WithInputsAsConstants(map[string]any{
		"text_lengths": inputs["text_lengths"], "words_mask": inputs["words_mask"], "input_ids": inputs["input_ids"],
	})
	defer r.om.WithInputsAsConstants(nil)
	exec, err := model.NewExec(r.backend, r.store, func(scope *model.Scope, x *graph.Node) []*graph.Node {
		return r.om.CallGraph(scope, x.Graph(), map[string]*graph.Node{"attention_mask": x})
	})
	if err != nil {
		return nil, err
	}
	defer exec.Finalize()
	out, err := exec.Call(inputs["attention_mask"])
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, t := range out {
			_ = t.FinalizeAll()
		}
	}()
	return tensors.MustCopyFlatData[float32](out[0]), nil
}

func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.om != nil {
		_ = r.om.Close()
		r.om = nil
	}
	if r.store != nil {
		r.store.Finalize()
		r.store = nil
	}
	if r.backend != nil {
		r.backend.Finalize()
		r.backend = nil
	}
	r.tok = nil
}
