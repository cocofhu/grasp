package handlers

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func sshKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ssh.NewPublicKey(pub)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " me@laptop"
}

func setupSSHKeyHandlers(t *testing.T, gateway *SSHGatewayInfo) (*gin.Engine, *Handlers) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "h.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UserSSHKey{}, &models.Session{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{Users: []config.AuthUser{
		{Username: "alice", PasswordHash: "h"}, {Username: "admin", PasswordHash: "h", IsAdmin: true},
	}}}
	getCfg := func() *config.Config { return cfg }
	h := &Handlers{SSHKeys: services.NewSSHKeyService(db, getCfg), SSHGateway: gateway, Auth: auth.NewService(db, getCfg)}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if user := c.GetHeader("X-Test-User"); user != "" {
			c.Set("auth_session", &models.Session{Username: user})
		}
	})
	r.GET("/api/ssh-gateway", h.GetSSHGateway)
	r.GET("/api/me/ssh-keys", h.ListSSHKeys)
	r.POST("/api/me/ssh-keys", h.AddSSHKey)
	r.DELETE("/api/me/ssh-keys/:id", h.DeleteSSHKey)
	return r, h
}

func doJSON(r http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSSHKeyHandlersCRUD(t *testing.T) {
	r, _ := setupSSHKeyHandlers(t, nil)
	line := sshKeyLine(t)
	body, _ := json.Marshal(addSSHKeyBody{PublicKey: line})

	if w := doJSON(r, http.MethodPost, "/api/me/ssh-keys", "", string(body)); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous add = %d", w.Code)
	}
	w := doJSON(r, http.MethodPost, "/api/me/ssh-keys", "alice", string(body))
	if w.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	var created models.UserSSHKey
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Name != "me@laptop" || !strings.HasPrefix(created.Fingerprint, "SHA256:") || strings.Contains(w.Body.String(), "alice") {
		t.Fatalf("created = %s", w.Body)
	}
	if w := doJSON(r, http.MethodPost, "/api/me/ssh-keys", "bob", string(body)); w.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d", w.Code)
	}
	if w := doJSON(r, http.MethodPost, "/api/me/ssh-keys", "alice", `{"publicKey":"nope"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "无法识别") {
		t.Fatalf("invalid = %d %s", w.Code, w.Body)
	}
	if w := doJSON(r, http.MethodPost, "/api/me/ssh-keys", "alice", `{`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", w.Code)
	}

	w = doJSON(r, http.MethodGet, "/api/me/ssh-keys", "alice", "")
	var list struct{ Keys []models.UserSSHKey }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Keys) != 1 {
		t.Fatalf("list = %s", w.Body)
	}
	w = doJSON(r, http.MethodGet, "/api/me/ssh-keys", "bob", "")
	if !strings.Contains(w.Body.String(), `"keys":[]`) {
		t.Fatalf("other user's list = %s", w.Body)
	}

	id := strconv.FormatUint(uint64(created.ID), 10)
	if w := doJSON(r, http.MethodDelete, "/api/me/ssh-keys/"+id, "bob", ""); w.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete = %d", w.Code)
	}
	if w := doJSON(r, http.MethodDelete, "/api/me/ssh-keys/x", "alice", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}
	if w := doJSON(r, http.MethodDelete, "/api/me/ssh-keys/"+id, "alice", ""); w.Code != http.StatusOK {
		t.Fatalf("delete = %d", w.Code)
	}
}

func TestSSHGatewayInfoHandler(t *testing.T) {
	r, h := setupSSHKeyHandlers(t, nil)
	if w := doJSON(r, http.MethodGet, "/api/ssh-gateway", "alice", ""); w.Body.String() != `{"enabled":false}` {
		t.Fatalf("disabled = %s", w.Body)
	}
	if w := doJSON(r, http.MethodGet, "/api/ssh-gateway", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", w.Code)
	}

	h.SSHGateway = &SSHGatewayInfo{PublicHost: "ssh.example.com", PublicPort: 2222, HostKeyFingerprint: "SHA256:host", AdminsOnly: true}
	var got struct {
		Enabled, AdminsOnly, Allowed, HasKey bool
		Host, HostKeyFingerprint             string
		Port                                 int
	}
	w := doJSON(r, http.MethodGet, "/api/ssh-gateway", "alice", "")
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if !got.Enabled || got.Host != "ssh.example.com" || got.Port != 2222 || got.HostKeyFingerprint != "SHA256:host" ||
		!got.AdminsOnly || got.Allowed || got.HasKey {
		t.Fatalf("regular user = %s", w.Body)
	}
	if _, err := h.SSHKeys.Add("admin", "", sshKeyLine(t)); err != nil {
		t.Fatal(err)
	}
	w = doJSON(r, http.MethodGet, "/api/ssh-gateway", "admin", "")
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if !got.Allowed || !got.HasKey {
		t.Fatalf("admin = %s", w.Body)
	}

	h.SSHKeys = nil
	if w := doJSON(r, http.MethodGet, "/api/me/ssh-keys", "alice", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no service = %d", w.Code)
	}
}
