package service

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"

	"backend/internal/provider"
)

// sessionPointer is the native session a chat continues after the backend
// restarts. It applies only to the same agent in the same directory.
type sessionPointer struct {
	Agent   string `json:"agent"`
	CWD     string `json:"cwd"`
	Session string `json:"session"`
	// History: the session took a turn, so resuming it keeps the dialogue.
	History bool `json:"history,omitempty"`
}

// pointerFile keeps the default chat's sessionPointer on disk. Agents store
// their sessions under the home directory, so the file belongs next to them
// and shares their lifetime.
type pointerFile struct {
	path string

	mu   sync.Mutex
	last sessionPointer
}

// DefaultPointerPath is where the sandbox backend keeps the pointer.
func DefaultPointerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grasp", "agent-session.json"), nil
}

func (f *pointerFile) load(agent provider.Name, cwd string) (string, bool) {
	if f == nil {
		return "", false
	}
	raw, err := os.ReadFile(f.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("acp: 读取会话指针失败: %v", err)
		}
		return "", false
	}
	var p sessionPointer
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Printf("acp: 会话指针 %s 无法解析，忽略: %v", f.path, err)
		return "", false
	}
	if p.Agent != string(agent) || p.CWD != cwd {
		return "", false
	}
	return p.Session, p.History
}

// store records p; an empty Session removes the pointer, so a new
// conversation is never mistaken for the previous one.
func (f *pointerFile) store(p sessionPointer) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p == f.last {
		return
	}
	if err := f.write(p); err != nil {
		log.Printf("acp: 保存会话指针失败: %v", err)
		return
	}
	f.last = p
}

func (f *pointerFile) write(p sessionPointer) error {
	if p.Session == "" {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".agent-session-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

// PersistSession keeps this chat's native session across backend restarts:
// the first agent opened later continues it when agent and directory match.
func (b *Bridge) PersistSession(path string) {
	b.mu.Lock()
	b.pointer = &pointerFile{path: path}
	b.mu.Unlock()
}

// rememberSession records sess as the conversation to continue, if it is
// still this chat's session.
func (b *Bridge) rememberSession(sess provider.Session) {
	b.mu.Lock()
	f, current, history := b.pointer, b.sess == sess, b.sessHistory
	b.mu.Unlock()
	if f == nil || !current {
		return
	}
	f.store(sessionPointer{Agent: string(b.agent().Name()), CWD: sess.CWD(), Session: resumePointer(sess), History: history})
}

// stateDir is where the agent keeps what resuming the persisted session
// needs besides its id; empty when this chat does not persist its session.
func (b *Bridge) stateDir() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pointer == nil {
		return ""
	}
	return filepath.Dir(b.pointer.path)
}

func (b *Bridge) storedSession(cwd string) (string, bool) {
	b.mu.Lock()
	f := b.pointer
	b.mu.Unlock()
	return f.load(b.agent().Name(), cwd)
}
