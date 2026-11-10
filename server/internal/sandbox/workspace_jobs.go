package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const WorkspaceJobVersion = 1

type WorkspaceJobSpec struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Resource string          `json:"resource"`
	Lineage  string          `json:"lineage"`
	Nonce    string          `json:"nonce"`
	Deadline time.Time       `json:"deadline"`
	Body     json.RawMessage `json:"body"`
}
type WorkspaceJobFence struct {
	Owner      string    `json:"owner"`
	Generation uint64    `json:"generation"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type WorkspaceJobCreate struct {
	Spec   WorkspaceJobSpec  `json:"spec"`
	SHA256 string            `json:"sha256"`
	Fence  WorkspaceJobFence `json:"fence"`
}
type WorkspaceJobRenew struct {
	SHA256  string            `json:"sha256"`
	Lineage string            `json:"lineage"`
	Fence   WorkspaceJobFence `json:"fence"`
}
type WorkspaceJobReceipt struct {
	Version    int             `json:"version"`
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Resource   string          `json:"resource"`
	Lineage    string          `json:"lineage"`
	SHA256     string          `json:"sha256"`
	Generation uint64          `json:"generation"`
	ExpiresAt  time.Time       `json:"expiresAt"`
	Deadline   time.Time       `json:"deadline"`
	State      string          `json:"state"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}
type WorkspaceJobCapabilities struct {
	Version              int      `json:"version"`
	Resource             string   `json:"resource"`
	Lineage              string   `json:"lineage"`
	RenewableLease       bool     `json:"renewableLease"`
	PersistentGeneration bool     `json:"persistentGeneration"`
	Kinds                []string `json:"kinds"`
	MaxLeaseMillis       int64    `json:"maxLeaseMillis"`
	MaxDurationMillis    int64    `json:"maxDurationMillis"`
	FileCapture          bool     `json:"fileCapture"`
	FileRestore          bool     `json:"fileRestore"`
}
type WorkspaceJobError struct {
	Status int
	Code   string
}

func (e *WorkspaceJobError) Error() string {
	return fmt.Sprintf("workspace job: %s (status %d)", e.Code, e.Status)
}

// WorkspaceJobClient uses bounded control requests; job duration/heartbeats are
// independent of one HTTP request. Its address/secret come only from a resolved
// sandbox connection, never browser host/path input.
type WorkspaceJobClient struct {
	Host     string
	Port     int
	Password string
	Timeout  time.Duration
}

func (c WorkspaceJobClient) call(ctx context.Context, method, path string, body, value any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	resp, err := chatRequestTimeout(ctx, method, c.Host, c.Port, c.Password, "/api/workspace/jobs"+path, data, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&failure)
		if failure.Code == "" {
			failure.Code = "workspace_job_unavailable"
		}
		return &WorkspaceJobError{Status: resp.StatusCode, Code: failure.Code}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(value)
}
func (c WorkspaceJobClient) Capabilities(ctx context.Context) (WorkspaceJobCapabilities, error) {
	var value WorkspaceJobCapabilities
	err := c.call(ctx, http.MethodGet, "/capabilities", nil, &value)
	return value, err
}
func (c WorkspaceJobClient) Create(ctx context.Context, in WorkspaceJobCreate) (WorkspaceJobReceipt, error) {
	var value WorkspaceJobReceipt
	err := c.call(ctx, http.MethodPost, "", in, &value)
	return value, err
}
func (c WorkspaceJobClient) Get(ctx context.Context, id string) (WorkspaceJobReceipt, error) {
	var value WorkspaceJobReceipt
	err := c.call(ctx, http.MethodGet, "/"+url.PathEscape(id), nil, &value)
	return value, err
}
func (c WorkspaceJobClient) Renew(ctx context.Context, id string, in WorkspaceJobRenew) (WorkspaceJobReceipt, error) {
	var value WorkspaceJobReceipt
	err := c.call(ctx, http.MethodPatch, "/"+url.PathEscape(id)+"/fence", in, &value)
	return value, err
}
func (c WorkspaceJobClient) Cancel(ctx context.Context, id string, in WorkspaceJobRenew) (WorkspaceJobReceipt, error) {
	var value WorkspaceJobReceipt
	err := c.call(ctx, http.MethodPost, "/"+url.PathEscape(id)+"/cancel", in, &value)
	return value, err
}
