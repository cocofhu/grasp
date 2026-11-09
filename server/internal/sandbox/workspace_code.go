package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Workspace code transfers are a versioned runtime capability. They move only
// selected immutable commits, never a caller-supplied host path or working tree.
const (
	WorkspaceCodeVersion               = 1
	WorkspaceCodeMaxRepositories       = 8
	WorkspaceCodeMaxBundleBytes  int64 = 64 << 20
	WorkspaceCodeMaxTotalBytes   int64 = 128 << 20
	WorkspaceCodeMaxFileBytes    int64 = 32 << 20
	WorkspaceCodeDirectory             = ".grasp-code-imports"
)

type WorkspaceCodeSelection struct {
	Path   string `json:"path"`
	Commit string `json:"commit"`
}

type WorkspaceCodeRepository struct {
	Path      string `json:"path"`
	Commit    string `json:"commit"`
	Head      string `json:"head"`
	Dirty     bool   `json:"dirty"`
	Untracked bool   `json:"untracked"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

type WorkspaceCodeInventory struct {
	Version        int                       `json:"version"`
	Repositories   []WorkspaceCodeRepository `json:"repositories"`
	WorkingChanges bool                      `json:"workingChanges"`
	NonGit         bool                      `json:"nonGit"`
	IgnoredFiles   bool                      `json:"ignoredFiles"`
	MaxBundleBytes int64                     `json:"maxBundleBytes"`
	MaxTotalBytes  int64                     `json:"maxTotalBytes"`
	MaxFileBytes   int64                     `json:"maxFileBytes"`
}

type WorkspaceCodeManifest struct {
	Version      int                       `json:"version"`
	Repositories []WorkspaceCodeRepository `json:"repositories"`
}

type WorkspaceCodeReceipt struct {
	ID            string                    `json:"id"`
	ArchiveSHA256 string                    `json:"archiveSha256"`
	Directory     string                    `json:"directory"`
	Repositories  []WorkspaceCodeRepository `json:"repositories"`
}

type WorkspaceCodeError struct {
	Status int
	Code   string
}

type WorkspaceCodeFence struct {
	Generation uint64
	ExpiresAt  time.Time
}

func workspaceCodeRequest(ctx context.Context, host string, port int, password, path, digest string, source io.Reader, fence WorkspaceCodeFence) (*http.Response, error) {
	if fence.Generation == 0 || !fence.ExpiresAt.After(time.Now()) {
		return nil, &WorkspaceCodeError{Status: http.StatusConflict, Code: "workspace_operation_expired"}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	cookie, err := bridgeLogin(ctx, host, port, password)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s:%d%s", host, port, path), source)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Grasp-Operation-Generation", strconv.FormatUint(fence.Generation, 10))
	req.Header.Set("X-Grasp-Operation-Expires-At", fence.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if digest != "" {
		req.Header.Set("Content-Type", "application/gzip")
		req.Header.Set("X-Grasp-Archive-SHA256", digest)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	return (&http.Client{Timeout: 2 * time.Minute}).Do(req)
}

func (e *WorkspaceCodeError) Error() string {
	return fmt.Sprintf("workspace code: %s (status %d)", e.Code, e.Status)
}

func workspaceCodeResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body)
	if body.Code == "" {
		body.Code = "workspace_code_unavailable"
	}
	return &WorkspaceCodeError{Status: resp.StatusCode, Code: body.Code}
}

func InspectWorkspaceCode(ctx context.Context, host string, port int, password string) (WorkspaceCodeInventory, error) {
	var result WorkspaceCodeInventory
	resp, err := chatRequestTimeout(ctx, http.MethodGet, host, port, password, "/api/workspace/code", nil, time.Minute)
	if err != nil {
		return result, err
	}
	if err := workspaceCodeResponse(resp); err != nil {
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result)
	return result, err
}

// The caller must consume a bounded archive and close it even on validation
// failure. No unverified remote body is ever published as an import receipt.
func ExportWorkspaceCode(ctx context.Context, host string, port int, password string, selected []WorkspaceCodeSelection, fence WorkspaceCodeFence) (io.ReadCloser, error) {
	body, err := json.Marshal(map[string]any{"repositories": selected})
	if err != nil {
		return nil, err
	}
	resp, err := workspaceCodeRequest(ctx, host, port, password, "/api/workspace/code/export", "", bytes.NewReader(body), fence)
	if err != nil {
		return nil, err
	}
	if err := workspaceCodeResponse(resp); err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func ImportWorkspaceCode(ctx context.Context, host string, port int, password, id, digest string, source io.Reader, fence WorkspaceCodeFence) (WorkspaceCodeReceipt, error) {
	var result WorkspaceCodeReceipt
	resp, err := workspaceCodeRequest(ctx, host, port, password, "/api/workspace/code/imports/"+url.PathEscape(id), digest, source, fence)
	if err != nil {
		return result, err
	}
	if err := workspaceCodeResponse(resp); err != nil {
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result)
	return result, err
}

func GetWorkspaceCodeReceipt(ctx context.Context, host string, port int, password, id string) (WorkspaceCodeReceipt, error) {
	var result WorkspaceCodeReceipt
	resp, err := chatRequestTimeout(ctx, http.MethodGet, host, port, password, "/api/workspace/code/imports/"+url.PathEscape(id), nil, time.Minute)
	if err != nil {
		return result, err
	}
	if err := workspaceCodeResponse(resp); err != nil {
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result)
	return result, err
}
