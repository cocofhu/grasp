package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// chatModelTimeout bounds POST /api/model. The bridge restarts the coding
// agent before it answers, which is slower than a plain chat-list call.
const chatModelTimeout = 2 * time.Minute

type ChatModelOption struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Vendor           string   `json:"vendor,omitempty"`
	Unavailable      bool     `json:"unavailable,omitempty"`
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
}

// ChatModelSettings contains one chat's explicit model and the configured options.
// A nil effort is an older runtime that does not expose this capability.
type ChatModelSettings struct {
	Models           []ChatModelOption `json:"models"`
	Configured       bool              `json:"configured"`
	Default          string            `json:"default"`
	Selected         string            `json:"selected"`
	ReasoningEffort  *string           `json:"reasoningEffort"`
	ReasoningEfforts []string          `json:"reasoningEfforts"`
}

func chatQuery(chat string) string {
	chat = strings.TrimSpace(chat)
	if chat == "" {
		chat = "default"
	}
	return url.QueryEscape(chat)
}

func GetChatModelSettings(ctx context.Context, host string, port int, password string) (ChatModelSettings, error) {
	return GetChatModelSettingsFor(ctx, host, port, password, "default")
}

// GetChatModelSettingsFor reads one bridge chat. Empty chat is the default chat.
func GetChatModelSettingsFor(ctx context.Context, host string, port int, password, chat string) (ChatModelSettings, error) {
	resp, err := chatRequest(ctx, http.MethodGet, host, port, password, "/api/models?chat="+chatQuery(chat), nil)
	if err != nil {
		return ChatModelSettings{}, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ChatModelSettings{}, fmt.Errorf("get chat settings: status %d", resp.StatusCode)
	}
	var settings ChatModelSettings
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&settings); err != nil {
		return ChatModelSettings{}, fmt.Errorf("get chat settings: %w", err)
	}
	settings.Selected = strings.TrimSpace(settings.Selected)
	if settings.ReasoningEffort != nil {
		value := strings.TrimSpace(*settings.ReasoningEffort)
		settings.ReasoningEffort = &value
	}
	return settings, nil
}

// SetIndependentModelSettings changes only the non-default chat this client owns.
func (c *ACPClient) SetIndependentModelSettings(ctx context.Context, model, effort string) (bool, error) {
	if c.chatID == "" || c.chatID == "default" {
		return false, fmt.Errorf("independent chat required")
	}
	settings, err := GetChatModelSettingsFor(ctx, c.host, c.port, c.password, c.chatID)
	if err != nil {
		return false, err
	}
	if model != "" {
		selectable := false
		for _, option := range settings.Models {
			selectable = selectable || option.ID == model && !option.Unavailable
		}
		if !settings.Configured || !selectable {
			return false, fmt.Errorf("model is outside the active preview configuration")
		}
	}
	if settings.Selected == model && settings.ReasoningEffort != nil && *settings.ReasoningEffort == effort {
		return false, nil
	}
	return true, SetChatModelSettingsFor(ctx, c.host, c.port, c.password, c.chatID, model, effort)
}

// GetChatModel reads the default chat's explicit model. Empty means the
// sandbox is following its primary model.
func GetChatModel(ctx context.Context, host string, port int, password string) (string, error) {
	resp, err := chatRequest(ctx, http.MethodGet, host, port, password, "/api/models?chat=default", nil)
	if err != nil {
		return "", err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get chat model: status %d", resp.StatusCode)
	}
	var info struct {
		Selected string `json:"selected"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return "", fmt.Errorf("get chat model: %w", err)
	}
	return strings.TrimSpace(info.Selected), nil
}

// SetChatModel selects the default chat's model and restarts that chat's
// coding agent. Empty restores the primary model. The caller must not invoke
// this while a turn is still generating.
func SetChatModel(ctx context.Context, host string, port int, password, model string) error {
	return setChatSettings(ctx, host, port, password, "/api/model?chat="+chatQuery("default"), map[string]string{"model": strings.TrimSpace(model)})
}

// SetChatModelSettings applies both settings through one runtime restart.
func SetChatModelSettings(ctx context.Context, host string, port int, password, model, effort string) error {
	return SetChatModelSettingsFor(ctx, host, port, password, "default", model, effort)
}

// SetChatModelSettingsFor applies both settings to one bridge chat. Empty chat
// is the default chat. Empty model restores that chat's primary model.
func SetChatModelSettingsFor(ctx context.Context, host string, port int, password, chat, model, effort string) error {
	return setChatSettings(ctx, host, port, password, "/api/model?chat="+chatQuery(chat), map[string]string{"model": strings.TrimSpace(model), "reasoningEffort": strings.TrimSpace(effort)})
}

func setChatSettings(ctx context.Context, host string, port int, password, path string, settings map[string]string) error {
	body, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	resp, err := chatRequestTimeout(ctx, http.MethodPost, host, port, password, path, body, chatModelTimeout)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var info struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info)
	if strings.TrimSpace(info.Error) == "" {
		return fmt.Errorf("set chat model: status %d", resp.StatusCode)
	}
	return fmt.Errorf("set chat model: %s", strings.TrimSpace(info.Error))
}
