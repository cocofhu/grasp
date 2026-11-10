package objectstore

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type blockedPublication struct {
	Store
	started chan struct{}
	settled chan struct{}
}

func (s blockedPublication) Put(ctx context.Context, _ string, _ io.ReadSeeker, _ Info) error {
	close(s.started)
	<-ctx.Done()
	close(s.settled)
	return ctx.Err()
}

func TestPublicationCloseCancelsAndJoinsLateAttachmentUpload(t *testing.T) {
	backend := blockedPublication{started: make(chan struct{}), settled: make(chan struct{})}
	store := FencePublications(backend)
	done := make(chan error, 1)
	go func() {
		done <- store.Put(context.Background(), "attachment", strings.NewReader("bytes"), Info{Size: 5})
	}()
	<-backend.started
	store.Close()
	select {
	case <-backend.settled:
	default:
		t.Fatal("Close returned while an old upload can still publish")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight publication not canceled: %v", err)
	}
	if err := store.Put(context.Background(), "late", strings.NewReader(""), Info{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("late PUT admitted: %v", err)
	}
	if err := store.Delete(context.Background(), "late"); !errors.Is(err, ErrClosed) {
		t.Fatalf("late DELETE admitted: %v", err)
	}
	store.Close()
}
