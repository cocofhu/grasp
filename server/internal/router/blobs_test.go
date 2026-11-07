package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/handlers"
	"golang.org/x/crypto/bcrypt"
)

func TestBlobRangesRequireExistingLogin(t *testing.T) {
	db, err := database.OpenSQLiteTest(t.TempDir() + "/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("attachment-test"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{Users: []config.AuthUser{{Username: "admin", PasswordHash: string(hash)}}, SessionTTL: "1h"}}
	store := blob.NewMemory()
	ref, err := store.Put(context.Background(), strings.NewReader("attachment"), blob.Meta{MimeType: "audio/mpeg", Name: "voice.mp3"})
	if err != nil {
		t.Fatal(err)
	}
	r := New(&handlers.Handlers{Auth: auth.NewService(db, func() *config.Config { return cfg }), Blobs: store})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/api/blobs/"+ref.ID(), nil)
		req.Header.Set("Range", "bytes=0-2")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous range: %d", w.Code)
		}
	}
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"attachment-test"}`))
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, login)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/blobs/"+ref.ID(), nil)
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}
	req.Header.Set("Range", "bytes=0-2")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "att" {
		t.Fatalf("authenticated range: %d %s", w.Code, w.Body)
	}
}
