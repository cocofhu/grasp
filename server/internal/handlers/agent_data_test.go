package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/services"
)

func attachPm(t *testing.T, hn *harness) *services.PmService {
	t.Helper()
	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, nil, hn.h.Runs, hn.h.Arts, nil)
	return pm
}

func grantAdmin(t *testing.T) {
	t.Helper()
	cfg := config.GetConfig()
	users := append([]config.AuthUser(nil), cfg.Auth.Users...)
	for i := range users {
		if users[i].Username == "admin" {
			users[i].IsAdmin = true
		}
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)
	t.Cleanup(func() {
		cfg := config.GetConfig()
		users := append([]config.AuthUser(nil), cfg.Auth.Users...)
		for i := range users {
			if users[i].Username == "admin" {
				users[i].IsAdmin = false
			}
		}
		cfg.Auth.Users = users
		config.StoreConfig(cfg)
	})
}

func TestAgentSaveRejectsSecretEnvKeys(t *testing.T) {
	hn := newHarness(t)
	for _, key := range envauth.SecretEnvKeys() {
		w := hn.do(http.MethodPost, "/api/agents", map[string]any{"inheritModel": false, "acpBackend": "cursor",
			"name": "ssh-env",
			"env":  map[string]string{key: "x"},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("create with %s want 400 got %d %s", key, w.Code, w.Body.String())
		}
	}
	w := hn.do(http.MethodPost, "/api/agents", map[string]any{"inheritModel": false, "acpBackend": "cursor", "name": "ssh-env"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPut, "/api/agents/ssh-env", map[string]any{"inheritModel": false, "acpBackend": "cursor",
		"name": "ssh-env",
		"env":  map[string]string{"GIT_SSH_PRIVATE_KEY": "x"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("save with secret env want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestDeleteAgentPurgesScopedData(t *testing.T) {
	hn := newHarness(t)
	pm := attachPm(t, hn)

	w := hn.do(http.MethodPost, "/api/agents", map[string]any{"inheritModel": false, "acpBackend": "cursor", "name": "to-delete"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if _, err := pm.UpsertMemory("to-delete", "M", "c", "admin", "u"); err != nil {
		t.Fatal(err)
	}
	w = hn.do(http.MethodDelete, "/api/agents/to-delete", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	mem, _ := pm.ListMemories("to-delete")
	if len(mem) != 0 {
		t.Fatalf("memories should be purged: %v", mem)
	}
}

func TestAgentDataAPI(t *testing.T) {
	hn := newHarness(t)
	attachPm(t, hn)
	grantAdmin(t)

	w := hn.do(http.MethodPost, "/api/agents", map[string]any{"inheritModel": false, "acpBackend": "cursor", "name": "data-agent"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPost, "/api/agents/data-agent/memories", map[string]any{
		"title": "T", "content": "C",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("upsert memory: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/agents/data-agent/memories", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list memories: %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []models.MemoryItem `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Items) != 1 || listed.Items[0].AgentName != "data-agent" {
		t.Fatalf("listed=%+v", listed.Items)
	}

	w = hn.do(http.MethodGet, "/api/agents/data-agent/threads", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list threads: %d %s", w.Code, w.Body.String())
	}
}
