package previewinject

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func namedServer(t *testing.T, name string) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>" + name + "</body></html>"))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return srv, p
}

func writePort(t *testing.T, file string, v string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(v+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getBody(t *testing.T, proxyURL string) (string, string) {
	t.Helper()
	resp, err := http.Get(proxyURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.Header.Get(UpstreamHeader)
}

func TestSwitchableHandler_FollowsUpstreamFile(t *testing.T) {
	def, defPort := namedServer(t, "default-app")
	_, altPort := namedServer(t, "switched-app")
	defURL, _ := url.Parse(def.URL)
	file := filepath.Join(t.TempDir(), "upstream")

	proxy := httptest.NewServer(NewSwitchableHandler(defURL, file, scriptURL, EmbedLookup{}))
	t.Cleanup(proxy.Close)

	body, port := getBody(t, proxy.URL)
	if !strings.Contains(body, "default-app") || port != strconv.Itoa(defPort) {
		t.Fatalf("no file: body=%q port=%q", body, port)
	}

	writePort(t, file, strconv.Itoa(altPort))
	body, port = getBody(t, proxy.URL)
	if !strings.Contains(body, "switched-app") || port != strconv.Itoa(altPort) {
		t.Fatalf("switched: body=%q port=%q", body, port)
	}
	if !strings.Contains(body, "preview-pick.js") {
		t.Fatalf("switched page must still carry the pick script: %q", body)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	body, port = getBody(t, proxy.URL)
	if !strings.Contains(body, "default-app") || port != strconv.Itoa(defPort) {
		t.Fatalf("after reset: body=%q port=%q", body, port)
	}
}

func TestSwitchableHandler_DeadSwitchedPortFallsBack(t *testing.T) {
	def, defPort := namedServer(t, "default-app")
	alt, altPort := namedServer(t, "switched-app")
	defURL, _ := url.Parse(def.URL)
	file := filepath.Join(t.TempDir(), "upstream")
	writePort(t, file, strconv.Itoa(altPort))
	alt.Close()

	proxy := httptest.NewServer(NewSwitchableHandler(defURL, file, scriptURL, EmbedLookup{}))
	t.Cleanup(proxy.Close)
	body, port := getBody(t, proxy.URL)
	if !strings.Contains(body, "default-app") || port != strconv.Itoa(defPort) {
		t.Fatalf("dead switched port must fall back to PREVIEW_PORT: body=%q port=%q", body, port)
	}
}

func TestSwitchableHandler_RejectsBadFileContent(t *testing.T) {
	def, defPort := namedServer(t, "default-app")
	defURL, _ := url.Parse(def.URL)
	file := filepath.Join(t.TempDir(), "upstream")
	proxy := httptest.NewServer(NewSwitchableHandler(defURL, file, scriptURL, EmbedLookup{}))
	t.Cleanup(proxy.Close)

	for _, v := range []string{"9222", "8765", "17980", "0", "70000", "abc", ""} {
		writePort(t, file, v)
		// Bypass the read cache so each value is evaluated.
		time.Sleep(5 * time.Millisecond)
		_, port := getBody(t, proxy.URL)
		if port != strconv.Itoa(defPort) {
			t.Fatalf("file %q must be ignored, served from %q", v, port)
		}
	}
}

func TestValidUpstreamPort(t *testing.T) {
	for p, want := range map[int]bool{
		5173: true, 3000: true, 18081: true,
		0: false, -1: false, 65536: false,
		22: false, 5900: false, 6080: false, 8744: false, 8765: false, 9222: false, ListenPort: false,
	} {
		if got := ValidUpstreamPort(p); got != want {
			t.Errorf("ValidUpstreamPort(%d)=%v want %v", p, got, want)
		}
	}
}

func TestUpstreamSelector_CachesFileAndLiveness(t *testing.T) {
	fallback, _ := url.Parse("http://127.0.0.1:18080")
	file := filepath.Join(t.TempDir(), "upstream")
	writePort(t, file, "5173")
	now := time.Unix(1000, 0)
	dials := 0
	alive := true
	s := newUpstreamSelector(fallback, file)
	s.now = func() time.Time { return now }
	s.alive = func(string) bool { dials++; return alive }

	if got := s.pick().Host; got != "127.0.0.1:5173" {
		t.Fatalf("pick=%s", got)
	}
	alive = false
	if got := s.pick().Host; got != "127.0.0.1:5173" || dials != 1 {
		t.Fatalf("liveness must be cached within TTL: pick=%s dials=%d", got, dials)
	}
	now = now.Add(upstreamAliveTTL + time.Millisecond)
	if got := s.pick().Host; got != "127.0.0.1:18080" || dials != 2 {
		t.Fatalf("expired liveness must re-dial and fall back: pick=%s dials=%d", got, dials)
	}

	// A rewrite with the same size inside one mtime tick is still seen after the file TTL.
	alive = true
	writePort(t, file, "5174")
	now = now.Add(upstreamFileTTL + upstreamAliveTTL)
	if got := s.pick().Host; got != "127.0.0.1:5174" {
		t.Fatalf("pick after rewrite=%s", got)
	}
}

func TestUpstreamSelector_NoFileUsesFallback(t *testing.T) {
	fallback, _ := url.Parse("http://127.0.0.1:18080")
	if got := newUpstreamSelector(fallback, "").pick(); got != fallback {
		t.Fatalf("no file configured: %v", got)
	}
	s := newUpstreamSelector(fallback, filepath.Join(t.TempDir(), "missing"))
	if got := s.pick(); got != fallback {
		t.Fatalf("missing file: %v", got)
	}
	file := filepath.Join(t.TempDir(), "upstream")
	writePort(t, file, "18080")
	s = newUpstreamSelector(fallback, file)
	s.alive = func(string) bool { t.Fatal("same port as fallback must not dial"); return false }
	if got := s.pick(); got != fallback {
		t.Fatalf("file naming the fallback port: %v", got)
	}
}

func TestDialAlive(t *testing.T) {
	srv, _ := namedServer(t, "x")
	u, _ := url.Parse(srv.URL)
	if !dialAlive(u.Host) {
		t.Fatal("listening server must be alive")
	}
	srv.Close()
	if dialAlive(u.Host) {
		t.Fatal("closed server must not be alive")
	}
}
