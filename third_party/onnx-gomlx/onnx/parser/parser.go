// Package parser provides functions to parse ONNX models into GoMLX.
package parser

import (
	"io"

	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx"
	_ "github.com/gomlx/onnx-gomlx/internal/onnxgomlx/fusion"
	"github.com/gomlx/onnx-gomlx/onnx"
)

// Parse an ONNX model (serialized proto) into an internal representation that can be used to build a GoMLX graph.
func Parse(contents []byte) (onnx.Model, error) {
	return onnxgomlx.Parse(contents)
}

// ParseFile parses an ONNX model (serialized proto) file into an internal representation that can be used to build a GoMLX graph.
func ParseFile(filePath string) (onnx.Model, error) {
	return onnxgomlx.ReadFile(filePath)
}

// ParseReader parses an ONNX model (serialized proto) from a reader into an internal representation that can be used to build a GoMLX graph.
func ParseReader(reader io.Reader) (onnx.Model, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return Parse(contents)
}

// FromProto parses an ONNX model into an internal representation that can be used to build a GoMLX graph.
//
// Deprecated: use Parse instead.
func FromProto(contents []byte) (onnx.Model, error) {
	return Parse(contents)
}

// FromFile parses an ONNX model file into an internal representation that can be used to build a GoMLX graph.
//
// Deprecated: use ParseFile instead.
func FromFile(filePath string) (onnx.Model, error) {
	return ParseFile(filePath)
}
