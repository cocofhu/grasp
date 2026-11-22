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
	ServiceTiers     []string `json:"serviceTiers,omitempty"`
}

// ChatModelSettings contains one chat's explicit model and the configured options.
// Nil settings identify an older runtime that does not expose that capability.
type ChatModelSettings struct {
	Models           []ChatModelOption `json:"models"`
	Configured       bool              `json:"configured"`
	Default          string            `json:"default"`
	Selected         string            `json:"selected"`
	ReasoningEffort  *string           `json:"reasoningEffort"`
	ReasoningEfforts []string          `json:"reasoningEfforts"`
	ServiceTier      *string           `json:"serviceTier"`
	ServiceTiers     []string          `json:"serviceTiers"`
}

// ChatHTTPError is a non-OK status from the bridge or the proxy in front of
// it. A paused or vanished guest typically surfaces as 502/503/504.
type ChatHTTPError struct {
	StatusCode int
	op         string
}

func (e *ChatHTTPError) Error() string { return fmt.Sprintf("%s: status %d", e.op, e.StatusCode) }

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
		return ChatModelSettings{}, &ChatHTTPError{StatusCode: resp.StatusCode, op: "get chat settings"}
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
	if settings.ServiceTier != nil {
		value := strings.TrimSpace(*settings.ServiceTier)
		settings.ServiceTier = &value
	}
	return settings, nil
}

// SetIndependentModelSettings changes only the non-default chat this client owns.
// A nil tier keeps the chat's current speed tier. It reports whether the chat
// had to restart its coding agent.
func (c *ACPClient) SetIndependentModelSettings(ctx context.Context, model, effort string, tier *string) (bool, error) {
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
	tierMatches := true
	if tier != nil {
		want := strings.TrimSpace(*tier)
		if settings.ServiceTier == nil && want != "" {
			return false, fmt.Errorf("runtime does not support service tiers")
		}
		tierMatches = settings.ServiceTier == nil || *settings.ServiceTier == want
		tier = &want
	}
	if settings.Selected == model && settings.ReasoningEffort != nil && *settings.ReasoningEffort == effort && tierMatches {
		return false, nil
	}
	_, err = SetChatModelSettingsFor(ctx, c.host, c.port, c.password, c.chatID, model, effort, tier)
	return true, err
}

// SetChatModel selects the default chat's model and restarts that chat's
// coding agent. Empty restores the primary model. The caller must not invoke
// this while a turn is still generating. resumed reports that the restarted
// agent continues its native conversation.
func SetChatModel(ctx context.Context, host string, port int, password, model string) (resumed bool, err error) {
	return setChatSettings(ctx, host, port, password, "/api/model?chat="+chatQuery("default"), map[string]any{"model": strings.TrimSpace(model)})
}

// SetChatModelSettings applies the settings through one runtime restart.
// A nil tier preserves the chat's current speed tier.
func SetChatModelSettings(ctx context.Context, host string, port int, password, model, effort string, tier *string) (resumed bool, err error) {
	return SetChatModelSettingsFor(ctx, host, port, password, "default", model, effort, tier)
}

// SetChatModelSettingsFor applies the settings to one bridge chat. Empty chat
// is the default chat. Empty model restores that chat's primary model.
func SetChatModelSettingsFor(ctx context.Context, host string, port int, password, chat, model, effort string, tier *string) (resumed bool, err error) {
	settings := map[string]any{"model": strings.TrimSpace(model), "reasoningEffort": strings.TrimSpace(effort)}
	if tier != nil {
		settings["serviceTier"] = strings.TrimSpace(*tier)
	}
	return setChatSettings(ctx, host, port, password, "/api/model?chat="+chatQuery(chat), settings)
}

// setChatSettings posts the settings. A runtime that predates native resume
// omits "resumed", which reads as a fresh conversation; an unreadable answer
// is an error, since the agent may have resumed either way.
func setChatSettings(ctx context.Context, host string, port int, password, path string, settings map[string]any) (bool, error) {
	body, err := json.Marshal(settings)
	if err != nil {
		return false, err
	}
	resp, err := chatRequestTimeout(ctx, http.MethodPost, host, port, password, path, body, chatModelTimeout)
	if err != nil {
		return false, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	var info struct {
		Error   string `json:"error"`
		Resumed bool   `json:"resumed"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info)
	if resp.StatusCode == http.StatusOK {
		if decodeErr != nil {
			return false, fmt.Errorf("set chat model: unreadable response: %w", decodeErr)
		}
		return info.Resumed, nil
	}
	if strings.TrimSpace(info.Error) == "" || resp.StatusCode >= http.StatusBadGateway {
		return false, &ChatHTTPError{StatusCode: resp.StatusCode, op: "set chat model"}
	}
	return false, fmt.Errorf("set chat model: %s", strings.TrimSpace(info.Error))
}
