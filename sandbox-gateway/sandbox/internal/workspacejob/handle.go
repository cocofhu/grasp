package workspacejob

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type Handle struct {
	service    *Service
	id         string
	generation uint64
	deadline   time.Time
	identity   Identity
	ctx        context.Context
	cancel     context.CancelCauseFunc
	done       chan struct{}
	changed    chan struct{}
	until      atomic.Int64
}

// Check never waits on journal I/O. In particular a blocked fsync or HTTP
// renewal cannot extend work past the last acknowledgement.
func (h *Handle) Check() error {
	if h.ctx.Err() != nil {
		return context.Cause(h.ctx)
	}
	if h.service.closed.Load() || h.service.high.Load() != h.generation || !time.Unix(0, h.until.Load()).After(time.Now()) || !h.deadline.After(time.Now()) {
		return ErrFence
	}
	return nil
}

func (h *Handle) watch() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		deadline := time.Unix(0, h.until.Load())
		if h.deadline.Before(deadline) {
			deadline = h.deadline
		}
		timer.Reset(max(time.Until(deadline), 0))
		select {
		case <-h.ctx.Done():
			return
		case <-h.changed:
			continue
		case <-timer.C:
			if h.Check() != nil {
				h.cancel(ErrFence)
				return
			}
		}
	}
}

func safeName(name string) bool {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || path.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// WriteFile is limited to this job's private staging area. Each chunk and the
// final fsync are fenced; partial files are retained and never reused. Collector
// and restorer packages must use equivalent confined, fenced operations for
// their explicitly granted roots, not arbitrary host-path callbacks.
func (h *Handle) WriteFile(name string, source io.Reader, limit int64) error {
	if !safeName(name) || limit < 1 {
		return ErrInvalid
	}
	s := h.service
	s.mu.Lock()
	if err := h.Check(); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := s.root.Mkdir("data/"+h.id, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		s.mu.Unlock()
		return err
	}
	root, err := s.root.OpenRoot("data/" + h.id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(path.Dir(name), 0700); err != nil {
		s.mu.Unlock()
		return err
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r := &io.LimitedReader{R: source, N: limit + 1}
	buf := make([]byte, 32<<10)
	var total int64
	for {
		if err := h.Check(); err != nil {
			return err
		}
		n, readErr := r.Read(buf)
		if int64(n) > limit-total {
			return ErrLimit
		}
		if n > 0 {
			s.mu.Lock()
			err = h.Check()
			if err == nil {
				_, err = f.Write(buf[:n])
				total += int64(n)
			}
			if err == nil {
				err = h.Check()
			}
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := h.Check(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return h.Check()
}

// Command is for trusted registered executors, never a browser argv. A small
// fixed supervisor also kills its group when its runtime parent disappears;
// Git packing/fetch children cannot outlive a runtime crash or lease deadline.
func (h *Handle) Command(dir string, stdout, stderr io.Writer, argv ...string) error {
	return h.Run(Process{Dir: dir, Stdout: stdout, Stderr: stderr, Argv: argv})
}
