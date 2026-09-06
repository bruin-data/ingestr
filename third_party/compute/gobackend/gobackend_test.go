package gobackend

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/must"
	"github.com/gomlx/compute/support/backendtest"
	"k8s.io/klog/v2"
)

var (
	backend compute.Backend

	configFlag = flag.String("config", "", "Go backend config string")
)

// TestCompliance runs all compute.Backend compliance tests.
func TestCompliance(t *testing.T) {
	backendtest.RunAll(t, backend, nil)
}

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
	var err error
	backend, err = New(*configFlag)
	if err != nil {
		klog.Fatalf("Failed to create backend: %+v", err)

	}
	fmt.Printf("Backend: %s, %s\n", backend.Name(), backend.Description())
}

func teardown() {
	backend.Finalize()
}

func TestMain(m *testing.M) {
	flag.Parse()
	setup()
	code := m.Run() // Run all tests in the file
	teardown()
	os.Exit(code)
}

// BenchmarkStandard runs all standard compute.Backend benchmarks on the given backend.
// To run:
//
//	$ go test -bench=. -benchmem
func BenchmarkGoBackend(b *testing.B) {
	fmt.Printf("Running benchmarks on backend: %s, %s\n", backend.Name(), backend.Description())
	backendtest.RunAllBenchmarks(b, backend)
}
