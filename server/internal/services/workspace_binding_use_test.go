package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func bindingAccessFixture(t *testing.T) (*WorkspaceBindingService, *bindingTestRemote, models.SoloSession) {
	t.Helper()
	bindings, remote, solo, _ := bindingResolverFixture(t)
	ds := &dockerState{}
	sbx := newSandboxService(t, bindings.db, ds)
	var row models.Sandbox
	if err := bindings.db.First(&row, solo.SandboxID).Error; err != nil {
		t.Fatal(err)
	}
	row.Profile = "agentA"
	sandboxTestAppliedBindings(t, sbx, &row)
	if err := bindings.db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := bindings.db.Model(&models.SoloSession{}).Where("id = ?", solo.ID).Update("agent_name", "agentA").Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	bindings.sandbox = sbx
	sbx.SetWorkspaceBindings(bindings)
	if _, err := bindings.Resolve(t.Context(), "alice", solo.ID); err != nil {
		t.Fatal(err)
	}
	return bindings, remote, solo
}
func TestWorkspaceBindingDurableConnectionUseBlocksSwitchUntilConfirmedCloseAndRetriesFailedClose(t *testing.T) {
	s, remote, solo := bindingAccessFixture(t)
	access, err := s.sandbox.BeginWorkspaceConsumer(t.Context(), solo.SandboxID, "ide")
	if err != nil {
		t.Fatal(err)
	}
	if access.Folder != remote.view.Path || access.Reference != remote.view.Head.Reference {
		t.Fatal(access)
	}
	var count int64
	if err := s.db.Model(&models.WorkspaceBindingUse{}).Where("state = 'active'").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.consumerIdleTx(t.Context(), tx, solo.SandboxID) }); err == nil {
		t.Fatal("active connection did not pin root")
	}
	failure := errors.New("local disconnect receipt write failed")
	if err := s.db.Callback().Update().Before("gorm:update").Register("binding-close-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "workspace_binding_uses" {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := access.Close(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := s.db.Callback().Update().Remove("binding-close-failure"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.consumerIdleTx(t.Context(), tx, solo.SandboxID) }); err == nil {
		t.Fatal("unknown close silently released pin")
	}
	if err := access.Close(); err != nil {
		t.Fatal(err)
	}
	if err := access.Close(); err != nil {
		t.Fatal("close not idempotent", err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.consumerIdleTx(t.Context(), tx, solo.SandboxID) }); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceBindingUnknownUseNeverExpiresByAgeAndOnlyProvenEnvironmentStopClosesIt(t *testing.T) {
	s, _, solo := bindingAccessFixture(t)
	access, err := s.sandbox.BeginWorkspaceConsumer(t.Context(), solo.SandboxID, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", solo.SandboxID).Updates(map[string]any{"state": "pending", "pending_operation_id": "owned-operation"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.sandbox.BeginWorkspaceConsumer(t.Context(), solo.SandboxID, "preview"); err == nil {
		t.Fatal("new consumer bypassed pending root")
	}
	if err := s.EnvironmentStoppedTx(t.Context(), s.db, solo.SandboxID); !errors.Is(err, ErrWorkspaceJobInvalid) {
		t.Fatal("unfenced provider-stop claim accepted", err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.EnvironmentStoppedTx(t.Context(), tx, solo.SandboxID) }); err != nil {
		t.Fatal(err)
	}
	if err := access.Close(); err != nil {
		t.Fatal("actual late close after provider stop lost receipt", err)
	}
	var active int64
	if err := s.db.Model(&models.WorkspaceBindingUse{}).Where("state <> 'closed'").Count(&active).Error; err != nil || active != 0 {
		t.Fatal(active, err)
	}
}
