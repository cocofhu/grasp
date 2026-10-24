//go:build e2ereal

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// TestRestartReattachOnboardingContract drives the real restart fixture through current
// HTTP handlers and persistence without Docker or a real model API key.
func TestRestartReattachOnboardingContract(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32)))
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "restart.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	agents := services.NewAgentService(t.TempDir())
	shared := services.NewSharedAgentService(t.TempDir())
	credentials := services.NewCredentialService(db)
	connections := services.NewModelConnectionService(db)
	configurations := services.NewModelConfigurationService(db)
	workflows := services.NewWorkflowService(db)
	agents.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		_, err := connections.Resolve(backend, *binding)
		return err
	})
	h := &handlers.Handlers{
		Agents: agents, Credentials: credentials,
		Onboarding: services.NewOnboardingService(agents, shared, workflows, credentials),
	}
	connectionHandlers := &handlers.ModelConnectionHandlers{Service: connections}
	configurationHandlers := &handlers.ModelConfigurationHandlers{Service: configurations}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/workspace/credentials", h.CreateCredential)
	router.POST("/api/workspace/model-connections", connectionHandlers.Create)
	router.POST("/api/workspace/model-configurations", configurationHandlers.Create)
	router.POST("/api/workspace/bootstrap-onboarding", h.BootstrapOnboarding)
	router.GET("/api/agents/:name", h.GetAgent)
	router.PUT("/api/agents/:name", h.SaveAgent)

	// A rejected old request proves the test is using the new onboarding
	// contract rather than a permissive stand-in for the production API.
	old := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspace/bootstrap-onboarding", strings.NewReader(`{"acpBackend":"opencode","apiKey":"old-key","openCodeProvider":"deepseek","openCodeModel":"deepseek-chat"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(old, req)
	if old.Code != http.StatusBadRequest || !strings.Contains(old.Body.String(), "modelConfigurationId is required") {
		t.Fatalf("old bootstrap request was not rejected: %d", old.Code)
	}
	if len(agents.List()) != 0 || len(workflows.List()) != 0 {
		t.Fatal("rejected bootstrap created partial resources")
	}

	type recordedRequest struct {
		method, path string
		body         []byte
	}
	var mu sync.Mutex
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mu.Lock()
		requests = append(requests, recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		mu.Unlock()
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s := &stack{t: t, api: server.URL, client: server.Client()}
	const secret = "restart-fixture-secret-never-in-model-or-agent"
	const model = "deepseek-fixture-raw-model"
	s.onboard(secret, model)

	mu.Lock()
	seen := append([]recordedRequest(nil), requests...)
	mu.Unlock()
	want := []string{
		"POST /api/workspace/credentials", "POST /api/workspace/model-connections",
		"POST /api/workspace/model-configurations", "POST /api/workspace/bootstrap-onboarding",
		"GET /api/agents/Implement", "PUT /api/agents/Implement",
	}
	var actual []string
	for i, request := range seen {
		actual = append(actual, request.method+" "+request.path)
		if bytes.Contains(request.body, []byte(secret)) != (i == 0) {
			t.Errorf("request %d has incorrect secret placement", i)
		}
		if i > 0 {
			var body map[string]any
			if len(request.body) != 0 {
				if err := json.Unmarshal(request.body, &body); err != nil {
					t.Fatal(err)
				}
			}
			for _, key := range []string{"apiKey", "openCodeProvider", "openCodeModel", "aiCredentialId", "openCodeCredentialId"} {
				if _, exists := body[key]; exists {
					t.Errorf("request %d still sends removed field %s", i, key)
				}
			}
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("fixture requests = %v, want %v", actual, want)
	}
	models, err := configurations.List()
	if err != nil || len(models) != 1 {
		t.Fatalf("saved model configurations: %d, error: %v", len(models), err)
	}
	configuration := models[0]
	connection, err := connections.Get(configuration.Binding.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Backend != "opencode" || configuration.Binding.Kind != "connection" || configuration.Binding.ModelID != model {
		t.Fatal("fixture did not persist the selected coding tool and raw model ID")
	}
	if connection.Protocol != modelrouting.ProtocolChat || connection.ProviderID != "deepseek" || connection.BaseURL != "https://api.deepseek.com/v1" || connection.Auth.Scheme != "bearer" || len(connection.Models) != 1 || connection.Models[0].ID != model {
		t.Fatal("fixture did not persist the explicit DeepSeek Chat service")
	}
	for _, name := range []string{"Clarify", "Implement"} {
		agent, ok := agents.Get(name)
		if !ok || agent.ModelBinding == nil {
			t.Fatalf("Agent %s was not configured", name)
		}
		if agent.ModelBinding.Kind != "configuration" || agent.ModelBinding.ConfigurationID != configuration.ID {
			t.Fatalf("Agent %s does not reference the saved configuration", name)
		}
		if agent.GitBindings != nil && (len(agent.GitBindings.Items) != 0 || agent.GitBindings.PreferredCredentialID != "") {
			t.Fatalf("Agent %s received unselected Git authorization", name)
		}
		resolved, err := connections.Resolve(agent.AcpBackend, *agent.ModelBinding)
		if err != nil || resolved.Secret != secret || resolved.CredentialID != connection.CredentialID {
			t.Fatalf("Agent %s model credential could not be resolved", name)
		}
		encoded, err := json.Marshal([]any{configuration, connection, agent, shared.Get(), resolved})
		if err != nil || bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("saved model, Agent or resolved JSON exposed the model secret")
		}
		if name == "Implement" && (agent.Capabilities == nil || agent.Capabilities.Review) {
			t.Fatal("restart fixture did not disable the implement review gate")
		}
	}
	if len(workflows.List()) != 1 {
		t.Fatal("onboarding did not create the default workflow")
	}
}
