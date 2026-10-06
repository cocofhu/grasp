package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
)

const (
	// CodexConfigRoot is the sandbox Codex home. The login file is auth.json
	// inside that directory.
	CodexConfigRoot = "/root/.codex"
	// CodexAuthFileName is the file Codex CLI reads for a ChatGPT login.
	CodexAuthFileName = "auth.json"
	// CodexLoginRepasteMessage is the user-facing failure when Codex rejects
	// or cannot refresh the login file. It must not include the file body.
	CodexLoginRepasteMessage = "Codex 登录已失效或无法刷新，请重新粘贴登录文件（~/.codex/auth.json）"
)

var (
	// ErrCodexLoginFileEmpty rejects a blank paste so the slot stays unconfigured.
	ErrCodexLoginFileEmpty = errors.New("Codex 登录文件不能为空")
	// ErrCodexLoginFileAPIKey rejects an API key or an API-key-mode login file.
	ErrCodexLoginFileAPIKey = errors.New("Codex 使用 ChatGPT 登录文件，不接受 API Key。用 API Key 接 OpenAI 请改用 OpenCode")
	// ErrCodexLoginFileMissing is returned before the CLI starts when the project
	// has no Codex login file.
	ErrCodexLoginFileMissing = errors.New("缺少 Codex 登录文件：请在项目凭据中粘贴 ChatGPT 登录文件（本机 ~/.codex/auth.json）")
)

// ValidateCodexLoginFile accepts a ChatGPT login file and rejects blank
// content, a raw API key, and a file marked as API-key mode. Other non-empty
// content is kept for the CLI to judge.
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

// DecideCodexWriteBack reports the body to store when the sandbox file changed
// and is still a usable login file. A read failure (sandbox already gone), an
// unchanged file, or a rejected API-key/blank body keeps the previous credential.
func DecideCodexWriteBack(readErr error, body, injected string) (string, bool) {
	if readErr != nil {
		return "", false
	}
	if err := ValidateCodexLoginFile(body); err != nil {
		return "", false
	}
	if injected != "" && body == injected {
		return "", false
	}
	return body, true
}

// ShouldWriteBackCodexLogin compares the sandbox file with the injected body,
// then keeps the saved credential when the turn's login was refused. A phrase
// in narration or tool output is not a refusal; callers pass authRejected only
// after a confirmed CLI login failure.
func ShouldWriteBackCodexLogin(readErr error, body, injected string, authRejected bool) (string, bool) {
	next, ok := DecideCodexWriteBack(readErr, body, injected)
	if !ok || authRejected {
		return "", false
	}
	return next, true
}

// IsCodexAuthRejectionText reports a CLI login-failure body: the login was
// refused or could not be refreshed. It does not match incidental phrases such
// as "refresh token" or "codex login" in a successful turn.
func IsCodexAuthRejectionText(text string) bool {
	s := strings.ToLower(text)
	if s == "" {
		return false
	}
	if strings.Contains(text, CodexLoginRepasteMessage) || strings.Contains(s, "请重新粘贴登录文件") {
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
	}
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	// codex-cli 0.160.1 reports a missing or rejected ChatGPT login as
	// "401 Unauthorized" against api.openai.com, not "please log in".
	if strings.Contains(s, "401 unauthorized") && strings.Contains(s, "api.openai.com") {
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

type codexLoginState struct {
	body string
	path string
}

// codexLogins remembers the login file injected into one run attempt, keyed by
// run|node, so write-back compares against that attempt rather than a newer
// credential saved by a concurrent run.
var codexLogins sync.Map

func codexLoginKey(req NodeReq) string {
	return req.RunID + "|" + req.NodeID
}

func (c *acpProvider) rememberCodexLogin(key, body, path string) {
	if body == "" {
		return
	}
	codexLogins.Store(c.backendKey(key), codexLoginState{body: body, path: path})
}

func (c *acpProvider) takeCodexLogin(key string) (codexLoginState, bool) {
	v, ok := codexLogins.LoadAndDelete(c.backendKey(key))
	if !ok {
		return codexLoginState{}, false
	}
	st, _ := v.(codexLoginState)
	return st, st.body != ""
}

func (c *acpProvider) forgetCodexLogin(key string) {
	codexLogins.Delete(c.backendKey(key))
}

func (c *acpProvider) backendKey(key string) string {
	return string(c.backend) + "|" + key
}
