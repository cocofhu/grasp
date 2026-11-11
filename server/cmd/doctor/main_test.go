package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyArtifactIsolation(t *testing.T) {
	var content string
	cleaned := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/_internal/doctor/artifact-sessions":
			if r.Header.Get("Authorization") != "Bearer doctor-secret" {
				t.Error("missing doctor authorization")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(artifactSession{
				ID: "session", RunA: "run-a", TokenA: "token-a",
				RunB: "run-b", TokenB: "token-b", CleanupToken: "cleanup",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/_internal/doctor/artifact-sessions/session":
			if r.Header.Get("X-Grasp-Doctor-Cleanup") != "cleanup" {
				t.Error("missing cleanup token")
			}
			cleaned = true
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/mcp/runs/"):
			if r.URL.Path == "/mcp/runs/run-b" && r.Header.Get("Authorization") == "Bearer token-a" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var call struct {
				Params struct {
					Name      string         `json:"name"`
					Arguments map[string]any `json:"arguments"`
				} `json:"params"`
			}
			_ = json.NewDecoder(r.Body).Decode(&call)
			if call.Params.Name == "write_artifact" {
				content, _ = call.Params.Arguments["content"].(string)
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
				return
			}
			if r.URL.Path == "/mcp/runs/run-b" {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":4,"result":{"isError":true,"content":[{"type":"text","text":"not found"}]}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": 2,
				"result": map[string]any{"content": []map[string]string{{"type": "text", "text": content}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := verifyArtifactIsolation(context.Background(), server.URL, "doctor-secret"); err != nil {
		t.Fatal(err)
	}
	if !cleaned {
		t.Fatal("artifact session was not cleaned")
	}
}

func TestCheckHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := checkHTTP(context.Background(), server.URL, "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestParseOptionsRejectsUnexpectedArguments(t *testing.T) {
	if _, err := parseOptions([]string{"doctor", "--run-demo", "extra"}); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}

func TestParseOptionsRequiresDoctorTokenForDemo(t *testing.T) {
	t.Setenv("GRASP_DOCTOR_TOKEN", "")
	if _, err := parseOptions([]string{"doctor", "--run-demo"}); err == nil {
		t.Fatal("expected missing doctor token error")
	}
}
