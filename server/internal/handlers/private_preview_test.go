package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/privatepreview"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gorilla/websocket"
)

const privatePreviewTicket = "0123456789abcdef0123456789abcdef"

func seedPrivatePreview(t *testing.T, hn *harness, upstream string) string {
	t.Helper()
	base, err := privatepreview.Parse("http://preview.localhost")
	if err != nil {
		t.Fatal(err)
	}
	hn.h.PrivatePreviewBase = base
	hn.host.SetPrivatePreviewBase(base)
	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.host.SetPreviewSandboxOps(preview)
	seedAppPreviewReview(t, hn, "private-run", "app")
	row := models.Sandbox{Name: "private-sandbox", RunID: "private-run", NodeID: "app", Purpose: "run", Status: "running"}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	hn.fg.Seed(row.Name)
	hn.fg.SetEndpoints(row.Name, map[string]string{
		"18080": strings.TrimPrefix(upstream, "http://"),
		"5174":  "127.0.0.1:1", // the app's raw port must never replace its injector
	})
	public := base.URL(privatePreviewTicket)
	if err := preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "private-run", NodeID: "app", Kind: mcp.PreviewKindPort, Port: 5174,
		ProxyURL: public, Host: "http://127.0.0.1:9", InjectorPort: 18080,
		SandboxName: row.Name, Healthy: true, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return public
}

func TestPrivatePreviewRootPathsAndCredentialIsolation(t *testing.T) {
	hn := newHarness(t)
	seen := make(chan *http.Request, 4)
	const html = `<!doctype html><html><head><script type="module" src="/@vite/client"></script></head><body><script src="/__grasp/preview-pick.js"></script></body></html>`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Clone(r.Context())
		w.Header().Set("E2b-Traffic-Access-Token", "provider-secret")
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, html)
			return
		}
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	upstreamURL, _ := url.Parse(up.URL)
	for _, path := range []string{"/", "/__grasp/preview-pick.js", "/api/app?q=a%2Fb", "/@vite/client"} {
		req := httptest.NewRequest(http.MethodGet, public+strings.TrimPrefix(path, "/"), nil)
		req.Header.Set("Authorization", "Bearer app-secret")
		req.Header.Set("E2b-Traffic-Access-Token", "provider-secret")
		req.Header.Set("X-E2b-Sandbox-Id", "provider-id")
		req.Header.Set("X-Api-Key", "app-api-key")
		req.Header.Set("X-Gate-Share-Token", "share-secret")
		req.Header.Set("X-Grasp-Doctor-Cleanup", "platform-secret")
		req.Header.Set("Origin", strings.TrimRight(public, "/"))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
		req.AddCookie(&http.Cookie{Name: "app_session", Value: "app-value"})
		w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
		hn.r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || w.Header().Get("E2b-Traffic-Access-Token") != "" {
			t.Fatalf("path %s: code %d headers %v", path, w.Code, w.Header())
		}
		if path == "/" && w.Body.String() != html {
			t.Fatalf("root HTML was rewritten: %s", w.Body.String())
		}
		if path != "/" && w.Body.String() != path {
			t.Fatalf("root path lost: %q, want %q", w.Body.String(), path)
		}
		got := <-seen
		if got.Host != upstreamURL.Host || got.Header.Get("X-Forwarded-Host") != privatePreviewTicket+".preview.localhost" || got.Header.Get("X-Forwarded-Proto") != "http" || got.Header.Get("Origin") != up.URL || got.Header.Get("Authorization") != "Bearer app-secret" || got.Header.Get("E2b-Traffic-Access-Token") != "" || got.Header.Get("X-E2b-Sandbox-Id") != "" || got.Header.Get("X-Api-Key") != "app-api-key" || got.Header.Get("X-Gate-Share-Token") != "" || got.Header.Get("X-Grasp-Doctor-Cleanup") != "" || strings.Contains(got.Header.Get("Cookie"), auth.CookieName) || got.Header.Get("Cookie") != "app_session=app-value" {
			t.Fatalf("credential/origin boundary failed: host=%q cookie=%q auth=%q", got.Host, got.Header.Get("Cookie"), got.Header.Get("Authorization"))
		}
	}
}

func TestPrivatePreviewUnknownHostsAndLegacyPathCannotReachPlatform(t *testing.T) {
	hn := newHarness(t)
	seedPrivatePreview(t, hn, "http://127.0.0.1:1")
	for _, host := range []string{
		"preview.localhost", "unknown.preview.localhost", "unknown.preview.localhost.", strings.Repeat("a", 32) + ".preview.localhost",
		privatePreviewTicket + ".preview.localhost:1234", "nested." + privatePreviewTicket + ".preview.localhost",
	} {
		for _, path := range []string{"/api/health", "/api/auth/login", "/assets/app.js", "/login", "/"} {
			req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
			w := httptest.NewRecorder()
			hn.r.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound || w.Body.String() != "preview not found" {
				t.Fatalf("preview host reached platform: %s%s: %d %s", host, path, w.Code, w.Body.String())
			}
		}
	}
	w := hn.do(http.MethodGet, "/preview/private-run/app/5174/", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("same-origin bypass: %d %s", w.Code, w.Body.String())
	}
	// X-Forwarded-Host alone cannot redeem a preview capability.
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Forwarded-Host", privatePreviewTicket+".preview.localhost")
	w = httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() == "preview not found" {
		t.Fatalf("forwarded host changed routing: %d %s", w.Code, w.Body.String())
	}
}

func TestPrivatePreviewRedirectsAndPersistedBinding(t *testing.T) {
	hn := newHarness(t)
	var upstream string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		location := "/login?next=%2Fapp"
		if r.URL.Path == "/absolute" {
			location = upstream + "/login?next=%2Fapp"
		}
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusFound)
	}))
	upstream = up.URL
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	for _, path := range []string{"relative", "absolute"} {
		req := httptest.NewRequest(http.MethodGet, public+path, nil)
		w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
		hn.r.ServeHTTP(w, req)
		want := "/login?next=%2Fapp"
		if path == "absolute" {
			want = public + "login?next=%2Fapp"
		}
		if w.Code != http.StatusFound || w.Header().Get("Location") != want {
			t.Fatalf("redirect %s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	// Replacing MCP memory does not invalidate a persisted registration.
	fresh := mcp.NewHost(services.NewArtifactService(hn.db))
	fresh.SetPrivatePreviewBase(hn.h.PrivatePreviewBase)
	hn.h.MCP = fresh
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	hn.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, public+"relative", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("server restart lost persisted capability: %d", w.Code)
	}
	// The gateway may still report an old guest running after its run binding
	// moves. The capability must not route into that stale generation.
	if err := hn.db.Model(&models.Sandbox{}).Where("name = ?", "private-sandbox").Update("name", "new-generation").Error; err != nil {
		t.Fatal(err)
	}
	w = &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	hn.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, public, nil))
	if w.Code != http.StatusGone {
		t.Fatalf("stale capability survived rebind: %d", w.Code)
	}
}

func TestPrivatePreviewWebSocketAndStoppedGuest(t *testing.T) {
	hn := newHarness(t)
	upgrader := websocket.Upgrader{} // Exercise the real default same-origin check.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/vite-hmr?session=synthetic" || r.Header.Get("E2b-Traffic-Access-Token") != "" {
			http.Error(w, "wrong private route", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		kind, payload, err := conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(kind, append([]byte("echo:"), payload...))
		}
	}))
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	server := httptest.NewServer(hn.r)
	t.Cleanup(server.Close)
	origin, _ := url.Parse(public)
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/vite-hmr?session=synthetic", http.Header{
		"Host": {origin.Host}, "Origin": {strings.TrimRight(public, "/")}, "E2b-Traffic-Access-Token": {"must-not-forward"},
	})
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hmr")); err != nil {
		t.Fatal(err)
	}
	_, got, err := conn.ReadMessage()
	if err != nil || string(got) != "echo:hmr" {
		t.Fatalf("WS passthrough: %q, %v", got, err)
	}
	// A foreign page cannot gain same-origin treatment just by knowing a
	// registered preview host. Its Origin is retained for the app's guard.
	foreign, denied, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/vite-hmr?session=synthetic", http.Header{
		"Host": {origin.Host}, "Origin": {"https://foreign.example.org"},
	})
	if foreign != nil {
		_ = foreign.Close()
	}
	if err == nil || denied == nil || denied.StatusCode != http.StatusForbidden {
		t.Fatal("foreign WebSocket Origin was accepted")
	}
	_ = denied.Body.Close()
	hn.fg.SetStatus("private-sandbox", "stopped")
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, public, nil))
	if w.Code != http.StatusGone {
		t.Fatalf("stopped guest remained reachable: %d", w.Code)
	}
}

func TestPrivatePreviewPreservesAppAuthorizationHeaders(t *testing.T) {
	hn := newHarness(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []string{"Authorization", "X-Api-Key", "X-Access-Token"} {
			if r.Header.Get(key) != "app-"+key {
				http.Error(w, "app authentication missing", http.StatusUnauthorized)
				return
			}
			w.Header().Set(key, r.Header.Get(key))
		}
		if r.Header.Get("Proxy-Authorization") != "" {
			http.Error(w, "proxy credential reached app", http.StatusBadRequest)
			return
		}
		for _, key := range []string{"E2b-Traffic-Access-Token", "X-E2b-Sandbox-Id", "X-Grasp-Internal", "X-Gate-Share-Token"} {
			if r.Header.Get(key) != "" {
				http.Error(w, "reserved credential reached app", http.StatusBadRequest)
				return
			}
			w.Header().Set(key, "reserved-value")
		}
		w.Header().Add("Set-Cookie", "cf_session=reserved-value; HttpOnly; Path=/")
		w.Header().Add("Set-Cookie", "app_session=app-value; HttpOnly; Path=/")
		_, _ = io.WriteString(w, "app-authenticated")
	}))
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	req := httptest.NewRequest(http.MethodGet, public+"api/private", nil)
	for _, key := range []string{"Authorization", "X-Api-Key", "X-Access-Token"} {
		req.Header.Set(key, "app-"+key)
	}
	for _, key := range []string{"E2b-Traffic-Access-Token", "X-E2b-Sandbox-Id", "X-Grasp-Internal", "X-Gate-Share-Token", "Proxy-Authorization"} {
		req.Header.Set(key, "reserved-value")
	}
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "app-authenticated" {
		t.Fatalf("app authentication failed: %d", w.Code)
	}
	for _, key := range []string{"Authorization", "X-Api-Key", "X-Access-Token"} {
		if w.Header().Get(key) != "app-"+key {
			t.Errorf("app response header %s was lost", key)
		}
	}
	for _, key := range []string{"E2b-Traffic-Access-Token", "X-E2b-Sandbox-Id", "X-Grasp-Internal", "X-Gate-Share-Token"} {
		if w.Header().Get(key) != "" {
			t.Errorf("reserved response header %s escaped", key)
		}
	}
	if got := w.Header().Values("Set-Cookie"); len(got) != 1 || !strings.HasPrefix(got[0], "app_session=") {
		t.Fatalf("cookie boundary failed: %v", got)
	}
}

func TestPrivatePreviewPreservesOnlyCanonicalUpstreamDiagnostic(t *testing.T) {
	hn := newHarness(t)
	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{"app-port", []string{"5174"}, "5174"},
		{"minimum", []string{"1"}, "1"},
		{"maximum", []string{"65535"}, "65535"},
		{"missing", nil, ""},
		{"empty", []string{""}, ""},
		{"zero", []string{"0"}, ""},
		{"negative", []string{"-1"}, ""},
		{"too-large", []string{"65536"}, ""},
		{"leading-zero", []string{"05174"}, ""},
		{"plus", []string{"+5174"}, ""},
		{"token", []string{"provider-secret"}, ""},
		{"comma", []string{"5174,5175"}, ""},
		{"duplicate", []string{"5174", "5174"}, ""},
		{"multiple", []string{"5174", "5175"}, ""},
	}
	headers := make(map[string][]string, len(cases))
	for _, tc := range cases {
		headers["/"+tc.name] = tc.values
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Grasp-Upstream") != "" {
			http.Error(w, "request diagnostic must be stripped", http.StatusBadRequest)
			return
		}
		for _, value := range headers[r.URL.Path] {
			w.Header().Add("X-Grasp-Upstream", value)
		}
		w.Header().Set("X-Grasp-Internal", "reserved-value")
		_, _ = io.WriteString(w, "app-response")
	}))
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, public+tc.name, nil)
			req.Header.Set("X-Grasp-Upstream", "9999")
			w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
			hn.r.ServeHTTP(w, req)
			if w.Code != http.StatusOK || w.Body.String() != "app-response" || w.Header().Get("X-Grasp-Internal") != "" {
				t.Fatalf("response boundary failed: %d", w.Code)
			}
			got := w.Header().Values("X-Grasp-Upstream")
			if tc.want == "" && len(got) != 0 || tc.want != "" && (len(got) != 1 || got[0] != tc.want) {
				t.Fatalf("diagnostic values %v, want %q", got, tc.want)
			}
		})
	}
}

func TestPrivatePreviewPreservesEmbedAndShareTicketScope(t *testing.T) {
	hn := newHarness(t)
	public := seedPrivatePreview(t, hn, "http://127.0.0.1:1")
	// Existing drawer tickets remain run/node-bound and retain the Grasp
	// parent origin, while frame-ancestors admits the isolated app origin.
	ticket := issueSessionTicket(t, hn, "private-run", "app")
	got := redeem(t, hn, ticket)
	if got["runId"] != "private-run" || got["nodeId"] != "app" {
		t.Fatalf("drawer scope changed: %+v", got)
	}
	w := hn.do(http.MethodGet, "/embed/runs/private-run/nodes/app/chat", nil)
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, strings.TrimRight(public, "/")) || strings.Contains(csp, "127.0.0.1") {
		t.Fatalf("drawer ancestors did not follow isolated origin: %q", csp)
	}
	created := parseJSON(t, hn.do(http.MethodPost, "/api/runs/private-run/nodes/app/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	shareURL, _ := created["url"].(string)
	_, token, ok := strings.Cut(shareURL, "#t=")
	if !ok {
		t.Fatalf("share URL: %q", shareURL)
	}
	ticketRes := parseJSON(t, hn.doPublic(http.MethodPost, "/public/approvals/preview-ticket", map[string]any{
		"port": 5174, "purpose": "api",
	}, map[string]string{headerShareToken: token, headerShareRequest: "1", "Origin": "http://" + publicHost}))
	shareTicket, _ := ticketRes["ticket"].(string)
	if shareTicket == "" {
		t.Fatalf("preview ticket missing: %+v", ticketRes)
	}
	path := "/public/approvals/preview-api/" + shareTicket + "/api/data?q=synthetic"
	w = hn.doPublic(http.MethodGet, path, nil, nil)
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != public+"api/data?q=synthetic" {
		t.Fatalf("private shared app escaped isolated origin: %d %q", w.Code, w.Header().Get("Location"))
	}
	// Invalid public tickets never reveal an isolated capability.
	w = hn.doPublic(http.MethodGet, "/public/approvals/preview-api/invalid/", nil, nil)
	if w.Code != http.StatusUnauthorized || w.Header().Get("Location") != "" {
		t.Fatalf("unscoped share ticket: %d %v", w.Code, w.Header())
	}
}
