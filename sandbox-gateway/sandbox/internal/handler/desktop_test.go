package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/desktop"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

func TestDesktopClipboard(t *testing.T) {
	const path = "/api/desktop/clipboard"
	const text = "  多行 🙂\nconst x = 1;\n"
	var copied string
	r := gin.New()
	r.PUT(path, DesktopClipboard(func(_ context.Context, input string) error {
		if input == "unavailable" {
			return errors.New("clipboard unavailable")
		}
		copied = input
		return nil
	}))
	body, _ := json.Marshal(map[string]string{"text": text})
	if code, _ := doJSON(r, http.MethodPut, path, string(body)); code != http.StatusNoContent || copied != text {
		t.Fatalf("status=%d clipboard=%q", code, copied)
	}
	for _, body := range []string{`{"text":""}`, `{`, `{"text":"` + strings.Repeat("x", desktopClipboardMaxBytes+1) + `"}`} {
		if code, _ := doJSON(r, http.MethodPut, path, body); code != http.StatusBadRequest || copied != text {
			t.Fatalf("invalid request: status=%d clipboard=%q", code, copied)
		}
	}
	if code, _ := doJSON(r, http.MethodPut, path, `{"text":"unavailable"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable: status=%d", code)
	}
}

func desktopRouter(t *testing.T, marker bool) (*gin.Engine, desktop.Paths) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	p := desktop.Paths{Marker: filepath.Join(dir, "marker"), RunDir: filepath.Join(dir, "run"), Apps: filepath.Join(dir, "apps.json")}
	if marker {
		_ = os.WriteFile(p.Marker, nil, 0o644)
	}
	store := desktop.NewControlStore(p, nil)
	chats := service.NewChatManager()
	r := gin.New()
	r.GET("/api/desktop/status", DesktopStatus(chats, p, store))
	r.PUT("/api/desktop/control", DesktopControl(chats, p, store))
	return r, p
}

func doJSON(r http.Handler, method, path, body string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestDesktopStatusAndControl(t *testing.T) {
	r, _ := desktopRouter(t, true)
	code, st := doJSON(r, http.MethodGet, "/api/desktop/status", "")
	if code != 200 || st["mode"] != "desktop" || st["holder"] != "agent" || st["agentActive"] != false {
		t.Fatalf("status: %d %v", code, st)
	}
	code, st = doJSON(r, http.MethodPut, "/api/desktop/control", `{"holder":"user","client":"tab-1"}`)
	if code != 200 || st["holder"] != "user" || st["expiresAt"] == nil {
		t.Fatalf("take: %d %v", code, st)
	}
	_, st = doJSON(r, http.MethodGet, "/api/desktop/status", "")
	if st["holder"] != "user" {
		t.Fatalf("status after take: %v", st)
	}
	_, st = doJSON(r, http.MethodPut, "/api/desktop/control", `{"holder":"agent","client":"tab-1"}`)
	if st["holder"] != "agent" {
		t.Fatalf("return: %v", st)
	}
	if code, _ := doJSON(r, http.MethodPut, "/api/desktop/control", `{"holder":"root"}`); code != 400 {
		t.Fatalf("bad holder: %d", code)
	}
	if code, _ := doJSON(r, http.MethodPut, "/api/desktop/control", `{`); code != 400 {
		t.Fatalf("bad json: %d", code)
	}
}

func TestDesktopStatusBrowserImageAndWriteFailure(t *testing.T) {
	r, p := desktopRouter(t, false)
	if _, st := doJSON(r, http.MethodGet, "/api/desktop/status", ""); st["mode"] != "browser" {
		t.Fatalf("browser image: %v", st)
	}
	_ = os.WriteFile(p.RunDir, []byte("not a dir"), 0o644)
	if code, _ := doJSON(r, http.MethodPut, "/api/desktop/control", `{"holder":"user"}`); code != 500 {
		t.Fatalf("write failure: %d", code)
	}
}

func TestCapabilitiesDeclareDesktopWithoutPreviewEnvSwitch(t *testing.T) {
	caps := BuildCapabilities(nil)
	preview := caps["preview"].(gin.H)
	if _, ok := preview["enableEnv"]; ok {
		t.Fatalf("preview must not name an enable switch: %v", preview)
	}
	d := caps["desktop"].(gin.H)
	if d["statusPath"] != "/api/desktop/status" || d["controlPath"] != "/api/desktop/control" {
		t.Fatalf("desktop: %v", d)
	}
}
