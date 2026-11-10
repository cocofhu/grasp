package workspacejob

import (
	"os"
	"syscall"
)

// LiveHandle checks the original private operation capability without renewing
// it. Artifact requests cannot manufacture a later expiry or revive a worker.
func (s *Service) LiveHandle(id string, req RenewRequest) (*Handle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, handle, err := s.ownedLocked(id, req)
	return handle, err
}

// OpenCompletedArtifact is deliberately restricted to the two immutable capture
// products. A trusted controller validates their descriptors; HTTP never supplies
// a filesystem path and a new read job supplies the current operation fence.
func (s *Service) OpenCompletedArtifact(id, hash, name string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return nil, ErrClosed
	}
	row := s.jobs[id]
	if row == nil {
		return nil, os.ErrNotExist
	}
	if row.Receipt.State != "completed" || row.Receipt.Kind != "capture_files" || row.Receipt.SHA256 != hash || (name != "workspace.tar.gz" && name != "manifest.json") {
		return nil, ErrConflict
	}
	file, err := s.root.OpenFile("data/"+id+"/"+name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, ErrCorrupt
	}
	return file, nil
}
