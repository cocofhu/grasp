package driver

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func coldArchive(t *testing.T, files map[string]string, extra *tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o600, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := tw.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
		if extra.Typeflag == tar.TypeReg && extra.Size > 0 {
			if _, err := tw.Write(make([]byte, extra.Size)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func coldSum(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

type coldCommandFixture struct {
	opts                                    StartOptions
	root, stage, workspace, control, output string
	logicalRoot                             string
	config, git                             []byte
	server                                  *httptest.Server
	requests                                atomic.Int32
	bad                                     atomic.Bool
	archives                                atomic.Pointer[coldCommandArchives]
}

type coldCommandArchives struct{ config, git []byte }

func coldCommand(t *testing.T) *coldCommandFixture {
	t.Helper()
	base := t.TempDir()
	f := &coldCommandFixture{root: filepath.Join(base, "config"), stage: filepath.Join(base, "git-stage"), workspace: filepath.Join(base, "workspace"), control: filepath.Join(base, "runtime", "control"), output: filepath.Join(base, "backend-started")}
	f.logicalRoot = f.root
	if !strings.HasPrefix(base, "/root/") {
		f.logicalRoot = "/root/grasp-cold-unit-fixture/config"
	}
	if err := os.MkdirAll(f.workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.workspace, "dirty.bin"), []byte{0, 1, 255, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	f.config = coldArchive(t, map[string]string{"mcp.json": `{"mcpServers":{"current":{"url":"https://current.invalid"}}}`, "rules/current.md": "current rules"}, nil)
	f.git = coldArchive(t, map[string]string{"git/manifest.json": "[]"}, nil)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-private-header" {
			w.WriteHeader(401)
			return
		}
		if f.bad.Load() {
			w.WriteHeader(503)
			return
		}
		archives := f.archives.Load()
		if archives == nil {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/config":
			_, _ = w.Write(archives.config)
		case "/git":
			_, _ = w.Write(archives.git)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	f.opts = StartOptions{BootstrapScript: "#!/bin/bash\nprintf launched > \"$GRASP_COLD_TEST_OUTPUT\"\n", RuntimeURL: f.server.URL + "/runtime", RuntimeHeaders: "Authorization: Bearer synthetic-private-header", ColdConfig: &ColdStartConfig{RequestID: "owned-cold-request-01", ExpectedNativeIdentity: "original-native-identity", InputSHA256: strings.Repeat("a", 64)}}
	f.opts.ColdConfig.Env = map[string]string{"CONFIG_ROOT": f.logicalRoot, "WORKSPACE_DIR": f.workspace, "GRASP_WORKSPACE_CONTROL_ROOT": f.control, "GRASP_COLD_REQUEST_ID": f.opts.ColdConfig.RequestID, "GRASP_COLD_TEST_OUTPUT": f.output, "ROOT_PASSWORD": "synthetic-private-password", "ACP_BRIDGE_PASSWORD": "synthetic-private-password", "GRASP_RUNTIME_URL": f.opts.RuntimeURL, "SANDBOX_INJECT_HEADERS": f.opts.RuntimeHeaders, "SANDBOX_INJECT": "", "GIT_REPOS": ""}
	f.refresh()
	return f
}

func (f *coldCommandFixture) refresh() {
	f.archives.Store(&coldCommandArchives{config: f.config, git: f.git})
	f.opts.ColdConfig.Injections = []ColdConfigInjection{{URL: f.server.URL + "/config", Destination: f.logicalRoot, SHA256: coldSum(f.config), Bytes: int64(len(f.config))}, {URL: f.server.URL + "/git", Destination: "/tmp/grasp-ssh-inject", SHA256: coldSum(f.git), Bytes: int64(len(f.git))}}
	f.opts.ColdConfig.PayloadSHA256 = ColdStartPayloadSHA256(f.opts)
}

func (f *coldCommandFixture) execute(t *testing.T) error {
	t.Helper()
	script, err := BuildColdStartCommand(f.opts)
	if err != nil {
		return err
	}
	if strings.Contains(script, "synthetic-private-header") || strings.Contains(script, "synthetic-private-password") || strings.Contains(script, f.server.URL) {
		t.Fatal("secret or private URL entered executable command")
	}
	// Test-only relocation avoids touching either platform-private destination
	// on the host. CI users without /root access still execute the real verifier
	// instead of skipping its subprocess/HTTP/cache checks. The command builder
	// continues to validate the original /root-only production contract.
	script = strings.ReplaceAll(script, "/tmp/grasp-ssh-inject", f.stage)
	if f.logicalRoot != f.root {
		prefix, err := json.Marshal(filepath.Dir(f.root) + "/")
		if err != nil {
			t.Fatal(err)
		}
		script = strings.ReplaceAll(script, "str(root).startswith('/root/')", "str(root).startswith("+string(prefix)+")")
	}
	descriptors := append([]ColdConfigInjection(nil), f.opts.ColdConfig.Injections...)
	for i := range descriptors {
		if descriptors[i].Destination == "/tmp/grasp-ssh-inject" {
			descriptors[i].Destination = f.stage
		} else if descriptors[i].Destination == f.logicalRoot {
			descriptors[i].Destination = f.root
		}
	}
	raw, err := json.Marshal(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/bash", "-c", script, "grasp-cold", "fetch")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "GRASP_COLD_INJECTIONS=" + string(raw)}
	for key, value := range f.opts.ColdConfig.Env {
		if key == "CONFIG_ROOT" && value == f.logicalRoot {
			value = f.root
		}
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if bytes.Contains(output, []byte("synthetic-private")) {
		t.Fatal("failed verifier exposed credentials")
	}
	return err
}

func TestColdBootstrapStrictReadbackAndVerifiedCacheSurviveExpiredURLs(t *testing.T) {
	f := coldCommand(t)
	if err := f.execute(t); err != nil {
		t.Fatal("valid exact configuration did not start", err)
	}
	if raw, err := os.ReadFile(f.output); err != nil || string(raw) != "launched" {
		t.Fatal("bootstrap was not executed", err)
	}
	if raw, err := os.ReadFile(filepath.Join(f.root, "rules/current.md")); err != nil || string(raw) != "current rules" {
		t.Fatal("current config missing", err)
	}
	if raw, err := os.ReadFile(filepath.Join(f.stage, "git/manifest.json")); err != nil || string(raw) != "[]" {
		t.Fatal("explicit Git revoke manifest missing", err)
	}
	requests := f.requests.Load()
	if requests != 2 {
		t.Fatal("incomplete or repeated download", requests)
	}
	f.server.Close()
	if err := os.Remove(f.output); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.stage); err != nil {
		t.Fatal(err)
	}
	if err := f.execute(t); err != nil {
		t.Fatal("ordinary cold restart depended on expired one-time URLs", err)
	}
	if f.requests.Load() != requests {
		t.Fatal("verified cache still attempted network")
	}
	if raw, err := os.ReadFile(filepath.Join(f.workspace, "dirty.bin")); err != nil || !bytes.Equal(raw, []byte{0, 1, 255, 3}) {
		t.Fatal("workspace files changed", err)
	}
}

func TestColdBootstrapFailuresNeverExecuteOrPartiallyReplaceConfig(t *testing.T) {
	for _, kind := range []string{"http", "sha", "size", "traversal", "symlink", "hardlink", "missing_config", "bad_git", "missing_git", "quota", "file_ancestor", "workspace_overlap", "staging_symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := coldCommand(t)
			if err := os.MkdirAll(f.root, 0o700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(f.root, "existing-platform-data")
			if err := os.WriteFile(original, []byte("preserve on failed preflight"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "http":
				f.bad.Store(true)
			case "traversal":
				f.config = coldArchive(t, map[string]string{"mcp.json": "{}", "../escaped": "bad"}, nil)
				f.refresh()
			case "symlink":
				f.config = coldArchive(t, map[string]string{"mcp.json": "{}"}, &tar.Header{Name: "linked", Typeflag: tar.TypeSymlink, Linkname: f.workspace, Mode: 0o700})
				f.refresh()
			case "hardlink":
				f.git = coldArchive(t, map[string]string{"git/manifest.json": "[]"}, &tar.Header{Name: "linked", Typeflag: tar.TypeLink, Linkname: "git/manifest.json", Mode: 0o600})
				f.refresh()
			case "missing_config":
				f.config = coldArchive(t, map[string]string{"other": "absent manifest"}, nil)
				f.refresh()
			case "bad_git":
				f.git = coldArchive(t, map[string]string{"git/manifest.json": "{}"}, nil)
				f.refresh()
			case "missing_git":
				f.git = coldArchive(t, map[string]string{"git/other": "[]"}, nil)
				f.refresh()
			case "quota":
				f.config = coldArchive(t, map[string]string{"mcp.json": "{}"}, &tar.Header{Name: "oversized", Typeflag: tar.TypeReg, Size: 17 << 20, Mode: 0o600})
				f.refresh()
			case "file_ancestor":
				f.config = coldArchive(t, map[string]string{"mcp.json": "{}", "conflict": "file", "conflict/nested": "not a directory"}, nil)
				f.refresh()
			case "workspace_overlap":
				f.opts.ColdConfig.Env["WORKSPACE_DIR"] = f.root
				f.opts.ColdConfig.PayloadSHA256 = ColdStartPayloadSHA256(f.opts)
			case "staging_symlink":
				if err := os.Symlink(f.workspace, f.stage); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "sha" {
				f.opts.ColdConfig.Injections[0].SHA256 = strings.Repeat("f", 64)
				f.opts.ColdConfig.PayloadSHA256 = ColdStartPayloadSHA256(f.opts)
			}
			if kind == "size" {
				f.opts.ColdConfig.Injections[0].Bytes++
				f.opts.ColdConfig.PayloadSHA256 = ColdStartPayloadSHA256(f.opts)
			}
			if err := f.execute(t); err == nil {
				t.Fatal("invalid configuration reached bootstrap")
			}
			if _, err := os.Stat(f.output); !os.IsNotExist(err) {
				t.Fatal("backend executed after failed proof", err)
			}
			if raw, err := os.ReadFile(original); err != nil || string(raw) != "preserve on failed preflight" {
				t.Fatal("failed validation modified original private configuration", err)
			}
			cache := filepath.Join(filepath.Dir(f.control), "authorization", f.opts.ColdConfig.RequestID)
			if _, err := os.Stat(cache); !os.IsNotExist(err) {
				t.Fatal("invalid configuration published a canonical cache", err)
			}
		})
	}
}

func TestColdBootstrapCacheIdentityAndSymlinksCannotBeAdopted(t *testing.T) {
	for _, kind := range []string{"intent", "archive_link", "cache_link", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			f := coldCommand(t)
			if err := f.execute(t); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(f.output); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(filepath.Dir(f.control), "authorization", f.opts.ColdConfig.RequestID)
			switch kind {
			case "intent":
				if err := os.WriteFile(filepath.Join(cache, "intent.json"), []byte(`{"requestId":"another-operation"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "archive_link":
				path := filepath.Join(cache, "archive-0.tgz")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.workspace, "dirty.bin"), path); err != nil {
					t.Fatal(err)
				}
			case "cache_link":
				if err := os.RemoveAll(cache); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(f.workspace, cache); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(cache, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			requests := f.requests.Load()
			if err := f.execute(t); err == nil {
				t.Fatal("untrusted cache reached bootstrap")
			}
			if f.requests.Load() != requests {
				t.Fatal("untrusted existing cache was replaced by another download")
			}
			if _, err := os.Stat(f.output); !os.IsNotExist(err) {
				t.Fatal("untrusted cache executed backend", err)
			}
		})
	}
}

func TestColdBootstrapCorruptPublishedCacheFailsWithoutNetworkOrExecution(t *testing.T) {
	f := coldCommand(t)
	if err := f.execute(t); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.output); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(f.control), "authorization", f.opts.ColdConfig.RequestID, "archive-0.tgz")
	if err := os.WriteFile(archive, []byte("corrupt cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := f.requests.Load()
	if err := f.execute(t); err == nil {
		t.Fatal("corrupt cache executed bootstrap")
	}
	if f.requests.Load() != before {
		t.Fatal("corrupt published cache was silently overwritten")
	}
	if _, err := os.Stat(f.output); !os.IsNotExist(err) {
		t.Fatal("bad cache started backend", err)
	}
}

func TestColdBootstrapRejectsLoaderHooksBeforeMutation(t *testing.T) {
	for _, key := range []string{"BASH_ENV", "ENV", "LD_PRELOAD", "LD_AUDIT"} {
		t.Run(key, func(t *testing.T) {
			f := coldCommand(t)
			f.opts.ColdConfig.Env[key] = "private-hook"
			f.opts.ColdConfig.PayloadSHA256 = ColdStartPayloadSHA256(f.opts)
			if _, err := BuildColdStartCommand(f.opts); err == nil {
				t.Fatal("pre-proof loader hook accepted")
			}
			if f.requests.Load() != 0 {
				t.Fatal("invalid preflight touched native IO")
			}
		})
	}
}
