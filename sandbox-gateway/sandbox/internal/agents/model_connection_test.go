package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

// Exercise real registry -> provider -> subprocess wiring against a local model
// endpoint. The fixture reads generated native config, never the binding JSON.
// It verifies transport generation; it does not substitute for vendor CLI tests.
func TestModelConnectionSubprocessReceivesSelectedService(t *testing.T) {
	for _, tc := range []struct {
		tool                  provider.Name
		protocol, scheme, bin string
	}{
		{provider.ClaudeCode, modelconnection.Messages, "x-api-key", "claude"},
		{provider.ClaudeCode, modelconnection.Messages, "bearer", "claude"},
		{provider.CodeBuddy, modelconnection.Chat, "bearer", "codebuddy"},
		{provider.OpenCode, modelconnection.Chat, "bearer", "opencode"},
		{provider.OpenCode, modelconnection.Responses, "bearer", "opencode"},
		{provider.OpenCode, modelconnection.Messages, "x-api-key", "opencode"},
		{provider.Codex, modelconnection.Responses, "bearer", "codex"},
	} {
		bases := []string{"/prefix/v1"}
		if tc.protocol == modelconnection.Messages {
			bases = append(bases, "/prefix")
		}
		for _, basePath := range bases {
			t.Run(string(tc.tool)+"/"+tc.protocol+"/"+tc.scheme+basePath, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					var payload struct {
						Model string `json:"model"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					if payload.Model != "vendor/model/raw-id" {
						t.Errorf("wrong upstream model %q", payload.Model)
					}
					wantPath := "/prefix/v1/chat/completions"
					if tc.protocol == modelconnection.Responses {
						wantPath = "/prefix/v1/responses"
					}
					if tc.protocol == modelconnection.Messages {
						wantPath = "/prefix/v1/messages"
					}
					if r.URL.Path != wantPath {
						t.Errorf("wrong endpoint %s", r.URL.Path)
					}
					if tc.scheme == "bearer" && r.Header.Get("Authorization") != "Bearer selected-model-secret" {
						t.Error("selected bearer auth not used")
					}
					if tc.scheme == "x-api-key" && r.Header.Get("x-api-key") != "selected-model-secret" {
						t.Error("selected x-api-key auth not used")
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ok":true}`))
				}))
				defer server.Close()
				c := modelconnection.Connection{ConnectionID: "connection-team", ProviderID: "gateway", Protocol: tc.protocol, BaseURL: server.URL + basePath, Auth: modelconnection.Auth{Scheme: tc.scheme}, Models: []modelconnection.Model{{ID: "vendor/model/raw-id"}}, SelectedModelID: "vendor/model/raw-id"}
				data, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, tc.bin), []byte("#!/bin/sh\nexec \"$GRASP_MODEL_TEST_BINARY\" -test.run=^TestModelConnectionCLIProcess$ -- \"$@\"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("GRASP_MODEL_TEST_BINARY", os.Args[0])
				t.Setenv("GRASP_MODEL_TEST_TOOL", string(tc.tool))
				t.Setenv(modelconnection.EnvConfig, string(data))
				t.Setenv(modelconnection.EnvAPIKey, "selected-model-secret")
				t.Setenv("ANTHROPIC_AUTH_TOKEN", "old-platform-key")
				t.Setenv("OPENAI_API_KEY", "old-platform-key")
				t.Setenv("CODEBUDDY_AUTH_TOKEN", "old-platform-key")
				t.Setenv("CONFIG_ROOT", t.TempDir())
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				sess, err := Get(tc.tool).Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: "old-platform-model"}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer sess.Close()
				result, err := sess.Prompt(ctx, "hello", nil)
				if err != nil || result.StopReason != "end_turn" {
					t.Fatalf("selected service failed: %+v %v", result, err)
				}
				if requests.Load() != 1 {
					t.Fatalf("expected one selected model request, got %d", requests.Load())
				}
			})
		}
	}
}

func TestModelConnectionCLIProcess(t *testing.T) {
	tool := os.Getenv("GRASP_MODEL_TEST_TOOL")
	if tool == "" {
		return
	}
	for _, arg := range os.Args {
		if arg == "--version" {
			fmt.Println("2.1.257 (Claude Code)")
			os.Exit(0)
		}
	}
	fail := func(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(2) }
	var model, base, key, protocol string
	scheme := "bearer"
	read := func(path string) map[string]any {
		data, err := os.ReadFile(path)
		if err != nil {
			fail("generated config missing")
		}
		if strings.Contains(string(data), "selected-model-secret") {
			fail("plaintext secret in config")
		}
		var v map[string]any
		if json.Unmarshal(data, &v) != nil {
			fail("invalid generated config")
		}
		return v
	}
	argModel := func() string {
		for i, arg := range os.Args {
			if arg == "--model" && i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		return ""
	}
	switch provider.Name(tool) {
	case provider.ClaudeCode:
		read(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json"))
		model = argModel()
		base = os.Getenv("ANTHROPIC_BASE_URL")
		key = os.Getenv("ANTHROPIC_API_KEY")
		scheme = "x-api-key"
		protocol = modelconnection.Messages
		if key == "" {
			key = os.Getenv("ANTHROPIC_AUTH_TOKEN")
			scheme = "bearer"
		}
		if os.Getenv("CLAUDE_CODE_SUBAGENT_MODEL") != model || os.Getenv("ANTHROPIC_DEFAULT_HAIKU_MODEL") != model {
			fail("auxiliary model was not pinned")
		}
	case provider.CodeBuddy:
		modelsPath := filepath.Join(os.Getenv("CODEBUDDY_CONFIG_DIR"), "models.json")

		doc := read(modelsPath)
		model = argModel()
		models := doc["models"].([]any)
		found := false
		for _, entry := range models {
			m := entry.(map[string]any)
			if m["id"] != model {
				continue
			}
			found = true
			base = m["url"].(string)
			key = os.Getenv("GRASP_MODEL_API_KEY")
			if m["api"] == modelconnection.Responses {
				protocol = modelconnection.Responses
			} else {
				protocol = modelconnection.Chat
				base = strings.TrimSuffix(base, "/chat/completions")
			}
		}
		if !found || os.Getenv("CODEBUDDY_AUTH_TOKEN") != "" {
			fail("selected custom model missing or platform auth survived")
		}
	case provider.OpenCode:
		var doc map[string]any
		if json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &doc) != nil {
			fail("inline config missing")
		}
		providers := doc["provider"].(map[string]any)
		for route, entry := range providers {
			p := entry.(map[string]any)
			if !strings.HasPrefix(argModel(), route+"/") {
				fail("wrong CLI route")
			}
			model = strings.TrimPrefix(argModel(), route+"/")
			if p["models"].(map[string]any)[model] == nil {
				fail("upstream model was split")
			}
			options := p["options"].(map[string]any)
			base = options["baseURL"].(string)
			if options["apiKey"] != "{env:GRASP_MODEL_API_KEY}" {
				fail("missing env-key reference")
			}
			key = os.Getenv("GRASP_MODEL_API_KEY")
			switch p["npm"] {
			case "@ai-sdk/openai-compatible":
				protocol = modelconnection.Chat
			case "@ai-sdk/openai":
				protocol = modelconnection.Responses
			case "@ai-sdk/anthropic":
				protocol = modelconnection.Messages
				scheme = "x-api-key"
			default:
				fail("unknown SDK")
			}
		}
	case provider.Codex:
		model = argModel()
		protocol = modelconnection.Responses
		for i, arg := range os.Args {
			if arg != "-c" || i+1 >= len(os.Args) {
				continue
			}
			setting := os.Args[i+1]
			name, value, _ := strings.Cut(setting, "=")
			var text string
			_ = json.Unmarshal([]byte(value), &text)
			if strings.HasSuffix(name, ".base_url") {
				base = text
			}
			if strings.HasSuffix(name, ".env_key") {
				key = os.Getenv(text)
			}
		}
	default:
		fail("unknown fixture tool")
	}
	if model == "" || key == "" || key == "old-platform-key" {
		fail("missing selected model credential")
	}
	path := "/chat/completions"
	if protocol == modelconnection.Responses {
		path = "/responses"
	}
	if protocol == modelconnection.Messages {
		// Native Anthropic clients append the version themselves; the Vercel
		// SDK used by OpenCode appends only /messages to a versioned root.
		path = "/v1/messages"
		if provider.Name(tool) == provider.OpenCode {
			path = "/messages"
		}
	}
	body, _ := json.Marshal(map[string]string{"model": model})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+path, strings.NewReader(string(body)))
	if err != nil {
		fail("bad endpoint")
	}
	if scheme == "x-api-key" {
		req.Header.Set("x-api-key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fail("fixture model request failed")
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		fail("fixture request rejected")
	}
	switch provider.Name(tool) {
	case provider.ClaudeCode, provider.CodeBuddy:
		fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"connected"}]}}`)
		fmt.Println(`{"type":"result","subtype":"success","session_id":"fixture","result":"connected"}`)
	case provider.OpenCode:
		fmt.Println(`{"type":"text","part":{"text":"connected"}}`)
		fmt.Println(`{"type":"step_finish","part":{"reason":"stop","tokens":{"input":1,"output":1}}}`)
	case provider.Codex:
		fmt.Println(`{"type":"thread.started","thread_id":"fixture"}`)
		fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"connected"}}`)
		fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`)
	}
	os.Exit(0)
}
