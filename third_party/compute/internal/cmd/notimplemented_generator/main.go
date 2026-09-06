// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// notimplemented_generator generates "notimplemented" stubs for every API of the compute.Backend interface
package main

import (
	"flag"

	"github.com/gomlx/compute/internal/must"
	"github.com/gomlx/compute/support/backendparser"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	klog.V(1).Info("notimplemented_generator:")
	methods := must.M1(backendparser.ParseBuilder())
	GenerateStandardOpsInterface(methods)
}
