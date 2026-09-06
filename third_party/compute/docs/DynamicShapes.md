# Dynamic Shapes

Dynamic shapes support for the `compute.Backend` for now entails allowing a computation graph to be defined
with input parameters having symbolic axes: these axes have unspecified dimensions but can be named 
(e.g.: "batchSize") and can have a "sentinel" dimension value `shapes.DynamicDim` (-1) indicating that
the particular axis length is dynamic (unknown at compile time).

Dynamic axes **must be named**, since we use the symbolic names to resolve the shape during execution.

These names are validated (if operands have named axes, their names must match). 

Compilation leave the concrete shapes unresolved: symbolic shapes are only made concrete during executiong when the
input shape is known. This avoids recompilation when only batch size (or other marked axes) change between calls.

## What is not included

Data dynamism, when the shapes depend on the content of the data (e.g.: it won't support a `Unique(x)` operation,
whose results depends on how many unique values there are in `x`).

Only input shapes conditioned dynamism: the shapes of the inputs can be dynamic, but must be resolvable given the
input shapes.

# Shape Resolution, the `shapes.AxisBindings`

During execution dynamic shapes must be "resolved" to the concrete input shapes. 
For this resolution we use `shapes.AxisBindings`, which is a map from axis names to axis lengths.

## Key design decisions

- **Per-specialization `node.data` override**: Shallow copies in `createSpecialization` already create new `*Node`
  structs. For DotGeneral/ConvGeneral/Gather, new data structs are created with values recomputed from resolved concrete
  shapes. Executors read `node.data` unchanged — they get the specialization-specific copy.
- **No executor changes**: All `exec*.go` files are untouched. Executors work on resolved nodes with concrete shapes and
  data.
- **Axis name propagation**: Every `DynamicDim` in output shapes carries an axis name so `Shape.Resolve()` works at
  specialization time. `propagateDynamicAxisNames` in `function_dedup.go` ensures this for ops that don't explicitly set
  names.
- **`DynamicAxes` capability flag**: Backends declare support; `graph.Exec.WithDynamicAxes()` checks the flag and
  returns a clear error if unsupported.

## TODO:

Allow instead of axisNames, have axis "expressions". E.g.: if we are doing data augmentation in a certain way of 
the input batch, and we concatenate the original batch with the augmented one on axis 0, the new axis 0 should
be be of size "=batchSize+batchSize" (or "=2*batchSize"), where "=" indicates an expression involving other axes
bindings.

For now we don't solve this.