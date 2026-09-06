package passes_test

import (
	_ "github.com/gomlx/compute/internal/gobackend/defaultpkgs"
	"k8s.io/klog/v2"
)

func init() {
	klog.InitFlags(nil)
}
