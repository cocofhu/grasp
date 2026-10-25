package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestRemoveModelAPIAccountHTTPKeepsClearAndRejectsOtherKinds(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	connections := services.NewModelConnectionService(hn.db)

	created := postCredential(t, hn, map[string]any{"purpose": "model_api", "issuer": "deepseek", "name": "1", "value": "synthetic-model-key"})
	connection, err := connections.Create(modelrouting.Connection{
		Name: "DeepSeek", ProviderID: "deepseek", Protocol: modelrouting.ProtocolChat,
		BaseURL: "https://api.deepseek.com/v1", CredentialID: created.ID,
		Models: []modelrouting.Model{{ID: "deepseek-chat", Input: []string{"text"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	configurations := services.NewModelConfigurationService(hn.db)
	configuration, err := configurations.Create(modelrouting.Configuration{
		Name: "Team model", Backend: "opencode",
		Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: "deepseek-chat"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cleared := hn.do(http.MethodDelete, "/api/workspace/credentials/"+created.ID, nil)
	if cleared.Code != http.StatusOK || !strings.Contains(cleared.Body.String(), `"cleared"`) {
		t.Fatalf("clear=%d %s", cleared.Code, cleared.Body.String())
	}
	if _, err := hn.h.Credentials.Get(created.ID); err != nil {
		t.Fatal("clear deleted the account row", err)
	}
	if _, err := connections.Get(connection.ID); err != nil {
		t.Fatal("clear deleted the model connection", err)
	}

	git := postCredential(t, hn, map[string]any{"purpose": "git", "provider": "github", "name": "GitHub", "value": "ghp-synthetic"})
	rejected := hn.do(http.MethodDelete, "/api/workspace/credentials/"+git.ID+"/account", nil)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("git account delete=%d %s", rejected.Code, rejected.Body.String())
	}
	if _, err := hn.h.Credentials.Get(git.ID); err != nil {
		t.Fatal("rejected delete removed the git account", err)
	}

	removed := hn.do(http.MethodDelete, "/api/workspace/credentials/"+created.ID+"/account", nil)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"removed"`) {
		t.Fatalf("remove=%d %s", removed.Code, removed.Body.String())
	}
	listed := hn.do(http.MethodGet, "/api/workspace/credentials", nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), created.ID) {
		t.Fatalf("list still contains the account: %s", listed.Body.String())
	}
	if _, err := connections.Get(connection.ID); err == nil {
		t.Fatal("bound connection remained")
	}
	if _, err := configurations.Get(configuration.ID); err != nil {
		t.Fatal("model configuration was deleted", err)
	}
	resolved, err := connections.Resolve("opencode", modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID})
	if err == nil || resolved.Secret != "" {
		t.Fatalf("configuration did not fail closed: %+v %v", resolved, err)
	}
	var stored models.Credential
	if err := hn.db.First(&stored, "id = ?", created.ID).Error; err == nil {
		t.Fatal("removed account row remained")
	}
}

func postCredential(t *testing.T, hn *harness, body map[string]any) services.CredentialView {
	t.Helper()
	response := hn.do(http.MethodPost, "/api/workspace/credentials", body)
	if response.Code != http.StatusOK {
		t.Fatalf("create credential %d %s", response.Code, response.Body.String())
	}
	var created services.CredentialView
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created
}
