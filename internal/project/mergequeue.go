package project

import (
	"context"
	"sort"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// QueueEntry is one task's place in the merge queue (#522, migration 017).
type QueueEntry = state.QueueEntry

// Merge queue states.
const (
	QueueQueued  = state.QueueQueued
	QueueMerged  = state.QueueMerged
	QueueEjected = state.QueueEjected
)

// QueueReader reads the merge queue; state.SQLite is one.
type QueueReader interface {
	QueueEntries(ctx context.Context) ([]QueueEntry, error)
}

// SortQueue puts merge queue entries in the order the queue merges them
// (#522 decision 1): phase first, then how many tasks depend on each,
// directly or not (more first: it unblocks more work), then when each
// entered, then id. The engine merges in this order and the dashboard shows
// it, so both read it from here.
func SortQueue(entries []QueueEntry, tasks []model.Task) {
	phase := map[string]int{}
	for _, t := range tasks {
		phase[t.ID] = t.Phase
	}
	deps := TransitiveDependents(tasks)
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if phase[a.TaskID] != phase[b.TaskID] {
			return phase[a.TaskID] < phase[b.TaskID]
		}
		if deps[a.TaskID] != deps[b.TaskID] {
			return deps[a.TaskID] > deps[b.TaskID]
		}
		if !a.EnqueuedAt.Equal(b.EnqueuedAt) {
			return a.EnqueuedAt.Before(b.EnqueuedAt)
		}
		return a.TaskID < b.TaskID
	})
}

// TransitiveDependents counts, per task, the tasks that depend on it
// directly or through others.
func TransitiveDependents(tasks []model.Task) map[string]int {
	children := map[string][]string{}
	for _, t := range tasks {
		for _, d := range t.Dependencies {
			children[d] = append(children[d], t.ID)
		}
	}
	out := map[string]int{}
	for _, t := range tasks {
		seen := map[string]bool{}
		stack := append([]string(nil), children[t.ID]...)
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[id] {
				continue
			}
			seen[id] = true
			stack = append(stack, children[id]...)
		}
		out[t.ID] = len(seen)
	}
	return out
}
