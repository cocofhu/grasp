package sandbox

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed workspace_changes.py
var workspaceChangesScript string

// WorkspaceTree is a frozen Git object and its checkout metadata.
type WorkspaceTree struct {
	Ref         string `json:"ref"`
	Head        string `json:"head"`
	Branch      string `json:"branch"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

type WorkspaceTreeQuery struct {
	Directory string `json:"directory"`
	Pin       string `json:"pin,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	TreeOnly  bool   `json:"treeOnly,omitempty"`
}

type WorkspaceTreeChanges struct {
	Added       int                 `json:"added"`
	Deleted     int                 `json:"deleted"`
	FileCount   int                 `json:"fileCount"`
	Unavailable bool                `json:"unavailable"`
	Files       []WorkspaceTreeFile `json:"files"`
}

type WorkspaceTreeFile struct {
	Path         string  `json:"path"`
	PreviousPath string  `json:"previousPath"`
	Status       string  `json:"status"`
	Added        *int    `json:"added"`
	Deleted      *int    `json:"deleted"`
	Patch        *[]byte `json:"patch"`
	Before       *[]byte `json:"before"`
	After        *[]byte `json:"after"`
}

func (s *Sandbox) CaptureWorkspaceTrees(ctx context.Context, queries []WorkspaceTreeQuery) ([]WorkspaceTree, error) {
	return runWorkspaceGit[WorkspaceTree](ctx, s, "capture", queries)
}

func (s *Sandbox) CompareWorkspaceTrees(ctx context.Context, queries []WorkspaceTreeQuery) ([]WorkspaceTreeChanges, error) {
	return runWorkspaceGit[WorkspaceTreeChanges](ctx, s, "compare", queries)
}

// ExportWorkspaceTree streams a standalone pack of the tree and its file
// objects. A new clone can load it without contacting the remote.
func (s *Sandbox) ExportWorkspaceTree(ctx context.Context, query WorkspaceTreeQuery) (io.ReadCloser, error) {
	script, err := s.workspaceGitScript("export-tree", []WorkspaceTreeQuery{query})
	if err != nil {
		return nil, err
	}
	cmd, err := newSafeCmd("python3", "-s")
	if err != nil {
		return nil, err
	}
	return s.creds().streamOutput(ctx, cmd, strings.NewReader(script))
}

// RestoreWorkspaceTree streams the pack to Git's object database. It does not
// check out files or update the real index.
func (s *Sandbox) RestoreWorkspaceTree(ctx context.Context, query WorkspaceTreeQuery, pack io.Reader) error {
	script, err := s.workspaceGitScript("restore-tree", []WorkspaceTreeQuery{query})
	if err != nil {
		return err
	}
	file := "/tmp/grasp-review-" + uuid.NewString() + ".py"
	cmd, err := newSafeCmd("python3", file)
	if err != nil {
		return err
	}
	err = s.WriteFileAtomic(ctx, file, []byte(script))
	if err == nil {
		_, err = s.creds().runInput(ctx, 25*time.Second, cmd, pack)
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, _ = s.Exec(cleanup, 2*time.Second, "rm", "-f", "--", file)
	}
	return err
}

func (s *Sandbox) workspaceGitScript(operation string, queries []WorkspaceTreeQuery) (string, error) {
	req, err := json.Marshal(struct {
		Workspace    string               `json:"workspace"`
		Operation    string               `json:"operation"`
		Repositories []WorkspaceTreeQuery `json:"repositories"`
	}{s.workspaceDir(), operation, queries})
	if err != nil {
		return "", err
	}
	return "import base64, json\nrequest = json.loads(base64.b64decode(" +
		strconv.Quote(base64.StdEncoding.EncodeToString(req)) + "))\n" + workspaceChangesScript, nil
}

func runWorkspaceGit[T any](ctx context.Context, s *Sandbox, operation string, queries []WorkspaceTreeQuery) ([]T, error) {
	script, err := s.workspaceGitScript(operation, queries)
	if err != nil {
		return nil, err
	}
	result, err := s.ExecScript(ctx, 25*time.Second, "python3", script)
	if err != nil {
		return nil, fmt.Errorf("read workspace changes: %w", err)
	}
	var values []T
	if err := json.Unmarshal([]byte(result), &values); err != nil || len(values) != len(queries) {
		return nil, fmt.Errorf("invalid workspace changes response")
	}
	return values, nil
}
