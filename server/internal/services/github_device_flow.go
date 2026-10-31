package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// GitHubDeviceScopes is the fixed OAuth scope set requested by the device flow.
const GitHubDeviceScopes = "repo read:org workflow"

// GitHubDeviceHost is the only Git host the device flow signs in to.
const GitHubDeviceHost = "https://github.com"

const (
	githubDeviceGrantType     = "urn:ietf:params:oauth:grant-type:device_code"
	githubDeviceSlowDownStep  = 5 * time.Second
	githubDeviceMinInterval   = 5 * time.Second
	githubDeviceDefaultTTL    = 15 * time.Minute
	githubDeviceMaxPerOwner   = 5
	githubDeviceResponseLimit = 1 << 20
)

// Device-flow poll states returned to the browser.
const (
	GitHubDevicePending    = "pending"
	GitHubDeviceAuthorized = "authorized"
	GitHubDeviceDenied     = "denied"
	GitHubDeviceExpired    = "expired"
	GitHubDeviceError      = "error"
)

var (
	// ErrGitHubDeviceFlowDisabled means no OAuth client ID is configured.
	ErrGitHubDeviceFlowDisabled = errors.New("GitHub sign-in is not configured")
	// ErrGitHubDeviceSessionNotFound covers unknown, finished and foreign sessions.
	ErrGitHubDeviceSessionNotFound = errors.New("GitHub sign-in session not found")
	// ErrGitHubDeviceTarget rejects re-login onto a credential that is not a
	// current github.com GitHub token.
	ErrGitHubDeviceTarget = errors.New("only a github.com GitHub credential can be signed in again")
)

// GitHubAPIError is an OAuth error reported by GitHub (for example
// device_flow_disabled or incorrect_client_credentials).
type GitHubAPIError struct {
	Code        string
	Description string
}

func (e *GitHubAPIError) Error() string {
	if e.Description != "" {
		return "GitHub: " + e.Code + ": " + e.Description
	}
	return "GitHub: " + e.Code
}

// GitHubDeviceStart is what the browser may see of a new session. The
// device_code stays on the server.
type GitHubDeviceStart struct {
	SessionID       string `json:"sessionId"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// GitHubDevicePoll is one poll outcome. Credential carries only the masked view.
type GitHubDevicePoll struct {
	Status       string          `json:"status"`
	Interval     int             `json:"interval,omitempty"`
	ExpiresIn    int             `json:"expiresIn,omitempty"`
	Credential   *CredentialView `json:"credential,omitempty"`
	GitHubLogin  string          `json:"githubLogin,omitempty"`
	Scopes       []string        `json:"scopes,omitempty"`
	ScopeWarning bool            `json:"scopeWarning,omitempty"`
	Error        string          `json:"error,omitempty"`
	Alias        string          `json:"alias,omitempty"`
	// Updated is true when an existing credential received the new token.
	Updated bool `json:"updated,omitempty"`
}

type githubDeviceSession struct {
	id           string
	owner        string
	name         string
	credentialID string
	deviceCode   string
	interval     time.Duration
	expiresAt    time.Time
	nextPoll     time.Time
	busy         bool
}

// GitHubDeviceFlowService runs the GitHub OAuth device flow on behalf of the
// browser and stores the resulting token as a regular GitHub credential.
// Sessions live in process memory (single-instance deployment); a restart
// drops them and the user starts again.
type GitHubDeviceFlowService struct {
	creds    *CredentialService
	clientID func() string
	webBase  string
	apiBase  string
	client   *http.Client
	now      func() time.Time

	mu       sync.Mutex
	sessions map[string]*githubDeviceSession
}

// NewGitHubDeviceFlowService reads the client ID on every call so a config
// reload takes effect without a restart.
func NewGitHubDeviceFlowService(creds *CredentialService, clientID func() string) *GitHubDeviceFlowService {
	return &GitHubDeviceFlowService{
		creds:    creds,
		clientID: clientID,
		webBase:  "https://github.com",
		apiBase:  "https://api.github.com",
		client:   &http.Client{Timeout: 10 * time.Second},
		now:      time.Now,
		sessions: map[string]*githubDeviceSession{},
	}
}

// SetBaseURLs points the service at another GitHub (tests use httptest).
func (s *GitHubDeviceFlowService) SetBaseURLs(web, api string) {
	s.webBase = strings.TrimRight(web, "/")
	s.apiBase = strings.TrimRight(api, "/")
}

// SetClock replaces the time source used for intervals and expiry.
func (s *GitHubDeviceFlowService) SetClock(now func() time.Time) {
	s.now = now
}

func (s *GitHubDeviceFlowService) currentClientID() string {
	if s == nil || s.clientID == nil {
		return ""
	}
	return strings.TrimSpace(s.clientID())
}

// Enabled reports whether a client ID is configured.
func (s *GitHubDeviceFlowService) Enabled() bool {
	return s.currentClientID() != ""
}

// Start validates the target before asking GitHub for a device code. An empty
// credentialID creates a new credential named name; otherwise that credential
// gets the new token and keeps its name.
func (s *GitHubDeviceFlowService) Start(ctx context.Context, owner, name, credentialID string) (GitHubDeviceStart, error) {
	clientID := s.currentClientID()
	if clientID == "" {
		return GitHubDeviceStart{}, ErrGitHubDeviceFlowDisabled
	}
	credentialID = strings.TrimSpace(credentialID)
	name = strings.TrimSpace(name)
	if credentialID != "" {
		target, err := s.reloginTarget(credentialID)
		if err != nil {
			return GitHubDeviceStart{}, err
		}
		name = target.Name
	} else {
		if name == "" {
			return GitHubDeviceStart{}, ErrCredentialName
		}
		if err := s.checkAlias(name); err != nil {
			return GitHubDeviceStart{}, err
		}
	}

	var resp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	form := url.Values{"client_id": {clientID}, "scope": {GitHubDeviceScopes}}
	if err := s.postForm(ctx, s.webBase+"/login/device/code", form, &resp); err != nil {
		return GitHubDeviceStart{}, err
	}
	if resp.DeviceCode == "" || resp.UserCode == "" {
		return GitHubDeviceStart{}, &GitHubAPIError{Code: "invalid_response", Description: "missing device code"}
	}
	if resp.VerificationURI == "" {
		resp.VerificationURI = s.webBase + "/login/device"
	}
	interval := time.Duration(resp.Interval) * time.Second
	if interval < githubDeviceMinInterval {
		interval = githubDeviceMinInterval
	}
	ttl := time.Duration(resp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = githubDeviceDefaultTTL
	}
	now := s.now()
	sess := &githubDeviceSession{
		id: "ghd-" + uuid.NewString(), owner: owner, name: name, credentialID: credentialID,
		deviceCode: resp.DeviceCode, interval: interval, expiresAt: now.Add(ttl), nextPoll: now.Add(interval),
	}
	s.mu.Lock()
	s.pruneLocked(now, owner)
	s.sessions[sess.id] = sess
	s.mu.Unlock()
	return GitHubDeviceStart{
		SessionID: sess.id, UserCode: resp.UserCode, VerificationURI: resp.VerificationURI,
		ExpiresIn: int(ttl / time.Second), Interval: int(interval / time.Second),
	}, nil
}

// Poll asks GitHub for the token at most once per interval. Calls in between
// (or while a request is in flight) return pending without contacting GitHub.
func (s *GitHubDeviceFlowService) Poll(ctx context.Context, owner, sessionID string) (GitHubDevicePoll, error) {
	now := s.now()
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok || sess.owner != owner {
		s.mu.Unlock()
		return GitHubDevicePoll{}, ErrGitHubDeviceSessionNotFound
	}
	if !now.Before(sess.expiresAt) {
		delete(s.sessions, sessionID)
		s.mu.Unlock()
		return GitHubDevicePoll{Status: GitHubDeviceExpired}, nil
	}
	if sess.busy || now.Before(sess.nextPoll) {
		out := s.pendingLocked(sess, now)
		s.mu.Unlock()
		return out, nil
	}
	sess.busy = true
	clientID, deviceCode := s.currentClientID(), sess.deviceCode
	s.mu.Unlock()

	tok, err := s.requestToken(ctx, clientID, deviceCode)

	s.mu.Lock()
	sess.busy = false
	sess.nextPoll = s.now().Add(sess.interval)
	if err != nil {
		var apiErr *GitHubAPIError
		if !errors.As(err, &apiErr) {
			// Network trouble is transient; the next poll tries again.
			log.Warn().Err(err).Str("session", sessionID).Msg("GitHub device token poll failed")
			out := s.pendingLocked(sess, now)
			s.mu.Unlock()
			return out, nil
		}
		switch apiErr.Code {
		case "authorization_pending":
			out := s.pendingLocked(sess, now)
			s.mu.Unlock()
			return out, nil
		case "slow_down":
			sess.interval += githubDeviceSlowDownStep
			if tok.Interval > 0 && time.Duration(tok.Interval)*time.Second > sess.interval {
				sess.interval = time.Duration(tok.Interval) * time.Second
			}
			sess.nextPoll = s.now().Add(sess.interval)
			out := s.pendingLocked(sess, now)
			s.mu.Unlock()
			return out, nil
		}
		delete(s.sessions, sessionID)
		s.mu.Unlock()
		switch apiErr.Code {
		case "access_denied":
			return GitHubDevicePoll{Status: GitHubDeviceDenied}, nil
		case "expired_token":
			return GitHubDevicePoll{Status: GitHubDeviceExpired}, nil
		}
		return GitHubDevicePoll{Status: GitHubDeviceError, Error: apiErr.Code}, nil
	}
	if tok.AccessToken == "" {
		delete(s.sessions, sessionID)
		s.mu.Unlock()
		return GitHubDevicePoll{Status: GitHubDeviceError, Error: "invalid_response"}, nil
	}
	// The device code is single-use; drop the session before saving so a
	// concurrent poll cannot store the token twice.
	delete(s.sessions, sessionID)
	s.mu.Unlock()

	login, scopes := s.fetchUser(ctx, tok.AccessToken)
	if scopes == nil {
		scopes = splitScopes(tok.Scope)
	}
	return s.save(sess, tok.AccessToken, login, scopes), nil
}

// Cancel drops a session. Unknown or foreign sessions are ignored.
func (s *GitHubDeviceFlowService) Cancel(owner, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok && sess.owner == owner {
		delete(s.sessions, sessionID)
	}
}

func (s *GitHubDeviceFlowService) pendingLocked(sess *githubDeviceSession, now time.Time) GitHubDevicePoll {
	return GitHubDevicePoll{
		Status:    GitHubDevicePending,
		Interval:  int(sess.interval / time.Second),
		ExpiresIn: int(sess.expiresAt.Sub(now) / time.Second),
	}
}

// pruneLocked drops expired sessions and keeps at most githubDeviceMaxPerOwner-1
// live sessions for owner, so the new one fits under the cap.
func (s *GitHubDeviceFlowService) pruneLocked(now time.Time, owner string) {
	var oldest *githubDeviceSession
	count := 0
	for id, sess := range s.sessions {
		if !now.Before(sess.expiresAt) {
			delete(s.sessions, id)
			continue
		}
		if sess.owner == owner {
			count++
			if oldest == nil || sess.expiresAt.Before(oldest.expiresAt) {
				oldest = sess
			}
		}
	}
	if count >= githubDeviceMaxPerOwner && oldest != nil {
		delete(s.sessions, oldest.id)
	}
}

func (s *GitHubDeviceFlowService) reloginTarget(id string) (CredentialView, error) {
	v, err := s.creds.Get(id)
	if err != nil {
		return CredentialView{}, err
	}
	if v.Purpose != "git" || !strings.EqualFold(v.Provider, "github") || v.Host != GitHubDeviceHost || v.RevokedAt != nil {
		return CredentialView{}, ErrGitHubDeviceTarget
	}
	return v, nil
}

func (s *GitHubDeviceFlowService) checkAlias(name string) error {
	taken, ok, err := s.creds.credentialAliasTaken(envauth.EnvGitHubToken, name, "")
	if err != nil {
		return err
	}
	if ok {
		return &CredentialAliasTakenError{Alias: taken}
	}
	return nil
}

func (s *GitHubDeviceFlowService) save(sess *githubDeviceSession, token, login string, scopes []string) GitHubDevicePoll {
	metadata := map[string]any{"authMethod": "device_flow", "scopes": scopes}
	if login != "" {
		metadata["githubLogin"] = login
	}
	var (
		view CredentialView
		err  error
	)
	updated := sess.credentialID != ""
	if updated {
		if _, err = s.reloginTarget(sess.credentialID); err == nil {
			view, err = s.creds.update(sess.credentialID, CredentialInput{Purpose: "git", Value: token}, metadata)
		}
	} else {
		view, err = s.creds.create(CredentialInput{
			Purpose: "git", Type: "git", Provider: "github", Issuer: "github", Host: GitHubDeviceHost,
			EnvKey: envauth.EnvGitHubToken, Name: sess.name, Value: token,
		}, metadata)
	}
	if err != nil {
		out := GitHubDevicePoll{Status: GitHubDeviceError, Error: "save_failed"}
		var taken *CredentialAliasTakenError
		switch {
		case errors.As(err, &taken):
			out.Error, out.Alias = "alias_taken", taken.Alias
		case errors.Is(err, ErrCredentialNotFound), errors.Is(err, ErrGitHubDeviceTarget):
			out.Error = "credential_unavailable"
		default:
			log.Warn().Err(err).Str("session", sess.id).Msg("save GitHub device-flow credential failed")
		}
		return out
	}
	return GitHubDevicePoll{
		Status: GitHubDeviceAuthorized, Credential: &view, GitHubLogin: login, Scopes: scopes,
		ScopeWarning: !hasScope(scopes, "repo"), Updated: updated,
	}
}

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Interval    int    `json:"interval"`
}

func (s *GitHubDeviceFlowService) requestToken(ctx context.Context, clientID, deviceCode string) (githubTokenResponse, error) {
	var tok githubTokenResponse
	form := url.Values{"client_id": {clientID}, "device_code": {deviceCode}, "grant_type": {githubDeviceGrantType}}
	err := s.postForm(ctx, s.webBase+"/login/oauth/access_token", form, &tok)
	return tok, err
}

// postForm sends an OAuth form request. GitHub reports OAuth errors with HTTP
// 200 and an "error" field, so the body is always checked.
func (s *GitHubDeviceFlowService) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, githubDeviceResponseLimit))
	if err != nil {
		return err
	}
	var oauthErr struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &oauthErr)
	if oauthErr.Error != "" {
		_ = json.Unmarshal(body, out)
		return &GitHubAPIError{Code: oauthErr.Error, Description: oauthErr.ErrorDescription}
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &GitHubAPIError{Code: fmt.Sprintf("http_%d", res.StatusCode)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &GitHubAPIError{Code: "invalid_response", Description: err.Error()}
	}
	return nil
}

// fetchUser reads the account login and granted scopes. Failure is not fatal:
// the token is still saved, without the account name. A nil scope slice means
// the header was absent.
func (s *GitHubDeviceFlowService) fetchUser(ctx context.Context, token string) (string, []string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase+"/user", nil)
	if err != nil {
		return "", nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := s.client.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("GitHub /user lookup failed")
		return "", nil
	}
	defer func() { _ = res.Body.Close() }()
	var scopes []string
	if _, ok := res.Header["X-Oauth-Scopes"]; ok {
		scopes = splitScopes(res.Header.Get("X-OAuth-Scopes"))
	}
	if res.StatusCode != http.StatusOK {
		log.Warn().Int("status", res.StatusCode).Msg("GitHub /user lookup failed")
		return "", scopes
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, githubDeviceResponseLimit)).Decode(&user); err != nil {
		return "", scopes
	}
	return user.Login, scopes
}

func splitScopes(raw string) []string {
	out := []string{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}
