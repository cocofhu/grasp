package services

// Pause stops new claims and publication discovery for maintenance. Accepted
// queued receipts remain queued; dispatched/unknown operations are never replayed.
// Already owned operations finish or are cancelled by their own lifetime ctx.
func (s *WorkspaceSnapshotJobs) Pause() { s.paused.Store(true) }

// ActiveOperations is a drain barrier, including scheduled goroutines which
// have not yet entered RunOne. Its value may count a running worker twice;
// only zero is used as permission to stop the process.
func (s *WorkspaceSnapshotJobs) ActiveOperations() int64 {
	active := s.operations.Load()
	s.local.Range(func(_, _ any) bool { active++; return true })
	return active
}
