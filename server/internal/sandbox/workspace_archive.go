package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// WorkspaceArchiveExcludes are regenerable dependency and cache directories
// left out of a stopped Solo workspace archive.
var WorkspaceArchiveExcludes = []string{"node_modules", ".venv", "__pycache__", ".cache", ".pnpm-store"}

const workspaceRestoreTimeout = 30 * time.Minute

func archiveArgs(workspace string) []string {
	args := []string{"tar", "-C", workspace, "-czf", "-"}
	for _, name := range WorkspaceArchiveExcludes {
		args = append(args, "--exclude="+name)
	}
	return append(args, ".")
}

// archiveExitErr accepts GNU tar's exit 1: a file changed while being read,
// but the archive is still complete and usable.
func archiveExitErr(err error) error {
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.ExitStatus() == 1 {
		return nil
	}
	return err
}

// ArchiveWorkspace streams the guest workspace as a gzip tar. The reader
// returns the command's error at the end of a failed stream, so a store only
// commits a complete archive.
func (s *Sandbox) ArchiveWorkspace(ctx context.Context) (io.ReadCloser, error) {
	if s.IsLocal() {
		return nil, errors.New("local worktree sandboxes have no guest workspace to archive")
	}
	workspace := s.workspaceDir()
	if strings.TrimSpace(workspace) == "" {
		return nil, errors.New("sandbox workspace is unavailable")
	}
	cmd, err := newSafeCmd(archiveArgs(workspace)...)
	if err != nil {
		return nil, err
	}
	command, err := cmd.render()
	if err != nil {
		return nil, err
	}
	c := s.creds()
	if execHook != nil {
		out, err := execHook(ctx, c.host, c.port, command, nil)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(out)), nil
	}
	client, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	reader, writer := io.Pipe()
	session.Stdout = writer
	session.Stderr = io.Discard
	stop := context.AfterFunc(ctx, func() { _ = session.Close(); _ = client.Close() })
	go func() {
		err := archiveExitErr(session.Run(command))
		_ = writer.CloseWithError(c.redactErr(err))
		stop()
		_ = session.Close()
		_ = client.Close()
	}()
	return &exportReader{PipeReader: reader, closeSource: func() { stop(); _ = session.Close(); _ = client.Close() }}, nil
}

// RestoreWorkspace extracts a gzip tar produced by ArchiveWorkspace into the
// guest workspace.
func (s *Sandbox) RestoreWorkspace(ctx context.Context, archive io.Reader) error {
	if s.IsLocal() {
		return errors.New("local worktree sandboxes have no guest workspace to restore")
	}
	workspace := s.workspaceDir()
	if strings.TrimSpace(workspace) == "" {
		return errors.New("sandbox workspace is unavailable")
	}
	mkdir, err := newSafeCmd("mkdir", "-p", workspace)
	if err != nil {
		return err
	}
	c := s.creds()
	if _, err := c.run(ctx, time.Minute, mkdir); err != nil {
		return err
	}
	cmd, err := newSafeCmd("tar", "-C", workspace, "-xzf", "-")
	if err != nil {
		return err
	}
	out, err := c.runInput(ctx, workspaceRestoreTimeout, cmd, archive)
	if err != nil {
		if tail := lastLine(string(out)); tail != "" {
			return errors.New("restore workspace: " + tail)
		}
		return err
	}
	return nil
}
