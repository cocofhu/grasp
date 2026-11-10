package services

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type workspaceAccessKey struct{}
type workspaceAccessParentKey struct{}
type workspaceAccessParent struct {
	SandboxID uint
	SoloID    string
	Epoch     uint64
	Attempt   string
}
type workspaceAccessProof struct {
	sandboxID uint
	path      string
	reference sandbox.WorkspaceBindingReference
}

// The caller already holds P1's live operation/current-grant fence. Legacy
// runtimes may retain their original chat, but a published or uncertain
// canonical head can never fall back to an independent cached directory.
func (s *SandboxService) canonicalSandboxAccess(ctx context.Context, row *models.Sandbox) (context.Context, error) {
	proof, err := s.canonicalSandboxProof(ctx, row)
	if err != nil {
		return ctx, err
	}
	if proof.sandboxID == 0 {
		return ctx, nil
	}
	if proof.sandboxID == row.ID {
		s.mu.Lock()
		live := s.live[row.ID]
		if live != nil && live.sb != nil {
			live.sb.SetActiveWorkspace(proof.path, proof.reference.Fingerprint)
		}
		s.mu.Unlock()
	}
	return context.WithValue(ctx, workspaceAccessKey{}, proof), nil
}

// canonicalSandboxProof only inspects the current root. Buffered image reads
// validate this proof again before serving bytes without changing the root
// used by concurrent Agent, IDE or terminal requests.
func (s *SandboxService) canonicalSandboxProof(ctx context.Context, row *models.Sandbox) (workspaceAccessProof, error) {
	bindings := s.workspaceBindings.Load()
	if bindings == nil {
		// Disabling new file operations or losing startup execution authority
		// cannot erase an already-published root. Without its resolver, retain
		// history but deny live access instead of falling back to original cwd.
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", row.ID).Count(&count).Error; err != nil {
			return workspaceAccessProof{}, err
		}
		if count != 0 {
			return workspaceAccessProof{}, soloErr(409, "workspace_unavailable", "当前工作区绑定暂不可核验，聊天和文件已保留")
		}
		return workspaceAccessProof{}, nil
	}
	var head models.WorkspaceBindingHead
	err := s.db.WithContext(ctx).Take(&head, "sandbox_id = ?", row.ID).Error
	known := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return workspaceAccessProof{}, err
	}
	if known && (head.State != "ready" || head.PendingOperationID != "") {
		return workspaceAccessProof{}, soloErr(409, "workspace_switch_pending", "工作区切换尚未确认，请核对原操作后继续")
	}
	remote, err := bindings.options.Remote(ctx, row.ID)
	if err != nil {
		return workspaceAccessProof{}, err
	}
	caps, err := remote.WorkspaceBindingCapabilities(ctx)
	if err != nil {
		var unavailable *sandbox.WorkspaceJobError
		if !known && errors.As(err, &unavailable) && (unavailable.Status == http.StatusNotFound || unavailable.Status == http.StatusNotImplemented) {
			return workspaceAccessProof{}, nil
		}
		return workspaceAccessProof{}, err
	}
	if caps.Version != 1 || caps.Resource != row.Name || caps.Lineage != bindings.options.ExpectedLineage || !caps.DurableHead || !caps.Activate || !caps.Return || caps.ProcessMemory {
		return workspaceAccessProof{}, ErrWorkspaceJobUnsupported
	}
	view, err := remote.WorkspaceBindingHead(ctx)
	if err != nil {
		return workspaceAccessProof{}, err
	}
	if !view.Ready || view.PendingOperationID != "" || !filepath.IsAbs(view.Path) || !validNativeBinding(view.Head, caps.Resource, caps.Lineage) {
		return workspaceAccessProof{}, ErrWorkspaceJobUnknown
	}
	if known {
		if view.Head.Reference.ID != head.NativeID || view.Head.Reference.Revision != head.Revision || view.Head.Reference.Fingerprint != head.NativeFingerprint {
			return workspaceAccessProof{}, ErrWorkspaceJobUnknown
		}
	} else if view.Head.Reference.Revision != 1 || view.Head.Target.Kind != "original" {
		return workspaceAccessProof{}, ErrWorkspaceJobUnknown
	}
	return workspaceAccessProof{row.ID, view.Path, view.Head.Reference}, nil
}

func canonicalWorkspacePath(ctx context.Context, sandboxID uint) string {
	proof, ok := ctx.Value(workspaceAccessKey{}).(workspaceAccessProof)
	if !ok || proof.sandboxID != sandboxID {
		return ""
	}
	return proof.path
}

// WorkspaceFolder returns trusted internal proxy metadata, never a browser
// selection. History/receipt handlers do not call this live-access method.
func (s *SandboxService) WorkspaceFolder(ctx context.Context, id uint) (string, error) {
	ctx, row, release, err := s.beginSandboxConsumerAccess(ctx, id)
	if err != nil {
		return "", err
	}
	defer release()
	if path := canonicalWorkspacePath(ctx, id); path != "" {
		return path, nil
	}
	s.mu.Lock()
	live := s.live[id]
	var path string
	if live != nil && live.sb != nil {
		path = live.sb.EffectiveWorkspaceDir()
	}
	s.mu.Unlock()
	if path != "" {
		return path, nil
	}
	if row.Purpose == SandboxPurposeSolo && s.workspaceBindings.Load() != nil && s.skills != nil {
		if profile, ok := s.skills.Get(row.Profile); ok {
			return s.effectiveAgent(profile).Layout.WorkspaceDir, nil
		}
	}
	return "", nil
}
