package dot

import (
	"fmt"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/gomlx/compute/internal/gobackend"
	"k8s.io/klog/v2"
)

// ImplementationKey reprsents a unique key for a DotGeneral algorithm implementation.
type ImplementationKey struct {
	Layout                  Layout
	InputDType, OutputDType dtypes.DType
}

func (key *ImplementationKey) String() string {
	return fmt.Sprintf("(Layout=%s, InputDType=%s, OutputDType=%s)",
		key.Layout, key.InputDType, key.OutputDType)
}

// DotGeneralExecFn is the signature of a function that implements a specific ImplementationKey
// for DotGeneral.
//
// The lhs and rhs inputs must be shaped according to the layout (batchSize, cross, contracting) and will
// be provided in the specified order.
//
// The output will always be shaped [batchSize, lhsCross, rhsCross], and the required space must have been
// pre-allocated.
type DotGeneralExecFn[I, O interface {
	gotype.Numeric | gotype.AnyHalfPrecision
}] func(
	backend *gobackend.Backend, layout Layout,
	lhs, rhs []I,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int,
	output []O)

type ImplementationRegistration struct {
	// implFn holds the DotGeneralExecFn[InputDType, OutputDType] for the specific ImplementationKey.
	name     string
	implFn   any
	priority gobackend.RegisterPriority
}

var (
	registeredImplementations = make(map[ImplementationKey]*ImplementationRegistration)

	// testRegisteredImplementations is used to store the registered algorithms during tests.
	// They override registeredAlgorithms if set.
	testRegisteredImplementations map[ImplementationKey]*ImplementationRegistration
)

// RegisterImplementation registers a DotGeneral algorithm implementation for a specific
// layout and pair of dtypes.
//
// The name is simply a human-readable identifier for the algorithm, used in logging.
//
// This should only be called during initialization.
func RegisterImplementation[I, O interface {
	gotype.Numeric | gotype.AnyHalfPrecision
}](
	name string,
	layout Layout, inputDType, outputDType dtypes.DType,
	implFn DotGeneralExecFn[I, O], priority gobackend.RegisterPriority,
	forTests bool) {

	// Use a separate map for tests so they don't interfere with each other.
	registration := registeredImplementations
	if forTests {
		registration = testRegisteredImplementations
		if registration == nil {
			registration = make(map[ImplementationKey]*ImplementationRegistration)
			testRegisteredImplementations = registration
		}
	}

	key := ImplementationKey{Layout: layout, InputDType: inputDType, OutputDType: outputDType}
	current, found := registration[key]
	if found && priority < current.priority {
		klog.V(1).Infof("DotGeneral algorithm %q for key %s not registered since priority %d is lower than current priority %d",
			name, key, priority, current.priority)
		return
	}
	registration[key] = &ImplementationRegistration{
		name:     name,
		implFn:   implFn,
		priority: priority,
	}
}

// ResetTestRegistrations resets the test registrations.
func ResetTestRegistrations() {
	testRegisteredImplementations = nil
}

// FindRegisteredImplementation returns the registered implementation for the given
// layout and dtypes, or nil if no implementation is registered.
//
// This can be used during the building of the graph, and the result cached for execution.
func FindRegisteredImplementation(layout Layout, inputDType, outputDType dtypes.DType) *ImplementationRegistration {
	key := ImplementationKey{Layout: layout, InputDType: inputDType, OutputDType: outputDType}
	if testRegisteredImplementations != nil {
		if reg := testRegisteredImplementations[key]; reg != nil {
			klog.V(1).Infof("DotGeneral algorithm %q for key %s registered for testing will be used", reg.name, key)
			return reg
		}
	}
	reg := registeredImplementations[key]
	if klog.V(1).Enabled() {
		if reg != nil {
			klog.V(1).Infof("DotGeneral algorithm %q for key %s found", reg.name, key)
		} else {
			klog.V(1).Infof("DotGeneral algorithm not found for key %s", key)
		}
	}
	return reg
}

// CallRegisteredImplementation calls the registered implementation for the given
// layout and dtypes.
func CallRegisteredImplementation(
	backend *gobackend.Backend,
	implementation *ImplementationRegistration,
	lhs, rhs, output *gobackend.Buffer,
	params *NodeData) {

	batchSize := params.BatchSize
	lhsCrossSize := params.LHSCrossSize
	rhsCrossSize := params.RHSCrossSize
	contractingSize := params.ContractingSize

	lhsFlat, rhsFlat, outputFlat := lhs.Flat, rhs.Flat, output.Flat
	implFnAny := implementation.implFn
	callFnAny, err := callImplementationDTypePairMap.Get(params.InputDType, params.OutputDType)
	if err != nil {
		panic(err)
	}
	callFn := callFnAny.(func(*gobackend.Backend, any, Layout, any, any, any, int, int, int, int))
	callFn(backend, implFnAny, params.Layout, lhsFlat, rhsFlat, outputFlat, batchSize, lhsCrossSize, rhsCrossSize, contractingSize)
}

var (
	//gobackend:dtypemap_pair callImplementationGeneric ints same
	//gobackend:dtypemap_pair callImplementationGeneric ints int32,int64
	//gobackend:dtypemap_pair callImplementationGeneric uints same
	//gobackend:dtypemap_pair callImplementationGeneric uints uint32,uint64
	//gobackend:dtypemap_pair callImplementationGeneric floats floats
	//gobackend:dtypemap_pair callImplementationGeneric half float32,half
	callImplementationDTypePairMap = gobackend.NewDTypePairMap("callImplementationGeneric")
)

// callImplementationGeneric calls the registered implementation with the given parameters.
// If casts all the parameter to the corresponding dtype.
func callImplementationGeneric[I, O interface {
	gotype.Numeric | gotype.AnyHalfPrecision
}](
	backend *gobackend.Backend,
	implFnAny any,
	layout Layout,
	lhsAny, rhsAny, outputAny any,
	batchSize, lhsCrossSize, rhsCrossSize, contractingSize int) {
	lhs := lhsAny.([]I)
	rhs := rhsAny.([]I)
	output := outputAny.([]O)
	implFn := implFnAny.(DotGeneralExecFn[I, O])
	implFn(backend, layout, lhs, rhs, batchSize, lhsCrossSize, rhsCrossSize, contractingSize, output)
}
