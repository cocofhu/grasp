package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestColdResumeRebuildsConfigurationGitAndRuntimeInputs(t *testing.T) {
	store := NewBundleStore()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{"current":{"url":"http://current/mcp"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(t.TempDir(), "runtime.tgz")
	runtimeData := writeRuntime(t, runtimePath, testVersion('b'), 2)
	started := 0
	var published []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var opts GWStartOptions
			if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
				t.Error(err)
			}
			started++
			if opts.RestoreEnv["AGENT_PROVIDER"] != "codex" || opts.RestoreEnv["CONFIG_ROOT"] != "/root/.codex" || opts.RestoreEnv["SANDBOX_INJECT"] == "" {
				t.Error("cold start lost the current tool or configuration")
			}
			if !slices.Contains(opts.ReplaceEnvKeys, "OLD_CUSTOM_KEY") || !slices.Contains(opts.ReplaceEnvKeys, "CURSOR_API_KEY") || slices.Contains(opts.ReplaceEnvKeys, "SSH_KEY") || slices.Contains(opts.ReplaceEnvKeys, "WORKSPACE_DIR") {
				t.Errorf("replacement names: %v", opts.ReplaceEnvKeys)
			}
			if _, ok := opts.RestoreEnv["WORKSPACE_DIR"]; ok {
				t.Error("cold start attempted to move the workspace")
			}
			token := strings.TrimPrefix(opts.RuntimeHeaders, "Authorization: Bearer ")
			for _, part := range strings.Split(opts.RestoreEnv["SANDBOX_INJECT"], ",") {
				pair := strings.SplitN(part, "|", 2)
				id := bundleIDFromURL(pair[0])
				published = append(published, id)
				data, ok := store.Get(id, token)
				if !ok {
					t.Error("configuration bundle is not downloadable at start")
					continue
				}
				files := untarReprime(t, data)
				if pair[1] == "/root/.codex" {
					if !strings.Contains(files["mcp.json"], "http://current/mcp") {
						t.Error("MCP configuration is missing")
					}
				} else if pair[1] == SSHInjectStagingDir {
					content, _ := json.Marshal(files)
					if !strings.Contains(string(content), "fresh-private-token") {
						t.Error("selected private Git credential is missing")
					}
				}
			}
			id := bundleIDFromURL(opts.RuntimeURL)
			published = append(published, id)
			if data, ok := store.Get(id, token); !ok || string(data) != string(runtimeData) {
				t.Error("fresh runtime URL is missing")
			}
			if opts.RestoreEnv["GRASP_RUNTIME_URL"] != opts.RuntimeURL {
				t.Error("runtime bootstrap and injected runtime diverge")
			}
			w.WriteHeader(200)
			return
		}
		_ = json.NewEncoder(w).Encode(GWSandbox{ID: "retained", Status: "running", WorkspaceDir: "/original/workspace", EnvKeys: []string{"OLD_CUSTOM_KEY", "CURSOR_API_KEY", "SANDBOX_INJECT", "SSH_KEY", "WORKSPACE_DIR"}, Endpoints: map[string]string{"session": "127.0.0.1:8765"}})
	}))
	defer server.Close()
	mgr := NewManager(NewGatewayClient(server.URL, ""), ManagerOptions{WorkspaceDir: "/different/default", InjectStore: store, InjectAdvertise: "http://control", Runtime: NewRuntimeBundle(runtimePath), CreateTimeout: time.Second})
	sb, err := mgr.ResumePrepared(context.Background(), "retained", Spec{ConfigHome: home, ConfigRoot: "/root/.codex", WorkspaceDir: "/new/profile/workspace", Env: map[string]string{"AGENT_PROVIDER": "codex", "CONFIG_ROOT": "/root/.codex", "WORKSPACE_DIR": "/new/profile/workspace"}, GitCredentials: []GitCredential{{ID: "selected", Kind: RepoCredentialGitHubHTTPS, Secret: "fresh-private-token"}}})
	if err != nil || started != 1 || sb.WorkspaceDir != "/original/workspace" || sb.ConfigRoot != "/root/.codex" {
		t.Fatalf("restored=%+v starts=%d err=%v", sb, started, err)
	}
	for _, id := range published {
		if _, ok := store.items[id]; ok {
			t.Error("completed cold-start bundle was not cleaned up")
		}
	}
}

func TestColdResumeMissingInputsNeverStartsOrDiscardsSavedGuest(t *testing.T) {
	starts, deletes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			starts++
		}
		if r.Method == http.MethodDelete {
			deletes++
		}
		_ = json.NewEncoder(w).Encode(GWSandbox{ID: "saved"})
	}))
	defer server.Close()
	mgr := NewManager(NewGatewayClient(server.URL, ""), ManagerOptions{InjectStore: NewBundleStore(), InjectAdvertise: "http://control"})
	for _, home := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if _, err := mgr.ResumePrepared(context.Background(), "saved", Spec{ConfigHome: home}); err == nil {
			t.Fatal("missing configuration was accepted")
		}
	}
	if starts != 0 || deletes != 0 {
		t.Fatalf("starts=%d deletes=%d", starts, deletes)
	}
}
