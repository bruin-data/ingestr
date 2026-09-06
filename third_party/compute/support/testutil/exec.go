package testutil

import (
	"fmt"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// Exec builds, compiles, and executes graph with multiple inputs and outputs.
//
// Each of the inputs are converted using ToBuffer, and the results are converted back to Go using FromBuffer.
func Exec(backend compute.Backend, inputs []any,
	buildFn func(f compute.Function, params []compute.Value) ([]compute.Value, error),
) ([]any, error) {
	builder := backend.Builder("test")
	mainFn := builder.Main()

	inputBuffers := make([]compute.Buffer, len(inputs))
	inputShapes := make([]shapes.Shape, len(inputs))
	var err error
	for i, goInput := range inputs {
		inputBuffers[i], err = ToBuffer(backend, goInput)
		if err != nil {
			return nil, errors.WithMessagef(err, "while transferring input #%d to a backend buffer", i)
		}
		inputShapes[i], err = inputBuffers[i].Shape()
		if err != nil {
			return nil, errors.WithMessagef(err, "while getting shape of input #%d", i)
		}
	}

	{
		// Build computation graph.
		var params []compute.Value
		if len(inputs) > 0 {
			params = make([]compute.Value, len(inputs))
			for i, s := range inputShapes {
				p, err := mainFn.Parameter(fmt.Sprintf("x%d", i), s, nil)
				if err != nil {
					return nil, errors.WithMessagef(err, "failed creating parameter for input #%d", i)
				}
				params[i] = p
			}
		}

		outputs, err := buildFn(mainFn, params)
		if err != nil {
			return nil, errors.WithMessage(err, "test build function returned an error")
		}

		if err := mainFn.Return(outputs, nil); err != nil {
			return nil, errors.WithMessage(err, "failed returning output from build function")
		}
	}

	exec, err := builder.Compile()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to compile")
	}

	outputBuffers, err := exec.Execute(inputBuffers, nil, 0)
	if err != nil {
		return nil, err
	}

	outputs := make([]any, len(outputBuffers))
	for i, o := range outputBuffers {
		outputs[i], err = FromBuffer(backend, o)
		if err != nil {
			return nil, errors.WithMessagef(err, "while converting output #%d to Go", i)
		}
	}
	return outputs, nil
}

// Exec1 is like Exec, but it has only 1 output.
func Exec1(backend compute.Backend, inputs []any,
	buildFn func(f compute.Function, params []compute.Value) (compute.Value, error),
) (any, error) {
	outputs, err := Exec(backend, inputs, func(f compute.Function, params []compute.Value) ([]compute.Value, error) {
		out, err := buildFn(f, params)
		if err != nil {
			return nil, err
		}
		return []compute.Value{out}, nil
	})
	if err != nil {
		return nil, err
	}
	if len(outputs) != 1 {
		return nil, errors.Errorf("expected 1 output, got %d", len(outputs))
	}
	return outputs[0], nil
}

// Exec2 is like Exec, but it has only 2 outputs.
func Exec2(backend compute.Backend, inputs []any,
	buildFn func(f compute.Function, params []compute.Value) (compute.Value, compute.Value, error),
) (any, any, error) {
	outputs, err := Exec(backend, inputs, func(f compute.Function, params []compute.Value) ([]compute.Value, error) {
		out0, out1, err := buildFn(f, params)
		if err != nil {
			return nil, err
		}
		return []compute.Value{out0, out1}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(outputs) != 2 {
		return nil, nil, errors.Errorf("expected 2 outputs, got %d", len(outputs))
	}
	return outputs[0], outputs[1], nil
}

// Exec3 is like Exec, but it has only 3 outputs.
func Exec3(backend compute.Backend, inputs []any,
	buildFn func(f compute.Function, params []compute.Value) (compute.Value, compute.Value, compute.Value, error),
) (any, any, any, error) {
	outputs, err := Exec(backend, inputs, func(f compute.Function, params []compute.Value) ([]compute.Value, error) {
		out0, out1, out2, err := buildFn(f, params)
		if err != nil {
			return nil, err
		}
		return []compute.Value{out0, out1, out2}, nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(outputs) != 3 {
		return nil, nil, nil, errors.Errorf("expected 3 outputs, got %d", len(outputs))
	}
	return outputs[0], outputs[1], outputs[2], nil
}
