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

func TestSessionBrowserOriginCompatibility(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	session, err := svc.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, target, origin, site string
		want                       int
	}{
		{"native client", "", "", "", http.StatusOK},
		{"same origin", "", "http://grasp.test:8080", "", http.StatusOK},
		{"dev proxy", "", "http://grasp.test:5173", "same-origin", http.StatusOK},
		{"tls proxy without forwarded proto", "http://grasp.test/api/protected", "https://grasp.test", "", http.StatusOK},
		{"explicit default port", "http://grasp.test:443/api/protected", "https://grasp.test", "", http.StatusOK},
		{"ipv6 host", "http://[::1]:8080/api/protected", "http://[::1]:8080", "", http.StatusOK},
		{"different port", "", "http://grasp.test:9090", "", http.StatusForbidden},
		{"preview port behind default port", "http://grasp.test/api/protected", "http://grasp.test:9090", "", http.StatusForbidden},
		{"non-http origin", "", "file://grasp.test:8080", "", http.StatusForbidden},
		{"opaque preview", "", "null", "", http.StatusForbidden},
		{"cross origin metadata", "", "", "same-site", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = "http://grasp.test:8080/api/protected"
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestSessionIgnoresPlantedCookies(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	victim, err := svc.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string, tokens ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, token := range tokens {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := request("/api/protected", "planted-invalid", victim.ID); w.Code != http.StatusOK {
		t.Fatalf("an invalid planted cookie hid the real session: %d", w.Code)
	}
	if w := request("/api/protected", victim.ID, victim.ID); w.Code != http.StatusOK {
		t.Fatalf("a repeated cookie value was treated as two sessions: %d", w.Code)
	}

	w := request("/api/protected", other.ID, victim.ID)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("two valid sessions must not authenticate: %d", w.Code)
	}
	cleared := map[string]bool{}
	for _, ck := range w.Result().Cookies() {
		if ck.Name == auth.CookieName && ck.MaxAge < 0 {
			cleared[ck.Path+"|"+ck.Domain] = true
		}
	}
	for _, key := range []string{"/|", "/api|", "/api/|", "/api/protected|", "/api|example.com"} {
		if !cleared[key] {
			t.Errorf("cookie copy %q was not expired: %v", key, cleared)
		}
	}

	if w := request("/sandbox/1/", other.ID, victim.ID); w.Code != http.StatusFound {
		t.Fatalf("sandbox proxy accepted two sessions: %d", w.Code)
	}
	if w := request("/ws", other.ID, victim.ID); w.Code != http.StatusUnauthorized {
		t.Fatalf("websocket accepted two sessions: %d", w.Code)
	}
}

func TestLogoutDeletesEverySessionCookie(t *testing.T) {
	svc, r := setupAuthHTTP(t)
	first, err := svc.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: first.ID})
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: second.ID})
	r.ServeHTTP(httptest.NewRecorder(), req)
	for _, id := range []string{first.ID, second.ID} {
		if _, err := svc.ValidateSession(id); err == nil {
			t.Fatal("logout left a session alive")
		}
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

func TestGetSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

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
