package deepseek

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"backend/internal/acp"
	"backend/internal/backend/common"
	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
	"gopkg.in/yaml.v3"
)

func TestMetadataAndAuthIsolation(t *testing.T) {
	p := New()
	if p.Name() != provider.DeepSeekHarness || p.Runtime() != "dsh-acp" || p.DefaultConfigRoot() != configRoot || p.Transport() != provider.LongLived {
		t.Fatalf("unexpected metadata: %+v", p)
	}
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != defaultModel || !models[0].IsDefault || models[1].ID != "deepseek-v4-pro" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	for _, alias := range []string{"GRASP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY"} {
		env := []string{alias + "=selected-test-key", "CONFIG_ROOT=/tmp/role-dsh", "DSH_HOME=/tmp/old"}
		before := append([]string(nil), env...)
		got := p.AuthEnv(env)
		if common.GetEnvValue(got, "DEEPSEEK_API_KEY") != "selected-test-key" || common.GetEnvValue(got, "DSH_HOME") != "/tmp/role-dsh" {
			t.Fatalf("alias %s not normalized: %v", alias, got)
		}
		if !reflect.DeepEqual(env, before) {
			t.Fatalf("AuthEnv mutated the caller's env")
		}
	}
	env := p.AuthEnv([]string{"DEEPSEEK_API_KEY=native-test-key", "GRASP_DEEPSEEK_API_KEY=other-test-key"})
	if common.GetEnvValue(env, "DEEPSEEK_API_KEY") != "native-test-key" || common.GetEnvValue(env, "DSH_HOME") != configRoot {
		t.Fatalf("native key/default home lost: %v", env)
	}
}

func TestOfficialAuthAliasesCannotHideMalformedCredentials(t *testing.T) {
	for _, alias := range []string{"DEEPSEEK_API_KEY", "GRASP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY"} {
		for _, value := range []string{"", " \t ", "synthetic\r", "synthetic\n", "synthetic\x00"} {
			env := New().AuthEnv([]string{alias + "=" + value})
			if _, err := modelconnection.Prepare(provider.DeepSeekHarness, env, "", t.TempDir()); err == nil {
				t.Fatalf("malformed alias %s reached native launch", alias)
			}
		}
	}
}

func TestModelResolutionUsesAdvertisedOpaqueValue(t *testing.T) {
	value := `[ "deepseek-official", "deepseek-v4-pro" ]`
	options := []acp.ConfigOption{{ID: "model", Groups: []acp.ConfigOptionGroup{{Options: []acp.ConfigOptionValue{
		{Value: `["deepseek-account","deepseek-v4-pro"]`}, {Value: value},
	}}}}}
	for _, model := range []string{"deepseek-v4-pro", "deepseek-official/deepseek-v4-pro", value} {
		got, err := modelValue(options, model)
		if err != nil || got != value {
			t.Fatalf("model=%q got=%q err=%v", model, got, err)
		}
	}
	if _, err := modelValue(options, "missing-model"); err == nil {
		t.Fatal("unsupported model must fail")
	}
	for _, model := range []string{"deepseek-account/deepseek-v4-pro", `["deepseek-account","deepseek-v4-pro"]`} {
		if _, err := modelValue(options, model); err == nil {
			t.Fatal("official API-key route accepted an account or third-party provider")
		}
	}
}

func TestOfficialStartupOverlayPinsNativeCredentialRoutes(t *testing.T) {
	var rows []struct {
		ID       string         `yaml:"id"`
		Disabled bool           `yaml:"disabled"`
		Config   map[string]any `yaml:"config"`
	}
	if err := yaml.Unmarshal([]byte(startupPatch), &rows); err != nil {
		t.Fatal(err)
	}
	byID := map[string]struct {
		Disabled bool
		Config   map[string]any
	}{}
	for _, row := range rows {
		byID[row.ID] = struct {
			Disabled bool
			Config   map[string]any
		}{row.Disabled, row.Config}
	}
	for _, id := range []string{"credentials", "authorization", "deepseek-account", "llm-deepseek-account", "llm-pi-ai", "settings", "config-editor"} {
		if !byID[id].Disabled {
			t.Errorf("saved account/config source still enabled: %s", id)
		}
	}
	for id, endpoint := range map[string]string{"llm-deepseek": "https://api.deepseek.com/anthropic", "web-search-deepseek": "https://api.deepseek.com/anthropic/v1", "web-deepseek-api": "https://api.deepseek.com/v1/tools"} {
		config := byID[id].Config
		if config["apiKeyEnv"] != "DEEPSEEK_API_KEY" || config["baseURL"] != endpoint || len(config) != 2 {
			t.Errorf("native official endpoint/credential not pinned for %s", id)
		}
	}
	if byID["acp"].Config["provider"] != officialProvider {
		t.Fatal("ACP default route must use the official API-key provider")
	}
}

func TestMCPCommandNormalization(t *testing.T) {
	for _, raw := range []string{"", "null", "[]"} {
		if _, err := normalizeMCP(json.RawMessage(raw)); err != nil {
			t.Fatalf("empty input %q: %v", raw, err)
		}
	}
	got, err := normalizeMCP(json.RawMessage(`[{"name":"stdio","command":"sh","args":[],"env":[]},{"name":"http","type":"http","url":"http://127.0.0.1/mcp","headers":[{"name":"Authorization","value":"test-token"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	var servers []struct {
		Command string `json:"command"`
		URL     string `json:"url"`
		Headers []any  `json:"headers"`
	}
	if json.Unmarshal(got, &servers) != nil || !filepath.IsAbs(servers[0].Command) || servers[1].URL != "http://127.0.0.1/mcp" || len(servers[1].Headers) != 1 {
		t.Fatalf("unexpected normalized MCP: %s", got)
	}
	for _, raw := range []string{`{}`, `[{"command":3}]`, `[{"command":"grasp-no-such-mcp-executable"}]`} {
		if _, err := normalizeMCP(json.RawMessage(raw)); err == nil {
			t.Fatalf("bad command accepted: %s", raw)
		}
	}
}

func setupDSH(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dsh"), []byte("#!/bin/sh\nexec \"$DSH_TEST_BINARY\" -test.run=^TestDSHProcess$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DSH_TEST_BINARY", os.Args[0])
	t.Setenv("DSH_TEST_HELPER", "1")
	t.Setenv("DSH_TEST_MODE", mode)
	t.Setenv("DSH_TEST_TRACE", filepath.Join(dir, "trace.jsonl"))
	t.Setenv("CONFIG_ROOT", filepath.Join(dir, "role-home"))
	t.Setenv("DEEPSEEK_API_KEY", "native-test-key")
	return filepath.Join(dir, "trace.jsonl")
}

func openDSH(t *testing.T, model string, onEvent func(json.RawMessage), chooser provider.PermissionChooser) provider.Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	handshake, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	sess, err := New().Open(ctx, handshake, provider.OpenOptions{
		Cwd: t.TempDir(), Model: model,
		McpServers: json.RawMessage(`[{"name":"test","command":"sh","args":[],"env":[]}]`),
	}, onEvent, chooser)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sess.Close()
		_, _ = sess.ExitInfo()
	})
	return sess
}

func TestOpenNegotiatesModelAndPreservesTransport(t *testing.T) {
	trace := setupDSH(t, "")
	var events []json.RawMessage
	sess := openDSH(t, "deepseek-v4-pro", func(ev json.RawMessage) { events = append(events, append(json.RawMessage(nil), ev...)) }, nil)
	if info := sess.Info(); info.ModelID != "deepseek-v4-pro" || info.ModelName != "DeepSeek Pro" || info.Name != "dsh-fixture" {
		t.Fatalf("wrong connected metadata: %+v", info)
	}
	if !sess.ReportsUsage() || sess.CumulativeUsage() != nil || sess.SessionID() != "fixture-session" || !filepath.IsAbs(sess.CWD()) || sess.FSRoot() != sess.CWD() {
		t.Fatalf("unexpected session metadata/usage")
	}
	for i := range 2 {
		res, err := sess.Prompt(context.Background(), fmt.Sprintf("turn %d", i), nil)
		if err != nil || res.StopReason != "end_turn" {
			t.Fatalf("turn failed: %+v %v", res, err)
		}
		// 夹具每轮累计 +100/+20/+30/+5，provider 必须按轮差分。
		want := map[string]provider.TokenUsage{"deepseek-v4-pro": {
			InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 5,
		}}
		if !reflect.DeepEqual(res.Usage, want) {
			t.Fatalf("turn %d usage = %+v, want %+v", i, res.Usage, want)
		}
	}
	if len(events) != 4 {
		t.Fatalf("expected text + done per turn, got %d events", len(events))
	}
	for _, idx := range []int{1, 3} {
		if !strings.Contains(string(events[idx]), `"usage"`) || !strings.Contains(string(events[idx]), `"inputTokens":100`) {
			t.Fatalf("prompt_done without per-turn usage: %s", events[idx])
		}
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--model") || !strings.Contains(string(data), `session/set_config_option`) || !strings.Contains(string(data), `role-home`) || !strings.Contains(string(data), `disabled: true`) {
		t.Fatalf("launch trace does not prove native ACP configuration: %s", data)
	}
	var first struct {
		PatchPath string `json:"patchPath"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(data), "\n")[0]), &first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.PatchPath); err != nil {
		t.Fatalf("startup patch disappeared during live session: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.PatchPath); !os.IsNotExist(err) {
		t.Fatalf("startup patch not cleaned: %v", err)
	}
}

func TestOpenConnectionNegotiatesExactRouteAndRawModel(t *testing.T) {
	for _, protocol := range []string{modelconnection.Chat, modelconnection.Responses, modelconnection.Messages} {
		t.Run(protocol, func(t *testing.T) {
			trace := setupDSH(t, "connection")
			scheme := "bearer"
			if protocol == modelconnection.Messages {
				scheme = "x-api-key"
			}
			c := modelconnection.Connection{ConnectionID: "same-vendor-team-a", ProviderID: "gateway", Protocol: protocol,
				BaseURL: "https://gateway.example/v1", Auth: modelconnection.Auth{Scheme: scheme},
				Models: []modelconnection.Model{{ID: "initial/model"}, {ID: "vendor/model/with/slashes"}}, SelectedModelID: "vendor/model/with/slashes"}
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv(modelconnection.EnvConfig, string(data))
			t.Setenv(modelconnection.EnvAPIKey, "selected-connection-key")
			sess := openDSH(t, "stale-platform-model", nil, nil)
			if sess.Info().ModelID != c.SelectedModelID {
				t.Fatalf("route/model identity changed: %+v", sess.Info())
			}
			if result, err := sess.Prompt(context.Background(), "hello", nil); err != nil || result.StopReason != "end_turn" {
				t.Fatalf("connection prompt failed: %+v %v", result, err)
			}
			data, err = os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), c.RouteID()) || !strings.Contains(string(data), `session/set_config_option`) || !strings.Contains(string(data), `"legacyKeyCleared":true`) {
				t.Fatalf("route not negotiated or legacy auth survived: %s", data)
			}
			if strings.Contains(string(data), "selected-connection-key") {
				t.Fatal("DSH configuration or protocol trace leaked the selected secret")
			}
		})
	}
}

func TestOpenConnectionHonorsCatalogSessionModel(t *testing.T) {
	trace := setupDSH(t, "connection")
	c := modelconnection.Connection{ConnectionID: "same-vendor-team-a", ProviderID: "gateway", Protocol: modelconnection.Chat,
		BaseURL: "https://gateway.example/v1", Auth: modelconnection.Auth{Scheme: "bearer"},
		Models:          []modelconnection.Model{{ID: "vendor/model/with/slashes"}, {ID: "initial/model"}},
		SelectedModelID: "vendor/model/with/slashes"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(modelconnection.EnvConfig, string(data))
	t.Setenv(modelconnection.EnvAPIKey, "selected-connection-key")
	sess := openDSH(t, "initial/model", nil, nil)
	if sess.Info().ModelID != "initial/model" {
		t.Fatalf("session model fell back to the primary: %+v", sess.Info())
	}
	data, err = os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(data), "\n")[0]), &first); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := yaml.Unmarshal([]byte(first.Patch), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["id"] != "acp" {
			continue
		}
		if row["config"].(map[string]any)["model"] != "initial/model" {
			t.Fatalf("ACP patch kept the primary model: %s", first.Patch)
		}
		return
	}
	t.Fatal("ACP patch missing")
}

func TestOpenDefaultAndFailures(t *testing.T) {
	for _, tc := range []struct{ mode, model, failure string }{
		{"", "", ""}, {"", "auto", ""},
		{"initialize-error", "", "initialize"}, {"new-error", "", "session/new"},
		{"set-error", "deepseek-v4-pro", "session/set_config_option"}, {"", "missing-model", "not advertised"},
	} {
		t.Run(tc.mode+tc.model, func(t *testing.T) {
			setupDSH(t, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sess, err := New().Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: tc.model}, nil, nil)
			if tc.failure != "" {
				if err == nil || !strings.Contains(err.Error(), tc.failure) {
					t.Fatalf("got session=%v err=%v, expected %s", sess, err, tc.failure)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sess.Close(); _, _ = sess.ExitInfo() }()
			if sess.Info().ModelID != defaultModel {
				t.Fatalf("wrong default model: %+v", sess.Info())
			}
		})
	}
}

func TestPermissionsAndCancelThenContinue(t *testing.T) {
	for _, decision := range []string{"allow", "deny"} {
		t.Run(decision, func(t *testing.T) {
			setupDSH(t, "")
			seen := false
			started := make(chan struct{}, 1)
			sess := openDSH(t, "", func(ev json.RawMessage) {
				if strings.Contains(string(ev), "waiting-for-cancel") {
					started <- struct{}{}
				}
			}, func(_ context.Context, _ json.RawMessage, params json.RawMessage) (string, error) {
				seen = strings.Contains(string(params), "allow_once")
				return decision, nil
			})
			if _, err := sess.Prompt(context.Background(), "permission", nil); err != nil || !seen {
				t.Fatalf("permission bridge failed: err=%v seen=%v", err, seen)
			}
			finished := make(chan error, 1)
			go func() {
				res, err := sess.Prompt(context.Background(), "wait", nil)
				if err == nil && res.StopReason != "cancelled" {
					err = fmt.Errorf("unexpected stop: %s", res.StopReason)
				}
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("prompt did not start")
			}
			if err := sess.Cancel(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancel did not finish prompt")
			}
			if res, err := sess.Prompt(context.Background(), "continue", nil); err != nil || res.StopReason != "end_turn" {
				t.Fatalf("could not continue after cancellation: %+v %v", res, err)
			}
		})
	}
}

func TestReasoningOptionRequiresAdvertisedCategoryAndOpaqueValue(t *testing.T) {
	options := []acp.ConfigOption{{ID: "arbitrary-agent-option", Category: "thought_level", CurrentValue: "low", Groups: []acp.ConfigOptionGroup{{Options: []acp.ConfigOptionValue{{Value: "low"}, {Value: "high"}}}}}}
	id, current, err := reasoningOption(options, "high")
	if err != nil || id != "arbitrary-agent-option" || current != "low" {
		t.Fatalf("did not retain advertised option id/value: %q %q %v", id, current, err)
	}
	if _, _, err := reasoningOption(options, "ultra"); err == nil {
		t.Fatal("unadvertised effort accepted")
	}
	options[0].Category = "mode"
	if _, _, err := reasoningOption(options, "high"); err == nil {
		t.Fatal("a mode or model name must not imply thought-level support")
	}
}

func TestOpenNegotiatesReasoningAfterModelAndRejectsUnconfirmedChoices(t *testing.T) {
	for _, mode := range []string{"effort", "effort-error", "effort-unconfirmed", ""} {
		t.Run(mode, func(t *testing.T) {
			trace := setupDSH(t, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			handshake, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			sess, err := New().Open(ctx, handshake, provider.OpenOptions{Cwd: t.TempDir(), Model: "deepseek-v4-pro", ReasoningEffort: "high", ReasoningEffortSet: true}, nil, nil)
			if mode != "effort" {
				if err == nil {
					if sess != nil {
						_ = sess.Close()
					}
					t.Fatal("unavailable or rejected effort accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sess.Close(); _, _ = sess.ExitInfo() }()
			current, values := sess.(provider.ReasoningOptionSource).ReasoningOptions()
			if current != "high" || !reflect.DeepEqual(values, []string{"low", "high"}) {
				t.Fatalf("post-negotiation snapshot = %q %v", current, values)
			}
			values[0] = "mutated"
			_, values = sess.(provider.ReasoningOptionSource).ReasoningOptions()
			if values[0] != "low" {
				t.Fatal("mutable capability snapshot exposed")
			}
			data, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			var selections []string
			for _, line := range strings.Split(string(data), "\n") {
				var call struct {
					Method string `json:"method"`
					Params struct {
						ConfigID string `json:"configId"`
						Value    string `json:"value"`
					} `json:"params"`
				}
				if json.Unmarshal([]byte(line), &call) == nil && call.Method == "session/set_config_option" {
					selections = append(selections, call.Params.ConfigID)
					if call.Params.ConfigID == "runtime-intensity" && call.Params.Value != "high" {
						t.Fatalf("opaque effort changed: %q", call.Params.Value)
					}
				}
			}
			if !reflect.DeepEqual(selections, []string{"model", "runtime-intensity"}) {
				t.Fatalf("effort must follow model negotiation using the advertised config ID: %v", selections)
			}
		})
	}
}

// TestDSHProcess is a deterministic ACP peer exercised through the real
// subprocess, handshake, request, notification, permission, and cancel paths.
func TestDSHProcess(t *testing.T) {
	if os.Getenv("DSH_TEST_HELPER") != "1" {
		return
	}
	trace, err := os.OpenFile(os.Getenv("DSH_TEST_TRACE"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		os.Exit(2)
	}
	defer trace.Close()
	var patchPath string
	for i, arg := range os.Args {
		if arg == "--patch" && i+1 < len(os.Args) {
			patchPath = os.Args[i+1]
		}
	}
	patch, _ := os.ReadFile(patchPath)
	_ = json.NewEncoder(trace).Encode(map[string]any{"argv": os.Args, "patchPath": patchPath, "patch": string(patch), "dshHome": os.Getenv("DSH_HOME"), "legacyKeyCleared": os.Getenv("DEEPSEEK_API_KEY") == ""})
	model := defaultModel
	var connection modelconnection.Connection
	connectionMode := os.Getenv("DSH_TEST_MODE") == "connection"
	if connectionMode {
		_ = json.Unmarshal([]byte(os.Getenv(modelconnection.EnvConfig)), &connection)
		model = connection.Models[0].ID
	}
	effort := "low"
	modelOptions := func() []any {
		if connectionMode {
			choices := make([]any, 0, len(connection.Models))
			for _, m := range connection.Models {
				value, _ := json.Marshal([]string{connection.RouteID(), m.ID})
				choices = append(choices, map[string]any{"value": string(value), "name": m.ID})
			}
			current, _ := json.Marshal([]string{connection.RouteID(), model})
			return []any{map[string]any{"id": "model", "currentValue": string(current), "options": []any{map[string]any{"group": connection.RouteID(), "options": choices}}}}
		}
		current := `["deepseek-official","deepseek-flash"]`
		if model == "deepseek-v4-pro" {
			current = `[ "deepseek-official", "deepseek-v4-pro" ]`
		}
		return []any{map[string]any{"id": "model", "currentValue": current, "options": []any{map[string]any{
			"group": officialProvider, "options": []any{
				map[string]any{"value": `["deepseek-official","deepseek-flash"]`, "name": "DeepSeek Flash"},
				map[string]any{"value": `[ "deepseek-official", "deepseek-v4-pro" ]`, "name": "DeepSeek Pro"},
			},
		}}}}
	}
	options := func() []any {
		out := modelOptions()
		if strings.HasPrefix(os.Getenv("DSH_TEST_MODE"), "effort") {
			out = append(out, map[string]any{"id": "runtime-intensity", "category": "thought_level", "currentValue": effort,
				"options": []any{map[string]any{"value": "low", "name": "Low"}, map[string]any{"value": "high", "name": "High"}}})
		}
		return out
	}
	enc := json.NewEncoder(os.Stdout)
	respond := func(id json.RawMessage, result any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	notify := func(text string) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}}})
	}
	// 模拟 dsh 的 tokenUsage 投影缓存：每完成一轮累计增加固定的四桶用量。
	usageCalls := int64(0)
	writeUsageCache := func() {
		usageCalls++
		home := os.Getenv("DSH_HOME")
		if home == "" {
			return
		}
		dir := filepath.Join(home, "storages", "session_projcache", "sessions")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			os.Exit(5)
		}
		record := map[string]any{
			"version": 7,
			"record": map[string]any{"rows": map[string]any{
				"tokenUsage": map[string]any{
					"ver": 2,
					"seq": usageCalls * 10,
					"val": map[string]any{"totals": map[string]any{
						"uncachedInputTokens": usageCalls * 100,
						"outputTokens":        usageCalls * 20,
						"cacheReadTokens":     usageCalls * 30,
						"cacheWriteTokens":    usageCalls * 5,
					}},
				},
				"title":       map[string]any{"ver": 1, "seq": usageCalls * 10, "val": "fixture"},
				"sandboxMode": map[string]any{"ver": 1, "seq": usageCalls * 10, "val": "workspace-write"},
				"todos":       map[string]any{"ver": 2, "seq": usageCalls * 10, "val": nil},
			}},
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			os.Exit(5)
		}
		path := filepath.Join(dir, "fixture-session.json")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
			os.Exit(5)
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Exit(5)
		}
	}
	var waiting, permissionPrompt json.RawMessage
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(3)
		}
		_ = json.NewEncoder(trace).Encode(map[string]any{"method": req.Method, "params": req.Params})
		if req.Method == "" && string(req.ID) == `"permission"` {
			notify(string(req.Result))
			respond(permissionPrompt, map[string]any{"stopReason": "end_turn"})
			continue
		}
		if os.Getenv("DSH_TEST_MODE") == "initialize-error" && req.Method == "initialize" || os.Getenv("DSH_TEST_MODE") == "new-error" && req.Method == "session/new" || os.Getenv("DSH_TEST_MODE") == "set-error" && req.Method == "session/set_config_option" {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32603, "message": "fixture failure"}})
			continue
		}
		switch req.Method {
		case "initialize":
			respond(req.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"name": "dsh-fixture", "version": "test"}})
		case "session/new":
			respond(req.ID, map[string]any{"sessionId": "fixture-session", "configOptions": options()})
		case "session/set_config_option":
			var params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			_ = json.Unmarshal(req.Params, &params)
			if params.ConfigID == "runtime-intensity" {
				if os.Getenv("DSH_TEST_MODE") == "effort-error" {
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32603, "message": "effort fixture failure"}})
					continue
				}
				if params.Value != "high" {
					os.Exit(4)
				}
				if os.Getenv("DSH_TEST_MODE") != "effort-unconfirmed" {
					effort = params.Value
				}
				respond(req.ID, map[string]any{"configOptions": options()})
				continue
			}
			if params.ConfigID != "model" {
				os.Exit(4)
			}
			if connectionMode {
				var route []string
				if json.Unmarshal([]byte(params.Value), &route) != nil || len(route) != 2 || route[0] != connection.RouteID() {
					os.Exit(4)
				}
				known := false
				for _, item := range connection.Models {
					if item.ID == route[1] {
						known = true
						model = item.ID
						break
					}
				}
				if !known {
					os.Exit(4)
				}
			} else if params.Value != `[ "deepseek-official", "deepseek-v4-pro" ]` {
				os.Exit(4)
			} else {
				model = "deepseek-v4-pro"
			}
			respond(req.ID, map[string]any{"configOptions": options()})
		case "session/prompt":
			var params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(req.Params, &params)
			switch params.Prompt[0].Text {
			case "wait":
				waiting = req.ID
				notify("waiting-for-cancel")
			case "permission":
				permissionPrompt = req.ID
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "permission", "method": "session/request_permission", "params": map[string]any{"sessionId": "fixture-session", "options": []any{map[string]any{"optionId": "allow", "kind": "allow_once"}, map[string]any{"optionId": "deny", "kind": "reject_once"}}}})
			default:
				notify(model)
				writeUsageCache()
				respond(req.ID, map[string]any{"stopReason": "end_turn"})
			}
		case "session/cancel":
			if waiting != nil {
				respond(waiting, map[string]any{"stopReason": "cancelled"})
				waiting = nil
			}
		}
	}
	os.Exit(0)
}
