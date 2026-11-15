package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func TestColdRestoreFinalCommitFailureKeepsAllocatedCapacity(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	failed := false
	name := "test:fail_recovery_running_commit"
	if err := f.db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if !failed && ok && tx.Statement.Table == "sandboxes" && values["status"] == "running" {
			failed = true
			tx.AddError(errors.New("database unavailable after guest restore"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Update().Remove(name) })
	if _, _, err := f.sbx.EnsureRunning(context.Background(), row.ID); err == nil || !failed {
		t.Fatal("test did not interrupt final running commit")
	}
	if observed, err := f.sbx.mgr.ProbeStatus(context.Background(), row.Name); err != nil || observed != "running" {
		t.Fatalf("guest=%s %v", observed, err)
	}
	if capacity, err := f.sbx.Capacity(); err != nil || capacity.Used != 1 {
		t.Fatalf("allocated guest lost its slot: %+v %v", capacity, err)
	}
	if saved := f.recovery(t); (saved.Status != "resuming" && saved.Status != "error") || len(saved.RecoveryPoints) != 1 {
		t.Fatalf("failed commit lost recovery intent: %+v", saved)
	}
}

func TestColdSoloBridgeRetryDoesNotOverwriteRestoredWorkspace(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	// An idle-saved Solo session stays open and wakes on its next message.
	f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("status", models.SoloStatusRunning)
	f.sbx.soloBridgeReady = func(context.Context, *sandbox.Sandbox) error {
		return errors.New("bridge connection delayed")
	}
	if err := f.sbx.EnsureSoloRunning(context.Background(), row.ID); err == nil {
		t.Fatal("bridge failure was hidden")
	}
	if fresh := f.recovery(t); fresh.Status != "running" || len(f.exec.restoredArchives()) != 1 || !f.row(t).ResumeContextPending {
		t.Fatal("completed restore was not retained for a delayed bridge")
	}
	f.sbx.soloBridgeReady = func(context.Context, *sandbox.Sandbox) error { return nil }
	if err := f.sbx.EnsureSoloRunning(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.exec.restoredArchives()) != 1 {
		t.Fatal("bridge retry overwrote the live workspace with its old archive")
	}
}

func TestColdRestoreRetriesMissingWorkloadWithoutDiscardingGatewaySpec(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	key := row.RecoveryPoints[0].Key
	// The gateway retains the original spec after a failed Create, while the
	// driver's live observation correctly reports that no workload exists.
	f.fg.SetStatus(row.Name, "not_found")
	f.fg.FailStart = true
	if _, _, err := f.sbx.EnsureRunning(context.Background(), row.ID); err == nil {
		t.Fatal("failed recreate reported success")
	}
	if saved := f.recovery(t); len(saved.RecoveryPoints) != 1 || saved.RecoveryPoints[0].Key != key {
		t.Fatal("failed recreate lost its latest archive")
	}
	f.fg.FailStart = false
	sb, warm, err := f.sbx.EnsureRunning(context.Background(), row.ID)
	if err != nil || warm || sb.ID != row.Name || len(f.exec.restoredArchives()) != 1 {
		t.Fatalf("explicit retry failed: sb=%+v warm=%v err=%v", sb, warm, err)
	}
}

func TestColdRecoveryUsesAppliedToolAfterSwitchAndProcessRestart(t *testing.T) {
	f := newStopFixture(t)
	agent, _ := f.sbx.skills.Get("agentA")
	agent.ModelBinding.AllConfigurations = true
	if err := f.sbx.skills.Save(agent); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.ModelConfiguration{ID: "recovery-codex", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "selected", ModelID: "primary"}}).Error; err != nil {
		t.Fatal(err)
	}
	row := f.recovery(t)
	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Updates(map[string]any{"current_configuration_id": "recovery-codex", "current_model_id": "aux", "current_model_set": true, "current_reasoning_effort": "high"}).Error; err != nil {
		t.Fatal(err)
	}
	target, err := selectSoloConfiguration(f.db, f.sbx.effectiveAgent(agent), "recovery-codex")
	if err != nil {
		t.Fatal(err)
	}
	f.sbx.soloRouteRuntime = &fakeSoloRouteRuntime{}
	if changed, err := f.sbx.AlignSoloModelRoute(context.Background(), row.ID, target); err != nil || !changed {
		t.Fatalf("switch=%v %v", changed, err)
	}
	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Updates(map[string]any{"applied_configuration_id": "recovery-codex", "applied_model_id": "aux", "applied_model_set": true, "applied_reasoning_effort": "high"}).Error; err != nil {
		t.Fatal(err)
	}
	f.stop(t)
	// A later queued selection has not run yet and must not become the restored
	// tool. Also discard all in-memory prepared configuration, as a restart does.
	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("current_configuration_id", "sandbox-fixture").Error; err != nil {
		t.Fatal(err)
	}
	f.sbx.mu.Lock()
	f.sbx.live = map[uint]*liveSandbox{}
	f.sbx.mu.Unlock()
	sb, warm, err := f.sbx.EnsureRunning(context.Background(), row.ID)
	if err != nil || warm || sb.ID != row.Name {
		t.Fatalf("restore=%+v warm=%v err=%v", sb, warm, err)
	}
	env := f.fg.LastCreateEnv()
	if env["AGENT_PROVIDER"] != "codex" || env["CONFIG_ROOT"] != "/root/.codex" || env[runtime.EnvInitialChatModel] != "aux" || env[runtime.EnvACPBridgeReasoningEffort] != "high" {
		t.Fatal("restored guest did not use the last applied tool/model/reasoning")
	}
	if _, present := env["CURSOR_API_KEY"]; present {
		t.Fatal("old tool credential survived cold rebuild")
	}
	if env["SANDBOX_INJECT"] == "" {
		t.Fatal("recovered guest has no config or Git inject")
	}
	if len(f.exec.restoredArchives()) != 1 {
		t.Fatal("workspace was not restored")
	}
}

func TestColdRecoveryResolvesCurrentPrivateGitAndMCPWithoutCloning(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	agent, _ := f.sbx.skills.Get(row.Profile)
	agent.MCP = []MCPServer{{Name: "private-tool", URL: "http://new-mcp/service", Headers: map[string]string{"Authorization": "${credential:selected-mcp}"}}}
	if err := f.sbx.skills.Save(agent); err != nil {
		t.Fatal(err)
	}
	f.sbx.credentialReferences = func() map[string]string { return map[string]string{"credential:selected-mcp": "fresh-mcp-token"} }
	row.Repos = []models.SessionRepo{{RepoURL: "https://github.com/example/private.git", ProjectID: "project"}}
	f.sbx.project = func(id string) (*models.Project, bool) {
		return &models.Project{ID: "project", RepoURL: row.Repos[0].RepoURL, CredentialID: "cred-proj"}, id == "project"
	}
	f.sbx.gitCredential = func(id string) (runtime.GitCredentialValue, bool) {
		return runtime.GitCredentialValue{EnvKey: "GITHUB_TOKEN", Secret: "rotated-private-token", Host: "https://github.com"}, id == "cred-proj"
	}
	spec, err := f.sbx.prepareRecoveryInputs(row)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(spec.ConfigHome) }()
	if spec.Env["GIT_REPOS"] != "" || len(spec.GitCredentials) != 1 || spec.GitCredentials[0].Secret != "rotated-private-token" {
		t.Fatal("recovery attempted to clone or lost current private repository credentials")
	}
	data, err := os.ReadFile(filepath.Join(spec.ConfigHome, "mcp.json"))
	if err != nil || !strings.Contains(string(data), "http://new-mcp/service") || !strings.Contains(string(data), "fresh-mcp-token") {
		t.Fatalf("MCP config was not regenerated: %v", err)
	}
	if row.GitBindingsFingerprint != gitBindingsFingerprint(spec.GitCredentials) {
		t.Fatal("saved authorization fingerprint was not refreshed")
	}
}

type recoverySpecRecorder struct {
	req   runtime.NodeReq
	calls int
}

func (r *recoverySpecRecorder) PrepareSandboxRecovery(req runtime.NodeReq) (sandbox.Spec, error) {
	r.req = req
	r.calls++
	return sandbox.Spec{}, nil
}

func TestWorkflowColdRecoveryReadsDurableAppliedRouteAndFrozenGraph(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	row.Purpose, row.NodeID, row.RunID = "run", "review", "saved-workflow"
	caps := &models.AgentCapabilities{}
	run := models.Run{ID: row.RunID, Status: "waiting_human", WorkflowID: "workflow", Graph: models.Graph{Nodes: []models.Node{{ID: row.NodeID, Type: "agent", Config: map[string]any{"agent_profile": row.Profile}, Caps: caps}}}}
	if err := f.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.RunVariable{RunID: run.ID, Name: "repo", Value: "persisted-value"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.ReactConversation{RunID: run.ID, NodeID: row.NodeID, Iteration: 3, AppliedModelSet: true, AppliedConfigurationID: "sandbox-fixture", AppliedModelID: "applied-model", AppliedReasoningEffort: "high", CurrentConfigurationID: "not-applied", CurrentModelID: "queued-model"}).Error; err != nil {
		t.Fatal(err)
	}
	recorder := &recoverySpecRecorder{}
	f.sbx.SetRunRecoveryPreparer(recorder)
	if _, err := f.sbx.prepareRecoveryInputs(row); err != nil {
		t.Fatal(err)
	}
	req := recorder.req
	if req.ChatRoute == nil || req.ChatRoute.ModelID != "applied-model" || req.ChatRoute.ConfigurationID != "sandbox-fixture" || req.ChatRoute.ReasoningEffort != "high" || req.Token != row.Token || req.Vars["repo"] != "persisted-value" || req.Caps == nil {
		t.Fatalf("incomplete recovery context: %+v", req)
	}
	f.db.Model(&models.Run{}).Where("id = ?", run.ID).Update("status", "cancelled")
	if _, err := f.sbx.prepareRecoveryInputs(row); err == nil || recorder.calls != 1 {
		t.Fatal("cancelled run recreated its environment")
	}
}

func TestColdRecoveryMissingConfigurationPreservesLatestPoint(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	key := row.RecoveryPoints[0].Key
	f.sbx.modelBinding = func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{}, errors.New("selected credential revoked")
	}
	if _, _, err := f.sbx.EnsureRunning(context.Background(), row.ID); err == nil {
		t.Fatal("revoked credential started empty environment")
	}
	fresh, err := f.sbx.Get(row.ID)
	if err != nil || fresh.Status != "stopped" || fresh.RecoveryPoints[0].Key != key || len(f.exec.restoredArchives()) != 0 {
		t.Fatalf("failed prepare changed recovery point: %+v %v", fresh, err)
	}
}
