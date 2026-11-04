package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// Codex login states shown on the credentials page.
const (
	CodexLoginOK           = "ok"
	CodexLoginNeedsRelogin = "needs_relogin"
	CodexLoginNetworkError = "network_error"
)

const (
	defaultCodexRefreshLead   = 96 * time.Hour
	defaultCodexCheckInterval = 10 * time.Minute
	// codexStaleRefreshAge renews a login whose expiry cannot be read.
	codexStaleRefreshAge = 6 * 24 * time.Hour
	codexMaxBackoff      = time.Hour
)

var (
	codexRefreshTimeout = 60 * time.Second
	codexSaveAttempts   = 5
	codexSaveBackoff    = 200 * time.Millisecond
)

// codexRefresher is the OpenAI token endpoint (codexoauth.Client in production).
type codexRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (codexoauth.Tokens, error)
}

// CodexLoginManagerConfig tunes the background renewal.
type CodexLoginManagerConfig struct {
	RefreshLead   time.Duration
	CheckInterval time.Duration
	AuthBaseURL   string
}

type codexLoginState struct {
	revision uint64
	status   string
	failures int
	nextTry  time.Time
}

type codexFlight struct {
	done    chan struct{}
	copy    string
	version string
	err     error
}

// codexUnsaved is a renewed login the database refused to store. OpenAI has
// already rotated the refresh token, so it is retried before anything else.
type codexUnsaved struct {
	prev    codexStoredLogin
	content string
}

// CodexLoginManager is the only holder and user of Codex refresh tokens. It
// renews each workspace login well before the access token expires, merges
// concurrent renewals of one login, stores the result with a compare-and-swap
// and only then pushes the token-free copy to running sandboxes.
type CodexLoginManager struct {
	creds    *CredentialService
	client   codexRefresher
	hub      *runtime.CodexLoginHub
	lead     time.Duration
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	flights map[string]*codexFlight
	states  map[string]*codexLoginState
	unsaved map[string]codexUnsaved

	bg     sync.WaitGroup
	cancel context.CancelFunc
	loop   sync.WaitGroup
}

// NewCodexLoginManager wires the manager as the hub's login source and the
// credential service's Codex hooks.
func NewCodexLoginManager(creds *CredentialService, hub *runtime.CodexLoginHub, cfg CodexLoginManagerConfig) *CodexLoginManager {
	return newCodexLoginManager(creds, hub, codexoauth.NewClient(cfg.AuthBaseURL), cfg)
}

func newCodexLoginManager(creds *CredentialService, hub *runtime.CodexLoginHub, client codexRefresher, cfg CodexLoginManagerConfig) *CodexLoginManager {
	m := &CodexLoginManager{
		creds: creds, client: client, hub: hub,
		lead: cfg.RefreshLead, interval: cfg.CheckInterval, now: time.Now,
		flights: map[string]*codexFlight{}, states: map[string]*codexLoginState{}, unsaved: map[string]codexUnsaved{},
	}
	if m.lead <= 0 {
		m.lead = defaultCodexRefreshLead
	}
	if m.interval <= 0 {
		m.interval = defaultCodexCheckInterval
	}
	hub.SetSource(m)
	creds.SetCodexLoginHooks(m)
	return m
}

// Start runs the renewal loop until Stop or ctx ends.
func (m *CodexLoginManager) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.loop.Add(1)
	go func() {
		defer m.loop.Done()
		t := time.NewTicker(m.interval)
		defer t.Stop()
		for {
			m.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Stop ends the loop and waits for background pushes.
func (m *CodexLoginManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.loop.Wait()
	m.bg.Wait()
}

// Tick renews every login that is due and not waiting out a backoff or a
// re-login.
func (m *CodexLoginManager) Tick(ctx context.Context) {
	m.retryUnsaved()
	ids, err := m.creds.codexLoginIDs()
	if err != nil {
		log.Warn().Err(err).Msg("list Codex logins for renewal failed")
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		login, auth, err := m.load(id)
		if err != nil || auth == nil || !m.needsRefresh(auth) || m.waiting(id, login.Revision) {
			continue
		}
		if _, _, err := m.renew(ctx, id, false, ""); err != nil {
			log.Warn().Str("credential", id).Str("reason", codexErrSummary(err)).Msg("scheduled Codex login renewal failed")
		}
	}
}

// EnsureFresh returns the sandbox copy for a new sandbox, renewing first when
// the login is due. A failed renewal still serves a login whose access token
// has not expired.
func (m *CodexLoginManager) EnsureFresh(ctx context.Context, credentialID string) (string, string, error) {
	login, auth, err := m.load(credentialID)
	if err != nil {
		return "", "", err
	}
	if auth == nil {
		out := runtime.CodexSandboxCopy(login.Body)
		return out, runtime.CodexCopyVersion(out), nil
	}
	if !m.needsRefresh(auth) || (m.waiting(credentialID, login.Revision) && m.accessValid(auth)) {
		return sandboxCopy(auth)
	}
	out, version, err := m.renew(ctx, credentialID, false, "")
	if err != nil {
		if m.accessValid(auth) {
			return sandboxCopy(auth)
		}
		return "", "", err
	}
	return out, version, nil
}

// ForceRefresh renews a login a sandbox reported as refused. When the stored
// access token already differs from staleAccess, someone renewed it and the
// current copy is returned without calling OpenAI.
func (m *CodexLoginManager) ForceRefresh(ctx context.Context, credentialID, staleAccess string) (string, string, error) {
	login, auth, err := m.load(credentialID)
	if err != nil {
		return "", "", err
	}
	if auth == nil {
		return "", "", runtime.ErrCodexLoginRejected
	}
	if staleAccess != "" && auth.AccessToken() != staleAccess {
		return sandboxCopy(auth)
	}
	if st := m.state(credentialID, login.Revision); st == CodexLoginNeedsRelogin {
		return "", "", runtime.ErrCodexLoginRejected
	}
	return m.renew(ctx, credentialID, true, staleAccess)
}

// Status is the token-free state of one login, or nil when it is not a usable
// Codex login.
func (m *CodexLoginManager) Status(credentialID string) *CodexLoginInfo {
	login, auth, err := m.load(credentialID)
	if err != nil {
		return nil
	}
	info := &CodexLoginInfo{Status: m.state(credentialID, login.Revision)}
	if auth == nil {
		info.Status = CodexLoginNeedsRelogin
		return info
	}
	info.Email = auth.Email()
	if t, ok := auth.LastRefresh(); ok {
		info.LastRefresh = &t
	}
	return info
}

// CodexLoginInfo implements CodexLoginHooks.
func (m *CodexLoginManager) CodexLoginInfo(credentialID string) *CodexLoginInfo {
	return m.Status(credentialID)
}

// CodexLoginSaved implements CodexLoginHooks: a user re-login resets the
// state and the new copy is pushed in the background.
func (m *CodexLoginManager) CodexLoginSaved(credentialID string) {
	m.mu.Lock()
	delete(m.states, credentialID)
	delete(m.unsaved, credentialID)
	m.mu.Unlock()
	m.bg.Add(1)
	go func() {
		defer m.bg.Done()
		login, auth, err := m.load(credentialID)
		if err != nil {
			return
		}
		out := runtime.CodexSandboxCopy(login.Body)
		if auth != nil {
			if out, _, err = sandboxCopy(auth); err != nil {
				return
			}
		}
		m.hub.Broadcast(credentialID, out, runtime.CodexCopyVersion(out))
	}()
}

// CodexLoginGone implements CodexLoginHooks.
func (m *CodexLoginManager) CodexLoginGone(credentialID string) {
	m.mu.Lock()
	delete(m.states, credentialID)
	delete(m.unsaved, credentialID)
	m.mu.Unlock()
	m.hub.Forget(credentialID)
}

// load reads the stored login. auth is nil when the body is not an auth.json
// (a legacy value); such a login is served as-is and never renewed.
func (m *CodexLoginManager) load(credentialID string) (codexStoredLogin, *codexoauth.AuthFile, error) {
	login, err := m.creds.codexLogin(credentialID)
	if errors.Is(err, ErrCredentialNotFound) {
		return login, nil, fmt.Errorf("%w: %w", runtime.ErrCodexLoginRejected, err)
	}
	if err != nil {
		return login, nil, err
	}
	m.mu.Lock()
	if u, ok := m.unsaved[credentialID]; ok && u.prev.ValueEnc == login.ValueEnc {
		login.Body = u.content
	}
	m.mu.Unlock()
	auth, perr := codexoauth.ParseAuthFile(login.Body)
	if perr != nil {
		return login, nil, nil
	}
	return login, auth, nil
}

func sandboxCopy(auth *codexoauth.AuthFile) (string, string, error) {
	out, err := auth.SandboxCopy()
	if err != nil {
		return "", "", err
	}
	return out, runtime.CodexCopyVersion(out), nil
}

func (m *CodexLoginManager) needsRefresh(auth *codexoauth.AuthFile) bool {
	now := m.now()
	if exp, ok := auth.AccessExpiry(); ok {
		return exp.Sub(now) < m.lead
	}
	if last, ok := auth.LastRefresh(); ok {
		return now.Sub(last) > codexStaleRefreshAge
	}
	return true
}

func (m *CodexLoginManager) accessValid(auth *codexoauth.AuthFile) bool {
	exp, ok := auth.AccessExpiry()
	return ok && exp.After(m.now())
}

// state is the status recorded for the login's current revision; a newer
// revision (the user signed in again) starts clean.
func (m *CodexLoginManager) state(credentialID string, revision uint64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.states[credentialID]; st != nil && st.revision == revision {
		return st.status
	}
	return CodexLoginOK
}

// waiting reports whether the scheduler should leave the login alone: it needs
// a re-login, or a transient failure is backing off.
func (m *CodexLoginManager) waiting(credentialID string, revision uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[credentialID]
	if st == nil || st.revision != revision {
		return false
	}
	return st.status == CodexLoginNeedsRelogin || st.status == CodexLoginNetworkError && m.now().Before(st.nextTry)
}

func (m *CodexLoginManager) record(credentialID string, revision uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		m.states[credentialID] = &codexLoginState{revision: revision, status: CodexLoginOK}
		return
	}
	st := m.states[credentialID]
	if st == nil || st.revision != revision {
		st = &codexLoginState{revision: revision}
		m.states[credentialID] = st
	}
	if codexoauth.IsTerminal(err) {
		st.status = CodexLoginNeedsRelogin
		return
	}
	st.status = CodexLoginNetworkError
	st.failures++
	backoff := time.Minute << min(st.failures-1, 6)
	st.nextTry = m.now().Add(min(backoff, codexMaxBackoff))
}

// renew runs one renewal per login however many callers ask at once. The
// renewal outlives a cancelled caller: a rotated refresh token must be saved.
func (m *CodexLoginManager) renew(ctx context.Context, credentialID string, force bool, stale string) (string, string, error) {
	m.mu.Lock()
	f := m.flights[credentialID]
	if f == nil {
		f = &codexFlight{done: make(chan struct{})}
		m.flights[credentialID] = f
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codexRefreshTimeout)
		go func() {
			defer cancel()
			f.copy, f.version, f.err = m.renewOnce(rctx, credentialID, force, stale)
			m.mu.Lock()
			delete(m.flights, credentialID)
			m.mu.Unlock()
			close(f.done)
		}()
	}
	m.mu.Unlock()
	select {
	case <-f.done:
		return f.copy, f.version, f.err
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func (m *CodexLoginManager) renewOnce(ctx context.Context, credentialID string, force bool, stale string) (string, string, error) {
	login, auth, err := m.load(credentialID)
	if err != nil {
		return "", "", err
	}
	if auth == nil {
		return "", "", runtime.ErrCodexLoginRejected
	}
	if force && stale != "" && auth.AccessToken() != stale || !force && !m.needsRefresh(auth) {
		return sandboxCopy(auth)
	}
	tokens, err := m.client.Refresh(ctx, auth.RefreshToken())
	if err != nil {
		m.record(credentialID, login.Revision, err)
		if codexoauth.IsTerminal(err) {
			log.Warn().Str("credential", credentialID).Str("reason", codexErrSummary(err)).Msg("Codex login needs a new sign-in")
			return "", "", fmt.Errorf("%w: %s", runtime.ErrCodexLoginRejected, codexErrSummary(err))
		}
		return "", "", fmt.Errorf("renew Codex login: %w", err)
	}
	auth.ApplyTokens(tokens, m.now())
	content, err := auth.Encode()
	if err != nil {
		return "", "", err
	}
	revision, err := m.save(login, content)
	switch {
	case errors.Is(err, errCodexLoginConflict):
		// The user signed in again meanwhile; the stored login wins.
		_, current, lerr := m.load(credentialID)
		if lerr != nil {
			return "", "", lerr
		}
		if current == nil {
			return "", "", runtime.ErrCodexLoginRejected
		}
		return sandboxCopy(current)
	case err != nil:
		log.Error().Err(err).Str("credential", credentialID).Msg("save renewed Codex login failed; retrying on the next check")
		m.mu.Lock()
		m.unsaved[credentialID] = codexUnsaved{prev: login, content: content}
		m.mu.Unlock()
		revision = login.Revision
	default:
		m.mu.Lock()
		delete(m.unsaved, credentialID)
		m.mu.Unlock()
	}
	m.record(credentialID, revision, nil)
	out, version, err := sandboxCopy(auth)
	if err != nil {
		return "", "", err
	}
	m.hub.Broadcast(credentialID, out, version)
	return out, version, nil
}

func (m *CodexLoginManager) save(prev codexStoredLogin, content string) (uint64, error) {
	var err error
	for attempt := range codexSaveAttempts {
		var revision uint64
		revision, err = m.creds.saveCodexLogin(prev, content)
		if err == nil || errors.Is(err, errCodexLoginConflict) {
			return revision, err
		}
		time.Sleep(codexSaveBackoff << attempt)
	}
	return 0, err
}

func (m *CodexLoginManager) retryUnsaved() {
	m.mu.Lock()
	pending := make(map[string]codexUnsaved, len(m.unsaved))
	for id, u := range m.unsaved {
		pending[id] = u
	}
	m.mu.Unlock()
	for id, u := range pending {
		revision, err := m.creds.saveCodexLogin(u.prev, u.content)
		if err != nil && !errors.Is(err, errCodexLoginConflict) {
			log.Error().Err(err).Str("credential", id).Msg("save renewed Codex login failed again")
			continue
		}
		m.mu.Lock()
		if cur, ok := m.unsaved[id]; ok && cur.content == u.content {
			delete(m.unsaved, id)
		}
		m.mu.Unlock()
		if err == nil {
			m.record(id, revision, nil)
		}
	}
}

// codexErrSummary is a log-safe reason: codexoauth errors carry no tokens.
func codexErrSummary(err error) string {
	var oe *codexoauth.Error
	if errors.As(err, &oe) {
		if oe.Code != "" {
			return oe.Code
		}
		return fmt.Sprintf("http %d", oe.Status)
	}
	return err.Error()
}
