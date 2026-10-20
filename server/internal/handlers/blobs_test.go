package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/blob"

	"github.com/gin-gonic/gin"
)

func TestGetBlobSniffsImageMIMEWithoutRewritingStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := blob.NewMemory()
	webp := []byte("RIFF\x00\x00\x00\x00WEBP extra-bytes-keep-original")
	storedMIME := "image/jpeg; charset=utf-8"
	ref, err := store.Put(context.Background(), bytes.NewReader(webp), blob.Meta{
		MimeType: storedMIME,
		Name:     "70345B2BE",
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &Handlers{Blobs: store}
	r := gin.New()
	r.GET("/api/blobs/:id", h.GetBlob)

	id := strings.TrimPrefix(ref.String(), "blob:")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/blobs/"+id, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("Content-Type = %q want image/webp (g2.1)", ct)
	}
	if strings.Contains(w.Header().Get("Content-Type"), "charset") {
		t.Fatalf("Content-Type still has charset: %q", w.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(w.Body)
	if !bytes.Equal(body, webp) {
		t.Fatalf("response bytes rewritten")
	}

	_, meta, err := store.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if meta.MimeType != storedMIME {
		t.Fatalf("store meta rewritten: %q want %q (g2.2)", meta.MimeType, storedMIME)
	}
	rc, _, err := store.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(stored, webp) {
		t.Fatalf("store bytes rewritten (g2.2)")
	}
}
