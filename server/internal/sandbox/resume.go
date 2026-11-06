package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"
)

// Resume restarts the existing sandbox. Unlike Create, a failed resume never
// destroys the workload: its writable layer may be the only copy of user files.
func (m *Manager) Resume(ctx context.Context, id string) (*Sandbox, error) {
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
		plan := m.buildMultiInject("", "", "", "", false, nil, data)
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
