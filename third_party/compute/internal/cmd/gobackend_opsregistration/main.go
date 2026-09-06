// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// gobackends_ops_registration generates a registration system for each of the compute.StandardOps methods,
// so they can be implemented separated by separate packages, as well as stub methods for `gobackends.Backend` that
// calls the corresponding registered method if configured.
package main

import (
	"flag"
	"os"

	"github.com/gomlx/compute/internal/must"
	"github.com/gomlx/compute/support/backendparser"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

func init() {
	// We want must() to report the error with stack and exit.
	must.M = func(err error) {
		if err == nil {
			return
		}
		err = errors.Wrapf(err, "must() failed")
		klog.Errorf("%+v", err)
		os.Exit(1)
	}
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	klog.V(1).Info("generating gobackend ops registration variables and stubs.")
	methods := must.M1(backendparser.ParseBuilder())
	GenerateOpsRegistration(methods)
}
