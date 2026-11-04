package services

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestCodexCredentialEditDuringRenewal(t *testing.T) {
	for _, edit := range []string{"alias", "relogin", "clear"} {
		t.Run(edit, func(t *testing.T) {
			fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
			loaded, release := make(chan struct{}), make(chan struct{})
			var blocked atomic.Bool
			if err := fx.creds.db.Callback().Update().Before("gorm:begin_transaction").Register("test:pause_credential_edit", func(tx *gorm.DB) {
				if row, ok := tx.Statement.Dest.(*models.Credential); ok && row.ID == fx.id && !blocked.Swap(true) {
					close(loaded)
					<-release
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = fx.creds.db.Callback().Update().Remove("test:pause_credential_edit") }()
			input := CredentialInput{Name: "Renamed Codex"}
			if edit == "relogin" {
				input.Value = testCodexLogin(t, testAccess(42, time.Now().Add(240*time.Hour)), "rt-user")
			} else if edit == "clear" {
				input.Clear = true
			}
			done := make(chan error, 1)
			go func() { _, err := fx.creds.Update(fx.id, input); done <- err }()
			<-loaded
			_, _, refreshErr := fx.m.ForceRefresh(context.Background(), fx.id, "")
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if refreshErr != nil {
				t.Fatal(refreshErr)
			}
			fx.m.bg.Wait()
			view, err := fx.creds.Get(fx.id)
			if err != nil || view.Name != input.Name {
				t.Fatalf("metadata edit lost: %+v %v", view, err)
			}
			if edit == "clear" {
				if view.Configured || view.SecretRevision != 3 {
					t.Fatalf("clear lost: %+v", view)
				}
				return
			}
			wantToken, wantRevision := "rt-1", uint64(2)
			if edit == "relogin" {
				wantToken, wantRevision = "rt-user", 3
			}
			if token := fx.stored(t).RefreshToken(); token != wantToken || view.SecretRevision != wantRevision {
				t.Fatalf("edit restored an old token/revision: token=%s revision=%d", token, view.SecretRevision)
			}
		})
	}
}

func TestCodexLoginManagerStopDrainsRenewal(t *testing.T) {
	for _, caller := range []string{"scheduler", "cancelled caller"} {
		t.Run(caller, func(t *testing.T) {
			fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(time.Hour)))
			entered, release := make(chan struct{}), make(chan struct{})
			fx.api.set(func(f *fakeOpenAI) { f.during = func() { close(entered); <-release } })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if caller == "scheduler" {
				fx.m.Start(ctx)
			} else {
				done := make(chan error, 1)
				go func() { _, _, err := fx.m.ForceRefresh(ctx, fx.id, ""); done <- err }()
				<-entered
				cancel()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			if caller == "scheduler" {
				<-entered
			}
			stopped := make(chan struct{})
			go func() { fx.m.Stop(); close(stopped) }()
			early := false
			select {
			case <-stopped:
				early = true
			case <-time.After(30 * time.Millisecond):
			}
			close(release)
			<-stopped
			if early || fx.stored(t).RefreshToken() != "rt-1" {
				t.Fatal("shutdown did not wait for the rotated token to be saved")
			}
			if _, _, err := fx.m.ForceRefresh(context.Background(), fx.id, ""); !errors.Is(err, context.Canceled) {
				t.Fatalf("a stopped manager admitted a renewal: %v", err)
			}
			fx.m.Tick(context.Background())
			fx.m.Start(context.Background())
			if fx.api.calls.Load() != 1 {
				t.Fatal("shutdown admitted another refresh request")
			}
		})
	}
}

func TestCodexLiveSandboxSurvivesRenewalAndRelogin(t *testing.T) {
	for _, expiry := range []time.Duration{time.Hour, 240 * time.Hour} {
		t.Run(expiry.String(), func(t *testing.T) {
			fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(expiry)))
			configurations := NewModelConfigurationService(fx.creds.db)
			configuration, err := configurations.Create(modelrouting.Configuration{
				Name: "Codex", Backend: "codex", Binding: modelrouting.Binding{Kind: "platform", CredentialID: fx.id},
			})
			if err != nil {
				t.Fatal(err)
			}
			acp, port := fakeACPServer(t)
			defer acp.Close()
			s := newSandboxService(t, fx.creds.db, &dockerState{acpPort: port})
			s.SetCodexLogins(fx.hub)
			connections := NewModelConnectionService(fx.creds.db)
			s.modelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
				return connections.Resolve(backend, *binding)
			}
			agent := Agent{Name: "agentA", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}
			if err := s.skills.Save(agent); err != nil {
				t.Fatal(err)
			}
			agent, _ = s.skills.Get(agent.Name)
			row := models.Sandbox{Name: "codex-renewal", Profile: agent.Name, Purpose: "test", Status: "creating", Token: "test-password"}
			if err := s.db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			s.startContainer(row.ID, row.Name, row.Profile, "bound-run", row.Token, nil, agent, "")
			if err := s.db.First(&row, row.ID).Error; err != nil || row.Status != "running" {
				t.Fatalf("sandbox did not start: %v %s", err, row.Error)
			}
			defer func() { _ = s.Destroy(context.Background(), row.ID) }()
			out, version, err := fx.m.EnsureFresh(context.Background(), fx.id)
			if err != nil {
				t.Fatal(err)
			}
			file := &memCodexFile{body: out}
			fx.hub.Register(fx.id, liveCodexKey(row.ID), file, "/root/.codex/auth.json", version)
			chat := func() {
				t.Helper()
				if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "continue", nil, 0, nil); err != nil {
					t.Fatalf("a platform-managed login could not continue: %v", err)
				}
			}
			chat() // Includes renewal during sandbox preparation when expiry=1h.
			if _, _, err := fx.m.ForceRefresh(context.Background(), fx.id, ""); err != nil {
				t.Fatal(err)
			}
			chat()
			user := testAccess(42, time.Now().Add(240*time.Hour))
			if _, err := fx.creds.Update(fx.id, CredentialInput{Value: testCodexLogin(t, user, "rt-user")}); err != nil {
				t.Fatal(err)
			}
			fx.m.bg.Wait()
			chat()
			body, _ := file.get()
			assertSandboxCopy(t, body, user)
			// Route changes and revoked authorization still fail before chat.
			configuration.Binding.ModelID = "another-model"
			if _, err := configurations.Update(configuration.ID, configuration); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "changed model", nil, 0, nil); err == nil {
				t.Fatal("changing the model bypassed the binding check")
			}
			if err := fx.creds.Revoke(fx.id); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "revoked", nil, 0, nil); err == nil {
				t.Fatal("a revoked credential remained usable")
			}
		})
	}
}

func TestCodexLoginManagerStopStoresPendingRenewal(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	fx.setUnsaved(t, testAccess(7, time.Now().Add(240*time.Hour)), "rt-7")
	fx.m.Stop()
	if token := fx.stored(t).RefreshToken(); token != "rt-7" || fx.api.calls.Load() != 0 {
		t.Fatal("shutdown did not save the pending token without rotating again")
	}
}
