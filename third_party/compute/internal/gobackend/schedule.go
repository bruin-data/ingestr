package gobackend

import (
	"slices"

	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

// This file implements different scheduling strategies: this defines the order in which the nodes
// of a function are executed.

// ScheduleStrategy defines the scheduling strategy to execute functions.
type ScheduleStrategy int

//go:generate go tool enumer -type ScheduleStrategy -trimprefix=Schedule -output=gen_schedulestrategy_enumer.go schedule.go

const (
	DependencyOrderSchedule ScheduleStrategy = iota
	CreationOrderSchedule
)

// InitSchedule initializes the execution schedule.
func (fe *FunctionExecutable) InitSchedule() {
	switch fe.Backend.SchedulingStrategy {
	case DependencyOrderSchedule:
		fe.DependencyOrderSchedule()
	case CreationOrderSchedule:
		fe.CreationOrderSchedule()
	default:
		panic(errors.Errorf("unknown schedule strategy %s", fe.Backend.SchedulingStrategy))
	}
}

// CreationOrderSchedule schedule nodes in creation order: a trivial sequentially increasing order.
func (fe *FunctionExecutable) CreationOrderSchedule() {
	fe.Schedule = xslices.Iota(0, fe.NumNodesToProcess)
}

// DependencyOrderSchedule schedules nodes in topological order, based on dependencies.
//
// It schedules those with the longest path to an output first. If there are
// multiple nodes with the same longest path to an output, it will schedule
// them in creation order.
func (fe *FunctionExecutable) DependencyOrderSchedule() {
	pathLen := make([]int, fe.NumNodesToProcess)
	isOutput := make([]bool, fe.NumNodesToProcess)
	for _, outNode := range fe.OutputNodes {
		if outNode.Index < fe.NumNodesToProcess {
			isOutput[outNode.Index] = true
		}
	}

	for i := fe.NumNodesToProcess - 1; i >= 0; i-- {
		if fe.NumUses[i] == 0 {
			pathLen[i] = -1
			continue
		}
		maxDepPath := -1
		for _, depIdx := range fe.Dependents[i] {
			if pathLen[depIdx] > maxDepPath {
				maxDepPath = pathLen[depIdx]
			}
		}
		if maxDepPath != -1 {
			pathLen[i] = 1 + maxDepPath
		} else if isOutput[i] {
			pathLen[i] = 0
		} else {
			pathLen[i] = -1
		}
	}

	fe.Schedule = make([]int, 0, fe.NumNodesToProcess)
	for i := range fe.NumNodesToProcess {
		if pathLen[i] != -1 {
			fe.Schedule = append(fe.Schedule, i)
		}
	}

	slices.SortFunc(fe.Schedule, func(a, b int) int {
		if pathLen[a] != pathLen[b] {
			return pathLen[b] - pathLen[a] // Descending by path length
		}
		return a - b // Ascending by creation order (index)
	})
}
