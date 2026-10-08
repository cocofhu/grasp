package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// inlineGW is a minimal in-test fake of the sandbox-gateway control plane. The
// sandbox package's own tests are internal (package sandbox) and cannot import
// the sandboxtest helper without an import cycle, so this compact version lives
// here.
type inlineGW struct {
	mu         sync.Mutex
	recs       map[string]map[string]any
	seq        int
	lastCreate map[string]any
	failCreate bool
}

func newInlineGW(t *testing.T) (*GatewayClient, *inlineGW) {
	t.Helper()
	fg := &inlineGW{recs: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(srv.Close)
	return NewGatewayClient(srv.URL, ""), fg
}

func (fg *inlineGW) endpoints() map[string]string {
	return map[string]string{
		"session": "127.0.0.1:34567",
		"ide":     "127.0.0.1:34568",
		"ssh":     "127.0.0.1:2222",
	}
}

func (fg *inlineGW) seed(id, status string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.recs[id] = map[string]any{
		"id": id, "name": id, "status": status,
		"endpoints": fg.endpoints(), "labels": map[string]string{managedByLabel: "1"},
	}
}

func (fg *inlineGW) handle(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/sandboxes"), "/")
	parts := strings.Split(p, "/")
	enc := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	fg.mu.Lock()
	defer fg.mu.Unlock()
	switch {
	case p == "" && r.Method == http.MethodPost:
		if fg.failCreate {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fg.lastCreate = body
		fg.seq++
		id := "gw-" + itoa(fg.seq)
		labels := map[string]string{managedByLabel: "1"}
		if bl, ok := body["labels"].(map[string]any); ok {
			for k, v := range bl {
				if s, ok := v.(string); ok {
					labels[k] = s
				}
			}
		}
		rec := map[string]any{"id": id, "name": id, "status": "running",
			"endpoints": fg.endpoints(), "labels": labels}
		fg.recs[id] = rec
		enc(rec)
	case p == "" && r.Method == http.MethodGet:
		out := []map[string]any{}
		for _, rec := range fg.recs {
			out = append(out, rec)
		}
		enc(map[string]any{"sandboxes": out})
	case len(parts) == 1 && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		enc(rec)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		delete(fg.recs, parts[0])
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		enc(map[string]any{"status": rec["status"]})
	case len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		content, _ := rec["logs"].(string)
		enc(map[string]string{"content": content})
	case len(parts) == 3 && parts[1] == "hosts" && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		enc(map[string]string{"address": "127.0.0.1:" + parts[2]})
	default:
		http.NotFound(w, r)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestNewContainerNameAndShellQuote(t *testing.T) {
	n1, n2 := NewContainerName(), NewContainerName()
	if !strings.HasPrefix(n1, containerPrefix) || n1 == n2 {
		t.Errorf("container names not unique/prefixed: %q %q", n1, n2)
	}
	if shellQuote("a'b") != `'a'\''b'` {
		t.Errorf("shellQuote = %q", shellQuote("a'b"))
	}
}

func TestManagerCreateInjectsEnvAndLabels(t *testing.T) {
	gw, fg := newInlineGW(t)
	m := NewManager(gw, ManagerOptions{Image: "img:test", WorkspaceDir: "/root/workspace"})
	sb, err := m.Create(context.Background(), Spec{
		Name: "grasp-sb-abc",
		Env:  map[string]string{"GIT_REPOS": "web|https://x/y.git", "K": "V", BridgePasswordEnv: testBridgePassword},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.Password != testBridgePassword {
		t.Errorf("password = %q", sb.Password)
	}
	if sb.Port != 34567 || sb.Host != "127.0.0.1" {
		t.Errorf("session endpoint = %+v", sb)
	}
	if sb.SSHPort != 2222 {
		t.Errorf("ssh port = %d", sb.SSHPort)
	}
	if sb.ID != sb.Name || sb.ID == "" {
		t.Errorf("id/name = %q/%q", sb.ID, sb.Name)
	}
	// Default enables DinD (SKIP_INNER_DOCKER=0); Spec.Env can set 1 to skip.
	env, _ := fg.lastCreate["env"].(map[string]any)
	if env["SKIP_INNER_DOCKER"] != "0" {
		t.Errorf("SKIP_INNER_DOCKER = %v, want 0", env["SKIP_INNER_DOCKER"])
	}
	if s, _ := env["SSH_KEY"].(string); strings.TrimSpace(s) == "" {
		t.Error("SSH_KEY not injected")
	}
	if env["GIT_REPOS"] != "web|https://x/y.git" {
		t.Errorf("GIT_REPOS not passed through: %v", env["GIT_REPOS"])
	}
	labels, _ := fg.lastCreate["labels"].(map[string]any)
	if labels[managedByLabel] != "1" || labels[cfNameLabel] != "grasp-sb-abc" {
		t.Errorf("labels = %v", labels)
	}
}

func TestManagerCreateAppliesDefaultMemory(t *testing.T) {
	prev := defaultMemoryMB.Load()
	t.Cleanup(func() { defaultMemoryMB.Store(prev) })
	SetDefaultMemoryMB(8192)

	gw, fg := newInlineGW(t)
	m := NewManager(gw, ManagerOptions{Image: "img:test"})
	if _, err := m.Create(context.Background(), Spec{Env: map[string]string{BridgePasswordEnv: testBridgePassword}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, _ := fg.lastCreate["resources"].(map[string]any)
	if res["memoryMB"] != float64(8192) {
		t.Fatalf("resources = %v, want memoryMB 8192", fg.lastCreate["resources"])
	}
}

func TestManagerCreateFailure(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.failCreate = true
	m := NewManager(gw, ManagerOptions{})
	if _, err := m.Create(context.Background(), Spec{Env: map[string]string{BridgePasswordEnv: "x"}}); err == nil {
		t.Fatal("expected create error")
	}
}

func TestManagerCreateRequiresBridgePassword(t *testing.T) {
	gw, _ := newInlineGW(t)
	m := NewManager(gw, ManagerOptions{})
	if _, err := m.Create(context.Background(), Spec{Env: map[string]string{BridgePasswordEnv: " "}}); err == nil || !strings.Contains(err.Error(), BridgePasswordEnv) {
		t.Fatalf("err=%v", err)
	}
}

func TestManagerStatusListAttachDestroy(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.seed("gw-a", "running")
	fg.seed("gw-b", "stopped")
	m := NewManager(gw, ManagerOptions{WorkspaceDir: "/root/workspace"})

	if st := m.Status(context.Background(), "gw-a"); st != "running" {
		t.Errorf("Status(gw-a) = %q", st)
	}
	if st := m.Status(context.Background(), "gw-b"); st != "exited" {
		t.Errorf("Status(gw-b) = %q (want exited)", st)
	}
	if st := m.Status(context.Background(), "gone"); st != "not_found" {
		t.Errorf("Status(gone) = %q", st)
	}

	ids, err := m.List(context.Background())
	if err != nil || len(ids) != 2 {
		t.Fatalf("List = %v, %v", ids, err)
	}
	statuses, err := m.ListStatuses(context.Background())
	if err != nil || statuses["gw-a"] != "running" || statuses["gw-b"] != "exited" {
		t.Fatalf("ListStatuses = %v, %v", statuses, err)
	}

	sb, err := m.Attach(context.Background(), "gw-a")
	if err != nil || sb.Port != 34567 {
		t.Fatalf("Attach = %+v, %v", sb, err)
	}
	if err := m.DestroyByName(context.Background(), "gw-a"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if st := m.Status(context.Background(), "gw-a"); st != "not_found" {
		t.Errorf("after destroy Status = %q", st)
	}
}

func TestManagerDataPlaneViaHook(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.seed("gw-x", "running")
	m := NewManager(gw, ManagerOptions{WorkspaceDir: "/root/workspace"})

	var gotCmds []string
	restore := SetExecHook(func(_ context.Context, host string, port int, command string, stdin io.Reader) ([]byte, error) {
		gotCmds = append(gotCmds, command)
		switch {
		case strings.Contains(command, "cat") && strings.Contains(command, "--"):
			return []byte("file contents"), nil
		case strings.Contains(command, "test") && strings.Contains(command, "-e"):
			return []byte(""), nil
		case stdin != nil:
			b, _ := io.ReadAll(stdin)
			return b, nil
		}
		return []byte("exec-out"), nil
	})
	defer restore()

	sb, err := m.Attach(context.Background(), "gw-x")
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	out, err := sb.Exec(context.Background(), 5*time.Second, "echo", "hi")
	if err != nil || out != "exec-out" {
		t.Errorf("Exec = %q, %v", out, err)
	}
	data, err := sb.ReadFile(context.Background(), "report.md")
	if err != nil || string(data) != "file contents" {
		t.Errorf("ReadFile = %q, %v", data, err)
	}
	if !sb.FileExists(context.Background(), "report.md") {
		t.Error("FileExists should be true")
	}
	if err := sb.WriteFile(context.Background(), "sub/out.txt", []byte("x")); err != nil {
		t.Errorf("WriteFile: %v", err)
	}
	// ReadFile of a relative path resolves against WORKSPACE_DIR.
	if !strings.Contains(strings.Join(gotCmds, "\n"), "/root/workspace/report.md") {
		t.Errorf("relative path not resolved against workspace: %v", gotCmds)
	}
	// Manager.Exec routes through the same hook.
	if o, err := m.Exec(context.Background(), "gw-x", 5*time.Second, "echo", "hey"); err != nil || o != "exec-out" {
		t.Errorf("Manager.Exec = %q, %v", o, err)
	}
}

func TestNormalizeGWStatus(t *testing.T) {
	cases := map[string]string{"running": "running", "stopped": "exited", "": "not_found", "pending": "pending"}
	for in, want := range cases {
		if got := normalizeGWStatus(in); got != want {
			t.Errorf("normalizeGWStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostPortOf(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPort int
	}{
		{"", "", 0},
		{"no-port", "no-port", 0},
		{":8080", "127.0.0.1", 8080},
		{"127.0.0.1:8765", "127.0.0.1", 8765},
		{"[::1]:443", "[::1]", 443},
	}
	for _, c := range cases {
		h, p := hostPortOf(c.in)
		if h != c.wantHost || p != c.wantPort {
			t.Errorf("hostPortOf(%q) = %q,%d want %q,%d", c.in, h, p, c.wantHost, c.wantPort)
		}
	}
	var nilSB *GWSandbox
	if nilSB.Endpoint("session") != "" {
		t.Error("nil GWSandbox.Endpoint should be empty")
	}
}

func TestSandboxACPHelper(t *testing.T) {
	sb := &Sandbox{Host: "127.0.0.1", Port: 8765}
	if c := sb.ACP(); c == nil || c.port != 8765 {
		t.Errorf("ACP = %+v", c)
	}
}

func TestSandboxDestroyNilSafe(t *testing.T) {
	var nilSB *Sandbox
	nilSB.Destroy(context.Background())        // no panic on nil
	(&Sandbox{}).Destroy(context.Background()) // no panic on nil mgr
}

func TestCopyTree(t *testing.T) {
	dst0 := t.TempDir()
	if err := copyTree(filepath.Join(t.TempDir(), "missing"), dst0); err != nil {
		t.Errorf("missing src should be nil, got %v", err)
	}
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(b) != "A" {
		t.Error("a.txt not copied")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "B" {
		t.Error("nested b.txt not copied")
	}
}
