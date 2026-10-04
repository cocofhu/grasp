package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ChangedFile is one VCS-neutral changed-file record reported by a sandbox.
type ChangedFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// Commit is one commit in the baseSha..HEAD range.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	At      string `json:"at"`
}

// Changes is the VCS-neutral code-change report a sandbox returns from the
// protocol's change-reporting capability (GET /api/changes). VCS=="none" means
// the workspace is not under version control (no changes to report).
type Changes struct {
	VCS        string `json:"vcs"`
	Branch     string `json:"branch"`
	BaseBranch string `json:"baseBranch"`
	NewBranch  bool   `json:"newBranch"`
	BaseSHA    string `json:"baseSha"`
	HeadSHA    string `json:"headSha"`
	Dirty      bool   `json:"dirty"`
	Ahead      int    `json:"ahead"`
	// Pushed reports whether the current commit reached the remote (origin).
	// A local-only commit is a key downstream signal: CI / MR steps need it.
	Pushed       bool          `json:"pushed"`
	RemoteSHA    string        `json:"remoteSha"`
	Unpushed     int           `json:"unpushed"`
	ChangedFiles []ChangedFile `json:"changedFiles"`
	DiffStat     string        `json:"diffStat"`
	Commits      []Commit      `json:"commits"`
	// Repos is populated in multi-repo (flat) mode (VCS=="multi"): one entry per
	// flat clone root under the workspace. Empty in single-repo mode.
	Repos []RepoChanges `json:"repos,omitempty"`
}

// RepoChanges is one repository's change report in multi-repo mode. It carries
// the same VCS-neutral fields as Changes plus the repo's Name and workspace
// Path (e.g. /root/workspace/<name>).
type RepoChanges struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Changes
}

// FetchChanges asks a live sandbox for the code changes its workspace accrued
// relative to the session baseline, via the protocol endpoint GET /api/changes.
// Best-effort: callers treat a nil/error result as "no change report available"
// and degrade gracefully (the platform never shells git itself).
//
// Unwired protocol stub: written against GET /api/changes but not yet wired
// into Manager / live session paths; keep in-tree, do not treat as dead code.
func fetchChanges(ctx context.Context, host string, port int) (*Changes, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d/api/changes", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("changes GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("changes %d", resp.StatusCode)
	}
	var ch Changes
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		return nil, fmt.Errorf("changes decode: %w", err)
	}
	return &ch, nil
}
