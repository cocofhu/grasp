package services

import "sync"

// runMutationMu guards the per-run mutex map. The mutex itself serializes
// save-failed deletion with failure retry so one cannot observe the other's
// halfway status.
var (
	runMutationMu    sync.Mutex
	runMutationLocks = map[string]*sync.Mutex{}
)

// LockRunMutation excludes save-failed deletion from a failure retry of the
// same run. The critical section covers the status check, sandbox Destroy,
// and row removal on the delete side, and the checkpoint cancel through
// admission on the retry side. Callers acquire this before any row lock.
func LockRunMutation(runID string) func() {
	runMutationMu.Lock()
	m, ok := runMutationLocks[runID]
	if !ok {
		m = &sync.Mutex{}
		runMutationLocks[runID] = m
	}
	runMutationMu.Unlock()
	m.Lock()
	return m.Unlock
}
