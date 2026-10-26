package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type persistenceAgents map[string]services.Agent

func (a persistenceAgents) Get(name string) (services.Agent, bool) {
	agent, ok := a[name]
	return agent, ok
}

func persistenceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "persistence.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Run{}, &models.Sandbox{}, &models.SoloSession{}, &models.Artifact{}, &models.ArtifactVersion{}, &models.StateRun{}, &models.Gate{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestArtifactMCPPersistenceRestoresSoloAfterRestart(t *testing.T) {
	for _, scenario := range []string{"running", "wrong token", "closed", "replaced sandbox", "missing agent", "no capabilities", "workflow database error"} {
		t.Run(scenario, func(t *testing.T) {
			db := persistenceDB(t)
			caps := &models.AgentCapabilities{Interaction: models.InteractionAuto, Tools: []string{models.ToolSetPreview}}
			agents := persistenceAgents{"Solo": {Name: "Solo", Capabilities: caps}}
			sb := models.Sandbox{Name: "solo-sandbox", RunID: "solo-session-generation", ThreadID: "session", Profile: "Solo", Purpose: services.SandboxPurposeSolo, Status: "running", Token: "persisted-solo-token"}
			if err := db.Create(&sb).Error; err != nil {
				t.Fatal(err)
			}
			session := models.SoloSession{ID: "session", AgentName: "Solo", SandboxID: sb.ID, Status: models.SoloStatusRunning}
			switch scenario {
			case "closed":
				session.Status = models.SoloStatusClosed
			case "replaced sandbox":
				session.SandboxID++
			case "missing agent":
				delete(agents, "Solo")
			case "no capabilities":
				agents["Solo"] = services.Agent{Name: "Solo"}
			}
			if err := db.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "workflow database error" {
				if err := db.Migrator().DropTable(&models.Run{}); err != nil {
					t.Fatal(err)
				}
			}
			preview := services.NewPreviewService(db, nil)
			preview.SetAgents(agents)
			// A fresh host has neither a registered token nor an active node.
			host := mcp.NewHost(services.NewArtifactService(db))
			wireArtifactMCPPersistence(host, db, services.NewRunService(db), preview)
			token := sb.Token
			if scenario == "wrong token" {
				token = "unrelated-token"
			}
			_, err := host.WriteArtifact(sb.RunID, token, "solo", "notes.txt", "restored", "text")
			authorized := scenario == "running" || scenario == "no capabilities"
			if !authorized {
				if !errors.Is(err, mcp.ErrUnauthorized) {
					t.Fatalf("invalid Solo binding authorized: %v", err)
				}
				if scenario != "wrong token" && (host.ActiveNode(sb.RunID) != "mcp" || host.ActiveCaps(sb.RunID) != nil) {
					t.Fatal("invalid Solo binding restored capabilities")
				}
				return
			}
			if err != nil {
				t.Fatalf("persisted Solo token failed after restart: %v", err)
			}
			if node := host.ActiveNode(sb.RunID); node != "solo" {
				t.Fatalf("active node = %q", node)
			}
			if canPreview := host.ActiveCaps(sb.RunID).CanPreview(); canPreview != (scenario == "running") {
				t.Fatalf("restored preview permission = %v", canPreview)
			}
			if err := db.Model(&session).Update("status", models.SoloStatusClosed).Error; err != nil {
				t.Fatal(err)
			}
			// Sandbox teardown revokes cached registrations through this hook.
			host.UnregisterRun(sb.RunID)
			if _, err := host.WriteArtifact(sb.RunID, token, "solo", "notes.txt", "after close", "text"); !errors.Is(err, mcp.ErrUnauthorized) {
				t.Fatalf("closed Solo binding remained authorized: %v", err)
			}
			if host.ActiveNode(sb.RunID) != "mcp" || host.ActiveCaps(sb.RunID) != nil {
				t.Fatal("closed Solo binding restored revoked capabilities")
			}
		})
	}
}

func TestArtifactMCPPersistenceKeepsWorkflowAndManualVisit(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "workflow", true: "manual takeover"}[manual], func(t *testing.T) {
			db := persistenceDB(t)
			caps := &models.AgentCapabilities{Interaction: models.InteractionAuto}
			run := models.Run{ID: "workflow-run", Status: "completed", McpToken: "workflow-token", Graph: models.Graph{Nodes: []models.Node{{ID: "node", Type: "agent", Caps: caps}}}}
			if err := db.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.Sandbox{Name: "retained-workflow", RunID: run.ID, NodeID: "node", Purpose: "run", Status: "running"}).Error; err != nil {
				t.Fatal(err)
			}
			if manual {
				if err := db.Create(&models.StateRun{RunID: run.ID, NodeID: "node", Status: "waiting_human", Mode: "manual"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			host := mcp.NewHost(services.NewArtifactService(db))
			wireArtifactMCPPersistence(host, db, services.NewRunService(db), services.NewPreviewService(db, nil))
			if _, err := host.WriteArtifact(run.ID, run.McpToken, "node", "notes.txt", "kept", "text"); err != nil {
				t.Fatal(err)
			}
			if node := host.ActiveNode(run.ID); node != "node" {
				t.Fatalf("workflow node = %q", node)
			}
			if review := host.ActiveCaps(run.ID).ReviewEnabled(); review != manual {
				t.Fatalf("manual-aware review = %v, want %v", review, manual)
			}
		})
	}
}
