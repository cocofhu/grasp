package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/services"
)

func TestSSHKeyKnownHostsHTTPContract(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	response := hn.do(http.MethodPost, "/api/workspace/credentials", map[string]any{
		"purpose": "ssh", "secretKind": "ssh_known_hosts", "name": "Retired hosts", "value": "host ssh-ed25519 AAAA",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("standalone known hosts status: %d", response.Code)
	}
	response = hn.do(http.MethodPost, "/api/workspace/credentials", map[string]any{
		"purpose": "ssh", "name": "Deploy key", "value": "private-handler-ssh-key", "knownHosts": "git.example ssh-ed25519 AAAA\n",
	})
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-handler-ssh-key") {
		t.Fatalf("create SSH key status: %d", response.Code)
	}
	var view services.CredentialView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || view.KnownHosts != "git.example ssh-ed25519 AAAA" {
		t.Fatalf("create view = %+v, %v", view, err)
	}
	path := "/api/workspace/credentials/" + view.ID
	response = hn.do(http.MethodPatch, path, map[string]any{"knownHosts": "bad\x00host"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("NUL known hosts status: %d", response.Code)
	}
	response = hn.do(http.MethodPatch, path, map[string]any{"knownHosts": ""})
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "knownHosts") {
		t.Fatalf("clear known hosts: %d %s", response.Code, response.Body.String())
	}
}
