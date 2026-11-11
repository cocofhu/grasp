package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func requireSoloFailure(t *testing.T, err error, code string) {
	t.Helper()
	var failure *SoloError
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// This legacy fixture never had a sandbox; it models an already-released old
// session, without deleting an existing workspace to manufacture availability.
func closedSoloForRestart(t *testing.T, solo *SoloService) *models.SoloSession {
	t.Helper()
	row := &models.SoloSession{ID: "legacy-" + t.Name(), UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed}
	if err := solo.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSoloRestartTwoServicesCannotReuseStaleClosedBinding(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 1)
	old := closedSoloForRestart(t, solo)
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	accepted, err := solo.claimSoloRestart(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	copyBeforeAllocation := *accepted
	_, err = other.claimSoloRestart(context.Background(), old)
	requireSoloFailure(t, err, "session_changed")
	if count, err := countSoloCompute(db, 0); err != nil || count != 1 {
		t.Fatalf("accepted replacement did not retain one quota reservation: count=%d err=%v", count, err)
	}
	if _, err := other.Create(context.Background(), "bob", SoloCreateInput{AgentName: "agentA"}); err == nil {
		t.Fatal("another creation stole the pending replacement's quota")
	}
	if err := solo.advanceSoloRestart(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "winning replacement ready", func() bool {
		row, _ := solo.Require(old.ID, "alice")
		return row.Status == models.SoloStatusRunning
	})
	winner, _ := solo.Require(old.ID, "alice")
	if err := other.advanceSoloRestart(context.Background(), &copyBeforeAllocation); err == nil {
		t.Fatal("stale accepted snapshot reused a newer binding")
	}
	_, err = other.claimSoloRestart(context.Background(), old)
	requireSoloFailure(t, err, "session_changed")
	again, _ := solo.Require(old.ID, "alice")
	var count int64
	if err := db.Model(&models.Sandbox{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if again.SandboxID != winner.SandboxID || again.LifecycleEpoch != accepted.LifecycleEpoch || again.Restarts != 1 || count != 1 {
		t.Fatalf("stale worker replaced/cleared the winner: before=%+v after=%+v count=%d", winner, again, count)
	}
	if _, err := solo.Close(context.Background(), old.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloRestartConcurrentPublicRequestsAllocateOnce(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 2)
	closed := closedSoloForRestart(t, solo)
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 12 {
		group.Go(func() {
			<-start
			service := solo
			if i%2 != 0 {
				service = other
			}
			_, _ = service.Restart(context.Background(), closed.ID, "alice")
		})
	}
	close(start)
	group.Wait()
	waitFor(t, "one concurrent replacement ready", func() bool {
		row, _ := solo.Require(closed.ID, "alice")
		return row.Status == models.SoloStatusRunning
	})
	row, _ := solo.Require(closed.ID, "alice")
	var count int64
	if err := db.Model(&models.Sandbox{}).Count(&count).Error; err != nil || count != 1 || row.Restarts != 1 {
		t.Fatalf("concurrent restart allocated more than once: count=%d restarts=%d err=%v", count, row.Restarts, err)
	}
	if _, err := solo.Close(context.Background(), row.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloStaleCloseAndDeleteRollBackQueueRetirement(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 1)
	old := closedSoloForRestart(t, solo)
	if _, err := solo.Restart(context.Background(), old.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "new epoch ready before stale removal", func() bool { row, _ := solo.Require(old.ID, "alice"); return row.Status == models.SoloStatusRunning })
	winner, _ := solo.Require(old.ID, "alice")
	receipt, err := solo.durable.Admit(context.Background(), soloQueueScope(old.ID), chatsession.QueueIntent{ID: "winning-queued-work", ActorID: "alice", ClientMessageID: "winning-queued-message", Fingerprint: strings.Repeat("a", 64), Payload: []byte(`{"ID":"winning-queued-work","Text":"retain new epoch work"}`)}, 16, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{soloCloseCleanup, soloDeleteCleanup} {
		_, err := solo.claimSoloRemoval(context.Background(), old, action)
		requireSoloFailure(t, err, "session_changed")
		current, err := solo.durable.Receipt(context.Background(), soloQueueScope(old.ID), receipt.Turn.ID)
		if err != nil || current.State != chatsession.TurnQueued {
			t.Fatalf("stale %s retired a successor queue: %+v %v", action, current, err)
		}
		var queue models.ChatQueue
		if err := db.First(&queue, "id = ?", soloQueueScope(old.ID).ID()).Error; err != nil || queue.Retired || queue.CancelRequested {
			t.Fatalf("stale %s left cancellation state: %+v %v", action, queue, err)
		}
	}
	current, _ := solo.Require(old.ID, "alice")
	if current.SandboxID != winner.SandboxID || current.LifecycleEpoch != winner.LifecycleEpoch {
		t.Fatal("stale removal changed the winner binding")
	}
	if _, err := solo.Close(context.Background(), old.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloAcceptedLegacyRestartSurvivesLostAckAndProcessRecovery(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 1)
	legacy := &models.SoloSession{ID: "legacy-restart-without-creation", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	accepted, err := solo.claimSoloRestart(context.Background(), legacy)
	if err != nil {
		t.Fatal(err)
	}
	// The accepted receipt commits, then the process/HTTP acknowledgement is
	// lost before provider allocation. Passive reads must not fill that gap.
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	for range 3 {
		if _, err := other.Get(context.Background(), legacy.ID, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&models.Sandbox{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("history read allocated a resident: count=%d err=%v", count, err)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 1 {
		t.Fatalf("legacy restart was not a persistent reservation: %d %v", count, err)
	}
	other.restoreUnboundSoloCreations()
	waitFor(t, "accepted restart restored by another service", func() bool {
		row, _ := other.Require(legacy.ID, "alice")
		return row.Status == models.SoloStatusRunning
	})
	row, _ := other.Require(legacy.ID, "alice")
	if row.LifecycleAttemptID != accepted.LifecycleAttemptID || row.LifecycleEpoch != accepted.LifecycleEpoch || row.Restarts != 1 {
		t.Fatal("recovery invented another restart receipt")
	}
	other.restoreUnboundSoloCreations()
	if err := db.Model(&models.Sandbox{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("restoring the receipt allocated twice: %d %v", count, err)
	}
	if _, err := other.Close(context.Background(), legacy.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloUnknownStopRetainsBindingQuotaAndHistoryThroughDelete(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 1)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resident before failed stop", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	before, _ := solo.Require(view.ID, "alice")
	if err := db.Create(&models.SoloMessage{ID: "keep-private-history", SessionID: before.ID, Role: "user", Content: "retained history"}).Error; err != nil {
		t.Fatal(err)
	}
	gateway.FailStop = true
	_, err = solo.Close(context.Background(), before.ID, "alice")
	requireSoloFailure(t, err, "environment_stop_pending")
	closed, _ := solo.Require(before.ID, "alice")
	if closed.Status != models.SoloStatusClosed || closed.SandboxID != before.SandboxID || closed.LifecycleAction != soloCloseCleanup {
		t.Fatalf("unknown Close lost its association: %+v", closed)
	}
	_, err = solo.Restart(context.Background(), before.ID, "alice")
	requireSoloFailure(t, err, "environment_stop_pending")
	requireSoloFailure(t, solo.Delete(context.Background(), before.ID, "alice"), "environment_retained")
	pending, _ := solo.Require(before.ID, "alice")
	if pending.SandboxID != before.SandboxID || pending.LifecycleAction != soloCloseCleanup || pending.LifecycleEpoch != closed.LifecycleEpoch || pending.Restarts != 0 {
		t.Fatal("retry/delete discarded an unconfirmed stop or invented replacement authority")
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 1 {
		t.Fatalf("unknown provider did not retain quota: %d %v", count, err)
	}
	var messages, environments int64
	db.Model(&models.SoloMessage{}).Where("session_id = ?", before.ID).Count(&messages)
	db.Model(&models.Sandbox{}).Count(&environments)
	if messages != 1 || environments != 1 {
		t.Fatalf("unknown stop lost history or allocated replacement: messages=%d environments=%d", messages, environments)
	}
	gateway.FailStop = false
	sbx.RecoverSoloSandboxes(context.Background())
	confirmed, _ := solo.Require(before.ID, "alice")
	if confirmed.Status != models.SoloStatusClosed || confirmed.SandboxID != before.SandboxID || confirmed.LifecycleAction != "" || confirmed.LifecycleEpoch != closed.LifecycleEpoch {
		t.Fatalf("stop recovery lost the closed receipt: %+v", confirmed)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 0 {
		t.Fatalf("confirmed stop did not free compute: %d %v", count, err)
	}
	requireSoloFailure(t, solo.Delete(context.Background(), before.ID, "alice"), "environment_retained")
	var environment models.Sandbox
	if err := db.First(&environment, before.SandboxID).Error; err != nil || !gateway.Has(environment.Name) {
		t.Fatalf("Delete erased a retained environment: %+v %v", environment, err)
	}
	if _, err := solo.Close(context.Background(), before.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func interceptResidentCreate(t *testing.T, sbx *SandboxService, upstream *httptest.Server) (<-chan struct{}, func()) {
	t.Helper()
	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var blocked, unblock sync.Once
	proxy := httputil.NewSingleHostReverseProxy(endpoint)
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method == http.MethodPost && strings.HasSuffix(response.Request.URL.Path, "/sandboxes") {
			blocked.Do(func() { close(started); <-release })
		}
		return nil
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	sbx.mgr = sandbox.NewManager(sandbox.NewGatewayClient(server.URL, ""), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	return started, func() { unblock.Do(func() { close(release) }) }
}

func TestSoloCloseAndRestartFenceLateProviderCreate(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 1)
	started, release := interceptResidentCreate(t, sbx, gateway.Server)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("real provider request did not start")
	}
	old, _ := solo.Require(view.ID, "alice")
	closeCtx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	_, err = solo.Close(closeCtx, old.ID, "alice")
	cancel()
	requireSoloFailure(t, err, "environment_stop_pending")
	closed, _ := solo.Require(old.ID, "alice")
	if closed.SandboxID != old.SandboxID || closed.Status != models.SoloStatusClosed || closed.LifecycleAction != soloCloseCleanup {
		t.Fatalf("Close raced past an unresolved create: %+v", closed)
	}
	_, err = solo.Restart(context.Background(), old.ID, "alice")
	requireSoloFailure(t, err, "environment_stop_pending")
	release()
	waitFor(t, "late old guest stopped and retained under the create lease", func() bool {
		row, _ := solo.Require(old.ID, "alice")
		var environment models.Sandbox
		return db.First(&environment, old.SandboxID).Error == nil && environment.Status == "stopped" && row.Status == models.SoloStatusClosed && row.LifecycleAction == ""
	})
	notRevived, _ := solo.Require(old.ID, "alice")
	if notRevived.SandboxID != old.SandboxID || notRevived.LifecycleEpoch != closed.LifecycleEpoch || notRevived.LifecycleAttemptID != closed.LifecycleAttemptID || notRevived.Restarts != 0 {
		t.Fatalf("late create overwrote its successor: %+v", notRevived)
	}
	var environment models.Sandbox
	if err := db.First(&environment, old.SandboxID).Error; err != nil || environment.AuthorizationFingerprint != "" || !gateway.Has(environment.Name) {
		t.Fatalf("unapplied authorization was forged or disk lost: %+v %v", environment, err)
	}
	// Creation never reached the final applied authorization proof. Reopen
	// cannot unpause old credentials merely because a provider ID now exists.
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	if _, err := other.Restart(context.Background(), old.ID, "alice"); err == nil {
		t.Fatal("unknown applied authorization was resumed")
	}
	provider, err := gateway.Client().Get(context.Background(), environment.Name)
	if err != nil || provider.Status != "stopped" {
		t.Fatalf("unsafe reopen started the provider: %+v %v", provider, err)
	}
	var count int64
	if err := db.Model(&models.Sandbox{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("unknown native create was replaced or forgotten: count=%d err=%v", count, err)
	}
	if _, err := other.Close(context.Background(), old.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloLateRecoveryAndGoneCallbacksCannotPublishAcrossEpoch(t *testing.T) {
	for _, kind := range []string{"recovered", "gone"} {
		t.Run(kind, func(t *testing.T) {
			solo, _, _, db := newSoloFixture(t, 0, 2)
			old := models.SoloSession{ID: "late-callback", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStarting, SandboxID: 101}
			if err := db.Create(&old).Error; err != nil {
				t.Fatal(err)
			}
			var intercepted atomic.Bool
			name := "test:replace-parent-after-callback-snapshot"
			if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table != "solo_sessions" || !intercepted.CompareAndSwap(false, true) {
					return
				}
				if err := db.Model(&models.SoloSession{}).Where("id = ?", old.ID).Updates(map[string]any{"lifecycle_epoch": 1, "lifecycle_attempt_id": "winning-attempt", "sandbox_id": 202, "status": models.SoloStatusStarting}).Error; err != nil {
					t.Error(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
			if kind == "recovered" {
				solo.onSandboxRecovered(old.SandboxID)
			} else {
				solo.onSandboxGone(old.SandboxID)
			}
			if err := db.Callback().Query().Remove(name); err != nil {
				t.Fatal(err)
			}
			row, _ := solo.Require(old.ID, "alice")
			if row.SandboxID != 202 || row.Status != models.SoloStatusStarting || row.LifecycleEpoch != 1 || row.Error != "" {
				t.Fatalf("late %s changed a new parent: %+v", kind, row)
			}
		})
	}
}

func TestSoloAuthorizationChangesDuringCreateCannotPublishRunning(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 1)
	started, release := interceptResidentCreate(t, sbx, gateway.Server)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider create did not start")
	}
	if err := db.Model(&models.Credential{}).Where("id = ?", "fixture-key").Update("secret_revision", 2).Error; err != nil {
		t.Fatal(err)
	}
	release()
	waitFor(t, "changed grant withheld running status", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusError })
	parent, _ := solo.Require(view.ID, "alice")
	var environment models.Sandbox
	if err := db.First(&environment, parent.SandboxID).Error; err != nil {
		t.Fatal(err)
	}
	if environment.AuthorizationFingerprint != "" || environment.DesiredState != "stopped" || environment.Name == "" || !gateway.Has(environment.Name) {
		t.Fatalf("old grant was published or attribution/files discarded: %+v", environment)
	}
	status, err := gateway.Client().Get(context.Background(), environment.Name)
	if err != nil || status.Status != "stopped" {
		t.Fatalf("obsolete environment compute not stopped: %+v %v", status, err)
	}
	if _, err := solo.Close(context.Background(), view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloAuthorizationRevokedDuringPreparationNeverCallsProvider(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	_, sbx, gateway, db := newSoloFixture(t, port, 1)
	row, err := sbx.OpenSoloSandbox(context.Background(), SoloSandboxOpts{Profile: "agentA", SessionID: "prepare-revocation", OnEnv: func(map[string]string) {
		if err := db.Model(&models.Credential{}).Where("id = ?", "fixture-key").Update("enabled", false).Error; err != nil {
			t.Error(err)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "preparation revocation reported", func() bool {
		var environment models.Sandbox
		return db.First(&environment, row.ID).Error == nil && environment.Status == "error"
	})
	guests, err := gateway.Client().List(context.Background())
	if err != nil || len(guests) != 0 {
		t.Fatalf("revoked prepared secret reached provider: guests=%d err=%v", len(guests), err)
	}
	var environment models.Sandbox
	if err := db.First(&environment, row.ID).Error; err != nil || environment.AuthorizationFingerprint != "" {
		t.Fatalf("unapplied authorization proof was published: %+v %v", environment, err)
	}
	requireSoloFailure(t, sbx.Destroy(context.Background(), row.ID), "environment_retained")
}

func soloLifecycleReplica(solo *SoloService, sbx *SandboxService) (*SoloService, *SandboxService) {
	// Independent operation locks/live maps model another application replica;
	// only SQL, provider and authorization sources are shared.
	replica := NewSandboxService(solo.db, sbx.mgr, sbx.skills, sbx.host, SandboxOptions{
		ProfilesRoot: sbx.profilesRoot, MCPEndpoint: sbx.mcpEndpoint, Env: sbx.env, MaxSolo: sbx.MaxSoloSandboxes(),
		ModelBinding: sbx.modelBinding, GitCredential: sbx.gitCredential, SelectedCredential: sbx.selectedCredential,
		CredentialReferences: sbx.credentialReferences, SharedAgent: sbx.shared,
	})
	return NewSoloService(solo.db, replica, solo.agents, solo.projects), replica
}

func TestSoloRetainedReopenTwoReplicasPreservesFilesAndSingleResource(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 1)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "retained resident initially ready", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	before, _ := solo.Require(view.ID, "alice")
	// This local marker represents the retained bookkeeping worktree. Provider
	// identity/retention is verified independently below against the fake gateway.
	home := t.TempDir()
	marker := filepath.Join(home, "uncommitted.bin")
	if err := os.WriteFile(marker, []byte{0, 1, 0xff, 4}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Sandbox{}).Where("id = ?", before.SandboxID).Update("home_dir", home).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Close(context.Background(), before.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	closed, _ := solo.Require(before.ID, "alice")
	if closed.SandboxID != before.SandboxID || closed.Status != models.SoloStatusClosed || closed.LifecycleAction != "" {
		t.Fatalf("Close did not retain its binding: %+v", closed)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 0 {
		t.Fatalf("confirmed Close did not release compute: %d %v", count, err)
	}
	requireSoloFailure(t, sbx.Destroy(context.Background(), before.SandboxID), "environment_retained")
	requireSoloFailure(t, solo.Delete(context.Background(), before.ID, "alice"), "environment_retained")
	var starts, stops atomic.Int32
	interceptStopGateway(t, sbx, gateway.Server, func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start") {
			starts.Add(1)
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			stops.Add(1)
		}
	})
	other, replicaSB := soloLifecycleReplica(solo, sbx)
	var group sync.WaitGroup
	ready := make(chan struct{})
	for i := range 12 {
		group.Go(func() {
			<-ready
			service := solo
			if i%2 != 0 {
				service = other
			}
			_, _ = service.Restart(context.Background(), before.ID, "alice")
		})
	}
	close(ready)
	group.Wait()
	current, _ := solo.Require(before.ID, "alice")
	if current.Status != models.SoloStatusRunning || current.SandboxID != before.SandboxID || current.Restarts != 0 || current.LifecycleEpoch != closed.LifecycleEpoch+1 || current.LifecycleAttemptID == closed.LifecycleAttemptID || starts.Load() != 1 {
		t.Fatalf("reopen allocated or started twice: row=%+v starts=%d", current, starts.Load())
	}
	guests, err := gateway.Client().List(context.Background())
	if err != nil || len(guests) != 1 {
		t.Fatalf("reopen did not use one retained provider resource: %d %v", len(guests), err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != string([]byte{0, 1, 0xff, 4}) {
		t.Fatalf("retained file changed: %v %v", data, err)
	}
	_, err = other.claimSoloRemoval(context.Background(), closed, soloCloseCleanup)
	requireSoloFailure(t, err, "session_changed")
	oldCloseReceipt := *closed
	oldCloseReceipt.LifecycleAction = soloCloseCleanup
	requireSoloFailure(t, other.stopClosedSolo(context.Background(), &oldCloseReceipt), "session_changed")
	requireSoloFailure(t, replicaSB.retrySoloStop(context.Background(), current.SandboxID), "session_changed")
	if stops.Load() != 0 {
		t.Fatal("a stale Close or background stop interrupted the reopened resource")
	}
	// Hooks without an epoch proof cannot turn a new starting incarnation into
	// running, or invalidate a persistent resource that still exists.
	if err := db.Model(&models.SoloSession{}).Where("id = ?", current.ID).Update("status", models.SoloStatusStarting).Error; err != nil {
		t.Fatal(err)
	}
	other.onSandboxRecovered(current.SandboxID)
	other.onSandboxGone(current.SandboxID)
	unchanged, _ := solo.Require(before.ID, "alice")
	if unchanged.Status != models.SoloStatusStarting || unchanged.SandboxID != current.SandboxID || unchanged.LifecycleEpoch != current.LifecycleEpoch {
		t.Fatalf("late unversioned hook mutated a retained epoch: %+v", unchanged)
	}
	if _, err := other.Close(context.Background(), before.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloRetainedReopenLostProviderAckRecoversSameReceipt(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, _ := newSoloFixture(t, port, 1)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resident ready before dropped Start ACK", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	if _, err := solo.Close(context.Background(), view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	closed, _ := solo.Require(view.ID, "alice")
	endpoint, err := url.Parse(gateway.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var starts atomic.Int32
	proxy := httputil.NewSingleHostReverseProxy(endpoint)
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method == http.MethodPost && strings.HasSuffix(response.Request.URL.Path, "/start") && starts.Add(1) == 1 {
			// The upstream already accepted and started its real fake-provider
			// resource. Discard that actual response before the client sees it.
			return errors.New("synthetic dropped provider acknowledgement")
		}
		return nil
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	sbx.mgr = sandbox.NewManager(sandbox.NewGatewayClient(server.URL, ""), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	if _, err := solo.Restart(context.Background(), closed.ID, "alice"); err == nil {
		t.Fatal("lost provider ACK was reported as confirmed running")
	}
	accepted, _ := solo.Require(closed.ID, "alice")
	if accepted.SandboxID != closed.SandboxID || accepted.LifecycleEpoch != closed.LifecycleEpoch+1 || accepted.Restarts != 0 || starts.Load() != 1 {
		t.Fatalf("accepted reopen lost its persistent receipt: %+v starts=%d", accepted, starts.Load())
	}
	other, replicaSB := soloLifecycleReplica(solo, sbx)
	for range 3 {
		if _, err := other.Get(context.Background(), closed.ID, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	if starts.Load() != 1 {
		t.Fatal("passive history reads repeated the provider request")
	}
	replicaSB.RecoverSoloSandboxes(context.Background())
	recovered, _ := other.Require(closed.ID, "alice")
	if recovered.Status != models.SoloStatusRunning || recovered.SandboxID != closed.SandboxID || recovered.LifecycleEpoch != accepted.LifecycleEpoch || recovered.LifecycleAttemptID != accepted.LifecycleAttemptID || starts.Load() != 1 {
		t.Fatalf("restart recovery changed receipt or repeated Start: %+v starts=%d", recovered, starts.Load())
	}
	if _, err := other.Close(context.Background(), closed.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloAppliedRouteAndContextWritesRejectPreviousEpoch(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 1)
	old := models.SoloSession{ID: "late-model-route", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, SandboxID: 201,
		CurrentModelSet: true, CurrentModelID: "old-request", AppliedModelSet: true}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SoloSession{}).Where("id = ?", old.ID).Updates(map[string]any{
		"lifecycle_epoch": 1, "lifecycle_attempt_id": "reopened-same-disk", "applied_configuration_id": "new-group", "applied_model_id": "new-model", "current_reasoning_effort": "low",
	}).Error; err != nil {
		t.Fatal(err)
	}
	requireSoloFailure(t, solo.rememberAppliedRoute(context.Background(), &old, "old-group", "old-model", "high"), "session_changed")
	requireSoloFailure(t, solo.markSoloModelContext(context.Background(), &old), "session_changed")
	if err := solo.persistResolvedEffort(&old, "low", "high"); err != nil {
		t.Fatal(err)
	}
	current, _ := solo.Require(old.ID, "alice")
	if current.AppliedConfigurationID != "new-group" || current.AppliedModelID != "new-model" || current.CurrentReasoningEffort != "low" || current.ModelContextPending || current.SandboxID != old.SandboxID {
		t.Fatalf("old native result overwrote a new same-disk lifecycle: %+v", current)
	}
}

func TestSoloMissingRowRetainsUnknownProviderAssociationAndQuota(t *testing.T) {
	solo, sbx, gateway, db := newSoloFixture(t, 0, 1)
	gateway.Seed("retained-unknown-provider")
	row := models.SoloSession{ID: "missing-bookkeeping", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, SandboxID: 404}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	solo.ReconcileSandboxReferences()
	kept, _ := solo.Require(row.ID, "alice")
	if kept.SandboxID != row.SandboxID || kept.Status != models.SoloStatusError {
		t.Fatalf("missing bookkeeping became authority to replace the sole disk: %+v", kept)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 1 {
		t.Fatalf("unknown provider association did not reserve compute: %d %v", count, err)
	}
	if _, err := solo.Restart(context.Background(), row.ID, "alice"); err == nil {
		t.Fatal("missing bookkeeping admitted a replacement")
	}
	requireSoloFailure(t, solo.Delete(context.Background(), row.ID, "alice"), "environment_retained")
	if _, err := solo.Create(context.Background(), "bob", SoloCreateInput{AgentName: "agentA"}); err == nil {
		t.Fatal("new compute ignored the retained unknown association")
	}
	guests, err := gateway.Client().List(context.Background())
	if err != nil || len(guests) != 1 || !gateway.Has("retained-unknown-provider") || sbx.activeSoloSandboxCount() != 1 {
		t.Fatalf("unknown provider resources changed: %+v %v", guests, err)
	}
}

func TestSoloLostCreateLeaseCannotStartProviderOrPublishParentFailure(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_provider", true: "failure_callback"}[revoke], func(t *testing.T) {
			_, sbx, gateway, db := newSoloFixture(t, 0, 1)
			var callbacks atomic.Int32
			prepared := make(chan struct{})
			row, err := sbx.OpenSoloSandbox(context.Background(), SoloSandboxOpts{Profile: "agentA", SessionID: "lost-create-lease",
				OnEnv: func(map[string]string) {
					if err := db.Model(&models.Sandbox{}).Where("thread_id = ?", "lost-create-lease").Updates(map[string]any{
						"operation_owner": "winning-physical-owner", "generation": gorm.Expr("generation + 1"), "status": "winner-state", "error": "keep winner",
					}).Error; err != nil {
						t.Error(err)
					}
					if revoke {
						if err := db.Model(&models.Credential{}).Where("id = ?", "fixture-key").Update("enabled", false).Error; err != nil {
							t.Error(err)
						}
					}
					close(prepared)
				}, OnFail: func(uint, error) { callbacks.Add(1) },
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-prepared:
			case <-time.After(5 * time.Second):
				t.Fatal("preparation was not reached")
			}
			// The in-process operation map is released only once the old create
			// goroutine has finished its failed proof/publication path.
			waitFor(t, "stale create finished", func() bool {
				return sbx.operations().LocalIdle(row.ID)
			})
			var environment models.Sandbox
			if err := db.First(&environment, row.ID).Error; err != nil || environment.OperationOwner != "winning-physical-owner" || environment.Status != "winner-state" || environment.Error != "keep winner" || callbacks.Load() != 0 {
				t.Fatalf("stale physical owner published a failure: %+v callbacks=%d err=%v", environment, callbacks.Load(), err)
			}
			guests, err := gateway.Client().List(context.Background())
			if err != nil || len(guests) != 0 {
				t.Fatalf("lost create lease reached provider: %d %v", len(guests), err)
			}
		})
	}
}

func TestSoloInitialReadyRollsBackParentWhenPhysicalPublicationFails(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 1)
	var injected, rolledBack atomic.Bool
	failure := errors.New("synthetic physical running publication rejected")
	name := "test:atomic_initial_resident_ready"
	if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if tx.Statement.Table != "sandboxes" || !ok {
			return
		}
		if proof, ok := values["authorization_fingerprint"].(string); ok && proof != "" && injected.CompareAndSwap(false, true) {
			tx.AddError(failure)
			return
		}
		if values["status"] == "error" && values["error"] == failure.Error() {
			var parent models.SoloSession
			if err := tx.Session(&gorm.Session{NewDB: true}).Select("status").First(&parent).Error; err != nil {
				t.Error(err)
				return
			}
			rolledBack.Store(parent.Status == models.SoloStatusStarting)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(name) })
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed native Ready remained unpublished", func() bool {
		row, _ := solo.Require(view.ID, "alice")
		return row.Status == models.SoloStatusError
	})
	row, _ := solo.Require(view.ID, "alice")
	var environment models.Sandbox
	if err := db.First(&environment, row.SandboxID).Error; err != nil || environment.Status != "error" || environment.AuthorizationFingerprint != "" || !injected.Load() || !rolledBack.Load() {
		t.Fatalf("parent ready escaped an aborted physical publication: %+v injected=%v rollback=%v err=%v", environment, injected.Load(), rolledBack.Load(), err)
	}
	if _, err := solo.Close(context.Background(), row.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestSoloCompletedCloseWithMissingEnvironmentRowHoldsNoQuota(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 1)
	legacy := &models.SoloSession{ID: "legacy-closed-missing-row", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed, SandboxID: 9001}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 0 {
		t.Fatalf("completed Close of a missing row held quota: %d %v", count, err)
	}
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatalf("legacy closed binding blocked a new session: %v", err)
	}
	waitFor(t, "new resident running", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	if _, err := solo.Close(context.Background(), view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	unknown := []*models.SoloSession{
		{ID: "closing-missing-row", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed, LifecycleAction: soloCloseCleanup, SandboxID: 9002},
		{ID: "error-missing-row", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusError, SandboxID: 9003},
	}
	for _, row := range unknown {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 2 {
		t.Fatalf("unconfirmed missing rows must keep quota: %d %v", count, err)
	}
}
