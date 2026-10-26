package handler

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/provider/modelconnection"
	"backend/internal/service"
)

func TestPinnedDSHModelUsesLiveEffortsWithoutStoredMetadata(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "dsh"), []byte("#!/bin/sh\nexec \"$DSH_REASONING_TEST_BINARY\" -test.run=^TestLiveDSHReasoningPeer$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DSH_REASONING_TEST_BINARY", os.Args[0])
	t.Setenv("DSH_REASONING_TEST_PEER", "1")
	t.Setenv("CONFIG_ROOT", t.TempDir())
	t.Setenv("AGENT_PROVIDER", "deepseek_harness")
	t.Setenv("ACP_BRIDGE_REASONING_EFFORT", "")
	t.Setenv("GRASP_MODEL_API_KEY", "synthetic-test-key")
	t.Setenv("GRASP_MODEL_CONNECTION", `{"connectionId":"reasoning-fixture","providerId":"kimi-provider","protocol":"openai-chat-completions","baseUrl":"https://models.example.test/v1","auth":{"scheme":"bearer"},"models":[{"id":"kimi-fixed","name":"Kimi"}],"selectedModelId":"kimi-fixed"}`)
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"kimi-fixed"}]`)
	chats := service.NewChatManager()
	chats.SetDefaultModel("kimi-fixed")
	bridge := chats.Default()
	if _, err := bridge.EnsureAgent(t.TempDir(), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if session := bridge.Session(); session != nil {
			_ = session.Close()
		}
	})
	r := newChatsEngine(chats)
	readOptions := func() []string {
		t.Helper()
		response := do(t, r, "GET", "/api/models", "")
		var payload struct {
			ReasoningEfforts []string             `json:"reasoningEfforts"`
			Models           []service.AgentModel `json:"models"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Models) != 1 || len(payload.Models[0].ReasoningEfforts) != 0 {
			t.Fatalf("pinned model must be present without stored efforts: %d %s", response.Code, response.Body)
		}
		return payload.ReasoningEfforts
	}
	if values := readOptions(); len(values) != 2 || strings.Join(values, ",") != "low,high" {
		t.Fatalf("actual ACP options missing: %v", values)
	}
	response := do(t, r, "POST", "/api/model", `{"reasoningEffort":"high"}`)
	if response.Code != 200 || bridge.Model() != "" || bridge.ReasoningEffort() != "high" || bridge.Session().Info().ModelID != "kimi-fixed" {
		t.Fatalf("effort-only switch changed/rejected pinned model: %d %s", response.Code, response.Body)
	}
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"kimi-fixed","reasoningDisabled":true}]`)
	if values := readOptions(); len(values) != 0 {
		t.Fatalf("explicit false did not suppress live capability: %v", values)
	}
	response = do(t, r, "POST", "/api/model", `{"reasoningEffort":"low"}`)
	if response.Code != 400 || bridge.ReasoningEffort() != "high" {
		t.Fatalf("disabled reasoning was still changed: %d %s", response.Code, response.Body)
	}
}

// The peer advertises thought levels independently of the stored model row,
// using the real provider handshake and an opaque option identifier.
func TestLiveDSHReasoningPeer(t *testing.T) {
	if os.Getenv("DSH_REASONING_TEST_PEER") != "1" {
		return
	}
	var connection modelconnection.Connection
	if json.Unmarshal([]byte(os.Getenv(modelconnection.EnvConfig)), &connection) != nil {
		os.Exit(2)
	}
	modelValue, _ := json.Marshal([]string{connection.RouteID(), connection.SelectedModelID})
	effort := "low"
	options := func() []any {
		return []any{
			map[string]any{"id": "model", "currentValue": string(modelValue), "options": []any{map[string]any{"value": string(modelValue), "name": "Kimi"}}},
			map[string]any{"id": "opaque-runtime-thought-id", "category": "thought_level", "currentValue": effort, "options": []any{map[string]any{"value": "low"}, map[string]any{"value": "high"}}},
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"name": "dsh-live-peer", "version": "test"}}
		case "session/new":
			result = map[string]any{"sessionId": "live-session", "configOptions": options()}
		case "session/set_config_option":
			if request.Params.ConfigID != "opaque-runtime-thought-id" || request.Params.Value != "high" {
				os.Exit(3)
			}
			effort = request.Params.Value
			result = map[string]any{"configOptions": options()}
		default:
			continue
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
	os.Exit(0)
}
