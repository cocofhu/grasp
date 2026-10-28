package kimicode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/acp"
	"backend/internal/backend/common"
)

// Opt-in proof against the pinned real CLI, with a local model endpoint and a
// synthetic key only: GRASP_KIMI_NATIVE_CLI=/absolute/path/to/kimi go test
// ./internal/provider/kimicode -run TestNativeKimiReasoningRequests -count=1.
func TestNativeKimiReasoningRequests(t *testing.T) {
	cli := os.Getenv("GRASP_KIMI_NATIVE_CLI")
	if cli == "" {
		t.Skip("set GRASP_KIMI_NATIVE_CLI to test the real Kimi Code 2.4.0 CLI")
	}
	if !filepath.IsAbs(cli) {
		t.Fatal("GRASP_KIMI_NATIVE_CLI must be an absolute path")
	}
	requests := make(chan map[string]any, 32)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-kimi-native-test" {
			http.Error(w, "unexpected authorization", http.StatusUnauthorized)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"native-regression\",\"object\":\"chat.completion.chunk\",\"model\":\"k3.1-pro\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":null}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"native-regression\",\"object\":\"chat.completion.chunk\",\"model\":\"k3.1-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	open := func(model, effort string) *acp.Panel {
		t.Helper()
		home, err := isolatedHome(root, endpoint.URL+"/v1", model, effort)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeConfig(home, endpoint.URL+"/v1", model, effort); err != nil {
			t.Fatal(err)
		}
		env := make([]string, 0)
		for _, item := range os.Environ() {
			key, _, _ := strings.Cut(item, "=")
			if !strings.HasPrefix(key, "KIMI_MODEL_") {
				env = append(env, item)
			}
		}
		env = common.UpsertEnv(env, "KIMI_CODE_HOME", home)
		env = common.UpsertEnv(env, APIKeyEnv, "synthetic-kimi-native-test")
		env = common.UpsertEnv(env, "KIMI_CODE_NO_AUTO_UPDATE", "1")
		env = common.UpsertEnv(env, "KIMI_DISABLE_TELEMETRY", "1")
		cwd := t.TempDir()
		panel, err := acp.NewPanel(ctx, ctx, []string{cli, "acp"}, env, cwd, cwd, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = panel.Close(); _ = panel.Conn().Wait() })
		if panel.AgentVersion != "2.4.0" {
			t.Fatalf("native regression requires pinned 2.4.0, got %s", panel.AgentVersion)
		}
		if err := selectReasoningEffort(ctx, panel, effort); err != nil {
			t.Fatal(err)
		}
		return panel
	}
	// Keep two native processes alive together. Their config/model watchers
	// share persistence but must never change each other's selected strength.
	low, max := open("kimi-k3.1-pro", "low"), open("kimi-k3.1-pro", "max")
	sameRoute := open("kimi-k3.1-pro", "low")
	for _, tc := range []struct {
		panel *acp.Panel
		want  string
	}{{low, "low"}, {max, "max"}, {sameRoute, "low"}, {low, "low"}} {
		if _, err := tc.panel.Prompt(ctx, "Reply OK only.", nil); err != nil {
			t.Fatal(err)
		}
		select {
		case request := <-requests:
			thinking, _ := request["thinking"].(map[string]any)
			if request["model"] != "k3.1-pro" || thinking["type"] != "enabled" || thinking["effort"] != tc.want {
				t.Fatalf("native request selected model=%v thinking=%v; want %s", request["model"], thinking, tc.want)
			}
		case <-ctx.Done():
			t.Fatal("native CLI did not reach the local model endpoint")
		}
	}
	// Reset creates a clean config with no [thinking] override, rather than
	// reusing the previous selected process's low or max setting.
	for _, effort := range []string{"high", ""} {
		panel := open("k3.1-pro", effort)
		if _, err := panel.Prompt(ctx, "Reply OK only.", nil); err != nil {
			t.Fatal(err)
		}
		select {
		case request := <-requests:
			thinking, _ := request["thinking"].(map[string]any)
			if thinking["effort"] != "high" {
				t.Fatalf("native %q effort request = %v; want high", effort, thinking)
			}
		case <-ctx.Done():
			t.Fatal("native default reset did not reach the local model endpoint")
		}
	}
	for _, model := range []string{"k3", "k3-256k", "k3.1-pro-highspeed", "kimi-for-coding", "kimi-k3", "kimi-k3.1-pro-highspeed"} {
		for _, effort := range []string{"max", ""} {
			panel := open(model, effort)
			if _, err := panel.Prompt(ctx, "Reply OK only.", nil); err != nil {
				t.Fatal(err)
			}
			select {
			case request := <-requests:
				thinking, _ := request["thinking"].(map[string]any)
				wire, _ := canonicalModel(model)
				want := effort
				if want == "" {
					want = defaultReasoningEffort(model)
				}
				if request["model"] != wire || thinking["effort"] != want {
					t.Fatalf("%s/%q reached wire model=%v thinking=%v; want %s/%s", model, effort, request["model"], thinking, wire, want)
				}
			case <-ctx.Done():
				t.Fatal("native alias/default request did not reach the local model endpoint")
			}
			_ = panel.Close()
		}
	}
	highspeed := open("kimi-for-coding-highspeed", "")
	if err := selectReasoningEffort(ctx, highspeed, "low"); err == nil {
		t.Fatal("K2.7 Highspeed silently accepted a strength")
	}
	if _, err := highspeed.Prompt(ctx, "Reply OK only.", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		thinking, _ := request["thinking"].(map[string]any)
		if request["model"] != "kimi-for-coding-highspeed" || thinking["type"] != "enabled" || thinking["effort"] != nil {
			t.Fatalf("K2.7 Highspeed must enable boolean thinking without a strength: model=%v thinking=%v", request["model"], thinking)
		}
	case <-ctx.Done():
		t.Fatal("native K2.7 Highspeed did not reach the local model endpoint")
	}
}

// Ensure shared native storage remains usable when the two configs are first
// prepared at the same time, before either process has made its directories.
func TestIsolatedHomeConcurrentCreation(t *testing.T) {
	root := t.TempDir()
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			home, err := isolatedHome(root, intlBase, "kimi-k3", "low")
			if err != nil {
				t.Error(err)
				return
			}
			if err := writeConfig(home, intlBase, "kimi-k3", "low"); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	entries, err := os.ReadDir(filepath.Join(root, ".grasp-configs"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("concurrent callers did not reuse one bounded config: %d %v", len(entries), err)
	}
	path := filepath.Join(root, ".grasp-configs", entries[0].Name(), "config.toml")
	if body, err := os.ReadFile(path); err != nil || string(body) != renderConfig(intlBase, "kimi-k3", "low") {
		t.Fatal("concurrent publication exposed incomplete configuration", err)
	}
}
