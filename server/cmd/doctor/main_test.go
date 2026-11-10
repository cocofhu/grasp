package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
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

// This is a protocol regression, not native sandbox/image proof. In particular
// the fake gateway must reject a create which lacks the real bootstrap shape.
func TestDemoObtainsBootstrapBeforeCreateAndReusesArtifactSession(t *testing.T) {
	for _, failure := range []string{"", "missing bootstrap", "invalid bootstrap", "incomplete session", "authority rejected", "create", "wait", "isolation", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			bootstrap := &sandbox.DoctorBootstrap{Env: map[string]string{
				"GRASP_RUNTIME_URL":       "http://api.example.test/sandbox-inject/owned.tgz",
				"SANDBOX_INJECT_HEADERS":  "Authorization: Bearer disposable-bundle-token",
				sandbox.BridgePasswordEnv: strings.Repeat("a", 48), "ROOT_PASSWORD": strings.Repeat("b", 48),
				"WORKSPACE_DIR": "/root/workspace", "SKIP_INNER_DOCKER": "1",
			}}
			var starts, creates, deletes, cleanups, mcpCalls atomic.Int32
			var mu sync.Mutex
			content := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && (r.URL.Path == "/api/health" || r.URL.Path == "/healthz"):
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPost && r.URL.Path == "/_internal/doctor/artifact-sessions":
					starts.Add(1)
					if r.URL.Query().Get("sandbox") != "true" || r.Header.Get("Authorization") != "Bearer doctor-test-token" {
						t.Error("demo did not request an authenticated sandbox bootstrap")
					}
					if failure == "authority rejected" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					session := artifactSession{ID: "session", RunA: "run-a", TokenA: "token-a", RunB: "run-b", TokenB: "token-b", CleanupToken: "cleanup", Bootstrap: bootstrap}
					if failure == "missing bootstrap" {
						session.Bootstrap = nil
					}
					if failure == "invalid bootstrap" {
						session.Bootstrap.Env["OPENAI_API_KEY"] = "must-not-reach-provider"
					}
					if failure == "incomplete session" {
						session.TokenB = ""
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(session)
				case r.Method == http.MethodDelete && r.URL.Path == "/_internal/doctor/artifact-sessions/session":
					cleanups.Add(1)
					if r.Header.Get("X-Grasp-Doctor-Cleanup") != "cleanup" || r.Header.Get("Authorization") != "Bearer doctor-test-token" {
						t.Error("cleanup lacked the original session credentials")
					}
					if failure == "cleanup" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sandboxes":
					creates.Add(1)
					var request sandbox.GWCreateRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if starts.Load() != 1 || !reflect.DeepEqual(request.Env, bootstrap.Env) || (sandbox.DoctorBootstrap{Env: request.Env}).Validate() != nil {
						t.Error("provider create preceded a valid session bootstrap")
					}
					if failure == "create" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(sandbox.GWSandbox{ID: "owned-scratch", Status: "creating"})
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sandboxes/owned-scratch":
					status := "running"
					if failure == "wait" {
						status = "error"
					}
					_ = json.NewEncoder(w).Encode(sandbox.GWSandbox{ID: "owned-scratch", Status: status, Endpoints: map[string]string{"session": "127.0.0.1:1"}})
				case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/sandboxes/owned-scratch":
					deletes.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/mcp/runs/"):
					mcpCalls.Add(1)
					if r.URL.Path == "/mcp/runs/run-b" && r.Header.Get("Authorization") == "Bearer token-a" && failure != "isolation" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					var call struct {
						Params struct {
							Name      string            `json:"name"`
							Arguments map[string]string `json:"arguments"`
						} `json:"params"`
					}
					_ = json.NewDecoder(r.Body).Decode(&call)
					mu.Lock()
					defer mu.Unlock()
					if call.Params.Name == "write_artifact" {
						content = call.Params.Arguments["content"]
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{
						"isError": r.URL.Path == "/mcp/runs/run-b", "content": []map[string]string{{"text": content}},
					}})
				default:
					t.Errorf("unexpected diagnostic request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			err := run([]string{"--run-demo", "--api-url", server.URL, "--gateway-url", server.URL, "--doctor-token", "doctor-test-token", "--timeout", "5s"}, io.Discard)
			if (err != nil) != (failure != "") {
				t.Fatal("unexpected demo result", err)
			}
			if starts.Load() != 1 {
				t.Fatal("demo created another artifact session", starts.Load())
			}
			rejected := failure == "missing bootstrap" || failure == "invalid bootstrap" || failure == "incomplete session" || failure == "authority rejected"
			if rejected && creates.Load() != 0 {
				t.Fatal("rejected session reached provider create")
			}
			if !rejected && creates.Load() != 1 {
				t.Fatal("demo did not create exactly one scratch sandbox", creates.Load())
			}
			if !rejected && failure != "create" && deletes.Load() != 1 {
				t.Fatal("scratch sandbox was not cleaned exactly once", deletes.Load())
			}
			if failure != "authority rejected" && cleanups.Load() < 1 {
				t.Fatal("accepted session was not cleaned")
			}
			if failure == "" && (mcpCalls.Load() != 4 || cleanups.Load() != 1) {
				t.Fatal("demo did not reuse and clean its original MCP session", mcpCalls.Load(), cleanups.Load())
			}
		})
	}
}

func TestBasicDoctorDoesNotCreateDiagnosticSessionOrSandbox(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/health" {
			t.Error("basic health check performed a diagnostic mutation")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := run([]string{"--api-url", server.URL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("basic doctor made more than its health request")
	}
}
