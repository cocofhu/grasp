// Package codexoauth talks to the OpenAI auth service the way the Codex CLI
// does: it refreshes ChatGPT logins and runs the device-code sign-in. Neither
// endpoint is a published OpenAI API, so the base URL is configurable and every
// failure is reported without token material.
package codexoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// ClientID is the public OAuth client of the Codex CLI.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	// DefaultBaseURL is the OpenAI auth issuer.
	DefaultBaseURL = "https://auth.openai.com"
	// RefreshScope is the scope the Codex CLI asks for on refresh.
	RefreshScope = "openid profile email"

	responseLimit         = 1 << 20
	defaultDeviceInterval = 5 * time.Second
	defaultDeviceTTL      = 15 * time.Minute
	maxErrorDetail        = 200
)

// terminalCodes are refresh failures that a retry cannot fix: the refresh
// token was used already, expired, or revoked.
var terminalCodes = []string{
	"refresh_token_reused",
	"refresh_token_expired",
	"refresh_token_invalidated",
	"invalid_grant",
}

// ErrDeviceNotEnabled means the account (or workspace) has device-code sign-in
// turned off; the user enables it in ChatGPT security settings or pastes
// auth.json instead.
var ErrDeviceNotEnabled = errors.New("Codex device code login is not enabled")

// ErrTransport marks a failed request or response read, including EOFs that
// do not implement net.Error. The underlying cause remains available via Is.
var ErrTransport = errors.New("openai auth transport failure")

// Error is a non-2xx answer from the auth service. Message never carries
// tokens: it is the service's error code or a truncated description.
type Error struct {
	Status   int
	Code     string
	Message  string
	terminal bool
}

func (e *Error) Error() string {
	parts := []string{"openai auth"}
	if e.Status != 0 {
		parts = append(parts, "status "+strconv.Itoa(e.Status))
	}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" && e.Message != e.Code {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, ": ")
}

// IsTerminal reports a refresh failure that only a new sign-in can fix.
func IsTerminal(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.terminal
}

// Tokens is one token response. RefreshToken is empty when the service did not
// rotate it.
type Tokens struct {
	IDToken      string
	AccessToken  string
	RefreshToken string
}

// DeviceStart is a pending device-code sign-in. DeviceAuthID stays on the
// server; UserCode and VerificationURL are shown to the user.
type DeviceStart struct {
	DeviceAuthID    string
	UserCode        string
	VerificationURL string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// Client calls the auth service. The zero value is not usable; use NewClient.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a client for baseURL (DefaultBaseURL when empty). Requests
// follow HTTPS_PROXY / HTTP_PROXY / NO_PROXY.
func NewClient(baseURL string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	return &Client{
		baseURL: normalizeBase(baseURL),
		http:    &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}
}

func normalizeBase(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return DefaultBaseURL
	}
	return base
}

// BaseURL is the issuer the client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// VerificationURL is where the user enters the device code.
func (c *Client) VerificationURL() string { return c.baseURL + "/codex/device" }

func (c *Client) deviceRedirectURI() string { return c.baseURL + "/deviceauth/callback" }

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Refresh trades a refresh token for new tokens. Errors classified terminal
// (see IsTerminal) mean the login is gone.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Tokens{}, &Error{Code: "missing_refresh_token", terminal: true}
	}
	form := url.Values{
		"client_id":     {ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {RefreshScope},
	}
	return c.tokenRequest(ctx, form, true)
}

func (c *Client) tokenRequest(ctx context.Context, form url.Values, refresh bool) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	status, body, err := c.do(req)
	if err != nil {
		return Tokens{}, err
	}
	if status < 200 || status > 299 {
		return Tokens{}, parseError(status, body, refresh)
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return Tokens{}, &Error{Status: status, Code: "invalid_response"}
	}
	if tok.AccessToken == "" {
		return Tokens{}, &Error{Status: status, Code: "invalid_response", Message: "missing access_token"}
	}
	return Tokens(tok), nil
}

// StartDevice asks for a user code. A 404 means device sign-in is off.
func (c *Client) StartDevice(ctx context.Context) (DeviceStart, error) {
	status, body, err := c.postJSON(ctx, "/api/accounts/deviceauth/usercode", map[string]string{"client_id": ClientID})
	if err != nil {
		return DeviceStart{}, err
	}
	if status == http.StatusNotFound || strings.Contains(strings.ToLower(string(body)), "not enabled") {
		return DeviceStart{}, ErrDeviceNotEnabled
	}
	if status < 200 || status > 299 {
		return DeviceStart{}, parseError(status, body, false)
	}
	var resp struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return DeviceStart{}, &Error{Status: status, Code: "invalid_response"}
	}
	code := strings.TrimSpace(resp.UserCode)
	if code == "" {
		code = strings.TrimSpace(resp.UserCodeAlt)
	}
	if resp.DeviceAuthID == "" || code == "" {
		return DeviceStart{}, &Error{Status: status, Code: "invalid_response", Message: "missing device code"}
	}
	interval := flexSeconds(resp.Interval, defaultDeviceInterval)
	if interval < time.Second {
		interval = defaultDeviceInterval
	}
	return DeviceStart{
		DeviceAuthID:    resp.DeviceAuthID,
		UserCode:        code,
		VerificationURL: c.VerificationURL(),
		Interval:        interval,
		ExpiresIn:       flexSeconds(resp.ExpiresIn, defaultDeviceTTL),
	}, nil
}

// PollDevice checks the sign-in once. authorized=false with a nil error means
// the user has not finished yet. On authorization the code is exchanged for
// tokens right away.
func (c *Client) PollDevice(ctx context.Context, d DeviceStart) (Tokens, bool, error) {
	status, body, err := c.postJSON(ctx, "/api/accounts/deviceauth/token", map[string]string{
		"device_auth_id": d.DeviceAuthID,
		"user_code":      d.UserCode,
	})
	if err != nil {
		return Tokens{}, false, err
	}
	if status == http.StatusForbidden || status == http.StatusNotFound {
		return Tokens{}, false, nil
	}
	if status < 200 || status > 299 {
		return Tokens{}, false, parseError(status, body, false)
	}
	var resp struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.AuthorizationCode == "" || resp.CodeVerifier == "" {
		return Tokens{}, false, &Error{Status: status, Code: "invalid_response", Message: "missing authorization code"}
	}
	tok, err := c.tokenRequest(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {resp.AuthorizationCode},
		"redirect_uri":  {c.deviceRedirectURI()},
		"client_id":     {ClientID},
		"code_verifier": {resp.CodeVerifier},
	}, false)
	if err != nil {
		return Tokens{}, false, err
	}
	return tok, true, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) (int, []byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) (int, []byte, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: request: %w", ErrTransport, scrubURLError(err))
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, responseLimit))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: response: %w", ErrTransport, err)
	}
	return res.StatusCode, body, nil
}

// scrubURLError keeps the transport cause and drops the request URL, which
// never carries tokens here but would add noise to user-facing status.
func scrubURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// parseError reads both OAuth error shapes: {"error":"invalid_grant"} and
// {"error":{"code":"refresh_token_expired","message":"..."}}.
func parseError(status int, body []byte, refresh bool) error {
	e := &Error{Status: status}
	var flat struct {
		Error       json.RawMessage `json:"error"`
		Description string          `json:"error_description"`
		Code        string          `json:"code"`
		Message     string          `json:"message"`
	}
	if json.Unmarshal(body, &flat) == nil {
		var code string
		if json.Unmarshal(flat.Error, &code) == nil {
			e.Code = code
			e.Message = flat.Description
		} else {
			var nested struct {
				Code    string `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			}
			if json.Unmarshal(flat.Error, &nested) == nil {
				e.Code = nested.Code
				if e.Code == "" {
					e.Code = nested.Type
				}
				e.Message = nested.Message
			}
		}
		if e.Code == "" {
			e.Code = flat.Code
		}
		if e.Message == "" {
			e.Message = flat.Message
		}
	}
	if e.Code == "" {
		e.Code = "http_" + strconv.Itoa(status)
	}
	e.Message = truncate(strings.TrimSpace(e.Message), maxErrorDetail)
	if refresh {
		e.terminal = status == http.StatusUnauthorized || hasTerminalCode(e.Code) || hasTerminalCode(string(body))
	}
	return e
}

func hasTerminalCode(s string) bool {
	s = strings.ToLower(s)
	for _, code := range terminalCodes {
		if strings.Contains(s, code) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// flexSeconds reads a seconds value sent either as a number or a string.
func flexSeconds(raw json.RawMessage, fallback time.Duration) time.Duration {
	if len(raw) == 0 {
		return fallback
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return time.Duration(n * float64(time.Second))
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	}
	return fallback
}
