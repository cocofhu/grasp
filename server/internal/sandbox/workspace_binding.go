package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type WorkspaceBindingReference struct {
	ID          string `json:"id"`
	Revision    uint64 `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}

func ValidWorkspaceBindingReference(ref WorkspaceBindingReference) bool {
	if !runtimeWorkspaceID.MatchString(ref.ID) || ref.Revision == 0 || len(ref.Fingerprint) != 64 || strings.ToLower(ref.Fingerprint) != ref.Fingerprint {
		return false
	}
	_, err := hex.DecodeString(ref.Fingerprint)
	return err == nil
}

type WorkspaceBindingSelector struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}
type WorkspaceBindingHead struct {
	Version   int                       `json:"version"`
	Resource  string                    `json:"resource"`
	Lineage   string                    `json:"lineage"`
	Reference WorkspaceBindingReference `json:"reference"`
	Target    WorkspaceBindingSelector  `json:"target"`
	ParentID  string                    `json:"parentId,omitempty"`
}

func ValidWorkspaceBindingHead(head WorkspaceBindingHead, resource, lineage string) bool {
	if head.Version != 1 || head.Resource != resource || head.Lineage != lineage || !ValidWorkspaceBindingReference(head.Reference) || (head.Target.Kind != "original" || head.Target.ID != "") && (head.Target.Kind != "restore" || !runtimeWorkspaceID.MatchString(head.Target.ID)) {
		return false
	}
	fingerprint := head.Reference.Fingerprint
	head.Reference.Fingerprint = ""
	data, _ := json.Marshal(head)
	hash := sha256.Sum256(data)
	return fingerprint == hex.EncodeToString(hash[:])
}

type WorkspaceBindingView struct {
	Head               WorkspaceBindingHead `json:"head"`
	Path               string               `json:"path"` // authenticated internal data plane only
	PendingOperationID string               `json:"pendingOperationId,omitempty"`
	Ready              bool                 `json:"ready"`
}
type WorkspaceBindingCapabilities struct {
	Version       int      `json:"version"`
	Resource      string   `json:"resource"`
	Lineage       string   `json:"lineage"`
	Activate      bool     `json:"activate"`
	Return        bool     `json:"return"`
	DurableHead   bool     `json:"durableHead"`
	Consumers     []string `json:"consumers"`
	ProcessMemory bool     `json:"processMemory"`
	PortOwnership bool     `json:"portOwnership"`
}

type WorkspaceBindingPort struct {
	Version   int                       `json:"version"`
	Reference WorkspaceBindingReference `json:"reference"`
	Port      int                       `json:"port"`
	Verified  bool                      `json:"verified"`
}
type WorkspaceBindingSpec struct {
	Version          int                       `json:"version"`
	Expected         WorkspaceBindingReference `json:"expected"`
	BindingID        string                    `json:"bindingId"`
	Target           WorkspaceBindingSelector  `json:"target"`
	ReturnSnapshotID string                    `json:"returnSnapshotId"`
	Authorization    string                    `json:"authorization"`
}
type WorkspaceBindingResult struct {
	Version          int                  `json:"version"`
	OperationID      string               `json:"operationId"`
	Previous         WorkspaceBindingHead `json:"previous"`
	Active           WorkspaceBindingHead `json:"active"`
	ReturnSnapshotID string               `json:"returnSnapshotId"`
	Consumers        []string             `json:"consumers"`
	ProcessMemory    bool                 `json:"processMemory"`
}
type WorkspaceBindingOperation struct {
	Version  int `json:"version"`
	Identity struct {
		ID         string    `json:"id"`
		Kind       string    `json:"kind"`
		Resource   string    `json:"resource"`
		Lineage    string    `json:"lineage"`
		SHA256     string    `json:"sha256"`
		Generation uint64    `json:"generation"`
		Deadline   time.Time `json:"deadline"`
	} `json:"identity"`
	State       string                  `json:"state"`
	Result      *WorkspaceBindingResult `json:"result,omitempty"`
	CreatedAt   time.Time               `json:"createdAt"`
	CompletedAt *time.Time              `json:"completedAt,omitempty"`
}

func (c WorkspaceJobClient) bindingCall(ctx context.Context, path string, value any) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	response, err := chatRequestTimeout(ctx, http.MethodGet, c.Host, c.Port, c.Password, "/api/workspace/binding"+path, nil, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&failure)
		if failure.Code == "" {
			failure.Code = "workspace_binding_unavailable"
		}
		return &WorkspaceJobError{Status: response.StatusCode, Code: failure.Code}
	}
	d := json.NewDecoder(io.LimitReader(response.Body, 128<<10))
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("workspace binding receipt: %w", err)
	}
	return nil
}
func (c WorkspaceJobClient) WorkspaceBindingCapabilities(ctx context.Context) (WorkspaceBindingCapabilities, error) {
	var out WorkspaceBindingCapabilities
	err := c.bindingCall(ctx, "/capabilities", &out)
	return out, err
}
func (c WorkspaceJobClient) WorkspaceBindingHead(ctx context.Context) (WorkspaceBindingView, error) {
	var out WorkspaceBindingView
	err := c.bindingCall(ctx, "/head", &out)
	return out, err
}
func (c WorkspaceJobClient) WorkspaceBindingOperation(ctx context.Context, id string) (WorkspaceBindingOperation, error) {
	var out WorkspaceBindingOperation
	err := c.bindingCall(ctx, "/operations/"+url.PathEscape(id), &out)
	return out, err
}

func (c WorkspaceJobClient) WorkspaceBindingPort(ctx context.Context, port int) (WorkspaceBindingPort, error) {
	var out WorkspaceBindingPort
	err := c.bindingCall(ctx, "/ports/"+strconv.Itoa(port), &out)
	return out, err
}
