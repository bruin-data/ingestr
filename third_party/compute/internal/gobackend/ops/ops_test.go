package ops_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"
	_ "github.com/gomlx/compute/internal/gobackend/defaultpkgs"
	"github.com/gomlx/compute/internal/must"
	"github.com/gomlx/compute/shapes"
	"k8s.io/klog/v2"
)

var backend *gobackend.Backend

func init() {
	klog.InitFlags(nil)
}

func setup() {
	fmt.Printf("Available backends: %q\n", compute.List())
	// Perform your setup logic here
	if os.Getenv(compute.ConfigEnvVar) == "" {
		must.M(os.Setenv(compute.ConfigEnvVar, "go"))
	} else {
		fmt.Printf("\t$%s=%q\n", compute.ConfigEnvVar, os.Getenv(compute.ConfigEnvVar))
	}
	backendGeneric, err := gobackend.New("")
	if err != nil {
		klog.Fatalf("Failed to create backend: %+v", err)
	}
	backend = backendGeneric.(*gobackend.Backend)
	fmt.Printf("Backend: %s, %s\n", backend.Name(), backend.Description())
}

func teardown() {
	backend.Finalize()
}

func makeBuffer(t *testing.T, shape shapes.Shape, flat any) *gobackend.Buffer {
	t.Helper()
	computeBuf, err := backend.BufferFromFlatData(0, flat, shape)
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}
	return computeBuf.(*gobackend.Buffer)
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run() // Run all tests in the file
	teardown()
	os.Exit(code)
}
