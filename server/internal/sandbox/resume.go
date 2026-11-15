package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/cocofhu/grasp/internal/execution"
)

// ResumePrepared rebuilds only application inputs for a released workload.
// The gateway keeps its original workspace, mounts, image and resource limits;
// a snapshot-backed warm resume ignores this patch entirely.
func (m *Manager) ResumePrepared(ctx context.Context, id string, spec Spec) (*Sandbox, error) {
	if execution.Worktree() {
		return nil, fmt.Errorf("local worktree environments have no gateway resume")
	}
	if m.gw == nil {
		return nil, fmt.Errorf("no gateway client")
	}
	if strings.TrimSpace(spec.ConfigHome) == "" {
		return nil, fmt.Errorf("cold resume requires a prepared agent configuration")
	}
	current, err := m.gw.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.EnvKeys == nil {
		return nil, fmt.Errorf("gateway does not expose saved environment keys; update the gateway before cold recovery")
	}
	var data []byte
	var bootstrap string
	if m.runtime != nil {
		data, _, err = m.runtime.Current()
		if err != nil {
			return nil, err
		}
		bootstrap, err = runtimeBootstrapScript(data)
		if err != nil {
			return nil, err
		}
	}
	plan, err := m.prepareMultiInject(spec.ConfigHome, spec.ConfigRoot, spec.GitCredentials, data)
	if err != nil {
		return nil, err
	}
	// These URLs are live only for this start. Never persist them as the source
	// of the next restore: each restore rebuilds the selected credentials.
	env := maps.Clone(spec.Env)
	if env == nil {
		env = make(map[string]string)
	}
	delete(env, "SSH_KEY")
	delete(env, "WORKSPACE_DIR")
	if _, set := env["SKIP_INNER_DOCKER"]; !set {
		env["SKIP_INNER_DOCKER"] = "1"
	}
	env["SANDBOX_INJECT"] = strings.Join(plan.parts, ",")
	env["SANDBOX_INJECT_HEADERS"] = plan.headers
	env["GRASP_RUNTIME_URL"] = plan.runtimeURL
	keys := make([]string, 0, len(current.EnvKeys))
	for _, key := range current.EnvKeys {
		if key != "SSH_KEY" && key != "WORKSPACE_DIR" {
			keys = append(keys, key)
		}
	}
	if err := m.gw.StartPrepared(ctx, id, GWStartOptions{
		BootstrapScript: bootstrap, RuntimeURL: plan.runtimeURL, RuntimeHeaders: plan.headers,
		RestoreEnv: env, ReplaceEnvKeys: keys,
	}); err != nil {
		m.deleteInjectBundles(plan.ids)
		return nil, fmt.Errorf("prepare cold sandbox resume: %w", err)
	}
	sb, err := m.WaitForRunning(ctx, id)
	if err != nil {
		return nil, err
	}
	m.deleteInjectBundles(plan.ids)
	sb.ConfigRoot = spec.ConfigRoot
	return sb, nil
}

// Resume restarts the existing sandbox. Unlike Create, a failed resume never
// destroys the workload: its writable layer may be the only copy of user files.
func (m *Manager) Resume(ctx context.Context, id string) (*Sandbox, error) {
	if execution.Worktree() {
		return nil, fmt.Errorf("local worktree environments have no gateway resume")
	}
	if m.gw == nil {
		return nil, fmt.Errorf("no gateway client")
	}
	if m.runtime != nil {
		data, _, err := m.runtime.Current()
		if err != nil {
			return nil, err
		}
		bootstrap, err := runtimeBootstrapScript(data)
		if err != nil {
			return nil, err
		}
		plan := m.buildMultiInject("", "", nil, data)
		if plan.runtimeURL == "" {
			return nil, fmt.Errorf("resuming a sandbox requires the runtime inject store and server URL")
		}
		if err := m.gw.StartPrepared(ctx, id, GWStartOptions{
			BootstrapScript: bootstrap, RuntimeURL: plan.runtimeURL, RuntimeHeaders: plan.headers,
		}); err != nil {
			m.deleteInjectBundles(plan.ids)
			return nil, fmt.Errorf("prepare sandbox resume: %w", err)
		}
	} else if err := m.gw.Start(ctx, id); err != nil {
		return nil, err
	}
	return m.WaitForRunning(ctx, id)
}

// WaitForRunning attaches a pending sandbox without starting it a second time.
func (m *Manager) WaitForRunning(ctx context.Context, id string) (*Sandbox, error) {
	if _, err := m.gw.WaitRunning(ctx, id, m.createTimeout); err != nil {
		return nil, fmt.Errorf("sandbox resume not ready (workspace retained): %w", err)
	}
	return m.AttachNoEnsure(ctx, id)
}

func runtimeBootstrapScript(data []byte) (string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err != nil {
			return "", fmt.Errorf("runtime bootstrap script missing: %w", err)
		}
		if strings.TrimPrefix(h.Name, "./") != "scripts/grasp-bootstrap.sh" {
			continue
		}
		if h.Size > 128<<10 || h.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("invalid runtime bootstrap script")
		}
		script, err := io.ReadAll(tr)
		if err != nil {
			return "", err
		}
		return string(script), nil
	}
}

// Stop saves recovery state and releases the workload for a later Resume.
// The gateway owns provider-specific persistence and restore-point identifiers.
func (m *Manager) Stop(ctx context.Context, id string, keep int) error {
	if execution.Worktree() {
		return fmt.Errorf("local worktree environments have no gateway stop")
	}
	if m.gw == nil {
		return fmt.Errorf("no gateway client")
	}
	return m.gw.Stop(ctx, id, keep)
}

// RecoveryKind resolves the driver's persistence contract. An unavailable
// gateway is an error, never permission to destroy a possibly stateful guest.
func (m *Manager) RecoveryKind(ctx context.Context, id string) (string, error) {
	if execution.Worktree() {
		return "", fmt.Errorf("local worktree environments have no gateway recovery")
	}
	if m.gw == nil {
		return "", fmt.Errorf("no gateway client")
	}
	driver, err := m.gw.Driver(ctx, id)
	if err != nil {
		return "", err
	}
	switch driver {
	case "e2b":
		return "snapshot", nil
	case "docker", "kubernetes":
		return "files", nil
	default:
		return "", fmt.Errorf("unsupported sandbox recovery driver %q", driver)
	}
}
