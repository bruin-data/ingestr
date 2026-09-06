// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	stderrors "errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support"
	"github.com/pkg/errors"
)

// ErrBackendAlreadyFinalized is returned when attempting to register or initialize a backend that already exists.
var ErrBackendAlreadyFinalized = stderrors.New("backend already exists")

// Compile-time check.
var _ compute.DataInterface = (*Backend)(nil)

// Buffer for the Go backend holds a shape and a reference to the flat data.
//
// The flat data may be shared -- for temporary buffers from compiled graphs they are
// taken from larger blobs of bytes allocated in one Go -- or owned by the buffer.
type Buffer struct {
	RawBackend *Backend
	RawShape   shapes.Shape

	// InUse is set to false when the buffer is finalized and moved back to the pool.
	InUse bool

	// isUserFed is set to true when the buffer was created from user-provided data
	// (e.g., via BufferFromFlatData). These buffers should not be pooled after use
	// to avoid unbounded pool growth when iterating over large datasets.
	isUserFed bool

	// RawBytes is the underlying storage for the buffer.
	// Its length is always >= requested size in bytes.
	RawBytes []byte

	// Flat is always a slice of the underlying data type (shape.DType).
	// It is a re-casting of RawBytes.
	Flat any
}

// EqualNodeData implements nodeDataComparable for Buffer.
// For Constants, this compares the shape and the actual data values.
func (b *Buffer) EqualNodeData(other NodeDataComparable) bool {
	o := other.(*Buffer) //nolint:errcheck
	if !b.RawShape.Equal(o.RawShape) || b.InUse != o.InUse {
		return false
	}
	// Compare flat data by comparing the underlying slice values
	return compareFlatData(b.Flat, o.Flat)
}

// compareFlatData compares two flat data slices element by element.
func compareFlatData(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Use reflection to compare slices element by element
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)
	if va.Kind() != reflect.Slice || vb.Kind() != reflect.Slice {
		return false
	}
	if va.Len() != vb.Len() {
		return false
	}
	for i := range va.Len() {
		if va.Index(i).Interface() != vb.Index(i).Interface() {
			return false
		}
	}
	return true
}

type bufferPoolKey struct {
	bucketedSize int
}

// getBufferPool for given bucketedSize.
func (b *Backend) getBufferPool(bucketedSize int) *sync.Pool {
	key := bufferPoolKey{bucketedSize: bucketedSize}
	poolInterface, ok := b.bufferPools.Load(key)
	if !ok {
		poolInterface, _ = b.bufferPools.LoadOrStore(key, &sync.Pool{})
	}
	return poolInterface.(*sync.Pool) //nolint:errcheck
}

// recast re-sets the Flat field based on RawBytes, DType and length.
func (b *Buffer) recast(dtype dtypes.DType, length int) {
	if length == 0 {
		b.Flat = dtypes.MakeAnySlice(dtype, 0)
		return
	}
	b.Flat = dtypes.UnsafeAnySliceFromBytes(unsafe.Pointer(unsafe.SliceData(b.RawBytes)), dtype, length)
}

// GetBuffer with the corresponding shape from the backend pool of buffers if possible, or it allocates a new one.
//
// It returns ErrBackendAlreadyFinalized if the backend is already finalized.
//
// Important: it's not necessarily initialized with zero, since it can reuse old buffers.
//
// See also Buffer.Zeros to initialize it with zeros, if needed.
func (b *Backend) GetBuffer(shape shapes.Shape) (*Buffer, error) {
	if b.isFinalized {
		return nil, errors.WithStack(ErrBackendAlreadyFinalized)
	}
	dtype := shape.DType
	length := shape.Size()
	requestedSizeInBytes := dtype.SizeForDimensions(length)
	bucketedSize := support.TwoBitBucketLen(requestedSizeInBytes)
	pool := b.getBufferPool(bucketedSize)
	var buf *Buffer
	if item := pool.Get(); item != nil {
		buf = item.(*Buffer) //nolint:errcheck
		// fmt.Printf("> Reusing buffer: \t(%s) %s\n", humanize.Bytes(bucketedSize), shape)
	} else {
		buf = &Buffer{RawBytes: make([]byte, bucketedSize)}
		// fmt.Printf("> Creating buffer:\t(%s) %s\n", humanize.Bytes(bucketedSize), shape)
	}
	buf.RawBackend = b
	buf.InUse = true
	buf.isUserFed = false
	buf.RawShape = shape.Clone()
	buf.recast(dtype, length)
	// buf.randomize() // Useful to help finding where zero-initialized is needed but missing.
	return buf, nil
}

// // randomize fills the buffer with random bits -- useful for testing.
// func (b *Buffer) randomize() {
// 	bBuf := b.mutableBytes()
// 	_, err := io.ReadFull(rand.Reader, bBuf)
// 	if err != nil {
// 		panic(errors.Wrapf(err, "failed to fill buffer with random bits"))
// 	}
// }

// PutBuffer back into the backend pool of buffers.
// After this any references to buffer should be dropped.
func (b *Backend) PutBuffer(buffer *Buffer) {
	// fmt.Printf("> Returning buffer:\t(%s) %s\n", humanize.Bytes(len(buffer.RawBytes)), buffer.RawShape)
	if b.isFinalized {
		return
	}
	if buffer == nil || !buffer.RawShape.Ok() {
		return
	}
	if !buffer.InUse {
		panic(errors.New("double-freeing Go backend buffer"))
	}
	if buffer.isUserFed {
		// User-fed buffers are not returned to the pool to avoid unbounded growth
		// when looping through large datasets.
		_ = buffer.Finalize()
		return
	}

	buffer.InUse = false
	if len(buffer.RawBytes) == 0 {
		return
	}
	buffer.Flat = nil
	buffer.RawShape = shapes.Invalid()
	pool := b.getBufferPool(len(buffer.RawBytes))
	pool.Put(buffer)
}

// shallowClone returns a new Buffer struct pointing to the same data.
func (buffer *Buffer) shallowClone() *Buffer {
	newBuf := *buffer
	return &newBuf
}

// CopyFlat assumes both flat slices are of the same underlying type.
// Fast paths for common dtypes avoid reflection overhead.
func CopyFlat(flatDst, flatSrc any) {
	// Fast paths for common types to avoid reflection overhead.
	switch dst := flatDst.(type) {
	case []float32:
		copy(dst, flatSrc.([]float32)) //nolint:errcheck
	case []float64:
		copy(dst, flatSrc.([]float64)) //nolint:errcheck
	case []int32:
		copy(dst, flatSrc.([]int32)) //nolint:errcheck
	case []int64:
		copy(dst, flatSrc.([]int64)) //nolint:errcheck
	case []int:
		copy(dst, flatSrc.([]int)) //nolint:errcheck
	case []uint8:
		copy(dst, flatSrc.([]uint8)) //nolint:errcheck
	case []bool:
		copy(dst, flatSrc.([]bool)) //nolint:errcheck
	default:
		// Fallback to reflection for less common types.
		reflect.Copy(reflect.ValueOf(flatDst), reflect.ValueOf(flatSrc))
	}
}

// MutableBytes returns the slice of the bytes used by the flat given -- it works with any of the supported data types for buffers.
func (b *Buffer) MutableBytes() ([]byte, error) {
	tmpAny, tmpErr := mutableBytesDTypeMap.Get(b.RawShape.DType)
	if tmpErr != nil {
		return nil, tmpErr
	}
	fn := tmpAny.(func(b *Buffer) ([]byte, error)) //nolint:errcheck
	return fn(b)
}

// gobackend:dtypemap MutableBytesGeneric ints,uints,floats,half,bool,packed
var mutableBytesDTypeMap = NewDTypeMap("MutableBytes")

// MutableBytesGeneric is the generic implementation of mutableBytes.
func MutableBytesGeneric[T SupportedTypesConstraints](b *Buffer) ([]byte, error) {
	flat, ok := b.Flat.([]T)
	if !ok || len(flat) == 0 {
		return nil, nil // Handle nil or incorrect types (or return an error)
	}
	ptr := (*byte)(unsafe.Pointer(unsafe.SliceData(flat)))
	size := len(flat) * int(unsafe.Sizeof(*new(T)))
	return unsafe.Slice(ptr, size), nil
}

// Fill the buffer with the given value.
// It returns an error if the value type doesn't correspond to the buffer dtype.
//
// As a special case, if value is nil, it will fill the buffer with zeroes for the corresponding DType.
func (b *Buffer) Fill(value any) error {
	dtype := b.RawShape.DType
	if value != nil && dtypes.FromAny(value) != dtype {
		return errors.Errorf("fillBuffer: invalid value type %T for buffer of dtype %s", value, dtype)
	}
	tmpAny, tmpErr := fillBufferDTypeMap.Get(dtype)
	if tmpErr != nil {
		panic(tmpErr)
	}
	fillFn := tmpAny.(func(*Buffer, any)) //nolint:errcheck
	fillFn(b, value)
	return nil
}

//gobackend:dtypemap fillBufferGeneric ints,uints,floats,half,bool
var fillBufferDTypeMap = NewDTypeMap("fillBuffer")

// fillBufferGeneric is the generic implementation of Buffer.Fill.
func fillBufferGeneric[T SupportedTypesConstraints](b *Buffer, valueAny any) {
	var value T
	if valueAny != nil {
		value = valueAny.(T) //nolint:errcheck
	}
	flat := b.Flat.([]T) //nolint:errcheck
	for i := range flat {
		flat[i] = value
	}
}

// Zeros fills the buffer with zeros.
//
// It returns a reference to the buffer to allow cascading calls.
func (b *Buffer) Zeros() *Buffer {
	_ = b.Fill(nil)
	return b
}

// Ones fills the buffer with ones.
//
// It returns a reference to the buffer to allow cascading calls.
func (b *Buffer) Ones() *Buffer {
	dtype := b.RawShape.DType
	_ = b.Fill(shapes.CastAsDType(1, dtype))
	return b
}

// CloneBuffer using the pool to allocate a new one.
func (b *Backend) CloneBuffer(buffer *Buffer) (*Buffer, error) {
	if buffer == nil {
		return nil, errors.Errorf("cloneBuffer(%p): buffer was nil -- buffer was already isFinalized", buffer)
	}
	var issues []string
	if buffer.Flat == nil {
		issues = append(issues, "buffer.flat was nil")
	}
	if !buffer.RawShape.Ok() {
		issues = append(issues, "buffer.shape was invalid")
	}
	if !buffer.InUse {
		issues = append(issues, "buffer was marked as not in use (cloning buffer already freed)")
	}
	if len(issues) > 0 {
		return nil, errors.Errorf("cloneBuffer(%p): %s -- buffer was already isFinalized", buffer,
			strings.Join(issues, ", "))
	}
	newBuffer, err := b.GetBuffer(buffer.RawShape)
	if err != nil {
		return nil, err
	}
	CopyFlat(newBuffer.Flat, buffer.Flat)
	return newBuffer, nil
}

// NewBuffer creates the buffer with a newly allocated flat space.
//
// TODO: should we study the possibility to change the signature with an error?
func (b *Backend) NewBuffer(shape shapes.Shape) *Buffer {
	buffer, err := b.GetBuffer(shape)
	if err != nil {
		return nil
	}
	return buffer
}

// Finalize allows the client to inform the backend that the buffer is no longer needed and associated resources
// can be freed immediately.
//
// A isFinalized buffer should never be used again. Preferably, the caller should set its references to it to nil.
// Finalize allows the client to inform the backend that the buffer is no longer needed and associated resources
// can be freed immediately.
func (buffer *Buffer) Finalize() error {
	if buffer == nil {
		return errors.Errorf("Finalize(%p): buffer was nil", buffer)
	}
	if buffer.RawBackend.isFinalized {
		buffer.Flat = nil // Accelerates GC.
		buffer.RawBytes = nil
		buffer.RawShape = shapes.Invalid()
		return errors.Errorf("Finalize(%p): backend is already finalized", buffer)
	}
	var issues []string
	if buffer.Flat == nil {
		issues = append(issues, "buffer.flat was nil")
	}
	if !buffer.RawShape.Ok() {
		issues = append(issues, "buffer.shape was invalid")
	}
	if !buffer.InUse {
		issues = append(issues, "buffer was marked as not in use (already back in the pool)")
	}
	if len(issues) > 0 {
		return errors.Errorf("Finalize(%p): %s -- buffer was already finalized or back in the pool",
			buffer, strings.Join(issues, ", "))
	}

	buffer.InUse = false
	buffer.Flat = nil
	buffer.RawBytes = nil
	buffer.RawShape = shapes.Invalid()
	return nil
}

// BufferShape returns the shape for the buffer.
// Shape returns the shape for the buffer.
func (buffer *Buffer) Shape() (shapes.Shape, error) {
	return buffer.RawShape, nil
}

// BufferDeviceNum returns the deviceNum for the buffer.
// DeviceNum returns the deviceNum for the buffer.
func (buffer *Buffer) DeviceNum() (compute.DeviceNum, error) {
	return 0, nil
}

// BufferToFlatData transfers the flat values of the buffer to the Go flat array.
// The slice flat must have the exact number of elements required to store the compute.Buffer shape.
//
// See also FlatDataToBuffer, BufferShape, and shapes.Shape.Size.
// ToFlatData transfers the flat values of the buffer to the Go flat array.
func (buffer *Buffer) ToFlatData(flat any) error {
	CopyFlat(flat, buffer.Flat)
	return nil
}

// BufferFromFlatData transfers data from Go given as a flat slice (of the type corresponding to the shape DType)
// to the deviceNum, and returns the corresponding compute.Buffer.
func (b *Backend) BufferFromFlatData(deviceNum compute.DeviceNum, flat any, shape shapes.Shape) (compute.Buffer,
	error) {
	if b.isFinalized {
		return nil, errors.WithStack(ErrBackendAlreadyFinalized)
	}
	if deviceNum != 0 {
		return nil,
			errors.Errorf("backend (%s) only supports deviceNum 0, cannot create buffer on deviceNum %d (shape=%s)",
				b.Name(), deviceNum, shape)
	}
	// For packed sub-byte types (Int4, Uint4, Int2, Uint2), flat data is []byte (packed bytes).
	// dtypes.FromGoType(byte) returns Uint8, not Int4, so we validate the element
	// type directly rather than round-tripping through FromGoType.
	if shape.DType.IsPacked() {
		if reflect.TypeOf(flat).Elem() != reflect.TypeFor[byte]() {
			return nil, errors.Errorf("sub-byte dtype %s requires []byte packed flat data, got %s",
				shape.DType, reflect.TypeOf(flat).Elem())
		}
	} else if dtypes.FromGoType(reflect.TypeOf(flat).Elem()) != shape.DType {
		return nil, errors.Errorf("flat data type (%s) does not match shape DType (%s)",
			reflect.TypeOf(flat).Elem(), shape.DType)
	}
	buffer := b.NewBuffer(shape)
	if buffer == nil {
		return nil, errors.WithStack(ErrBackendAlreadyFinalized)
	}
	buffer.isUserFed = true

	CopyFlat(buffer.Flat, flat)
	return buffer, nil
}

// HasSharedBuffers returns whether the backend supports "shared buffers": these are buffers
// that can be used directly by the engine and has a local address that can be read or mutated
// directly by the client.
func (b *Backend) HasSharedBuffers() bool {
	return true
}

// NewSharedBuffer returns a "shared buffer" that can be both used as input for execution of
// computations and directly read or mutated by the clients.
//
// It panics if the backend doesn't support shared buffers -- see HasSharedBuffer.
//
// The shared buffer should not be mutated while it is used by an execution.
// Also, the shared buffer cannot be "donated" during execution.
//
// When done, to release the memory, call BufferFinalized on the returned buffer.
//
// It returns a handle to the buffer and a slice of the corresponding data type pointing
// to the shared data.
func (b *Backend) NewSharedBuffer(deviceNum compute.DeviceNum, shape shapes.Shape) (buffer compute.Buffer,
	flat any, err error) {
	if b.isFinalized {
		return nil, nil, errors.Errorf("backend is already finalized")
	}
	if deviceNum != 0 {
		return nil, nil,
			errors.Errorf("backend (%s) only supports deviceNum 0, cannot create buffer on deviceNum %d (shape=%s)",
				b.Name(), deviceNum, shape)
	}
	goBuffer := b.NewBuffer(shape)
	if goBuffer == nil {
		return nil, nil, errors.WithStack(ErrBackendAlreadyFinalized)
	}
	goBuffer.isUserFed = true
	return goBuffer, goBuffer.Flat, nil
}

// BufferData returns a slice pointing to the buffer storage memory directly.
//
// This only works if HasSharedBuffer is true, that is, if the backend engine runs on CPU, or
// shares CPU memory.
//
// The returned slice becomes invalid after the buffer is destroyed.
// Data returns a slice pointing to the buffer storage memory directly.
func (buffer *Buffer) Data() (flat any, err error) {
	if buffer.RawBackend.isFinalized {
		return nil, errors.Errorf("backend is already finalized")
	}
	return buffer.Flat, nil
}

// BufferCopyToDevice implements the compute.Backend interface.
// CopyToDevice implements the compute.Buffer interface.
func (buffer *Buffer) CopyToDevice(deviceNum compute.DeviceNum) (compute.Buffer, error) {
	return nil, errors.Errorf("backend %q: multi-device not supported on this backend", BackendName)
}

// Backend returns the backend that owns this buffer.
func (buffer *Buffer) Backend() compute.Backend {
	return buffer.RawBackend
}

// Check does a sanity-check on the buffer.
// Returns a corresponding error if something is wrong.
//
// Used only for debugging.
func (buffer *Buffer) Check() error {
	var issues []string
	if buffer == nil {
		return errors.Errorf("Check(%p): buffer was nil", buffer)
	}
	if buffer.Flat == nil {
		issues = append(issues, "buffer.Flat was nil")
	}
	if !buffer.RawShape.Ok() {
		issues = append(issues, "buffer.RawShape was invalid")
	}
	if !buffer.InUse {
		issues = append(issues, "buffer was marked as not in use")
	}
	if buffer.Flat != nil && buffer.RawShape.Ok() && buffer.RawShape.Size() != reflect.ValueOf(buffer.Flat).Len() {
		issues = append(issues, fmt.Sprintf("buffer.RawShape.Size() != len(buffer.Flat): %d != %d", buffer.RawShape.Size(), reflect.ValueOf(buffer.Flat).Len()))
	}
	if len(issues) > 0 {
		return errors.Errorf("Check(%p, shape=%s): %s",
			buffer, buffer.RawShape, strings.Join(issues, "; "))
	}
	return nil
}
