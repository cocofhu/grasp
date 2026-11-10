package service

import (
	"context"
	"errors"
	"path/filepath"

	"backend/internal/provider"
	"backend/internal/workspacebinding"
)

var ErrWorkspaceChanged = errors.New("active workspace changed; reconnect the Agent")

// BlockWorkspaceStartup is used only by authenticated platform bootstrap while
// the provider's real resource ID is not yet known. It cannot be cleared by a
// browser command; trusted configuration restarts the backend instead.
func (m *ChatManager) BlockWorkspaceStartup() { m.mu.Lock(); m.workspaceBlocked = true; m.mu.Unlock() }

// SetWorkspaceBinding is a startup-only dependency. All session creation and
// queue admission then acquire the same native binding barrier; browser cwd
// and fsRoot fields cannot select a different directory.
func (m *ChatManager) SetWorkspaceBinding(binding *workspacebinding.Manager) error {
	if binding == nil {
		return workspacebinding.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.workspace != nil {
		return workspacebinding.ErrConflict
	}
	for _, b := range m.chats {
		b.mu.Lock()
		connected := b.sess != nil
		b.mu.Unlock()
		if connected {
			return workspacebinding.ErrBusy
		}
	}
	m.workspace = binding
	return nil
}

func (b *Bridge) bindingManager() *workspacebinding.Manager {
	if b.mgr == nil {
		return nil
	}
	b.mgr.mu.Lock()
	defer b.mgr.mu.Unlock()
	return b.mgr.workspace
}

func (b *Bridge) canonicalWorkspace(cwd, fsRoot string) (string, string, func(), error) {
	if b.mgr != nil {
		b.mgr.mu.Lock()
		blocked := b.mgr.workspaceBlocked
		b.mgr.mu.Unlock()
		if blocked {
			return "", "", nil, workspacebinding.ErrUninstalled
		}
	}
	binding := b.bindingManager()
	if binding == nil {
		return cwd, fsRoot, func() {}, nil
	}
	view, release, err := binding.Acquire(nil, false)
	if err != nil {
		return "", "", nil, err
	}
	return view.Path, view.Path, release, nil
}

func (b *Bridge) workspaceSessionMatches(session provider.Session, root string) bool {
	// The manager pointer is immutable after startup. This helper is called
	// under Bridge.mu, so it must not take ChatManager.mu (the opposite order
	// is used by metadata list/delete).
	if b.mgr == nil || b.mgr.workspace == nil {
		return true
	}
	return session != nil && filepath.Clean(session.CWD()) == root && filepath.Clean(session.FSRoot()) == root
}

// WorkspaceConsumer proves that no old Agent, turn or queued request survives
// a committed root switch. Idle subscribed clients do not count as a busy
// turn; they are closed after the intent so that reconnect picks a new session.
func (m *ChatManager) WorkspaceConsumer() workspacebinding.Consumer {
	return &chatWorkspaceConsumer{manager: m}
}

type chatWorkspaceConsumer struct{ manager *ChatManager }

func (c *chatWorkspaceConsumer) Name() string { return "agent" }

type chatWorkspaceTransition struct {
	bridges  []*Bridge
	released bool
}

func (c *chatWorkspaceConsumer) Prepare(ctx context.Context, _, _ string) (workspacebinding.Transition, error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	m := c.manager
	m.mu.Lock()
	list := make([]*Bridge, 0, len(m.order))
	for _, id := range m.order {
		list = append(list, m.chats[id])
	}
	m.mu.Unlock()
	t := &chatWorkspaceTransition{}
	for _, b := range list {
		// A connect or prompt admitted before the switch already holds a
		// binding read permit, so Execute refuses before entering this stage.
		if !b.ensureMu.TryLock() {
			_ = t.Close()
			return nil, workspacebinding.ErrBusy
		}
		if !b.enqueueMu.TryLock() {
			b.ensureMu.Unlock()
			_ = t.Close()
			return nil, workspacebinding.ErrBusy
		}
		t.bridges = append(t.bridges, b)
		b.turnMu.Lock()
		active := b.activeTurn != nil
		b.turnMu.Unlock()
		b.queueMu.Lock()
		queued := len(b.promptQueue) > 0
		b.queueMu.Unlock()
		if active || queued {
			_ = t.Close()
			return nil, workspacebinding.ErrBusy
		}
	}
	return t, nil
}
func (t *chatWorkspaceTransition) Apply(ctx context.Context) error {
	for _, b := range t.bridges {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		b.mu.Lock()
		session, cancel := b.sess, b.agentCancel
		b.sess = nil
		b.agentGeneration++
		clients := make([]*wsClient, 0, len(b.clients))
		for _, c := range b.clients {
			clients = append(clients, c)
		}
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if session != nil {
			if err := session.Close(); err != nil {
				return err
			}
		}
		for _, client := range clients {
			_ = client.conn.Close()
		}
	}
	return nil
}
func (t *chatWorkspaceTransition) Verify(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	for _, b := range t.bridges {
		b.mu.Lock()
		session := b.sess
		b.mu.Unlock()
		b.turnMu.Lock()
		active := b.activeTurn != nil
		b.turnMu.Unlock()
		b.queueMu.Lock()
		queued := len(b.promptQueue) > 0
		b.queueMu.Unlock()
		if session != nil || active || queued {
			return workspacebinding.ErrUnknown
		}
	}
	return nil
}
func (t *chatWorkspaceTransition) Close() error {
	if !t.released {
		for i := len(t.bridges) - 1; i >= 0; i-- {
			t.bridges[i].enqueueMu.Unlock()
			t.bridges[i].ensureMu.Unlock()
		}
		t.released = true
	}
	return nil
}
