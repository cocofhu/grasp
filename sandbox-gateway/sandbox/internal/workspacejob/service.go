package workspacejob

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Service struct {
	mu      sync.Mutex
	config  Config
	root    *os.Root
	journal *os.Root
	lock    *os.File
	state   checkpoint
	high    atomic.Uint64
	closed  atomic.Bool
	jobs    map[string]*record
	live    map[string]*Handle
	active  *Handle
	// Tests can hold a disk publication without weakening the independent
	// expiry watchdog. Production always uses the fsynced journal writer.
	write func(*os.Root, string, any) error
}

func New(cfg Config) (*Service, error) {
	if cfg.Root == "" || !idPattern.MatchString(cfg.Resource) || !idPattern.MatchString(cfg.Lineage) {
		return nil, ErrInvalid
	}
	if cfg.MaxLease <= 0 {
		cfg.MaxLease = 45 * time.Second
	}
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = 30 * time.Minute
	}
	if cfg.MaxJobs <= 0 {
		cfg.MaxJobs = 10000
	}
	executors := map[string]Executor{}
	for kind, executor := range cfg.Executors {
		if !kindPattern.MatchString(kind) || executor == nil {
			return nil, ErrInvalid
		}
		executors[kind] = executor
	}
	cfg.Executors = executors
	root, lock, err := openJournal(cfg)
	if err != nil {
		return nil, err
	}
	s := &Service{config: cfg, root: root, lock: lock, jobs: map[string]*record{}, live: map[string]*Handle{}, write: writeRecord}
	cleanup := func(err error) (*Service, error) { _ = lock.Close(); _ = root.Close(); return nil, err }
	err = readRecord(root, "state.json", &s.state)
	if errors.Is(err, os.ErrNotExist) {
		s.state = checkpoint{Version: Version, Resource: cfg.Resource, Lineage: cfg.Lineage}
		err = s.write(root, "state.json", s.state)
	}
	if err != nil {
		return cleanup(err)
	}
	if s.state.Version != Version || s.state.Resource != cfg.Resource || s.state.Lineage != cfg.Lineage {
		return cleanup(ErrLineage)
	}
	s.high.Store(s.state.Generation)
	for _, name := range []string{"jobs", "data"} {
		if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return cleanup(err)
		}
	}
	s.journal, err = root.OpenRoot("jobs")
	if err != nil {
		return cleanup(err)
	}
	dir, err := s.journal.Open(".")
	if err != nil {
		_ = s.journal.Close()
		return cleanup(err)
	}
	entries, err := dir.ReadDir(cfg.MaxJobs + 1)
	_ = dir.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		_ = s.journal.Close()
		return cleanup(err)
	}
	if len(entries) > cfg.MaxJobs {
		_ = s.journal.Close()
		return cleanup(ErrLimit)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending-") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		var row record
		if !idPattern.MatchString(id) || entry.Name() != id+".json" || readRecord(s.journal, entry.Name(), &row) != nil {
			_ = s.journal.Close()
			return cleanup(ErrCorrupt)
		}
		_, hash, err := Fingerprint(row.Spec)
		if err != nil || row.Spec.ID != id || row.Receipt.SHA256 != hash || row.Receipt.Lineage != cfg.Lineage || row.Receipt.Resource != cfg.Resource || row.Receipt.Generation > s.state.Generation {
			_ = s.journal.Close()
			return cleanup(ErrCorrupt)
		}
		if !terminal(row.Receipt.State) {
			row.Receipt.State, row.Receipt.Error = "unknown", "runtime_restarted"
			if err := s.write(s.journal, entry.Name(), row); err != nil {
				_ = s.journal.Close()
				return cleanup(err)
			}
		}
		s.jobs[id] = &row
	}
	return s, nil
}

func (s *Service) Capabilities() Capabilities {
	kinds := []string{}
	for kind := range s.config.Executors {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return Capabilities{Version: Version, Resource: s.config.Resource, Lineage: s.config.Lineage, RenewableLease: true, PersistentGeneration: true, Kinds: kinds, MaxLeaseMillis: s.config.MaxLease.Milliseconds(), MaxDurationMillis: s.config.MaxDuration.Milliseconds()}
}

func copyReceipt(row Receipt) Receipt {
	row.Result = append(json.RawMessage(nil), row.Result...)
	return row
}

func (s *Service) Get(id string) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return Receipt{}, ErrClosed
	}
	row := s.jobs[id]
	if row == nil {
		return Receipt{}, os.ErrNotExist
	}
	if live := s.live[id]; live != nil && live.Check() != nil && !terminal(row.Receipt.State) && row.Receipt.State != "cancelling" {
		s.unknownLocked(row, "lease_expired")
	}
	return copyReceipt(row.Receipt), nil
}

func (s *Service) validateFence(spec Spec, f Fence) error {
	now := time.Now()
	if !idPattern.MatchString(f.Owner) || f.Generation == 0 || !f.ExpiresAt.After(now) || f.ExpiresAt.After(now.Add(s.config.MaxLease)) || f.ExpiresAt.After(spec.Deadline) {
		return ErrFence
	}
	return nil
}

func (s *Service) Create(req CreateRequest) (Receipt, error) {
	spec, hash, err := Fingerprint(req.Spec)
	if err != nil || hash != req.SHA256 {
		return Receipt{}, ErrInvalid
	}
	if spec.Resource != s.config.Resource || spec.Lineage != s.config.Lineage {
		return Receipt{}, ErrLineage
	}
	executor := s.config.Executors[spec.Kind]
	if executor == nil {
		return Receipt{}, ErrUnsupported
	}
	if !spec.Deadline.After(time.Now()) || spec.Deadline.After(time.Now().Add(s.config.MaxDuration)) {
		return Receipt{}, ErrInvalid
	}
	if err := s.validateFence(spec, req.Fence); err != nil {
		return Receipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return Receipt{}, ErrClosed
	}
	if old := s.jobs[spec.ID]; old != nil {
		if old.Receipt.SHA256 != hash || old.OwnerHash != ownerHash(req.Fence.Owner) || old.Receipt.Generation != req.Fence.Generation {
			return Receipt{}, ErrConflict
		}
		if terminal(old.Receipt.State) {
			return copyReceipt(old.Receipt), nil
		}
		if live := s.live[spec.ID]; live == nil || live.Check() != nil {
			s.unknownLocked(old, "lease_expired")
			return copyReceipt(old.Receipt), ErrUnknown
		}
		return copyReceipt(old.Receipt), nil // idempotence is never renewal
	}
	if len(s.jobs) >= s.config.MaxJobs {
		return Receipt{}, ErrLimit
	}
	if req.Fence.Generation <= s.state.Generation {
		return Receipt{}, ErrFence
	}
	next := s.state
	next.Generation = req.Fence.Generation
	if err := s.write(s.root, "state.json", next); err != nil {
		return Receipt{}, err
	}
	s.state = next
	s.high.Store(next.Generation)
	var predecessor <-chan struct{}
	if s.active != nil {
		predecessor = s.active.done
		s.active.cancel(ErrFence)
		if old := s.jobs[s.active.id]; old != nil && !terminal(old.Receipt.State) {
			s.unknownLocked(old, "superseded")
		}
	}
	now := time.Now().UTC()
	row := &record{Spec: spec, OwnerHash: ownerHash(req.Fence.Owner), Receipt: Receipt{Version: Version, ID: spec.ID, Kind: spec.Kind, Resource: spec.Resource, Lineage: spec.Lineage, SHA256: hash, Generation: req.Fence.Generation, ExpiresAt: req.Fence.ExpiresAt.UTC(), Deadline: spec.Deadline, State: "accepted", CreatedAt: now}}
	if err := s.write(s.journal, spec.ID+".json", row); err != nil {
		return Receipt{}, err
	}
	if !req.Fence.ExpiresAt.After(time.Now()) {
		s.jobs[spec.ID] = row
		s.unknownLocked(row, "lease_expired_before_start")
		return copyReceipt(row.Receipt), ErrFence
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	h := &Handle{service: s, id: spec.ID, generation: req.Fence.Generation, deadline: spec.Deadline, ctx: ctx, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{}, 1)}
	h.until.Store(req.Fence.ExpiresAt.UnixNano())
	s.jobs[spec.ID], s.live[spec.ID], s.active = row, h, h
	go h.watch()
	go s.execute(h, predecessor, executor, spec.Body)
	return copyReceipt(row.Receipt), nil
}

func (s *Service) unknownLocked(row *record, code string) {
	row.Receipt.State, row.Receipt.Error = "unknown", code
	row.Receipt.Result = nil
	_ = s.write(s.journal, row.Spec.ID+".json", row)
}

func (s *Service) Renew(id string, req RenewRequest) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, h, err := s.ownedLocked(id, req)
	if err != nil {
		return Receipt{}, err
	}
	if err := s.validateFence(row.Spec, req.Fence); err != nil {
		return Receipt{}, err
	}
	if !req.Fence.ExpiresAt.After(row.Receipt.ExpiresAt) {
		return copyReceipt(row.Receipt), nil
	}
	previous := row.Receipt.ExpiresAt
	row.Receipt.ExpiresAt = req.Fence.ExpiresAt.UTC()
	if err := s.write(s.journal, id+".json", row); err != nil {
		row.Receipt.ExpiresAt = previous
		return Receipt{}, err
	}
	// Disk I/O cannot keep native work alive: watch() uses its last ack atomically
	// without this mutex. A late fsync response must not revive that worker.
	if h.ctx.Err() != nil || !previous.After(time.Now()) {
		h.cancel(ErrFence)
		s.unknownLocked(row, "lease_expired")
		return Receipt{}, ErrFence
	}
	h.until.Store(req.Fence.ExpiresAt.UnixNano())
	select {
	case h.changed <- struct{}{}:
	default:
	}
	if h.ctx.Err() != nil {
		s.unknownLocked(row, "lease_expired")
		return Receipt{}, ErrFence
	}
	return copyReceipt(row.Receipt), nil
}

func (s *Service) ownedLocked(id string, req RenewRequest) (*record, *Handle, error) {
	if s.closed.Load() {
		return nil, nil, ErrClosed
	}
	row, h := s.jobs[id], s.live[id]
	if row == nil {
		return nil, nil, os.ErrNotExist
	}
	if req.Lineage != s.config.Lineage {
		return nil, nil, ErrLineage
	}
	if row.Receipt.SHA256 != req.SHA256 || row.OwnerHash != ownerHash(req.Fence.Owner) || req.Fence.Generation != row.Receipt.Generation {
		return nil, nil, ErrConflict
	}
	if h == nil || h.Check() != nil || terminal(row.Receipt.State) || row.Receipt.State == "cancelling" {
		return nil, nil, ErrFence
	}
	return row, h, nil
}

func (s *Service) Cancel(id string, req RenewRequest) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, h, err := s.ownedLocked(id, req)
	if err != nil {
		return Receipt{}, err
	}
	h.cancel(ErrCancelled)
	row.Receipt.State = "cancelling"
	if err := s.write(s.journal, id+".json", row); err != nil {
		return Receipt{}, err
	}
	return copyReceipt(row.Receipt), nil
}

func (s *Service) execute(h *Handle, predecessor <-chan struct{}, executor Executor, body json.RawMessage) {
	defer close(h.done)
	defer h.cancel(context.Canceled)
	if predecessor != nil {
		select {
		case <-predecessor:
		case <-h.ctx.Done():
			s.finish(h, nil, context.Cause(h.ctx))
			return
		}
	}
	s.mu.Lock()
	row := s.jobs[h.id]
	err := h.Check()
	if err == nil {
		row.Receipt.State = "running"
		err = s.write(s.journal, h.id+".json", row)
	}
	s.mu.Unlock()
	if err != nil {
		s.finish(h, nil, err)
		return
	}
	result, err := callExecutor(executor, h, body)
	s.finish(h, result, err)
}

func callExecutor(executor Executor, h *Handle, body json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			result, err = nil, errors.New("workspace executor panicked")
		}
	}()
	return executor(h.ctx, h, append(json.RawMessage(nil), body...))
}

func (s *Service) finish(h *Handle, result json.RawMessage, executionErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.jobs[h.id]
	if row == nil || terminal(row.Receipt.State) {
		return
	}
	now := time.Now().UTC()
	if errors.Is(context.Cause(h.ctx), ErrCancelled) && s.high.Load() == h.generation {
		row.Receipt.State, row.Receipt.Error = "cancelled", "cancelled"
	} else if h.Check() != nil {
		s.unknownLocked(row, "ownership_lost")
		return
	} else if executionErr != nil {
		row.Receipt.State, row.Receipt.Error = "failed", "executor_failed"
	} else {
		if len(result) > 64<<10 || (len(result) > 0 && !json.Valid(result)) {
			row.Receipt.State, row.Receipt.Error = "failed", "invalid_executor_result"
		} else {
			row.Receipt.State, row.Receipt.Result = "completed", append(json.RawMessage(nil), result...)
		}
	}
	row.Receipt.FinishedAt = &now
	if err := s.write(s.journal, h.id+".json", row); err != nil {
		h.cancel(err)
		s.unknownLocked(row, "publication_failed")
		return
	}
	if row.Receipt.State == "completed" && h.Check() != nil {
		s.unknownLocked(row, "ownership_lost")
	}
}

func (s *Service) Close(ctx context.Context) error {
	s.closed.Store(true)
	s.mu.Lock()
	wait := make([]<-chan struct{}, 0, len(s.live))
	for _, h := range s.live {
		h.cancel(ErrClosed)
		wait = append(wait, h.done)
	}
	s.mu.Unlock()
	for _, done := range wait {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return nil
	}
	err := errors.Join(s.journal.Close(), syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN), s.lock.Close(), s.root.Close())
	s.lock = nil
	return err
}
