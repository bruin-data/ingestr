// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// GGML Block Quantization
//
// This file implements dequantization routines for GGML's native block quantization
// formats, as used by llama.cpp / GGUF model files. These formats store weights in
// fixed-size blocks where scales (and sometimes zero-points / mins) are embedded
// directly in each block, rather than in separate tensors.
//
// Weight layout: [N, bytesPerRow] Uint8, where:
//   - N is the output-features dimension (number of rows),
//   - bytesPerRow = (K / valuesPerBlock) * bytesPerBlock,
//   - K is the input-features (logical column count) of the weight matrix.
//
// Each row consists of consecutive blocks. A block packs `valuesPerBlock` logical
// float32 values into `bytesPerBlock` bytes. The block format determines how scales,
// zero-points, and quantized nibbles/bytes are packed.
//
// Supported formats:
//
//	Q4_0   – 32 values/block, 18 bytes/block: fp16 scale + 16 nibble-bytes.
//	         Each nibble is dequantized as: scale * (nibble − 8).
//
//	Q8_0   – 32 values/block, 34 bytes/block: fp16 scale + 32 int8 quants.
//	         Each quant is dequantized as: scale * int8(q).
//
//	IQ4_NL – 32 values/block, 18 bytes/block: same layout as Q4_0 but nibbles
//	         index into a fixed 16-entry non-linear lookup table instead of
//	         using (nibble − 8).
//
//	Q4_K   – 256 values/block, 144 bytes/block: fp16 d + fp16 dmin +
//	         12-byte packed 6-bit scales/mins + 128 nibble bytes.
//	         Sub-block dequantization with per-sub-block scale and min.
//
//	Q6_K   – 256 values/block, 210 bytes/block: ql (128) + qh (64) +
//	         scales (16) + fp16 d (2). 6-bit quants reconstructed from
//	         low/high nibble arrays.
//
// References:
//   - GGML quantization types: https://github.com/ggerganov/ggml/blob/master/docs/gguf.md
//   - llama.cpp dequant kernels: https://github.com/ggerganov/llama.cpp/blob/master/ggml/src/ggml-quants.c

package fusedops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/pkg/errors"
)

// execFusedQuantizedDenseGGML handles GGML-quantized weights.
// Inputs: [x, weights, bias?]. Weights are [N, bytesPerRow] Uint8.
func execFusedQuantizedDenseGGML(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, data *nodeFusedQuantizedDense) (*gobackend.Buffer, error) {
	xBuf := inputs[0]
	wBuf := inputs[1]

	var biasBuf *gobackend.Buffer
	if data.hasBias {
		biasBuf = inputs[2]
	}

	if xBuf.RawShape.DType != dtypes.Float32 {
		return nil, errors.Wrapf(compute.ErrNotImplemented, "FusedQuantizedDense(GGML): only float32 input supported, got %s", xBuf.RawShape.DType)
	}

	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	x := xBuf.Flat.([]float32)
	weights := wBuf.Flat.([]uint8)
	out := output.Flat.([]float32)

	N := data.ggmlN
	K := data.ggmlK
	M := xBuf.RawShape.Size() / K
	bytesPerRow := wBuf.RawShape.Dimensions[1]
	ggmlType := data.ggmlType

	var bias []float32
	if biasBuf != nil {
		bias = biasBuf.Flat.([]float32)
	}

	if err := quantizedDenseGGML(backend, x, weights, bias, out, M, K, N, bytesPerRow, ggmlType); err != nil {
		return nil, err
	}

	fusedDenseApplyActivation[float32](backend, output, data.activation)
	return output, nil
}

// quantizedDenseGGML performs GGML dequant + matmul + bias.
// Uses quantizedDenseParallel for consistent parallelism with the other quantized paths,
// including M=1 column tiling for single-token inference.
func quantizedDenseGGML(backend *gobackend.Backend, x []float32, weights []uint8, bias, out []float32,
	M, K, N, bytesPerRow int, ggmlType compute.GGMLQuantType) error {

	dequantFn, err := ggmlDequantFunc(ggmlType)
	if err != nil {
		return err
	}

	// Pre-allocate per-worker scratch buffers to avoid heap allocation per tile invocation.
	numWorkers := quantizedDenseParallelTileCount(backend, M, K, N)
	scratchBufs := make([][]float32, numWorkers)
	for i := range scratchBufs {
		scratchBufs[i] = make([]float32, K)
	}

	quantizedDenseParallel(backend, M, K, N, func(workerIdx, m, nStart, nEnd int) {
		dequantRow := scratchBufs[workerIdx]
		for n := nStart; n < nEnd; n++ {
			rowData := weights[n*bytesPerRow : (n+1)*bytesPerRow]
			dequantFn(rowData, dequantRow)
			var dot float32
			xRow := x[m*K:]
			for k := range K {
				dot += xRow[k] * dequantRow[k]
			}
			outIdx := m*N + n
			out[outIdx] = dot
			if bias != nil {
				out[outIdx] += bias[n]
			}
		}
	})

	return nil
}

// ggmlDequantFunc returns the dequantization function for the given GGML type.
func ggmlDequantFunc(ggmlType compute.GGMLQuantType) (func(data []uint8, output []float32), error) {
	switch ggmlType {
	case compute.GGMLQ4_0:
		return dequantQ4_0Row, nil
	case compute.GGMLQ8_0:
		return dequantQ8_0Row, nil
	case compute.GGMLIQ4NL:
		return dequantIQ4NLRow, nil
	case compute.GGMLQ4_K:
		return dequantQ4_KRow, nil
	case compute.GGMLQ6_K:
		return dequantQ6_KRow, nil
	default:
		return nil, errors.Wrapf(compute.ErrNotImplemented, "GGML type %s not yet supported in fused path", ggmlType)
	}
}

// ggmlFp16LE decodes a little-endian fp16 value from two bytes into float32.
func ggmlFp16LE(lo, hi uint8) float32 {
	return float16.FromBits(uint16(lo) | uint16(hi)<<8).Float32()
}

// dequantQ8_0Row converts Q8_0 quantized blocks to float32.
// Each block is 34 bytes: 2-byte fp16 scale + 32 int8 quants.
func dequantQ8_0Row(data []uint8, output []float32) {
	const blockSize = 34
	const qk = 32
	nblocks := len(data) / blockSize
	for b := range nblocks {
		blockData := data[b*blockSize : (b+1)*blockSize]
		d := ggmlFp16LE(blockData[0], blockData[1])
		qs := blockData[2:]
		outOff := b * qk
		for i := range qk {
			output[outOff+i] = d * float32(int8(qs[i]))
		}
	}
}

// dequantQ4_0Row converts Q4_0 quantized blocks to float32.
// Each block is 18 bytes: 2-byte fp16 scale + 16 nibble bytes.
func dequantQ4_0Row(data []uint8, output []float32) {
	const blockSize = 18
	const qk = 32
	nblocks := len(data) / blockSize
	for b := range nblocks {
		blockData := data[b*blockSize : (b+1)*blockSize]
		d := ggmlFp16LE(blockData[0], blockData[1])
		qs := blockData[2:]
		outOff := b * qk
		for i := range 16 {
			lo := int(qs[i] & 0x0F)
			output[outOff+i] = d * float32(lo-8)
			hi := int((qs[i] >> 4) & 0x0F)
			output[outOff+16+i] = d * float32(hi-8)
		}
	}
}

// dequantIQ4NLRow converts IQ4_NL quantized blocks to float32.
// Same layout as Q4_0 (2-byte fp16 scale + 16 nibble bytes), but nibble values are
// indices into the IQ4NLLookupTable (pre-normalization integers) instead of linear (nibble - 8).
// Final value: output[i] = scale * IQ4NLLookupTable[nibble].
func dequantIQ4NLRow(data []uint8, output []float32) {
	const blockSize = 18
	const qk = 32
	lut := &compute.IQ4NLLookupTable
	nblocks := len(data) / blockSize
	for b := range nblocks {
		blockData := data[b*blockSize : (b+1)*blockSize]
		d := ggmlFp16LE(blockData[0], blockData[1])
		qs := blockData[2:]
		outOff := b * qk
		for i := range 16 {
			lo := qs[i] & 0x0F
			output[outOff+i] = d * lut[lo]
			hi := (qs[i] >> 4) & 0x0F
			output[outOff+16+i] = d * lut[hi]
		}
	}
}

// getScaleMinK4 extracts a 6-bit scale and min value from the Q4_K/Q5_K
// 12-byte packed scales array.
func getScaleMinK4(j int, scales []byte) (sc, m uint8) {
	if j < 4 {
		sc = scales[j] & 63
		m = scales[j+4] & 63
	} else {
		sc = (scales[j+4] & 0xF) | ((scales[j-4] >> 6) << 4)
		m = (scales[j+4] >> 4) | ((scales[j] >> 6) << 4)
	}
	return
}

// dequantQ4_KRow converts Q4_K quantized blocks to float32.
// Each block is 144 bytes: fp16 d (2) + fp16 dmin (2) + 12 bytes packed scales + 128 bytes nibbles.
func dequantQ4_KRow(data []uint8, output []float32) {
	const blockSize = 144
	const qk = 256
	nblocks := len(data) / blockSize
	for b := range nblocks {
		blockData := data[b*blockSize : (b+1)*blockSize]
		d := ggmlFp16LE(blockData[0], blockData[1])
		dmin := ggmlFp16LE(blockData[2], blockData[3])
		scales := blockData[4:16]
		qs := blockData[16:]
		outOff := b * qk
		idx := 0
		for chunk := range 4 {
			is := chunk * 2
			sc1, m1 := getScaleMinK4(is, scales)
			d1 := d * float32(sc1)
			min1 := dmin * float32(m1)

			sc2, m2 := getScaleMinK4(is+1, scales)
			d2 := d * float32(sc2)
			min2 := dmin * float32(m2)

			qOff := chunk * 32
			for l := range 32 {
				output[outOff+idx] = d1*float32(qs[qOff+l]&0xF) - min1
				idx++
			}
			for l := range 32 {
				output[outOff+idx] = d2*float32(qs[qOff+l]>>4) - min2
				idx++
			}
		}
	}
}

// dequantQ6_KRow converts Q6_K quantized blocks to float32.
// Each block is 210 bytes: ql (128) + qh (64) + scales (16) + fp16 d (2).
func dequantQ6_KRow(data []uint8, output []float32) {
	const blockSize = 210
	const qk = 256
	nblocks := len(data) / blockSize
	for b := range nblocks {
		blockData := data[b*blockSize : (b+1)*blockSize]
		ql := blockData[0:128]
		qh := blockData[128:192]
		sc := blockData[192:208]
		d := ggmlFp16LE(blockData[208], blockData[209])
		outOff := b * qk
		idx := 0
		qlOff := 0
		qhOff := 0
		scOff := 0
		for range 2 {
			for l := range 32 {
				is := l / 16
				q1 := int8((ql[qlOff+l]&0xF)|((qh[qhOff+l]&3)<<4)) - 32
				q2 := int8((ql[qlOff+l+32]&0xF)|(((qh[qhOff+l]>>2)&3)<<4)) - 32
				q3 := int8((ql[qlOff+l]>>4)|(((qh[qhOff+l]>>4)&3)<<4)) - 32
				q4 := int8((ql[qlOff+l+32]>>4)|(((qh[qhOff+l]>>6)&3)<<4)) - 32
				output[outOff+idx+l] = d * float32(int8(sc[scOff+is])) * float32(q1)
				output[outOff+idx+l+32] = d * float32(int8(sc[scOff+is+2])) * float32(q2)
				output[outOff+idx+l+64] = d * float32(int8(sc[scOff+is+4])) * float32(q3)
				output[outOff+idx+l+96] = d * float32(int8(sc[scOff+is+6])) * float32(q4)
			}
			idx += 128
			qlOff += 64
			qhOff += 32
			scOff += 8
		}
	}
}
