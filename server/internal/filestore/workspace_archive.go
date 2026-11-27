package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const workspaceArchiveNamespace = "workspace"

type workspaceArchiveKey struct{}

func validArchiveID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func (s *Store) workspaceArchive(id string) string {
	return filepath.Join(s.Root, workspaceArchiveNamespace, id)
}

// PrepareWorkspaceArchive scopes writes to one disposable archive. Identical
// payloads in other archives or in the shared store have their own files.
func (s *Store) PrepareWorkspaceArchive(ctx context.Context, id string) (context.Context, error) {
	if !validArchiveID(id) {
		return nil, errors.New("invalid workspace archive ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := s.workspaceArchive(id)
	if err := os.MkdirAll(filepath.Join(dir, "sha256"), 0o700); err != nil {
		return nil, err
	}
	if err := errors.Join(syncDir(dir), syncDir(filepath.Dir(dir)), syncDir(s.Root)); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, workspaceArchiveKey{}, id), nil
}

// RemoveWorkspaceArchive is only for IDs retired in a committed SQL
// transaction. The shared store and other workspace archives are separate.
func (s *Store) RemoveWorkspaceArchive(ctx context.Context, id string) error {
	if !validArchiveID(id) {
		return errors.New("invalid workspace archive ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := s.workspaceArchive(id)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(dir)); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
