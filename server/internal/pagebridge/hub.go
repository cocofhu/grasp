// Package pagebridge relays page-operation commands from an agent's MCP tool
// call to the direct-preview drawer a user opened, and carries results back.
//
// A drawer connection registers under (run, node, owner). The owner is the
// person the drawer belongs to; a command is routed only to the owner of the
// turn that issued it, so an agent never acts on someone else's page.
package pagebridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusOnline  Status = "online"
	StatusPaused  Status = "paused"
	StatusOffline Status = "offline"
)

const (
	DefaultPauseWait      = 15 * time.Second
	DefaultReconnectWait  = 10 * time.Second
	DefaultCommandTimeout = 30 * time.Second
	// DefaultCallBudget bounds one tool call end to end (pause wait, command,
	// reconnect and the follow-up state read) below the MCP client timeout.
	DefaultCallBudget = 50 * time.Second
)

var (
	ErrNoOwner   = errors.New("这一轮不是由用户在对话里发起的,不能操作页面")
	ErrOffline   = errors.New("用户没有打开允许 Agent 操作的直连预览页(或已关闭开关);请在回复里请用户在预览页抽屉打开「允许 Agent 操作页面」")
	ErrPaused    = errors.New("用户已切到其他标签页,预览页暂停操作;请在回复里请用户切回预览页后再继续")
	ErrTimeout   = errors.New("页面没有在规定时间内返回结果,操作结果未知;请先调用 page_state 确认页面现状")
	ErrStopped   = errors.New("用户停止了页面操作")
	ErrCancelled = errors.New("本轮已取消,页面操作已停止")
	ErrLost      = errors.New("页面在操作过程中断开且没有恢复,操作结果无法确认")
	errLost      = errors.New("controller lost")
)

// Key identifies whose page, on which run and node.
type Key struct {
	RunID  string
	NodeID string
	Owner  string
}

// Command is one page operation. Action is state|click|input|select|scroll.
type Command struct {
	Action string         `json:"action"`
	Args   map[string]any `json:"args,omitempty"`
}

// Result is what the page reported for a command.
type Result struct {
	OK    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	Note  string         `json:"note,omitempty"`
	State map[string]any `json:"state,omitempty"`
	// Unconfirmed means the page went away mid-command; State, when present,
	// is the page as it looked after it came back.
	Unconfirmed bool `json:"-"`
}

type outcome struct {
	res Result
	err error
}

type watchKey struct{ runID, owner string }

type watcher struct {
	send func([]byte) error
	last map[string]Status
}

// Hub tracks drawer connections and in-flight commands. The zero value is not
// usable; call NewHub.
type Hub struct {
	PauseWait      time.Duration
	ReconnectWait  time.Duration
	CommandTimeout time.Duration
	CallBudget     time.Duration

	mu       sync.Mutex
	conns    map[Key]map[*Conn]struct{}
	lost     map[Key]time.Time
	watchers map[watchKey]map[*watcher]struct{}
	wake     chan struct{}
	seq      uint64
	now      func() time.Time
}

func NewHub() *Hub {
	return &Hub{
		PauseWait:      DefaultPauseWait,
		ReconnectWait:  DefaultReconnectWait,
		CommandTimeout: DefaultCommandTimeout,
		CallBudget:     DefaultCallBudget,
		conns:          map[Key]map[*Conn]struct{}{},
		lost:           map[Key]time.Time{},
		watchers:       map[watchKey]map[*watcher]struct{}{},
		wake:           make(chan struct{}),
		now:            time.Now,
	}
}

// Conn is one drawer connection. send must be safe for concurrent use.
type Conn struct {
	hub      *Hub
	key      Key
	send     func([]byte) error
	on       bool
	visible  bool
	activeAt uint64
	closed   bool
	pending  map[string]chan outcome
	lostCh   chan struct{}
	sent     string
}

// Attach registers a drawer connection. It stays offline until SetControl.
func (h *Hub) Attach(key Key, send func([]byte) error) *Conn {
	c := &Conn{hub: h, key: key, send: send, pending: map[string]chan outcome{}, lostCh: make(chan struct{})}
	h.mu.Lock()
	if h.conns[key] == nil {
		h.conns[key] = map[*Conn]struct{}{}
	}
	h.conns[key][c] = struct{}{}
	h.mu.Unlock()
	return c
}

// SetControl records the drawer's toggle and tab visibility. The connection
// that most recently became on and visible is the one commands go to.
func (c *Conn) SetControl(on, visible bool) {
	h := c.hub
	h.mu.Lock()
	if c.closed {
		h.mu.Unlock()
		return
	}
	wasEligible := c.on && c.visible
	c.on, c.visible = on, visible
	if on && visible && !wasEligible {
		h.seq++
		c.activeAt = h.seq
	}
	var stopped []chan outcome
	if !on {
		for id, ch := range c.pending {
			stopped = append(stopped, ch)
			delete(c.pending, id)
		}
	}
	sends := h.changedLocked(c.key)
	h.mu.Unlock()
	for _, ch := range stopped {
		ch <- outcome{err: ErrStopped}
	}
	deliver(sends)
}

// Deliver hands a page_result frame to the waiting command, if any.
func (c *Conn) Deliver(id string, res Result) {
	h := c.hub
	h.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	h.mu.Unlock()
	if ch != nil {
		ch <- outcome{res: res}
	}
}

// Detach drops the connection. Commands in flight wait for a reconnect.
func (c *Conn) Detach() {
	h := c.hub
	h.mu.Lock()
	if c.closed {
		h.mu.Unlock()
		return
	}
	c.closed = true
	close(c.lostCh)
	c.pending = map[string]chan outcome{}
	if c.on {
		h.lost[c.key] = h.now()
	}
	if set := h.conns[c.key]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, c.key)
		}
	}
	for k, t := range h.lost {
		if h.now().Sub(t) > h.ReconnectWait {
			delete(h.lost, k)
		}
	}
	sends := h.changedLocked(c.key)
	h.mu.Unlock()
	deliver(sends)
}

// Watch streams status changes for every node of a run owned by owner (the
// logged-in workbench, where the same person may be chatting). It returns a
// func that stops watching.
func (h *Hub) Watch(runID, owner string, send func([]byte) error) func() {
	wk := watchKey{runID, owner}
	w := &watcher{send: send, last: map[string]Status{}}
	h.mu.Lock()
	if h.watchers[wk] == nil {
		h.watchers[wk] = map[*watcher]struct{}{}
	}
	h.watchers[wk][w] = struct{}{}
	var sends []pendingSend
	for key := range h.conns {
		if key.RunID != runID || key.Owner != owner {
			continue
		}
		st, _ := h.statusLocked(key)
		if st != StatusOffline {
			w.last[key.NodeID] = st
			sends = append(sends, pendingSend{send, stateFrame(key.NodeID, st, nil)})
		}
	}
	h.mu.Unlock()
	deliver(sends)
	return func() {
		h.mu.Lock()
		if set := h.watchers[wk]; set != nil {
			delete(set, w)
			if len(set) == 0 {
				delete(h.watchers, wk)
			}
		}
		h.mu.Unlock()
	}
}

// Status reports the owner's page state for a run node.
func (h *Hub) Status(key Key) Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, _ := h.statusLocked(key)
	return st
}

// Do runs one command on the owner's page. When the page is paused it waits
// up to PauseWait; when the page drops mid-command it waits up to
// ReconnectWait and returns the fresh state marked Unconfirmed. Commands are
// never replayed.
func (h *Hub) Do(ctx context.Context, key Key, cmd Command) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, h.CallBudget)
	defer cancel()
	c, err := h.waitActive(ctx, key)
	if err != nil {
		return Result{}, err
	}
	res, err := h.run(ctx, c, cmd)
	if !errors.Is(err, errLost) {
		return res, err
	}
	c2, err := h.waitActive(ctx, key)
	if err != nil {
		return Result{Unconfirmed: true}, ErrLost
	}
	st, err := h.run(ctx, c2, Command{Action: "state"})
	if err != nil {
		return Result{Unconfirmed: true}, ErrLost
	}
	st.Unconfirmed = true
	return st, nil
}

func (h *Hub) waitActive(ctx context.Context, key Key) (*Conn, error) {
	var pausedUntil time.Time
	for {
		h.mu.Lock()
		st, active := h.statusLocked(key)
		if active != nil {
			h.mu.Unlock()
			return active, nil
		}
		now := h.now()
		var until time.Time
		fail := ErrOffline
		if st == StatusPaused {
			if pausedUntil.IsZero() {
				pausedUntil = now.Add(h.PauseWait)
			}
			until, fail = pausedUntil, ErrPaused
		} else if t, ok := h.lost[key]; ok {
			until = t.Add(h.ReconnectWait)
		}
		wake := h.wake
		h.mu.Unlock()
		d := until.Sub(now)
		if d <= 0 {
			return nil, fail
		}
		timer := time.NewTimer(d)
		select {
		case <-wake:
			timer.Stop()
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctxErr(ctx, fail)
		}
	}
}

func (h *Hub) run(ctx context.Context, c *Conn, cmd Command) (Result, error) {
	h.mu.Lock()
	if c.closed {
		h.mu.Unlock()
		return Result{}, errLost
	}
	h.seq++
	id := "c" + strconv.FormatUint(h.seq, 10)
	ch := make(chan outcome, 1)
	c.pending[id] = ch
	lostCh := c.lostCh
	h.mu.Unlock()

	frame, _ := json.Marshal(map[string]any{"type": "page_cmd", "id": id, "action": cmd.Action, "args": cmd.Args})
	if err := c.send(frame); err != nil {
		h.drop(c, id)
		return Result{}, errLost
	}
	timer := time.NewTimer(h.CommandTimeout)
	defer timer.Stop()
	select {
	case o := <-ch:
		return o.res, o.err
	case <-lostCh:
		return Result{}, errLost
	case <-timer.C:
		h.abandon(c, id)
		return Result{}, ErrTimeout
	case <-ctx.Done():
		h.abandon(c, id)
		return Result{}, ctxErr(ctx, ErrTimeout)
	}
}

func ctxErr(ctx context.Context, onDeadline error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return onDeadline
	}
	return ErrCancelled
}

func (h *Hub) drop(c *Conn, id string) {
	h.mu.Lock()
	delete(c.pending, id)
	h.mu.Unlock()
}

// abandon forgets a command and tells the page to stop it.
func (h *Hub) abandon(c *Conn, id string) {
	h.drop(c, id)
	frame, _ := json.Marshal(map[string]any{"type": "page_cmd_cancel", "id": id})
	_ = c.send(frame)
}

func (h *Hub) statusLocked(key Key) (Status, *Conn) {
	var active *Conn
	anyOn := false
	for c := range h.conns[key] {
		if c.closed || !c.on {
			continue
		}
		anyOn = true
		if c.visible && (active == nil || c.activeAt > active.activeAt) {
			active = c
		}
	}
	switch {
	case active != nil:
		return StatusOnline, active
	case anyOn:
		return StatusPaused, nil
	default:
		return StatusOffline, nil
	}
}

type pendingSend struct {
	send  func([]byte) error
	frame []byte
}

func deliver(sends []pendingSend) {
	for _, s := range sends {
		_ = s.send(s.frame)
	}
}

// stateFrame builds a page_control_state frame. Drawers get active and no
// node id (public frames never carry ids); watchers get the node id.
func stateFrame(nodeID string, st Status, active *bool) []byte {
	m := map[string]any{"type": "page_control_state", "state": st}
	if nodeID != "" {
		m["nodeId"] = nodeID
	}
	if active != nil {
		m["active"] = *active
	}
	b, _ := json.Marshal(m)
	return b
}

// changedLocked wakes waiters and returns the state frames to send: each
// drawer learns whether it is the active one, watchers learn the status.
func (h *Hub) changedLocked(key Key) []pendingSend {
	close(h.wake)
	h.wake = make(chan struct{})
	st, active := h.statusLocked(key)
	var out []pendingSend
	for c := range h.conns[key] {
		isActive := c == active
		sig := string(st) + "|" + strconv.FormatBool(isActive)
		if c.sent == sig {
			continue
		}
		c.sent = sig
		out = append(out, pendingSend{c.send, stateFrame("", st, &isActive)})
	}
	for w := range h.watchers[watchKey{key.RunID, key.Owner}] {
		prev, seen := w.last[key.NodeID]
		if !seen {
			prev = StatusOffline
		}
		if prev == st {
			continue
		}
		w.last[key.NodeID] = st
		out = append(out, pendingSend{w.send, stateFrame(key.NodeID, st, nil)})
	}
	return out
}

// UserOwner is the owner id of a logged-in user.
func UserOwner(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return "user:" + username
}

// TokenOwner is the owner id of an anonymous credential (share link or
// drawer session): stable for the credential, without revealing it.
func TokenOwner(kind, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return kind + ":" + hex.EncodeToString(sum[:8])
}

// TurnSource reports who started the turn now running on a node, and a
// channel closed when that turn ends or is cancelled.
type TurnSource interface {
	ActivePageTurn(runID, nodeID string) (owner string, done <-chan struct{}, ok bool)
}

// Router resolves the turn owner and runs the command on their page.
type Router struct {
	Hub   *Hub
	Turns TurnSource
}

func (r *Router) Do(runID, nodeID string, cmd Command) (Result, error) {
	if r == nil || r.Hub == nil || r.Turns == nil {
		return Result{}, ErrOffline
	}
	owner, done, ok := r.Turns.ActivePageTurn(runID, nodeID)
	if !ok || owner == "" {
		return Result{}, ErrNoOwner
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if done != nil {
		go func() {
			select {
			case <-done:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return r.Hub.Do(ctx, Key{RunID: runID, NodeID: nodeID, Owner: owner}, cmd)
}
