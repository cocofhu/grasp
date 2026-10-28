package kimicode

import (
	"encoding/json"
	"strings"
)

const (
	// MsgMissingKey is shown when the Coding Plan key was not injected.
	// The CLI must not be started into /login in that case.
	MsgMissingKey = "需要 Kimi Code 的 Coding Plan API Key：请在工作区凭据中配置 GRASP_KIMI_CODE_API_KEY"
	// MsgSiteMismatch explains a 401 without echoing the key or changing site.
	MsgSiteMismatch = "密钥需与所选站点一致：请使用国内站 kimi.com/code/console 或国际站 kimi.ai/code/console 的 Coding Plan API Key。"
	// MsgModelUnavailable explains a missing or forbidden model id.
	MsgModelUnavailable = "模型不可用：请确认模型 ID 在所选站点存在，且当前账号可以使用。"
)

// Explain turns a CLI or API failure into one of the three user-facing
// messages. Other text is returned with the secret removed.
func Explain(text, secret string) string {
	text = redact(text, secret)
	low := strings.ToLower(text)
	switch {
	case isMissingKey(low):
		return MsgMissingKey
	case isAuthFailure(low):
		return MsgSiteMismatch
	case isModelFailure(low):
		return MsgModelUnavailable
	default:
		return text
	}
}

func isMissingKey(low string) bool {
	if strings.Contains(low, "coding plan api key") || strings.Contains(low, "开放平台 api key") {
		return true
	}
	mentionsVar := strings.Contains(low, "kimi_code_api_key") || strings.Contains(low, "api_key_env")
	return mentionsVar && (strings.Contains(low, "unset") || strings.Contains(low, "empty") || strings.Contains(low, "missing") || strings.Contains(low, "未设置"))
}

func isAuthFailure(low string) bool {
	if strings.Contains(low, "invalid_api_key") || strings.Contains(low, "authentication_error") || strings.Contains(low, "incorrect api key") {
		return true
	}
	if !strings.Contains(low, "401") {
		return false
	}
	return strings.Contains(low, "unauthorized") || strings.Contains(low, "api key") || strings.Contains(low, "authentication") || strings.Contains(low, "invalid")
}

func isModelFailure(low string) bool {
	if strings.Contains(low, "resource_not_found") || strings.Contains(low, "model_not_found") || strings.Contains(low, "model not found") || strings.Contains(low, "model does not exist") || strings.Contains(low, "no such model") || strings.Contains(low, "invalid model") {
		return true
	}
	return strings.Contains(low, "模型不存在") || strings.Contains(low, "模型不可用") || strings.Contains(low, "无权使用")
}

func redact(text, secret string) string {
	if len(secret) >= 8 {
		text = strings.ReplaceAll(text, secret, "••••")
	}
	return text
}

// RewriteEvent redacts and classifies user-visible strings inside one ACP event.
func RewriteEvent(raw json.RawMessage, secret string) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	if !rewriteValue(&value, secret) {
		return raw
	}
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func rewriteValue(value *any, secret string) bool {
	switch item := (*value).(type) {
	case string:
		next := Explain(item, secret)
		if next == item {
			return false
		}
		*value = next
		return true
	case map[string]any:
		changed := false
		for key, child := range item {
			if rewriteValue(&child, secret) {
				item[key] = child
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for i := range item {
			if rewriteValue(&item[i], secret) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}
