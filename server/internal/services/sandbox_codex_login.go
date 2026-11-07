package services

import (
	"context"
	"fmt"

	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// SetCodexLogins wires the hub that keeps interactive sandboxes on the
// platform's current Codex login.
func (s *SandboxService) SetCodexLogins(hub *runtime.CodexLoginHub) {
	s.codexLogins = hub
}

func liveCodexKey(id uint) string { return fmt.Sprintf("sandbox:%d", id) }

func codexLiveAuthPath(backend runtime.AcpBackend, configRoot string) string {
	if runtime.NormalizeBackend(string(backend)) != runtime.BackendCodex {
		return ""
	}
	return runtime.CodexAuthPath(configRoot)
}

// installCodexLogin writes the token-free Codex login copy into home.
func (s *SandboxService) installCodexLogin(backend runtime.AcpBackend, home, body string, env map[string]string) (credentialID, version string, err error) {
	return runtime.InstallCodexLoginCopy(context.Background(), s.codexLogins, backend, home, body, env)
}

// registerLiveCodex records the sandbox for login pushes, replacing any earlier
// registration; a sandbox without a platform-managed login is unregistered.
// version is the installed copy, or "" when unknown.
func (s *SandboxService) registerLiveCodex(id uint, sb *sandbox.Sandbox, credentialID, path, version string) {
	key := liveCodexKey(id)
	if credentialID == "" || path == "" || sb == nil {
		s.codexLogins.Unregister(key)
		return
	}
	s.codexLogins.Register(credentialID, key, sb, path, version)
}

// codexLiveTurn runs one interactive turn on the current login: the sandbox is
// aligned first, and a turn refused for its login is retried once after the
// platform renews it.
func (s *SandboxService) codexLiveTurn(ctx context.Context, id uint, send func() (*sandbox.ChatResult, error)) (*sandbox.ChatResult, error) {
	key := liveCodexKey(id)
	hub := s.codexLogins
	if !hub.Registered(key) {
		return send()
	}
	if err := hub.BeforeTurn(ctx, key); err != nil {
		return nil, fmt.Errorf("align Codex login before turn: %w", err)
	}
	seenVersion, seenAccess := hub.Snapshot(key)
	result, err := send()
	if ctx.Err() != nil || !liveCodexRefused(result, err) {
		return result, err
	}
	if !hub.Recover(ctx, key, seenVersion, seenAccess) {
		return result, err
	}
	retried, retryErr := send()
	return sandbox.MergeAttemptUsage(result, retried), retryErr
}

func liveCodexRefused(result *sandbox.ChatResult, err error) bool {
	errorText, failed := "", false
	if result != nil {
		errorText, failed = result.ErrorText, result.Failed
	}
	return runtime.CodexTurnAuthRejected(err, errorText, failed)
}
