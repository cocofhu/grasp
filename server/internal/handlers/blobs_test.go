package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

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

func TestAttachmentRanges(t *testing.T) {
	local, err := blob.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]blob.Store{"memory": blob.NewMemory(), "local": local} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Repeat("media-", 200)
			ref, err := store.Put(context.Background(), strings.NewReader(raw), blob.Meta{MimeType: "application/octet-stream", Name: "演示.webm"})
			if err != nil {
				t.Fatal(err)
			}
			h := &Handlers{Blobs: store}
			r := gin.New()
			r.GET("/blobs/:id", h.GetBlob)
			r.HEAD("/blobs/:id", h.GetBlob)
			for _, tc := range []struct {
				method, byteRange  string
				status             int
				body, contentRange string
			}{
				{http.MethodGet, "", http.StatusOK, raw, ""},
				{http.MethodGet, "bytes=0-3", http.StatusPartialContent, raw[:4], "bytes 0-3/1200"},
				{http.MethodGet, "bytes=512-", http.StatusPartialContent, raw[512:], "bytes 512-1199/1200"},
				{http.MethodGet, "bytes=-6", http.StatusPartialContent, raw[len(raw)-6:], "bytes 1194-1199/1200"},
				{http.MethodHead, "", http.StatusOK, "", ""},
			} {
				t.Run(tc.method+tc.byteRange, func(t *testing.T) {
					req := httptest.NewRequest(tc.method, "/blobs/"+ref.ID(), nil)
					req.Header.Set("Range", tc.byteRange)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					if w.Code != tc.status || w.Body.String() != tc.body {
						t.Fatalf("status/body: %d %q", w.Code, w.Body.String())
					}
					if w.Header().Get("Content-Type") != "video/webm" || w.Header().Get("Accept-Ranges") != "bytes" || w.Header().Get("Content-Range") != tc.contentRange {
						t.Fatalf("media headers: %v", w.Header())
					}
					if !strings.Contains(w.Header().Get("Content-Disposition"), "filename*=utf-8''") {
						t.Fatalf("original unicode filename missing: %s", w.Header().Get("Content-Disposition"))
					}
					wantLength := len(tc.body)
					if tc.method == http.MethodHead {
						wantLength = len(raw)
					}
					if w.Header().Get("Content-Length") != strconv.Itoa(wantLength) {
						t.Fatalf("content-length: %v", w.Header())
					}
				})
			}
			for _, byteRange := range []string{"bytes=9999-", "bytes=bad"} {
				req := httptest.NewRequest(http.MethodGet, "/blobs/"+ref.ID(), nil)
				req.Header.Set("Range", byteRange)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusRequestedRangeNotSatisfiable {
					t.Fatalf("range %s: %d", byteRange, w.Code)
				}
			}
		})
	}
}

func TestPublicPromptMediaRanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{}
	r := gin.New()
	r.GET("/scoped", func(c *gin.Context) {
		h.writePublicPromptImage(c, models.PromptImage{Data: "YWJjZGVm", MimeType: "audio/mpeg", Name: "voice.mp3"})
	})
	req := httptest.NewRequest(http.MethodGet, "/scoped", nil)
	req.Header.Set("Range", "bytes=1-3")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "bcd" || w.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("public media range: %d %v %s", w.Code, w.Header(), w.Body)
	}
}
