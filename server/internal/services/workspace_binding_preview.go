package services

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func (s *PreviewService) bindingHead(db *gorm.DB, name string) (models.WorkspaceBindingHead, bool, error) {
	if db == nil {
		return models.WorkspaceBindingHead{}, false, ErrWorkspaceJobUnsupported
	}
	var resource struct {
		ID      uint
		Purpose string
	}
	err := db.Table("sandboxes").Select("id", "purpose").Take(&resource, "name = ?", name).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.WorkspaceBindingHead{}, false, nil
	}
	if err != nil {
		return models.WorkspaceBindingHead{}, false, err
	}
	if resource.Purpose != SandboxPurposeSolo {
		return models.WorkspaceBindingHead{}, false, nil
	}
	var head models.WorkspaceBindingHead
	err = workspaceCurrentRead(db).Take(&head, "sandbox_id = ?", resource.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return head, false, nil
	}
	return head, err == nil, err
}

func (s *PreviewService) annotatePreviewBinding(rec *mcp.PreviewPort) error {
	if rec == nil || rec.SandboxName == "" || rec.Kind == mcp.PreviewKindURL {
		return nil
	}
	// Transport-only callers can probe an untouched legacy registration without
	// a metadata store. An existing binding or configured binding service still
	// requires its authoritative head; missing metadata must never authorize it.
	if s.db == nil && rec.WorkspaceBindingID == "" && rec.WorkspaceBindingFingerprint == "" && (s.leases == nil || s.leases.workspaceBindings.Load() == nil) {
		return nil
	}
	head, known, err := s.bindingHead(s.db, rec.SandboxName)
	if err != nil {
		return err
	}
	if !known {
		return nil
	}
	rec.Stale = head.State != "ready" || head.PendingOperationID != "" || (rec.WorkspaceBindingID != "" && (rec.WorkspaceBindingID != head.BindingID || rec.WorkspaceBindingFingerprint != head.NativeFingerprint)) || (rec.WorkspaceBindingID == "" && head.Revision > 1)
	if rec.Stale {
		rec.Healthy = false
		rec.DirectURL = ""
		rec.Mode = ""
	}
	return nil
}
func (s *PreviewService) checkPreviewBindingTx(tx *gorm.DB, rec mcp.PreviewPort) error {
	if rec.SandboxName == "" || rec.Kind == mcp.PreviewKindURL {
		return nil
	}
	head, known, err := s.bindingHead(tx, rec.SandboxName)
	if err != nil {
		return err
	}
	if !known {
		return nil
	}
	if head.State != "ready" || head.PendingOperationID != "" || rec.WorkspaceBindingID == "" || rec.WorkspaceBindingID != head.BindingID || rec.WorkspaceBindingFingerprint != head.NativeFingerprint {
		return soloErr(409, "workspace_preview_stale", "预览属于先前工作区，请在当前目录启动服务并重新设置预览")
	}
	return nil
}

type workspacePortRemote interface {
	WorkspaceBindingPort(context.Context, int) (sandbox.WorkspaceBindingPort, error)
}

func verifyWorkspacePort(ctx context.Context, remote WorkspaceBindingRemote, port int, expected sandbox.WorkspaceBindingReference) error {
	inspector, ok := remote.(workspacePortRemote)
	if !ok {
		return ErrWorkspaceJobUnsupported
	}
	view, err := inspector.WorkspaceBindingPort(ctx, port)
	if err != nil {
		return err
	}
	if view.Version != 1 || !view.Verified || view.Port != port || view.Reference != expected {
		return ErrWorkspaceJobConflict
	}
	return nil
}

// BindPreviewPort runs for a new explicit set_preview, before the registration
// is persisted or displayed. A socket still serving an old directory cannot
// acquire a new binding label merely by retaining its port number.
func (s *PreviewService) BindPreviewPort(ctx context.Context, rec mcp.PreviewPort) (mcp.PreviewPort, error) {
	if s.leases == nil || rec.SandboxName == "" || rec.Kind == mcp.PreviewKindURL {
		return rec, nil
	}
	bindings := s.leases.workspaceBindings.Load()
	if bindings == nil {
		return rec, nil
	}
	var resource models.Sandbox
	if err := s.db.WithContext(ctx).Select("id", "name", "purpose", "thread_id", "run_id").Take(&resource, "name = ?", rec.SandboxName).Error; err != nil {
		return rec, err
	}
	if resource.Purpose != SandboxPurposeSolo {
		return rec, nil
	}
	if resource.RunID != rec.RunID || rec.NodeID != "solo" {
		return rec, gorm.ErrRecordNotFound
	}
	var parent struct{ UserID string }
	if err := s.db.WithContext(ctx).Table("solo_sessions").Select("user_id").Take(&parent, "id = ? AND sandbox_id = ?", resource.ThreadID, resource.ID).Error; err != nil {
		return rec, err
	}
	value, err := bindings.Resolve(ctx, parent.UserID, resource.ThreadID)
	if err != nil {
		return rec, err
	}
	remote, err := bindings.options.Remote(ctx, resource.ID)
	if err != nil {
		return rec, err
	}
	if err := verifyWorkspacePort(ctx, remote, rec.Port, value.Reference); err != nil {
		return rec, err
	}
	rec.WorkspaceBindingID = bindingHistoryID(resource.ID, value.Reference.ID)
	rec.WorkspaceBindingFingerprint = value.Reference.Fingerprint
	return rec, nil
}

// BeginPreviewConsumer validates the stored binding on every new HTTP/WS
// connection and pins its actual lifetime. It never restarts a dev server.
func (s *PreviewService) BeginPreviewConsumer(ctx context.Context, rec mcp.PreviewPort) (func() error, error) {
	noop := func() error { return nil }
	if err := s.annotatePreviewBinding(&rec); err != nil {
		return noop, err
	}
	if rec.Stale {
		return noop, soloErr(409, "workspace_preview_stale", "预览属于先前工作区，请重新设置预览")
	}
	if s.leases == nil || s.leases.workspaceBindings.Load() == nil || rec.SandboxName == "" {
		return noop, nil
	}
	var row models.Sandbox
	if err := s.db.WithContext(ctx).Select("id", "purpose").Take(&row, "name = ?", rec.SandboxName).Error; err != nil {
		return noop, err
	}
	if row.Purpose != SandboxPurposeSolo {
		return noop, nil
	}
	access, err := s.leases.BeginWorkspaceConsumer(ctx, row.ID, "preview")
	if err != nil {
		return noop, err
	}
	fail := func(err error) (func() error, error) { _ = access.Close(); return noop, err }
	if rec.WorkspaceBindingID == "" {
		if access.Reference.Revision > 1 {
			return fail(ErrWorkspaceJobConflict)
		}
		return access.Close, nil // untouched original legacy preview
	}
	if access.Reference.Fingerprint != rec.WorkspaceBindingFingerprint || bindingHistoryID(row.ID, access.Reference.ID) != rec.WorkspaceBindingID {
		return fail(ErrWorkspaceJobConflict)
	}
	remote, err := s.leases.workspaceBindings.Load().options.Remote(ctx, row.ID)
	if err != nil {
		return fail(err)
	}
	if err := verifyWorkspacePort(ctx, remote, rec.Port, access.Reference); err != nil {
		return fail(err)
	}
	return access.Close, nil
}
