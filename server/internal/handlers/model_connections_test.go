package handlers_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/modelrouting"
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
