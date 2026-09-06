// Package onnx provides the public interface for ONNX models in GoMLX.
//
// The a parsed ONNX Model can be used to on-the-fly generate a GoMLX model.
//
// Use onnx/parser to parse ONNX models from either the proto contents, or reading from a file.
package onnx

import (
	"io"

	"github.com/gomlx/compute/shapes"
	. "github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/ml/model"
)

// Model interface represents a parsed ONNX file.
//
// It can be used to generate the corresponding GoMLX model graph and executed for inference or used on a training loop for fine-tuning.
// It can also be used to populate a context with the variables of the ONNX model.
//
// See examples of usage in internal/benchmarks: there are a couple of small LLM models and the InceptionV3 ONNX models imported to
// GoMLX there.
type Model interface {
	// Name of the model graph.
	Name() string

	// Close releases resources held by the model.
	Close() error

	// Inputs return the names and shapes of the inputs.
	// Shapes will return with a dimension set to DynamicDim (-1) for dynamic axes.
	Inputs() (names []string, dshapes []shapes.Shape)

	// Outputs return a description of the outputs.
	// Shapes will return with a dimension set to DynamicDim (-1) for dynamic axes.
	Outputs() (names []string, dshapes []shapes.Shape)

	// NumInputs returns the number of inputs this graph takes.
	NumInputs() int

	// WithInputsAsConstants marks inputs to be considered as constants and not vary for different examples in training or inference.
	WithInputsAsConstants(inputsAsConstants map[string]any) Model

	// AllowDTypePromotion enables automatic dtype promotion for operations with mismatched types.
	AllowDTypePromotion() Model

	// PrioritizeFloat16 configures dtype promotion to prefer Float16 over Float32.
	PrioritizeFloat16() Model

	// WithBaseDir sets the base directory for the model. This is used for resolving external data file paths.
	// This must be set before any reading of the model data (e.g.: VariablesToScope or CallGraph).
	//
	// It defaults to the current directory (".") or, if the model was read from a file, to the directory of that file.
	WithBaseDir(baseDir string) Model

	// WithExternalDataReader configures the model to use a specialized ExternalDataReader.
	//
	// It defaults to a standard file reader that reads from files in the base directory (see WithBaseDir).
	WithExternalDataReader(reader ExternalDataReader) Model

	// Write will write the ONNX model to the given writer (usually a file).
	Write(w io.Writer) error

	// SaveToFile serializes the ONNX model to the given file.
	SaveToFile(path string) error

	// CallGraph calls the ONNX graph, and hence are building it with GoMLX ops.
	CallGraph(scope *model.Scope, g *Graph, inputs map[string]*Node, outputNames ...string) (outputs []*Node)

	// VariablesToScope uploads all variable values from the ONNX model to the given model's Store, at the given Scope.
	VariablesToScope(scope *model.Scope) error

	// FreeUnusedVariables frees variables that are not used in the graph.
	FreeUnusedVariables()

	// ScopeToONNX copies over the variables in GoMLX's Store, in the given Scope, to the ONNX's model proto.
	ScopeToONNX(scope *model.Scope) error

	// String implements fmt.Stringer.
	String() string

	// PrintGraph prints the model graph to the given writer.
	PrintGraph(writer io.Writer) error

	// PrintVariables prints the model variables to the given writer.
	PrintVariables(writer io.Writer) error

	// PrintGraphviz prints the model graph in Graphviz format to the given writer.
	PrintGraphviz(writer io.Writer, targets ...string) error

	// ShapeForName returns the shape of the given node output name.
	ShapeForName(name string) shapes.Shape

	// ValidateInputs checks the inputs has a shape that is compatible with the DynamicShapes of the inputs for the model.
	ValidateInputs(inputsShapes ...shapes.Shape) error

	// DisableFusion clears all detected fusions, forcing normal (unfused) conversion.
	DisableFusion()
}

// ModelScope is the default scope used for ONNX model variables in a GoMLX context.
const ModelScope = "ONNX"

// ExternalDataReader interface for reading ONNX tensors data from external files.
//
// ONNX-GoMLX provides a default implementation that reads from files in BaseDir (see WithBaseDir),
// but if the user has the files located in a different place (e.g.: cloud storage, or remotely in some way),
// they can provide an implementation of this interface to read the data.
//
// Notice an implementation should keep the file handles open, as tensors can
// potentially be read in an undefined order.
// Close will be called at the end of series of calls (like when executing Model.VariablesToScope).
type ExternalDataReader interface {
	// ReadInto reads the data (for a tensor) from a file (Location) and offset, into the given output slice of bytes.
	//
	// The info.Length field should be ignored, instead read len(output) bytes (currently they always match).
	ReadInto(info ExternalDataInfo, output []byte) error

	// Close is called at the end of session of reading tensor data, it allows the implementation to free
	// resources (close files).
	//
	// This shouldn't be seen as permanent: the Model may call ReadInto again after a call to Close,
	// if the user calls Model.VariablesToScope again, for instance.
	// So one should keep the ability to re-open any resources one may need.
	Close() error
}

// ExternalDataInfo holds parsed external data parameters from an ONNX TensorProto.
type ExternalDataInfo struct {
	Location string // path to external file (relative to model directory)
	Offset   int64  // byte offset in file, default 0
	Length   int64  // number of the data bytes
}
