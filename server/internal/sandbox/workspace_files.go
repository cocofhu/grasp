package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const WorkspaceFileChunkBytes = 1 << 20

type WorkspaceFileLimits struct {
	MaxFileBytes     int64 `json:"maxFileBytes"`
	MaxBytes         int64 `json:"maxBytes"`
	MaxGitBytes      int64 `json:"maxGitBytes"`
	MaxArchiveBytes  int64 `json:"maxArchiveBytes"`
	MaxManifestBytes int64 `json:"maxManifestBytes"`
	MaxSpoolBytes    int64 `json:"maxSpoolBytes"`
	MaxEntries       int   `json:"maxEntries"`
	MaxRepositories  int   `json:"maxRepositories"`
	MaxScanEntries   int   `json:"maxScanEntries"`
}
type WorkspaceFileCapabilities struct {
	Version       int                      `json:"version"`
	Jobs          WorkspaceJobCapabilities `json:"jobs"`
	Capture       bool                     `json:"capture"`
	Read          bool                     `json:"read"`
	Restore       bool                     `json:"restore"`
	PolicyVersion string                   `json:"policyVersion"`
	PolicySHA256  string                   `json:"policySha256"`
	Limits        WorkspaceFileLimits      `json:"limits"`
	MaxChunkBytes int                      `json:"maxChunkBytes"`
	Consistency   string                   `json:"consistency"`
	ProcessMemory bool                     `json:"processMemory"`
	Metadata      []string                 `json:"metadata"`
}

func (c WorkspaceJobClient) fileCall(ctx context.Context, method, path string, fence *WorkspaceJobRenew, body []byte, digest string) (*http.Response, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	call, cancel := context.WithTimeout(ctx, timeout)
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	cookie, err := bridgeLogin(call, host, c.Port, c.Password)
	if err != nil {
		cancel()
		return nil, err
	}
	request, err := http.NewRequestWithContext(call, method, fmt.Sprintf("http://%s:%d%s", host, c.Port, path), bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("Cookie", cookie)
	request.Header.Set("Content-Type", "application/octet-stream")
	if fence != nil {
		data, err := json.Marshal(fence)
		if err != nil {
			cancel()
			return nil, err
		}
		request.Header.Set("X-Grasp-Workspace-Fence", base64.RawURLEncoding.EncodeToString(data))
	}
	if digest != "" {
		request.Header.Set("X-Grasp-Snapshot-SHA256", digest)
	}
	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &workspaceFileBody{ReadCloser: response.Body, cancel: cancel}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		defer func() { _ = response.Body.Close() }()
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&failure)
		return nil, &WorkspaceJobError{Status: response.StatusCode, Code: failure.Code}
	}
	return response, nil
}

type workspaceFileBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *workspaceFileBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

func (c WorkspaceJobClient) FileCapabilities(ctx context.Context) (WorkspaceFileCapabilities, error) {
	var result WorkspaceFileCapabilities
	response, err := c.fileCall(ctx, http.MethodGet, "/api/workspace/files/capabilities", nil, nil, "")
	if err != nil {
		return result, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return result, errors.New("workspace file capabilities exceed limit")
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// ReadWorkspaceChunk verifies the bounded transport receipt. Full artifact SHA
// and semantic manifest validation remain the publication consumer's job.
func (c WorkspaceJobClient) ReadWorkspaceChunk(ctx context.Context, id string, fence WorkspaceJobRenew, part string, offset int64, length int) ([]byte, bool, error) {
	if (part != "archive" && part != "manifest") || offset < 0 || length < 1 || length > WorkspaceFileChunkBytes {
		return nil, false, errors.New("invalid workspace artifact range")
	}
	path := "/api/workspace/jobs/" + url.PathEscape(id) + "/artifacts/" + part + "?offset=" + strconv.FormatInt(offset, 10) + "&length=" + strconv.Itoa(length)
	response, err := c.fileCall(ctx, http.MethodGet, path, &fence, nil, "")
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(length)+1))
	if err != nil || len(data) > length {
		return nil, false, errors.New("workspace artifact chunk exceeds requested range")
	}
	actualOffset, offsetErr := strconv.ParseInt(response.Header.Get("X-Grasp-Snapshot-Offset"), 10, 64)
	last, lastErr := strconv.ParseBool(response.Header.Get("X-Grasp-Snapshot-EOF"))
	hash := sha256.Sum256(data)
	if offsetErr != nil || lastErr != nil || actualOffset != offset || hex.EncodeToString(hash[:]) != response.Header.Get("X-Grasp-Snapshot-SHA256") || (len(data) == 0 && !last) {
		return nil, false, errors.New("workspace artifact chunk receipt mismatch")
	}
	return data, last, nil
}

func (c WorkspaceJobClient) WriteWorkspaceChunk(ctx context.Context, id string, fence WorkspaceJobRenew, part string, offset int64, data []byte) error {
	if (part != "archive" && part != "manifest") || offset < 0 || len(data) < 1 || len(data) > WorkspaceFileChunkBytes {
		return errors.New("invalid workspace artifact chunk")
	}
	hash := sha256.Sum256(data)
	path := "/api/workspace/jobs/" + url.PathEscape(id) + "/artifacts/" + part + "?offset=" + strconv.FormatInt(offset, 10)
	response, err := c.fileCall(ctx, http.MethodPut, path, &fence, data, hex.EncodeToString(hash[:]))
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	var ack struct {
		Offset int64 `json:"offset"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&ack); err != nil || ack.Offset != offset+int64(len(data)) {
		return errors.New("workspace artifact write receipt mismatch")
	}
	return nil
}

func (c WorkspaceJobClient) FinishWorkspaceTransfer(ctx context.Context, id string, fence WorkspaceJobRenew) error {
	response, err := c.fileCall(ctx, http.MethodPost, "/api/workspace/jobs/"+url.PathEscape(id)+"/finish", &fence, nil, "")
	if err != nil {
		return err
	}
	return response.Body.Close()
}
