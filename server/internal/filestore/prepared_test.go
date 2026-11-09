package filestore_test

import (
	"context"
	"errors"
	"math"
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
	if _, err = store.Load(context.Background(), inline, filestore.Bytes); !errors.Is(err, filestore.ErrNonCanonicalPayload) {
		t.Fatalf("runtime accepted unconverted bytes: %v", err)
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

func TestPreparedBoundedLoadRejectsInlineAndLimitsVerifiedReferences(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	ctx := context.Background()
	body := "exact bytes"
	prepared, err := store.Prepare(ctx, strings.NewReader(body), filestore.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadBounded(ctx, body, filestore.Bytes, 1024); !errors.Is(err, filestore.ErrNonCanonicalPayload) {
		t.Fatalf("bounded runtime loader accepted inline bytes: %v", err)
	}
	for _, raw := range []string{prepared.Ref()} {
		if _, err := store.LoadBounded(ctx, raw, filestore.Bytes, int64(len(body)-1)); err == nil {
			t.Fatal("oversize body accepted")
		}
		got, err := store.LoadBounded(ctx, raw, filestore.Bytes, int64(len(body)))
		if err != nil || string(got) != body {
			t.Fatalf("exact boundary body changed: %q %v", got, err)
		}
		for _, limit := range []int64{-1, 0, math.MaxInt64} {
			if _, err := store.LoadBounded(ctx, raw, filestore.Bytes, limit); err == nil {
				t.Fatal("invalid size bound accepted")
			}
		}
	}
	_, digest, _ := filestore.Parse(prepared.Ref())
	if err := os.WriteFile(filepath.Join(store.Root, "sha256", digest[:2], digest), []byte("wrong bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadBounded(ctx, prepared.Ref(), filestore.Bytes, 1024); err == nil {
		t.Fatal("bounded read skipped local checksum validation")
	}
	if _, err := store.LoadBounded(ctx, prepared.Ref(), filestore.JSON, 1024); err == nil {
		t.Fatal("bounded read skipped owner encoding validation")
	}
}

func TestRuntimeLoadAcceptsOnlyDefinedEmptyValuesWithoutObjectAccess(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.SetObjectStore(&objectBackend{err: errors.New("must not access storage")}); err != nil {
		t.Fatal(err)
	}
	for _, encoding := range []string{filestore.Text, filestore.JSON, filestore.Bytes, filestore.Image, filestore.Binary} {
		got, err := store.Load(t.Context(), "", encoding)
		if err != nil || len(got) != 0 {
			t.Fatalf("empty %s=%q %v", encoding, got, err)
		}
		got, err = store.Load(t.Context(), "null", encoding)
		if encoding == filestore.JSON {
			if err != nil || string(got) != "null" {
				t.Fatal(got, err)
			}
		} else if !errors.Is(err, filestore.ErrNonCanonicalPayload) {
			t.Fatalf("literal null text/bytes accepted without a reference: %s %v", encoding, err)
		}
	}
	if _, err := store.Load(t.Context(), "", "unknown"); err == nil {
		t.Fatal("undefined owner accepted an empty payload")
	}
}
