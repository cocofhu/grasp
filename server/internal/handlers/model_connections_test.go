package handlers_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/router"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func TestModelConnectionHandlerLifecycle(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "connections.db"))
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	key, err := services.NewCredentialService(db).Create(services.CredentialInput{Purpose: "model_api", Issuer: "deepseek", Name: "Key", Value: "secret-handler-value"})
	if err != nil {
		t.Fatal(err)
	}
	h := &handlers.ModelConnectionHandlers{Service: services.NewModelConnectionService(db)}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/connections", h.List)
	router.POST("/connections", h.Create)
	router.GET("/connections/:connectionId", h.Get)
	router.PUT("/connections/:connectionId", h.Update)
	router.DELETE("/connections/:connectionId", h.Delete)
	router.GET("/capabilities", h.Capabilities)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		if strings.Contains(response.Body.String(), "secret-handler-value") {
			t.Fatal("handler leaked secret")
		}
		return response
	}
	response := request("GET", "/connections", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("empty list: %d %s", response.Code, response.Body)
	}
	body := `{"name":"Service","providerId":"deepseek","protocol":"openai-chat-completions","baseUrl":"https://example.com/v1","credentialId":"` + key.ID + `","models":[{"id":"vendor/model"}]}`
	response = request("POST", "/connections", body)
	if response.Code != 201 {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}
	var connection modelrouting.Connection
	if err := json.Unmarshal(response.Body.Bytes(), &connection); err != nil {
		t.Fatal(err)
	}
	if response = request("GET", "/connections/"+connection.ID, ""); response.Code != 200 {
		t.Fatal(response.Body)
	}
	response = request("PUT", "/connections/"+connection.ID, body)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"revision":2`) {
		t.Fatalf("update: %d %s", response.Code, response.Body)
	}
	if response = request("GET", "/capabilities", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"verification":"not-run"`) {
		t.Fatal(response.Body)
	}
	for _, method := range []string{"POST", "PUT"} {
		path := "/connections"
		if method == "PUT" {
			path += "/" + connection.ID
		}
		if response = request(method, path, `{"credentialId":"secret-handler-value"`); response.Code != 400 {
			t.Fatal(response.Body)
		}
	}
	if response = request("POST", "/connections", `{"name":"Bad"}`); response.Code != 400 {
		t.Fatal(response.Body)
	}
	if response = request("PUT", "/connections/missing", body); response.Code != 404 {
		t.Fatal(response.Body)
	}
	if response = request("DELETE", "/connections/"+connection.ID, ""); response.Code != 200 {
		t.Fatal(response.Body)
	}
	for _, method := range []string{"GET", "DELETE"} {
		if response = request(method, "/connections/"+connection.ID, ""); response.Code != 404 {
			t.Fatal(response.Body)
		}
	}
}

func TestModelConnectionDiscoveryMetadataAndSafeRetryError(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	hn := newHarness(t)
	enableAdmin(t)
	hn.cookie = hn.login(t)
	service := services.NewModelConnectionService(hn.db)
	// Build the normal authenticated routes with their real model service.
	hn.h.ModelConnections = service
	hn.r = router.New(hn.h)
	key, err := services.NewCredentialService(hn.db).Create(services.CredentialInput{Purpose: "model_api", Issuer: "custom", Name: "Discovery", Value: "discovery-private-key"})
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"discovery-private-key"}`))
			return
		}
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer discovery-private-key" {
			t.Error("discovery did not use the selected saved account")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"provider/raw-model","name":"Provider model"}]}`))
	}))
	t.Cleanup(upstream.Close)
	connection, err := service.Create(modelrouting.Connection{Name: "Catalog", ProviderID: "custom", Protocol: modelrouting.ProtocolChat, BaseURL: upstream.URL + "/v1", CredentialID: key.ID})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/workspace/model-connections/" + connection.ID + "/models"
	response := hn.do(http.MethodGet, path, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"source":"discovered"`) || !strings.Contains(response.Body.String(), `"id":"provider/raw-model"`) || strings.Contains(response.Body.String(), "discovery-private-key") {
		t.Fatalf("discovery API: %d %s", response.Code, response.Body.String())
	}
	fail.Store(true)
	response = hn.do(http.MethodGet, path, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"models":[]`) || !strings.Contains(response.Body.String(), `"error":`) || strings.Contains(response.Body.String(), "discovery-private-key") {
		t.Fatalf("unsafe discovery fallback: %d %s", response.Code, response.Body.String())
	}
	if response = hn.do(http.MethodGet, "/api/workspace/model-connections/missing/models", nil); response.Code != http.StatusNotFound {
		t.Fatalf("missing connection discovery: %d", response.Code)
	}
	hn.cookie = ""
	if response = hn.do(http.MethodGet, path, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("discovery skipped workspace authentication: %d", response.Code)
	}
}
