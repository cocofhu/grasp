package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func TestPackConfigHomeTarGz(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "rules", "base.md"), []byte("# base"), 0o644)

	gz, err := PackConfigHomeTarGz(dir)
	if err != nil {
		t.Fatal(err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	names := map[string]bool{}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[strings.TrimSuffix(hdr.Name, "/")] = true
	}
	if !names["mcp.json"] || !names["rules/base.md"] {
		t.Fatalf("names=%v", names)
	}
}

func TestBundleStoreAuth(t *testing.T) {
	s := NewBundleStore()
	id, token := s.Put([]byte("hello-tgz"), DefaultInjectBundleTTL)
	if id == "" || token == "" {
		t.Fatal("empty id/token")
	}
	if _, ok := s.Get(id, "wrong"); ok {
		t.Fatal("wrong token accepted")
	}
	data, ok := s.Get(id+".tgz", token)
	if !ok || string(data) != "hello-tgz" {
		t.Fatalf("get = %q ok=%v", data, ok)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sandbox-inject/", func(w http.ResponseWriter, r *http.Request) {
		s.ServeHTTP(w, r, strings.TrimPrefix(r.URL.Path, "/sandbox-inject/"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/sandbox-inject/"+id+".tgz", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello-tgz" {
		t.Fatalf("body=%q", body)
	}
}

func TestManagerCreateUsesSANDBOXInject(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://api.example.com"}})

	gw, fg := newInlineGW(t)
	store := NewBundleStore()
	m := NewManager(gw, ManagerOptions{
		Image:           "img:test",
		WorkspaceDir:    "/root/workspace",
		InstallHelpers:  false, // no SSH; inject only
		InjectStore:     store,
		InjectAdvertise: "http://api.example.com",
	})

	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{"a":{"url":"http://x"}}}`), 0o644)
	_ = os.WriteFile(filepath.Join(home, "rules", "base.md"), []byte("rule"), 0o644)

	sb, err := m.Create(context.Background(), Spec{
		Name:       "grasp-sb-inj",
		ConfigHome: home,
		ConfigRoot: "/root/.cursor",
		Env: map[string]string{
			"GITHUB_TOKEN":    "gh-tok",
			"GITLAB_TOKEN":    "gl-tok",
			BridgePasswordEnv: testBridgePassword,
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.ConfigRoot != "/root/.cursor" {
		t.Errorf("ConfigRoot=%q", sb.ConfigRoot)
	}
	env, _ := fg.lastCreate["env"].(map[string]any)
	if env == nil {
		t.Fatalf("create missing env: %+v", fg.lastCreate)
	}
	inj, _ := env["SANDBOX_INJECT"].(string)
	if !strings.Contains(inj, "http://api.example.com/sandbox-inject/") || !strings.Contains(inj, ".tgz|/root/.cursor") {
		t.Fatalf("SANDBOX_INJECT=%q", inj)
	}
	if strings.Contains(inj, ",") {
		t.Fatalf("config-only inject should be single part, got %q", inj)
	}
	hdr, _ := env["SANDBOX_INJECT_HEADERS"].(string)
	if !strings.HasPrefix(hdr, "Authorization: Bearer ") {
		t.Fatalf("SANDBOX_INJECT_HEADERS=%q", hdr)
	}
	if env["GITHUB_TOKEN"] != "gh-tok" || env["GITLAB_TOKEN"] != "gl-tok" {
		t.Fatalf("token env lost: %+v", env)
	}
	if _, ok := env["GIT_SSH_PRIVATE_KEY"]; ok {
		t.Fatal("GIT_SSH_PRIVATE_KEY must not appear in create env")
	}
	if _, ok := env["GIT_SSH_KNOWN_HOSTS"]; ok {
		t.Fatal("GIT_SSH_KNOWN_HOSTS must not appear in create env")
	}
	// Prefer Env inject; gateway config.bundleUrl should stay unset.
	if cfg, _ := fg.lastCreate["config"].(map[string]any); cfg != nil {
		t.Fatalf("expected no gateway config inject when SANDBOX_INJECT used, got %+v", cfg)
	}
}

func TestManagerCreateSSHAndConfigMultiInject(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://api.example.com"}})

	gw, fg := newInlineGW(t)
	store := NewBundleStore()
	m := NewManager(gw, ManagerOptions{
		Image:           "img:test",
		WorkspaceDir:    "/root/workspace",
		InstallHelpers:  false,
		InjectStore:     store,
		InjectAdvertise: "http://api.example.com",
	})

	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{}`), 0o644)

	spec := Spec{
		Name:       "grasp-sb-ssh",
		ConfigHome: home,
		ConfigRoot: "/root/.cursor",
		Env: map[string]string{
			"GITHUB_TOKEN":        "gh",
			"GITLAB_TOKEN":        "gl",
			"GIT_SSH_PRIVATE_KEY": "should-strip",
			"GIT_SSH_KNOWN_HOSTS": "should-strip",
			BridgePasswordEnv:     testBridgePassword,
		},
	}
	ApplySSHCredentials(&spec, "-----BEGIN KEY-----\nk\n-----END KEY-----", "host ssh-ed25519 AAAA")

	if _, err := m.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	env, _ := fg.lastCreate["env"].(map[string]any)
	inj, _ := env["SANDBOX_INJECT"].(string)
	parts := strings.Split(inj, ",")
	if len(parts) != 2 {
		t.Fatalf("want SSH+ConfigHome two parts, got %q", inj)
	}
	if !strings.HasSuffix(parts[0], "|/tmp/grasp-ssh-inject") {
		t.Fatalf("SSH staging first: %q", parts[0])
	}
	if !strings.HasSuffix(parts[1], "|/root/.cursor") {
		t.Fatalf("ConfigHome second: %q", parts[1])
	}
	if env["GITHUB_TOKEN"] != "gh" || env["GITLAB_TOKEN"] != "gl" {
		t.Fatalf("tokens must remain: %+v", env)
	}
	if _, ok := env["GIT_SSH_PRIVATE_KEY"]; ok {
		t.Fatal("stripped SSH env leaked")
	}
	hdr, _ := env["SANDBOX_INJECT_HEADERS"].(string)
	if !strings.HasPrefix(hdr, "Authorization: Bearer ") {
		t.Fatalf("headers=%q", hdr)
	}
}
