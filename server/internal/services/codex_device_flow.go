package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Codex device-login poll states returned to the browser.
const (
	CodexDevicePending = "pending"
	CodexDeviceSuccess = "success"
	CodexDeviceExpired = "expired"
	CodexDeviceDenied  = "denied"
	CodexDeviceError   = "error"
)

const (
	codexDeviceMinInterval = 3 * time.Second
	codexDeviceMaxPerOwner = 5
	codexDevicePollTimeout = 30 * time.Second
)

var (
	// ErrCodexDeviceSessionNotFound covers unknown, finished and foreign sessions.
	ErrCodexDeviceSessionNotFound = errors.New("Codex sign-in session not found")
	// ErrCodexDeviceTarget rejects re-login onto a credential that is not a
	// current workspace Codex login.
	ErrCodexDeviceTarget = errors.New("only a Codex ChatGPT login credential can be signed in again")
	// ErrCodexDeviceNotEnabled means the account cannot use device-code sign-in.
	ErrCodexDeviceNotEnabled = errors.New("device-code sign-in is not enabled for this ChatGPT account; enable it in ChatGPT security settings or paste ~/.codex/auth.json")
)

// codexDeviceClient is the OpenAI device-login API (codexoauth.Client).
type codexDeviceClient interface {
	StartDevice(ctx context.Context) (codexoauth.DeviceStart, error)
	// PollDevice reports authorized=false with a nil error while the user has
	// not finished.
	PollDevice(ctx context.Context, d codexoauth.DeviceStart) (codexoauth.Tokens, bool, error)
}

// CodexDeviceStart is what the browser may see of a new session; the device
// auth ID stays on the server.
type CodexDeviceStart struct {
	SessionID       string `json:"sessionId"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// CodexDevicePoll is one poll outcome. Credential carries only the masked view.
type CodexDevicePoll struct {
	Status     string          `json:"status"`
	Interval   int             `json:"interval,omitempty"`
	ExpiresIn  int             `json:"expiresIn,omitempty"`
	Credential *CredentialView `json:"credential,omitempty"`
	Email      string          `json:"email,omitempty"`
	Error      string          `json:"error,omitempty"`
	Alias      string          `json:"alias,omitempty"`
	// Updated is true when an existing credential received the new login.
	Updated bool `json:"updated,omitempty"`
}

type codexDeviceSession struct {
	id           string
	owner        string
	name         string
	credentialID string
	device       codexoauth.DeviceStart
	interval     time.Duration
	expiresAt    time.Time
	nextPoll     time.Time
	busy         bool
}

// CodexDeviceFlowService signs a workspace in to ChatGPT with the Codex device
// code flow and stores the full login (with its refresh token) as a Codex
// credential. Sessions live in process memory and are bound to the user who
// started them.
type CodexDeviceFlowService struct {
	creds  *CredentialService
	client codexDeviceClient
	verify string
	now    func() time.Time

	mu       sync.Mutex
	sessions map[string]*codexDeviceSession
}

// NewCodexDeviceFlowService talks to the OpenAI auth server at baseURL.
func NewCodexDeviceFlowService(creds *CredentialService, baseURL string) *CodexDeviceFlowService {
	client := codexoauth.NewClient(baseURL)
	return newCodexDeviceFlowService(creds, client, client.VerificationURL())
}

func newCodexDeviceFlowService(creds *CredentialService, client codexDeviceClient, verificationURL string) *CodexDeviceFlowService {
	return &CodexDeviceFlowService{creds: creds, client: client, verify: verificationURL, now: time.Now, sessions: map[string]*codexDeviceSession{}}
}

// SetClock replaces the time source used for intervals and expiry.
func (s *CodexDeviceFlowService) SetClock(now func() time.Time) {
	s.now = now
}

// Start validates the target before asking OpenAI for a user code. An empty
// credentialID creates a credential named name; otherwise that credential
// receives the new login and keeps its name.
func (s *CodexDeviceFlowService) Start(ctx context.Context, owner, name, credentialID string) (CodexDeviceStart, error) {
	credentialID = strings.TrimSpace(credentialID)
	name = strings.TrimSpace(name)
	if credentialID != "" {
		target, err := s.reloginTarget(credentialID)
		if err != nil {
			return CodexDeviceStart{}, err
		}
		name = target.Name
	} else {
		if name == "" {
			return CodexDeviceStart{}, ErrCredentialName
		}
		taken, ok, err := s.creds.credentialAliasTaken(envauth.EnvCodexAuthFile, name, "")
		if err != nil {
			return CodexDeviceStart{}, err
		}
		if ok {
			return CodexDeviceStart{}, &CredentialAliasTakenError{Alias: taken}
		}
	}
	device, err := s.client.StartDevice(ctx)
	if errors.Is(err, codexoauth.ErrDeviceNotEnabled) {
		return CodexDeviceStart{}, ErrCodexDeviceNotEnabled
	}
	if err != nil {
		return CodexDeviceStart{}, err
	}
	interval := max(device.Interval, codexDeviceMinInterval)
	verify := device.VerificationURL
	if verify == "" {
		verify = s.verify
	}
	now := s.now()
	sess := &codexDeviceSession{
		id: "cxd-" + uuid.NewString(), owner: owner, name: name, credentialID: credentialID,
		device: device, interval: interval, expiresAt: now.Add(device.ExpiresIn), nextPoll: now.Add(interval),
	}
	s.mu.Lock()
	s.pruneLocked(now, owner)
	s.sessions[sess.id] = sess
	s.mu.Unlock()
	return CodexDeviceStart{
		SessionID: sess.id, UserCode: device.UserCode, VerificationURI: verify,
		ExpiresIn: int(device.ExpiresIn / time.Second), Interval: int(interval / time.Second),
	}, nil
}

// Poll asks OpenAI at most once per interval; calls in between, or while a
// request is in flight, return pending.
func (s *CodexDeviceFlowService) Poll(ctx context.Context, owner, sessionID string) (CodexDevicePoll, error) {
	now := s.now()
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok || sess.owner != owner {
		s.mu.Unlock()
		return CodexDevicePoll{}, ErrCodexDeviceSessionNotFound
	}
	if !sess.busy && !now.Before(sess.expiresAt) {
		delete(s.sessions, sessionID)
		s.mu.Unlock()
		return CodexDevicePoll{Status: CodexDeviceExpired}, nil
	}
	if sess.busy || now.Before(sess.nextPoll) {
		out := s.pendingLocked(sess, now)
		s.mu.Unlock()
		return out, nil
	}
	sess.busy = true
	device := sess.device
	s.mu.Unlock()

	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codexDevicePollTimeout)
	tokens, authorized, err := s.client.PollDevice(pctx, device)
	cancel()

	s.mu.Lock()
	if s.sessions[sessionID] != sess {
		s.mu.Unlock()
		return CodexDevicePoll{}, ErrCodexDeviceSessionNotFound
	}
	sess.busy = false
	sess.nextPoll = s.now().Add(sess.interval)
	if err == nil && !authorized {
		out := s.pendingLocked(sess, now)
		s.mu.Unlock()
		return out, nil
	}
	var oe *codexoauth.Error
	if err != nil && !errors.As(err, &oe) {
		log.Warn().Err(err).Str("session", sessionID).Msg("Codex device token poll failed")
		out := s.pendingLocked(sess, now)
		s.mu.Unlock()
		return out, nil
	}
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if err != nil {
		switch {
		case oe.Code == "access_denied" || oe.Code == "authorization_declined":
			return CodexDevicePoll{Status: CodexDeviceDenied}, nil
		case oe.Code == "expired_token" || oe.Code == "deviceauth_expired":
			return CodexDevicePoll{Status: CodexDeviceExpired}, nil
		}
		code := oe.Code
		if code == "" {
			code = "exchange_failed"
		}
		return CodexDevicePoll{Status: CodexDeviceError, Error: code}, nil
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return CodexDevicePoll{Status: CodexDeviceError, Error: "invalid_response"}, nil
	}
	return s.save(sess, tokens), nil
}

// Cancel drops a session. Unknown or foreign sessions are ignored.
func (s *CodexDeviceFlowService) Cancel(owner, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok && sess.owner == owner {
		delete(s.sessions, sessionID)
	}
}

func (s *CodexDeviceFlowService) pendingLocked(sess *codexDeviceSession, now time.Time) CodexDevicePoll {
	return CodexDevicePoll{
		Status:    CodexDevicePending,
		Interval:  int(sess.interval / time.Second),
		ExpiresIn: int(sess.expiresAt.Sub(now) / time.Second),
	}
}

// pruneLocked drops expired sessions and keeps at most codexDeviceMaxPerOwner-1
// live sessions for owner, so the new one fits under the cap.
func (s *CodexDeviceFlowService) pruneLocked(now time.Time, owner string) {
	var oldest *codexDeviceSession
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
	if count >= codexDeviceMaxPerOwner && oldest != nil {
		delete(s.sessions, oldest.id)
	}
}

// reloginTarget accepts a current Codex login credential, including a revoked
// or cleared one the user is signing in again.
func (s *CodexDeviceFlowService) reloginTarget(id string) (CredentialView, error) {
	v, err := s.creds.Get(id)
	if err != nil {
		return CredentialView{}, err
	}
	if v.Purpose != "tool_auth" || v.ToolID != "codex" || v.EnvKey != envauth.EnvCodexAuthFile {
		return CredentialView{}, ErrCodexDeviceTarget
	}
	return v, nil
}

func (s *CodexDeviceFlowService) save(sess *codexDeviceSession, tokens codexoauth.Tokens) CodexDevicePoll {
	auth := codexoauth.NewAuthFile(tokens, s.now())
	content, err := auth.Encode()
	if err != nil {
		return CodexDevicePoll{Status: CodexDeviceError, Error: "save_failed"}
	}
	metadata := map[string]any{"authMethod": "device_code"}
	var view CredentialView
	updated := sess.credentialID != ""
	if updated {
		if _, err = s.reloginTarget(sess.credentialID); err == nil {
			view, err = s.creds.update(sess.credentialID, CredentialInput{Purpose: "tool_auth", Value: content}, metadata)
		}
	} else {
		view, err = s.creds.create(CredentialInput{
			Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex",
			EnvKey: envauth.EnvCodexAuthFile, Name: sess.name, Value: content,
		}, metadata)
	}
	if err != nil {
		out := CodexDevicePoll{Status: CodexDeviceError, Error: "save_failed"}
		var taken *CredentialAliasTakenError
		switch {
		case errors.As(err, &taken):
			out.Error, out.Alias = "alias_taken", taken.Alias
		case errors.Is(err, ErrCredentialNotFound), errors.Is(err, ErrCodexDeviceTarget):
			out.Error = "credential_unavailable"
		default:
			log.Warn().Err(err).Str("session", sess.id).Msg("save Codex device-login credential failed")
		}
		return out
	}
	return CodexDevicePoll{Status: CodexDeviceSuccess, Credential: &view, Email: auth.Email(), Updated: updated}
}
