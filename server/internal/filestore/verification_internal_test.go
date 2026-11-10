package filestore

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/objectstore"
)

type boundedReadBody struct {
	io.Reader
	reads, closes *atomic.Int32
}

func (b boundedReadBody) Read(p []byte) (int, error) { b.reads.Add(1); return b.Reader.Read(p) }
func (b boundedReadBody) Close() error               { b.closes.Add(1); return nil }

type boundedReadBackend struct {
	objectstore.Store
	body                 string
	size                 int64
	opens, reads, closes atomic.Int32
}

func (b *boundedReadBackend) Open(context.Context, string) (io.ReadCloser, objectstore.Info, error) {
	b.opens.Add(1)
	return boundedReadBody{Reader: strings.NewReader(b.body), reads: &b.reads, closes: &b.closes}, objectstore.Info{Size: b.size}, nil
}

func TestBoundedVerificationRejectsOversizeBeforeReadingAndChecksActualBytes(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	backend := &boundedReadBackend{body: "body", size: 3 << 30}
	if err := store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	ref := Prefix + Binary + "/" + strings.Repeat("a", 64)
	if _, err := store.VerifyBounded(context.Background(), ref, Binary, 1024); err == nil {
		t.Fatal("oversize remote body was verified")
	}
	if backend.opens.Load() != 1 || backend.reads.Load() != 0 || backend.closes.Load() != 1 {
		t.Fatal("oversize object was read or its transport was not closed")
	}
	backend.size = 4
	if _, err := store.VerifyBounded(context.Background(), ref, Binary, 1024); err == nil {
		t.Fatal("wrong actual checksum was verified")
	}
	backend.size = 2
	if _, err := store.VerifyBounded(context.Background(), ref, Binary, 1024); err == nil {
		t.Fatal("underreported object size was accepted")
	}
	before := backend.opens.Load()
	if _, err := store.VerifyBounded(context.Background(), ref, JSON, 1024); err == nil || backend.opens.Load() != before {
		t.Fatal("owner encoding mismatch reached object storage")
	}
	for _, limit := range []int64{0, -1, math.MaxInt64} {
		if _, err := store.PrepareBounded(context.Background(), strings.NewReader("body"), Binary, limit); err == nil {
			t.Fatal("invalid bounded publication limit accepted")
		}
		if _, err := store.OpenBounded(context.Background(), ref, limit); err == nil {
			t.Fatal("invalid bounded open limit accepted")
		}
	}
}

func TestReadBackProofIsPrivateStoreBoundAndExpiresAtClose(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	other, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	prepared, err := store.PrepareBounded(context.Background(), strings.NewReader("exact capture"), Binary, 1024)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := store.VerifyBounded(context.Background(), prepared.Ref(), Binary, 1024)
	if err != nil || verified.Size() != prepared.Size() {
		t.Fatalf("read-back verification failed: %+v %v", verified, err)
	}
	if _, err := other.VerifiedRef(verified); err == nil {
		t.Fatal("foreign storage accepted a read-back proof")
	}
	if _, err := store.VerifiedRef(Verified{}); err == nil {
		t.Fatal("zero read-back proof accepted")
	}
	store.Close()
	if _, err := store.VerifiedRef(verified); !errors.Is(err, ErrClosed) {
		t.Fatal("closed storage accepted a held read-back proof")
	}
	if _, err := store.PreparedRef(prepared); !errors.Is(err, ErrClosed) {
		t.Fatal("closed storage accepted a held publication proof")
	}
}
