package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type WorkspaceRetainedBindingView struct {
	ID         string `json:"id"`
	Revision   uint64 `json:"revision"`
	State      string `json:"state"`
	SnapshotID string `json:"snapshotId,omitempty"`
}
type WorkspaceReturnPointView struct {
	ID         string `json:"id"`
	BindingID  string `json:"bindingId"`
	SnapshotID string `json:"snapshotId"`
	State      string `json:"state"`
}
type WorkspaceBindingView struct {
	Available          bool                           `json:"available"`
	CanCapture         bool                           `json:"canCapture"`
	CanRestore         bool                           `json:"canRestore"`
	ID                 string                         `json:"id,omitempty"`
	Revision           uint64                         `json:"revision,omitempty"`
	Fingerprint        string                         `json:"fingerprint,omitempty"`
	PendingOperationID string                         `json:"pendingOperationId,omitempty"`
	ReturnSnapshotID   string                         `json:"returnSnapshotId,omitempty"`
	CanActivate        bool                           `json:"canActivate"`
	CanReturn          bool                           `json:"canReturn"`
	Reason             string                         `json:"reason,omitempty"`
	RetainedBindings   []WorkspaceRetainedBindingView `json:"retainedBindings"`
	ReturnPoints       []WorkspaceReturnPointView     `json:"returnPoints"`
}
type WorkspaceBindingOperationView struct {
	ID                     string    `json:"id"`
	ClientRequestID        string    `json:"clientRequestId"`
	Action                 string    `json:"action"`
	State                  string    `json:"state"`
	ExpectedBinding        string    `json:"expectedBinding"`
	BindingID              string    `json:"bindingId,omitempty"`
	RestoreID              string    `json:"restoreId,omitempty"`
	TargetBindingID        string    `json:"targetBindingId,omitempty"`
	ReturnSnapshotID       string    `json:"returnSnapshotId"`
	RequiresReconciliation bool      `json:"requiresReconciliation"`
	Error                  string    `json:"error,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
}

func (s *WorkspaceBindingService) CurrentView(ctx context.Context, actor, soloID string) (WorkspaceBindingView, error) {
	view := WorkspaceBindingView{RetainedBindings: []WorkspaceRetainedBindingView{}, ReturnPoints: []WorkspaceReturnPointView{}}
	if err := s.options.ReadScope(ctx, s.db, actor, "solo", soloID); err != nil {
		return view, err
	}
	var solo struct {
		SandboxID uint
		Status    string
	}
	if err := s.db.WithContext(ctx).Table("solo_sessions").Select("sandbox_id", "status").Take(&solo, "id = ? AND user_id = ?", soloID, actor).Error; err != nil {
		return view, err
	}
	var head models.WorkspaceBindingHead
	err := s.db.WithContext(ctx).Take(&head, "sandbox_id = ? AND scope_kind = 'solo' AND scope_id = ?", solo.SandboxID, soloID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return view, err
	}
	if solo.Status == models.SoloStatusRunning && solo.SandboxID != 0 && (errors.Is(err, gorm.ErrRecordNotFound) || head.State == "ready" && head.PendingOperationID == "") {
		if _, resolveErr := s.Resolve(ctx, actor, soloID); resolveErr != nil {
			view.Reason = "workspace_unavailable"
			if errors.Is(resolveErr, ErrWorkspaceJobUnknown) {
				view.Reason = "workspace_switch_pending"
			}
		} else {
			err = s.db.WithContext(ctx).Take(&head, "sandbox_id = ?", solo.SandboxID).Error
			if err != nil {
				return view, err
			}
		}
	}
	if head.SandboxID != 0 {
		view.Available = head.State != "released"
		view.ID, view.Revision, view.Fingerprint, view.PendingOperationID = head.BindingID, head.Revision, head.Fingerprint, head.PendingOperationID
		if head.PendingOperationID != "" || head.State == "pending" {
			view.Reason = "workspace_switch_pending"
		}
		if head.State == "released" {
			view.Reason = "environment_released"
		}
		var candidate struct{ ID string }
		err = s.db.WithContext(ctx).Table("workspace_snapshots").Select("id").Where("actor_id = ? AND scope_kind = 'solo' AND scope_id = ? AND source_sandbox_id = ? AND state = 'ready' AND verified_at IS NOT NULL AND source_binding_fingerprint = ?", actor, soloID, solo.SandboxID, head.Fingerprint).Order("created_at DESC, id DESC").Take(&candidate).Error
		if err == nil {
			view.ReturnSnapshotID = candidate.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return view, err
		}
	}
	var roots []models.WorkspaceBinding
	if err := s.db.WithContext(ctx).Where("actor_id = ? AND solo_id = ?", actor, soloID).Order("created_at, id").Find(&roots).Error; err != nil {
		return view, err
	}
	for _, root := range roots {
		var active sandbox.WorkspaceBindingHead
		sameRoot := workspacefiles.Decode([]byte(head.NativeHead), &active) == nil && active.Target.Kind == root.SelectorKind && active.Target.ID == root.SelectorID
		if root.State == "retained" && root.SandboxID == solo.SandboxID && root.NativeID != head.NativeID && !sameRoot {
			view.RetainedBindings = append(view.RetainedBindings, WorkspaceRetainedBindingView{root.ID, root.Revision, root.State, root.SnapshotID})
		}
	}
	var points []models.WorkspaceReturnPoint
	if err := s.db.WithContext(ctx).Where("actor_id = ? AND solo_id = ?", actor, soloID).Order("created_at DESC, id DESC").Find(&points).Error; err != nil {
		return view, err
	}
	for _, point := range points {
		view.ReturnPoints = append(view.ReturnPoints, WorkspaceReturnPointView{point.ID, point.PreviousBindingID, point.SnapshotID, point.State})
	}
	// Native capability alone is not product readiness. The operation module
	// installs only after every server consumer and the unique worker compose.
	view.CanCapture = s.options.FileOperationsEnabled && view.Available && view.Reason == "" && solo.Status == models.SoloStatusRunning
	view.CanRestore = view.CanCapture
	view.CanActivate = view.CanCapture && s.operationsReady() && view.ReturnSnapshotID != ""
	view.CanReturn = view.CanActivate && len(view.RetainedBindings) > 0
	if solo.Status != models.SoloStatusRunning && view.Reason == "" {
		view.Reason = "environment_stopped"
	}
	if !s.operationsReady() && view.Reason == "" {
		view.Reason = "workspace_activation_unavailable"
	}
	return view, nil
}

func bindingOperationView(row models.WorkspaceBindingOperation) WorkspaceBindingOperationView {
	return WorkspaceBindingOperationView{ID: row.ID, ClientRequestID: row.ClientNonce, Action: row.Action, State: row.State, ExpectedBinding: row.ExpectedBinding, BindingID: row.BindingID, RestoreID: row.RestoreID, TargetBindingID: row.TargetBindingID, ReturnSnapshotID: row.ReturnSnapshotID, RequiresReconciliation: row.State == "unknown", Error: row.Error, CreatedAt: row.CreatedAt}
}

func (s *WorkspaceBindingService) GetOperation(ctx context.Context, actor, soloID, id string) (models.WorkspaceBindingOperation, error) {
	var row models.WorkspaceBindingOperation
	if err := s.options.ReadScope(ctx, s.db, actor, "solo", soloID); err != nil {
		return row, err
	}
	if err := s.db.WithContext(ctx).Take(&row, "id = ? AND actor_id = ? AND solo_id = ?", id, actor, soloID).Error; err != nil {
		return row, err
	}
	if row.State == "activated" || row.State == "returned" {
		return row, nil
	}
	var job struct{ State, Error string }
	err := s.db.WithContext(ctx).Table("workspace_snapshot_jobs").Select("state", "error").Take(&job, "id = ? AND actor_id = ?", row.JobID, actor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row.State, row.Error = "unknown", "workspace_job_missing"
		return row, nil
	}
	if err != nil {
		return row, err
	}
	row.State, row.Error = job.State, job.Error
	if row.State == "executed" {
		row.State, row.Error = "unknown", "workspace_binding_unconfirmed"
	}
	return row, nil
}

func (s *WorkspaceBindingService) OperationView(ctx context.Context, actor, soloID, id string) (WorkspaceBindingOperationView, error) {
	row, err := s.GetOperation(ctx, actor, soloID, id)
	view := bindingOperationView(row)
	if err == nil && (row.State == "failed" || row.State == "cancelled") {
		var count int64
		if check := s.db.WithContext(ctx).Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ? AND pending_operation_id = ?", row.SandboxID, row.ID).Count(&count).Error; check != nil {
			return view, check
		}
		view.RequiresReconciliation = count > 0
	}
	return view, err
}
func (s *WorkspaceBindingService) OperationByRequest(ctx context.Context, actor, soloID, nonce string) (WorkspaceBindingOperationView, error) {
	if !workspaceJobID.MatchString(nonce) {
		return WorkspaceBindingOperationView{}, gorm.ErrRecordNotFound
	}
	return s.OperationView(ctx, actor, soloID, workspaceRestoreIdentity(actor, soloID, nonce))
}
func (s *WorkspaceBindingService) ListOperations(ctx context.Context, actor, soloID string) ([]WorkspaceBindingOperationView, error) {
	if err := s.options.ReadScope(ctx, s.db, actor, "solo", soloID); err != nil {
		return nil, err
	}
	var rows []models.WorkspaceBindingOperation
	if err := s.db.WithContext(ctx).Where("actor_id = ? AND solo_id = ?", actor, soloID).Order("created_at DESC,id DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]WorkspaceBindingOperationView, 0, len(rows))
	for _, row := range rows {
		view, err := s.OperationView(ctx, actor, soloID, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}
