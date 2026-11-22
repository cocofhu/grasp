package codex

import (
	"os"
	"strings"

	"backend/internal/provider/modelconnection"
)

// codexLoginRepaste is the user-facing text when ChatGPT login is rejected or
// cannot be refreshed. Keep it identical to the server-side message.
const codexLoginRepaste = "Codex 登录已失效或无法刷新，请在工作区凭据中用设备码重新登录 ChatGPT，或请重新粘贴登录文件（~/.codex/auth.json）"

func rewriteCodexAuthError(message string) string {
	if codexAuthRejected(message) {
		if strings.TrimSpace(os.Getenv(modelconnection.EnvConfig)) != "" {
			return "Codex 模型连接认证失败，请检查所选模型 API Key"
		}
		return codexLoginRepaste
	}
	return message
}

func codexAuthRejected(text string) bool {
	s := strings.ToLower(text)
	if s == "" {
		return false
	}
	if strings.Contains(text, codexLoginRepaste) || strings.Contains(s, "请重新粘贴登录文件") {
		return true
	}
	for _, p := range []string{
		"not logged in", "please log in", "please login",
		"failed to refresh", "could not refresh", "couldn't refresh",
		"unable to refresh", "invalid_grant", "re-authenticate", "reauthenticate",
		"authentication required", "authentication failed", "login required",
		"missing bearer",
		// The platform renews the login; a sandbox copy has no refresh token,
		// so Codex reports rotation or a missing token instead of renewing.
		"refresh_token_reused", "refresh_token_expired", "refresh_token_invalidated",
		"could not be refreshed", "sign in again", "log in again",
		"token data is not available",
		"chatgpt auth is missing", "no more recovery steps available",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	// codex-cli reports a missing or rejected ChatGPT login as "401
	// Unauthorized" against api.openai.com or chatgpt.com/backend-api.
	if strings.Contains(s, "401 unauthorized") &&
		(strings.Contains(s, "api.openai.com") || strings.Contains(s, "chatgpt.com/backend-api")) {
		return true
	}
	return false
}
