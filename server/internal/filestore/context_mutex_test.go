package filestore_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
)

type stagedPayloadReader struct {
	started, release chan struct{}
	once             sync.Once
	reader           io.Reader
}

func (r *stagedPayloadReader) Read(body []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.release })
	return r.reader.Read(body)
}

func TestPublicationWaitHonorsShorterPrepareAndOpenDeadlines(t *testing.T) {
	for _, phase := range []string{"create_temp", "publish"} {
		t.Run(phase, func(t *testing.T) {
			store, err := filestore.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(store.Close)
			backend := &objectBackend{started: make(chan struct{})}
			if err := store.SetObjectStore(backend); err != nil {
				t.Fatal(err)
			}
			shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer shortCancel()
			shortDone := make(chan error, 1)
			staged := &stagedPayloadReader{started: make(chan struct{}), release: make(chan struct{}), reader: strings.NewReader("shorter deadline")}
			if phase == "publish" {
				go func() { _, err := store.Prepare(shortCtx, staged, filestore.Bytes); shortDone <- err }()
				<-staged.started
			}
			longDone := make(chan error, 1)
			go func() {
				_, err := store.Prepare(context.Background(), strings.NewReader("long-running publication"), filestore.Bytes)
				longDone <- err
			}()
			<-backend.started
			if phase == "create_temp" {
				go func() {
					_, err := store.Prepare(shortCtx, strings.NewReader("shorter deadline"), filestore.Bytes)
					shortDone <- err
				}()
			} else {
				close(staged.release)
			}
			select {
			case err := <-shortDone:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("prepare wait=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("another upload made Prepare exceed its context deadline")
			}
			readCtx, readCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer readCancel()
			if _, err := store.Load(readCtx, filestore.Prefix+filestore.Bytes+"/"+strings.Repeat("a", 64), filestore.Bytes); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("object lookup waited beyond its caller's deadline: %v", err)
			}
			closed := make(chan struct{})
			go func() { store.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("Close did not cancel/drain the active publication")
			}
			if err := <-longDone; !errors.Is(err, context.Canceled) {
				t.Fatalf("active publication survived Close: %v", err)
			}
			if prepared, err := store.Prepare(context.Background(), strings.NewReader("late"), filestore.Bytes); !errors.Is(err, filestore.ErrClosed) || prepared.Ref() != "" {
				t.Fatalf("closed namespace produced a prepared proof: %+v %v", prepared, err)
			}
		})
	}
}
