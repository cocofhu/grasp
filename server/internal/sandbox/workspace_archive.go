package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"
)

// WorkspaceArchiveExcludes are regenerable dependency and cache directories
// left out of a workspace recovery archive.
var WorkspaceArchiveExcludes = []string{"node_modules", ".venv", "__pycache__", ".cache", ".pnpm-store"}

const workspaceRestoreTimeout = 30 * time.Minute

func archiveArgs(workspace string) []string {
	args := []string{"tar", "-C", workspace, "-czf", "-"}
	for _, name := range WorkspaceArchiveExcludes {
		args = append(args, "--exclude="+name)
	}
	return append(args, ".")
}

// ArchiveWorkspace streams the guest workspace as a gzip tar. The reader
// returns the command's error at the end of a failed stream, so a store only
// commits a complete archive.
func (s *Sandbox) ArchiveWorkspace(ctx context.Context) (io.ReadCloser, error) {
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
		err := session.Run(command)
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
	workspace := s.workspaceDir()
	if strings.TrimSpace(workspace) == "" {
		return errors.New("sandbox workspace is unavailable")
	}
	if !path.IsAbs(workspace) || path.Clean(workspace) == "/" {
		return errors.New("refusing to replace an unsafe workspace root")
	}
	mkdir, err := newSafeCmd("mkdir", "-p", workspace)
	if err != nil {
		return err
	}
	c := s.creds()
	if _, err := c.run(ctx, time.Minute, mkdir); err != nil {
		return err
	}
	// A recreated image can clone files that the saved workspace had deleted.
	// Empty its dedicated workspace first so restoration also preserves those
	// deletions and a retry cannot retain a partially extracted old archive.
	clear, err := newSafeCmd("find", workspace, "-xdev", "-mindepth", "1", "-delete")
	if err != nil {
		return err
	}
	if _, err := c.run(ctx, workspaceRestoreTimeout, clear); err != nil {
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
