package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestSoloInferenceConsoleAccessPreservesTurnOwnershipAcrossReplicas(t *testing.T) {
	for _, peer := range []bool{false, true} {
		t.Run(map[bool]string{false: "same replica", true: "other replica"}[peer], func(t *testing.T) {
			bindings, remote, solo := bindingAccessFixture(t)
			owner := bindings.sandbox
			if err := owner.db.Model(&models.Sandbox{}).Where("id = ?", solo.SandboxID).Update("code_server_port", 8080).Error; err != nil {
				t.Fatal(err)
			}
			held, release, err := owner.acquireSandboxOperation(t.Context(), solo.SandboxID, sandboxOperationInference)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			lease, _ := owner.heldSandboxOperation(held, solo.SandboxID)
			reader := owner
			if peer {
				reader = NewSandboxService(owner.db, owner.mgr, owner.skills, owner.host, SandboxOptions{
					ModelBinding: owner.modelBinding, ProfilesRoot: owner.profilesRoot, MCPEndpoint: owner.mcpEndpoint,
				})
				reader.SetWorkspaceBindings(bindings)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if addr, err := reader.IDEUpstream(ctx, solo.SandboxID); err != nil || addr == "" {
				t.Fatalf("IDE blocked by inference: %q %v", addr, err)
			}
			for _, kind := range []string{"ide", "terminal", "preview"} {
				access, err := reader.BeginWorkspaceConsumer(ctx, solo.SandboxID, kind)
				if err != nil {
					t.Fatalf("%s blocked by inference: %v", kind, err)
				}
				if access.Folder != remote.view.Path || access.Reference != remote.view.Head.Reference {
					t.Fatal("consumer lost canonical root", access)
				}
				if err := bindings.db.Transaction(func(tx *gorm.DB) error { return bindings.consumerIdleTx(ctx, tx, solo.SandboxID) }); err == nil {
					t.Fatal("connection did not block workspace switching")
				}
				if err := access.Close(); err != nil {
					t.Fatal(err)
				}
			}
			row, err := owner.Get(solo.SandboxID)
			if err != nil || row.OperationOwner != lease.Owner || row.Generation != lease.Generation || row.OperationKind != sandboxOperationInference {
				t.Fatalf("console changed inference ownership: %+v %v", row, err)
			}
			if err := owner.commitSandboxOperation(held, solo.SandboxID, func(*gorm.DB) error { return nil }); err != nil {
				t.Fatal("console invalidated reply publication", err)
			}
		})
	}
}

func TestSoloConsoleWaitsForPreparationThenAdmitsDuringSameTurn(t *testing.T) {
	bindings, _, solo := bindingAccessFixture(t)
	owner := bindings.sandbox
	held, release, err := owner.acquireSandboxOperation(t.Context(), solo.SandboxID, "turn")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		access, err := owner.BeginWorkspaceConsumer(ctx, solo.SandboxID, "terminal")
		if err == nil {
			err = access.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("consumer entered during preparation", err)
	case <-time.After(60 * time.Millisecond):
	}
	if err := owner.commitSandboxOperation(held, solo.SandboxID, func(tx *gorm.DB) error {
		return owner.updateSandboxOperation(held, tx, solo.SandboxID, map[string]any{"operation_kind": sandboxOperationInference})
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("console still waited for the entire turn")
	}
}

func TestSoloConsoleRejectsChangedInferenceOrWorkspaceDuringAdmission(t *testing.T) {
	for _, changed := range []string{"owner", "generation", "expiry", "preparing", "stop", "closed", "workspace"} {
		t.Run(changed, func(t *testing.T) {
			bindings, remote, solo := bindingAccessFixture(t)
			service := bindings.sandbox
			_, release, err := service.acquireSandboxOperation(t.Context(), solo.SandboxID, sandboxOperationInference)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			remote.before = func() {
				remote.before = nil
				updates := map[string]any{}
				switch changed {
				case "owner":
					updates["operation_owner"] = "successor"
				case "generation":
					updates["generation"] = gorm.Expr("generation + 1")
				case "expiry":
					updates["operation_expires_at"] = time.Now().Add(-time.Second)
				case "preparing":
					updates["operation_kind"] = "turn"
				case "stop":
					updates["desired_state"] = "stopped"
				case "closed":
					if err := service.db.Model(&models.SoloSession{}).Where("id = ?", solo.ID).Update("status", models.SoloStatusClosed).Error; err != nil {
						t.Fatal(err)
					}
				case "workspace":
					if err := service.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", solo.SandboxID).Update("pending_operation_id", "switching").Error; err != nil {
						t.Fatal(err)
					}
				}
				if len(updates) > 0 {
					if err := service.db.Model(&models.Sandbox{}).Where("id = ?", solo.SandboxID).Updates(updates).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if _, err := service.BeginWorkspaceConsumer(ctx, solo.SandboxID, "terminal"); err == nil {
				t.Fatal("changed admission was accepted")
			}
			var count int64
			if err := service.db.Model(&models.WorkspaceBindingUse{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("rejected connection left a pin", count, err)
			}
		})
	}
}

func TestSoloConsoleObservationCannotMutateOrPublishAsTurnOwner(t *testing.T) {
	bindings, _, solo := bindingAccessFixture(t)
	service := bindings.sandbox
	_, release, err := service.acquireSandboxOperation(t.Context(), solo.SandboxID, sandboxOperationInference)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, _, done, err := service.beginSandboxConsumerAccess(t.Context(), solo.SandboxID)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, ok := service.heldSandboxOperation(ctx, solo.SandboxID); ok {
		t.Fatal("observation impersonates task owner")
	}
	called := false
	if err := service.commitSandboxOperation(ctx, solo.SandboxID, func(*gorm.DB) error { called = true; return nil }); !errors.Is(err, errSandboxOperationLost) || called {
		t.Fatal("observation can publish a reply", err)
	}
	if err := service.updateSandboxOperation(ctx, service.db, solo.SandboxID, map[string]any{"operation_owner": "wrong"}); !errors.Is(err, errSandboxOperationLost) {
		t.Fatal("observation can mutate task lease", err)
	}
}
