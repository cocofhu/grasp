package services

import (
	"context"
	"io"
	"strings"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

const maxSoloImageBytes = 16 << 20

// Image reads a local image from the owned session's currently authorized
// environment. Export confines guest paths to the workspace or /tmp; local
// worktree mode confines every path to its workspace with os.Root.
func (s *SoloService) Image(ctx context.Context, id, user, path string) ([]byte, string, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, "", err
	}
	if path == "" || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") {
		return nil, "", soloErr(400, "invalid_image_path", "图片路径无效")
	}
	if s.sbx == nil || row.SandboxID == 0 {
		return nil, "", soloErr(404, "image_unavailable", "图片所在环境不可用")
	}
	ctx, release, err := s.sbx.operations().Access(ctx, row.SandboxID, sandboxOperationInference)
	if err != nil {
		return nil, "", err
	}
	defer release()
	environment, err := s.sbx.Get(row.SandboxID)
	if err != nil {
		return nil, "", err
	}
	if environment.ThreadID != row.ID || environment.Purpose != SandboxPurposeSolo {
		return nil, "", soloErr(404, "image_unavailable", "图片所在环境不可用")
	}
	if err := s.sbx.executionCheck(ctx, environment.ID); err != nil {
		return nil, "", err
	}
	// A bounded image read can run alongside a native turn. Do not queue it
	// behind the turn's mutation lease: the browser may be loading a screenshot
	// while that same turn is still streaming. Buffer first, then recheck the
	// exact parent, resource, authorization and workspace before serving bytes.
	if environment.Status != "running" || environment.DesiredState == "stopped" {
		return nil, "", soloErr(409, "environment_stopped", "图片所在环境未运行")
	}
	grant, err := s.sbx.soloRecoveryAuthorization(ctx, environment)
	if err != nil {
		return nil, "", soloErr(409, "environment_authorization_blocked", "图片所在环境的当前授权不可用")
	}
	before, err := s.sbx.canonicalSandboxProof(ctx, environment)
	if err != nil {
		return nil, "", err
	}
	s.sbx.mu.Lock()
	live := s.sbx.live[environment.ID]
	var source *sandbox.Sandbox
	if live != nil {
		source = live.sb
	}
	s.sbx.mu.Unlock()
	if source == nil && s.sbx.mgr != nil {
		source, err = s.sbx.mgr.AttachNoEnsure(ctx, environment.Name)
	}
	if err != nil || source == nil {
		return nil, "", soloErr(404, "image_unavailable", "图片所在环境不可用")
	}
	workspace := source.EffectiveWorkspaceDir()
	if before.sandboxID == environment.ID {
		workspace = before.path
	}
	reader, err := source.OpenArtifactFileAtWorkspace(ctx, workspace, path)
	if err != nil {
		return nil, "", soloErr(404, "image_unavailable", "图片文件不可用")
	}
	defer func() { _ = reader.Close() }()
	data, media, err := readSoloImage(reader)
	if err != nil {
		return nil, "", err
	}
	current, err := s.Require(id, user)
	if err != nil {
		return nil, "", err
	}
	if current.SandboxID != row.SandboxID || current.LifecycleEpoch != row.LifecycleEpoch || current.LifecycleAttemptID != row.LifecycleAttemptID {
		return nil, "", soloErr(409, "environment_changed", "图片所在环境已变化，请重新加载")
	}
	latest, err := s.sbx.Get(environment.ID)
	if err != nil {
		return nil, "", err
	}
	if latest.Name != environment.Name || latest.ThreadID != id || latest.Purpose != SandboxPurposeSolo || latest.Status != "running" || latest.DesiredState == "stopped" || current.Status == models.SoloStatusClosed {
		return nil, "", soloErr(409, "environment_changed", "图片所在环境已变化，请重新加载")
	}
	if err := s.sbx.recheckSoloRecoveryGrant(ctx, latest, grant); err != nil {
		return nil, "", soloErr(409, "environment_authorization_blocked", "图片读取期间授权已变化")
	}
	if err := s.sbx.executionCheck(ctx, environment.ID); err != nil {
		return nil, "", err
	}
	after, err := s.sbx.canonicalSandboxProof(ctx, latest)
	if err != nil {
		return nil, "", err
	}
	if before != after {
		return nil, "", soloErr(409, "environment_changed", "图片所在工作区已变化，请重新加载")
	}
	if err := s.sbx.operations().CommitAccess(ctx, environment.ID, func(tx *gorm.DB) error {
		return requireSoloRecoveryParent(tx, latest, grant)
	}, func(*gorm.DB) error { return nil }); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return data, media, nil
}

func readSoloImage(reader io.Reader) ([]byte, string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxSoloImageBytes+1))
	if err != nil {
		return nil, "", soloErr(404, "image_unavailable", "图片文件不可用")
	}
	if len(data) > maxSoloImageBytes {
		return nil, "", soloErr(413, "image_too_large", "图片超过大小限制")
	}
	media := blob.SniffSupportedImageMIME(data)
	if media == "" {
		return nil, "", soloErr(415, "unsupported_image", "文件不是支持的图片格式")
	}
	return data, media, nil
}
