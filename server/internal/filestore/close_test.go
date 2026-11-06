package filestore_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
)

type heldSource struct {
	started chan struct{}
	release chan struct{}
	done    bool
}

func (r *heldSource) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	close(r.started)
	<-r.release
	r.done = true
	return copy(p, "late file body"), nil
}

func TestCloseRejectsLatePublicationWithoutWaitingForSource(t *testing.T) {
	root := t.TempDir() + "/files"
	store, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	source := &heldSource{started: make(chan struct{}), release: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, _, err := store.Put(context.Background(), source, filestore.Binary); result <- err }()
	<-source.started
	closed := make(chan struct{})
	go func() { store.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		close(source.release)
		t.Fatal("Close waited for an unresponsive source")
	}
	if err = os.RemoveAll(root); err != nil {
		close(source.release)
		t.Fatal(err)
	}
	close(source.release)
	if err = <-result; !errors.Is(err, filestore.ErrClosed) {
		t.Fatalf("late import=%v", err)
	}
	store.Close() // idempotent
	if _, _, err = store.Put(context.Background(), strings.NewReader("new"), filestore.Text); !errors.Is(err, filestore.ErrClosed) {
		t.Fatalf("new import=%v", err)
	}
	if _, err = os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed store recreated its directory: %v", err)
	}
}
