package auth_test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func setupAuthHTTP(t *testing.T) (*auth.Service, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.OpenSQLiteTest(t.TempDir() + "/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Users:      []config.AuthUser{{Username: "admin", PasswordHash: string(hash)}},
			SessionTTL: "168h",
		},
	}
	config.StoreConfig(cfg)
	svc := auth.NewService(db, config.GetConfig)

	r := gin.New()
	r.POST("/login", svc.LoginHandler)
	r.POST("/logout", svc.LogoutHandler)
	r.GET("/me", svc.MeHandler)
	r.GET("/api/protected", svc.APIMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/sandbox/*path", svc.SandboxRedirectMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"sandbox": true})
	})
	r.GET("/ws", func(c *gin.Context) {
		if _, ok := svc.RequireSession(c); ok {
			c.JSON(http.StatusOK, gin.H{"ws": true})
		}
	})
	return svc, r
}

func TestLoginHandlerSuccess(t *testing.T) {
	_, r := setupAuthHTTP(t)
	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "secret",
		"redirect": "/workflows",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["username"] != "admin" || resp["redirect"] != "/workflows" {
		t.Fatalf("response: %+v", resp)
	}
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == auth.CookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected session cookie")
	}
}

func TestLoginHandlerInvalidJSON(t *testing.T) {
	_, r := setupAuthHTTP(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: %d", w.Code)
	}
}

func TestLoginHandlerWrongPassword(t *testing.T) {
	_, r := setupAuthHTTP(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", w.Code)
	}
}

func TestLoginHandlerRepeatedFailuresStayUnauthorized(t *testing.T) {
	_, r := setupAuthHTTP(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "10.0.0.99")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, w.Code)
		}
	}
}

func TestLogoutAndMeHandler(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	sess, err := svc.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sess.ID})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("me without middleware: %d", w.Code)
	}

	// MeHandler with session in context (simulating middleware).
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("auth_session", sess)
	svc.MeHandler(c)
	if c.Writer.Status() != http.StatusOK {
		t.Fatalf("me with session: %d", c.Writer.Status())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sess.ID})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout: %d", w.Code)
	}
}

func TestAPIMiddleware(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	sess, err := svc.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sess.ID})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("auth: %d %s", w.Code, w.Body.String())
	}
}

func TestSandboxRedirectMiddleware(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sandbox/1/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("redirect: %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc == "" || loc == "/login" {
		t.Fatalf("location: %q", loc)
	}

	sess, _ := svc.CreateSession("admin")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/sandbox/1/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sess.ID})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authed sandbox: %d", w.Code)
	}
}

func TestRequireSession(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("require session: %d", w.Code)
	}

	sess, _ := svc.CreateSession("admin")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sess.ID})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ws authed: %d", w.Code)
	}
}

func TestSessionCookieSecure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	auth.SetSessionCookie(c, "tok")
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].Secure {
		t.Fatal("expected secure cookie with X-Forwarded-Proto")
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.TLS = &tls.ConnectionState{}
	auth.SetSessionCookie(c, "tok2")
	cookies = w.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].Secure {
		t.Fatal("expected secure cookie with TLS")
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	auth.ClearSessionCookie(c)
	auth.SetSessionCookie(c, "plain")
	cookies = w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie")
	}
}

func TestSessionTokenAndGetSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: " abc "})
	if auth.SessionToken(c) != "abc" {
		t.Fatalf("token: %q", auth.SessionToken(c))
	}
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if auth.SessionToken(c) != "" {
		t.Fatal("expected empty token")
	}

	sess := &models.Session{ID: "t", Username: "u"}
	c.Set("auth_session", sess)
	got, ok := auth.GetSession(c)
	if !ok || got.ID != "t" {
		t.Fatalf("GetSession: %+v %v", got, ok)
	}
	c.Set("auth_session", "bad")
	if _, ok := auth.GetSession(c); ok {
		t.Fatal("expected bad type")
	}
}

func TestValidateSessionExpired(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/exp.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Users:      []config.AuthUser{{Username: "admin", PasswordHash: string(hash)}},
			SessionTTL: "168h",
		},
	}
	config.StoreConfig(cfg)
	svc := auth.NewService(db, config.GetConfig)
	if err := db.Create(&models.Session{
		ID: "expired", Username: "admin", ExpiresAt: time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateSession("expired"); err == nil {
		t.Fatal("expected expired session error")
	}
}
