package desktop

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed launcher.html
var launcherHTML []byte

// Launcher serves the browser launch page and starts whitelisted apps. It
// listens on loopback only; Host and Origin checks stop other pages (and DNS
// rebinding) from launching anything.
type Launcher struct {
	Paths    Paths
	Runner   Runner
	LookPath func(string) (string, error)
	Port     int
}

type launcherApp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NameZH   string `json:"nameZh,omitempty"`
	Category string `json:"category"`
	Icon     string `json:"icon,omitempty"`
}

func (l *Launcher) apps() []App {
	return InstalledApps(LoadApps(l.Paths.Apps), l.LookPath)
}

// webIcon is the icon file the launch page shows for a: the runtime bundle's
// icon when there is one, else the image icon if browsers can render it
// (apps.json may point at XPM).
func (l *Launcher) webIcon(a App) string {
	if l.Paths.Share != "" {
		p := filepath.Join(l.Paths.Share, "icons", a.ID+".svg")
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	switch strings.ToLower(filepath.Ext(a.Icon)) {
	case ".svg", ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return a.Icon
	}
	return ""
}

func (l *Launcher) allowedHost(host string) bool {
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		if host == fmt.Sprintf("%s:%d", h, l.Port) {
			return true
		}
	}
	return false
}

// Handler returns the launcher routes.
func (l *Launcher) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'self' chrome-extension:")
		_, _ = w.Write(launcherHTML)
	})
	mux.HandleFunc("GET /apps", func(w http.ResponseWriter, _ *http.Request) {
		list := []launcherApp{}
		for _, a := range l.apps() {
			item := launcherApp{ID: a.ID, Name: a.Name, NameZH: a.NameZH, Category: a.Category}
			if l.webIcon(a) != "" {
				item.Icon = "/icons/" + a.ID
			}
			list = append(list, item)
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /icons/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, ok := FindApp(l.apps(), r.PathValue("id"))
		icon := l.webIcon(a)
		if !ok || icon == "" {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(icon)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if ct := mime.TypeByExtension(filepath.Ext(icon)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("POST /launch", l.launch)
	return l.guard(mux)
}

func (l *Launcher) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allowedHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Launcher) launch(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !l.allowedHost(strings.TrimPrefix(origin, "http://")) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin launch refused"})
		return
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content-type must be application/json"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	a, ok := FindApp(l.apps(), body.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown app"})
		return
	}
	if err := LaunchApp(l.Runner, a); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": a.ID})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
