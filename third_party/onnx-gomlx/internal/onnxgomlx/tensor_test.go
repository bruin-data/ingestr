package onnxgomlx

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/support/testutil"
	"github.com/gomlx/onnx-gomlx/internal/onnxgomlx/filesreader"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/gomlx/gomlx/backends/default"
)

// TestShape tests the Shape() function that converts ONNX TensorProto to GoMLX shapes.Shape
func TestShape(t *testing.T) {
	t.Run("NilProto", func(t *testing.T) {
		_, err := Shape(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("Float32Scalar", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:     []int64{},
			DataType: int32(protos.TensorProto_FLOAT),
		}
		shape, err := Shape(proto)
		require.NoError(t, err)
		require.Equal(t, dtypes.Float32, shape.DType)
		require.Equal(t, 0, shape.Rank())
	})

	t.Run("Float32_1D", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:     []int64{5},
			DataType: int32(protos.TensorProto_FLOAT),
		}
		shape, err := Shape(proto)
		require.NoError(t, err)
		require.Equal(t, dtypes.Float32, shape.DType)
		require.Equal(t, []int{5}, shape.Dimensions)
	})

	t.Run("Int32_2D", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:     []int64{3, 4},
			DataType: int32(protos.TensorProto_INT32),
		}
		shape, err := Shape(proto)
		require.NoError(t, err)
		require.Equal(t, dtypes.Int32, shape.DType)
		require.Equal(t, []int{3, 4}, shape.Dimensions)
	})

	t.Run("Int64_4D", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:     []int64{2, 3, 4, 5},
			DataType: int32(protos.TensorProto_INT64),
		}
		shape, err := Shape(proto)
		require.NoError(t, err)
		require.Equal(t, dtypes.Int64, shape.DType)
		require.Equal(t, []int{2, 3, 4, 5}, shape.Dimensions)
	})

	t.Run("SegmentedTensorNotSupported", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:     []int64{10},
			DataType: int32(protos.TensorProto_FLOAT),
			Segment:  &protos.TensorProto_Segment{Begin: 0, End: 5},
		}
		_, err := Shape(proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "segmented tensor not supported")
	})
}

// TestSparseShape tests the SparseShape() function
func TestSparseShape(t *testing.T) {
	t.Run("NilProto", func(t *testing.T) {
		_, err := SparseShape(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("NilValues", func(t *testing.T) {
		proto := &protos.SparseTensorProto{
			Values:  nil,
			Indices: &protos.TensorProto{},
			Dims:    []int64{3, 3},
		}
		_, err := SparseShape(proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("NilIndices", func(t *testing.T) {
		proto := &protos.SparseTensorProto{
			Values:  &protos.TensorProto{DataType: int32(protos.TensorProto_FLOAT)},
			Indices: nil,
			Dims:    []int64{3, 3},
		}
		_, err := SparseShape(proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("ValidSparseFloat32", func(t *testing.T) {
		proto := &protos.SparseTensorProto{
			Values: &protos.TensorProto{
				DataType: int32(protos.TensorProto_FLOAT),
			},
			Indices: &protos.TensorProto{
				DataType: int32(protos.TensorProto_INT64),
			},
			Dims: []int64{10, 20},
		}
		shape, err := SparseShape(proto)
		require.NoError(t, err)
		require.Equal(t, dtypes.Float32, shape.DType)
		require.Equal(t, []int{10, 20}, shape.Dimensions)
	})
}

// TestTensorToGoMLX tests the tensorToGoMLX() function for ONNX→GoMLX conversion
func TestTensorToGoMLX(t *testing.T) {
	backend := testutil.BuildTestBackend()

	t.Run("NilProto", func(t *testing.T) {
		_, err := tensorToGoMLX(backend, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("FloatData_Float32", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:      []int64{2, 2},
			DataType:  int32(protos.TensorProto_FLOAT),
			FloatData: []float32{1.0, 2.0, 3.0, 4.0},
		}
		tensor, err := tensorToGoMLX(backend, proto)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll() // Memory leak detection

		require.Equal(t, dtypes.Float32, tensor.Shape().DType)
		require.Equal(t, []int{2, 2}, tensor.Shape().Dimensions)
		data := tensors.MustCopyFlatData[float32](tensor)
		require.Equal(t, []float32{1.0, 2.0, 3.0, 4.0}, data)
	})

	t.Run("Int32Data_Int32", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:      []int64{3},
			DataType:  int32(protos.TensorProto_INT32),
			Int32Data: []int32{10, 20, 30},
		}
		tensor, err := tensorToGoMLX(backend, proto)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Int32, tensor.Shape().DType)
		require.Equal(t, []int{3}, tensor.Shape().Dimensions)
		data := tensors.MustCopyFlatData[int32](tensor)
		require.Equal(t, []int32{10, 20, 30}, data)
	})

	t.Run("Int64Data_Int64", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:      []int64{2},
			DataType:  int32(protos.TensorProto_INT64),
			Int64Data: []int64{100, 200},
		}
		tensor, err := tensorToGoMLX(backend, proto)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Int64, tensor.Shape().DType)
		data := tensors.MustCopyFlatData[int64](tensor)
		require.Equal(t, []int64{100, 200}, data)
	})

	t.Run("DTypeConversion_Int64ToInt32", func(t *testing.T) {
		// ONNX proto has int64 data but requests int32 dtype
		proto := &protos.TensorProto{
			Dims:      []int64{2},
			DataType:  int32(protos.TensorProto_INT32),
			Int64Data: []int64{5, 10},
		}
		tensor, err := tensorToGoMLX(backend, proto)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Int32, tensor.Shape().DType)
		data := tensors.MustCopyFlatData[int32](tensor)
		require.Equal(t, []int32{5, 10}, data)
	})

	t.Run("RawData_Float32", func(t *testing.T) {
		// Create raw bytes for float32 data
		data := []float32{1.5, 2.5, 3.5, 4.5}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}

		proto := &protos.TensorProto{
			Dims:     []int64{4},
			DataType: int32(protos.TensorProto_FLOAT),
			RawData:  rawBytes,
		}
		tensor, err := tensorToGoMLX(backend, proto)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Float32, tensor.Shape().DType)
		result := tensors.MustCopyFlatData[float32](tensor)
		require.InDeltaSlice(t, data, result, 0.0001)
	})

	t.Run("RawData_Int8_Quantized", func(t *testing.T) {
		// Quantized int8 data (common in quantized models)
		proto := &protos.TensorProto{
			Dims:     []int64{4},
			DataType: int32(protos.TensorProto_INT8),
			RawData:  []byte{128, 255, 0, 127}, // -128, -1, 0, 127 as unsigned bytes
		}
		tensor, err := tensorToGoMLX(backend, proto)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Int8, tensor.Shape().DType)
		result := tensors.MustCopyFlatData[int8](tensor)
		require.Equal(t, []int8{-128, -1, 0, 127}, result)
	})

	t.Run("StringDataNotSupported", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:       []int64{2},
			DataType:   int32(protos.TensorProto_STRING),
			StringData: [][]byte{[]byte("hello"), []byte("world")},
		}
		_, err := tensorToGoMLX(backend, proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported")
	})

	t.Run("ExternalDataRequiresBaseDir", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "external.bin"},
			},
		}
		// tensorToGoMLX should return an error for external data
		// since it doesn't have access to the base directory
		_, err := tensorToGoMLX(backend, proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "external data")
	})

	t.Run("SizeMismatch", func(t *testing.T) {
		proto := &protos.TensorProto{
			Dims:      []int64{2, 2}, // Expects 4 elements
			DataType:  int32(protos.TensorProto_FLOAT),
			FloatData: []float32{1.0, 2.0}, // Only 2 elements
		}
		_, err := tensorToGoMLX(backend, proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "size")
	})
}

// TestTensorValueToONNX tests the TensorValueToONNX() function for GoMLX→ONNX conversion
func TestTensorValueToONNX(t *testing.T) {
	t.Run("Float32Copy", func(t *testing.T) {
		// Create GoMLX tensor
		gomlxTensor := tensors.FromFlatDataAndDimensions([]float32{1.0, 2.0, 3.0, 4.0}, 2, 2)
		defer gomlxTensor.FinalizeAll()

		// Create ONNX proto with matching shape
		proto := &protos.TensorProto{
			Dims:      []int64{2, 2},
			DataType:  int32(protos.TensorProto_FLOAT),
			FloatData: make([]float32, 4),
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		require.Equal(t, []float32{1.0, 2.0, 3.0, 4.0}, proto.FloatData)
	})

	t.Run("Int32Copy", func(t *testing.T) {
		gomlxTensor := tensors.FromFlatDataAndDimensions([]int32{10, 20, 30}, 3)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:      []int64{3},
			DataType:  int32(protos.TensorProto_INT32),
			Int32Data: make([]int32, 3),
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		require.Equal(t, []int32{10, 20, 30}, proto.Int32Data)
	})

	t.Run("RawDataCopy", func(t *testing.T) {
		gomlxTensor := tensors.FromFlatDataAndDimensions([]float32{1.5, 2.5}, 2)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			RawData:  make([]byte, 8), // 2 floats * 4 bytes
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		require.Len(t, proto.RawData, 8)
		// Verify non-zero data was copied
		hasNonZero := false
		for _, b := range proto.RawData {
			if b != 0 {
				hasNonZero = true
				break
			}
		}
		require.True(t, hasNonZero, "RawData should contain non-zero bytes")
	})

	t.Run("ShapeMismatch", func(t *testing.T) {
		gomlxTensor := tensors.FromFlatDataAndDimensions([]float32{1.0, 2.0}, 2)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:      []int64{3}, // Different shape
			DataType:  int32(protos.TensorProto_FLOAT),
			FloatData: make([]float32, 3),
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot copy")
	})

	// Test dtype conversion in checkAndCopyTensorToProto (the critical path with simplego backend)
	t.Run("DTypeConversion_Int32ToFloat32", func(t *testing.T) {
		// GoMLX tensor is int32, but ONNX proto wants to store it as float32
		// This tests the conversion path with simplego backend
		gomlxTensor := tensors.FromFlatDataAndDimensions([]int32{1, 2, 3, 4}, 2, 2)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:      []int64{2, 2},
			DataType:  int32(protos.TensorProto_INT32),
			FloatData: make([]float32, 4), // Storage type differs from proto dtype
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		require.Equal(t, []float32{1.0, 2.0, 3.0, 4.0}, proto.FloatData)
	})

	t.Run("DTypeConversion_Float64ToFloat32", func(t *testing.T) {
		// GoMLX tensor is float64, ONNX proto storage is float32
		gomlxTensor := tensors.FromFlatDataAndDimensions([]float64{1.5, 2.5, 3.5}, 3)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:      []int64{3},
			DataType:  int32(protos.TensorProto_DOUBLE),
			FloatData: make([]float32, 3), // Storage type differs from proto dtype
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		require.InDeltaSlice(t, []float32{1.5, 2.5, 3.5}, proto.FloatData, 0.0001)
	})

	t.Run("DTypeConversion_Int64ToInt32", func(t *testing.T) {
		// GoMLX tensor is int64, ONNX proto storage is int32
		gomlxTensor := tensors.FromFlatDataAndDimensions([]int64{100, 200, 300}, 3)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:      []int64{3},
			DataType:  int32(protos.TensorProto_INT64),
			Int32Data: make([]int32, 3), // Storage type differs from proto dtype
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		require.Equal(t, []int32{100, 200, 300}, proto.Int32Data)
	})

	t.Run("DTypeConversion_Float32ToInt32", func(t *testing.T) {
		// GoMLX tensor is float32, ONNX proto storage is int32 (with truncation)
		gomlxTensor := tensors.FromFlatDataAndDimensions([]float32{1.9, 2.1, 3.7, 4.2}, 2, 2)
		defer gomlxTensor.FinalizeAll()

		proto := &protos.TensorProto{
			Dims:      []int64{2, 2},
			DataType:  int32(protos.TensorProto_FLOAT),
			Int32Data: make([]int32, 4), // Storage type differs from proto dtype
		}

		err := TensorValueToONNX(gomlxTensor, proto)
		require.NoError(t, err)
		// Conversion truncates floats to ints
		require.Equal(t, []int32{1, 2, 3, 4}, proto.Int32Data)
	})
}

// TestRoundTripConversion tests GoMLX→ONNX→GoMLX conversion preserves data
func TestRoundTripConversion(t *testing.T) {
	backend := testutil.BuildTestBackend()

	tests := []struct {
		name      string
		original  *tensors.Tensor
		onnxDType protos.TensorProto_DataType
		makeProto func(dims []int64, size int) *protos.TensorProto
	}{
		{
			name:      "Float32_2D",
			original:  tensors.FromFlatDataAndDimensions([]float32{1.0, 2.0, 3.0, 4.0}, 2, 2),
			onnxDType: protos.TensorProto_FLOAT,
			makeProto: func(dims []int64, size int) *protos.TensorProto {
				return &protos.TensorProto{
					Dims:      dims,
					DataType:  int32(protos.TensorProto_FLOAT),
					FloatData: make([]float32, size),
				}
			},
		},
		{
			name:      "Int32_1D",
			original:  tensors.FromFlatDataAndDimensions([]int32{10, 20, 30}, 3),
			onnxDType: protos.TensorProto_INT32,
			makeProto: func(dims []int64, size int) *protos.TensorProto {
				return &protos.TensorProto{
					Dims:      dims,
					DataType:  int32(protos.TensorProto_INT32),
					Int32Data: make([]int32, size),
				}
			},
		},
		{
			name:      "Int64_Scalar",
			original:  tensors.FromFlatDataAndDimensions([]int64{42}, 1),
			onnxDType: protos.TensorProto_INT64,
			makeProto: func(dims []int64, size int) *protos.TensorProto {
				return &protos.TensorProto{
					Dims:      dims,
					DataType:  int32(protos.TensorProto_INT64),
					Int64Data: make([]int64, size),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer tt.original.FinalizeAll()

			// Convert GoMLX → ONNX
			shape := tt.original.Shape()
			dims := make([]int64, len(shape.Dimensions))
			for i, d := range shape.Dimensions {
				dims[i] = int64(d)
			}
			proto := tt.makeProto(dims, shape.Size())

			err := TensorValueToONNX(tt.original, proto)
			require.NoError(t, err)

			// Convert ONNX → GoMLX
			recovered, err := tensorToGoMLX(backend, proto)
			require.NoError(t, err)
			require.NotNil(t, recovered)
			defer recovered.FinalizeAll()

			// Verify shapes match
			require.Equal(t, tt.original.Shape(), recovered.Shape())

			// Verify data matches based on dtype
			switch tt.onnxDType {
			case protos.TensorProto_FLOAT:
				originalData := tensors.MustCopyFlatData[float32](tt.original)
				recoveredData := tensors.MustCopyFlatData[float32](recovered)
				require.Equal(t, originalData, recoveredData)
			case protos.TensorProto_INT32:
				originalData := tensors.MustCopyFlatData[int32](tt.original)
				recoveredData := tensors.MustCopyFlatData[int32](recovered)
				require.Equal(t, originalData, recoveredData)
			case protos.TensorProto_INT64:
				originalData := tensors.MustCopyFlatData[int64](tt.original)
				recoveredData := tensors.MustCopyFlatData[int64](recovered)
				require.Equal(t, originalData, recoveredData)
			}
		})
	}
}

// TestParseExternalData tests the parseExternalData function
func TestParseExternalData(t *testing.T) {
	t.Run("NoExternalData", func(t *testing.T) {
		proto := &protos.TensorProto{
			Name: "test",
		}
		_, err := parseExternalData(proto)
		require.ErrorIs(t, err, ErrNoExternalData)
	})

	t.Run("LocationOnly", func(t *testing.T) {
		proto := &protos.TensorProto{
			Name: "test",
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
			},
		}
		info, err := parseExternalData(proto)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "weights.bin", info.Location)
		require.Equal(t, int64(0), info.Offset)
		require.Equal(t, int64(-1), info.Length)
	})

	t.Run("AllFields", func(t *testing.T) {
		proto := &protos.TensorProto{
			Name: "test",
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
				{Key: "offset", Value: "1024"},
				{Key: "length", Value: "4096"},
				{Key: "checksum", Value: "abc123"}, // Should be ignored
			},
		}
		info, err := parseExternalData(proto)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "weights.bin", info.Location)
		require.Equal(t, int64(1024), info.Offset)
		require.Equal(t, int64(4096), info.Length)
	})

	t.Run("MissingLocation", func(t *testing.T) {
		proto := &protos.TensorProto{
			Name: "test",
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "offset", Value: "1024"},
			},
		}
		_, err := parseExternalData(proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing required 'location'")
	})

	t.Run("InvalidOffset", func(t *testing.T) {
		proto := &protos.TensorProto{
			Name: "test",
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
				{Key: "offset", Value: "not-a-number"},
			},
		}
		_, err := parseExternalData(proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid offset")
	})

	t.Run("InvalidLength", func(t *testing.T) {
		proto := &protos.TensorProto{
			Name: "test",
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
				{Key: "length", Value: "not-a-number"},
			},
		}
		_, err := parseExternalData(proto)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid length")
	})
}

// TestTensorToGoMLXWithBaseDir_ExternalData tests external data loading
func TestTensorToGoMLXWithBaseDir_ExternalData(t *testing.T) {
	backend := testutil.BuildTestBackend()

	t.Run("BasicExternalData", func(t *testing.T) {
		// Create a temporary directory for the test
		tmpDir := t.TempDir()

		// Create external data file with float32 values
		data := []float32{1.5, 2.5, 3.5, 4.5}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		externalFile := filepath.Join(tmpDir, "weights.bin")
		err := os.WriteFile(externalFile, rawBytes, 0644)
		require.NoError(t, err)

		// Create proto with external data reference
		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{4},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
			},
		}

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		// Load tensor
		tensor, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Float32, tensor.Shape().DType)
		require.Equal(t, []int{4}, tensor.Shape().Dimensions)
		result := tensors.MustCopyFlatData[float32](tensor)
		require.InDeltaSlice(t, data, result, 0.0001)
	})

	t.Run("ExternalDataWithOffset", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create external data file with padding + float32 values
		padding := make([]byte, 128) // Some padding at the start
		data := []float32{10.0, 20.0}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		fileContent := append(padding, rawBytes...)
		externalFile := filepath.Join(tmpDir, "weights.bin")
		err := os.WriteFile(externalFile, fileContent, 0644)
		require.NoError(t, err)

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
				{Key: "offset", Value: "128"},
				{Key: "length", Value: "8"},
			},
		}

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		tensor, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		result := tensors.MustCopyFlatData[float32](tensor)
		require.InDeltaSlice(t, data, result, 0.0001)
	})

	t.Run("ExternalDataInt32", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create external data file with int32 values
		data := []int32{100, 200, 300}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := uint32(val)
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		externalFile := filepath.Join(tmpDir, "data.bin")
		err := os.WriteFile(externalFile, rawBytes, 0644)
		require.NoError(t, err)

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{3},
			DataType: int32(protos.TensorProto_INT32),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "data.bin"},
			},
		}

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		tensor, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Int32, tensor.Shape().DType)
		result := tensors.MustCopyFlatData[int32](tensor)
		require.Equal(t, data, result)
	})

	t.Run("SubdirectoryLocation", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create subdirectory and external data file
		subDir := filepath.Join(tmpDir, "weights")
		err := os.MkdirAll(subDir, 0755)
		require.NoError(t, err)

		data := []float32{5.0, 6.0}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		externalFile := filepath.Join(subDir, "layer1.bin")
		err = os.WriteFile(externalFile, rawBytes, 0644)
		require.NoError(t, err)

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights/layer1.bin"},
			},
		}

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		tensor, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		result := tensors.MustCopyFlatData[float32](tensor)
		require.InDeltaSlice(t, data, result, 0.0001)
	})

	t.Run("MissingFile", func(t *testing.T) {
		tmpDir := t.TempDir()

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "nonexistent.bin"},
			},
		}

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		_, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to open external data file")
	})

	t.Run("SizeMismatch", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create file with wrong size (2 floats instead of 4)
		data := []float32{1.0, 2.0}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		externalFile := filepath.Join(tmpDir, "weights.bin")
		err := os.WriteFile(externalFile, rawBytes, 0644)
		require.NoError(t, err)

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{4}, // Expects 4 floats = 16 bytes
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
			},
		}

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		_, err = ONNXTensorToGoMLX(backend, proto, reader)
		require.Error(t, err)
		require.Contains(t, err.Error(), "bytes")
	})
}

// TestExternalDataReader tests the ExternalDataReader mmap functionality
func TestExternalDataReader(t *testing.T) {
	backend := testutil.BuildTestBackend()

	t.Run("BasicMmapRead", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create external data file with float32 values
		data := []float32{1.5, 2.5, 3.5, 4.5}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		externalFile := filepath.Join(tmpDir, "weights.bin")
		err := os.WriteFile(externalFile, rawBytes, 0644)
		require.NoError(t, err)

		// Create reader and use it
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{4},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "weights.bin"},
			},
		}

		tensor, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		require.Equal(t, dtypes.Float32, tensor.Shape().DType)
		require.Equal(t, []int{4}, tensor.Shape().Dimensions)
		result := tensors.MustCopyFlatData[float32](tensor)
		require.InDeltaSlice(t, data, result, 0.0001)
	})

	t.Run("MultipleTensorsShareSameFile", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create external data file with multiple tensors' data
		// Tensor 1: 2 floats at offset 0
		// Tensor 2: 3 floats at offset 8
		data1 := []float32{1.0, 2.0}
		data2 := []float32{3.0, 4.0, 5.0}
		rawBytes := make([]byte, (len(data1)+len(data2))*4)

		for i, val := range data1 {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		for i, val := range data2 {
			offset := (len(data1) + i) * 4
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[offset] = byte(bits)
			rawBytes[offset+1] = byte(bits >> 8)
			rawBytes[offset+2] = byte(bits >> 16)
			rawBytes[offset+3] = byte(bits >> 24)
		}
		externalFile := filepath.Join(tmpDir, "shared.bin")
		err := os.WriteFile(externalFile, rawBytes, 0644)
		require.NoError(t, err)

		// Create reader
		reader := filesreader.New(tmpDir)
		defer reader.Close()

		// Load first tensor
		proto1 := &protos.TensorProto{
			Name:     "tensor1",
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "shared.bin"},
				{Key: "offset", Value: "0"},
				{Key: "length", Value: "8"},
			},
		}
		tensor1, err := ONNXTensorToGoMLX(backend, proto1, reader)
		require.NoError(t, err)
		defer tensor1.FinalizeAll()

		// Load second tensor
		proto2 := &protos.TensorProto{
			Name:     "tensor2",
			Dims:     []int64{3},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "shared.bin"},
				{Key: "offset", Value: "8"},
				{Key: "length", Value: "12"},
			},
		}
		tensor2, err := ONNXTensorToGoMLX(backend, proto2, reader)
		require.NoError(t, err)
		defer tensor2.FinalizeAll()

		// Verify both tensors have correct data
		result1 := tensors.MustCopyFlatData[float32](tensor1)
		require.InDeltaSlice(t, data1, result1, 0.0001)

		result2 := tensors.MustCopyFlatData[float32](tensor2)
		require.InDeltaSlice(t, data2, result2, 0.0001)
	})

	t.Run("MissingFileMmap", func(t *testing.T) {
		tmpDir := t.TempDir()

		reader := filesreader.New(tmpDir)
		defer reader.Close()
		info := onnx.ExternalDataInfo{
			Location: "nonexistent.bin",
			Offset:   0,
			Length:   8,
		}

		dst := make([]byte, 8)
		err := reader.ReadInto(info, dst)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to open external data file")
	})

	t.Run("OffsetAndLengthMmap", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create external data file with padding + data
		padding := make([]byte, 256)
		data := []float32{10.0, 20.0, 30.0}
		rawBytes := make([]byte, len(data)*4)
		for i, val := range data {
			bits := *(*uint32)(unsafe.Pointer(&val))
			rawBytes[i*4] = byte(bits)
			rawBytes[i*4+1] = byte(bits >> 8)
			rawBytes[i*4+2] = byte(bits >> 16)
			rawBytes[i*4+3] = byte(bits >> 24)
		}
		fileContent := append(padding, rawBytes...)
		externalFile := filepath.Join(tmpDir, "offset_test.bin")
		err := os.WriteFile(externalFile, fileContent, 0644)
		require.NoError(t, err)

		reader := filesreader.New(tmpDir)
		defer reader.Close()

		proto := &protos.TensorProto{
			Name:     "test_tensor",
			Dims:     []int64{3},
			DataType: int32(protos.TensorProto_FLOAT),
			ExternalData: []*protos.StringStringEntryProto{
				{Key: "location", Value: "offset_test.bin"},
				{Key: "offset", Value: "256"},
				{Key: "length", Value: "12"},
			},
		}

		tensor, err := ONNXTensorToGoMLX(backend, proto, reader)
		require.NoError(t, err)
		require.NotNil(t, tensor)
		defer tensor.FinalizeAll()

		result := tensors.MustCopyFlatData[float32](tensor)
		require.InDeltaSlice(t, data, result, 0.0001)
	})
}

// TestTensorProtoRawBytes tests the tensorProtoRawBytes helper across all supported data formats.
func TestTensorProtoRawBytes(t *testing.T) {
	t.Run("FloatData", func(t *testing.T) {
		tp := &protos.TensorProto{
			Name:      "f",
			Dims:      []int64{2},
			DataType:  int32(protos.TensorProto_FLOAT),
			FloatData: []float32{1.0, 2.0},
		}
		raw, err := tensorProtoRawBytes(tp)
		require.NoError(t, err)
		require.Len(t, raw, 8) // 2 floats * 4 bytes

		// Verify little-endian encoding.
		got0 := math.Float32frombits(binary.LittleEndian.Uint32(raw[0:4]))
		got1 := math.Float32frombits(binary.LittleEndian.Uint32(raw[4:8]))
		assert.Equal(t, float32(1.0), got0)
		assert.Equal(t, float32(2.0), got1)
	})

	t.Run("RawData", func(t *testing.T) {
		expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
		tp := &protos.TensorProto{
			Name:     "r",
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
			RawData:  expected,
		}
		raw, err := tensorProtoRawBytes(tp)
		require.NoError(t, err)
		assert.Equal(t, expected, raw)
	})

	t.Run("DoubleData", func(t *testing.T) {
		tp := &protos.TensorProto{
			Name:       "d",
			Dims:       []int64{1},
			DataType:   int32(protos.TensorProto_DOUBLE),
			DoubleData: []float64{3.14},
		}
		raw, err := tensorProtoRawBytes(tp)
		require.NoError(t, err)
		require.Len(t, raw, 8)
		got := math.Float64frombits(binary.LittleEndian.Uint64(raw))
		assert.Equal(t, 3.14, got)
	})

	t.Run("Int32Data", func(t *testing.T) {
		tp := &protos.TensorProto{
			Name:      "i32",
			Dims:      []int64{3},
			DataType:  int32(protos.TensorProto_INT32),
			Int32Data: []int32{10, -20, 30},
		}
		raw, err := tensorProtoRawBytes(tp)
		require.NoError(t, err)
		require.Len(t, raw, 12)
		assert.Equal(t, uint32(10), binary.LittleEndian.Uint32(raw[0:4]))
		assert.Equal(t, int32(-20), int32(binary.LittleEndian.Uint32(raw[4:8])))
		assert.Equal(t, uint32(30), binary.LittleEndian.Uint32(raw[8:12]))
	})

	t.Run("Int64Data", func(t *testing.T) {
		tp := &protos.TensorProto{
			Name:      "i64",
			Dims:      []int64{2},
			DataType:  int32(protos.TensorProto_INT64),
			Int64Data: []int64{100, -200},
		}
		raw, err := tensorProtoRawBytes(tp)
		require.NoError(t, err)
		require.Len(t, raw, 16)
		assert.Equal(t, uint64(100), binary.LittleEndian.Uint64(raw[0:8]))
		assert.Equal(t, int64(-200), int64(binary.LittleEndian.Uint64(raw[8:16])))
	})

	t.Run("NoData", func(t *testing.T) {
		tp := &protos.TensorProto{
			Name:     "empty",
			Dims:     []int64{2},
			DataType: int32(protos.TensorProto_FLOAT),
		}
		_, err := tensorProtoRawBytes(tp)
		require.Error(t, err)
	})
}

// TestConcatenateTensorProtos tests concatenation of TensorProtos along an axis.
func TestConcatenateTensorProtos(t *testing.T) {
	t.Run("Basic2DLastAxis", func(t *testing.T) {
		// A = [[1,2,3],[4,5,6]] shape [2,3]
		// B = [[7,8,9],[10,11,12]] shape [2,3]
		// Result = [[1,2,3,7,8,9],[4,5,6,10,11,12]] shape [2,6]
		a := &protos.TensorProto{
			Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT),
			FloatData: []float32{1, 2, 3, 4, 5, 6},
		}
		b := &protos.TensorProto{
			Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT),
			FloatData: []float32{7, 8, 9, 10, 11, 12},
		}
		result, err := ConcatenateTensorProtos([]*protos.TensorProto{a, b}, -1)
		require.NoError(t, err)
		assert.Equal(t, []int64{2, 6}, result.Dims)

		// Decode result raw bytes back to float32.
		expected := []float32{1, 2, 3, 7, 8, 9, 4, 5, 6, 10, 11, 12}
		for i, want := range expected {
			got := math.Float32frombits(binary.LittleEndian.Uint32(result.RawData[i*4 : (i+1)*4]))
			assert.InDelta(t, want, got, 1e-6, "index %d", i)
		}
	})

	t.Run("1DConcatenation", func(t *testing.T) {
		a := &protos.TensorProto{
			Dims: []int64{3}, DataType: int32(protos.TensorProto_FLOAT),
			FloatData: []float32{1, 2, 3},
		}
		b := &protos.TensorProto{
			Dims: []int64{2}, DataType: int32(protos.TensorProto_FLOAT),
			FloatData: []float32{4, 5},
		}
		result, err := ConcatenateTensorProtos([]*protos.TensorProto{a, b}, 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{5}, result.Dims)

		expected := []float32{1, 2, 3, 4, 5}
		for i, want := range expected {
			got := math.Float32frombits(binary.LittleEndian.Uint32(result.RawData[i*4 : (i+1)*4]))
			assert.InDelta(t, want, got, 1e-6)
		}
	})

	t.Run("RawDataFormat", func(t *testing.T) {
		// Same as Basic2DLastAxis but using RawData storage.
		aRaw := make([]byte, 6*4)
		bRaw := make([]byte, 6*4)
		for i, v := range []float32{1, 2, 3, 4, 5, 6} {
			binary.LittleEndian.PutUint32(aRaw[i*4:], math.Float32bits(v))
		}
		for i, v := range []float32{7, 8, 9, 10, 11, 12} {
			binary.LittleEndian.PutUint32(bRaw[i*4:], math.Float32bits(v))
		}
		a := &protos.TensorProto{Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT), RawData: aRaw}
		b := &protos.TensorProto{Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT), RawData: bRaw}

		result, err := ConcatenateTensorProtos([]*protos.TensorProto{a, b}, -1)
		require.NoError(t, err)
		assert.Equal(t, []int64{2, 6}, result.Dims)

		expected := []float32{1, 2, 3, 7, 8, 9, 4, 5, 6, 10, 11, 12}
		for i, want := range expected {
			got := math.Float32frombits(binary.LittleEndian.Uint32(result.RawData[i*4 : (i+1)*4]))
			assert.InDelta(t, want, got, 1e-6, "index %d", i)
		}
	})

	t.Run("SingleTensor", func(t *testing.T) {
		a := &protos.TensorProto{
			Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT),
			FloatData: []float32{1, 2, 3, 4, 5, 6},
		}
		result, err := ConcatenateTensorProtos([]*protos.TensorProto{a}, 0)
		require.NoError(t, err)
		assert.Equal(t, a, result) // Should return same pointer.
	})

	t.Run("ErrorDimensionMismatch", func(t *testing.T) {
		a := &protos.TensorProto{Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT), FloatData: make([]float32, 6)}
		b := &protos.TensorProto{Dims: []int64{3, 3}, DataType: int32(protos.TensorProto_FLOAT), FloatData: make([]float32, 9)}
		_, err := ConcatenateTensorProtos([]*protos.TensorProto{a, b}, -1)
		require.Error(t, err)
	})

	t.Run("ErrorDTypeMismatch", func(t *testing.T) {
		a := &protos.TensorProto{Dims: []int64{2}, DataType: int32(protos.TensorProto_FLOAT), FloatData: make([]float32, 2)}
		b := &protos.TensorProto{Dims: []int64{2}, DataType: int32(protos.TensorProto_INT32), Int32Data: make([]int32, 2)}
		_, err := ConcatenateTensorProtos([]*protos.TensorProto{a, b}, 0)
		require.Error(t, err)
	})

	t.Run("ErrorBadAxis", func(t *testing.T) {
		a := &protos.TensorProto{Dims: []int64{2, 3}, DataType: int32(protos.TensorProto_FLOAT), FloatData: make([]float32, 6)}
		_, err := ConcatenateTensorProtos([]*protos.TensorProto{a, a}, 5)
		require.Error(t, err)
	})

	t.Run("ErrorEmpty", func(t *testing.T) {
		_, err := ConcatenateTensorProtos(nil, 0)
		require.Error(t, err)
	})
}

// TestTensorProtoToScalar tests scalar extraction from TensorProto across all dtype branches.
func TestTensorProtoToScalar(t *testing.T) {
	tests := []struct {
		name string
		tp   *protos.TensorProto
		want float64
	}{
		{
			name: "FLOAT_FloatData",
			tp:   &protos.TensorProto{Dims: []int64{}, DataType: int32(protos.TensorProto_FLOAT), FloatData: []float32{3.14}},
			want: float64(float32(3.14)),
		},
		{
			name: "FLOAT_RawData",
			tp: func() *protos.TensorProto {
				raw := make([]byte, 4)
				binary.LittleEndian.PutUint32(raw, math.Float32bits(2.5))
				return &protos.TensorProto{Dims: []int64{1}, DataType: int32(protos.TensorProto_FLOAT), RawData: raw}
			}(),
			want: 2.5,
		},
		{
			name: "DOUBLE_DoubleData",
			tp:   &protos.TensorProto{Dims: []int64{}, DataType: int32(protos.TensorProto_DOUBLE), DoubleData: []float64{1.23456789}},
			want: 1.23456789,
		},
		{
			name: "DOUBLE_RawData",
			tp: func() *protos.TensorProto {
				raw := make([]byte, 8)
				binary.LittleEndian.PutUint64(raw, math.Float64bits(9.87))
				return &protos.TensorProto{Dims: []int64{}, DataType: int32(protos.TensorProto_DOUBLE), RawData: raw}
			}(),
			want: 9.87,
		},
		{
			name: "FLOAT16_RawData",
			tp: func() *protos.TensorProto {
				// float16 for 1.5: use the float16 package to get exact bits.
				h := float16.FromFloat32(1.5)
				raw := make([]byte, 2)
				binary.LittleEndian.PutUint16(raw, uint16(h))
				return &protos.TensorProto{Dims: []int64{}, DataType: int32(protos.TensorProto_FLOAT16), RawData: raw}
			}(),
			want: 1.5,
		},
		{
			name: "INT32_Int32Data",
			tp:   &protos.TensorProto{Dims: []int64{1}, DataType: int32(protos.TensorProto_INT32), Int32Data: []int32{42}},
			want: 42,
		},
		{
			name: "INT64_Int64Data",
			tp:   &protos.TensorProto{Dims: []int64{}, DataType: int32(protos.TensorProto_INT64), Int64Data: []int64{-7}},
			want: -7,
		},
		{
			name: "NonScalar_ReturnsZero",
			tp:   &protos.TensorProto{Dims: []int64{2, 2}, DataType: int32(protos.TensorProto_FLOAT), FloatData: []float32{1, 2, 3, 4}},
			want: 0,
		},
		{
			name: "UnsupportedDType_ReturnsZero",
			tp:   &protos.TensorProto{Dims: []int64{}, DataType: int32(protos.TensorProto_STRING)},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TensorProtoToScalar(tt.tp)
			assert.InDelta(t, tt.want, got, 1e-6)
		})
	}
}

// TestConstantNodeToScalar tests scalar extraction from Constant op nodes.
func TestConstantNodeToScalar(t *testing.T) {
	t.Run("ValueAttribute", func(t *testing.T) {
		node := &protos.NodeProto{
			OpType: "Constant",
			Attribute: []*protos.AttributeProto{
				{
					Name: "value",
					T: &protos.TensorProto{
						Dims:      []int64{},
						DataType:  int32(protos.TensorProto_FLOAT),
						FloatData: []float32{7.5},
					},
				},
			},
		}
		assert.InDelta(t, 7.5, ConstantNodeToScalar(node), 1e-6)
	})

	t.Run("ValueFloatAttribute", func(t *testing.T) {
		node := &protos.NodeProto{
			OpType: "Constant",
			Attribute: []*protos.AttributeProto{
				{Name: "value_float", F: 0.125},
			},
		}
		assert.InDelta(t, 0.125, ConstantNodeToScalar(node), 1e-6)
	})

	t.Run("NoMatchingAttribute", func(t *testing.T) {
		node := &protos.NodeProto{
			OpType: "Constant",
			Attribute: []*protos.AttributeProto{
				{Name: "something_else"},
			},
		}
		assert.Equal(t, 0.0, ConstantNodeToScalar(node))
	})
}
