package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

const (
	// CodexConfigRoot is the sandbox Codex home. The login file is auth.json
	// inside that directory.
	CodexConfigRoot = "/root/.codex"
	// CodexAuthFileName is the file Codex CLI reads for a ChatGPT login.
	CodexAuthFileName = "auth.json"
	// CodexLoginRepasteMessage is the user-facing failure when Codex rejects
	// the login and the platform cannot renew it. It must not include the file
	// body.
	CodexLoginRepasteMessage = "Codex 登录已失效或无法刷新，请在工作区凭据中用设备码重新登录 ChatGPT，或请重新粘贴登录文件（~/.codex/auth.json）"
	// CodexLoginRepasteMark is the stable phrase that identifies the re-paste
	// message. The engine (no auto-retry), the web failure banner (credentials
	// link) and the sandbox gateway's rewritten message all key on it, so any
	// copy change must keep it.
	CodexLoginRepasteMark = "请重新粘贴登录文件"
)

var (
	// ErrCodexLoginFileEmpty rejects a blank paste so the slot stays unconfigured.
	ErrCodexLoginFileEmpty = errors.New("Codex 登录文件不能为空")
	// ErrCodexLoginFileAPIKey rejects an API key or an API-key-mode login file.
	ErrCodexLoginFileAPIKey = errors.New("Codex 平台授权使用 ChatGPT 登录文件，不接受 API Key；使用 API Key 请选择 Responses 模型连接")
	// ErrCodexLoginFileInvalid rejects text that is not a ChatGPT auth.json.
	ErrCodexLoginFileInvalid = errors.New("Codex 登录文件不是有效的 auth.json：请粘贴本机 ~/.codex/auth.json 的完整内容")
	// ErrCodexLoginFileNoRefreshToken rejects a login the platform cannot renew.
	ErrCodexLoginFileNoRefreshToken = errors.New("Codex 登录文件缺少 refresh_token，平台无法自动续期：请改用设备码登录，或粘贴本机完整的 ~/.codex/auth.json")
	// ErrCodexLoginFileMissing is returned before the CLI starts when the workspace
	// has no Codex login file.
	ErrCodexLoginFileMissing = errors.New("缺少 Codex 登录：请在工作区凭据中用设备码登录 ChatGPT，或粘贴登录文件（本机 ~/.codex/auth.json）")
	// ErrCodexLoginRejected marks a node attempt whose Codex login was refused
	// and could not be renewed by the platform. Retrying cannot help until the
	// user signs in again, so callers must not auto-retry it.
	ErrCodexLoginRejected = errors.New(CodexLoginRepasteMessage)
)

// codexLoginErr keeps the original error text while matching
// ErrCodexLoginRejected, for errors that already carry the re-paste message.
type codexLoginErr struct{ err error }

func (e *codexLoginErr) Error() string   { return e.err.Error() }
func (e *codexLoginErr) Unwrap() []error { return []error{ErrCodexLoginRejected, e.err} }

// asCodexLoginErr makes err match ErrCodexLoginRejected; nil becomes the bare
// sentinel.
func asCodexLoginErr(err error) error {
	switch {
	case err == nil:
		return ErrCodexLoginRejected
	case errors.Is(err, ErrCodexLoginRejected):
		return err
	case strings.Contains(err.Error(), CodexLoginRepasteMessage):
		return &codexLoginErr{err: err}
	default:
		return fmt.Errorf("%w: %w", ErrCodexLoginRejected, err)
	}
}

// codexTurnLoginErr returns ErrCodexLoginRejected when a finished Codex turn
// failed because the CLI login was refused, keeping the CLI error body when it
// says more than the re-paste message. Other turns return nil. Only the turn's
// failure body is checked, never narration or tool output.
func (c *acpProvider) codexTurnLoginErr(res *sandbox.ChatResult, req ...NodeReq) error {
	if len(req) > 0 {
		if _, connection := codexConnectionRuns.Load(c.backendKey(codexLoginKey(req[0]))); connection {
			return nil
		}
	}
	if c.backend != BackendCodex {
		return nil
	}
	fail := chatFailure(res)
	if fail == "" || !IsCodexAuthRejectionText(fail) {
		return nil
	}
	if fail == CodexLoginRepasteMessage {
		return ErrCodexLoginRejected
	}
	return fmt.Errorf("%w: %s", ErrCodexLoginRejected, fail)
}

// ValidateCodexLoginFile accepts a ChatGPT login file the platform can renew:
// a JSON auth.json with a refresh token. Blank content, a raw API key and an
// API-key-mode file are rejected with their own messages.
func ValidateCodexLoginFile(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ErrCodexLoginFileEmpty
	}
	if strings.HasPrefix(trimmed, "sk-") {
		return ErrCodexLoginFileAPIKey
	}
	if codexAuthModeAPIKey(trimmed) {
		return ErrCodexLoginFileAPIKey
	}
	auth, err := codexoauth.ParseAuthFile(trimmed)
	if err != nil {
		return ErrCodexLoginFileInvalid
	}
	if strings.TrimSpace(auth.RefreshToken()) == "" {
		return ErrCodexLoginFileNoRefreshToken
	}
	return nil
}

func codexAuthModeAPIKey(trimmed string) bool {
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(trimmed), &doc) != nil {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(jsonStringField(doc, "auth_mode")))
	switch mode {
	case "apikey", "api_key", "api-key":
		return true
	}
	key := strings.TrimSpace(jsonStringField(doc, "OPENAI_API_KEY"))
	if strings.HasPrefix(key, "sk-") && mode != "chatgpt" && mode != "chatgpt_auth_tokens" {
		if _, ok := doc["tokens"]; !ok {
			return true
		}
	}
	return false
}

func jsonStringField(doc map[string]json.RawMessage, key string) string {
	raw, ok := doc[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// CodexLoginFileFromEnv returns the credential body when backend is Codex.
// The caller must read it before mergeAuthEnv strips the key from the sandbox env.
func CodexLoginFileFromEnv(backend AcpBackend, env map[string]string) string {
	if NormalizeBackend(string(backend)) != BackendCodex || env == nil {
		return ""
	}
	return env[envauth.EnvCodexAuthFile]
}

// CodexAuthPath is the in-sandbox path of auth.json for a config root.
func CodexAuthPath(configRoot string) string {
	root := strings.TrimSpace(configRoot)
	if root == "" {
		root = CodexConfigRoot
	}
	return strings.TrimRight(root, "/") + "/" + CodexAuthFileName
}

// InstallCodexLoginFile writes the login file into the host config-home tree
// so the inject bundle places it at {configRoot}/auth.json. Other backends are
// a no-op. The body is not logged.
func InstallCodexLoginFile(backend AcpBackend, home, body string) error {
	if NormalizeBackend(string(backend)) != BackendCodex {
		return nil
	}
	if strings.TrimSpace(body) == "" {
		return ErrCodexLoginFileMissing
	}
	if strings.TrimSpace(home) == "" {
		return ErrCodexLoginFileMissing
	}
	path := filepath.Join(home, CodexAuthFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	return nil
}

// codexPrepareTimeout bounds a refresh run while a sandbox is being prepared.
var codexPrepareTimeout = 45 * time.Second

// InstallCodexLoginCopy writes the sandbox copy (no refresh token) of the
// bound workspace Codex login into home, renewing a login close to expiry
// first. It returns the credential and copy version to register with hub, or
// empty strings when the platform does not manage this login (another backend,
// a Responses connection, or no login).
func InstallCodexLoginCopy(ctx context.Context, hub *CodexLoginHub, backend AcpBackend, home, body string, env map[string]string) (credentialID, version string, err error) {
	if NormalizeBackend(string(backend)) != BackendCodex || env[EnvModelConnection] != "" || strings.TrimSpace(body) == "" {
		return "", "", InstallBoundCodexLoginFile(backend, home, body, env)
	}
	credentialID = CodexLoginCredentialID(env)
	ctx, cancel := context.WithTimeout(ctx, codexPrepareTimeout)
	defer cancel()
	sandboxCopy, version, err := hub.Prepare(ctx, credentialID, body)
	if err != nil {
		return "", "", err
	}
	if err := InstallCodexLoginFile(backend, home, sandboxCopy); err != nil {
		return "", "", err
	}
	return credentialID, version, nil
}

// CodexLoginCredentialForBinding is the workspace Codex login a reattached
// sandbox runs on, or "" when the binding does not use one.
func CodexLoginCredentialForBinding(backend AcpBackend, binding *modelrouting.Binding, resolve ModelBindingResolver) string {
	if NormalizeBackend(string(backend)) != BackendCodex || binding == nil {
		return ""
	}
	env := map[string]string{}
	if err := ApplyModelBinding(env, string(backend), binding, resolve); err != nil {
		return ""
	}
	if env[EnvModelConnection] != "" || strings.TrimSpace(CodexLoginFileFromEnv(backend, env)) == "" {
		return ""
	}
	return CodexLoginCredentialID(env)
}

// installCodexLogin installs the node's login copy and registers its sandbox,
// bound once it exists, for later pushes.
func (c *acpProvider) installCodexLogin(req NodeReq, backend AcpBackend, home, body string, env map[string]string, path string) error {
	credentialID, version, err := InstallCodexLoginCopy(context.Background(), c.opts.CodexLogins, backend, home, body, env)
	if err != nil || credentialID == "" {
		return err
	}
	c.opts.CodexLogins.Register(credentialID, c.codexHubKey(req), nil, path, version)
	return nil
}

// registerReattachedCodexLogin re-registers a node sandbox that survived a
// control-plane restart. Its copy version is unknown, so the first turn
// installs the current one.
func (c *acpProvider) registerReattachedCodexLogin(req NodeReq, sb *sandbox.Sandbox) {
	if c.opts.CodexLogins == nil || sb == nil {
		return
	}
	agentCfg := c.effectiveAgent(req)
	backend, binding := routeBackend(req, c.backend, agentCfg.ModelBinding)
	credentialID := CodexLoginCredentialForBinding(backend, binding, c.opts.ModelBinding)
	if credentialID == "" {
		return
	}
	path := CodexAuthPath(c.agentLayout(models.AgentProfile(req.Config), agentCfg).ConfigRoot)
	c.opts.CodexLogins.Register(credentialID, c.codexHubKey(req), sb, path, "")
}

// mergeCodexAuthEnv drops the login-file slot and any API-key env that would
// otherwise authenticate Codex. A missing file fails before the CLI starts.
func mergeCodexAuthEnv(out map[string]string, requireAuth bool) (map[string]string, error) {
	file := out[envauth.EnvCodexAuthFile]
	delete(out, envauth.EnvCodexAuthFile)
	delete(out, "OPENAI_API_KEY")
	delete(out, "CODEX_API_KEY")
	delete(out, "ACP_CODEX_API_KEY")
	if strings.TrimSpace(file) == "" && requireAuth {
		return out, ErrCodexLoginFileMissing
	}
	return out, nil
}

// IsCodexAuthRejectionText reports a CLI login-failure body: the login was
// refused or could not be refreshed. It does not match incidental phrases such
// as "refresh token" or "codex login" in a successful turn.
func IsCodexAuthRejectionText(text string) bool {
	s := strings.ToLower(text)
	if s == "" {
		return false
	}
	if strings.Contains(text, CodexLoginRepasteMessage) || strings.Contains(s, CodexLoginRepasteMark) {
		return true
	}
	phrases := []string{
		"not logged in",
		"please log in",
		"please login",
		"failed to refresh",
		"could not refresh",
		"couldn't refresh",
		"unable to refresh",
		"invalid_grant",
		"re-authenticate",
		"reauthenticate",
		"authentication required",
		"authentication failed",
		"login required",
		"missing bearer",
		// Rotation failures reported by codex-cli 0.164 when the refresh
		// token was used elsewhere, expired, revoked, or (in a platform
		// sandbox copy) is empty.
		"refresh_token_reused",
		"refresh_token_expired",
		"refresh_token_invalidated",
		"could not be refreshed",
		"sign in again",
		"log in again",
		"token data is not available",
		// Other codex-cli 0.164 auth-recovery messages. A sandbox copy (empty
		// refresh token) that hits a 401 ends with "Failed to refresh token:
		// 400 Bad Request", matched above.
		"chatgpt auth is missing",
		"no more recovery steps available",
	}
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	// codex-cli 0.160.1 reports a missing or rejected ChatGPT login as
	// "401 Unauthorized" against api.openai.com, not "please log in"; 0.164
	// with a null refresh token reports it against chatgpt.com/backend-api.
	if strings.Contains(s, "401 unauthorized") &&
		(strings.Contains(s, "api.openai.com") || strings.Contains(s, "chatgpt.com/backend-api")) {
		return true
	}
	return false
}

// codexFailureEvent is a turn that ended in failure. Narration, thoughts, and
// tool output are not login failures even when they mention a refresh token.
func codexFailureEvent(ev models.AcpEvent) bool {
	return ev.Kind == models.AcpKindTurnEnd && strings.EqualFold(strings.TrimSpace(ev.Status), "failed")
}

// CodexAuthRejected reports whether the run error or a failed turn says the
// login was refused or could not be refreshed. Tool output and ordinary
// message text are ignored.
func CodexAuthRejected(err error, events []models.AcpEvent) bool {
	if err != nil && IsCodexAuthRejectionText(err.Error()) {
		return true
	}
	for _, ev := range events {
		if !codexFailureEvent(ev) {
			continue
		}
		if IsCodexAuthRejectionText(ev.Text) || IsCodexAuthRejectionText(ev.Title) {
			return true
		}
	}
	return false
}

// CodexTurnAuthRejected is CodexAuthRejected for a live sandbox turn, using
// the CLI error body (ChatResult.ErrorText) rather than narration or tool output.
func CodexTurnAuthRejected(err error, errorText string, failed bool) bool {
	events := []models.AcpEvent(nil)
	if failed || strings.TrimSpace(errorText) != "" {
		events = []models.AcpEvent{{
			Kind:   models.AcpKindTurnEnd,
			Status: "failed",
			Text:   errorText,
		}}
	}
	return CodexAuthRejected(err, events)
}

// RewriteCodexAuthError replaces a login failure with the re-paste message.
func RewriteCodexAuthError(message string) string {
	if IsCodexAuthRejectionText(message) {
		return CodexLoginRepasteMessage
	}
	return message
}

var codexConnectionRuns sync.Map

func codexLoginKey(req NodeReq) string {
	return req.RunID + "|" + req.NodeID
}

func (c *acpProvider) backendKey(key string) string {
	return string(c.backend) + "|" + key
}

// codexHubKey is the hub registration of a node's sandbox. Visitor lanes and
// parked review turns share it with the node.
func (c *acpProvider) codexHubKey(req NodeReq) string {
	return c.backendKey(codexLoginKey(req))
}

// unregisterCodexLogin stops pushing logins to the node's sandbox.
func (c *acpProvider) unregisterCodexLogin(req NodeReq) {
	c.opts.CodexLogins.Unregister(c.codexHubKey(req))
}

// settleCodexLogin ends a node attempt's login bookkeeping and marks a refused
// login so the engine does not auto-retry the node. The sandbox file is never
// read back: the platform alone renews the login.
func (c *acpProvider) settleCodexLogin(req NodeReq, runErr error, events []models.AcpEvent) error {
	c.unregisterCodexLogin(req)
	if _, connection := codexConnectionRuns.LoadAndDelete(c.codexHubKey(req)); connection || c.backend != BackendCodex {
		return runErr
	}
	if runErr != nil && CodexAuthRejected(runErr, events) {
		return asCodexLoginErr(runErr)
	}
	return runErr
}

// codexTurn runs one chat turn of a Codex sandbox that uses the workspace
// login. The sandbox copy is brought up to date first; a turn the CLI failed
// because the login was refused gets the platform's renewed login and is sent
// once more. The refusal happens before any model call, so the retry does not
// repeat work.
func (c *acpProvider) codexTurn(ctx context.Context, req NodeReq, send func() (*sandbox.ChatResult, error)) (*sandbox.ChatResult, error) {
	hub := c.opts.CodexLogins
	key := c.codexHubKey(req)
	if !hub.Registered(key) {
		return send()
	}
	if err := hub.BeforeTurn(ctx, key); err != nil {
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).Msg("align Codex login before turn failed")
	}
	seenVersion, seenAccess := hub.Snapshot(key)
	res, err := send()
	if !codexTurnRefused(res, err) || ctx.Err() != nil {
		return res, err
	}
	if !hub.Recover(ctx, key, seenVersion, seenAccess) {
		return res, err
	}
	log.Info().Str("run", req.RunID).Str("node", req.NodeID).Msg("Codex login renewed by the platform; retrying the turn once")
	return send()
}

// codexTurnRefused reports a turn the CLI failed because the login was refused.
func codexTurnRefused(res *sandbox.ChatResult, err error) bool {
	if res == nil {
		return err != nil && IsCodexAuthRejectionText(err.Error())
	}
	return CodexTurnAuthRejected(err, chatFailure(res), res.Failed)
}
