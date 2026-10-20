package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/previewinject"
)

func TestParseOptions_Flags(t *testing.T) {
	opt, err := parseOptions([]string{
		"-listen", ":17980",
		"-upstream", "127.0.0.1:18080",
		"-script-url", "http://app.example/preview-pick.js",
	}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if opt.Listen != ":17980" {
		t.Fatalf("listen=%q", opt.Listen)
	}
	if opt.Upstream != "http://127.0.0.1:18080" {
		t.Fatalf("upstream=%q", opt.Upstream)
	}
	if opt.ScriptURL != "http://app.example/preview-pick.js" {
		t.Fatalf("script=%q", opt.ScriptURL)
	}
}

func TestParseOptions_EnvFallback(t *testing.T) {
	env := map[string]string{
		"PREVIEW_PORT":            "18100",
		"PREVIEW_PICK_SCRIPT_URL": "http://h/preview-pick.js",
	}
	opt, err := parseOptions(nil, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if opt.Listen != fmt.Sprintf(":%d", previewinject.ListenPort) {
		t.Fatalf("default listen=%q", opt.Listen)
	}
	if opt.Upstream != "http://127.0.0.1:18100" {
		t.Fatalf("upstream from PREVIEW_PORT=%q", opt.Upstream)
	}
	if opt.ScriptURL != previewinject.ScriptPath {
		t.Fatalf("env pick URL must not override same-origin script: %q", opt.ScriptURL)
	}
}

func TestParseOptions_DefaultSameOriginScript(t *testing.T) {
	opt, err := parseOptions(nil, func(k string) string {
		if k == "PREVIEW_PORT" {
			return "18100"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if opt.ScriptURL != previewinject.ScriptPath {
		t.Fatalf("default script=%q", opt.ScriptURL)
	}
}

func TestParseOptions_FlagBeatsEnv(t *testing.T) {
	env := map[string]string{
		"PREVIEW_INJECT_LISTEN":   ":1111",
		"PREVIEW_INJECT_UPSTREAM": "http://127.0.0.1:2222",
		"PREVIEW_PICK_SCRIPT_URL": "http://env/preview-pick.js",
		"PREVIEW_PORT":            "3333",
	}
	opt, err := parseOptions([]string{
		"-listen", ":17980",
		"-upstream", "http://127.0.0.1:18080",
		"-script-url", "http://flag/preview-pick.js",
	}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if opt.Listen != ":17980" || opt.Upstream != "http://127.0.0.1:18080" || opt.ScriptURL != "http://flag/preview-pick.js" {
		t.Fatalf("%+v", opt)
	}
}

func TestParseOptions_UpstreamFile(t *testing.T) {
	opt, err := parseOptions([]string{"-upstream-file", "/tmp/x/upstream"}, func(k string) string {
		if k == "PREVIEW_PORT" {
			return "18080"
		}
		return ""
	})
	if err != nil || opt.UpstreamFile != "/tmp/x/upstream" {
		t.Fatalf("flag: %+v err=%v", opt, err)
	}
	env := map[string]string{"PREVIEW_PORT": "18080", "PREVIEW_INJECT_UPSTREAM_FILE": "/env/upstream"}
	opt, err = parseOptions(nil, func(k string) string { return env[k] })
	if err != nil || opt.UpstreamFile != "/env/upstream" {
		t.Fatalf("env: %+v err=%v", opt, err)
	}
}

func TestParseOptions_Missing(t *testing.T) {
	if _, err := parseOptions(nil, func(string) string { return "" }); err == nil {
		t.Fatal("expected error")
	}
	env := map[string]string{"PREVIEW_PICK_SCRIPT_URL": "http://x/preview-pick.js"}
	if _, err := parseOptions(nil, func(k string) string { return env[k] }); err == nil {
		t.Fatal("missing upstream")
	}
}

func TestParseOptions_RejectSamePort(t *testing.T) {
	_, err := parseOptions([]string{
		"-listen", ":18080",
		"-upstream", "http://127.0.0.1:18080",
		"-script-url", "http://x/preview-pick.js",
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("listen==upstream must fail")
	}
}

func TestBinary_InjectsHTML(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "preview-inject")
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var seenHost string
	up := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHost = r.Host
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>live</body></html>"))
	})}
	go func() { _ = up.Serve(upLn) }()
	t.Cleanup(func() { _ = up.Close() })

	injLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	injAddr := injLn.Addr().String()
	_ = injLn.Close()

	cmd := exec.Command(exe,
		"-listen", injAddr,
		"-upstream", "http://"+upLn.Addr().String(),
		"-script-url", "http://app.example/preview-pick.js",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	var resp *http.Response
	deadline := time.Now().Add(3 * time.Second)
	for {
		req, _ := http.NewRequest(http.MethodGet, "http://"+injAddr+"/", nil)
		req.Host = "198.51.100.9:18080"
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("proxy never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "preview-pick.js") || !strings.Contains(string(body), "live") {
		t.Fatalf("body=%s", body)
	}
	if seenHost != "198.51.100.9:18080" {
		t.Fatalf("upstream Host=%q", seenHost)
	}
}
