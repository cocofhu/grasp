package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestGitCredentialHostHTTPContract(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	response := hn.do(http.MethodPost, "/api/workspace/credentials", map[string]any{
		"purpose": "git", "provider": "gitlab", "name": "GitLab API test", "value": "private-handler-token",
	})
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-handler-token") {
		t.Fatalf("create credential status: %d", response.Code)
	}
	var view services.CredentialView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || view.Host != "https://gitlab.com" {
		t.Fatalf("API did not return the default host: %v", err)
	}
	path := "/api/workspace/credentials/" + view.ID
	response = hn.do(http.MethodPut, path, map[string]any{"host": "http://Git.Internal:8080/"})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"host":"http://git.internal:8080"`) {
		t.Fatalf("edit credential host status: %d", response.Code)
	}
	for _, host := range []string{"https://", "https://git.internal/path", "https://user:token@git.internal"} {
		response = hn.do(http.MethodPut, path, map[string]any{"host": host})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid host returned status %d", response.Code)
		}
	}
	if err := hn.db.Create(&models.Credential{ID: "standalone-url", Name: "GitLab URL", Type: "git", Provider: "gitlab_url", EnvKey: "GITLAB_URL"}).Error; err != nil {
		t.Fatal(err)
	}
	response = hn.do(http.MethodGet, "/api/workspace/credentials", nil)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "standalone-url") || strings.Contains(response.Body.String(), "GITLAB_URL") || strings.Contains(response.Body.String(), "private-handler-token") {
		t.Fatalf("credential list includes standalone URL or token (status %d)", response.Code)
	}
}
