package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
)

// OpenArtifactFile exports a regular file from this sandbox. Local worktree
// mode uses os.Root, which prevents symlink and parent traversal onto the host.
func (s *Sandbox) OpenArtifactFile(ctx context.Context, name string) (io.ReadCloser, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\x00\r\n") {
		return nil, errors.New("invalid export path")
	}
	workspace := s.workspaceDir()
	if workspace == "" {
		return nil, errors.New("sandbox workspace is unavailable")
	}
	if s.local != nil {
		root, err := os.OpenRoot(workspace)
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		if filepath.IsAbs(name) {
			name, err = filepath.Rel(workspace, name)
			if err != nil {
				return nil, err
			}
		}
		f, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			_ = f.Close()
			return nil, errors.New("only regular workspace files can be exported")
		}
		return f, nil
	}
	// SFTP opens the path directly, so file names never pass through a remote
	// shell or the shell argv allowlist.
	client, err := s.creds().dial(ctx)
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sandbox sftp: %w", err)
	}
	closeAll := func() { _ = sc.Close(); _ = client.Close() }
	stop := context.AfterFunc(ctx, closeAll)
	f, err := openSFTPExport(sc, workspace, name)
	if err != nil {
		stop()
		closeAll()
		return nil, err
	}
	return &sftpExport{File: f, closeSource: func() { stop(); closeAll() }}, nil
}

// exportRoots are the guest directories files may be exported from, besides
// the workspace.
var exportRoots = []string{"/tmp"}

// openSFTPExport opens name (relative to workspace, or absolute) for reading.
// It relies on SSH_FXP_REALPATH resolving symlinks, as OpenSSH's sftp-server
// does, so links cannot point outside the export roots.
func openSFTPExport(sc *sftp.Client, workspace, name string) (*sftp.File, error) {
	if !path.IsAbs(name) {
		name = path.Join(workspace, name)
	}
	target, err := sc.RealPath(name)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, root := range append([]string{workspace}, exportRoots...) {
		real, err := sc.RealPath(root)
		if err == nil && withinRoot(target, real) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, errors.New("path outside export roots")
	}
	f, err := sc.Open(target)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("only regular workspace files can be exported")
	}
	return f, nil
}

func withinRoot(p, root string) bool {
	root = path.Clean(root)
	p = path.Clean(p)
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}

type sftpExport struct {
	*sftp.File
	closeSource func()
}

func (r *sftpExport) Close() error {
	err := r.File.Close()
	r.closeSource()
	return err
}

type exportReader struct {
	*io.PipeReader
	closeSource func()
}

func (r *exportReader) Close() error { r.closeSource(); return r.PipeReader.Close() }
