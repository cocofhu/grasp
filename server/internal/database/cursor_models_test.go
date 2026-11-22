package database

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/cocofhu/grasp/internal/clicatalog"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestLegacyCursorModelsLandOnOfferedChoices(t *testing.T) {
	offered := map[string]clicatalog.Model{}
	for _, m := range clicatalog.Models("cursor") {
		offered[m.ID] = m
	}
	for id, to := range legacyCursorModels {
		m, ok := offered[to.base]
		if !ok || id == to.base {
			t.Fatalf("%s: base %q is not a catalog model", id, to.base)
		}
		if to.effort != "" && !slices.Contains(m.Efforts, to.effort) {
			t.Fatalf("%s: %s does not offer effort %q", id, to.base, to.effort)
		}
		if to.tier != "" && !slices.Contains(m.ServiceTiers, to.tier) {
			t.Fatalf("%s: %s does not offer tier %q", id, to.base, to.tier)
		}
	}
}

func TestMigrateLegacyCursorModels(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "cursor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	platform := func(model string, aux ...string) modelrouting.Binding {
		return modelrouting.Binding{Kind: "platform", CredentialID: "cred", ModelID: model, AuxiliaryModelIDs: aux}
	}
	for _, c := range []models.ModelConfiguration{
		{ID: "cur", Backend: "cursor", Binding: platform("claude-opus-5-5-high", "gpt-5.5-medium-fast", "gpt-5.5-low", "claude-opus-5-5-max", "composer-2.5"), Revision: 1},
		{ID: "base", Backend: "cursor", Binding: platform("composer-2.5"), Revision: 1},
		{ID: "codex", Backend: "codex", Binding: platform("gpt-5.5-high"), Revision: 1},
	} {
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	sessions := []models.SoloSession{
		// Follows the primary explicitly: keeps the old primary's level.
		{ID: "primary", InitialConfigurationID: "cur", CurrentConfigurationID: "cur", CurrentModelSet: true, AppliedConfigurationID: "cur", AppliedModelSet: true},
		// An explicit model: its own level and speed, unless already chosen.
		{ID: "aux", InitialConfigurationID: "cur", InitialModelID: "gpt-5.5-medium-fast", CurrentConfigurationID: "cur", CurrentModelID: "gpt-5.5-medium-fast", CurrentModelSet: true, CurrentReasoningEffort: "high"},
		// A row that never stored a current model is left alone.
		{ID: "unset", CurrentConfigurationID: "cur"},
		{ID: "other", CurrentConfigurationID: "codex", CurrentModelID: "gpt-5.5-high", CurrentModelSet: true},
	}
	for _, s := range sessions {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.ReactConversation{ID: 7, CurrentConfigurationID: "cur", CurrentModelID: "claude-opus-5-5-max", CurrentModelSet: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Run{ID: "run", LaunchModelSet: true, LaunchConfigurationID: "cur", LaunchModelID: "cursor-grok-4.6-xhigh-fast"}).Error; err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := migrateLegacyCursorModels(db); err != nil {
			t.Fatal(err)
		}
	}

	var cur, codex models.ModelConfiguration
	db.First(&cur, "id = ?", "cur")
	db.First(&codex, "id = ?", "codex")
	if cur.Binding.ModelID != "claude-opus-5-5" || !slices.Equal(cur.Binding.AuxiliaryModelIDs, []string{"gpt-5.5", "composer-2.5"}) || cur.Revision != 2 {
		t.Fatalf("cursor configuration %+v revision %d", cur.Binding, cur.Revision)
	}
	if codex.Binding.ModelID != "gpt-5.5-high" || codex.Revision != 1 {
		t.Fatalf("codex configuration changed: %+v", codex.Binding)
	}
	got := map[string]models.SoloSession{}
	var rows []models.SoloSession
	db.Find(&rows)
	for _, r := range rows {
		got[r.ID] = r
	}
	if p := got["primary"]; p.CurrentModelID != "" || p.CurrentReasoningEffort != "high" || p.AppliedReasoningEffort != "high" || p.CurrentServiceTier != "" {
		t.Fatalf("primary session %+v", p)
	}
	if a := got["aux"]; a.InitialModelID != "gpt-5.5" || a.CurrentModelID != "gpt-5.5" || a.CurrentReasoningEffort != "high" || a.CurrentServiceTier != "fast" {
		t.Fatalf("aux session %+v", a)
	}
	if u := got["unset"]; u.CurrentReasoningEffort != "" || u.CurrentModelID != "" {
		t.Fatalf("unset session %+v", u)
	}
	if o := got["other"]; o.CurrentModelID != "gpt-5.5-high" || o.CurrentReasoningEffort != "" {
		t.Fatalf("codex session changed: %+v", o)
	}
	var visit models.ReactConversation
	db.First(&visit, 7)
	if visit.CurrentModelID != "claude-opus-5-5" || visit.CurrentReasoningEffort != "max" {
		t.Fatalf("visit %+v", visit)
	}
	var run models.Run
	db.First(&run, "id = ?", "run")
	if run.LaunchModelID != "grok-4.6" || run.LaunchReasoningEffort != "xhigh" || run.LaunchServiceTier != "fast" {
		t.Fatalf("run %+v", run)
	}
}
