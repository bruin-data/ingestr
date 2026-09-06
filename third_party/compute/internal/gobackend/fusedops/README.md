# Fused Operations

Fused operations are operations that are fused together for performance.

Since the Go backend doesn't have a JIT-compiler, for the most common cases we
simply implement fused operations, that could yield most of the gain in most
cases.

Reminder: often it's not about reducing the number of operations, but rather
improve the locality of data. E.g.: instead of looping over a large buffer each
time for a different operation, execute many operations and loop over the buffer
only once -- this can be orders of magnitude faster if the buffer doesn't fit
into the vaious CPU caches.

## Adding new fused ops

If your model has a bottleneck, and you think you could improve things by fusing
operations, here's a template to implement your own fused op:

- (Recommended) Measure performance of the computation you are fusing before you
  start your work, as well as the performance of the model where it is used.
  See `support/backendtest/benchmarks.go` for benchmarks of benchmarking individual
  operations (and their composed counterparts).
- Add the signature of the new fused op in `compute.FusedOps` interface, along
  with a new `compute.OpType`.
- Add the new op to the list of ops to generate registration for, in
  `internal/cmd/gobackend_opsregistration`.
- Regenerate code with `go generate ./...` (enumerations, registrations,
  `notimplemented` entries)
- Create a new file in this directory (`internal/gobackend/fusedops`) to
  implement the fused op: follow the pattern from the other files here.
- Add compliance tests in `support/backendtest/`: these are important, and helps
  other backends to reuse your tests.
- Modify GoMLX `pkg/core/graph` (or `pkg/ml/nn`, `pkg/ml/layers/...` or
  somewhere else) ML libraries to use the fused op if available (and if not
  training: if training, you may want to keep the non-fused op, so that you can
  do the gradient, see how other functions like `nn.Dense` do it)
- (Recommende) Measure performance of the computation you are fusing after you
  start your work, as well as the performance of the model where it is used. Add
  a comment in the source code about the performance improvements ( include the
  impact on only the computation, and the "diluted impact" on the model as
  well).

Sounds like a lot, but it's straight forward -- except the actual implementation
of the fused op, which in some cases is not trivial.

## To Do's

* Add `FusedActivation`: and split it from FusedDense, since it is not fused there.
* The implicit broadcasting is much slower than first doing an explicit broadcast. 
  You can see in the benchmarks `go test ./gobackend -bench=Standard/Softmax`.
  This deserves some attention.
