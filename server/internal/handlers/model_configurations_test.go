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

func TestModelConfigurationHandlerLifecycle(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "configurations.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	credentials := services.NewCredentialService(db)
	key, err := credentials.Create(services.CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Cursor key", Value: "private-platform-secret"})
	if err != nil {
		t.Fatal(err)
	}
	h := &handlers.ModelConfigurationHandlers{Service: services.NewModelConfigurationService(db)}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/configurations", h.List)
	router.POST("/configurations", h.Create)
	router.GET("/configurations/:configurationId", h.Get)
	router.PUT("/configurations/:configurationId", h.Update)
	router.DELETE("/configurations/:configurationId", h.Delete)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		if strings.Contains(response.Body.String(), "private-platform-secret") || strings.Contains(response.Body.String(), "valueEnc") || strings.Contains(response.Body.String(), `"secret"`) {
			t.Fatal("configuration handler returned a secret")
		}
		return response
	}
	if response := request("GET", "/configurations", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("empty list: %d %s", response.Code, response.Body)
	}
	body := `{"name":"Cursor default","backend":"cursor","binding":{"kind":"platform","credentialId":"` + key.ID + `"}}`
	response := request("POST", "/configurations", body)
	if response.Code != 201 {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}
	var configuration modelrouting.Configuration
	if err := json.Unmarshal(response.Body.Bytes(), &configuration); err != nil {
		t.Fatal(err)
	}
	path := "/configurations/" + configuration.ID
	if configuration.State != "active" || configuration.Revision != 1 || configuration.Binding.CredentialID != key.ID {
		t.Fatalf("unexpected saved configuration: %+v", configuration)
	}
	if response := request("GET", path, ""); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if response := request("PUT", path, body); response.Code != 200 || !strings.Contains(response.Body.String(), `"revision":2`) {
		t.Fatalf("update: %d %s", response.Code, response.Body)
	}
	if response := request("PUT", path, strings.Replace(body, `"backend":"cursor"`, `"backend":"trae"`, 1)); response.Code != 400 {
		t.Fatalf("changed tool accepted: %d %s", response.Code, response.Body)
	}
	for _, method := range []string{"POST", "PUT"} {
		target := "/configurations"
		if method == "PUT" {
			target = path
		}
		if response := request(method, target, `{"binding":`); response.Code != 400 {
			t.Fatalf("malformed JSON accepted: %d %s", response.Code, response.Body)
		}
	}
	for _, invalid := range []string{
		`{"name":"Inherited","backend":"cursor","binding":{"kind":"inherit"}}`,
		`{"name":"Recursive","backend":"cursor","binding":{"kind":"configuration","configurationId":"` + configuration.ID + `"}}`,
		`{"name":"Missing key","backend":"cursor","binding":{"kind":"platform","credentialId":"missing"}}`,
	} {
		if response := request("POST", "/configurations", invalid); response.Code != 400 {
			t.Fatalf("invalid configuration accepted: %d %s", response.Code, response.Body)
		}
	}
	if response := request("PUT", "/configurations/missing", body); response.Code != 404 {
		t.Fatalf("update missing: %d %s", response.Code, response.Body)
	}
	if err := credentials.Clear(key.ID); err != nil {
		t.Fatal(err)
	}
	if response := request("GET", path, ""); response.Code != 200 || !strings.Contains(response.Body.String(), key.ID) {
		t.Fatalf("invalid reference metadata became unreadable: %d %s", response.Code, response.Body)
	}
	if response := request("PUT", path, body); response.Code != 400 {
		t.Fatalf("cleared credential passed validation: %d %s", response.Code, response.Body)
	}
	if response := request("DELETE", path, ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatal(response.Body)
	}
	for _, method := range []string{"GET", "DELETE"} {
		if response := request(method, path, ""); response.Code != 404 {
			t.Fatalf("deleted configuration: %d %s", response.Code, response.Body)
		}
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if response := request("GET", "/configurations", ""); response.Code != 500 || strings.Contains(response.Body.String(), "database") {
		t.Fatalf("database error not masked: %d %s", response.Code, response.Body)
	}
}
