package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestCredentialHTTPRequiresCurrentDomainAndPreservesServiceAdapters(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	for _, kind := range []string{"git", "ssh", "mcp", "custom", "ai"} {
		response := hn.do(http.MethodPost, "/api/workspace/credentials", map[string]any{"type": kind, "provider": "github", "name": "Old input", "value": "synthetic"})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("type-only %s request returned %d", kind, response.Code)
		}
	}
	response := hn.do(http.MethodGet, "/api/workspace/credentials", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatal("fresh workspace still exposed generated credential slots")
	}
	ciphertext, err := crypto.Encrypt("synthetic-legacy-secret")
	if err != nil {
		t.Fatal(err)
	}
	old := models.Credential{ID: "old-credential", Type: "ai", Provider: "cursor", Name: "Cursor", EnvKey: "GRASP_CURSOR_API_KEY", ValueEnc: ciphertext, Enabled: true}
	if err := hn.db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	response = hn.do(http.MethodPut, "/api/workspace/credentials/"+old.ID, map[string]any{"purpose": "tool_auth", "toolId": "cursor", "value": "synthetic-new-value"})
	if response.Code != http.StatusNotFound {
		t.Fatal("editing upgraded an old credential")
	}
	response = hn.do(http.MethodDelete, "/api/workspace/credentials/"+old.ID, nil)
	if response.Code != http.StatusNotFound {
		t.Fatal("clear accepted an old credential")
	}
	var stored models.Credential
	if err := hn.db.First(&stored, "id = ?", old.ID).Error; err != nil || stored.ValueEnc != ciphertext || stored.Purpose != "" {
		t.Fatal("old encrypted data was changed")
	}
	if err := hn.h.Agents.Save(services.Agent{Name: "channel-adapter", AcpBackend: "cursor"}); err != nil {
		t.Fatal(err)
	}
	hn.h.Channels = services.NewChannelConfigService(hn.db)
	hn.h.Channels.SetAgentService(hn.h.Agents)
	channel, err := hn.h.Channels.Create(services.ChannelConfigInput{Type: models.ChannelTypeQQ, Name: "QQ service", AppID: "adapter-app", AppSecret: "synthetic-channel-secret", AgentName: "channel-adapter"})
	if err != nil {
		t.Fatal(err)
	}
	hn.h.McpKeys = services.NewWorkspaceMcpApiKeyService(hn.db)
	mcp, err := hn.h.McpKeys.Create("MCP service")
	if err != nil {
		t.Fatal(err)
	}
	workflow := models.WorkflowDef{ID: "credential-workflow", Name: "Workflow service"}
	if err := hn.db.Create(&workflow).Error; err != nil {
		t.Fatal(err)
	}
	key, err := hn.h.APIKeys.Create(workflow.ID, "Workflow key")
	if err != nil {
		t.Fatal(err)
	}
	response = hn.do(http.MethodGet, "/api/workspace/credentials", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list=%d", response.Code)
	}
	if strings.Contains(response.Body.String(), old.ID) || strings.Contains(response.Body.String(), "synthetic-channel-secret") || strings.Contains(response.Body.String(), mcp.Plaintext) || strings.Contains(response.Body.String(), key.Plaintext) {
		t.Fatal("list exposed an old row or a service secret")
	}
	var listed struct {
		Items []services.CredentialView `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"channel:" + channel.ID: false, "external_mcp:" + mcp.Key.ID: false, "workflow:" + key.Key.ID: false}
	for _, item := range listed.Items {
		if _, ok := want[item.ID]; ok && item.Source != "workspace" && item.Configured {
			want[item.ID] = true
		}
	}
	for _, found := range want {
		if !found {
			t.Fatal("real read-only service adapter disappeared")
		}
	}
}
