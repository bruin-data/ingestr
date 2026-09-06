package onnxgomlx

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/gomlx/support/sets"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/pkg/errors"
)

// String implements fmt.Stringer, and pretty prints model information.
func (m *Model) String() string {
	// Convenient writing to buffer that will hold the result.
	var buf bytes.Buffer
	w := func(format string, args ...any) { buf.WriteString(fmt.Sprintf(format, args...)) }

	// Model header:
	w("ONNX Model:\n")
	if m.Proto.DocString != "" {
		w("# %s\n", m.Proto.DocString)
	}
	if m.Proto.ModelVersion != 0 {
		w("\tVersion:\t%d\n", m.Proto.ModelVersion)
	}
	if m.Proto.ProducerName != "" {
		w("\tProducer:\t%s / %s\n", m.Proto.ProducerName, m.Proto.ProducerVersion)
	}

	// Graph information:
	w("\t# inputs:\t%d\n", len(m.Proto.Graph.Input))
	for ii, input := range m.Proto.Graph.Input {
		w("\t\t[#%d] %s\n", ii, ppValueInfo(input))
	}
	w("\t# outputs:\t%d\n", len(m.Proto.Graph.Output))
	for ii, output := range m.Proto.Graph.Output {
		w("\t\t[#%d] %s\n", ii, ppValueInfo(output))
	}
	w("\t# nodes:\t%d\n", len(m.Proto.Graph.Node))

	// Tensors (variables):
	w("\t# tensors (variables):\t%d\n", len(m.Proto.Graph.Initializer))
	w("\t# sparse tensors (variables):\t%d\n", len(m.Proto.Graph.SparseInitializer))

	// List op-sets used.
	opTypesSet := sets.Make[string]()
	for _, n := range m.Proto.Graph.Node {
		opTypesSet.Insert(n.GetOpType())
	}
	w("\tOp sets:\t%#v\n", slices.Sorted(maps.Keys(opTypesSet)))

	// Training Info.
	if len(m.Proto.TrainingInfo) > 0 {
		w("\t# training info:\t%d\n", len(m.Proto.TrainingInfo))
	}

	// Extra functions:
	if len(m.Proto.Functions) > 0 {
		fnSet := sets.Make[string]()
		for _, f := range m.Proto.Functions {
			fnSet.Insert(f.Name)
		}
		w("\tFunctions:\t%#v\n", slices.Sorted(maps.Keys(fnSet)))
	}

	// Versions.
	w("\tIR Version:\t%d\n", m.Proto.IrVersion)
	w("\tOperator Sets:\t[")
	for ii, opSetId := range m.Proto.OpsetImport {
		if ii > 0 {
			w(", ")
		}
		if opSetId.Domain != "" {
			w("v%d (%s)", opSetId.Version, opSetId.Domain)
		} else {
			w("v%d", opSetId.Version)
		}
	}
	w("]\n")

	// Extra meta-data.
	if len(m.Proto.MetadataProps) > 0 {
		w("\tMetadata: [")
		for ii, prop := range m.Proto.MetadataProps {
			if ii > 0 {
				w(", ")
			}
			w("%s=%s", prop.Key, prop.Value)
		}
		w("]\n")
	}
	return buf.String()
}

func ppValueInfo(vi *protos.ValueInfoProto) string {
	if vi.DocString != "" {
		return fmt.Sprintf("%q: %s  # %s", vi.Name, ppType(vi.Type), vi.DocString)
	}
	return fmt.Sprintf("%q: %s", vi.Name, ppType(vi.Type))
}

func ppType(t *protos.TypeProto) string {
	if seq := t.GetSequenceType(); seq != nil {
		return ppSeqType(seq)
	} else if tensor := t.GetTensorType(); tensor != nil {
		return ppTensorType(tensor)
	}
	return "??type??"
}

func ppSeqType(seq *protos.TypeProto_Sequence) string {
	return fmt.Sprintf("(%s...)", ppType(seq.ElemType))
}

func ppTensorType(t *protos.TypeProto_Tensor) string {
	dshape, err := makeShapeFromProto(t)
	if err != nil {
		return "(invalid dtype)"
	}
	return dshape.String()
}

// PrintGraph prints a +/- human-readable (or debuggable) version of the graph to the given writer.
func (m *Model) PrintGraph(writer io.Writer) error {
	var err error
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(writer, format, args...)
		if err != nil {
			err = errors.Wrapf(err, "Model.PrintGraph() failed to write")
		}
	}

	w("Model Graph %q:\n", m.Proto.Graph.Name)
	// Convenient writing to buffer that will hold the result.
	for _, n := range m.Proto.Graph.Node {
		w("%q:\t[%s]\n", n.GetName(), n.GetOpType())
		w("\tInputs: %q\n", n.GetInput())
		w("\tOutputs: %q\n", n.GetOutput())
		if len(n.Attribute) > 0 {
			w("\tAttributes: ")
			for ii, attr := range n.Attribute {
				if ii > 0 {
					w(", ")
				}
				w("%s (%s", attr.Name, attr.Type)
				switch attr.Type {
				case protos.AttributeProto_TENSOR:
					shape, err := Shape(attr.T)
					if err != nil {
						w(" - unparseable shape: %v", err)
					} else {
						w(": %s", shape)
					}
				case protos.AttributeProto_INT:
					w(": %d", attr.I)
				case protos.AttributeProto_INTS:
					if len(attr.Ints) < 20 {
						w(": %v", attr.Ints)
					}
				case protos.AttributeProto_FLOAT:
					w(": %f", attr.F)
				default:
				}
				w(")")
			}
			w("\n")
		}
	}
	return err
}

// NodeToString converts a NodeProto to a one-line string that can be used for debugging.
func NodeToString(n *protos.NodeProto) string {
	var buf bytes.Buffer
	w := func(format string, args ...any) { _, _ = fmt.Fprintf(&buf, format, args...) }

	w("Node %q [%s]", n.Name, n.OpType)
	w("(%s)", strings.Join(n.Input, ", "))    // Inputs
	w(" -> %s", strings.Join(n.Output, ", ")) // Output(s)
	if len(n.Attribute) > 0 {
		w(" - attrs[")
		for ii, attr := range n.Attribute {
			if ii > 0 {
				w(", ")
			}
			w("%s (%s)", attr.Name, attr.Type)
		}
		w("]")
	}
	return buf.String()
}

func (m *Model) PrintVariables(writer io.Writer) error {
	var err error
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(writer, format, args...)
		if err != nil {
			err = errors.Wrapf(err, "Model.PrintGraph() failed to write")
		}
	}

	w("%d tensors (variables)", len(m.Proto.Graph.Initializer))
	if len(m.Proto.Graph.Initializer) > 0 {
		w(":")
	}
	w("\n")
	for _, t := range m.Proto.Graph.Initializer {
		shape, _ := Shape(t)
		w("\t%q: %s", t.Name, shape)
		if t.DocString != "" {
			w(" # %s", t.DocString)
		}
		w("\n")
	}
	w("%d sparse tensors (variables)", len(m.Proto.Graph.SparseInitializer))
	if len(m.Proto.Graph.SparseInitializer) > 0 {
		w(":")
	}
	w("\n")
	for _, st := range m.Proto.Graph.SparseInitializer {
		shape, _ := SparseShape(st)
		w("\t\t%q: dense shape=%v\n", st.Values.Name, shape)
	}
	return err
}

// PrintGraphviz outputs the model graph using the "dot" language, starting from the target nodes towards
// its dependencies.
//
// If targets are left empty, it takes the default graph outputs as targets.
func (m *Model) PrintGraphviz(writer io.Writer, targets ...string) error {
	if targets == nil {
		targets = m.OutputsNames
	}

	var err error
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(writer, format, args...)
		if err != nil {
			err = errors.Wrapf(err, "Model.PrintGraphviz() failed to write")
		}
	}

	w("digraph %s {\n", m.Name())
	visited := sets.Make[string]()
	for _, target := range targets {
		if err != nil {
			break
		}
		err = m.recursiveGraphviz(writer, visited, target)
	}
	w("}")
	return err
}

var (
	GraphvizInputColor = "#FFF59E"
	GraphvizVarColor   = "#E0E0E0"
)

func (m *Model) recursiveGraphviz(writer io.Writer, visited sets.Set[string], target string) error {
	if visited.Has(target) {
		return nil
	}
	visited.Insert(target)

	// Define w.
	var err error
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(writer, format, args...)
		if err != nil {
			err = errors.Wrapf(err, "Model.PrintGraphviz() failed to write")
		}
	}

	// The target is an input.
	if m.InputsNameSet.Has(target) {
		w("\t%q [shape=box, style=filled, fillcolor=%q];\n", target, GraphvizInputColor)
		return err
	}

	// the target is a label.
	if v, found := m.VariableNameToValue[target]; found {
		var vShape shapes.Shape
		vShape, err = Shape(v)
		w("\t%q [shape=box, style=filled, fillcolor=%q, tooltip=%q];\n", target, GraphvizVarColor, vShape)
		return err
	}

	node, found := m.NodeOutputToNode[target]
	if !found {
		err = errors.Errorf("couldn't find target %q in model graph!?", target)
		return err
	}

	for _, input := range node.Input {
		w("\t%q -> %q\n", input, target)
		if err != nil {
			return err
		}
		err = m.recursiveGraphviz(writer, visited, input)
	}
	return err
}
