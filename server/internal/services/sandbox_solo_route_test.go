package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/execution"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type fakeSoloRouteRuntime struct {
	specs       []sandbox.Spec
	previous    [][]string
	containers  []*sandbox.Sandbox
	failReprime int
	failReady   int
	failConnect int
	readyCalls  int
	connects    int
}

func (f *fakeSoloRouteRuntime) Reprime(_ context.Context, sb *sandbox.Sandbox, spec sandbox.Spec, previous []string) error {
	f.specs = append(f.specs, spec)
	f.previous = append(f.previous, slices.Clone(previous))
	f.containers = append(f.containers, sb)
	if len(f.specs) == f.failReprime {
		return errors.New("reprime unavailable")
	}
	sb.ConfigRoot = spec.ConfigRoot
	return nil
}

func (f *fakeSoloRouteRuntime) Ready(context.Context, *sandbox.Sandbox) error {
	f.readyCalls++
	if f.readyCalls == f.failReady {
		return errors.New("new bridge unavailable")
	}
	return nil
}

func (f *fakeSoloRouteRuntime) Connect(context.Context, *sandbox.Sandbox, []sandbox.MCPServerSpec) (*sandbox.ACPClient, error) {
	f.connects++
	if f.connects == f.failConnect {
		return nil, errors.New("new ACP unavailable")
	}
	return &sandbox.ACPClient{}, nil
}

func soloRouteFixture(t *testing.T) (*SandboxService, *models.Sandbox, Agent, Agent, *fakeSoloRouteRuntime) {
	t.Helper()
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	agent, _ := s.skills.Get("agentA")
	agent = s.effectiveAgent(agent)
	agent.ModelBinding = modelrouting.ConfigurationReference("sandbox-fixture")
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent)
	if err != nil {
		t.Fatal(err)
	}
	row := &models.Sandbox{Name: "same-container", Profile: agent.Name, ThreadID: "same-task", RunID: "same-artifacts", Token: "same-token", Purpose: SandboxPurposeSolo, Status: "running", ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: row.ThreadID, AgentName: agent.Name, UserID: "alice", SandboxID: row.ID, Status: models.SoloStatusRunning, InitialConfigurationID: "sandbox-fixture"}).Error; err != nil {
		t.Fatal(err)
	}
	prepared, err := s.prepareAgentContainer(row.Profile, row.ThreadID, row.RunID, row.Token, row.Name, s.soloPlatformSpecs(row), agent, agentContainerExtra{Resident: true, LaunchKind: "solo"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := agent.Layout.WorkspaceDir
	if workspace == "" {
		workspace = "/root/workspace"
	}
	s.live[row.ID] = &liveSandbox{sb: &sandbox.Sandbox{ID: row.Name, Name: row.Name, WorkspaceDir: workspace, ConfigRoot: agent.Layout.ConfigRoot, Password: row.Token}, acp: &sandbox.ACPClient{}, home: prepared.spec.ConfigHome, prepared: prepared, soloRouteAgent: &agent, modelBindingFingerprint: modelFingerprint, gitBindingsFingerprint: gitFingerprint}
	t.Cleanup(func() { s.teardownLive(row.ID) })
	target := agent
	target.AcpBackend = "codex"
	target.Layout.ConfigRoot = "/root/.codex"
	target.ModelBinding = modelrouting.ConfigurationReference("second-group")
	ops := &fakeSoloRouteRuntime{}
	s.soloRouteRuntime = ops
	return s, row, agent, target, ops
}

func TestSoloRouteSwitchPreservesWorkspaceAndActualGroup(t *testing.T) {
	s, row, original, target, ops := soloRouteFixture(t)
	before := s.live[row.ID]
	container, workspace, oldHome := before.sb, before.sb.WorkspaceDir, before.home
	changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, target)
	if err != nil || !changed {
		t.Fatalf("switch: changed=%v err=%v", changed, err)
	}
	if len(ops.specs) != 1 || ops.containers[0] != container || container.WorkspaceDir != workspace || ops.specs[0].Env["AGENT_PROVIDER"] != "codex" {
		t.Fatalf("workspace or route changed incorrectly: specs=%+v container=%+v", ops.specs, container)
	}
	if !slices.Contains(ops.previous[0], "CURSOR_API_KEY") || ops.specs[0].Env["CURSOR_API_KEY"] != "" {
		t.Fatalf("previous tool credentials were not removed: old=%v env=%v", ops.previous[0], ops.specs[0].Env)
	}
	if _, err := os.Stat(oldHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previous host config not released: %v", err)
	}
	if container.Name != row.Name || s.live[row.ID].soloRouteAgent.ModelBinding.ConfigurationID != "second-group" {
		t.Fatal("container replaced or applied group lost")
	}
	var stored models.SoloSession
	if err := s.db.First(&stored, "id = ?", row.ThreadID).Error; err != nil || !stored.ModelContextPending {
		t.Fatalf("new tool must receive history even after cancellation: pending=%v err=%v", stored.ModelContextPending, err)
	}
	if changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, target); err != nil || changed || len(ops.specs) != 1 {
		t.Fatalf("unchanged route restarted: changed=%v err=%v calls=%d", changed, err, len(ops.specs))
	}
	// Reverting a failed model-setting change must restore the earlier group,
	// including a different CLI, in the same workspace.
	if changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, original); err != nil || !changed || len(ops.specs) != 2 || ops.containers[1] != container {
		t.Fatalf("switch back: changed=%v err=%v calls=%d", changed, err, len(ops.specs))
	}
	if container.ConfigRoot != original.Layout.ConfigRoot || ops.specs[1].Env["AGENT_PROVIDER"] != original.AcpBackend {
		t.Fatal("previous tool config was not restored")
	}
}

func TestSoloRouteSwitchFailureRestoresPreviousRuntime(t *testing.T) {
	for _, failure := range []string{"reprime", "ready", "connect"} {
		t.Run(failure, func(t *testing.T) {
			s, row, original, target, ops := soloRouteFixture(t)
			oldFingerprint, oldHome := row.ModelBindingFingerprint, s.live[row.ID].home
			switch failure {
			case "reprime":
				ops.failReprime = 1
			case "ready":
				ops.failReady = 1
			case "connect":
				ops.failConnect = 1
			}
			changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, target)
			if err == nil || changed || !strings.Contains(err.Error(), "已恢复原配置") {
				t.Fatalf("failure must not commit selection: changed=%v err=%v", changed, err)
			}
			if len(ops.specs) != 2 || ops.specs[1].Env["AGENT_PROVIDER"] != original.AcpBackend || ops.containers[0] != ops.containers[1] {
				t.Fatalf("rollback did not retain container: %+v", ops.specs)
			}
			var stored models.Sandbox
			s.db.First(&stored, row.ID)
			if stored.ModelBindingFingerprint != oldFingerprint || s.live[row.ID].home != oldHome || s.live[row.ID].soloRouteAgent.ModelBinding.ConfigurationID != original.ModelBinding.ConfigurationID {
				t.Fatal("failed switch changed persisted/live applied configuration")
			}
			if s.live[row.ID].switching {
				t.Fatal("switch lock leaked")
			}
		})
	}
}

func TestSoloRouteSwitchRejectsBusyGitAndImageChanges(t *testing.T) {
	s, row, _, target, ops := soloRouteFixture(t)
	s.live[row.ID].busy = true
	if _, err := s.AlignSoloModelRoute(context.Background(), row.ID, target); err == nil {
		t.Fatal("must not interrupt a live turn")
	}
	s.live[row.ID].busy = false
	if err := s.db.Model(row).Update("git_bindings_fingerprint", "old-git").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.AlignSoloModelRoute(context.Background(), row.ID, target); err == nil || !strings.Contains(err.Error(), "Git 授权") {
		t.Fatalf("must preserve Git permission guard: %v", err)
	}
	if err := s.db.Model(row).Update("git_bindings_fingerprint", s.live[row.ID].gitBindingsFingerprint).Error; err != nil {
		t.Fatal(err)
	}
	previous := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(previous) })
	config.StoreConfig(&config.Config{Sandbox: config.SandboxConfig{Images: map[string]string{"cursor": "cursor-only", "codex": "codex-only"}}})
	if _, err := s.AlignSoloModelRoute(context.Background(), row.ID, target); err == nil || !strings.Contains(err.Error(), "不同的沙箱镜像") {
		t.Fatalf("image mismatch must be explicit: %v", err)
	}
	if len(ops.specs) != 0 {
		t.Fatal("validation failure touched running workspace")
	}
}

func TestSoloRouteSwitchKeepsRepoSnapshotAndAppliedModelOnRollback(t *testing.T) {
	s, row, _, target, ops := soloRouteFixture(t)
	var session models.SoloSession
	if err := s.db.First(&session, "id = ?", row.ThreadID).Error; err != nil {
		t.Fatal(err)
	}
	session.Repos = []models.SessionRepo{{Name: "app", RepoURL: "https://example.test/team/app.git", RepoDirName: "app", Branch: "feature/orders"}}
	session.CurrentModelSet, session.CurrentModelID, session.CurrentReasoningEffort = true, "new-auxiliary", "high"
	session.CurrentConfigurationID = target.ModelBinding.ConfigurationID
	session.AppliedModelSet, session.AppliedModelID, session.AppliedReasoningEffort = true, "old-auxiliary", "medium"
	if err := s.db.Save(&session).Error; err != nil {
		t.Fatal(err)
	}
	ops.failReady = 1
	if _, err := s.AlignSoloModelRoute(context.Background(), row.ID, target); err == nil {
		t.Fatal("failure expected")
	}
	if len(ops.specs) != 2 {
		t.Fatalf("expected forward and rollback: %+v", ops.specs)
	}
	forward, rollback := ops.specs[0].Env, ops.specs[1].Env
	if forward["GIT_REPOS"] != "app|https://example.test/team/app.git|feature/orders" {
		t.Fatalf("lost frozen repo snapshot: %q", forward["GIT_REPOS"])
	}
	if forward[runtime.EnvInitialChatModel] != "new-auxiliary" || forward[runtime.EnvACPBridgeReasoningEffort] != "high" {
		t.Fatalf("target did not start at requested model settings: %+v", forward)
	}
	if rollback[runtime.EnvInitialChatModel] != "old-auxiliary" || rollback[runtime.EnvACPBridgeReasoningEffort] != "medium" {
		t.Fatalf("rollback reused stale opening settings: %+v", rollback)
	}
	if err := s.db.First(&session, "id = ?", row.ThreadID).Error; err != nil || !session.ModelContextPending {
		t.Fatalf("restored fresh tool needs context: pending=%v err=%v", session.ModelContextPending, err)
	}
}

func TestSoloRouteSwitchRecoversAfterOldGroupDisappears(t *testing.T) {
	s, row, _, target, ops := soloRouteFixture(t)
	old := s.live[row.ID]
	_ = os.RemoveAll(old.home)
	// A platform restart retained the container but forgot its config snapshot.
	old.home, old.prepared, old.soloRouteAgent = "", nil, nil
	old.sb.Image = s.routeImage(target.AcpBackend)
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Updates(map[string]any{"applied_configuration_id": "deleted-group", "applied_model_set": true}).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, target)
	if err != nil || !changed || len(ops.specs) != 1 {
		t.Fatalf("valid new group should recover workspace: changed=%v calls=%d err=%v", changed, len(ops.specs), err)
	}
	if !slices.Contains(ops.previous[0], "CURSOR_API_KEY") || !slices.Contains(ops.previous[0], runtime.EnvModelAPIKey) {
		t.Fatalf("forgotten original auth keys survived recovery: %v", ops.previous[0])
	}
	if s.live[row.ID].sb != old.sb || old.sb.Name != row.Name {
		t.Fatal("recovery replaced workspace")
	}
}

func TestSoloRouteSwitchFailedRollbackForcesRecovery(t *testing.T) {
	s, row, original, target, ops := soloRouteFixture(t)
	ops.failReady, ops.failConnect = 1, 1
	if changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, target); err == nil || changed || !strings.Contains(err.Error(), "恢复原配置也失败") {
		t.Fatalf("must report incomplete rollback: changed=%v err=%v", changed, err)
	}
	var stored models.Sandbox
	s.db.First(&stored, row.ID)
	if stored.ModelBindingFingerprint != "" {
		t.Fatal("must not trust the old fingerprint after incomplete rollback")
	}
	ops.failReady, ops.failConnect = 0, 0
	if changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, original); err != nil || !changed || len(ops.specs) != 3 {
		t.Fatalf("must recover original route in same container: changed=%v err=%v calls=%d", changed, err, len(ops.specs))
	}
}

func TestSoloRouteRefreshesConfiguredChoicesWithoutChangingPrimary(t *testing.T) {
	s, row, original, _, ops := soloRouteFixture(t)
	resolve := s.modelBinding
	s.modelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		resolved, err := resolve(backend, binding)
		resolved.Binding.AuxiliaryModelIDs = []string{"newly-added-model"}
		return resolved, err
	}
	changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, original)
	if err != nil || !changed || len(ops.specs) != 1 {
		t.Fatalf("same primary with changed allow-list must refresh: changed=%v calls=%d err=%v", changed, len(ops.specs), err)
	}
	if !strings.Contains(ops.specs[0].Env[runtime.EnvConfiguredModels], "newly-added-model") {
		t.Fatal("updated selector model was not applied to the sandbox")
	}
}

type worktreeSoloRouteOps struct {
	*fakeSoloRouteRuntime
	manager *sandbox.Manager
}

func (o worktreeSoloRouteOps) Reprime(ctx context.Context, sb *sandbox.Sandbox, spec sandbox.Spec, keys []string) error {
	return o.manager.Reprime(ctx, sb, spec, keys)
}

func TestSoloRouteSwitchUsesSameLocalWorktreeAndPersistsNewPort(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[rollback], func(t *testing.T) {
			backendPath, err := filepath.Abs(filepath.Join("..", "..", "..", "sandbox-gateway", "sandbox", "bin", "backend"))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GRASP_WORKTREE_BACKEND", backendPath)
			t.Setenv(execution.EnvMode, execution.ModeWorktree)
			t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
			execution.ResetForTest()
			t.Cleanup(execution.ResetForTest)
			t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
			started := 0
			t.Cleanup(sandbox.SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
				started++
				return 16000 + started, func() {}, nil
			}))
			s, row, _, target, fake := soloRouteFixture(t)
			local, err := s.mgr.Create(context.Background(), s.live[row.ID].prepared.spec)
			if err != nil {
				t.Fatal(err)
			}
			s.live[row.ID].sb = local
			root := local.WorkspaceDir
			if err := os.WriteFile(filepath.Join(root, "progress.txt"), []byte("continue this task"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(row).Updates(map[string]any{"host": local.Host, "acp_port": local.Port}).Error; err != nil {
				t.Fatal(err)
			}
			s.soloRouteRuntime = worktreeSoloRouteOps{fake, s.mgr}
			if rollback {
				fake.failReady = 1
			}
			changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, target)
			if rollback && (err == nil || changed) {
				t.Fatalf("rollback: changed=%v err=%v", changed, err)
			}
			if !rollback && (err != nil || !changed) {
				t.Fatalf("switch: changed=%v err=%v", changed, err)
			}
			var stored models.Sandbox
			if err := s.db.First(&stored, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.ACPPort != local.Port || stored.Host != "127.0.0.1" || local.Port == 16001 || s.live[row.ID].sb != local || local.WorkspaceDir != root {
				t.Fatalf("lost local endpoint/workspace: row=%+v handle=%+v", stored, local)
			}
			if body, err := os.ReadFile(filepath.Join(root, "progress.txt")); err != nil || string(body) != "continue this task" {
				t.Fatalf("lost task work: %q %v", body, err)
			}
		})
	}
}
