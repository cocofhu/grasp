package handlers_test

import (
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/services"
)

func enableAdmin(t *testing.T) {
	t.Helper()
	cfg := config.GetConfig()
	if cfg == nil {
		t.Fatal("no config")
	}
	users := make([]config.AuthUser, len(cfg.Auth.Users))
	copy(users, cfg.Auth.Users)
	for i := range users {
		if users[i].Username == "admin" {
			users[i].IsAdmin = true
		}
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)
}

func seedAgent(t *testing.T, hn *harness, name string) {
	t.Helper()
	if err := hn.h.Agents.Save(services.Agent{
		Name:  name,
		Files: []services.AgentFile{{Path: "AGENTS.md", Content: "# hi"}},
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
}
