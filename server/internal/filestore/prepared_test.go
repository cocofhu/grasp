package filestore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
)

func TestPreparedPublicationIsStoreBoundAndReadsExactBytes(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	other, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	// Reference-looking user content must stay literal after a single resolve.
	original := filestore.Prefix + filestore.Bytes + "/" + strings.Repeat("a", 64)
	prepared, err := store.Prepare(context.Background(), strings.NewReader(original), filestore.Bytes)
	if err != nil || prepared.Size() != int64(len(original)) {
		t.Fatalf("publication=%+v %v", prepared, err)
	}
	ref, err := store.PreparedRef(prepared)
	if err != nil || ref != prepared.Ref() {
		t.Fatalf("prepared proof rejected: %s %v", ref, err)
	}
	if _, err := other.PreparedRef(prepared); err == nil {
		t.Fatal("another storage namespace accepted the proof")
	}
	if _, err := store.PreparedRef(filestore.Prepared{}); err == nil {
		t.Fatal("zero proof accepted")
	}
	got, err := store.Load(context.Background(), ref, filestore.Bytes)
	if err != nil || string(got) != original {
		t.Fatalf("body was interpreted as another reference: %q %v", got, err)
	}
	inline := "legacy\x00\xff bytes"
	got, err = store.Load(context.Background(), inline, filestore.Bytes)
	if err != nil || string(got) != inline {
		t.Fatalf("legacy bytes changed: %q %v", got, err)
	}
	if _, err := store.Load(context.Background(), ref, filestore.JSON); err == nil {
		t.Fatal("wrong owner encoding accepted")
	}
	if _, err := store.Load(context.Background(), filestore.Prefix+"broken", filestore.Bytes); err == nil {
		t.Fatal("malformed reference was treated as inline history")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(ctx, inline, filestore.Bytes); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read=%v", err)
	}
	_, digest, _ := filestore.Parse(ref)
	if err := os.WriteFile(filepath.Join(store.Root, "sha256", digest[:2], digest), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), ref, filestore.Bytes); err == nil {
		t.Fatal("corrupt local accepted bytes were returned")
	}
}

func TestPreparedFailedPublicationNeverProducesProof(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	want := errors.New("publication unavailable")
	if err := store.SetObjectStore(&objectBackend{err: want}); err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(context.Background(), strings.NewReader("accepted only after publication"), filestore.Bytes)
	if !errors.Is(err, want) || prepared.Ref() != "" {
		t.Fatalf("failed upload produced a reference: %+v %v", prepared, err)
	}
	if _, err := store.PreparedRef(prepared); err == nil {
		t.Fatal("failed publication produced a valid proof")
	}
}
