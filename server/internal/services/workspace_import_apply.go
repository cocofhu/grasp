package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacearchive"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *WorkspaceImportService) target(tx *gorm.DB, row models.WorkspaceImport) error {
	session, _, err := s.authorize(tx, row.SoloID, row.ActorID, row.RunID)
	if err != nil {
		return err
	}
	if session.SandboxID != row.TargetSandboxID || session.Status == models.SoloStatusClosed || session.Status == models.SoloStatusStarting {
		return workspaceImportError(http.StatusConflict, "workspace_target_changed", "Solo 环境已变化，请保留原文件并重新创建预览")
	}
	var environment models.Sandbox
	if err := tx.Select("id", "status", "desired_state", "purpose", "thread_id").First(&environment, row.TargetSandboxID).Error; err != nil {
		return err
	}
	if environment.Purpose != SandboxPurposeSolo || environment.ThreadID != row.SoloID {
		return workspaceImportError(http.StatusConflict, "workspace_target_changed", "目标不再属于当前 Solo")
	}
	if environment.Status != "running" || environment.DesiredState == "stopped" {
		return workspaceImportError(http.StatusConflict, "workspace_target_unavailable", "请先恢复 Solo 环境；导入不会自动启动环境")
	}
	return nil
}

func (s *WorkspaceImportService) guardIdle(tx *gorm.DB, id string) error {
	if s.solo == nil || s.solo.durable == nil {
		return workspaceImportError(http.StatusServiceUnavailable, "workspace_import_unavailable", "会话队列尚未就绪")
	}
	if err := s.solo.durable.GuardIdle(tx, soloQueueScope(id)); err != nil {
		if errors.Is(err, chatsession.ErrQueueBusy) {
			return workspaceImportError(http.StatusConflict, "session_busy", "请先停止当前轮次并处理待发送消息，再确认导入")
		}
		if errors.Is(err, chatsession.ErrQueueRetired) {
			return workspaceImportError(http.StatusConflict, "session_closed", "Solo 会话已关闭")
		}
		return err
	}
	return nil
}

// Confirm can dispatch only prepared -> applying. A crash or uncertain transport
// never turns applying back into prepared; reconciliation reads the runtime's
// completion receipt and never replays a filesystem operation.
func (s *WorkspaceImportService) Confirm(ctx context.Context, soloID, user, id string, in WorkspaceImportConfirmInput) (*WorkspaceImportView, error) {
	if !in.Confirmed || !workspaceRequestID.MatchString(in.ClientRequestID) {
		return nil, workspaceImportError(http.StatusBadRequest, "workspace_import_confirmation_required", "请先查看预览并明确确认导入")
	}
	row, err := s.load(ctx, soloID, user, id)
	if err != nil {
		return nil, err
	}
	if row.ConfirmNonce != "" && row.ConfirmNonce != in.ClientRequestID {
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_confirmation_conflict", "此预览已有其他确认请求，请核对导入结果")
	}
	if row.State == "applied" {
		return importView(row)
	}
	if row.State != "prepared" {
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_reconcile_required", "上次导入结果尚未确认，请先核对结果；不会自动重复导入")
	}
	if err := s.target(s.db.WithContext(ctx), row); err != nil {
		return nil, err
	}
	ctx, release, err := s.sandbox.acquireSandboxOperation(ctx, row.TargetSandboxID, "code_import")
	if err != nil {
		return nil, err
	}
	defer release()
	row, err = s.load(ctx, soloID, user, id)
	if err != nil {
		return nil, err
	}
	if row.ConfirmNonce != "" && row.ConfirmNonce != in.ClientRequestID {
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_confirmation_conflict", "此预览已有其他确认请求，请核对导入结果")
	}
	if row.State == "applied" {
		return importView(row)
	}
	if row.State != "prepared" {
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_reconcile_required", "上次导入结果尚未确认，请先核对结果")
	}
	if err := s.target(s.db.WithContext(ctx), row); err != nil {
		return nil, err
	}
	capability, err := s.runtime.Inventory(ctx, row.TargetSandboxID)
	if err != nil {
		return nil, workspaceRuntimeError(err)
	}
	if capability.Version != sandbox.WorkspaceCodeVersion || !capability.RenewableLease {
		return nil, workspaceImportError(http.StatusUnprocessableEntity, "workspace_import_unsupported", "Solo 环境尚不支持提交导入，请更新运行时后重试")
	}
	// Open and validate object bytes outside SQL, before accepting dispatch. A
	// missing archive cannot produce an accepted import that can never execute.
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	archive, err := files.Open(ctx, row.ArchiveRef)
	if err != nil {
		return nil, workspaceImportError(http.StatusServiceUnavailable, "workspace_archive_unavailable", "导入归档暂不可用；未执行任何导入，可稍后重试")
	}
	defer func() { _ = archive.Close() }()
	manifest, digest, err := inspectCodeArchive(ctx, archive)
	if err != nil || digest != row.ArchiveSHA256 || stringJSON(manifest.Repositories) != row.Repositories {
		return nil, workspaceRuntimeError(workspacearchive.ErrUnsafe)
	}
	// Preparing this small result before the TX keeps object writes out of the
	// queue/sandbox lock order, including on remote S3-compatible storage.
	message, err := files.Prepare(ctx, strings.NewReader(importResultText(row)), filestore.Text)
	if err != nil {
		return nil, err
	}
	err = s.sandbox.commitSandboxOperationPrepared(ctx, row.TargetSandboxID, func(tx *gorm.DB) error { return s.guardIdle(tx, soloID) }, func(tx *gorm.DB) error {
		if err := s.target(tx, row); err != nil {
			return err
		}
		result := tx.Model(&models.WorkspaceImport{}).Where("id = ? AND state = ? AND (confirm_nonce = '' OR confirm_nonce = ?)", id, "prepared", in.ClientRequestID).Updates(map[string]any{"state": "applying", "confirm_nonce": in.ClientRequestID, "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return workspaceImportError(http.StatusConflict, "workspace_import_reconcile_required", "此预览已被确认，请核对结果；不会自动重复导入")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	row.State, row.ConfirmNonce = "applying", in.ClientRequestID
	// The object download may have taken time. Check the physical owner again
	// immediately before sending the provider request, not just on final publish.
	if err := ctx.Err(); err != nil {
		s.uncertain(ctx, row)
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_unknown", "导入结果尚未确认，请核对结果")
	}
	if err := s.sandbox.commitSandboxOperation(ctx, row.TargetSandboxID, func(tx *gorm.DB) error { return s.target(tx, row) }); err != nil {
		s.uncertain(ctx, row)
		return nil, err
	}
	receipt, err := s.runtime.Import(ctx, row.TargetSandboxID, row.ID, row.ArchiveSHA256, archive)
	if err != nil {
		s.uncertain(ctx, row)
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_unknown", "导入未取得完整收据；原工作区和已写入文件均保留，请核对结果后再决定")
	}
	if err := s.complete(ctx, &row, receipt, message); err != nil {
		s.uncertain(ctx, row)
		return nil, err
	}
	s.notifyImport(row.SoloID, row.ID)
	return importView(row)
}

func importResultText(row models.WorkspaceImport) string {
	view, err := importView(row)
	if err != nil {
		return "工作流代码已导入独立目录，请核对导入结果。"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "工作流 %s · %s 第 %d 轮的 Git 提交已导入 Solo 独立目录：%s/%s。原工作区保留。", row.RunID, row.NodeID, row.Iteration, sandbox.WorkspaceCodeDirectory, row.ID)
	for i, repo := range view.Repositories {
		fmt.Fprintf(&text, "\n仓库 %s · 提交 %s · 路径 %s/%s/%s", repo.Path, repo.Commit, sandbox.WorkspaceCodeDirectory, row.ID, importRepoName(repo.Path, i))
	}
	text.WriteString("\n此次不包含未提交改动、未跟踪或忽略文件、非 Git 文件及原生会话。可在上述路径检查并继续修改。")
	return text.String()
}

func (s *WorkspaceImportService) uncertain(ctx context.Context, row models.WorkspaceImport) {
	_ = s.sandbox.commitSandboxOperation(ctx, row.TargetSandboxID, func(tx *gorm.DB) error {
		return tx.Model(&models.WorkspaceImport{}).Where("id = ? AND state = ? AND confirm_nonce = ?", row.ID, "applying", row.ConfirmNonce).Updates(map[string]any{"state": "unknown", "error": "workspace_import_unknown"}).Error
	})
}

func (s *WorkspaceImportService) complete(ctx context.Context, row *models.WorkspaceImport, receipt sandbox.WorkspaceCodeReceipt, prepared filestore.Prepared) error {
	directory := sandbox.WorkspaceCodeDirectory + "/" + row.ID
	if receipt.ID != row.ID || receipt.ArchiveSHA256 != row.ArchiveSHA256 || receipt.Directory != directory || stringJSON(receipt.Repositories) != row.Repositories {
		return workspaceImportError(http.StatusConflict, "workspace_import_receipt_mismatch", "环境返回的导入收据不匹配，已保留文件，请核对结果")
	}
	err := s.sandbox.commitSandboxOperation(ctx, row.TargetSandboxID, func(tx *gorm.DB) error {
		if err := s.target(tx, *row); err != nil {
			return err
		}
		result := tx.Model(&models.WorkspaceImport{}).Where("id = ? AND state IN ? AND confirm_nonce = ?", row.ID, []string{"applying", "unknown"}, row.ConfirmNonce).Updates(map[string]any{"state": "applied", "directory": directory, "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return workspaceImportError(http.StatusConflict, "workspace_import_changed", "导入状态已变化，请重新加载")
		}
		currentFiles, err := filestore.FromDB(tx)
		if err != nil {
			return err
		}
		ref, err := currentFiles.PreparedRef(prepared)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		// Raw cells deliberately bypass filetext serializers: PreparedRef proves
		// publication, so this transaction never performs object HEAD/GET/PUT.
		created := tx.Table("solo_messages").Clauses(clause.OnConflict{DoNothing: true}).Create(map[string]any{"id": "workspace-import-" + row.ID, "session_id": row.SoloID, "role": "system", "content": ref, "workflow_run_id": row.RunID, "created_at": now})
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected > 0 {
			result := tx.Model(&models.SoloSession{}).Where("id = ? AND user_id = ? AND sandbox_id = ? AND status <> ?", row.SoloID, row.ActorID, row.TargetSandboxID, models.SoloStatusClosed).Updates(map[string]any{"updated_at": now, "conversation_context_pending": true, "workflow_context_version": gorm.Expr("COALESCE(workflow_context_version, 0) + 1")})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return workspaceImportError(http.StatusConflict, "workspace_target_changed", "Solo 环境已变化，请核对导入结果")
			}
		}
		return nil
	})
	if err == nil {
		row.State, row.Directory, row.Error = "applied", directory, ""
	}
	return err
}

// Reconcile is explicit and read-only at the provider. Missing or partial
// runtime receipts are unknown, never proof that it is safe to replay.
func (s *WorkspaceImportService) Reconcile(ctx context.Context, soloID, user, id string) (*WorkspaceImportView, error) {
	row, err := s.load(ctx, soloID, user, id)
	if err != nil {
		return nil, err
	}
	if row.State == "applied" || row.State == "prepared" {
		return importView(row)
	}
	if err := s.target(s.db.WithContext(ctx), row); err != nil {
		return nil, err
	}
	ctx, release, err := s.sandbox.acquireSandboxOperation(ctx, row.TargetSandboxID, "code_import_reconcile")
	if err != nil {
		return nil, err
	}
	defer release()
	row, err = s.load(ctx, soloID, user, id)
	if err != nil {
		return nil, err
	}
	if row.State == "applied" || row.State == "prepared" {
		return importView(row)
	}
	if err := s.target(s.db.WithContext(ctx), row); err != nil {
		return nil, err
	}
	receipt, err := s.runtime.Receipt(ctx, row.TargetSandboxID, row.ID)
	if err != nil {
		s.uncertain(ctx, row)
		return nil, workspaceImportError(http.StatusConflict, "workspace_import_unknown", "尚未找到完整导入收据，文件均保留；请检查隔离目录或重新创建预览")
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	prepared, err := files.Prepare(ctx, strings.NewReader(importResultText(row)), filestore.Text)
	if err != nil {
		return nil, err
	}
	if err := s.complete(ctx, &row, receipt, prepared); err != nil {
		return nil, err
	}
	s.notifyImport(soloID, id)
	return importView(row)
}

func (s *WorkspaceImportService) notifyImport(soloID, id string) {
	if s.solo == nil {
		return
	}
	s.solo.publishStatus(soloID)
	if chat := s.solo.lookup(soloID); chat != nil {
		chat.publish("history_changed", map[string]any{"eventId": "workspace-import-" + id})
	}
}
