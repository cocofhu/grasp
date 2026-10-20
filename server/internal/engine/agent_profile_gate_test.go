package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestCheckAgentProfile(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "skill-gate.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	wf := models.WorkflowDef{ID: "wf-sp", Name: "SP", Graph: models.Graph{}}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("wf: %v", err)
	}
	run := models.Run{ID: "run-sp", WorkflowID: wf.ID, Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("run: %v", err)
	}

	skills := services.NewAgentService(t.TempDir())
	if err := skills.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "ok"}); err != nil {
		t.Fatalf("save ok: %v", err)
	}

	// Minimal engine — avoid New() which requires a non-nil MCP host.
	eng := &Engine{db: db, skills: skills}
	c := &execCtx{run: &run}

	t.Run("empty skipped", func(t *testing.T) {
		n := &models.Node{ID: "n", Label: "空", Config: map[string]any{"agent_profile": ""}}
		if err := eng.checkAgentProfile(c, n); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("existing agent ok", func(t *testing.T) {
		n := &models.Node{ID: "n", Label: "实现", Config: map[string]any{"agent_profile": "ok"}}
		if err := eng.checkAgentProfile(c, n); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("deleted", func(t *testing.T) {
		n := &models.Node{ID: "n", Label: "N", Config: map[string]any{"agent_profile": "ghost"}}
		err := eng.checkAgentProfile(c, n)
		if err == nil || !strings.Contains(err.Error(), "已删除") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("nil skills skipped", func(t *testing.T) {
		eng2 := &Engine{db: db, skills: nil}
		n := &models.Node{ID: "n", Label: "N", Config: map[string]any{"agent_profile": "ghost"}}
		if err := eng2.checkAgentProfile(c, n); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

}
