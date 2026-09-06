// Package onnxgomlx provides the internal implementation for ONNX models in GoMLX.
package onnxgomlx

import (
	"io"
	"os"
	"path/filepath"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/gomlx/support/sets"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx/filesreader"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
)

// Model represents a parsed ONNX file.
type Model struct {
	ONNXFileName, baseDir string
	Proto                 protos.ModelProto
	NodeOutputToNode      map[string]*protos.NodeProto

	// Names used for variables and inputs: these are like internal outputs, but they come not from a node,
	// but from an input or variable. Used to introspect the graph.
	InputsNameSet       sets.Set[string]
	VariableNameToValue map[string]*protos.TensorProto

	name                        string
	InputsNames, OutputsNames   []string
	InputsShapes, OutputsShapes []shapes.Shape

	// InputsAsConstants: see WithInputsAsConstants
	InputsAsConstants map[string]any

	// Backend used for ONNX-conversion time tensor processing.
	Backend compute.Backend

	// allowDTypePromotion enables automatic dtype promotion for mixed-precision models.
	// By default (false), dtype mismatches will panic per ONNX spec.
	allowDTypePromotion bool

	// prioritizeFloat16 prefers Float16 over Float32 when promoting dtypes.
	// Only applies when allowDTypePromotion is true.
	prioritizeFloat16 bool

	// ExternalDataReader manages memory-mapped external data files for efficient tensor loading.
	// It is initialized lazily when external data is first accessed.
	ExternalDataReader onnx.ExternalDataReader

	// Consumers maps output names to the nodes that consume them. Built during detectFusionPatterns
	// and used by fusion detectors to walk the graph.
	Consumers map[string][]*protos.NodeProto

	// DetectedFusions maps output names to detected fusion candidates (SDPA, QKV Dense, Dense+Gelu).
	// Populated by detectFusionPatterns during Parse. The GoMLX wrapper functions
	// (attention.Core, attention.QKVProjection, nn.Dense) handle fused-vs-decomposed
	// fallback internally, so all detected fusions are always active.
	DetectedFusions map[string]FusionCandidate
}

// Ensure Model implements onnx.Model.
var _ onnx.Model = &Model{}

// Parse parses an ONNX model into an internal representation that can be used to build a GoMLX graph.
func Parse(contents []byte) (*Model, error) {
	// Parse the ONNX proto.
	m := &Model{
		baseDir: ".",
	}
	err := proto.Unmarshal(contents, &m.Proto)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse ONNX model proto")
	}

	// Create the backend that we'll use for processing of tensors.
	m.Backend, err = gobackend.New("")
	if err != nil {
		return nil, errors.WithMessage(err, "ONNX conversion requires GoMLX for processing of tensors, but failed to create the Go backend for GoMLX model")
	}

	// Parse inputs and outputs.
	m.name = m.Proto.Graph.Name
	m.InputsNameSet = sets.Make[string]()
	m.InputsNames = make([]string, len(m.Proto.Graph.Input))
	m.InputsShapes = make([]shapes.Shape, len(m.Proto.Graph.Input))
	for ii, input := range m.Proto.Graph.Input {
		m.InputsNames[ii] = input.Name
		m.InputsNameSet.Insert(input.Name)

		tensorType, ok := input.Type.Value.(*protos.TypeProto_TensorType)
		if !ok {
			return nil, errors.Errorf("output #%d (%q) is not a tensor, not sure how to handle it", ii, input.Name)
		}
		m.InputsShapes[ii], err = makeShapeFromProto(tensorType.TensorType)
		if err != nil {
			return nil, errors.WithMessagef(err, "while parsing output #%d (%q)", ii, input.Name)
		}
	}
	m.OutputsNames = make([]string, len(m.Proto.Graph.Output))
	m.OutputsShapes = make([]shapes.Shape, len(m.Proto.Graph.Output))
	for ii, output := range m.Proto.Graph.Output {
		m.OutputsNames[ii] = output.Name
		tensorType, ok := output.Type.Value.(*protos.TypeProto_TensorType)
		if !ok {
			return nil, errors.Errorf("output #%d (%q) is not a tensor, not sure how to handle it", ii, output.Name)
		}
		m.OutputsShapes[ii], err = makeShapeFromProto(tensorType.TensorType)
		if err != nil {
			return nil, errors.WithMessagef(err, "while parsing output #%d (%q)", ii, output.Name)
		}
	}

	// Set of variable names.
	m.VariableNameToValue = make(map[string]*protos.TensorProto)
	for _, tensorProto := range m.Proto.Graph.Initializer {
		m.VariableNameToValue[tensorProto.Name] = tensorProto
	}

	// Maps the intermediary node outputs to the nodes that create them.
	m.NodeOutputToNode = make(map[string]*protos.NodeProto)
	for _, node := range m.Proto.Graph.Node {
		for _, outputName := range node.GetOutput() {
			if otherNode, found := m.NodeOutputToNode[outputName]; found {
				return nil, errors.Errorf("invalid graph: node output name %q used by 2 different nodes: (1) %s, (2) %s",
					outputName, NodeToString(otherNode), NodeToString(node))
			}
			m.NodeOutputToNode[outputName] = node
		}
	}

	// Detect fusible patterns (SDPA, QKV Dense) for potential acceleration.
	m.detectFusionPatterns()

	return m, nil
}

// ReadFile parses an ONNX model file into an internal representation that can be used to build a GoMLX graph.
// Notice any large constant is converted to variables.
func ReadFile(filePath string) (*Model, error) {
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read ONNX model file in %s", filePath)
	}
	m, err := Parse(contents)
	if err != nil {
		return nil, err
	}
	m.ONNXFileName = filePath
	m.baseDir = filepath.Dir(filePath)
	return m, nil
}

// Name of the model graph.
func (m *Model) Name() string { return m.name }

// BaseDir returns the directory containing the ONNX model file.
// This is used for resolving external data file paths.
// Returns an empty string if the model was not loaded from a file.
func (m *Model) BaseDir() string {
	return m.baseDir
}

// WithBaseDir sets the base directory for the model. This is used for resolving external data file paths.
//
// It defaults to the current directory (".") or, if the model was read from a file, to the directory of that file.
func (m *Model) WithBaseDir(baseDir string) onnx.Model {
	m.baseDir = baseDir
	return m
}

// WithExternalDataReader configures the model to use a specialized ExternalDataReader.
//
// It defaults to a standard file reader that reads from files in the base directory (see WithBaseDir).
func (m *Model) WithExternalDataReader(reader onnx.ExternalDataReader) onnx.Model {
	m.ExternalDataReader = reader
	return m
}

// getExternalDataReader returns the ExternalDataReader for this model, creating it lazily if needed.
// Returns nil if the model has no base directory (e.g., parsed from bytes without a file path).
func (m *Model) getExternalDataReader() onnx.ExternalDataReader {
	if m.ExternalDataReader != nil {
		return m.ExternalDataReader
	}
	baseDir := m.BaseDir()
	if baseDir == "" {
		return nil
	}
	m.ExternalDataReader = filesreader.New(baseDir)
	return m.ExternalDataReader
}

// Close releases resources held by the model, including memory-mapped external data files.
// After Close is called, the model should not be used for operations that require external data.
// It is safe to call Close multiple times.
func (m *Model) Close() error {
	if m.ExternalDataReader != nil {
		err := m.ExternalDataReader.Close()
		m.ExternalDataReader = nil
		return err
	}
	return nil
}

// Inputs return the names and shapes of the inputs.
// Shapes will return with a dimension set to onnx.DynamicDim (-1) for dynamic axes.
func (m *Model) Inputs() (names []string, gshapes []shapes.Shape) {
	names = m.InputsNames
	gshapes = make([]shapes.Shape, len(m.InputsShapes))
	copy(gshapes, m.InputsShapes)
	return
}

// Outputs return a description of the outputs.
// Shapes will return with a dimension set to onnx.DynamicDim (-1) for dynamic axes.
func (m *Model) Outputs() (names []string, gshapes []shapes.Shape) {
	names = m.OutputsNames
	gshapes = make([]shapes.Shape, len(m.OutputsShapes))
	copy(gshapes, m.OutputsShapes)
	return
}

// NumInputs returns the number of inputs this graph takes.
func (m *Model) NumInputs() int {
	return len(m.InputsNames)
}

// WithInputsAsConstants marks inputs to be considered as constants and not vary for different examples in training
// or inference.
// Use this just immediately after the creation of the Model.
// Later changes can cause inconsistencies.
//
// This makes them become constants in the graph, and they shouldn't be passed to CallGraph as inputs.
//
// The value each input maps to will be converted to a tensors.FromAnyValue.
func (m *Model) WithInputsAsConstants(inputsAsConstants map[string]any) onnx.Model {
	m.InputsAsConstants = inputsAsConstants
	return m
}

// AllowDTypePromotion enables automatic dtype promotion for operations with
// mismatched types. By default, ONNX does not allow implicit casting, so
// dtype mismatches will panic. Enable this for mixed-precision models
// (e.g., from quantization-aware training or mixed-precision export).
func (m *Model) AllowDTypePromotion() onnx.Model {
	m.allowDTypePromotion = true
	return m
}

// PrioritizeFloat16 configures dtype promotion to prefer Float16 over Float32.
// This leverages hardware-accelerated FP16 kernels on ARM64/NEON platforms.
// Only effective when AllowDTypePromotion() is also called.
func (m *Model) PrioritizeFloat16() onnx.Model {
	m.prioritizeFloat16 = true
	return m
}

// Write will write the ONNX model to the given writer (usually a file).
//
// This is useful if the model variables were updated (e.g.: fine-tuning in GoMLX) and one wants to save the
// model.
// See ScopeToONNX to copy over the variables in GoMLX's model.Scope (presumably after some training/update) to the
// ONNX's model proto.
//
// See also Model.SaveToFile.
func (m *Model) Write(w io.Writer) error {
	content, err := proto.Marshal(&m.Proto)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize ONNX model proto")
	}
	_, err = w.Write(content)
	if err != nil {
		return errors.Wrapf(err, "failed to write serialized ONNX model proto")
	}
	return nil
}

// SaveToFile serializes the ONNX model to the given file.
//
// This is useful if the model variables were updated (e.g.: fine-tuning in GoMLX) and one wants to save the
// model.
// See ScopeToONNX to copy over the variables in GoMLX's model.Scope (presumably after some training/update) to the
// ONNX's model proto.
func (m *Model) SaveToFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return errors.Wrapf(err, "failed to save ONNX model proto to %s", path)
	}
	err = m.Write(f)
	if err != nil {
		_ = f.Close()
		return err
	}
	err = f.Close()
	if err != nil {
		return errors.Wrapf(err, "failed to save ONNX model proto to %s", path)
	}
	return nil
}
