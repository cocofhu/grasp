package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Holder values.
const (
	HolderAgent = "agent"
	HolderUser  = "user"
)

// ControlTTL is how long one user renewal lasts; viewers renew every 10s.
const ControlTTL = 30 * time.Second

// ErrBadHolder rejects any holder other than agent or user.
var ErrBadHolder = errors.New("holder must be agent or user")

// Control is the effective control state.
type Control struct {
	Holder    string
	ExpiresAt time.Time
}

// controlFile maps viewer ids to their user-control expiry.
type controlFile struct {
	Clients map[string]time.Time `json:"clients"`
}

// ControlStore reads and writes RunDir/control. Any unexpired viewer makes
// the holder user; with none left the agent holds control.
type ControlStore struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

// NewControlStore uses p.RunDir/control.
func NewControlStore(p Paths, now func() time.Time) *ControlStore {
	if now == nil {
		now = time.Now
	}
	return &ControlStore{path: p.controlPath(), now: now}
}

func (s *ControlStore) load() map[string]time.Time {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]time.Time{}
	}
	var f controlFile
	if json.Unmarshal(b, &f) != nil || f.Clients == nil {
		return map[string]time.Time{}
	}
	return f.Clients
}

func (s *ControlStore) effective(clients map[string]time.Time) Control {
	now := s.now()
	var latest time.Time
	for id, exp := range clients {
		if !exp.After(now) {
			delete(clients, id)
			continue
		}
		if exp.After(latest) {
			latest = exp
		}
	}
	if latest.IsZero() {
		return Control{Holder: HolderAgent}
	}
	return Control{Holder: HolderUser, ExpiresAt: latest}
}

// Read returns the effective state. An unreadable file means agent control.
func (s *ControlStore) Read() Control {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effective(s.load())
}

// Set takes (user) or returns (agent) control for one viewer. An empty
// client id is the shared "default" viewer.
func (s *ControlStore) Set(holder, client string) (Control, error) {
	holder = strings.TrimSpace(holder)
	if holder != HolderAgent && holder != HolderUser {
		return Control{}, ErrBadHolder
	}
	client = strings.TrimSpace(client)
	if client == "" {
		client = "default"
	}
	if len(client) > 128 {
		client = client[:128]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clients := s.load()
	if holder == HolderUser {
		clients[client] = s.now().Add(ControlTTL)
	} else {
		delete(clients, client)
	}
	ctl := s.effective(clients)
	if err := s.write(clients); err != nil {
		return Control{}, err
	}
	return ctl, nil
}

func (s *ControlStore) write(clients map[string]time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(controlFile{Clients: clients})
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".control-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
