package ops

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/pkg/errors"
)

func init() {
	gobackend.RegisterIsNaN.Register(IsNaN, gobackend.PriorityGeneric)
}

// IsNaN implements compute.Builder interface.
// Notice there is no executor because it uses f.NotEqual instead.
func IsNaN(f *gobackend.Function, x compute.Value) (compute.Value, error) {
	result, err := f.NotEqual(x, x)
	if err != nil {
		return nil, errors.WithMessage(err, "while building op IsNaN")
	}
	return result, nil
}
