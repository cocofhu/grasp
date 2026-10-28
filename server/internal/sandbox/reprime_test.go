package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func untarReprime(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[hdr.Name] = string(b)
	}
}

func reprimeSpec(t *testing.T) Spec {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "rules", "r.md"), []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Spec{
		Env: map[string]string{
			"AGENT_PROVIDER": "claude_code", "MULTI": "a\nb 'c'",
			"SANDBOX_INJECT": "x", "SSH_KEY": "k", "bad-name": "v",
		},
		ConfigHome:    home,
		ConfigRoot:    "/root/.claude",
		SSHPrivateKey: "PRIVATE",
	}
}

func TestPackReprimeBundle(t *testing.T) {
	spec := reprimeSpec(t)
	data, err := packReprimeBundle(spec, []string{"OLD", "AGENT_PROVIDER", "SSH_KEY", "bad name"})
	if err != nil {
		t.Fatal(err)
	}
	files := untarReprime(t, data)
	env := files["env"]
	for _, line := range strings.Split(strings.TrimSpace(env), "\n") {
		name, val, _ := strings.Cut(line, "=")
		raw, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			t.Fatalf("env %s not base64: %v", name, err)
		}
		if spec.Env[name] != string(raw) {
			t.Fatalf("env %s = %q", name, raw)
		}
	}
	for _, skipped := range []string{"SANDBOX_INJECT=", "SSH_KEY=", "bad-name="} {
		if strings.Contains(env, skipped) {
			t.Fatalf("env carries %s", skipped)
		}
	}
	if files["unset"] != "OLD\n" {
		t.Fatalf("unset = %q", files["unset"])
	}
	if files["config/rules/r.md"] != "rule" {
		t.Fatalf("config home not packed: %v", files)
	}
	if files["ssh/id_rsa"] != "PRIVATE\n" {
		t.Fatalf("ssh bundle not packed: %v", files)
	}
	for name := range files {
		if strings.HasPrefix(name, "ssh/repos/") {
			t.Fatalf("explicit repository credential was packed: %s", name)
		}
	}
}

func TestPackReprimeBundleMinimal(t *testing.T) {
	data, err := packReprimeBundle(Spec{Env: map[string]string{"A": "1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := untarReprime(t, data)
	if len(files) != 1 || files["env"] == "" {
		t.Fatalf("files = %v", files)
	}
	if _, err := packReprimeBundle(Spec{ConfigHome: filepath.Join(t.TempDir(), "missing")}, nil); err == nil {
		t.Fatal("missing config home packed")
	}
}

func TestSpecFingerprint(t *testing.T) {
	spec := reprimeSpec(t)
	a := SpecFingerprint(spec)
	if a == "" || a != SpecFingerprint(spec) {
		t.Fatal("fingerprint not stable")
	}
	// Plumbing keys do not count.
	spec.Env["SANDBOX_INJECT"] = "other"
	if SpecFingerprint(spec) != a {
		t.Fatal("plumbing env changed the fingerprint")
	}
	spec.Env["GRASP_NODE_ID"] = "n2"
	if SpecFingerprint(spec) == a {
		t.Fatal("env change not fingerprinted")
	}
	b := SpecFingerprint(spec)
	if err := os.WriteFile(filepath.Join(spec.ConfigHome, "rules", "r.md"), []byte("rule2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if SpecFingerprint(spec) == b {
		t.Fatal("config home change not fingerprinted")
	}
	spec.ConfigHome = filepath.Join(t.TempDir(), "missing")
	if SpecFingerprint(spec) != "" {
		t.Fatal("unreadable config home fingerprinted")
	}
}

func TestReprimeRunsScriptWithBundle(t *testing.T) {
	var gotCmd string
	var gotStdin []byte
	restore := SetExecHook(func(_ context.Context, _ string, _ int, cmd string, stdin io.Reader) ([]byte, error) {
		gotCmd = cmd
		gotStdin, _ = io.ReadAll(stdin)
		return []byte("reprime: ok"), nil
	})
	defer restore()
	m := &Manager{}
	sb := &Sandbox{ID: "sb", ConfigRoot: "/root/.cursor"}
	spec := Spec{Env: map[string]string{"AGENT_PROVIDER": "claude_code"}, ConfigRoot: "/root/.claude"}
	if err := m.Reprime(context.Background(), sb, spec, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotCmd, runtimeReprime) {
		t.Fatalf("cmd = %q", gotCmd)
	}
	if untarReprime(t, gotStdin)["env"] == "" {
		t.Fatal("bundle not sent on stdin")
	}
	if sb.ConfigRoot != "/root/.claude" {
		t.Fatalf("ConfigRoot = %q", sb.ConfigRoot)
	}
	if err := m.Reprime(context.Background(), nil, spec, nil); err == nil {
		t.Fatal("nil sandbox accepted")
	}
}

func TestReprimeReportsFailure(t *testing.T) {
	restore := SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("cloning\nreprime: clone a failed\n"), errors.New("exit 1")
	})
	defer restore()
	err := (&Manager{}).Reprime(context.Background(), &Sandbox{ID: "sb"}, Spec{}, nil)
	if err == nil || !strings.Contains(err.Error(), "clone a failed") || strings.Contains(err.Error(), "cloning") {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncBranch(t *testing.T) {
	var script string
	restore := SetExecHook(func(_ context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		b, _ := io.ReadAll(stdin)
		script = string(b)
		return []byte("switched\n"), nil
	})
	defer restore()
	sb := &Sandbox{ID: "sb"}
	note, err := sb.SyncBranch(context.Background(), "/root/workspace/app", "feat/x")
	if err != nil || note != "switched" {
		t.Fatalf("note=%q err=%v", note, err)
	}
	if !strings.Contains(script, "'feat/x'") || !strings.Contains(script, "dirty") {
		t.Fatalf("script = %s", script)
	}
	if _, err := sb.SyncBranch(context.Background(), "/root/workspace/app", "bad\nbranch"); err == nil {
		t.Fatal("unsafe branch accepted")
	}
	if _, err := sb.SyncBranch(context.Background(), "bad\ndir", "b"); err == nil {
		t.Fatal("unsafe dir accepted")
	}
}
