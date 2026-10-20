package workspacemigration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestMergeWithoutSharedFilesSkipsEmptyRevision(t *testing.T) {
	db, opts := fixture(t, true)
	for _, id := range []string{"workspace-a", "workspace-b"} {
		if err := os.RemoveAll(filepath.Join(opts.SharedRoot, id, "workspace")); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", plan.Blockers)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatalf("apply failed for Agents without shared files: %v", err)
	}
	if err := Verify(db, plan, backup); err != nil {
		t.Fatal(err)
	}
	agent, ok := services.NewAgentService(opts.ProfilesRoot).Get("agent-workspace-b")
	if !ok || agent.ProjectID != opts.CanonicalID || agent.Env["BASE"] != "Beta" {
		t.Fatalf("effective agent not materialized: %+v", agent)
	}
	revisions, err := services.NewWorkspaceVcsService(opts.ProfilesRoot).ListRevisions(agent.Name, 10)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("expected only the baseline revision: %v %d", err, len(revisions))
	}
}

func TestMergeDefaultsForInheritingWorkflowOutsideWorkspace(t *testing.T) {
	db, opts := fixture(t, true)
	if err := db.Model(&models.Project{ID: "workspace-b"}).Select("Variables", "PmGateAutoVar", "PmGateAutoPrompt").Updates(models.Project{
		Variables: []models.ProjectVariable{
			{Name: "region", Type: "string", Value: "beta-region"},
			{Name: "beta_token", Type: "string", Value: "beta-secret", Secret: true},
		},
		PmGateAutoVar: "auto_gate", PmGateAutoPrompt: "beta prompt",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{ID: "flow-workspace-b"}).Select("Defaults").Updates(models.WorkflowDef{Defaults: &models.WorkflowDefaults{
		InheritWorkspace: true,
		Variables:        []models.ProjectVariable{{Name: "region", Type: "string", Value: "wf-own"}, {Name: "wf_only", Type: "string", Value: "x"}},
	}}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(db, plan, filepath.Join(t.TempDir(), "backup"), true); err != nil {
		t.Fatal(err)
	}
	var flow models.WorkflowDef
	if err := db.First(&flow, "id = ?", "flow-workspace-b").Error; err != nil {
		t.Fatal(err)
	}
	d := flow.Defaults
	if d == nil || d.InheritWorkspace {
		t.Fatalf("workflow still inherits the retained workspace: %+v", d)
	}
	values := map[string]models.ProjectVariable{}
	for _, v := range d.Variables {
		values[v.Name] = v
	}
	if len(values) != 3 || values["region"].Value != "wf-own" || values["wf_only"].Value != "x" {
		t.Fatalf("workflow values must win over the source project: %+v", d.Variables)
	}
	if !values["beta_token"].Secret || values["beta_token"].Value != "beta-secret" {
		t.Fatalf("source secret variable lost: %+v", values["beta_token"])
	}
	if d.NotifyPolicy == nil || d.NotifyPolicy.FailedTemplate != "failure {project}" {
		t.Fatalf("source notify policy not filled in: %+v", d.NotifyPolicy)
	}
	if d.GateAuto == nil || d.GateAuto.Variable != "auto_gate" || d.GateAuto.Prompt != "beta prompt" {
		t.Fatalf("source gate policy not filled in: %+v", d.GateAuto)
	}
	var canonical models.WorkflowDef
	if err := db.First(&canonical, "id = ?", "flow-workspace-a").Error; err != nil {
		t.Fatal(err)
	}
	if canonical.Defaults != nil {
		t.Fatalf("retained workspace workflow gained overrides: %+v", canonical.Defaults)
	}
}

func TestMergeDefaultsOverlayOrder(t *testing.T) {
	base := []models.ProjectVariable{{Name: "a", Value: "1", Secret: true}, {Name: "b", Value: "2"}}
	got := mergeDefaults(base, []models.ProjectVariable{{Name: "b", Value: "own"}, {Name: "c", Value: "3"}})
	if len(got) != 3 || got[0].Value != "1" || !got[0].Secret || got[1].Value != "own" || got[2].Name != "c" {
		t.Fatalf("unexpected merge %+v", got)
	}
	if base[1].Value != "2" {
		t.Fatal("merge mutated the source variables")
	}
}

func TestSharedPublishFailureRestoresBothTrees(t *testing.T) {
	db, opts := fixture(t, true)
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	profilesBefore, _ := scanFiles(opts.ProfilesRoot)
	sharedBefore, _ := scanFiles(opts.SharedRoot)
	backup := filepath.Join(t.TempDir(), "backup")
	publish := func(root, staged, held string) error {
		if root == opts.SharedRoot {
			return errors.New("synthetic shared failure")
		}
		return publishTree(root, staged, held)
	}
	if err := applyWithPublish(db, plan, backup, true, publish); err == nil {
		t.Fatal("expected shared publication failure")
	}
	var journal Journal
	if err := readJSON(filepath.Join(backup, "journal.json"), &journal); err != nil || journal.Stage != "profiles-published" {
		t.Fatalf("wrong recovery stage %q: %v", journal.Stage, err)
	}
	if err := Rollback(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	profilesAfter, _ := scanFiles(opts.ProfilesRoot)
	sharedAfter, _ := scanFiles(opts.SharedRoot)
	if digest(profilesAfter) != digest(profilesBefore) || digest(sharedAfter) != digest(sharedBefore) {
		t.Fatal("rollback did not restore both file trees")
	}
	for _, kind := range []string{"profiles", "shared"} {
		for _, path := range []string{stageRoot(plan, kind), heldRoot(plan, kind)} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("migration path left behind: %s", path)
			}
		}
	}
	restored, err := BuildPlan(db, opts)
	if err != nil || restored.Fingerprint != plan.Fingerprint {
		t.Fatalf("partial migration not restored: %v", err)
	}
}

func TestLoginSessionsDoNotBlockVerifyOrRollback(t *testing.T) {
	db, opts := fixture(t, true)
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Session{ID: "after-migration", Username: "admin", ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Verify(db, plan, backup); err != nil {
		t.Fatalf("a login after migration counted as a business write: %v", err)
	}
	if err := Rollback(db, plan, backup, true); err != nil {
		t.Fatalf("a login after migration blocked rollback: %v", err)
	}
	restored, err := BuildPlan(db, opts)
	if err != nil || restored.Fingerprint != plan.Fingerprint {
		t.Fatalf("rollback did not restore the source: %v", err)
	}
}

func TestRetainedWorkspaceKeepsDuplicateNames(t *testing.T) {
	db, opts := fixture(t, true)
	opts.CanonicalID = "workspace-b"
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.WorkflowRenames["flow-workspace-b"]; ok || plan.WorkflowRenames["flow-workspace-a"] == "" {
		t.Fatalf("retained workflow renamed: %+v", plan.WorkflowRenames)
	}
	if _, ok := plan.MemoryRenames["memory-workspace-b"]; ok || plan.MemoryRenames["memory-workspace-a"] == "" {
		t.Fatalf("retained memory renamed: %+v", plan.MemoryRenames)
	}
}

func TestPlanWarnsWhenRetainedNotificationsAreOff(t *testing.T) {
	db, opts := fixture(t, true)
	off := false
	if err := db.Model(&models.Project{ID: "workspace-a"}).Select("NotifyPolicy").Updates(models.Project{NotifyPolicy: models.ProjectNotifyPolicy{Enabled: &off}}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range plan.Warnings {
		found = found || warning.Code == "workspace-notify-disabled" && warning.Resource == "workspace-b"
	}
	if !found {
		t.Fatalf("missing notification warning: %+v", plan.Warnings)
	}
}

func TestMariaDBCollationPadding(t *testing.T) {
	for collation, want := range map[string]string{
		"utf8mb4_general_ci": "PAD SPACE", "utf8mb4_uca1400_ai_ci": "PAD SPACE",
		"utf8mb4_nopad_bin": "NO PAD", "utf8mb4_uca1400_nopad_ai_ci": "NO PAD",
	} {
		if got := mariaDBPadding(collation); got != want {
			t.Fatalf("%s: got %s want %s", collation, got, want)
		}
	}
}
