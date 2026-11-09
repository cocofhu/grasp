package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/objectstore"
)

type publicationObject struct {
	body []byte
	info objectstore.Info
}

type publicationBackend struct {
	objectstore.Store
	mu      sync.Mutex
	objects map[string]publicationObject
	before  func(context.Context, io.ReadSeeker, objectstore.Info) error
}

func (b *publicationBackend) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	if b.before != nil {
		if err := b.before(ctx, body, info); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = publicationObject{body: data, info: info}
	return nil
}

func (b *publicationBackend) Open(_ context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	stored, ok := b.objects[key]
	if !ok {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(stored.body)), stored.info, nil
}

func (b *publicationBackend) Head(_ context.Context, key string) (objectstore.Info, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	stored, ok := b.objects[key]
	if !ok {
		return objectstore.Info{}, os.ErrNotExist
	}
	return stored.info, nil
}

type zeroReaderAt struct{}

func (zeroReaderAt) ReadAt(p []byte, _ int64) (int, error) {
	clear(p)
	return len(p), nil
}

func TestLargeRemoteStreamDoesNotBlockIndependentPayloads(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	const archiveBytes int64 = 3 << 30
	started := make(chan struct{})
	backend := &publicationBackend{objects: map[string]publicationObject{}}
	backend.before = func(ctx context.Context, body io.ReadSeeker, info objectstore.Info) error {
		if info.Size < archiveBytes {
			return nil
		}
		// Exercise the actual network-publication phase with a seekable 3 GiB
		// stream and bounded memory. It deliberately never claims to complete
		// or prove this archive, so the fixture need not stage 3 GiB of zeros.
		if n, err := io.CopyN(io.Discard, body, 32<<10); err != nil || n != 32<<10 {
			return errors.New("large stream did not supply its first bounded block")
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	if err := store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		// This is the same atomic registration/Close barrier as Store.put.
		store.mu.Lock()
		store.publications.Add(1)
		store.mu.Unlock()
		defer store.publications.Done()
		done <- store.publishRemote(context.Background(), backend, "large-stream", io.NewSectionReader(zeroReaderAt{}, 0, archiveBytes), objectstore.Info{Size: archiveBytes})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("large network publication did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, strings.NewReader("a new Journal body"), Bytes)
	if err != nil {
		t.Fatalf("large stream blocked a small publication: %v", err)
	}
	verified, err := store.VerifyBounded(ctx, prepared.Ref(), Bytes, 1024)
	if err != nil || verified.Size() != prepared.Size() {
		t.Fatalf("large stream blocked bounded read-back: %+v %v", verified, err)
	}
	store.Close()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close did not stop the streaming archive: %v", err)
	}
	if _, err := store.PreparedRef(prepared); !errors.Is(err, ErrClosed) {
		t.Fatal("previous publication remained usable after Close")
	}
	if _, err := store.VerifiedRef(verified); !errors.Is(err, ErrClosed) {
		t.Fatal("previous verification remained usable after Close")
	}
}

func TestCloseDrainsAllRemotePublicationsAndFencesLateSuccess(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started, canceled := make(chan struct{}, 2), make(chan struct{}, 2)
	release := make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }); store.Close() })
	backend := &publicationBackend{objects: map[string]publicationObject{}}
	var block atomic.Bool
	backend.before = func(ctx context.Context, _ io.ReadSeeker, _ objectstore.Info) error {
		if block.Load() {
			started <- struct{}{}
			<-ctx.Done()
			canceled <- struct{}{}
			<-release // Model a backend that acknowledges cancellation late.
		}
		return nil
	}
	if err := store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	old, err := store.Prepare(context.Background(), strings.NewReader("already prepared"), Text)
	if err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	results := make(chan error, 2)
	for _, body := range []string{"pending-a", "pending-b"} {
		go func() {
			p, err := store.Prepare(context.Background(), strings.NewReader(body), Text)
			if p.Ref() != "" {
				err = errors.New("late backend success produced a proof")
			}
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("remote publications were serialized")
		}
	}
	closed := make(chan struct{}, 2)
	for range 2 {
		go func() { store.Close(); closed <- struct{}{} }()
	}
	for range 2 {
		select {
		case <-canceled:
		case <-time.After(time.Second):
			t.Fatal("Close did not cancel every active publisher")
		}
	}
	select {
	case <-closed:
		t.Fatal("Close returned before all backend publishers joined")
	default:
	}
	if _, err := store.PreparedRef(old); !errors.Is(err, ErrClosed) {
		t.Fatal("held proof passed validation during namespace shutdown")
	}
	unblock.Do(func() { close(release) })
	for range 2 {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("Close failed to join completed publishers")
		}
		if err := <-results; !errors.Is(err, ErrClosed) {
			t.Fatalf("late successful upload escaped shutdown fence: %v", err)
		}
	}
}

func TestArchivePublicationIsCanceledAndJoinedByClose(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if _, _, err := store.Put(context.Background(), strings.NewReader("legacy payload"), Text); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	backend := &publicationBackend{objects: map[string]publicationObject{}, before: func(ctx context.Context, _ io.ReadSeeker, _ objectstore.Info) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	if err := store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := store.ArchiveLocal(context.Background()); done <- err }()
	<-started
	store.Close()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy archive publication escaped shutdown: %v", err)
	}
}
