package objectstore

import (
	"context"
	"errors"
	"io"
	"sync"
)

var ErrClosed = errors.New("object storage publications are closed")

// Publications fences writes at process shutdown. In particular an attachment
// upload that outlives HTTP draining cannot publish in the old storage after
// the runtime unregisters and an offline migration takes its inventory.
type Publications struct {
	Store
	mu     sync.Mutex
	closed bool
	ctx    context.Context
	cancel context.CancelFunc
	writes sync.WaitGroup
}

func FencePublications(store Store) *Publications {
	ctx, cancel := context.WithCancel(context.Background())
	return &Publications{Store: store, ctx: ctx, cancel: cancel}
}

func (s *Publications) begin(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, ErrClosed
	}
	s.writes.Add(1)
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return child, func() { stop(); cancel(); s.writes.Done() }, nil
}

func (s *Publications) Put(ctx context.Context, key string, body io.ReadSeeker, info Info) error {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	return s.Store.Put(ctx, key, body, info)
}

func (s *Publications) Delete(ctx context.Context, key string) error {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	return s.Store.Delete(ctx, key)
}

func (s *Publications) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.writes.Wait()
}
