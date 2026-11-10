package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// OpenArtifactFile exports a regular file from this sandbox. Local worktree
// mode uses os.Root, which prevents symlink and parent traversal onto the host.
func (s *Sandbox) OpenArtifactFile(ctx context.Context, name string) (io.ReadCloser, error) {
	return s.OpenArtifactFileAtWorkspace(ctx, s.workspaceDir(), name)
}

// OpenArtifactFileAtWorkspace pins a trusted, server-resolved root for one
// export without changing the sandbox's shared active workspace. workspace
// must come from canonical workspace resolution, never from browser input.
func (s *Sandbox) OpenArtifactFileAtWorkspace(ctx context.Context, workspace, name string) (io.ReadCloser, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\x00\r\n") {
		return nil, errors.New("invalid export path")
	}
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
		f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
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
	// The source process only opens paths inside the authorized guest, never
	// paths on the server. Python is already part of the sandbox contract.
	// Keep the fixed program on stdin. Only encoded data passes through the
	// existing strict argv validator; paths with spaces or non-ASCII names
	// never become shell syntax, and the validator needs no script exception.
	script := "import base64,os,sys,stat,shutil; w=os.path.realpath(os.fsdecode(base64.b64decode(sys.argv[1],validate=True))); n=os.fsdecode(base64.b64decode(sys.argv[2],validate=True)); p=os.path.realpath(n if os.path.isabs(n) else os.path.join(w,n)); assert any(os.path.commonpath([p,r])==r for r in [w,'/tmp']), 'path outside export roots'; fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK); assert stat.S_ISREG(os.fstat(fd).st_mode), 'not a regular file'; shutil.copyfileobj(os.fdopen(fd,'rb'),sys.stdout.buffer,1048576)"
	cmd, err := newSafeCmd("python3", "-", base64.StdEncoding.EncodeToString([]byte(workspace)), base64.StdEncoding.EncodeToString([]byte(name)))
	if err != nil {
		return nil, err
	}
	command, err := cmd.render()
	if err != nil {
		return nil, err
	}
	client, err := s.creds().dial(ctx)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	reader, writer := io.Pipe()
	session.Stdin = strings.NewReader(script)
	session.Stdout = writer
	session.Stderr = io.Discard
	cancel := context.AfterFunc(ctx, func() { _ = session.Close(); _ = client.Close() })
	go func() {
		err := session.Run(command)
		_ = writer.CloseWithError(err)
		cancel()
		_ = session.Close()
		_ = client.Close()
	}()
	return &exportReader{PipeReader: reader, closeSource: func() { cancel(); _ = session.Close(); _ = client.Close() }}, nil
}

type exportReader struct {
	*io.PipeReader
	closeSource func()
}

func (r *exportReader) Close() error { r.closeSource(); return r.PipeReader.Close() }
