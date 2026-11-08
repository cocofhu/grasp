package blob

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/objectstore"
)

func TestLocalCloseFencesLateUploadWithoutRemovingPublishedAttachment(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(ctx, strings.NewReader("retained"), Meta{})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { _, err := store.Put(ctx, reader, Meta{}); _ = reader.Close(); done <- err }()
	if _, err := writer.Write([]byte("unpublished")); err != nil {
		t.Fatal(err)
	}
	store.Close()
	_ = writer.Close()
	if err := <-done; !errors.Is(err, objectstore.ErrClosed) {
		t.Fatalf("late upload acknowledged: %v", err)
	}
	if err := store.Delete(ctx, ref); !errors.Is(err, objectstore.ErrClosed) {
		t.Fatalf("late delete admitted: %v", err)
	}
	if _, err := store.Put(ctx, strings.NewReader("late"), Meta{}); !errors.Is(err, objectstore.ErrClosed) {
		t.Fatal("new upload admitted after close")
	}
	body, _, err := store.Open(ctx, ref)
	if err != nil {
		t.Fatal("close destroyed readable source")
	}
	_ = body.Close()
}
