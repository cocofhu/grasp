package previewinject

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func embedProxy(t *testing.T, grasp http.HandlerFunc, token string) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("app"))
	}))
	t.Cleanup(up.Close)
	g := httptest.NewServer(grasp)
	t.Cleanup(g.Close)
	u, _ := url.Parse(up.URL)
	p := httptest.NewServer(NewHandlerWithEmbed(u, scriptURL, EmbedLookup{RunURL: g.URL + "/mcp/runs/run-1", Token: token}))
	t.Cleanup(p.Close)
	return p
}

func getEmbed(t *testing.T, base, query string) (int, embedOrigin) {
	t.Helper()
	resp, err := http.Get(base + EmbedOriginPath + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got embedOrigin
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, got
}

func TestEmbedOriginAsksGraspWithRunToken(t *testing.T) {
	var seen *http.Request
	p := embedProxy(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.Header.Get("Authorization") != "Bearer run-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(embedOrigin{Origin: "https://grasp.example", RunID: "run-1", NodeID: r.URL.Query().Get("nodeId")})
	}, "run-token")

	code, got := getEmbed(t, p.URL, "?ticket=tk&node=ap1")
	if code != http.StatusOK || got.Origin != "https://grasp.example" || got.NodeID != "ap1" {
		t.Fatalf("code=%d got=%+v", code, got)
	}
	if seen.URL.Path != "/mcp/runs/run-1/embed-origin" || seen.URL.Query().Get("ticket") != "tk" {
		t.Fatalf("upstream request %s", seen.URL)
	}
}

func TestEmbedOriginRejects(t *testing.T) {
	origin := "https://grasp.example"
	node := "ap1"
	status := http.StatusOK
	p := embedProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(embedOrigin{Origin: origin, RunID: "run-1", NodeID: node})
	}, "run-token")

	if code, _ := getEmbed(t, p.URL, "?node=ap1"); code != http.StatusNotFound {
		t.Fatalf("missing ticket: %d", code)
	}
	status = http.StatusNotFound
	if code, _ := getEmbed(t, p.URL, "?ticket=tk&node=ap1"); code != http.StatusNotFound {
		t.Fatalf("grasp 404: %d", code)
	}
	status = http.StatusOK
	node = "other"
	if code, _ := getEmbed(t, p.URL, "?ticket=tk&node=ap1"); code != http.StatusNotFound {
		t.Fatalf("node mismatch: %d", code)
	}
	node = "ap1"
	for _, bad := range []string{"javascript:alert(1)", "https://grasp.example/path", "grasp.example", ""} {
		origin = bad
		if code, _ := getEmbed(t, p.URL, "?ticket=tk&node=ap1"); code != http.StatusNotFound {
			t.Fatalf("origin %q: %d", bad, code)
		}
	}
}

func TestEmbedOriginWithoutRunCredentials(t *testing.T) {
	called := false
	p := embedProxy(t, func(http.ResponseWriter, *http.Request) { called = true }, "")
	if code, _ := getEmbed(t, p.URL, "?ticket=tk&node=ap1"); code != http.StatusNotFound || called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}
