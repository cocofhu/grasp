package services

// Pause is an admission barrier for maintenance. It shares the lock used to
// count every operation before its first metadata or provider request, so zero
// cannot race with an already admitted but not-yet-scheduled native operation.
// Existing owners finish or follow their own finite cancellation/lease bounds.
func (s *WorkspaceReleaseService) Pause() {
	if s == nil {
		return
	}
	s.admissionMu.Lock()
	s.paused = true
	s.admissionMu.Unlock()
}
func (s *WorkspaceReleaseService) ActiveOperations() int64 {
	if s == nil {
		return 0
	}
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	return s.operations
}
func (s *WorkspaceReleaseService) releasePaused() bool {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	return s.paused
}
func (s *WorkspaceReleaseService) beginRelease() error {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	if s.paused {
		return ErrWorkspaceReleaseBlocked
	}
	s.operations++
	return nil
}
func (s *WorkspaceReleaseService) finishRelease() {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	s.operations--
}
