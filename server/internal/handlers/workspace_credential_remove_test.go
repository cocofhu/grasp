package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
)

func TestRemoveModelAPIAccountHTTPKeepsClearAndDropsConnections(t *testing.T) {
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

	removed := hn.do(http.MethodDelete, "/api/workspace/credentials/"+created.ID+"/account", nil)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"removed"`) {
		t.Fatalf("remove=%d %s", removed.Code, removed.Body.String())
	}
	assertCredentialNotListed(t, hn, created.ID)
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

func TestRemoveCredentialHTTPDeletesToolAuthAndKeepsPlatformConfigurationClosed(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	connections := services.NewModelConnectionService(hn.db)
	configurations := services.NewModelConfigurationService(hn.db)

	empty := postCredential(t, hn, map[string]any{"purpose": "tool_auth", "toolId": "opencode", "name": "123123", "value": "temporary-test-key"})
	if cleared := hn.do(http.MethodDelete, "/api/workspace/credentials/"+empty.ID, nil); cleared.Code != http.StatusOK {
		t.Fatalf("clear=%d %s", cleared.Code, cleared.Body.String())
	}
	if view, err := hn.h.Credentials.Get(empty.ID); err != nil || view.Configured {
		t.Fatalf("cleared credential: %+v %v", view, err)
	}
	removedEmpty := hn.do(http.MethodDelete, "/api/workspace/credentials/"+empty.ID+"/account", nil)
	if removedEmpty.Code != http.StatusOK || !strings.Contains(removedEmpty.Body.String(), `"removed"`) {
		t.Fatalf("remove empty tool credential=%d %s", removedEmpty.Code, removedEmpty.Body.String())
	}
	assertCredentialNotListed(t, hn, empty.ID)

	key := postCredential(t, hn, map[string]any{"purpose": "tool_auth", "toolId": "opencode", "name": "Official", "value": "official-test-key"})
	configuration, err := configurations.Create(modelrouting.Configuration{
		Name: "Official model", Backend: "opencode",
		Binding: modelrouting.Binding{Kind: "platform", CredentialID: key.ID, ModelID: "opencode/kimi-k3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}
	if resolved, err := connections.Resolve("opencode", ref); err != nil || resolved.Secret != "official-test-key" {
		t.Fatalf("platform configuration did not resolve before removal: %+v %v", resolved, err)
	}
	removed := hn.do(http.MethodDelete, "/api/workspace/credentials/"+key.ID+"/account", nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove tool credential=%d %s", removed.Code, removed.Body.String())
	}
	assertCredentialNotListed(t, hn, key.ID)
	if _, err := configurations.Get(configuration.ID); err != nil {
		t.Fatal("platform configuration was deleted", err)
	}
	resolved, err := connections.Resolve("opencode", ref)
	if err == nil || resolved.Secret != "" {
		t.Fatalf("platform configuration did not fail closed: %+v %v", resolved, err)
	}
}

func TestRemoveCredentialHTTPDeletesGitAndBindingFailsClosed(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	credentials := services.NewCredentialService(hn.db)
	hn.h.Credentials = credentials

	git := postCredential(t, hn, map[string]any{"purpose": "git", "provider": "github", "name": "GitHub", "value": "ghp-synthetic"})
	bindings := &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: git.ID}}}
	if _, err := runtime.ResolveGitBindings(bindings, credentials.ResolveGitValue); err != nil {
		t.Fatalf("git binding did not resolve before removal: %v", err)
	}
	removed := hn.do(http.MethodDelete, "/api/workspace/credentials/"+git.ID+"/account", nil)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"removed"`) {
		t.Fatalf("git delete=%d %s", removed.Code, removed.Body.String())
	}
	assertCredentialNotListed(t, hn, git.ID)
	if _, err := runtime.ResolveGitBindings(bindings, credentials.ResolveGitValue); err == nil || !strings.Contains(err.Error(), "missing") || strings.Contains(err.Error(), "ghp-synthetic") {
		t.Fatalf("git binding did not fail closed: %v", err)
	}

	again := hn.do(http.MethodDelete, "/api/workspace/credentials/"+git.ID+"/account", nil)
	if again.Code != http.StatusNotFound {
		t.Fatalf("second delete=%d %s", again.Code, again.Body.String())
	}
	missing := hn.do(http.MethodDelete, "/api/workspace/credentials/missing-credential/account", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing delete=%d %s", missing.Code, missing.Body.String())
	}
}

func assertCredentialNotListed(t *testing.T, hn *harness, id string) {
	t.Helper()
	listed := hn.do(http.MethodGet, "/api/workspace/credentials", nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), id) {
		t.Fatalf("list still contains %s: %d %s", id, listed.Code, listed.Body.String())
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
