package services

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceConsumerAccess struct {
	Folder    string
	Reference sandbox.WorkspaceBindingReference
	Close     func() error
}

// BeginWorkspaceConsumer pins a real connection, not an estimated timeout.
// Every replica uses this durable barrier. A crash can leave an active row;
// only a confirmed disconnect or exact provider stop may end that uncertainty.
func (s *SandboxService) BeginWorkspaceConsumer(ctx context.Context, id uint, kind string) (WorkspaceConsumerAccess, error) {
	access := WorkspaceConsumerAccess{Close: func() error { return nil }}
	bindings := s.workspaceBindings.Load()
	if bindings == nil {
		return access, nil
	}
	if kind != "ide" && kind != "terminal" && kind != "preview" {
		return access, ErrWorkspaceJobInvalid
	}
	ctx, row, release, err := s.beginSandboxAccess(ctx, id)
	if err != nil {
		return access, err
	}
	defer release()
	access.Folder = canonicalWorkspacePath(ctx, id)
	if proof, ok := ctx.Value(workspaceAccessKey{}).(workspaceAccessProof); ok && proof.sandboxID == id {
		access.Reference = proof.reference
	}
	if row.Purpose != SandboxPurposeSolo {
		return access, nil
	}
	var parent models.SoloSession
	if row.Purpose == SandboxPurposeSolo {
		if err := s.db.WithContext(ctx).Select("id", "user_id").Take(&parent, "id = ? AND sandbox_id = ?", row.ThreadID, id).Error; err != nil {
			return access, err
		}
	}
	use := models.WorkspaceBindingUse{ID: uuid.NewString(), SandboxID: id, Kind: kind, Owner: uuid.NewString(), State: "active"}
	err = s.commitSandboxOperationPrepared(ctx, id, func(tx *gorm.DB) error {
		if row.Purpose == SandboxPurposeSolo {
			current, err := bindings.parent(ctx, tx, parent.UserID, "solo", parent.ID, id)
			if err != nil {
				return err
			}
			expected, ok := ctx.Value(workspaceAccessParentKey{}).(workspaceAccessParent)
			if !ok || expected.SandboxID != id || expected.SoloID != parent.ID || expected.Epoch != current.Epoch || expected.Attempt != current.Attempt {
				return ErrWorkspaceJobConflict
			}
		}
		var head models.WorkspaceBindingHead
		err := workspaceCurrentRead(tx).Take(&head, "sandbox_id = ?", id).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if head.State != "ready" || head.PendingOperationID != "" || head.NativeFingerprint != access.Reference.Fingerprint {
				return ErrWorkspaceJobUnknown
			}
			use.BindingID = head.BindingID
		}
		return nil
	}, func(tx *gorm.DB) error { return tx.Create(&use).Error })
	if err != nil {
		return access, err
	}
	var closeMu sync.Mutex
	closed := false
	access.Close = func() error {
		closeMu.Lock()
		defer closeMu.Unlock()
		if closed {
			return nil
		}
		end, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		result := s.db.WithContext(end).Model(&models.WorkspaceBindingUse{}).Where("id = ? AND sandbox_id = ? AND owner = ? AND binding_id = ? AND state = 'active'", use.ID, id, use.Owner, use.BindingID).Update("state", "closed")
		if result.Error != nil {
			return result.Error
		}
		// A confirmed provider stop may already have closed the same row.
		if result.RowsAffected == 0 {
			var current models.WorkspaceBindingUse
			if err := s.db.WithContext(end).Select("id", "state", "owner").Take(&current, "id = ?", use.ID).Error; err != nil {
				return err
			}
			if current.State != "closed" || current.Owner != use.Owner {
				return ErrWorkspaceJobConflict
			}
		}
		closed = true
		return nil
	}
	return access, nil
}

func (s *WorkspaceBindingService) consumerIdleTx(ctx context.Context, tx *gorm.DB, sandboxID uint) error {
	var count int64
	if err := workspaceCurrentRead(tx.WithContext(ctx)).Table("workspace_binding_uses").Where("sandbox_id = ? AND state <> 'closed'", sandboxID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return soloErr(409, "workspace_consumers_busy", "请先关闭当前环境的 IDE、终端和预览连接，再切换工作区")
	}
	return nil
}
