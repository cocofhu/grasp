package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// chatModelTimeout bounds POST /api/model. The bridge restarts the coding
// agent before it answers, which is slower than a plain chat-list call.
const chatModelTimeout = 2 * time.Minute

// ChatModelSettings is the default chat's explicit, session-local selection.
// A nil effort is an older runtime that does not expose this capability.
type ChatModelSettings struct {
	Selected         string   `json:"selected"`
	ReasoningEffort  *string  `json:"reasoningEffort"`
	ReasoningEfforts []string `json:"reasoningEfforts"`
}

func GetChatModelSettings(ctx context.Context, host string, port int, password string) (ChatModelSettings, error) {
	resp, err := chatRequest(ctx, http.MethodGet, host, port, password, "/api/models?chat=default", nil)
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
	return setChatSettings(ctx, host, port, password, map[string]string{"model": strings.TrimSpace(model)})
}

// SetChatModelSettings applies both settings through one runtime restart.
func SetChatModelSettings(ctx context.Context, host string, port int, password, model, effort string) error {
	return setChatSettings(ctx, host, port, password, map[string]string{"model": strings.TrimSpace(model), "reasoningEffort": strings.TrimSpace(effort)})
}

func setChatSettings(ctx context.Context, host string, port int, password string, settings map[string]string) error {
	body, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	resp, err := chatRequestTimeout(ctx, http.MethodPost, host, port, password, "/api/model?chat=default", body, chatModelTimeout)
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
