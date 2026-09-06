// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package distributed_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/gomlx/compute/distributed"
	"github.com/gomlx/compute/support/testutil"
)

func TestDeviceMesh(t *testing.T) {
	t.Run("NewDeviceMesh_Valid", func(t *testing.T) {
		tests := []struct {
			name      string
			shape     []int
			axisNames []string
			wantRank  int
			wantNum   int
		}{
			{
				name:      "1D mesh",
				shape:     []int{8},
				axisNames: []string{"replica"},
				wantRank:  1,
				wantNum:   8,
			},
			{
				name:      "2D mesh",
				shape:     []int{2, 4},
				axisNames: []string{"x", "y"},
				wantRank:  2,
				wantNum:   8,
			},
			{
				name:      "3D mesh",
				shape:     []int{2, 2, 2},
				axisNames: []string{"x", "y", "z"},
				wantRank:  3,
				wantNum:   8,
			},
			{
				name:      "single device",
				shape:     []int{1},
				axisNames: []string{"replica"},
				wantRank:  1,
				wantNum:   1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mesh, err := distributed.NewDeviceMesh(tt.shape, tt.axisNames)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if mesh == nil {
					t.Fatalf("expected non-nil mesh")
				}
				if mesh.Rank() != tt.wantRank {
					t.Errorf("want rank %v, got %v", tt.wantRank, mesh.Rank())
				}
				if mesh.NumDevices() != tt.wantNum {
					t.Errorf("want num devices %v, got %v", tt.wantNum, mesh.NumDevices())
				}
			})
		}
	})

	t.Run("NewDeviceMesh_Errors", func(t *testing.T) {
		tests := []struct {
			name      string
			shape     []int
			axisNames []string
			wantErr   string
		}{
			{
				name:      "mismatched lengths",
				shape:     []int{2, 4},
				axisNames: []string{"x"},
				wantErr:   "axesSizes and axesNames must have the same length",
			},
			{
				name:      "empty axesSizes",
				shape:     []int{},
				axisNames: []string{},
				wantErr:   "DeviceMesh axesSizes cannot be empty",
			},
			{
				name:      "empty axis name",
				shape:     []int{4},
				axisNames: []string{""},
				wantErr:   "is not a valid identifier",
			},
			{
				name:      "duplicate axis names",
				shape:     []int{2, 4},
				axisNames: []string{"x", "x"},
				wantErr:   "axis name \"x\" is duplicated",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mesh, err := distributed.NewDeviceMesh(tt.shape, tt.axisNames)
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if mesh != nil {
					t.Errorf("expected nil mesh on error, got %v", mesh)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error to contain %q, got %v", tt.wantErr, err)
				}
			})
		}
	})

	t.Run("AxesNames", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{2, 4}, []string{"x", "y"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		axisNames := mesh.AxesNames()
		if !slices.Equal(axisNames, []string{"x", "y"}) {
			t.Errorf("want %v, got %v", []string{"x", "y"}, axisNames)
		}

		// Verify it returns a copy
		axisNames[0] = "modified"
		if !slices.Equal(mesh.AxesNames(), []string{"x", "y"}) {
			t.Errorf("AxesNames() did not return a copy; want %v, got %v", []string{"x", "y"}, mesh.AxesNames())
		}
	})

	t.Run("Shape", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{2, 4}, []string{"x", "y"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		axesSizes := mesh.AxesSizes()
		if !slices.Equal(axesSizes, []int{2, 4}) {
			t.Errorf("want %v, got %v", []int{2, 4}, axesSizes)
		}

		// Verify it returns a copy
		axesSizes[0] = 99
		if !slices.Equal(mesh.AxesSizes(), []int{2, 4}) {
			t.Errorf("AxesSizes() did not return a copy; want %v, got %v", []int{2, 4}, mesh.AxesSizes())
		}
	})

	t.Run("AxisSize", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{2, 4}, []string{"x", "y"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tests := []struct {
			name     string
			axisName string
			wantSize int
			wantErr  bool
		}{
			{
				name:     "valid axis x",
				axisName: "x",
				wantSize: 2,
				wantErr:  false,
			},
			{
				name:     "valid axis y",
				axisName: "y",
				wantSize: 4,
				wantErr:  false,
			},
			{
				name:     "non-existent axis",
				axisName: "z",
				wantSize: 0,
				wantErr:  true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				size, err := mesh.AxisSize(tt.axisName)
				if tt.wantErr {
					if err == nil {
						t.Fatalf("expected error for non-existent axis, got nil")
					}
					if !strings.Contains(err.Error(), "not found") {
						t.Errorf("expected error to contain %q, got %v", "not found", err)
					}
				} else {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if size != tt.wantSize {
						t.Errorf("want size %v, got %v", tt.wantSize, size)
					}
				}
			})
		}
	})

	t.Run("String", func(t *testing.T) {
		tests := []struct {
			name      string
			shape     []int
			axisNames []string
			want      string
		}{
			{
				name:      "1D mesh",
				shape:     []int{8},
				axisNames: []string{"replica"},
				want:      "DeviceMesh(axesSizes={replica: 8})",
			},
			{
				name:      "2D mesh",
				shape:     []int{2, 4},
				axisNames: []string{"x", "y"},
				want:      "DeviceMesh(axesSizes={x: 2, y: 4})",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mesh, err := distributed.NewDeviceMesh(tt.shape, tt.axisNames)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if mesh.String() != tt.want {
					t.Errorf("want %q, got %q", tt.want, mesh.String())
				}
			})
		}
	})

	t.Run("SetDeviceAssignment_Valid", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{4}, []string{"replica"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tests := []struct {
			name    string
			devices []int
		}{
			{
				name:    "sequential mapping",
				devices: []int{0, 1, 2, 3},
			},
			{
				name:    "reverse mapping",
				devices: []int{3, 2, 1, 0},
			},
			{
				name:    "custom mapping",
				devices: []int{2, 1, 3, 0},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := mesh.SetLogicalDeviceAssignment(tt.devices...)
				if err != nil {
					t.Fatalf("failed test %q: %v", tt.name, err)
				}
			})
		}
	})

	t.Run("SetDeviceAssignment_Errors", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{4}, []string{"replica"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tests := []struct {
			name    string
			devices []int
			wantErr string
		}{
			{
				name:    "wrong number of devices",
				devices: []int{0, 1, 2},
				wantErr: "devices must have 4 elements",
			},
			{
				name:    "duplicate device",
				devices: []int{0, 1, 1, 3},
				wantErr: "physical device #1 is duplicated",
			},
			{
				name:    "device out of range (negative)",
				devices: []int{0, 1, -1, 3},
				wantErr: "devices must be between 0 and 3",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := mesh.SetLogicalDeviceAssignment(tt.devices...)
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error to contain %q, got %v", tt.wantErr, err)
				}
			})
		}
	})

	t.Run("DeviceToMesh_2D", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{2, 4}, []string{"x", "y"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mesh.NumDevices() != 8 {
			t.Fatalf("want 8 devices, got %v", mesh.NumDevices())
		}
	})

	t.Run("DeviceToMesh_3D", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{2, 2, 2}, []string{"x", "y", "z"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mesh.NumDevices() != 8 {
			t.Fatalf("want 8 devices, got %v", mesh.NumDevices())
		}
	})

	t.Run("DeviceToMesh_WithCustomMapping", func(t *testing.T) {
		mesh, err := distributed.NewDeviceMesh([]int{4}, []string{"replica"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		err = mesh.SetLogicalDeviceAssignment(3, 2, 1, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mesh.NumDevices() != 4 {
			t.Fatalf("want 4 devices, got %v", mesh.NumDevices())
		}
		err = mesh.SetLogicalDeviceAssignment(4, 2, 1, 0)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("ComputeReplicaGroups", func(t *testing.T) {
		t.Run("2D mesh batch groups", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2}, []string{"batch", "data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Example from comments: m.ComputeReplicaGroups([]string{"batch"}) -> [][]int{{0, 2}, {1, 3}}
			groups, err := mesh.ComputeReplicaGroups([]string{"batch"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0, 2}, {1, 3}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("2D mesh data groups", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2}, []string{"batch", "data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Example from comments: m.ComputeReplicaGroups([]string{"data"}) -> [][]int{{0, 1}, {2, 3}}
			groups, err := mesh.ComputeReplicaGroups([]string{"data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0, 1}, {2, 3}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("2D mesh global groups", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2}, []string{"batch", "data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Example from comments: m.ComputeReplicaGroups([]string{"batch", "data"}) -> [][]int{{0, 1, 2, 3}}
			groups, err := mesh.ComputeReplicaGroups([]string{"batch", "data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0, 1, 2, 3}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("1D mesh", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{4}, []string{"replica"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			groups, err := mesh.ComputeReplicaGroups([]string{"replica"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0, 1, 2, 3}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("3D mesh single axis", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2, 2}, []string{"x", "y", "z"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Groups along x axis: should split by y and z
			groups, err := mesh.ComputeReplicaGroups([]string{"x"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0, 4}, {1, 5}, {2, 6}, {3, 7}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("3D mesh two axes", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2, 2}, []string{"x", "y", "z"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Groups along x and y axes: should split by z
			groups, err := mesh.ComputeReplicaGroups([]string{"x", "y"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0, 2, 4, 6}, {1, 3, 5, 7}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("empty axes list", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2}, []string{"batch", "data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Empty axes list: each device is its own group
			groups, err := mesh.ComputeReplicaGroups([]string{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok, diff := testutil.IsEqual([][]int{{0}, {1}, {2}, {3}}, groups); !ok {
				t.Errorf("groups mismatch: diff\n%s", diff)
			}
		})

		t.Run("non-existent axis", func(t *testing.T) {
			mesh, err := distributed.NewDeviceMesh([]int{2, 2}, []string{"batch", "data"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// A non-existent axis should return an error.
			_, err = mesh.ComputeReplicaGroups([]string{"nonexistent"})
			if err == nil {
				t.Fatalf("expected error for nonexistent axis, got nil")
			}
		})
	})
}
