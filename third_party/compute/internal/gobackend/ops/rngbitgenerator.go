package ops

import (
	"encoding/binary"
	"math/rand/v2"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterRNGBitGenerator.Register(RNGBitGenerator, gobackend.PriorityGeneric)
	gobackend.MultiOutputsNodeExecutors[compute.OpTypeRNGBitGenerator] = execRNGBitGenerator
}

// RNGBitGenerator generates the given shape filled with random bits.
// It takes as input the current random number generator (RNG) state, see RNGState or RNGStateFromSeed.
// The algorithm is hard-coded to use Philox algorithm for now.
//
// It returns the new state of the RNG and the generated values (with random bits) with the given shape.
func RNGBitGenerator(f *gobackend.Function, stateOp compute.Value, shape shapes.Shape) (newState, values compute.Value, err error) {
	opType := compute.OpTypeRNGBitGenerator
	inputs, err := f.VerifyAndCastValues(opType.String(), stateOp)
	if err != nil {
		return nil, nil, err
	}
	state := inputs[0]
	if !state.Shape.Equal(compute.RNGStateShape) {
		err := errors.Errorf(
			"expected random state to be shaped %s, got state.shape=%s instead for RNGBitGenerator",
			compute.RNGStateShape,
			state.Shape,
		)
		return nil, nil, err
	}
	outputShapes := []shapes.Shape{
		state.Shape.Clone(),
		shape.Clone(),
	}
	node := f.NewMultiOutputsNode(opType, outputShapes, state)
	newState = node.MultiOutputsNodes[0]
	values = node.MultiOutputsNodes[1]
	return
}

// execRNGBitGenerator is the executor function registered for compute.OpTypeRngBitGenerator.
func execRNGBitGenerator(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) ([]*gobackend.Buffer, error) {
	state := inputs[0]
	stateFlat := state.Flat.([]uint64)

	// Reserved outputs:
	rngData, err := backend.GetBuffer(node.MultiOutputsShapes[1])
	if err != nil {
		return nil, err
	}
	rngDataBytes, err := rngData.MutableBytes()
	if err != nil {
		return nil, err
	}

	// Generate random using rand/v2:
	rng := rand.NewPCG(stateFlat[0], stateFlat[1]) // Use state and increment as seed
	var randomBits uint64
	for idx := range rngDataBytes {
		if idx%8 == 0 {
			randomBits = rng.Uint64()
		}
		// Take one byte from the randomBits.
		rngDataBytes[idx] = byte(randomBits & 0xFF)
		randomBits >>= 8
	}

	// Update state output - PCG internal state after generating random bytes
	if inputsOwned[0] {
		// We re-use the current state.
		inputs[0] = nil
	} else {
		state, err = backend.GetBuffer(node.MultiOutputsShapes[0])
		if err != nil {
			return nil, err
		}
	}
	stateFlat = state.Flat.([]uint64)

	// See details on Go source code src/math/rand/v2/pcg.go:
	rngState, err := rng.MarshalBinary()
	if err != nil {
		panic(errors.Wrapf(err, "cannot update RNGBitGenerator state"))
	}
	if len(rngState) != 20 && string(rngState[:4]) != "pcg:" {
		return nil, errors.Errorf("format of PCG random number generator changed (we got %d bytes starting with %q, "+
			"we wanted 20 and starting with the string 'pcg:'), pls open an issue in GoMLX",
			len(rngState), rngState[:4])
	}
	stateFlat[0] = binary.LittleEndian.Uint64(rngState[4 : 4+8])
	stateFlat[1] = binary.LittleEndian.Uint64(rngState[4+8 : 4+16])
	return []*gobackend.Buffer{state, rngData}, nil
}
