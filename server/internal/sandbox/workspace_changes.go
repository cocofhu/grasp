package sandbox

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
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

func runWorkspaceGit[T any](ctx context.Context, s *Sandbox, operation string, queries []WorkspaceTreeQuery) ([]T, error) {
	req, err := json.Marshal(struct {
		Workspace    string               `json:"workspace"`
		Operation    string               `json:"operation"`
		Repositories []WorkspaceTreeQuery `json:"repositories"`
	}{s.workspaceDir(), operation, queries})
	if err != nil {
		return nil, err
	}
	script := "import base64, json\nrequest = json.loads(base64.b64decode(" +
		strconv.Quote(base64.StdEncoding.EncodeToString(req)) + "))\n" + workspaceChangesScript
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
