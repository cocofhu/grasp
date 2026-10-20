package services

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
)

// SecretMask is the placeholder returned for secret values on read and accepted
// on write to mean "keep the previously stored plaintext".
const SecretMask = "****"

var (
	// ErrSecretPlaceholderOnNewKey is returned when a new/renamed key is saved with only the mask.
	ErrSecretPlaceholderOnNewKey = errors.New("新密钥或重命名的键不能使用打码占位值，请重新填写明文")
	// ErrSecretPlaceholderOnPublicValue prevents a masked secret from becoming
	// readable merely because its Secret flag was cleared.
	ErrSecretPlaceholderOnPublicValue = errors.New("取消 Secret 后不能保留打码值，请重新填写公开值")
	// ErrUnknownModelDisplayNameTooLong is returned when the alias exceeds 64 runes.
	ErrUnknownModelDisplayNameTooLong = errors.New("显示名最多 64 个字符，请缩短后再保存。")
)

// UnknownModelDisplayNameMaxLen is the max rune length for UnknownModelDisplayName.
const UnknownModelDisplayNameMaxLen = 64

// NormalizeUnknownModelDisplayName trims input; empty / whitespace-only / equal to
// the unknown bucket key or its default label become "" (unset). Over-length rejects.
func NormalizeUnknownModelDisplayName(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" || v == models.TokenUsageModelUnknown || v == models.TokenUsageModelUnknownDisplay {
		return "", nil
	}
	if utf8.RuneCountInString(v) > UnknownModelDisplayNameMaxLen {
		return "", ErrUnknownModelDisplayNameTooLong
	}
	return v, nil
}

// ResolveUnknownModelDisplayName returns the configured alias or the default display label.
func ResolveUnknownModelDisplayName(alias string) string {
	v := strings.TrimSpace(alias)
	if v == "" || v == models.TokenUsageModelUnknown || v == models.TokenUsageModelUnknownDisplay {
		return models.TokenUsageModelUnknownDisplay
	}
	return v
}

// IsConfiguredUnknownAlias reports whether alias is a real workspace override (not empty/default labels).
func IsConfiguredUnknownAlias(alias string) bool {
	v := strings.TrimSpace(alias)
	return v != "" && v != models.TokenUsageModelUnknown && v != models.TokenUsageModelUnknownDisplay
}

func sanitizeWorkspaceVars(in []models.WorkspaceVariable) ([]models.WorkspaceVariable, error) {
	out := make([]models.WorkspaceVariable, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		n := strings.TrimSpace(v.Name)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		v.Name = n
		if v.Type == "" {
			v.Type = "string"
		}
		if v.Value == SecretMask {
			return nil, ErrSecretPlaceholderOnNewKey
		}
		out = append(out, v)
	}
	return out, nil
}

func mergeWorkspaceVars(existing, incoming []models.WorkspaceVariable) ([]models.WorkspaceVariable, error) {
	byName := make(map[string]models.WorkspaceVariable, len(existing))
	for _, v := range existing {
		byName[v.Name] = v
	}
	out := make([]models.WorkspaceVariable, 0, len(incoming))
	seen := map[string]struct{}{}
	for _, v := range incoming {
		n := strings.TrimSpace(v.Name)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		v.Name = n
		if v.Type == "" {
			v.Type = "string"
		}
		if isSecretVarPlaceholder(v.Value) {
			if old, ok := byName[n]; ok {
				if old.Secret && !v.Secret {
					if v.Value == SecretMask {
						return nil, ErrSecretPlaceholderOnPublicValue
					}
					// Empty/nil is the editor's explicit clear when changing a
					// secret to a public value; never reveal the stored secret.
				} else {
					v.Value = old.Value
				}
			} else if v.Value == SecretMask {
				return nil, ErrSecretPlaceholderOnNewKey
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func isSecretPlaceholder(v string) bool {
	return v == "" || v == SecretMask
}

func isSecretVarPlaceholder(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return isSecretPlaceholder(s)
}

// MaskedSandboxEnv returns env entries with secret values replaced by SecretMask.
func MaskedSandboxEnv(env []models.EnvEntry) []models.EnvEntry {
	out := make([]models.EnvEntry, len(env))
	for i, e := range env {
		out[i] = e
		if e.Secret {
			out[i].Value = SecretMask
		}
	}
	return out
}

// MaskedWorkspaceVars returns variables with secret values replaced by SecretMask.
func MaskedWorkspaceVars(vars []models.WorkspaceVariable) []models.WorkspaceVariable {
	out := make([]models.WorkspaceVariable, len(vars))
	for i, v := range vars {
		out[i] = v
		if v.Secret {
			out[i].Value = SecretMask
		}
	}
	return out
}
