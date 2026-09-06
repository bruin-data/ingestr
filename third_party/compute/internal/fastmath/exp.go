// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package fastmath

import (
	"math"
)

// Exp32 approximates e^x for float32.
//
// Algorithm (Cephes-style expf, Stephen L. Moshier):
//  1. Clamp x to ≈ [-127 ln 2, 127 ln 2].
//  2. Reduce: x = n·ln2 + g with |g| ≤ ln2/2, n = round(x / ln2).
//     ln2 is split (hi/lo) so n·ln2 is subtracted accurately.
//  3. Approximate e^g with a degree-7 Horner polynomial (Cephes).
//  4. Scale by exact 2^n via the float32 exponent field: bits (n+127)<<23.
//
// Accuracy vs math.Exp: max relative error < 1e-7 over the normal float32
// range (see TestExp32). Not bit-identical to math.Exp;
// overflow/underflow return a large finite / 0 rather than ±Inf / subnormals.
//
// Intended for go-backend fused ops (softmax, etc.) where math.Exp on float32
// dominates runtime and this precision is enough. Softmax always evaluates
// exp(x-max) ≤ 1, so arguments are ≤ 0 and the overflow clamp is unused.
func Exp32(x float32) float32 {
	// Cephes expf range / reduction constants.
	const (
		maxNumF = 1.7014117331926442990585209174225846272e38 // largest finite Cephes float32 result
		maxLogF = 88.02969187150841                          // ≈ 127 ln 2
		minLogF = -88.02969187150841                         // ≈ -127 ln 2

		log2E = 1.44269504088896341 // log₂(e) = 1/ln 2
		ln2Hi = 0.693359375         // 355/512; high part of ln 2
		ln2Lo = -2.12194440e-4      // ln2Hi+ln2Lo = ln 2 (split for accuracy)
	)

	if x > maxLogF {
		return maxNumF
	}
	if x < minLogF {
		return 0
	}

	// n = round(x / ln 2); g = x - n·ln 2 (range-reduced remainder).
	z := float32(math.Floor(float64(x)*log2E + 0.5))
	g := x - z*ln2Hi - z*ln2Lo
	n := int(z)

	// Exact power-of-two scale: float32 exponent bits hold n + bias(127).
	// Safe because the clamps keep n ∈ [-127, 127].
	scale := math.Float32frombits(uint32(int32(n)+127) << 23)
	return scale * exp32Poly(g)
}

// exp32Poly approximates e^x for |x| ≤ ln2/2 using Cephes's Horner polynomial:
//
//	e^x ≈ 1 + x + x²·P(x)
func exp32Poly(x float32) float32 {
	const (
		// Cephes expf Remez coefficients for e^g on the reduced argument g ∈ [-ln2/2, ln2/2].
		// Close to Taylor 1/n! but fitted for float32.
		p7 = 1.9875691500e-4
		p6 = 1.3981999507e-3
		p5 = 8.3334519073e-3
		p4 = 4.1665795894e-2
		p3 = 1.6666665459e-1
		p2 = 5.0000001201e-1
	)
	return (((((p7*x+p6)*x+p5)*x+p4)*x+p3)*x+p2)*(x*x) + x + 1.0
}
