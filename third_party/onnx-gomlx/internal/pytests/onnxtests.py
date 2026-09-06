import onnx
import numpy as np
import onnxruntime
import tempfile
import os
from typing import Dict, Any, List, Optional, Union

def numpy_to_onnx_tensor_type(np_dtype):
    """
    Converts a numpy dtype to its corresponding ONNX TensorProto.DataType.

    Args:
        np_dtype (numpy.dtype): The numpy data type.

    Returns:
        int: The ONNX TensorProto.DataType enumeration value.

    Raises:
        ValueError: If the numpy dtype is not supported for ONNX conversion.
    """
    if np_dtype == np.float32:
        return onnx.TensorProto.FLOAT
    elif np_dtype == np.float64:
        return onnx.TensorProto.DOUBLE
    elif np_dtype == np.int8:
        return onnx.TensorProto.INT8
    elif np_dtype == np.int16:
        return onnx.TensorProto.INT16
    elif np_dtype == np.int32:
        return onnx.TensorProto.INT32
    elif np_dtype == np.int64:
        return onnx.TensorProto.INT64
    elif np_dtype == np.bool_:
        return onnx.TensorProto.BOOL
    elif np_dtype == np.uint8:
        return onnx.TensorProto.UINT8
    else:
        raise ValueError(f"Unsupported numpy dtype for ONNX conversion: {np_dtype}")

def run_single_onnx_op(
    op_name: str,
    inputs: Dict[str, Any],
    output_shapes: List[List[Union[int, str, None]]],
    output_dtypes: List[int],
    attributes: Optional[Dict[str, Any]] = None,
    opset_version: int = 15, # Default opset_version to 15
    ir_version: int = 10,    # Default ir_version to 10
) -> List[np.ndarray]:
    """
    Creates a temporary ONNX model with a single specified ONNX operator,
    executes it with ONNX Runtime using the provided inputs and attributes,
    and returns the outputs.

    This function is useful for quickly testing the behavior of individual ONNX
    operators with specific inputs without needing to build complex models.

    Args:
        op_name (str): The name of the ONNX operator (e.g., "Add", "Relu", "MatMul", "Cast").
        inputs (dict): A dictionary where keys are input names (str) and
                            values are the input data. The values can be:
                            - `numpy.ndarray`: Directly used as input.
                            - `int` or `float`: Converted to a scalar `numpy.ndarray`
                              (e.g., `np.int64` for integers, `np.float32` for floats).
                            - `list`: Converted to a `numpy.ndarray`. The dtype will be
                              inferred (e.g., `np.float32` if all elements are floats,
                              `np.int64` if all elements are integers).
        output_shapes (list of int, str, or None, optional): The explicit list of shapes for the output tensors
            in the ONNX graph definition.
             - `int`: for fixed dimensions (e.g., 64).
             - `str`: for symbolic dimensions (e.g., "batch_size").
             - `None`: for dynamic dimensions.
        output_dtypes (int, optional): The explicit ONNX TensorProto.DataType for the output tensors
                                                in the ONNX graph definition.
        attributes (dict, optional): A dictionary where keys are attribute names (str)
                                     and values are the attribute values. These are
                                     passed directly to `onnx.helper.make_node`.
                                     Defaults to an empty dictionary.
        opset_version (int): The ONNX opset version to use when creating the model.
                             Defaults to 15 for broader compatibility with older ONNX Runtime versions.
        ir_version (int): The ONNX IR (Intermediate Representation) version to use when creating the model.
                          Defaults to 10 for compatibility with ONNX Runtime 1.22.

    Returns:
        list: A list of output tensors (numpy arrays) from ONNX Runtime execution.
              The number of elements in the list corresponds to the number of outputs
              of the specified ONNX operator.

    Raises:
        ValueError: If the input dictionary is empty, or if an unsupported
                    numpy dtype is encountered during conversion.
        TypeError: If an unsupported Python input type (not ndarray, int, float, or list)
                   is provided, or if a list contains non-numeric types.
        Exception: Catches and re-raises any general errors that occur during
                   ONNX model creation, saving, or runtime execution.
    """
    if attributes is None:
        attributes = {}

    temp_onnx_file = None
    try:
        graph_inputs = []
        onnx_runtime_inputs = {}
        input_names = []

        # To infer the output type for the ONNX graph definition, we'll use the
        # ONNX data type of the *first* input provided. This works for many
        # common operations where the output type is derived from the input types.
        first_input_onnx_type = None
        first_input_actual_shape = None # To store the actual shape of the first NumPy input


        if not inputs:
            raise ValueError("The input dictionary cannot be empty. Please provide at least one input.")

        # Process each input to prepare for ONNX graph definition and ONNX Runtime execution.
        for i, (name, value) in enumerate(inputs.items()):
            input_names.append(name) # Collect input names for the ONNX node
            np_array_value = None

            if isinstance(value, np.ndarray):
                # If already a numpy array, use it directly.
                np_array_value = value
            elif isinstance(value, (int, float)):
                # Convert scalar Python numbers to appropriate numpy arrays.
                if isinstance(value, int):
                    np_array_value = np.array(value, dtype=np.int64)
                else:  # float
                    np_array_value = np.array(value, dtype=np.float32)
            elif isinstance(value, list):
                # Convert Python lists to numpy arrays.
                # Attempt to infer a suitable numeric dtype for the list.
                inferred_dtype = None
                if all(isinstance(x, float) for x in value):
                    inferred_dtype = np.float32
                elif all(isinstance(x, int) for x in value):
                    inferred_dtype = np.int64
                
                if inferred_dtype:
                    np_array_value = np.array(value, dtype=inferred_dtype)
                else:
                    # Fallback for mixed numeric or other types.
                    # This might fail for non-numeric lists if the ONNX op expects numbers.
                    np_array_value = np.array(value)
                    if not np.issubdtype(np_array_value.dtype, np.number):
                        raise TypeError(
                            f"List input '{name}' contains non-numeric types after conversion to numpy array. "
                            "Most ONNX operators require numeric tensor inputs."
                        )
            else:
                raise TypeError(f"Unsupported input type for '{name}': {type(value)}. "
                                 "Expected `numpy.ndarray`, `int`, `float`, or `list`.")

            # Store the prepared numpy array to be fed into ONNX Runtime.
            onnx_runtime_inputs[name] = np_array_value

            # Convert the numpy dtype to its ONNX equivalent.
            onnx_dtype = numpy_to_onnx_tensor_type(np_array_value.dtype)
            
            # Create a `ValueInfoProto` object for the ONNX graph's input definition.
            # `list(np_array_value.shape)` converts the tuple shape to a list, which ONNX expects.
            graph_inputs.append(onnx.helper.make_tensor_value_info(
                name, onnx_dtype, list(np_array_value.shape)
            ))

            # If this is the first input, store its ONNX type and actual shape for output definition.
            if i == 0:
                first_input_onnx_type = onnx_dtype
                first_input_actual_shape = np_array_value.shape


        # Ensure we have a valid type for output definition.
        if first_input_onnx_type is None or first_input_actual_shape is None:
             raise ValueError("Could not determine a primary input type or shape to infer the output. "
                              "This typically means the input dictionary was processed unexpectedly.")

        # Create a `ValueInfoProto` object for the ONNX graph's output definition.
        # The shape and dtype are now explicitly set based on parameters or robust inference.
        num_outputs = len(output_shapes)
        graph_outputs = []
        output_names = []

        for outputIdx in range(num_outputs):
            output_name = "output_{}".format(outputIdx)
            output_names.append(output_name)
            graph_outputs.append(
                onnx.helper.make_tensor_value_info(output_name,
                    output_dtypes[outputIdx], output_shapes[outputIdx]))

        # Create the ONNX `NodeProto` for the specified operator.
        # This defines the operation itself within the graph.
        # Pass the 'attributes' dictionary directly to make_node.
        node = onnx.helper.make_node(op_name, inputs=input_names, outputs=output_names, **attributes)

        # Create the ONNX `GraphProto`. A graph contains nodes, inputs, and outputs.
        graph = onnx.helper.make_graph(
            [node], # List of nodes in the graph (in this case, just one)
            f"{op_name}_graph", # A descriptive name for the graph
            graph_inputs,       # List of graph inputs
            graph_outputs,      # List of graph outputs
        )

        # Create the complete ONNX `ModelProto`. A model contains a graph and metadata.
        # Explicitly set opset_imports to target the specified opset_version.
        # Explicitly set ir_version for compatibility.
        model = onnx.helper.make_model(graph, producer_name="single_op_test_utility",
                                       opset_imports=[onnx.helper.make_opsetid("", opset_version)],
                                       ir_version=ir_version)

        # Validate the generated ONNX model to ensure it's well-formed according to ONNX specifications.
        # This helps catch issues before attempting execution.
        onnx.checker.check_model(model)

        # Save the ONNX model to a temporary file. This is necessary because
        # ONNX Runtime typically loads models from file paths.
        with tempfile.NamedTemporaryFile(suffix=".onnx", delete=False) as f:
            temp_onnx_file = f.name
            onnx.save(model, temp_onnx_file)

        # Initialize the ONNX Runtime session.
        # `providers=onnxruntime.get_available_providers()` ensures that the session
        # uses the best available execution provider (e.g., CUDA if available, otherwise CPU).
        session = onnxruntime.InferenceSession(temp_onnx_file, providers=onnxruntime.get_available_providers())
        
        # Get the actual output names from the ONNX Runtime session.
        # This is a robust way to ensure we request the correct output names,
        # as internal optimizations or different ONNX versions might sometimes
        # subtly change output names (though unlikely for a single-op model).
        session_output_names = [output.name for output in session.get_outputs()]

        # Execute the model with the prepared inputs.
        # The `run` method returns a list of numpy arrays, one for each output.
        outputs = session.run(session_output_names, onnx_runtime_inputs)

        return outputs

    except Exception as e:
        # Print the error message for debugging purposes.
        print(f"An error occurred during ONNX single-op execution: {e}")
        # Re-raise the exception so the caller is aware of the failure.
        raise
    finally:
        # Ensure the temporary ONNX file is deleted, regardless of whether
        # the execution was successful or an error occurred.
        if temp_onnx_file and os.path.exists(temp_onnx_file):
            os.remove(temp_onnx_file)

# --- Example Usage ---
if __name__ == "__main__":
    print("--- Testing 'Add' operator ---")
    # Example 1: Add two numpy arrays (using default opset_version=15, ir_version=10)
    inputs_add_np = {
        'A': np.array([1.0, 2.0, 3.0], dtype=np.float32),
        'B': np.array([4.0, 5.0, 6.0], dtype=np.float32)
    }
    outputs_add_np = run_single_onnx_op("Add", inputs_add_np, [[3]], [onnx.TensorProto.FLOAT])
    print(f"Inputs: {inputs_add_np}")
    print(f"Output for 'Add':\n  Value: {outputs_add_np[0]}\n  Shape: {outputs_add_np[0].shape}\n  Dtype: {outputs_add_np[0].dtype}")
    # Expected Value: [5.0, 7.0, 9.0], Shape: (3,), Dtype: float32

    # Example 2: Add two scalar Python numbers (using default opset_version=15, ir_version=10)
    inputs_add_scalars = {
        'A': 5.0,
        'B': 3.0
    }
    outputs_add_scalars = run_single_onnx_op("Add", inputs_add_scalars, [[]], [onnx.TensorProto.FLOAT])
    print(f"\nInputs: {inputs_add_scalars}")
    print(f"Output for 'Add' (scalars):\n  Value: {outputs_add_scalars[0]}\n  Shape: {outputs_add_scalars[0].shape}\n  Dtype: {outputs_add_scalars[0].dtype}")
    # Expected Value: 8.0, Shape: (), Dtype: float32 (or int64 depending on initial scalar type)


    print("\n--- Testing 'Relu' operator ---")
    # Example 3: Relu with a numpy array containing negative, zero, and positive values
    inputs_relu = {
        'X': np.array([-1.0, 0.0, 1.0, -5.0, 2.5], dtype=np.float32)
    }
    outputs_relu = run_single_onnx_op("Relu", inputs_relu, [[5]], [onnx.TensorProto.FLOAT])
    print(f"Inputs: {inputs_relu}")
    print(f"Output for 'Relu':\n  Value: {outputs_relu[0]}\n  Shape: {outputs_relu[0].shape}\n  Dtype: {outputs_relu[0].dtype}")
    # Expected Value: [0.0, 0.0, 1.0, 0.0, 2.5], Shape: (5,), Dtype: float32


    print("\n--- Testing 'MatMul' operator ---")
    # Example 4: Matrix Multiplication
    inputs_matmul = {
        'A': np.array([[1.0, 2.0], [3.0, 4.0]], dtype=np.float32),
        'B': np.array([[5.0], [6.0]], dtype=np.float32)
    }
    outputs_matmul = run_single_onnx_op("MatMul", inputs_matmul)
    print(f"Inputs: {inputs_matmul}")
    print(f"Output for 'MatMul':\n  Value:\n{outputs_matmul[0]}\n  Shape: {outputs_matmul[0].shape}\n  Dtype: {outputs_matmul[0].dtype}")
    # Expected Value: [[17.0], [39.0]], Shape: (2, 1), Dtype: float32


    print("\n--- Testing 'Cast' operator (int to float) ---")
    # Example 5: Cast operator, converting int64 to float32
    inputs_cast = {
        'input': np.array([1, 2, 3], dtype=np.int64)
    }
    attributes_cast = {
        'to': onnx.TensorProto.FLOAT # This attribute defines the output type of the Cast op
    }
    # We also explicitly set output_dtypes to match the 'to' attribute for graph definition
    outputs_cast = run_single_onnx_op("Cast", inputs_cast, attributes=attributes_cast, output_dtypes=onnx.TensorProto.FLOAT)
    print(f"Inputs: {inputs_cast}")
    print(f"Output for 'Cast' (int64 to float32):\n  Value: {outputs_cast[0]}\n  Shape: {outputs_cast[0].shape}\n  Dtype: {outputs_cast[0].dtype}")
    # Expected Value: [1.0, 2.0, 3.0], Shape: (3,), Dtype: float32


    print("\n--- Testing 'ReduceSum' operator with 'axis' attribute ---")
    # Example 6: ReduceSum along a specific axis
    inputs_reducesum = {
        'data': np.array([[1, 2, 3], [4, 5, 6]], dtype=np.float32),
        'axes': [1],
    }
    attributes_reducesum = {
        'keepdims': 1 # This means the reduced dimension will still be present as a 1
    }
    outputs_reducesum = run_single_onnx_op("ReduceSum", inputs_reducesum, attributes=attributes_reducesum)
    print(f"Inputs: {inputs_reducesum}")
    print(f"Output for 'ReduceSum' (axis=1, keepdims=1):\n  Value:\n{outputs_reducesum[0]}\n  Shape: {outputs_reducesum[0].shape}\n  Dtype: {outputs_reducesum[0].dtype}")
    # Expected Value: [[6.], [15.]], Shape: (2, 1), Dtype: float32


    print("\n--- Testing 'ReduceSum' operator without 'keepdims' (default 1) ---")
    # Example 7: ReduceSum along a specific axis without keepdims
    inputs_reducesum_no_keepdims = {
        'data': np.array([[1, 2, 3], [4, 5, 6]], dtype=np.float32),
        'axes': [1],
    }
    attributes_reducesum_no_keepdims = {
        'keepdims': 0 # This means the reduced dimension will be removed
    }
    outputs_reducesum_no_keepdims = run_single_onnx_op("ReduceSum", inputs_reducesum_no_keepdims, attributes=attributes_reducesum_no_keepdims)
    print(f"Inputs: {inputs_reducesum_no_keepdims}")
    print(f"Output for 'ReduceSum' (axis=1, keepdims=0):\n  Value: {outputs_reducesum_no_keepdims[0]}\n  Shape: {outputs_reducesum_no_keepdims[0].shape}\n  Dtype: {outputs_reducesum_no_keepdims[0].dtype}")
    # Expected Value: [6., 15.], Shape: (2,), Dtype: float32


    print("\n--- Testing 'Add' operator with a specific opset version (e.g., 10) and IR version (e.g., 7) ---")
    # Example 8: Using opset_version=10 and ir_version=7 explicitly
    inputs_add_opset_ir_test = {
        'A': np.array([10.0, 20.0], dtype=np.float32),
        'B': np.array([1.0, 2.0], dtype=np.float32)
    }
    outputs_add_opset_ir_test = run_single_onnx_op("Add", inputs_add_opset_ir_test, opset_version=10, ir_version=7)
    print(f"Inputs: {inputs_add_opset_ir_test}")
    print(f"Output for 'Add' (opset_version=10, ir_version=7):\n  Value: {outputs_add_opset_ir_test[0]}\n  Shape: {outputs_add_opset_ir_test[0].shape}\n  Dtype: {outputs_add_opset_ir_test[0].dtype}")
    # Expected Value: [11.0, 22.0], Shape: (2,), Dtype: float32


    print("\n--- Testing 'Shape' operator with explicit output_shapes and output_dtypes ---")
    # Example 9: Testing 'Shape' operator, which returns a 1D tensor of long integers
    inputs_shape_op = {
        'data': np.array([[1.0, 2.0], [3.0, 4.0], [5.0, 6.0]], dtype=np.float32)
    }
    # The output of 'Shape' is a 1D tensor representing the input's shape.
    # Its length depends on the input's rank, so it's dynamic.
    # Its dtype is always INT64.
    outputs_shape_op = run_single_onnx_op("Shape", inputs_shape_op,
                                            output_shapes=["num_dims"], # Symbolic dynamic dimension
                                            output_dtypes=onnx.TensorProto.INT64) # Explicit output dtype
    print(f"Inputs: {inputs_shape_op}")
    print(f"Output for 'Shape':\n  Value: {outputs_shape_op[0]}\n  Shape: {outputs_shape_op[0].shape}\n  Dtype: {outputs_shape_op[0].dtype}")
    # Expected Value: [3, 2], Shape: (2,), Dtype: int64


    print("\n--- Testing with unsupported input type ---")
    try:
        inputs_unsupported = {
            'text_input': "hello world"
        }
        outputs_unsupported = run_single_onnx_op("Identity", inputs_unsupported)
        print(f"Output for unsupported type: {outputs_unsupported[0]}")
    except TypeError as e:
        print(f"Caught expected error for unsupported type: {e}")
    except Exception as e:
        print(f"Caught unexpected error for unsupported type: {e}")

    print("\n--- Testing with empty inputs ---")
    try:
        run_single_onnx_op("Identity", {})
    except ValueError as e:
        print(f"Caught expected error for empty inputs: {e}")
    except Exception as e:
        print(f"Caught unexpected error for empty inputs: {e}")
