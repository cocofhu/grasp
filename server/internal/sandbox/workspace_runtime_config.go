package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// WorkspaceRuntimeConfig is trusted process configuration. A tool profile,
// API caller or restored SQL row cannot select its lineage or private roots.
type WorkspaceRuntimeConfig struct {
	Lineage     string
	PrivateRoot string
	Limits      WorkspaceFileLimits
}

var runtimeWorkspaceID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
var ErrWorkspaceRuntimeUnconfirmed = errors.New("workspace runtime configuration requires its original receipt")

func copyWorkspaceRuntimeConfig(cfg *WorkspaceRuntimeConfig) *WorkspaceRuntimeConfig {
	if cfg == nil {
		return nil
	}
	copy := *cfg
	if copy.PrivateRoot == "" {
		copy.PrivateRoot = "/tmp/grasp-private-workspace"
	}
	return &copy
}
func validRuntimeLimits(l WorkspaceFileLimits) bool {
	for _, n := range []int64{l.MaxFileBytes, l.MaxBytes, l.MaxGitBytes, l.MaxArchiveBytes, l.MaxManifestBytes, l.MaxSpoolBytes} {
		if n < 1 || n > 1<<50 {
			return false
		}
	}
	return l.MaxFileBytes <= l.MaxBytes && l.MaxArchiveBytes <= l.MaxSpoolBytes && l.MaxEntries > 0 && l.MaxEntries <= 1000000 && l.MaxScanEntries >= l.MaxEntries && l.MaxScanEntries <= 10000000 && l.MaxRepositories > 0 && l.MaxRepositories <= 256
}
func defaultWorkspaceRuntimeLimits() WorkspaceFileLimits {
	return WorkspaceFileLimits{MaxFileBytes: 512 << 20, MaxBytes: 1536 << 20, MaxGitBytes: 512 << 20, MaxArchiveBytes: 3 << 30, MaxManifestBytes: 64 << 20, MaxSpoolBytes: 8 << 30, MaxEntries: 100000, MaxRepositories: 64, MaxScanEntries: 500000}
}
func (m *Manager) prepareWorkspaceRuntimeEnv(env map[string]string, resource, localRoot string) error {
	for _, key := range []string{"GRASP_SANDBOX_RESOURCE_ID", "GRASP_EXECUTION_LINEAGE", "GRASP_WORKSPACE_CONTROL_ROOT", "GRASP_WORKSPACE_RESTORE_ROOT", "GRASP_WORKSPACE_FILE_LIMITS"} {
		delete(env, key)
	}
	if m.workspaceOperations == nil {
		return nil
	}
	cfg := *m.workspaceOperations
	if cfg.Limits == (WorkspaceFileLimits{}) {
		cfg.Limits = defaultWorkspaceRuntimeLimits()
	}
	if !runtimeWorkspaceID.MatchString(cfg.Lineage) || !filepath.IsAbs(cfg.PrivateRoot) || strings.ContainsAny(cfg.PrivateRoot, "\x00\r\n") || !validRuntimeLimits(cfg.Limits) {
		return fmt.Errorf("invalid trusted workspace runtime configuration")
	}
	limits, _ := json.Marshal(cfg.Limits)
	env["GRASP_EXECUTION_LINEAGE"], env["GRASP_WORKSPACE_FILE_LIMITS"] = cfg.Lineage, string(limits)
	if resource == "" {
		return nil
	} // authenticated native standby until real ID exists
	if !runtimeWorkspaceID.MatchString(resource) {
		return fmt.Errorf("invalid workspace runtime resource identity")
	}
	root := filepath.Join(cfg.PrivateRoot, resource)
	if localRoot != "" {
		root = filepath.Join(filepath.Dir(localRoot), ".grasp-private-workspace", resource)
	}
	env["GRASP_SANDBOX_RESOURCE_ID"], env["GRASP_WORKSPACE_CONTROL_ROOT"], env["GRASP_WORKSPACE_RESTORE_ROOT"] = resource, filepath.Join(root, "control"), filepath.Join(root, "restores")
	return nil
}

type workspaceRuntimeWire struct {
	Version     int                 `json:"version"`
	Resource    string              `json:"resource"`
	Lineage     string              `json:"lineage"`
	ControlRoot string              `json:"controlRoot"`
	RestoreRoot string              `json:"restoreRoot"`
	Limits      WorkspaceFileLimits `json:"limits"`
}

// RecoverWorkspaceRuntime is ConfigureWorkspaceRuntime for an attached
// resident. Recovery attaches without the async bundle update, so a runtime
// fix could otherwise never reach a guest whose old runtime blocks its head.
// Only a newly installed backend is observed again; it is still read-only.
func (m *Manager) RecoverWorkspaceRuntime(ctx context.Context, sb *Sandbox) error {
	err := m.ConfigureWorkspaceRuntime(ctx, sb)
	if err == nil {
		return nil
	}
	restarted, refreshErr := m.refreshRuntime(ctx, sb)
	if !restarted {
		return errors.Join(err, refreshErr)
	}
	return m.ConfigureWorkspaceRuntime(ctx, sb)
}

// ConfigureWorkspaceRuntime is also called after a provider Stop→Start, before
// P1 publishes a recovered binding. A new pod initially serves authenticated
// standby; its /run env is rebuilt from this fixed platform tuple and the head
// remains in the provider's persistent /tmp volume. Legacy live runtimes are
// never silently reconfigured, and an ambiguous ACK only triggers GET checks.
func (m *Manager) ConfigureWorkspaceRuntime(ctx context.Context, sb *Sandbox) error {
	if m == nil || m.workspaceOperations == nil {
		return nil
	}
	if sb == nil {
		return ErrWorkspaceRuntimeUnconfirmed
	}
	if sb.IsLocal() {
		return nil
	} // local startup receives the final tuple directly
	env := map[string]string{}
	if err := m.prepareWorkspaceRuntimeEnv(env, sb.ID, ""); err != nil {
		return err
	}
	client := WorkspaceJobClient{Host: sb.Host, Port: sb.Port, Password: sb.Password, Timeout: 3 * time.Second}
	if client.Password == "" {
		client.Password = sb.dataPlanePassword()
	}
	check := func() (bool, error) {
		caps, err := client.WorkspaceBindingCapabilities(ctx)
		if err != nil {
			return false, err
		}
		if caps.Version != 1 || caps.Resource != sb.ID || caps.Lineage != env["GRASP_EXECUTION_LINEAGE"] || !caps.DurableHead || !caps.Activate || !caps.Return || !caps.PortOwnership || caps.ProcessMemory {
			return false, ErrWorkspaceRuntimeUnconfirmed
		}
		view, err := client.WorkspaceBindingHead(ctx)
		if err != nil {
			return false, err
		}
		files, err := client.FileCapabilities(ctx)
		if err != nil {
			return false, err
		}
		var limits WorkspaceFileLimits
		_ = json.Unmarshal([]byte(env["GRASP_WORKSPACE_FILE_LIMITS"]), &limits)
		if !ValidWorkspaceBindingHead(view.Head, caps.Resource, caps.Lineage) || files.Limits != limits {
			return false, ErrWorkspaceRuntimeUnconfirmed
		}
		if view.Ready {
			if !filepath.IsAbs(view.Path) || strings.ContainsRune(view.Path, 0) || view.PendingOperationID != "" {
				return false, ErrWorkspaceRuntimeUnconfirmed
			}
			sb.SetActiveWorkspace(view.Path, view.Head.Reference.Fingerprint)
		} else if view.PendingOperationID == "" {
			return false, ErrWorkspaceRuntimeUnconfirmed
		}
		return true, nil
	}
	ready, err := check()
	if ready {
		return nil
	}
	var response *WorkspaceJobError
	if !errors.As(err, &response) || response.Status != http.StatusLocked || response.Code != "workspace_startup_pending" {
		return errors.Join(ErrWorkspaceRuntimeUnconfirmed, err)
	}
	var limits WorkspaceFileLimits
	_ = json.Unmarshal([]byte(env["GRASP_WORKSPACE_FILE_LIMITS"]), &limits)
	wire := workspaceRuntimeWire{1, sb.ID, env["GRASP_EXECUTION_LINEAGE"], env["GRASP_WORKSPACE_CONTROL_ROOT"], env["GRASP_WORKSPACE_RESTORE_ROOT"], limits}
	data, _ := json.Marshal(wire)
	cmd, err := newSafeCmd("bash", "/opt/grasp-runtime/current/scripts/workspace-configure.sh")
	if err != nil {
		return err
	}
	_, applyErr := sb.creds().runInput(ctx, 30*time.Second, cmd, bytes.NewReader(data))
	// Both success and a lost SSH ACK require independent authenticated reads.
	// This branch never sends a second configure or restarts the backend again.
	ready, observeErr := check()
	if ready {
		return nil
	}
	return errors.Join(ErrWorkspaceRuntimeUnconfirmed, applyErr, observeErr)
}
