package gobackend

// OpHandlerRegistration is a generic registration handler type for Function methods.
//
// FN represents the function type, which should take as first parameter always the Function.
// Example for the op "Max(lhs, rhs Value) (Value, error)", FN would take the form
// "func(f *Function, lhs, rhs *Node) (*Node, error)".
type OpHandlerRegistration[FN any] struct {
	Method   string
	Priority RegisterPriority
	Fn       FN
}

// Register registers a handler with the given priority.
// It will only override the op if the given priority is higher than the current one.
func (handler *OpHandlerRegistration[FN]) Register(fn FN, priority RegisterPriority) {
	if priority < handler.Priority {
		return // Lower priority, do not override.
	}
	handler.Priority = priority
	handler.Fn = fn
}

// The registration variables for each Backend as well as the stub methods are automatically generated.
//go:generate go run ../cmd/gobackend_opsregistration
