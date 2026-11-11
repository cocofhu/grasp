package blob

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestLocalFSPutOpenDelete(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalFS(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref, err := store.Put(ctx, bytes.NewReader([]byte("hello")), Meta{MimeType: "text/plain", Name: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref.String(), "blob:") {
		t.Fatalf("ref = %q", ref)
	}
	rc, meta, err := store.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(raw) != "hello" || meta.MimeType != "text/plain" || meta.Name != "a.txt" || meta.Size != 5 {
		t.Fatalf("got %q meta=%+v", raw, meta)
	}
	if meta.SHA256 == "" {
		t.Fatal("expected sha256")
	}
	// Sharded path exists.
	id := ref.ID()
	if _, err := filepath.Glob(filepath.Join(root, id[:2], id, "blob")); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(ctx, ref); err == nil {
		t.Fatal("expected miss after delete")
	}
}

func TestParseRefRejectsTraversal(t *testing.T) {
	for _, s := range []string{"", "file:///tmp/x", "blob:", "blob:../x", "blob:a/b", "blob:a.b"} {
		if _, err := ParseRef(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
	if _, err := ParseRef("blob:abcDEF0123"); err != nil {
		t.Fatal(err)
	}
}

func TestIngestAndResolve(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	b64 := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	out, err := IngestPromptImages(ctx, store, []models.PromptImage{{
		Data: b64, MimeType: "image/png", Name: "x.png",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Data != "" || out[0].Ref == "" || out[0].SizeBytes != 9 {
		t.Fatalf("ingest = %+v", out[0])
	}
	wire, err := ResolveForWire(ctx, store, out)
	if err != nil {
		t.Fatal(err)
	}
	if wire[0].Data != b64 {
		t.Fatalf("resolve data = %q", wire[0].Data)
	}
}

func TestIngestCompositeInputs(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	b64 := base64.StdEncoding.EncodeToString([]byte("x"))
	inputs := map[string]any{
		"feature": map[string]any{
			"text": "hi",
			"images": []any{
				map[string]any{"data": b64, "mimeType": "image/png", "name": "a.png"},
			},
		},
		"plain": "ok",
	}
	got, err := IngestCompositeInputs(ctx, store, inputs)
	if err != nil {
		t.Fatal(err)
	}
	ct := models.AsCompositeText(got["feature"])
	if ct == nil || len(ct.Images) != 1 || ct.Images[0].Data != "" || ct.Images[0].Ref == "" {
		t.Fatalf("feature = %#v", got["feature"])
	}
	if got["plain"] != "ok" {
		t.Fatalf("plain = %#v", got["plain"])
	}
}
