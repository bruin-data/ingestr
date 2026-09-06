// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package backendparser parses the compute interfaces (Backend, Builder, Function, etc.) and enumerate
// their methods.
//
// This is useful to generate code that works with these interfaces.
package backendparser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/gomlx/compute/internal/exceptions"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// Method represents a single method from the backends.Builder or backends.Function interface
// with all its signature information as strings.
type Method struct {
	// Name is the method name
	Name string
	// Comment is the method documentation comment
	Comments []string
	// Parameters of the method.
	Parameters []NameAndType
	// Outputs of the method.
	// Outputs names may contain all empty strings if they are not defined.
	Outputs []NameAndType
	// Interface indicates which interface this method belongs to: "Builder" or "Function"
	Interface string
}

type NameAndType struct {
	Name, Type string
}

// ParseBuilder returns all methods defined in the backends.Builder and backends.Function interfaces,
// including those from embedded interfaces like backends.StandardOps and backends.CollectiveOps.
func ParseBuilder() ([]Method, error) {
	fileSet := token.NewFileSet()
	var methods []Method

	root, err := findModuleRoot()
	if err != nil {
		return nil, err
	}

	// Parse all relevant files
	fileNames := []string{"builder.go", "function.go", "ops.go", "ops_dynamic.go", "ops_fused.go", "ops_collective.go"}
	parsedFiles := make(map[string]*ast.File)
	fileCache := make(map[string][]byte)
	for _, fileName := range fileNames {
		filePath := filepath.Join(root, fileName)
		var err error
		fileCache[fileName], err = os.ReadFile(filePath)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read %s", fileName)
		}
		klog.V(1).Infof("Read file %s: %d bytes", fileName, len(fileCache[fileName]))
		parsedFiles[fileName], err = parser.ParseFile(fileSet, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse %s", fileName)
		}
	}

	// Extract the text from a node
	getText := func(node ast.Node) string {
		pos := fileSet.Position(node.Pos())
		fileName := filepath.Base(pos.Filename)
		fileContent := fileCache[fileName]
		endOffset := fileSet.Position(node.End()).Offset
		if endOffset > len(fileContent) {
			exceptions.Panicf("end offset out of bounds for file %s (len(fileContent)=%d, endOffset=%d)",
				fileName, len(fileContent), endOffset)
		}
		return string(fileContent[pos.Offset:endOffset])
	}

	// Helper to extract methods from interface declarations
	includeInterfaces := []string{"Builder", "Function", "StandardOps", "DynamicOps", "CollectiveOps", "FusedOps"}
	for _, fileName := range fileNames {
		ast.Inspect(parsedFiles[fileName], func(n ast.Node) bool {
			if typeSpec, ok := n.(*ast.TypeSpec); ok {
				if interfaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
					if !slices.Contains(includeInterfaces, typeSpec.Name.Name) {
						return true
					}
					klog.V(1).Infof("- Processing %s, interface %q", fileName, typeSpec.Name.Name)
					for _, method := range interfaceType.Methods.List {
						// Extract method information
						funcType, ok := method.Type.(*ast.FuncType)
						if !ok {
							continue
						}
						m := Method{
							Name:      method.Names[0].Name,
							Interface: typeSpec.Name.Name,
						}

						// Get method comment if any
						if method.Doc != nil {
							m.Comments = make([]string, 0, len(method.Doc.List))
							for _, comment := range method.Doc.List {
								m.Comments = append(m.Comments, comment.Text)
							}
						}

						// Get parameters
						if funcType.Params != nil {
							for _, param := range funcType.Params.List {
								paramType := getText(param.Type)
								for _, name := range param.Names {
									param := NameAndType{Name: name.Name, Type: paramType}
									m.Parameters = append(m.Parameters, param)
								}
							}
						}

						// Get outputs
						if funcType.Results != nil {
							for _, result := range funcType.Results.List {
								resultType := getText(result.Type)
								if len(result.Names) == 0 {
									m.Outputs = append(m.Outputs, NameAndType{Type: resultType})
								} else {
									for _, name := range result.Names {
										param := NameAndType{Name: name.Name, Type: resultType}
										m.Outputs = append(m.Outputs, param)
									}
								}
							}
						}

						klog.V(1).Infof("   - Method %q", method.Names[0].Name)
						methods = append(methods, m)
					}
				}
			}
			return true
		})
	}
	return methods, nil
}

// findModuleRoot returns the absolute path to the module root directory
// for github.com/gomlx/compute, determined relative to this file's location.
func findModuleRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("could not get caller information to determine module root")
	}
	// filename is the path to support/backendparser/backendparser.go.
	// Go up 2 levels (to support/, then to compute root directory).
	return filepath.Join(filepath.Dir(filename), "../.."), nil
}
