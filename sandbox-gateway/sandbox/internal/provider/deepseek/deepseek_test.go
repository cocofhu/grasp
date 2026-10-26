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
	if sess.ReportsUsage() || sess.CumulativeUsage() != nil || sess.SessionID() != "fixture-session" || !filepath.IsAbs(sess.CWD()) || sess.FSRoot() != sess.CWD() {
		t.Fatalf("unexpected session metadata/usage")
	}
	for i := range 2 {
		res, err := sess.Prompt(context.Background(), fmt.Sprintf("turn %d", i), nil)
		if err != nil || res.StopReason != "end_turn" || res.Usage != nil {
			t.Fatalf("turn failed: %+v %v", res, err)
		}
	}
	if len(events) != 4 {
		t.Fatalf("expected text + done per turn, got %d events", len(events))
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
	options := func() []any {
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
	enc := json.NewEncoder(os.Stdout)
	respond := func(id json.RawMessage, result any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	notify := func(text string) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}}})
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
				Value string `json:"value"`
			}
			_ = json.Unmarshal(req.Params, &params)
			if connectionMode {
				want, _ := json.Marshal([]string{connection.RouteID(), connection.SelectedModelID})
				if params.Value != string(want) {
					os.Exit(4)
				}
				model = connection.SelectedModelID
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
