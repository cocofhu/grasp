package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
)

func runtimeTgz(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	add := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("./scripts/startup.sh", "#!/bin/bash\n")
	if manifest != "" {
		add("./MANIFEST", manifest)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testVersion(c byte) string { return strings.Repeat(string(c), 64) }

func writeRuntime(t *testing.T, path, version string, minImage int) []byte {
	t.Helper()
	data := runtimeTgz(t, "version="+version+"\narch=amd64\nmin_image="+strconv.Itoa(minImage)+"\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseRuntimeManifest(t *testing.T) {
	v := testVersion('a')
	m, err := ParseRuntimeManifest(runtimeTgz(t, "version="+v+"\narch=amd64\nmin_image=3\n"))
	if err != nil || m.Version != v || m.Arch != "amd64" || m.MinImage != 3 {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
	for name, data := range map[string][]byte{
		"not gzip":      []byte("plain"),
		"no manifest":   runtimeTgz(t, ""),
		"bad version":   runtimeTgz(t, "version=abc\nmin_image=1\n"),
		"bad min_image": runtimeTgz(t, "version="+v+"\nmin_image=x\n"),
		"neg min_image": runtimeTgz(t, "version="+v+"\nmin_image=-1\n"),
		"truncated tar": func() []byte {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			_, _ = zw.Write([]byte("not a tar header at all"))
			_ = zw.Close()
			return buf.Bytes()
		}(),
	} {
		if _, err := ParseRuntimeManifest(data); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestRuntimeBundleCurrentReload(t *testing.T) {
	if _, _, err := NewRuntimeBundle("").Current(); err == nil {
		t.Fatal("empty path should error")
	}
	path := filepath.Join(t.TempDir(), "rt.tgz")
	r := NewRuntimeBundle(path)
	if r.Path() != path {
		t.Fatalf("path=%q", r.Path())
	}
	if _, _, err := r.Current(); err == nil || !strings.Contains(err.Error(), "build-sandbox-runtime.sh") {
		t.Fatalf("missing bundle err=%v", err)
	}

	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }
	first := writeRuntime(t, path, testVersion('a'), 1)
	data, man, err := r.Current()
	if err != nil || man.Version != testVersion('a') || !bytes.Equal(data, first) {
		t.Fatalf("first load man=%+v err=%v", man, err)
	}

	writeRuntime(t, path, testVersion('b'), 1)
	_ = os.Chtimes(path, now.Add(time.Hour), now.Add(time.Hour))
	if _, man, _ = r.Current(); man.Version != testVersion('a') {
		t.Fatal("reload must be throttled")
	}
	now = now.Add(runtimeReloadInterval)
	if _, man, _ = r.Current(); man.Version != testVersion('b') {
		t.Fatalf("after interval want b, got %s", man.Version)
	}

	// Unchanged file: re-stat but keep the parsed copy.
	now = now.Add(runtimeReloadInterval)
	if _, man, _ = r.Current(); man.Version != testVersion('b') {
		t.Fatal("unchanged file should keep version")
	}

	// Broken or removed file keeps serving the last good bundle.
	_ = os.WriteFile(path, []byte("garbage"), 0o644)
	now = now.Add(runtimeReloadInterval)
	if _, man, err = r.Current(); err != nil || man.Version != testVersion('b') {
		t.Fatalf("broken file: man=%+v err=%v", man, err)
	}
	_ = os.Remove(path)
	now = now.Add(runtimeReloadInterval)
	if _, man, err = r.Current(); err != nil || man.Version != testVersion('b') {
		t.Fatalf("removed file: man=%+v err=%v", man, err)
	}

	unreadable := filepath.Join(t.TempDir(), "dir.tgz")
	_ = os.Mkdir(unreadable, 0o755)
	if _, _, err := NewRuntimeBundle(unreadable).Current(); err == nil {
		t.Fatal("directory path should error")
	}
}

func TestRuntimeBundleState(t *testing.T) {
	var nilBundle *RuntimeBundle
	nilBundle.noteInstalled("x", "v")
	nilBundle.forget("x")

	r := NewRuntimeBundle("unused")
	r.noteInstalled("", "v")
	if r.upToDate("sb", "v") {
		t.Fatal("unknown sandbox is not up to date")
	}
	r.noteInstalled("sb", "v")
	if !r.upToDate("sb", "v") || r.upToDate("sb", "w") {
		t.Fatal("upToDate mismatch")
	}
	if _, ok := r.begin("sb"); !ok {
		t.Fatal("first begin should claim")
	}
	if _, ok := r.begin("sb"); ok {
		t.Fatal("second begin should be refused while in flight")
	}
	r.end("sb", runtimeState{version: "v", backendPending: true})
	if r.upToDate("sb", "v") {
		t.Fatal("pending backend is not up to date")
	}
	r.forget("sb")
	if st, _ := r.begin("sb"); st.version != "" {
		t.Fatalf("forgotten state should be empty, got %+v", st)
	}
}

func TestManagerCreateRuntimeURL(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://api.example.com"}})

	path := filepath.Join(t.TempDir(), "rt.tgz")
	data := writeRuntime(t, path, testVersion('c'), 1)
	rt := NewRuntimeBundle(path)

	gw, fg := newInlineGW(t)
	store := NewBundleStore()
	m := NewManager(gw, ManagerOptions{
		Image: "img:test", WorkspaceDir: "/root/workspace",
		InjectStore: store, InjectAdvertise: "http://api.example.com", Runtime: rt,
	})
	if m.Runtime() != rt {
		t.Fatal("Runtime() accessor")
	}
	sb, err := m.Create(context.Background(), Spec{Name: "grasp-sb-rt", Env: map[string]string{BridgePasswordEnv: testBridgePassword}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	env, _ := fg.lastCreate["env"].(map[string]any)
	u, _ := env["GRASP_RUNTIME_URL"].(string)
	if !strings.HasPrefix(u, "http://api.example.com/sandbox-inject/") || !strings.HasSuffix(u, ".tgz") {
		t.Fatalf("GRASP_RUNTIME_URL=%q", u)
	}
	if _, ok := env["SANDBOX_INJECT"]; ok {
		t.Fatalf("runtime alone must not set SANDBOX_INJECT: %+v", env)
	}
	hdr, _ := env["SANDBOX_INJECT_HEADERS"].(string)
	token := strings.TrimPrefix(hdr, "Authorization: Bearer ")
	if token == hdr || token == "" {
		t.Fatalf("headers=%q", hdr)
	}
	got, ok := store.Get(strings.TrimPrefix(u, "http://api.example.com/sandbox-inject/"), token)
	if !ok || !bytes.Equal(got, data) {
		t.Fatal("runtime bundle not served under the shared token")
	}
	if !rt.upToDate(sb.ID, testVersion('c')) {
		t.Fatal("created sandbox should be recorded at the served version")
	}

	// Runtime plus ConfigHome share one token; SANDBOX_INJECT keeps the config part only.
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{}`), 0o644)
	if _, err := m.Create(context.Background(), Spec{Name: "grasp-sb-rt2", ConfigHome: home, ConfigRoot: "/root/.cursor", Env: map[string]string{BridgePasswordEnv: testBridgePassword}}); err != nil {
		t.Fatal(err)
	}
	env, _ = fg.lastCreate["env"].(map[string]any)
	if inj, _ := env["SANDBOX_INJECT"].(string); strings.Contains(inj, ",") || !strings.HasSuffix(inj, "|/root/.cursor") {
		t.Fatalf("SANDBOX_INJECT=%q", inj)
	}
	if env["GRASP_RUNTIME_URL"] == nil {
		t.Fatal("GRASP_RUNTIME_URL missing next to config inject")
	}
}

func TestManagerCreateRuntimeErrors(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{})

	gw, _ := newInlineGW(t)
	missing := NewManager(gw, ManagerOptions{Image: "img:test", Runtime: NewRuntimeBundle(filepath.Join(t.TempDir(), "none.tgz"))})
	if _, err := missing.Create(context.Background(), Spec{Name: "a", Env: map[string]string{BridgePasswordEnv: testBridgePassword}}); err == nil || !strings.Contains(err.Error(), "runtime bundle") {
		t.Fatalf("missing bundle err=%v", err)
	}

	path := filepath.Join(t.TempDir(), "rt.tgz")
	writeRuntime(t, path, testVersion('d'), 1)
	noStore := NewManager(gw, ManagerOptions{Image: "img:test", Runtime: NewRuntimeBundle(path)})
	if _, err := noStore.Create(context.Background(), Spec{Name: "b", Env: map[string]string{BridgePasswordEnv: testBridgePassword}}); err == nil || !strings.Contains(err.Error(), "inject store") {
		t.Fatalf("no store err=%v", err)
	}
}
