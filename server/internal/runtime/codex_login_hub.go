package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
	"github.com/rs/zerolog/log"
)

// CodexFileIO is the part of a sandbox the hub writes the login copy through.
type CodexFileIO interface {
	WriteFileAtomic(ctx context.Context, path string, content []byte) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

// CodexLoginSource holds the full workspace login and is the only place that
// refreshes it. Both calls return the sandbox copy (no refresh token) and its
// version.
type CodexLoginSource interface {
	EnsureFresh(ctx context.Context, credentialID string) (sandboxCopy, version string, err error)
	ForceRefresh(ctx context.Context, credentialID, staleAccessToken string) (sandboxCopy, version string, err error)
}

// codexHubWriteTimeout bounds one sandbox write during a broadcast or turn.
var codexHubWriteTimeout = 15 * time.Second

type codexLatest struct {
	body    string
	version string
	access  string
}

type codexReg struct {
	credID string
	key    string
	path   string

	mu      sync.Mutex
	file    CodexFileIO
	version string
	access  string
}

// CodexLoginHub tracks every sandbox that runs on a workspace Codex login and
// keeps their auth.json in step with the platform's copy: it pushes a new copy
// after each platform refresh or re-login, re-checks before every turn, and
// recovers a turn whose login was refused. Sandboxes never write back.
type CodexLoginHub struct {
	mu     sync.Mutex
	source CodexLoginSource
	regs   map[string]*codexReg
	latest map[string]codexLatest
}

// NewCodexLoginHub returns a hub reading logins from source (may be set later).
func NewCodexLoginHub(source CodexLoginSource) *CodexLoginHub {
	return &CodexLoginHub{source: source, regs: map[string]*codexReg{}, latest: map[string]codexLatest{}}
}

// SetSource wires the login holder after construction.
func (h *CodexLoginHub) SetSource(source CodexLoginSource) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.source = source
	h.mu.Unlock()
}

func (h *CodexLoginHub) currentSource() CodexLoginSource {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.source
}

// CodexCopyVersion identifies one sandbox copy.
func CodexCopyVersion(sandboxCopy string) string {
	sum := sha256.Sum256([]byte(sandboxCopy))
	return hex.EncodeToString(sum[:8])
}

func codexCopyAccess(sandboxCopy string) string {
	a, err := codexoauth.ParseAuthFile(sandboxCopy)
	if err != nil {
		return ""
	}
	return a.AccessToken()
}

// CodexSandboxCopy strips the refresh token from a login file. Text that is not
// a JSON object carries no refresh token Codex could use and is kept as-is.
func CodexSandboxCopy(body string) string {
	out, err := codexoauth.SandboxCopyOf(body)
	if err != nil {
		return body
	}
	return out
}

// Prepare returns the copy to install in a new sandbox. With a source and a
// credential ID it refreshes a login close to expiry first; otherwise it only
// strips the refresh token from body.
func (h *CodexLoginHub) Prepare(ctx context.Context, credentialID, body string) (string, string, error) {
	credentialID = strings.TrimSpace(credentialID)
	source := h.currentSource()
	if source == nil || credentialID == "" {
		out := CodexSandboxCopy(body)
		return out, CodexCopyVersion(out), nil
	}
	out, version, err := source.EnsureFresh(ctx, credentialID)
	if err != nil {
		return "", "", err
	}
	h.mu.Lock()
	if _, ok := h.latest[credentialID]; !ok {
		h.latest[credentialID] = codexLatest{body: out, version: version, access: codexCopyAccess(out)}
	}
	h.mu.Unlock()
	return out, version, nil
}

// Register records a sandbox under key. file may be nil while the sandbox is
// still being created (see Bind); version is the copy it was started with, or
// empty when unknown. The returned func unregisters this registration only.
func (h *CodexLoginHub) Register(credentialID, key string, file CodexFileIO, path, version string) func() {
	if h == nil || strings.TrimSpace(credentialID) == "" || key == "" {
		return func() {}
	}
	reg := &codexReg{credID: credentialID, key: key, path: path, file: file, version: version}
	h.mu.Lock()
	if l, ok := h.latest[credentialID]; ok && version != "" && l.version == version {
		reg.access = l.access
	}
	h.regs[key] = reg
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		if h.regs[key] == reg {
			delete(h.regs, key)
		}
		h.mu.Unlock()
	}
}

// Bind attaches the created sandbox to a pending registration.
func (h *CodexLoginHub) Bind(key string, file CodexFileIO) {
	reg := h.reg(key)
	if reg == nil || file == nil {
		return
	}
	reg.mu.Lock()
	reg.file = file
	reg.mu.Unlock()
}

// Unregister drops the registration under key.
func (h *CodexLoginHub) Unregister(key string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	delete(h.regs, key)
	h.mu.Unlock()
}

// Registered reports whether key has a registration.
func (h *CodexLoginHub) Registered(key string) bool {
	return h.reg(key) != nil
}

func (h *CodexLoginHub) reg(key string) *codexReg {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.regs[key]
}

func (h *CodexLoginHub) regsFor(credentialID string) []*codexReg {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*codexReg
	for _, reg := range h.regs {
		if reg.credID == credentialID {
			out = append(out, reg)
		}
	}
	return out
}

// Broadcast records a new copy for the credential and writes it to every
// registered sandbox in parallel. A failed write is logged; that sandbox is
// brought up to date before its next turn.
func (h *CodexLoginHub) Broadcast(credentialID, sandboxCopy, version string) {
	if h == nil || credentialID == "" {
		return
	}
	l := codexLatest{body: sandboxCopy, version: version, access: codexCopyAccess(sandboxCopy)}
	h.mu.Lock()
	h.latest[credentialID] = l
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, reg := range h.regsFor(credentialID) {
		wg.Add(1)
		go func(reg *codexReg) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), codexHubWriteTimeout)
			defer cancel()
			if err := reg.write(ctx, l); err != nil {
				log.Warn().Err(err).Str("credential", credentialID).Str("sandbox", reg.key).
					Msg("push Codex login to sandbox failed; retrying before its next turn")
			}
		}(reg)
	}
	wg.Wait()
}

// Forget drops the cached copy of a revoked, cleared or deleted credential so
// no turn re-installs it. Registrations stay: a later re-login still reaches
// the sandboxes.
func (h *CodexLoginHub) Forget(credentialID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	delete(h.latest, credentialID)
	h.mu.Unlock()
}

// BeforeTurn writes the current copy when the sandbox is behind; an up-to-date
// sandbox is not touched.
func (h *CodexLoginHub) BeforeTurn(ctx context.Context, key string) error {
	reg := h.reg(key)
	if reg == nil {
		return nil
	}
	h.mu.Lock()
	l, ok := h.latest[reg.credID]
	source := h.source
	h.mu.Unlock()
	if !ok {
		if source == nil {
			return nil
		}
		out, version, err := source.EnsureFresh(ctx, reg.credID)
		if err != nil {
			return err
		}
		l = codexLatest{body: out, version: version, access: codexCopyAccess(out)}
		h.mu.Lock()
		if cur, ok := h.latest[reg.credID]; ok {
			l = cur
		} else {
			h.latest[reg.credID] = l
		}
		h.mu.Unlock()
	}
	wctx, cancel := context.WithTimeout(ctx, codexHubWriteTimeout)
	defer cancel()
	return reg.write(wctx, l)
}

// Snapshot is the copy version and access token a sandbox holds; a turn takes
// it before it starts so Recover knows which login was refused.
func (h *CodexLoginHub) Snapshot(key string) (version, access string) {
	reg := h.reg(key)
	if reg == nil {
		return "", ""
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.version, reg.access
}

// Recover handles a turn whose Codex login was refused. seenVersion and
// seenAccess are the turn's Snapshot. When a newer copy reached the sandbox
// during the turn it is simply retried; otherwise the source renews the login
// (once, however many sandboxes ask) and the new copy is installed. It reports
// whether the turn should be retried.
func (h *CodexLoginHub) Recover(ctx context.Context, key, seenVersion, seenAccess string) bool {
	reg := h.reg(key)
	source := h.currentSource()
	if reg == nil || source == nil {
		return false
	}
	reg.mu.Lock()
	current, file := reg.version, reg.file
	reg.mu.Unlock()
	if current != "" && current != seenVersion {
		return true
	}
	before, stale := seenVersion, seenAccess
	if stale == "" && file != nil {
		if body, err := file.ReadFile(ctx, reg.path); err == nil {
			stale = codexCopyAccess(string(body))
		}
	}
	out, version, err := source.ForceRefresh(ctx, reg.credID, stale)
	if err != nil {
		log.Warn().Err(err).Str("credential", reg.credID).Str("sandbox", key).Msg("Codex login recovery failed")
		return false
	}
	l := codexLatest{body: out, version: version, access: codexCopyAccess(out)}
	h.mu.Lock()
	h.latest[reg.credID] = l
	h.mu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, codexHubWriteTimeout)
	defer cancel()
	if err := reg.write(wctx, l); err != nil {
		log.Warn().Err(err).Str("credential", reg.credID).Str("sandbox", key).Msg("install recovered Codex login failed")
		return false
	}
	return version != before
}

func (r *codexReg) write(ctx context.Context, l codexLatest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil || l.version == "" || r.version == l.version {
		return nil
	}
	if err := r.file.WriteFileAtomic(ctx, r.path, []byte(l.body)); err != nil {
		return err
	}
	r.version, r.access = l.version, l.access
	return nil
}
