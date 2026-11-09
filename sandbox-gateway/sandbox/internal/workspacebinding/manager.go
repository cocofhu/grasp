package workspacebinding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"backend/internal/workspacejob"
)

var nativeConsumers = []string{"agent", "capture", "code"}

type Manager struct {
	mu        sync.Mutex
	config    Config
	root      *os.Root
	lock      *os.File
	state     checkpoint
	consumers []Consumer
	installed bool
	readers   int
	switching bool
	closed    bool
	write     func(*os.Root, checkpoint) error
}

func workspaceLimit() error { return workspacejob.ErrLimit }

func New(cfg Config) (*Manager, error) {
	if !idPattern.MatchString(cfg.Resource) || !idPattern.MatchString(cfg.Lineage) || cfg.ResolveRestore == nil {
		return nil, ErrInvalid
	}
	if cfg.MaxOperations == 0 {
		cfg.MaxOperations = 4096
	}
	if cfg.MaxOperations < 1 || cfg.MaxOperations > 4096 {
		return nil, ErrInvalid
	}
	original, device, inode, err := realDirectory(cfg.OriginalRoot, false)
	if err != nil {
		return nil, err
	}
	restore, _, _, err := realDirectory(cfg.RestoreRoot, true)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cfg.ControlRoot) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(cfg.ControlRoot, 0700); err != nil {
		return nil, err
	}
	control, _, _, err := realDirectory(cfg.ControlRoot, true)
	if err != nil {
		return nil, err
	}
	if overlap(original, restore) || overlap(original, control) || overlap(restore, control) {
		return nil, ErrInvalid
	}
	cfg.OriginalRoot, cfg.RestoreRoot, cfg.ControlRoot = original, restore, control
	root, err := os.OpenRoot(control)
	if err != nil {
		return nil, err
	}
	lock, err := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err == nil {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		if lock != nil {
			_ = lock.Close()
		}
		_ = root.Close()
		return nil, workspacejob.ErrInUse
	}
	m := &Manager{config: cfg, root: root, lock: lock, write: writeCheckpoint}
	fail := func(err error) (*Manager, error) { _ = m.Close(); return nil, err }
	m.state, err = readCheckpoint(root)
	if errors.Is(err, os.ErrNotExist) {
		head := initialHead(cfg.Resource, cfg.Lineage)
		m.state = checkpoint{Version: Version, Resource: cfg.Resource, Lineage: cfg.Lineage, OriginalRoot: original, RestoreRoot: restore, HeadID: head.Reference.ID, Bindings: map[string]retained{head.Reference.ID: {Head: head, Device: device, Inode: inode}}, Operations: map[string]Operation{}}
		err = m.write(root, m.state)
	}
	if err != nil {
		return fail(err)
	}
	if err := m.validate(); err != nil {
		return fail(err)
	}
	return m, nil
}

func (m *Manager) validate() error {
	s := m.state
	if s.Version != Version || s.Resource != m.config.Resource || s.Lineage != m.config.Lineage || s.OriginalRoot != m.config.OriginalRoot || s.RestoreRoot != m.config.RestoreRoot || len(s.Bindings) == 0 || len(s.Operations) > m.config.MaxOperations {
		return ErrCorrupt
	}
	for id, row := range s.Bindings {
		h := row.Head
		if id != h.Reference.ID || !validReference(h.Reference) || h.Version != Version || h.Resource != s.Resource || h.Lineage != s.Lineage || h.Reference.Fingerprint != fingerprint(h) || row.Inode == 0 {
			return ErrCorrupt
		}
		if (h.Target.Kind != "original" || h.Target.ID != "") && (h.Target.Kind != "restore" || !idPattern.MatchString(h.Target.ID)) {
			return ErrCorrupt
		}
	}
	if _, ok := s.Bindings[s.HeadID]; !ok {
		return ErrCorrupt
	}
	for id, op := range s.Operations {
		if id != op.Identity.ID || !idPattern.MatchString(id) || op.Version != Version || op.Identity.Resource != s.Resource || op.Identity.Lineage != s.Lineage || op.Identity.Generation == 0 || !hashPattern.MatchString(op.Identity.SHA256) || (op.Identity.Kind != "activate_workspace" && op.Identity.Kind != "return_workspace") || op.Identity.Deadline.IsZero() || (op.State != "unknown" && op.State != "committed") {
			return ErrCorrupt
		}
		if op.State == "committed" {
			if op.Result == nil || op.CompletedAt == nil || op.Result.OperationID != id || op.Result.Version != Version || op.Result.Active.Reference.Fingerprint != fingerprint(op.Result.Active) || op.Result.Previous.Reference.Fingerprint != fingerprint(op.Result.Previous) || !slices.Equal(op.Result.Consumers, nativeConsumers) {
				return ErrCorrupt
			}
		} else if s.PendingID != id || op.Result != nil {
			return ErrCorrupt
		}
	}
	if s.PendingID != "" {
		if op, ok := s.Operations[s.PendingID]; !ok || op.State != "unknown" {
			return ErrCorrupt
		}
	}
	return nil
}

// Install occurs before any default Agent is started. It is deliberately
// required: a journal-only manager cannot advertise activation capability.
func (m *Manager) Install(ctx context.Context, consumers ...Consumer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.installed || m.switching || m.readers != 0 {
		return ErrConflict
	}
	byName := make(map[string]Consumer, len(consumers))
	for _, c := range consumers {
		if c == nil || byName[c.Name()] != nil {
			return ErrInvalid
		}
		byName[c.Name()] = c
	}
	if len(byName) != len(nativeConsumers) {
		return ErrUninstalled
	}
	ordered := make([]Consumer, 0, len(nativeConsumers))
	for _, name := range nativeConsumers {
		c := byName[name]
		if c == nil {
			return ErrUninstalled
		}
		ordered = append(ordered, c)
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	m.consumers, m.installed = ordered, true
	return nil
}

func (m *Manager) Capabilities() Capabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	enabled := m.installed && !m.closed
	return Capabilities{Version: Version, Resource: m.config.Resource, Lineage: m.config.Lineage, Activate: enabled, Return: enabled, DurableHead: true, Consumers: append([]string(nil), nativeConsumers...), PortOwnership: true}
}

func (m *Manager) path(row retained) (string, error) {
	name := m.config.OriginalRoot
	if row.Head.Target.Kind == "restore" {
		name = filepath.Join(m.config.RestoreRoot, row.Head.Target.ID)
	}
	actual, device, inode, err := realDirectory(name, false)
	if err != nil {
		return "", err
	}
	if device != row.Device || inode != row.Inode {
		return "", ErrConflict
	}
	return actual, nil
}

func (m *Manager) Head() (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, ErrClosed
	}
	row := m.state.Bindings[m.state.HeadID]
	view := View{Head: row.Head, PendingOperationID: m.state.PendingID, Ready: m.installed && !m.switching && m.state.PendingID == ""}
	if view.Ready {
		var err error
		view.Path, err = m.path(row)
		if err != nil {
			return View{}, err
		}
	}
	return view, nil
}

// Acquire pins the canonical root until the operation has finished. Callers
// cannot change the root through this read. Capture requires a frozen expected
// identity once any explicit switch has occurred.
func (m *Manager) Acquire(expected *Reference, capture bool) (View, func(), error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return View{}, nil, ErrClosed
	}
	if !m.installed {
		m.mu.Unlock()
		return View{}, nil, ErrUninstalled
	}
	if m.switching || m.state.PendingID != "" {
		m.mu.Unlock()
		return View{}, nil, ErrUnknown
	}
	row := m.state.Bindings[m.state.HeadID]
	if (expected != nil && *expected != row.Head.Reference) || (capture && expected == nil && row.Head.Reference.Revision != 1) {
		m.mu.Unlock()
		return View{}, nil, ErrConflict
	}
	name, err := m.path(row)
	if err != nil {
		m.mu.Unlock()
		return View{}, nil, err
	}
	m.readers++
	view := View{Head: row.Head, Path: name, Ready: true}
	m.mu.Unlock()
	var once sync.Once
	return view, func() { once.Do(func() { m.mu.Lock(); m.readers--; m.mu.Unlock() }) }, nil
}

func (m *Manager) Operation(id string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Operation{}, ErrClosed
	}
	op, ok := m.state.Operations[id]
	if !ok {
		return Operation{}, os.ErrNotExist
	}
	return copyOperation(op), nil
}

func decodeSpec(body json.RawMessage) (Spec, error) {
	var spec Spec
	if len(body) > 64<<10 {
		return spec, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&spec); err != nil {
		return spec, ErrInvalid
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return spec, ErrInvalid
	}
	if spec.Version != Version || !validReference(spec.Expected) || !idPattern.MatchString(spec.BindingID) || !idPattern.MatchString(spec.ReturnSnapshotID) || !hashPattern.MatchString(spec.Authorization) || ((spec.Target.Kind != "restore" && spec.Target.Kind != "binding") || !idPattern.MatchString(spec.Target.ID)) {
		return spec, ErrInvalid
	}
	return spec, nil
}

func Validate(body json.RawMessage) error { _, err := decodeSpec(body); return err }

func (m *Manager) Execute(ctx context.Context, h *workspacejob.Handle, body json.RawMessage) (json.RawMessage, error) {
	spec, err := decodeSpec(body)
	if err != nil {
		return nil, err
	}
	identity := h.Identity()
	if identity.Resource != m.config.Resource || identity.Lineage != m.config.Lineage || (identity.Kind != "activate_workspace" && identity.Kind != "return_workspace") {
		return nil, ErrInvalid
	}
	if (identity.Kind == "activate_workspace" && spec.Target.Kind != "restore") || (identity.Kind == "return_workspace" && spec.Target.Kind != "binding") {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	if prior, ok := m.state.Operations[identity.ID]; ok {
		m.mu.Unlock()
		if prior.Identity != identity {
			return nil, ErrConflict
		}
		if prior.State != "committed" || prior.Result == nil {
			return nil, ErrUnknown
		}
		return json.Marshal(prior.Result)
	}
	if m.closed || !m.installed {
		m.mu.Unlock()
		return nil, ErrUninstalled
	}
	if m.switching || m.readers != 0 {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	if m.state.PendingID != "" {
		m.mu.Unlock()
		return nil, ErrUnknown
	}
	previous := m.state.Bindings[m.state.HeadID]
	if previous.Head.Reference != spec.Expected {
		m.mu.Unlock()
		return nil, ErrConflict
	}
	if _, exists := m.state.Bindings[spec.BindingID]; exists || len(m.state.Operations) >= m.config.MaxOperations {
		m.mu.Unlock()
		return nil, ErrConflict
	}
	m.switching = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.switching = false; m.mu.Unlock() }()
	oldPath, err := m.path(previous)
	if err != nil {
		return nil, err
	}
	var target retained
	if spec.Target.Kind == "binding" {
		m.mu.Lock()
		var exists bool
		target, exists = m.state.Bindings[spec.Target.ID]
		m.mu.Unlock()
		if !exists {
			return nil, ErrConflict
		}
	} else {
		if err := m.config.ResolveRestore(ctx, spec.Target.ID); err != nil {
			return nil, err
		}
		name := filepath.Join(m.config.RestoreRoot, spec.Target.ID)
		_, device, inode, err := realDirectory(name, false)
		if err != nil {
			return nil, err
		}
		target = retained{Head: Head{Target: Selector{Kind: "restore", ID: spec.Target.ID}}, Device: device, Inode: inode}
	}
	newPath, err := m.path(target)
	if err != nil {
		return nil, err
	}
	if oldPath == newPath {
		return nil, ErrConflict
	}
	transitions := make([]Transition, 0, len(m.consumers))
	defer func() {
		for i := len(transitions) - 1; i >= 0; i-- {
			_ = transitions[i].Close()
		}
	}()
	for _, consumer := range m.consumers {
		if err := h.Check(); err != nil {
			return nil, err
		}
		transition, err := consumer.Prepare(ctx, oldPath, newPath)
		if err != nil {
			return nil, err
		}
		if transition == nil {
			return nil, ErrUninstalled
		}
		transitions = append(transitions, transition)
	}
	now := time.Now().UTC()
	op := Operation{Version: Version, Identity: identity, State: "unknown", CreatedAt: now}
	m.mu.Lock()
	next := cloneCheckpoint(m.state)
	next.PendingID = identity.ID
	next.Operations[identity.ID] = op
	err = h.Guard(func() error {
		if err := m.write(m.root, next); err != nil {
			return err
		}
		m.state = next
		return nil
	})
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, transition := range transitions {
		if err := h.Check(); err != nil {
			return nil, err
		}
		if err := transition.Apply(ctx); err != nil {
			return nil, err
		}
		if err := transition.Verify(ctx); err != nil {
			return nil, err
		}
	}
	// Recheck the actual inode after consumer work. Replacing a target path
	// cannot turn a verified materialization into a different live workspace.
	if _, err := m.path(target); err != nil {
		return nil, err
	}
	head := Head{Version: Version, Resource: identity.Resource, Lineage: identity.Lineage, Reference: Reference{ID: spec.BindingID, Revision: previous.Head.Reference.Revision + 1}, Target: target.Head.Target, ParentID: previous.Head.Reference.ID}
	head.Reference.Fingerprint = fingerprint(head)
	target.Head = head
	result := Result{Version: Version, OperationID: identity.ID, Previous: previous.Head, Active: head, ReturnSnapshotID: spec.ReturnSnapshotID, Consumers: append([]string(nil), nativeConsumers...)}
	finished := time.Now().UTC()
	op.State, op.Result, op.CompletedAt = "committed", &result, &finished
	m.mu.Lock()
	next = cloneCheckpoint(m.state)
	next.HeadID, next.PendingID = head.Reference.ID, ""
	next.Bindings[head.Reference.ID] = target
	next.Operations[identity.ID] = op
	err = h.Guard(func() error {
		if err := m.write(m.root, next); err != nil {
			return err
		}
		m.state = next
		return nil
	})
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	if m.switching || m.readers != 0 {
		return ErrBusy
	}
	m.closed = true
	return errors.Join(syscall.Flock(int(m.lock.Fd()), syscall.LOCK_UN), m.lock.Close(), m.root.Close())
}
