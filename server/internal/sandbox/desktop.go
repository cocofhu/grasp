package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DesktopStatus is the sandbox GET /api/desktop/status body.
type DesktopStatus struct {
	Mode           string   `json:"mode"`
	ShellReady     bool     `json:"shellReady"`
	AgentActive    bool     `json:"agentActive"`
	Holder         string   `json:"holder"`
	ExpiresAt      string   `json:"expiresAt,omitempty"`
	FallbackReason string   `json:"fallbackReason,omitempty"`
	Apps           []string `json:"apps"`
}

// browserDesktop is what a runtime bundle without the desktop API reports.
func browserDesktop() DesktopStatus {
	return DesktopStatus{Mode: "browser", Holder: "agent", Apps: []string{}}
}

// GetDesktopStatus reads the desktop status. Older runtime bundles answer
// 404 and count as the browser image.
func GetDesktopStatus(ctx context.Context, host string, port int, password string) (DesktopStatus, error) {
	resp, err := chatRequest(ctx, http.MethodGet, host, port, password, "/api/desktop/status", nil)
	if err != nil {
		return DesktopStatus{}, err
	}
	return decodeDesktop(resp, "desktop status")
}

// SetDesktopControl takes (holder=user) or returns (holder=agent) control
// for one viewer.
func SetDesktopControl(ctx context.Context, host string, port int, password, holder, client string) (DesktopStatus, error) {
	body, _ := json.Marshal(map[string]string{"holder": holder, "client": client})
	resp, err := chatRequest(ctx, http.MethodPut, host, port, password, "/api/desktop/control", body)
	if err != nil {
		return DesktopStatus{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return DesktopStatus{}, &ChatHTTPError{op: "desktop control", StatusCode: http.StatusNotFound}
	}
	return decodeDesktop(resp, "desktop control")
}

// SetDesktopClipboard replaces the sandbox desktop's text clipboard.
func SetDesktopClipboard(ctx context.Context, host string, port int, password, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := chatRequest(ctx, http.MethodPut, host, port, password, "/api/desktop/clipboard", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return &ChatHTTPError{op: "desktop clipboard", StatusCode: resp.StatusCode}
	}
	return nil
}

func decodeDesktop(resp *http.Response, op string) (DesktopStatus, error) {
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return browserDesktop(), nil
	}
	if resp.StatusCode != http.StatusOK {
		return DesktopStatus{}, &ChatHTTPError{op: op, StatusCode: resp.StatusCode}
	}
	var st DesktopStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&st); err != nil {
		return DesktopStatus{}, fmt.Errorf("%s: %w", op, err)
	}
	if st.Apps == nil {
		st.Apps = []string{}
	}
	return st, nil
}
