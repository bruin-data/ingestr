package onnxgomlx

import (
	"encoding/binary"
	"math"
	"strconv"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/exceptions"
	. "github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/pkg/errors"
)

// DefaultDeviceNum is the device number used in local graph operations
// (like converting tensors for different types).
var DefaultDeviceNum = compute.DeviceNum(0)

// Shape converts an ONNX data type and shape to GoMLX shapes.Shape (it includes the dtype).
func Shape(proto *protos.TensorProto) (shape shapes.Shape, err error) {
	if proto == nil {
		err = errors.New("ONNX TensorProto is nil")
		return
	}
	shape.DType, err = dtypeForONNX(protos.TensorProto_DataType(proto.DataType))
	if err != nil {
		return
	}
	shape.Dimensions = make([]int, len(proto.Dims))
	for axis, dim := range proto.Dims {
		// A TensorProto describes a concrete stored tensor, so every dimension
		// must be non-negative (ONNX spec). A negative dim (e.g. -1, which
		// shapes treats as a dynamic axis) is invalid here and would later panic
		// in Shape.Size() ("called on shape with dynamic dimensions"), so reject
		// it up front instead of propagating a poisoned shape.
		if dim < 0 {
			err = errors.Errorf("tensor %q has invalid negative dimension %d at axis %d", proto.Name, dim, axis)
			return
		}
		shape.Dimensions[axis] = int(dim)
	}
	if proto.Segment != nil {
		err = errors.Errorf("segmented tensor not supported (%v)", proto.Segment)
		return
	}
	return
}

// SparseShape returns what would be the dense shape of an ONNX SparseTensor.
func SparseShape(proto *protos.SparseTensorProto) (shape shapes.Shape, err error) {
	if proto == nil || proto.Values == nil || proto.Indices == nil {
		err = errors.New("ONNX SparseTensorProto or its components are nil")
		return
	}
	shape.DType, err = dtypeForONNX(protos.TensorProto_DataType(proto.Values.DataType))
	if err != nil {
		return
	}
	shape.Dimensions = make([]int, len(proto.Dims))
	for axis, dim := range proto.Dims {
		// Concrete sparse tensor dimensions must be non-negative; a negative dim
		// would later panic in Shape.Size(). See Shape() for details.
		if dim < 0 {
			err = errors.Errorf("sparse tensor has invalid negative dimension %d at axis %d", dim, axis)
			return
		}
		shape.Dimensions[axis] = int(dim)
	}
	return
}

// checkAndCreateTensorFromProto implements the generic check and copy of the ONNX proto data to a tensor for the supported data type.
// TODO: It assumes it was saved in the same endian-ness and row-major order. Check/adjust if not.
func checkAndCreateTensorFromProto[T interface {
	float32 | float64 | int32 | int64 | uint64
}](backend compute.Backend, proto *protos.TensorProto, onnxData []T, shape shapes.Shape) (*tensors.Tensor, error) {
	if onnxData == nil {
		// Not this type of data.
		return nil, nil
	}
	if len(onnxData) != shape.Size() {
		return nil, errors.Errorf("tensor %q shaped %s has size %d , but ONNX model provided a slice with %d values!?",
			proto.Name, shape, shape.Size(), len(onnxData))
	}

	onnxDataTensor := tensors.FromFlatDataAndDimensions(onnxData, shape.Dimensions...)
	if shape.DType == dtypes.FromGenericsType[T]() {
		// The provided ONNX tensor is exactly what we want:
		return onnxDataTensor, nil
	}
	defer onnxDataTensor.FinalizeAll() // Help the GC.

	// Convert from the ONNX proto data type to the target datatype.
	// It uses GoMLX SimpleGo backend.
	var converted *tensors.Tensor
	err := exceptions.TryCatch[error](func() {
		converted = MustExecOnce(backend, func(x *Node) *Node {
			return ConvertDType(x, shape.DType)
		}, onnxDataTensor)
		converted.ToLocal() // Detach from the conversion backend.
	})
	return converted, err
}

// tensorToGoMLX converts a protos.TensorProto object to a tensors.Tensor object, handling errors and different data types.
func tensorToGoMLX(backend compute.Backend, proto *protos.TensorProto) (t *tensors.Tensor, err error) {
	if proto == nil {
		return nil, errors.New("ONNX TensorProto is nil")
	}

	var shape shapes.Shape
	shape, err = Shape(proto)
	if err != nil {
		err = errors.WithMessagef(err, "while parsing tensor %q", proto.Name)
		return
	}

	// Handle zero-sized tensors (e.g., shape [0]): no data is expected.
	if shape.Size() == 0 {
		return tensors.FromShape(shape), nil
	}

	// If data is provided as RawData: check that the size of the data is the same used in GoMLX.
	if proto.RawData != nil {
		t = tensors.FromShape(shape)
		t.MutableBytes(func(data []byte) {
			if len(data) != len(proto.RawData) {
				err = errors.Errorf("tensor %q shaped %s uses %d bytes, but ONNX model provided %d bytes of raw-data!?",
					proto.Name, shape, len(data), len(proto.RawData))
			} else {
				copy(data, proto.RawData)
			}
		})
		if err != nil {
			t.FinalizeAll()
			t = nil
			return nil, err
		}
		return
	}

	// Tries to convert to each data type.
	if proto.DoubleData != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.DoubleData, shape)
	}
	if proto.FloatData != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.FloatData, shape)
	}
	if proto.Int64Data != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.Int64Data, shape)
	}
	if proto.Uint64Data != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.Uint64Data, shape)
	}
	if proto.Int32Data != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.Int32Data, shape)
	}
	if proto.StringData != nil {
		return nil, errors.Errorf("ONNX model tensor %q holds string data which is not supported in GoMLX models", proto.Name)
	}
	if len(proto.ExternalData) > 0 {
		return nil, errors.Errorf("ONNX model tensor %q is stored as external data; use tensorToGoMLXWithBaseDir with the model's base directory", proto.Name)
	}
	// Unknown tensor data type!?
	return nil, errors.Errorf("tensor %q shaped %s has no supported format of data in the ONNX model!?", proto.Name, shape)
}

var ErrNoExternalData = errors.New("no external data")

// parseExternalData extracts external data parameters from the TensorProto.
// Returns an error ErrNoExternalData if there is no external data.
func parseExternalData(proto *protos.TensorProto) (onnx.ExternalDataInfo, error) {
	var info onnx.ExternalDataInfo
	if len(proto.ExternalData) == 0 {
		return info, errors.Wrapf(ErrNoExternalData, "tensor %q has no external data", proto.Name)
	}

	info = onnx.ExternalDataInfo{
		Offset: 0,
		Length: -1, // -1 means read to EOF
	}

	for _, entry := range proto.ExternalData {
		switch entry.Key {
		case "location":
			info.Location = entry.Value
		case "offset":
			offset, err := strconv.ParseInt(entry.Value, 10, 64)
			if err != nil {
				return info, errors.Wrapf(err, "invalid offset value %q for tensor %q", entry.Value, proto.Name)
			}
			info.Offset = offset
		case "length":
			length, err := strconv.ParseInt(entry.Value, 10, 64)
			if err != nil {
				return info, errors.Wrapf(err, "invalid length value %q for tensor %q", entry.Value, proto.Name)
			}
			info.Length = length
		case "checksum":
			// Checksum is optional and used for verification; we don't validate it currently
		default:
			// Ignore unknown keys for forward compatibility
		}
	}

	if info.Location == "" {
		return info, errors.Errorf("external data for tensor %q is missing required 'location' key", proto.Name)
	}

	return info, nil
}

// ONNXTensorToGoMLX converts a protos.TensorProto object to a GoMLX tensors.Tensor object,
// handling errors and different data types including external data.
//
// externalReader is only used if the TensorProto specifies an external data location.
func ONNXTensorToGoMLX(backend compute.Backend, proto *protos.TensorProto, externalReader onnx.ExternalDataReader) (
	t *tensors.Tensor, err error) {
	if proto == nil {
		return nil, errors.New("ONNX TensorProto is nil")
	}

	var shape shapes.Shape
	shape, err = Shape(proto)
	if err != nil {
		err = errors.WithMessagef(err, "while parsing tensor %q", proto.Name)
		return
	}

	// Handle zero-sized tensors (e.g., shape [0]): no data is expected.
	if shape.Size() == 0 {
		return tensors.FromShape(shape), nil
	}

	// Check for external data first
	if len(proto.ExternalData) > 0 {
		if externalReader == nil {
			return nil, errors.Errorf(
				"external data for tensor %q is present but no external data reader was provided", proto.Name)
		}
		info, err := parseExternalData(proto)
		if err != nil {
			return nil, err
		}

		// Create tensor and read directly into its backing memory
		t = tensors.FromShape(shape)
		t.MutableBytes(func(data []byte) {
			// Use the reader if available (mmap path), otherwise fall back to direct I/O
			err = externalReader.ReadInto(info, data)
		})
		if err != nil {
			t.FinalizeAll()
			return nil, errors.WithMessagef(err, "while reading external data for tensor %q", proto.Name)
		}
		return t, nil
	}

	// If data is provided as RawData: check that the size of the data is the same used in GoMLX.
	if proto.RawData != nil {
		t = tensors.FromShape(shape)
		t.MutableBytes(func(data []byte) {
			if len(data) != len(proto.RawData) {
				err = errors.Errorf("tensor %q shaped %s uses %d bytes, but ONNX model provided %d bytes of raw-data!?",
					proto.Name, shape, len(data), len(proto.RawData))
			} else {
				copy(data, proto.RawData)
			}
		})
		if err != nil {
			t.FinalizeAll()
			t = nil
			return nil, err
		}
		return
	}

	// Tries to convert to each data type.
	if proto.DoubleData != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.DoubleData, shape)
	}
	if proto.FloatData != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.FloatData, shape)
	}
	if proto.Int64Data != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.Int64Data, shape)
	}
	if proto.Uint64Data != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.Uint64Data, shape)
	}
	if proto.Int32Data != nil {
		return checkAndCreateTensorFromProto(backend, proto, proto.Int32Data, shape)
	}
	if proto.StringData != nil {
		return nil, errors.Errorf("ONNX model tensor %q holds string data which is not supported in GoMLX models", proto.Name)
	}

	// Unknown tensor data type!?
	return nil, errors.Errorf("tensor %q shaped %s has no supported format of data in the ONNX model!?", proto.Name, shape)
}

// checkAndCopyTensorToProto implements the generic check and copy of the tensor to the ONNX proto data.
func checkAndCopyTensorToProto[T interface {
	float32 | float64 | int32 | int64 | uint64
}](t *tensors.Tensor, proto *protos.TensorProto, onnxData []T) error {
	shape := t.Shape()
	if len(onnxData) != shape.Size() {
		return errors.Errorf("tensor %q shaped %s has size %d , but ONNX model provided a slice with %d values!?",
			proto.Name, shape, shape.Size(), len(onnxData))
	}

	// If the dtype of the tensor doesn't match the dtype of the proto storing it:
	var converted *tensors.Tensor
	if shape.DType != dtypes.FromGenericsType[T]() {
		// Convert from GoMLX tensor to the ONNX proto data type.
		// It uses GoMLX SimpleGo backend.
		backend, err := gobackend.New("")
		if err != nil {
			return err
		}
		defer backend.Finalize()
		cloned, err := t.OnDeviceClone(backend, DefaultDeviceNum)
		if err != nil {
			return err
		}
		converted, err = CallOnce(backend, func(x *Node) *Node {
			return ConvertDType(x, dtypes.FromGenericsType[T]())
		}, cloned)
		if err != nil {
			return err
		}
		err = converted.ToLocal() // Detach from the temporarily created backend.
		if err != nil {
			return err
		}
		t = converted
		defer func() {
			// Notice this only gets used if there was another error, so we ignore this one, if one
			// happens here.
			_ = converted.FinalizeAll()
		}()
	}

	// Copy GoMLX value (potentially converted) to the ONNX proto.
	err := tensors.ConstFlatData(t, func(tensorData []T) {
		copy(onnxData, tensorData) // Copy data to ONNX proto.
	})
	if err != nil {
		return err
	}
	if converted != nil {
		// Return the error of the FinalizeAll -- it will be called again by the deferred fucntion again,
		// but it is fine.
		return converted.FinalizeAll()
	}
	return nil
}

// TensorValueToONNX copies the value of a GoMLX tensors.Tensor to the ONNX protos.TensorProto object handling errors and different data types.
//
// Both tensors (GoMLX and ONNX) must already have the same shape.
func TensorValueToONNX(t *tensors.Tensor, proto *protos.TensorProto) (err error) {
	var shape shapes.Shape
	shape, err = Shape(proto)
	if err != nil {
		return errors.WithMessagef(err, "while parsing tensor %q", proto.Name)
	}
	if !shape.Equal(t.Shape()) {
		return errors.Errorf("TensorValueToONNX: cannot copy value of GoMLX tensor shaped %s to ONNX tensor shaped %s",
			t.Shape(), shape)
	}

	// Raw data tensor.
	if proto.RawData != nil {
		t.ConstBytes(func(data []byte) {
			if len(data) != len(proto.RawData) {
				err = errors.Errorf("tensor %q shaped %s uses %d bytes, but ONNX model provided %d bytes of raw-data!?",
					proto.Name, shape, len(data), len(proto.RawData))
			}
			copy(proto.RawData, data) // Copy data to ONNX proto.
		})
		return err
	}

	// Float32
	if proto.FloatData != nil {
		return checkAndCopyTensorToProto(t, proto, proto.FloatData)
	}
	if proto.DoubleData != nil {
		return checkAndCopyTensorToProto(t, proto, proto.DoubleData)
	}
	if proto.Int32Data != nil {
		return checkAndCopyTensorToProto(t, proto, proto.Int32Data)
	}
	if proto.Int64Data != nil {
		return checkAndCopyTensorToProto(t, proto, proto.Int64Data)
	}
	if proto.Uint64Data != nil {
		return checkAndCopyTensorToProto(t, proto, proto.Uint64Data)
	}
	return errors.Errorf("tensor %q shaped %s has no supported format of data in the ONNX model!?", proto.Name, shape)
}

// TensorProtoToScalar extracts a scalar float64 from a TensorProto.
// Returns 0 if the tensor is not a scalar or the data type is unsupported.
func TensorProtoToScalar(tp *protos.TensorProto) float64 {
	// Must be scalar (empty dims or [1]).
	totalElements := int64(1)
	for _, d := range tp.Dims {
		totalElements *= d
	}
	if totalElements != 1 {
		return 0
	}

	switch tp.DataType {
	case int32(protos.TensorProto_FLOAT):
		if len(tp.FloatData) > 0 {
			return float64(tp.FloatData[0])
		}
		if len(tp.RawData) >= 4 {
			bits := uint32(tp.RawData[0]) | uint32(tp.RawData[1])<<8 | uint32(tp.RawData[2])<<16 | uint32(tp.RawData[3])<<24
			return float64(math.Float32frombits(bits))
		}
	case int32(protos.TensorProto_DOUBLE):
		if len(tp.DoubleData) > 0 {
			return tp.DoubleData[0]
		}
		if len(tp.RawData) >= 8 {
			bits := uint64(tp.RawData[0]) | uint64(tp.RawData[1])<<8 | uint64(tp.RawData[2])<<16 |
				uint64(tp.RawData[3])<<24 | uint64(tp.RawData[4])<<32 | uint64(tp.RawData[5])<<40 |
				uint64(tp.RawData[6])<<48 | uint64(tp.RawData[7])<<56
			return math.Float64frombits(bits)
		}
	case int32(protos.TensorProto_FLOAT16):
		if len(tp.RawData) >= 2 {
			bits := uint16(tp.RawData[0]) | uint16(tp.RawData[1])<<8
			return float64(float16.Float16(bits).Float32())
		}
	case int32(protos.TensorProto_INT32):
		if len(tp.Int32Data) > 0 {
			return float64(tp.Int32Data[0])
		}
	case int32(protos.TensorProto_INT64):
		if len(tp.Int64Data) > 0 {
			return float64(tp.Int64Data[0])
		}
	}
	return 0
}

// ConstantNodeToScalar extracts a scalar float64 from a Constant op node.
// Returns 0 if no scalar value is found.
func ConstantNodeToScalar(node *protos.NodeProto) float64 {
	for _, attr := range node.Attribute {
		if attr.Name == "value" && attr.T != nil {
			return TensorProtoToScalar(attr.T)
		}
		if attr.Name == "value_float" {
			return float64(attr.F)
		}
	}
	return 0
}

// tensorProtoRawBytes returns the raw byte representation of a TensorProto's data.
// If the data is already in RawData format, returns it directly. Otherwise converts
// typed data fields (FloatData, etc.) to little-endian raw bytes.
func tensorProtoRawBytes(tp *protos.TensorProto) ([]byte, error) {
	if tp.RawData != nil {
		return tp.RawData, nil
	}
	if tp.FloatData != nil {
		buf := make([]byte, len(tp.FloatData)*4)
		for i, v := range tp.FloatData {
			binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
		}
		return buf, nil
	}
	if tp.DoubleData != nil {
		buf := make([]byte, len(tp.DoubleData)*8)
		for i, v := range tp.DoubleData {
			binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(v))
		}
		return buf, nil
	}
	if tp.Int32Data != nil {
		buf := make([]byte, len(tp.Int32Data)*4)
		for i, v := range tp.Int32Data {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(v))
		}
		return buf, nil
	}
	if tp.Int64Data != nil {
		buf := make([]byte, len(tp.Int64Data)*8)
		for i, v := range tp.Int64Data {
			binary.LittleEndian.PutUint64(buf[i*8:], uint64(v))
		}
		return buf, nil
	}
	return nil, errors.Errorf("tensor %q has no supported data format for raw byte extraction", tp.Name)
}

// ConcatenateTensorProtos concatenates TensorProto data along the given axis.
// All tensors must have the same data type and matching dimensions on all axes
// except the concatenation axis. Returns a new TensorProto with RawData format.
func ConcatenateTensorProtos(tps []*protos.TensorProto, axis int) (*protos.TensorProto, error) {
	if len(tps) == 0 {
		return nil, errors.New("no tensors to concatenate")
	}
	if len(tps) == 1 {
		return tps[0], nil
	}

	ndim := len(tps[0].Dims)
	if axis < 0 {
		axis = ndim + axis
	}
	if axis < 0 || axis >= ndim {
		return nil, errors.Errorf("axis %d out of range for %d-dimensional tensor", axis, ndim)
	}

	// Validate shapes and data types match.
	for i := 1; i < len(tps); i++ {
		if len(tps[i].Dims) != ndim {
			return nil, errors.Errorf("tensor %d has %d dims, expected %d", i, len(tps[i].Dims), ndim)
		}
		if tps[i].DataType != tps[0].DataType {
			return nil, errors.Errorf("tensor %d has DataType %d, expected %d", i, tps[i].DataType, tps[0].DataType)
		}
		for d := range ndim {
			if d != axis && tps[i].Dims[d] != tps[0].Dims[d] {
				return nil, errors.Errorf("tensor %d dim %d is %d, expected %d", i, d, tps[i].Dims[d], tps[0].Dims[d])
			}
		}
	}

	// Get raw bytes for each tensor.
	rawDatas := make([][]byte, len(tps))
	for i, t := range tps {
		var err error
		rawDatas[i], err = tensorProtoRawBytes(t)
		if err != nil {
			return nil, err
		}
	}

	// Compute element size in bytes.
	totalElements := int64(1)
	for _, d := range tps[0].Dims {
		totalElements *= d
	}
	if totalElements == 0 || len(rawDatas[0]) == 0 {
		return nil, errors.New("cannot concatenate empty tensors")
	}
	elemSize := len(rawDatas[0]) / int(totalElements)

	// Output shape.
	outDims := make([]int64, ndim)
	copy(outDims, tps[0].Dims)
	for i := 1; i < len(tps); i++ {
		outDims[axis] += tps[i].Dims[axis]
	}

	// outerSize = product of dims before axis.
	outerSize := int64(1)
	for d := 0; d < axis; d++ {
		outerSize *= tps[0].Dims[d]
	}

	// suffix = product of dims after axis.
	suffix := int64(1)
	for d := axis + 1; d < ndim; d++ {
		suffix *= tps[0].Dims[d]
	}

	// innerSize[i] = tps[i].Dims[axis] * suffix (in elements).
	innerSizes := make([]int64, len(tps))
	for i, t := range tps {
		innerSizes[i] = t.Dims[axis] * suffix
	}

	// Allocate output.
	totalOutElements := int64(1)
	for _, d := range outDims {
		totalOutElements *= d
	}
	outRaw := make([]byte, int(totalOutElements)*elemSize)

	// Copy data row by row.
	outOffset := 0
	for row := int64(0); row < outerSize; row++ {
		for i := range tps {
			nBytes := int(innerSizes[i]) * elemSize
			srcOffset := int(row*innerSizes[i]) * elemSize
			copy(outRaw[outOffset:outOffset+nBytes], rawDatas[i][srcOffset:srcOffset+nBytes])
			outOffset += nBytes
		}
	}

	return &protos.TensorProto{
		Dims:     outDims,
		DataType: tps[0].DataType,
		RawData:  outRaw,
	}, nil
}
