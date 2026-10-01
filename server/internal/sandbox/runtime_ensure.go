package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// In-sandbox entry points of the runtime contract (see
// sandbox-gateway/sandbox/scripts/grasp-bootstrap.sh and services.sh).
const (
	runtimeBootstrap = "/grasp-bootstrap.sh"
	runtimeServices  = "/opt/grasp-runtime/current/scripts/services.sh"

	// grasp-bootstrap.sh install exit code: bundle needs a newer image.
	runtimeExitImageTooOld = 4
	// services.sh restart backend --if-idle exit code: backend busy.
	runtimeExitBusy = 3
	// Shell exit code for "command not found": image predates the bootstrap.
	runtimeExitNotFound = 127

	ensureRuntimeTimeout = 3 * time.Minute
)

// exitStatus extracts a remote exit code (ssh.ExitError and test doubles).
func exitStatus(err error) (int, bool) {
	var e interface{ ExitStatus() int }
	if errors.As(err, &e) {
		return e.ExitStatus(), true
	}
	return 0, false
}

// ensureRuntimeAsync updates a running sandbox's runtime in the background so
// callers of Attach are never slowed down.
func (m *Manager) ensureRuntimeAsync(sb *Sandbox) {
	if m == nil || m.runtime == nil || sb == nil || sb.ID == "" {
		return
	}
	_, man, err := m.runtime.Current()
	if err != nil || m.runtime.upToDate(sb.ID, man.Version) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ensureRuntimeTimeout)
		defer cancel()
		if err := m.EnsureRuntime(ctx, sb); err != nil {
			log.Warn().Err(err).Str("sandbox", sb.ID).Msg("sandbox runtime update failed; will retry on next use")
		}
	}()
}

// EnsureRuntime brings a running sandbox to the server's runtime bundle: push
// the bundle over SSH when the installed version differs, restart
// preview-inject, and restart backend once it is idle. Sandboxes from images
// without the bootstrap, or images too old for the bundle, are skipped for good.
func (m *Manager) EnsureRuntime(ctx context.Context, sb *Sandbox) error {
	if m == nil || m.runtime == nil || sb == nil || sb.ID == "" {
		return nil
	}
	data, man, err := m.runtime.Current()
	if err != nil {
		return err
	}
	st, ok := m.runtime.begin(sb.ID)
	if !ok {
		return nil
	}
	err = m.ensureRuntime(ctx, sb, data, man, &st)
	m.runtime.end(sb.ID, st)
	return err
}

func (m *Manager) ensureRuntime(ctx context.Context, sb *Sandbox, data []byte, man RuntimeManifest, st *runtimeState) error {
	if st.skip != "" {
		return nil
	}
	creds := sb.creds()
	if st.version != man.Version {
		installed, err := m.installedRuntime(ctx, creds)
		if code, ok := exitStatus(err); ok && code == runtimeExitNotFound {
			st.skip = "legacy-image"
			log.Info().Str("sandbox", sb.ID).Msg("sandbox image has no runtime bootstrap; runtime updates skipped until it is recreated")
			return nil
		}
		if installed != man.Version {
			if err := m.pushRuntime(ctx, creds, sb.ID, data, man, st); err != nil || st.skip != "" {
				return err
			}
		}
		st.version = man.Version
	}
	if !st.backendPending {
		return nil
	}
	cmd, err := newSafeCmd(runtimeServices, "restart", "backend", "--if-idle")
	if err != nil {
		return err
	}
	out, err := creds.run(ctx, 90*time.Second, cmd)
	if code, ok := exitStatus(err); ok && code == runtimeExitBusy {
		log.Info().Str("sandbox", sb.ID).Msg("sandbox backend busy; runtime restart deferred")
		return nil
	}
	if err != nil {
		return fmt.Errorf("restart backend: %w: %s", err, strings.TrimSpace(string(out)))
	}
	st.backendPending = false
	log.Info().Str("sandbox", sb.ID).Str("version", shortVersion(man.Version)).Msg("sandbox backend restarted on new runtime")
	return nil
}

// installedRuntime returns the sandbox's current runtime version ("" when
// none is installed yet).
func (m *Manager) installedRuntime(ctx context.Context, creds sshCreds) (string, error) {
	cmd, err := newSafeCmd(runtimeBootstrap, "version")
	if err != nil {
		return "", err
	}
	out, err := creds.run(ctx, 20*time.Second, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (m *Manager) pushRuntime(ctx context.Context, creds sshCreds, id string, data []byte, man RuntimeManifest, st *runtimeState) error {
	cmd, err := newSafeCmd(runtimeBootstrap, "install-stdin")
	if err != nil {
		return err
	}
	out, err := creds.runInput(ctx, 120*time.Second, cmd, bytes.NewReader(data))
	if code, ok := exitStatus(err); ok && code == runtimeExitImageTooOld {
		st.skip = "image-too-old"
		log.Warn().Str("sandbox", id).Int("min_image", man.MinImage).Str("detail", strings.TrimSpace(string(out))).
			Msg("sandbox image too old for the runtime bundle; rebuild the sandbox image")
		return nil
	}
	if err != nil {
		return fmt.Errorf("install runtime: %w: %s", err, strings.TrimSpace(string(out)))
	}
	log.Info().Str("sandbox", id).Str("version", shortVersion(man.Version)).Msg("sandbox runtime installed")
	restart, err := newSafeCmd(runtimeServices, "restart", "preview-inject")
	if err != nil {
		return err
	}
	if out, err := creds.run(ctx, 60*time.Second, restart); err != nil {
		log.Warn().Err(err).Str("sandbox", id).Str("detail", strings.TrimSpace(string(out))).
			Msg("restart preview-inject after runtime update failed")
	}
	st.backendPending = true
	return nil
}

func shortVersion(v string) string {
	if len(v) > 12 {
		return v[:12]
	}
	return v
}
