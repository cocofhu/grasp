package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// artifactUploadPath is where the helper CLI is seeded inside each sandbox.
const artifactUploadPath = "/usr/local/bin/artifact-upload"

// mcpSpaProxyPath is an optional local reverse-proxy helper retained for
// deployments that map a front-door host to the API ingress. The public map is
// empty (no-op).
const mcpSpaProxyPath = "/usr/local/bin/grasp-mcp-spa-proxy"

const mcpAdvertiseProfilePath = "/etc/profile.d/grasp-mcp-advertise.sh"

//go:embed seedhelpers/artifact-upload
var artifactUploadScript string

//go:embed seedhelpers/grasp-mcp-advertise.sh
var mcpAdvertiseProfileScript string

//go:embed seedhelpers/grasp-mcp-spa-proxy.py
var mcpSpaProxyScript string

// EnsureHelpers re-seeds sandbox helper CLIs (artifact-upload, mcp advertise
// profile.d, optional mcp host proxy). Safe to call on Attach/reconnect so
// sandboxes created by an older control plane (or whose seed failed) heal after
// upgrade without requiring container recreate. No-op when InstallHelpers is off
// (unit tests). Best-effort; never fatal.
func (m *Manager) EnsureHelpers(ctx context.Context, sb *Sandbox) {
	if m == nil || !m.installHelpers {
		return
	}
	m.seedHelpers(ctx, sb)
}

// seedHelpers installs approving's sandbox-side helper CLIs over SSH. The
// universal-sandbox image ships no artifact-upload command, so approving writes
// it in itself (rather than extending the image): test/UI nodes tell the agent
// to run `artifact-upload <file>` to push screenshots into the run's artifact
// store. Best-effort — a failure only means screenshot upload is unavailable,
// it never blocks sandbox creation.
func (m *Manager) seedHelpers(ctx context.Context, sb *Sandbox) {
	if sb == nil {
		return
	}
	creds := sb.creds()
	// Bound the wait: sshd usually accepts shortly after the gateway reports
	// running. If it is not ready in time we skip (degraded, not fatal).
	rctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cli, err := creds.dialReady(rctx, 25*time.Second)
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Msg("seed helpers: ssh not ready; artifact-upload unavailable")
		return
	}
	if cli != nil {
		defer func() { _ = cli.Close() }()
	}
	// File data and permissions travel together. Reuse the authenticated
	// readiness connection for extraction and the optional proxy, rather than
	// authenticating again for every write and chmod.
	payload, err := packSeedHelpers()
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Msg("seed helpers: pack failed")
		return
	}
	tarCmd, err := newSafeCmd("tar", "-C", "/", "-xf", "-")
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Msg("seed helpers: invalid extract command")
		return
	}
	if out, err := creds.runInputOnClient(ctx, 20*time.Second, cli, tarCmd, bytes.NewReader(payload)); err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("out", strings.TrimSpace(string(out))).
			Msg("seed helpers: install failed; will retry on reconnect")
		return
	}
	ensureCmd, err := newSafeCmd(mcpSpaProxyPath, "--ensure")
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Msg("seed helpers: invalid mcp host proxy path")
		return
	}
	if out, err := creds.runInputOnClient(ctx, 15*time.Second, cli, ensureCmd, nil); err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("out", strings.TrimSpace(string(out))).
			Msg("seed helpers: start mcp host proxy failed")
	}
	log.Debug().Str("id", sb.ID).Msg("seeded artifact-upload CLI")
}

func packSeedHelpers() ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, file := range []struct {
		path, content string
		mode          int64
	}{
		{artifactUploadPath, artifactUploadScript, 0o755},
		{mcpAdvertiseProfilePath, mcpAdvertiseProfileScript, 0o644},
		{mcpSpaProxyPath, mcpSpaProxyScript, 0o755},
	} {
		if err := tw.WriteHeader(&tar.Header{
			Name: strings.TrimPrefix(file.path, "/"), Mode: file.mode,
			Size: int64(len(file.content)), Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, fmt.Errorf("helper header %s: %w", file.path, err)
		}
		if _, err := tw.Write([]byte(file.content)); err != nil {
			return nil, fmt.Errorf("helper content %s: %w", file.path, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
