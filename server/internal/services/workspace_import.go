package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/leasewatch"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacearchive"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type workspaceCodeRuntime interface {
	Inventory(context.Context, uint) (sandbox.WorkspaceCodeInventory, error)
	Export(context.Context, uint, []sandbox.WorkspaceCodeSelection) (io.ReadCloser, error)
	Import(context.Context, uint, string, string, io.Reader) (sandbox.WorkspaceCodeReceipt, error)
	Receipt(context.Context, uint, string) (sandbox.WorkspaceCodeReceipt, error)
}

type workspaceCodeBridge struct{ sandbox *SandboxService }

func (b workspaceCodeBridge) Inventory(ctx context.Context, id uint) (sandbox.WorkspaceCodeInventory, error) {
	host, port, password, err := b.sandbox.acpConnection(ctx, id)
	if err != nil {
		return sandbox.WorkspaceCodeInventory{}, err
	}
	return sandbox.InspectWorkspaceCode(ctx, host, port, password)
}
func (b workspaceCodeBridge) Export(ctx context.Context, id uint, selected []sandbox.WorkspaceCodeSelection) (io.ReadCloser, error) {
	fence, err := b.fence(ctx, id)
	if err != nil {
		return nil, err
	}
	host, port, password, err := b.sandbox.acpConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := requireCodeLeaseCapability(ctx, host, port, password); err != nil {
		return nil, err
	}
	leaseCtx, stop := b.maintainCodeLease(ctx, id, host, port, password, fence)
	body, err := sandbox.ExportWorkspaceCode(leaseCtx, host, port, password, selected, fence)
	if err != nil {
		stop()
		return nil, err
	}
	return &workspaceCodeBody{ReadCloser: body, stop: stop}, nil
}
func (b workspaceCodeBridge) Import(ctx context.Context, id uint, importID, digest string, source io.Reader) (sandbox.WorkspaceCodeReceipt, error) {
	fence, err := b.fence(ctx, id)
	if err != nil {
		return sandbox.WorkspaceCodeReceipt{}, err
	}
	host, port, password, err := b.sandbox.acpConnection(ctx, id)
	if err != nil {
		return sandbox.WorkspaceCodeReceipt{}, err
	}
	if err := requireCodeLeaseCapability(ctx, host, port, password); err != nil {
		return sandbox.WorkspaceCodeReceipt{}, err
	}
	leaseCtx, stop := b.maintainCodeLease(ctx, id, host, port, password, fence)
	defer stop()
	return sandbox.ImportWorkspaceCode(leaseCtx, host, port, password, importID, digest, source, fence)
}

func requireCodeLeaseCapability(ctx context.Context, host string, port int, password string) error {
	capability, err := sandbox.InspectWorkspaceCode(ctx, host, port, password)
	if err != nil {
		return err
	}
	if capability.Version != sandbox.WorkspaceCodeVersion || !capability.RenewableLease {
		return &sandbox.WorkspaceCodeError{Status: http.StatusUnprocessableEntity, Code: "workspace_import_unsupported"}
	}
	return nil
}

type workspaceCodeBody struct {
	io.ReadCloser
	stop func()
}

func (b *workspaceCodeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.stop()
	}
	return n, err
}

func (b *workspaceCodeBody) Close() error {
	b.stop()
	return b.ReadCloser.Close()
}

// Runtime renewal only forwards a newer, already committed SQL lease after a
// fresh access check. It never extends the SQL lease or creates a new import.
func (b workspaceCodeBridge) maintainCodeLease(ctx context.Context, id uint, host string, port int, password string, initial sandbox.WorkspaceCodeFence) (context.Context, func()) {
	leaseCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		acknowledged := initial.ExpiresAt
		err := leasewatch.Maintain(leaseCtx, acknowledged, sandboxOperationLeaseTTL/3, errSandboxOperationLost, func(check context.Context) (time.Time, error) {
			access, row, release, err := b.sandbox.beginSandboxAccess(check, id)
			if err != nil {
				return time.Time{}, errors.Join(errSandboxOperationLost, err)
			}
			defer release()
			if row.Status != "running" || row.Token != password || row.DesiredState == "stopped" {
				return time.Time{}, errSandboxOperationLost
			}
			fence, err := b.fence(access, id)
			if err != nil || fence.Generation != initial.Generation {
				return time.Time{}, errors.Join(errSandboxOperationLost, err)
			}
			if !fence.ExpiresAt.After(acknowledged) {
				return time.Time{}, errors.New("waiting for the committed physical lease renewal")
			}
			if err := sandbox.RenewWorkspaceCode(access, host, port, password, fence); err != nil {
				var remote *sandbox.WorkspaceCodeError
				if errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 {
					return time.Time{}, errors.Join(errSandboxOperationLost, err)
				}
				return time.Time{}, err
			}
			acknowledged = fence.ExpiresAt
			return acknowledged, nil
		})
		cancel(err)
	}()
	return leaseCtx, func() { cancel(context.Canceled); <-done }
}

func (b workspaceCodeBridge) fence(ctx context.Context, id uint) (sandbox.WorkspaceCodeFence, error) {
	for operation, _ := ctx.Value(sandboxOperationKey{}).(*sandboxOperation); operation != nil; operation = operation.parent {
		if operation.service != b.sandbox || operation.id != id {
			continue
		}
		var row models.Sandbox
		err := b.sandbox.db.WithContext(ctx).Select("generation", "operation_expires_at").Where("id = ? AND operation_owner = ? AND generation = ? AND operation_expires_at > ?", id, operation.owner, operation.generation, time.Now().UTC()).First(&row).Error
		if err != nil || row.OperationExpiresAt == nil {
			return sandbox.WorkspaceCodeFence{}, errSandboxOperationLost
		}
		return sandbox.WorkspaceCodeFence{Generation: row.Generation, ExpiresAt: *row.OperationExpiresAt}, nil
	}
	return sandbox.WorkspaceCodeFence{}, errSandboxOperationLost
}
func (b workspaceCodeBridge) Receipt(ctx context.Context, id uint, importID string) (sandbox.WorkspaceCodeReceipt, error) {
	host, port, password, err := b.sandbox.acpConnection(ctx, id)
	if err != nil {
		return sandbox.WorkspaceCodeReceipt{}, err
	}
	return sandbox.GetWorkspaceCodeReceipt(ctx, host, port, password, importID)
}

// WorkspaceImportService imports explicit Git commits into a fresh directory.
// Snapshot, uncommitted-file and native-session restoration are separate P7
// capabilities; this service never silently substitutes one of those modes.
type WorkspaceImportService struct {
	db      *gorm.DB
	solo    *SoloService
	sandbox *SandboxService
	runtime workspaceCodeRuntime
}

func NewWorkspaceImportService(db *gorm.DB, solo *SoloService, sbx *SandboxService) *WorkspaceImportService {
	return &WorkspaceImportService{db: db, solo: solo, sandbox: sbx, runtime: workspaceCodeBridge{sandbox: sbx}}
}

type WorkspaceImportSource struct {
	NodeID    string    `json:"nodeId"`
	Iteration int       `json:"iteration"`
	Attempt   int       `json:"attempt"`
	Shared    bool      `json:"shared"`
	Available bool      `json:"available"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
}

// Attempt selects a retained environment when a visit has several generations.
// It is an ordinal in the visit's chronological environment list, not a provider
// resource ID. Preview freezes the actual environment and commit privately.
type WorkspaceImportSourceInput struct {
	RunID     string `json:"runId"`
	NodeID    string `json:"nodeId"`
	Iteration int    `json:"iteration"`
	Attempt   int    `json:"attempt"`
}
type WorkspaceImportPreviewInput struct {
	WorkspaceImportSourceInput
	ClientRequestID string                           `json:"clientRequestId"`
	Repositories    []sandbox.WorkspaceCodeSelection `json:"repositories"`
}
type WorkspaceImportConfirmInput struct {
	ClientRequestID string `json:"clientRequestId"`
	Confirmed       bool   `json:"confirmed"`
}
type WorkspaceImportView struct {
	ID           string                            `json:"id"`
	Source       WorkspaceImportSourceInput        `json:"source"`
	State        string                            `json:"state"`
	Repositories []sandbox.WorkspaceCodeRepository `json:"repositories"`
	Directory    string                            `json:"directory,omitempty"`
	Destination  string                            `json:"destination"`
	Paths        []string                          `json:"paths"`
	Capabilities sandbox.WorkspaceCodeInventory    `json:"capabilities"`
	Warnings     []string                          `json:"warnings"`
	Error        string                            `json:"error,omitempty"`
	CreatedAt    time.Time                         `json:"createdAt"`
	ExpiresAt    *time.Time                        `json:"expiresAt,omitempty"`
}

var workspaceRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

func workspaceImportError(status int, code, message string) error {
	return soloErr(status, code, "%s", message)
}

func workspaceRuntimeError(err error) error {
	var remote *sandbox.WorkspaceCodeError
	if errors.As(err, &remote) {
		switch remote.Status {
		case http.StatusNotFound, http.StatusNotImplemented, http.StatusUnprocessableEntity:
			return workspaceImportError(http.StatusUnprocessableEntity, "workspace_snapshot_required", "当前环境或仓库不支持提交导入；未提交文件、非 Git 目录及原生会话需文件快照能力")
		case http.StatusRequestEntityTooLarge:
			return workspaceImportError(http.StatusRequestEntityTooLarge, "workspace_import_limit", "仓库或 Git 历史超过导入限额，请减少所选仓库；不会截断导入")
		case http.StatusConflict:
			return workspaceImportError(http.StatusConflict, "workspace_import_conflict", "导入目录已存在但未获得完整收据，已保留所有文件，请核对后新建预览")
		case http.StatusBadRequest:
			return workspaceImportError(http.StatusBadRequest, "invalid_workspace_import", "仓库、提交或归档格式无效")
		}
	}
	if errors.Is(err, workspacearchive.ErrLimit) {
		return workspaceImportError(http.StatusRequestEntityTooLarge, "workspace_import_limit", "导入归档超过限额，不会截断导入")
	}
	if errors.Is(err, workspacearchive.ErrUnsafe) {
		return workspaceImportError(http.StatusBadRequest, "invalid_workspace_import", "导入归档未通过安全校验")
	}
	return workspaceImportError(http.StatusBadGateway, "workspace_code_unavailable", "无法确认工作区状态，请核对环境后重试")
}

// Existing workspace Run APIs authorize the authenticated workspace user; Run
// CreatedBy is attribution, not a private-owner ACL. Import additionally needs
// the caller's private Solo and its explicit authorized Run link. No originating
// Solo's messages or credentials are read through that link.
func (s *WorkspaceImportService) authorize(db *gorm.DB, soloID, user, runID string) (models.SoloSession, models.Run, error) {
	var session models.SoloSession
	var run models.Run
	if user == "" {
		return session, run, workspaceImportError(http.StatusUnauthorized, "unauthorized", "请先登录")
	}
	err := db.Select("id", "user_id", "sandbox_id", "status").Where("id = ? AND user_id = ?", soloID, user).First(&session).Error
	if err != nil {
		return session, run, workspaceImportError(http.StatusNotFound, "session_not_found", "Solo 会话不存在")
	}
	var count int64
	if err := db.Model(&models.SoloWorkflowLink{}).Where("solo_id = ? AND user_id = ? AND run_id = ?", soloID, user, runID).Count(&count).Error; err != nil {
		return session, run, err
	}
	if count != 1 {
		return session, run, workspaceImportError(http.StatusNotFound, "workflow_not_found", "当前会话未关联此工作流")
	}
	if err := db.Select("id", "status").First(&run, "id = ?", runID).Error; err != nil {
		return session, run, err
	}
	if !workflowHistoryTerminal(run.Status) {
		return session, run, workspaceImportError(http.StatusConflict, "workflow_still_running", "请在工作流结束后选择要导入的提交")
	}
	return session, run, nil
}

func importSourceRows(db *gorm.DB, runID string) ([]models.Sandbox, error) {
	var rows []models.Sandbox
	err := db.Select("id", "run_id", "node_id", "iteration", "shared", "status", "desired_state", "created_at", "generation", "purpose").
		Where("run_id = ? AND purpose = ?", runID, "run").Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (s *WorkspaceImportService) Sources(ctx context.Context, soloID, user, runID string) ([]WorkspaceImportSource, error) {
	if _, _, err := s.authorize(s.db.WithContext(ctx), soloID, user, runID); err != nil {
		return nil, err
	}
	rows, err := importSourceRows(s.db.WithContext(ctx), runID)
	if err != nil {
		return nil, err
	}
	result := []WorkspaceImportSource{}
	attempts := map[string]int{}
	for _, row := range rows {
		key := row.NodeID + "\x00" + stringJSON(row.Iteration)
		attempts[key]++
		result = append(result, WorkspaceImportSource{NodeID: row.NodeID, Iteration: row.Iteration, Attempt: attempts[key], Shared: row.Shared, Available: row.Status == "running" && row.DesiredState != "stopped", State: row.Status, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func stringJSON(value any) string { body, _ := json.Marshal(value); return string(body) }

func (s *WorkspaceImportService) source(ctx context.Context, soloID, user string, in WorkspaceImportSourceInput) (models.SoloSession, models.Sandbox, error) {
	session, _, err := s.authorize(s.db.WithContext(ctx), soloID, user, in.RunID)
	if err != nil {
		return session, models.Sandbox{}, err
	}
	if in.NodeID == "" || len(in.NodeID) > 191 || in.Iteration < 0 || in.Attempt < 0 {
		return session, models.Sandbox{}, workspaceImportError(http.StatusBadRequest, "invalid_workspace_source", "请选择工作流节点和执行轮次")
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.StateRun{}).Where("run_id = ? AND node_id = ? AND iteration = ?", in.RunID, in.NodeID, in.Iteration).Count(&count).Error; err != nil {
		return session, models.Sandbox{}, err
	}
	if count == 0 {
		return session, models.Sandbox{}, workspaceImportError(http.StatusNotFound, "workspace_source_not_found", "所选执行轮次不存在")
	}
	var rows []models.Sandbox
	if err := s.db.WithContext(ctx).Select("id", "run_id", "node_id", "iteration", "shared", "status", "desired_state", "generation", "purpose").Where("run_id = ? AND node_id = ? AND iteration = ? AND purpose = ?", in.RunID, in.NodeID, in.Iteration, "run").Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return session, models.Sandbox{}, err
	}
	attempt := in.Attempt
	if attempt == 0 && len(rows) == 1 {
		attempt = 1
	}
	if attempt == 0 && len(rows) > 1 {
		return session, models.Sandbox{}, workspaceImportError(http.StatusConflict, "workspace_source_ambiguous", "此轮次有多个保留环境，请明确选择来源")
	}
	if attempt < 1 || attempt > len(rows) {
		return session, models.Sandbox{}, workspaceImportError(http.StatusUnprocessableEntity, "workspace_snapshot_required", "该执行轮次的工作区已不再保留，需要文件快照后才能导入")
	}
	row := rows[attempt-1]
	if row.Status != "running" || row.DesiredState == "stopped" {
		return session, row, workspaceImportError(http.StatusConflict, "workspace_source_unavailable", "来源环境未运行；查看预览不会自动恢复或重建环境")
	}
	return session, row, nil
}

func (s *WorkspaceImportService) Inventory(ctx context.Context, soloID, user string, in WorkspaceImportSourceInput) (sandbox.WorkspaceCodeInventory, error) {
	_, source, err := s.source(ctx, soloID, user, in)
	if err != nil {
		return sandbox.WorkspaceCodeInventory{}, err
	}
	ctx, release, err := s.sandbox.acquireSandboxOperation(ctx, source.ID, "code_export")
	if err != nil {
		return sandbox.WorkspaceCodeInventory{}, err
	}
	defer release()
	if _, current, err := s.source(ctx, soloID, user, in); err != nil || current.ID != source.ID {
		if err != nil {
			return sandbox.WorkspaceCodeInventory{}, err
		}
		return sandbox.WorkspaceCodeInventory{}, workspaceImportError(http.StatusConflict, "workspace_source_changed", "来源环境已变化，请重新选择")
	}
	result, err := s.runtime.Inventory(ctx, source.ID)
	if err != nil {
		return result, workspaceRuntimeError(err)
	}
	if result.Version != sandbox.WorkspaceCodeVersion || len(result.Repositories) > sandbox.WorkspaceCodeMaxRepositories {
		return sandbox.WorkspaceCodeInventory{}, workspaceRuntimeError(workspacearchive.ErrUnsafe)
	}
	// Disclose only verified capabilities, never provider-supplied host errors.
	result.WorkingChanges, result.NonGit, result.IgnoredFiles = false, false, false
	for i := range result.Repositories {
		if !workspaceRepoPath(result.Repositories[i].Path) {
			return sandbox.WorkspaceCodeInventory{}, workspaceRuntimeError(workspacearchive.ErrUnsafe)
		}
		if result.Repositories[i].Reason != "" {
			switch result.Repositories[i].Reason {
			case "repository_submodules", "repository_lfs", "repository_shallow", "repository_external_objects", "repository_external_worktree", "repository_custom_configuration", "repository_rewritten_history", "repository_unsafe_metadata", "repository_limit":
				// Only stable capability codes may leave the runtime boundary.
			default:
				result.Repositories[i].Reason = "repository_requires_snapshot"
			}
		}
	}
	return result, nil
}

func (s *WorkspaceImportService) Get(ctx context.Context, soloID, user, id string) (*WorkspaceImportView, error) {
	row, err := s.load(ctx, soloID, user, id)
	if err != nil {
		return nil, err
	}
	return importView(row)
}

// List allows a reloaded drawer to recover a durable preview or an uncertain
// dispatch without relying on a browser receipt cache or reading object bytes.
func (s *WorkspaceImportService) List(ctx context.Context, soloID, user, runID string) ([]WorkspaceImportView, error) {
	if _, _, err := s.authorize(s.db.WithContext(ctx), soloID, user, runID); err != nil {
		return nil, err
	}
	if err := s.expirePrepared(ctx, soloID, user, runID, ""); err != nil {
		return nil, err
	}
	var rows []models.WorkspaceImport
	if err := s.db.WithContext(ctx).Where("solo_id = ? AND actor_id = ? AND run_id = ?", soloID, user, runID).Order("created_at DESC, id DESC").Limit(20).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := []WorkspaceImportView{}
	for _, row := range rows {
		view, err := importView(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *view)
	}
	return result, nil
}

func (s *WorkspaceImportService) load(ctx context.Context, soloID, user, id string) (models.WorkspaceImport, error) {
	var row models.WorkspaceImport
	if err := s.db.WithContext(ctx).Where("id = ? AND solo_id = ? AND actor_id = ?", id, soloID, user).First(&row).Error; err != nil {
		return row, workspaceImportError(http.StatusNotFound, "workspace_import_not_found", "导入预览不存在")
	}
	_, _, err := s.authorize(s.db.WithContext(ctx), soloID, user, row.RunID)
	if err == nil && workspaceImportExpired(row, time.Now()) {
		if err := s.expirePrepared(ctx, soloID, user, row.RunID, id); err != nil {
			return row, err
		}
		err = s.db.WithContext(ctx).Where("id = ? AND solo_id = ? AND actor_id = ?", id, soloID, user).First(&row).Error
	}
	return row, err
}

func importView(row models.WorkspaceImport) (*WorkspaceImportView, error) {
	if workspaceImportExpired(row, time.Now()) {
		row.State, row.Error = "expired", "workspace_import_expired"
	}
	var repositories []sandbox.WorkspaceCodeRepository
	if err := json.Unmarshal([]byte(row.Repositories), &repositories); err != nil {
		return nil, err
	}
	warnings := []string{"仅导入所选 Git 提交和历史；不会自动提交、推送或修改原工作区", "未提交改动、未跟踪文件、忽略文件、非 Git 目录与原生会话不包含在内，需后续文件快照能力", "导入到新的独立目录；不会切换或覆盖 Solo 当前仓库", "每仓库 Git bundle 上限 64 MiB、合计 128 MiB，单个工作区文件上限 32 MiB；超限会拒绝"}
	paths := []string{}
	for i, repo := range repositories {
		if repo.Dirty || repo.Untracked {
			warnings = append(warnings, repo.Path+" 包含未提交或未跟踪文件，这些文件不会导入")
		}
		if row.Directory != "" {
			paths = append(paths, row.Directory+"/"+importRepoName(repo.Path, i))
		}
	}
	return &WorkspaceImportView{
		ID:     row.ID,
		Source: WorkspaceImportSourceInput{RunID: row.RunID, NodeID: row.NodeID, Iteration: row.Iteration, Attempt: row.Attempt},
		State:  row.State, Repositories: repositories, Directory: row.Directory,
		Destination: sandbox.WorkspaceCodeDirectory + "/" + row.ID, Paths: paths,
		Capabilities: sandbox.WorkspaceCodeInventory{Version: sandbox.WorkspaceCodeVersion, Repositories: []sandbox.WorkspaceCodeRepository{}, MaxBundleBytes: sandbox.WorkspaceCodeMaxBundleBytes, MaxTotalBytes: sandbox.WorkspaceCodeMaxTotalBytes, MaxFileBytes: sandbox.WorkspaceCodeMaxFileBytes},
		Warnings:     warnings, Error: row.Error, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
	}, nil
}

func importRepoName(path string, index int) string {
	if path == "." {
		return "repo-" + stringJSON(index+1)
	}
	return path
}

func (s *WorkspaceImportService) Preview(ctx context.Context, soloID, user string, in WorkspaceImportPreviewInput) (*WorkspaceImportView, error) {
	if !workspaceRequestID.MatchString(in.ClientRequestID) || len(in.Repositories) < 1 || len(in.Repositories) > sandbox.WorkspaceCodeMaxRepositories {
		return nil, workspaceImportError(http.StatusBadRequest, "invalid_workspace_import", "请选择仓库和完整提交，并提供有效请求标识")
	}
	seen := map[string]bool{}
	for _, repo := range in.Repositories {
		if !workspaceRepoPath(repo.Path) || !workspaceCommit.MatchString(repo.Commit) || seen[repo.Path] {
			return nil, workspaceImportError(http.StatusBadRequest, "invalid_workspace_import", "仓库选择或完整 Git 提交无效")
		}
		seen[repo.Path] = true
	}
	key := sha256.Sum256([]byte(user + "\x00" + soloID + "\x00" + in.ClientRequestID))
	id := hex.EncodeToString(key[:])
	request := in
	request.ClientRequestID = ""
	fp := sha256.Sum256([]byte(stringJSON(request)))
	fingerprint := hex.EncodeToString(fp[:])
	var previous models.WorkspaceImport
	err := s.db.WithContext(ctx).First(&previous, "id = ?", id).Error
	if err == nil {
		if previous.ActorID != user || previous.SoloID != soloID || previous.Fingerprint != fingerprint {
			return nil, workspaceImportError(http.StatusConflict, "workspace_import_request_conflict", "此请求标识已用于不同预览，请重新选择")
		}
		return s.Get(ctx, soloID, user, id)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	session, source, err := s.source(ctx, soloID, user, in.WorkspaceImportSourceInput)
	if err != nil {
		return nil, err
	}
	if session.Status == models.SoloStatusClosed || session.SandboxID == 0 {
		return nil, workspaceImportError(http.StatusConflict, "environment_missing", "请先启动 Solo 环境后再创建导入预览")
	}
	if err := s.expirePrepared(ctx, soloID, user, in.RunID, ""); err != nil {
		return nil, err
	}
	ctx, release, err := s.sandbox.acquireSandboxOperation(ctx, source.ID, "code_export")
	if err != nil {
		return nil, err
	}
	defer release()
	_, current, err := s.source(ctx, soloID, user, in.WorkspaceImportSourceInput)
	if err != nil {
		return nil, err
	}
	if current.ID != source.ID {
		return nil, workspaceImportError(http.StatusConflict, "workspace_source_changed", "来源环境已变化，请重新选择")
	}
	stream, err := s.runtime.Export(ctx, source.ID, in.Repositories)
	if err != nil {
		return nil, workspaceRuntimeError(err)
	}
	defer func() { _ = stream.Close() }()
	archive, manifest, digest, err := canonicalCodeArchive(ctx, stream)
	if err != nil {
		return nil, workspaceRuntimeError(err)
	}
	defer func() { _ = archive.Close(); _ = os.Remove(archive.Name()) }()
	if len(manifest.Repositories) != len(in.Repositories) {
		return nil, workspaceRuntimeError(workspacearchive.ErrUnsafe)
	}
	for i, repo := range manifest.Repositories {
		if repo.Path != in.Repositories[i].Path || repo.Commit != in.Repositories[i].Commit {
			return nil, workspaceRuntimeError(workspacearchive.ErrUnsafe)
		}
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	ref, _, err := files.PutBounded(ctx, archive, filestore.Binary, sandbox.WorkspaceCodeMaxTotalBytes+1<<20)
	if err != nil {
		return nil, err
	}
	_, storedDigest, err := filestore.Parse(ref)
	if err != nil || storedDigest != digest {
		return nil, workspaceRuntimeError(workspacearchive.ErrUnsafe)
	}
	expiresAt := time.Now().UTC().Add(workspaceImportPreviewTTL)
	row := models.WorkspaceImport{ID: id, ActorID: user, SoloID: soloID, RunID: in.RunID, NodeID: in.NodeID, Iteration: in.Iteration, Attempt: in.Attempt, SourceSandboxID: source.ID, SourceGeneration: current.Generation, TargetSandboxID: session.SandboxID, Fingerprint: fingerprint, ArchiveRef: ref, ArchiveSHA256: digest, Repositories: stringJSON(manifest.Repositories), State: "prepared", ExpiresAt: &expiresAt}
	if row.Attempt == 0 {
		row.Attempt = 1
	}
	err = s.sandbox.commitSandboxOperation(ctx, source.ID, func(tx *gorm.DB) error {
		fresh, _, err := s.authorize(tx, soloID, user, in.RunID)
		if err != nil {
			return err
		}
		if fresh.SandboxID != session.SandboxID || fresh.Status == models.SoloStatusClosed {
			return workspaceImportError(http.StatusConflict, "session_changed", "Solo 环境已变化，请重新创建预览")
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing models.WorkspaceImport
			if err := tx.First(&existing, "id = ?", id).Error; err != nil {
				return err
			}
			if existing.Fingerprint != fingerprint || existing.ActorID != user || existing.SoloID != soloID {
				return workspaceImportError(http.StatusConflict, "workspace_import_request_conflict", "此请求标识已用于不同预览")
			}
			row = existing
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return importView(row)
}
