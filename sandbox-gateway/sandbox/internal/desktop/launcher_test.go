package desktop

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func launcherReq(method, path, host, origin, ctype, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	return req
}

func TestLauncherPagesAndApps(t *testing.T) {
	p := testPaths(t, true)
	l := &Launcher{Paths: p, Runner: &fakeRunner{}, LookPath: lookAll, Port: 6091}
	h := l.Handler()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, launcherReq("GET", "/", "127.0.0.1:6091", "", "", ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "我的电脑") || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("index: %d %q", w.Code, w.Body.String()[:40])
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, launcherReq("GET", "/apps", "localhost:6091", "", "", ""))
	var apps []launcherApp
	if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil || len(apps) != 2 || apps[1].Icon != "/icons/blender" || apps[0].Icon != "" {
		t.Fatalf("apps: %s %v", w.Body.String(), err)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, launcherReq("GET", "/icons/blender", "[::1]:6091", "", "", ""))
	if w.Code != 200 || w.Body.String() != "<svg/>" || w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("icon: %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/icons/mousepad", "/icons/../../etc/passwd", "/icons/gimp"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, launcherReq("GET", path, "127.0.0.1:6091", "", "", ""))
		if w.Code == 200 {
			t.Fatalf("%s must not be served", path)
		}
	}
	apps2 := LoadApps(p.Apps)
	_ = os.Remove(apps2[1].Icon)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, launcherReq("GET", "/icons/blender", "127.0.0.1:6091", "", "", ""))
	if w.Code != 404 {
		t.Fatalf("missing icon file: %d", w.Code)
	}
}

func TestLauncherLaunchGuards(t *testing.T) {
	p := testPaths(t, true)
	fr := &fakeRunner{fail: map[string]error{}}
	l := &Launcher{Paths: p, Runner: fr, LookPath: lookOnly("blender", "mousepad"), Port: 6091}
	h := l.Handler()
	cases := []struct {
		name, host, origin, ctype, body string
		code                            int
	}{
		{"evil host (DNS rebinding)", "evil.example:6091", "", "application/json", `{"id":"blender"}`, 403},
		{"cross-site origin", "127.0.0.1:6091", "https://evil.example", "application/json", `{"id":"blender"}`, 403},
		{"null origin", "127.0.0.1:6091", "null", "application/json", `{"id":"blender"}`, 403},
		{"simple form post", "127.0.0.1:6091", "", "text/plain", `{"id":"blender"}`, 415},
		{"bad json", "127.0.0.1:6091", "", "application/json", `{`, 400},
		{"not whitelisted", "127.0.0.1:6091", "", "application/json", `{"id":"rm"}`, 404},
		{"same origin", "127.0.0.1:6091", "http://127.0.0.1:6091", "application/json; charset=utf-8", `{"id":"blender"}`, 200},
		{"repeat click", "127.0.0.1:6091", "http://localhost:6091", "application/json", `{"id":"blender"}`, 200},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, launcherReq("POST", "/launch", tc.host, tc.origin, tc.ctype, tc.body))
		if w.Code != tc.code {
			t.Fatalf("%s: code=%d want %d (%s)", tc.name, w.Code, tc.code, w.Body.String())
		}
	}
	if len(fr.started) != 2 || fr.started[0].name != "blender" || fr.started[0].args[0] != "--online-mode" {
		t.Fatalf("started = %+v", fr.started)
	}
	fr.fail["start mousepad"] = errors.New("exec failed")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, launcherReq("POST", "/launch", "127.0.0.1:6091", "", "application/json", `{"id":"mousepad"}`))
	if w.Code != 500 {
		t.Fatalf("start failure: %d", w.Code)
	}
}
