package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// ErrSandboxAbsent means this run node has no sandbox row, or the container is
// not running and the row is not still creating or pulling. Handlers answer
// HTTP 200 with found:false so the browser does not log a failed fetch.
var ErrSandboxAbsent = errors.New("not found")

// BeginRunSandbox records a per-run node sandbox as "creating" BEFORE the (slow)
// gateway provisioning completes, so it shows up in the sandbox list and the
// node's live log as "starting" instead of a 404 during the cold-start window.
// The row uses a placeholder name; RegisterRunSandbox later adopts the real
// gateway id and flips it to "running", or UnregisterRunSandbox removes it on
// failure. Implements runtime.RunSandboxBeginner. Keyed by run+node so a retry
// replaces the previous placeholder row.
func (s *SandboxService) BeginRunSandbox(info runtime.RunSandboxInfo) error {
	if info.Name == "" {
		return nil
	}
	now := time.Now()
	row := &models.Sandbox{Name: info.Name, Profile: info.Profile, Purpose: "run", Status: "creating",
		RepoURL: info.RepoURL, RunID: info.RunID, WorkflowID: info.WorkflowID, WorkflowName: info.WorkflowName,
		NodeID: info.NodeID, HomeDir: info.HomeDir, Token: info.Token, Shared: info.Shared, LastActiveAt: &now, ModelActiveAt: &now}
	if err := s.createSandboxRow(row); err != nil {
		return err
	}
	s.mu.Lock()
	s.runActive[info.Name] = true
	s.mu.Unlock()
	return nil
}

// RegisterRunSandbox records a per-run workflow node sandbox so it appears in
// the sandbox list while the node runs. Implements runtime.SandboxRegistry.
// The row is marked active so it cannot be reclaimed during execution.
// Waiting or finished nodes use the same idle policy as every other purpose. When a "creating" placeholder row was
// pre-registered (BeginRunSandbox), it is adopted in place: the gateway-assigned
// id replaces the placeholder name and the status flips to "running".
func (s *SandboxService) RegisterRunSandbox(info runtime.RunSandboxInfo) {
	if info.Name == "" {
		return
	}
	fields := map[string]any{
		"name":             info.Name,
		"profile":          info.Profile,
		"purpose":          "run",
		"status":           "running",
		"host":             info.Host,
		"acp_port":         info.ACPPort,
		"code_server_port": info.CodeServerPort,
		"repo_url":         info.RepoURL,
		"run_id":           info.RunID,
		"workflow_id":      info.WorkflowID,
		"workflow_name":    info.WorkflowName,
		"node_id":          info.NodeID,
		"home_dir":         info.HomeDir,
		"error":            "",
		"updated_at":       time.Now(),
		"last_active_at":   time.Now(),
		"model_active_at":  time.Now(),
	}
	if info.Token != "" {
		fields["token"] = info.Token
	}
	if info.Shared {
		fields["shared"] = true
	}
	s.markVisitSandboxReady(info.RunID, info.NodeID, fields["updated_at"].(time.Time))
	// Adopt the pre-registered creating/pulling placeholder row (by run+node) in place,
	// swapping its placeholder name for the gateway id. Falls back to an upsert
	// by name when no placeholder exists (registry without RunSandboxBeginner).
	var existing models.Sandbox
	err := s.db.Where("run_id = ? AND node_id = ? AND purpose = ? AND status IN ?",
		info.RunID, info.NodeID, "run", []string{"creating", "pulling"}).
		Order("updated_at desc").First(&existing).Error
	if err == nil {
		oldName := existing.Name
		if uerr := s.db.Model(&models.Sandbox{}).Where("id = ?", existing.ID).
			Updates(fields).Error; uerr != nil {
			log.Warn().Err(uerr).Str("name", info.Name).Msg("adopt run sandbox failed")
		}
		s.mu.Lock()
		if oldName != "" && oldName != info.Name {
			delete(s.runActive, oldName)
		}
		s.runActive[info.Name] = true
		s.mu.Unlock()
		return
	}
	if err := s.db.Where(models.Sandbox{Name: info.Name}).
		Assign(fields).FirstOrCreate(&models.Sandbox{}).Error; err != nil {
		log.Warn().Err(err).Str("name", info.Name).Msg("register run sandbox failed")
	}
	s.mu.Lock()
	s.runActive[info.Name] = true
	s.mu.Unlock()
}

// markVisitSandboxReady stamps the node's active visit with the first time its
// sandbox came up, so the boot steps stay readable after the sandbox is gone.
func (s *SandboxService) markVisitSandboxReady(runID, nodeID string, at time.Time) {
	if runID == "" || nodeID == "" {
		return
	}
	var sr models.StateRun
	if err := s.db.Select("id").Where("run_id = ? AND node_id = ? AND status IN ?", runID, nodeID,
		[]string{"running", "waiting_human"}).Order("id desc").First(&sr).Error; err != nil {
		return
	}
	if err := s.db.Model(&models.StateRun{}).Where("id = ? AND sandbox_ready_at IS NULL", sr.ID).
		Update("sandbox_ready_at", at).Error; err != nil {
		log.Warn().Err(err).Str("run", runID).Str("node", nodeID).Msg("stamp sandbox ready failed")
	}
}

// RetireRunSandbox marks task completion and starts the shared idle interval.
func (s *SandboxService) RetireRunSandbox(name string) {
	if name == "" {
		return
	}
	ctx := context.Background()
	if s.changes != nil {
		var row models.Sandbox
		if s.db.Where("name = ?", name).Take(&row).Error == nil {
			s.archiveWorkspaceChanges(ctx, &row, nil)
		}
	}
	s.archiveLog(ctx, name)
	s.mu.Lock()
	delete(s.runActive, name)
	s.mu.Unlock()
	s.MarkModelActivityByName(name)
}

// ParkRunSandbox releases execution ownership while waiting for human input.
// The workflow and its latest recovery point remain persisted indefinitely.
func (s *SandboxService) ParkRunSandbox(name string) {
	s.mu.Lock()
	delete(s.runActive, name)
	s.mu.Unlock()
	s.MarkModelActivityByName(name)
}

// SetRunKeepAlive turns automatic idle reclaim off or back on for a run's
// sandboxes. Turning it back on restarts their full idle interval.
func (s *SandboxService) SetRunKeepAlive(runID string, on bool) error {
	if err := s.db.Model(&models.Run{}).Where("id = ?", runID).Update("keep_alive", on).Error; err != nil || on {
		return err
	}
	var rows []models.Sandbox
	if err := s.db.Select("id").Where("run_id = ? AND purpose = ?", runID, "run").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		s.MarkModelActivity(row.ID)
	}
	return nil
}

// AcquireRunSandbox serializes new work with idle save/resume. Reference
// counts protect a shared sandbox when another node is still executing.
func (s *SandboxService) AcquireRunSandbox(ctx context.Context, name string) (func(), error) {
	var row models.Sandbox
	if err := s.db.Where("name = ?", name).First(&row).Error; err != nil {
		return nil, err
	}
	unlock, err := s.lockSoloRecovery(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, _, err := s.ensureRunningLocked(ctx, row.ID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.runTasks[name]++
	delete(s.runActive, name)
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.runTasks[name]--
			if s.runTasks[name] <= 0 {
				delete(s.runTasks, name)
			}
			s.mu.Unlock()
			s.MarkModelActivity(row.ID)
		})
	}, nil
}

// ResumeRunSandbox returns the retained environment before a workflow reply.
// found=false means no recovery environment exists; transient errors never
// authorize destroying a saved environment and silently creating a new one.
func (s *SandboxService) ResumeRunSandbox(ctx context.Context, runID, nodeID string) (*sandbox.Sandbox, bool, bool, error) {
	row, err := runNodeSandboxRow(s.db, runID, nodeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, true, false, err
	}
	sb, warm, err := s.EnsureRunning(ctx, row.ID)
	return sb, true, warm, err
}

func (s *SandboxService) CancelRunSandboxes(ctx context.Context, runID string) error {
	var rows []models.Sandbox
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Find(&rows).Error; err != nil {
		return err
	}
	// One durable write protects the entire run before slow provider cleanup.
	// The FSM can finish promptly even when every VM's storage is unavailable.
	if err := s.db.WithContext(ctx).Model(&models.Sandbox{}).Where("run_id = ?", runID).Update("cleanup_pending", true).Error; err != nil {
		return err
	}
	for _, row := range rows {
		s.mu.Lock()
		delete(s.runActive, row.Name)
		s.mu.Unlock()
		s.Cancel(row.ID)
	}
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, row := range rows {
			if cleanupCtx.Err() != nil {
				return
			}
			_ = s.Destroy(cleanupCtx, row.ID)
		}
	}()
	return nil
}

// ArchiveRunSandboxLogs best-effort archives live logs for every per-run node
// sandbox still recorded for runID. Returns how many containers were archived
// and a non-empty note when none could be captured (for DisplayReason degrade).
func (s *SandboxService) ArchiveRunSandboxLogs(ctx context.Context, runID string) (archived int, degradeNote string) {
	if s == nil || runID == "" {
		return 0, "无沙箱服务，未能拉取 live logs"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var rows []models.Sandbox
	if err := s.db.Where("run_id = ? AND purpose = ?", runID, "run").Find(&rows).Error; err != nil {
		return 0, "查询沙箱失败，未能拉取 live logs"
	}
	if len(rows) == 0 {
		return 0, "无存活沙箱记录，未能拉取 live logs"
	}
	for _, row := range rows {
		s.archiveLog(ctx, row.Name)
		if s.hasArchivedLog(row.Name) {
			archived++
		}
	}
	if archived == 0 {
		return 0, "容器已销毁或日志为空，未能归档 live logs"
	}
	return archived, ""
}

func (s *SandboxService) hasArchivedLog(name string) bool {
	if name == "" || s == nil || s.db == nil {
		return false
	}
	var rec models.SandboxLog
	if err := s.db.Where("name = ?", name).First(&rec).Error; err != nil {
		return false
	}
	return strings.TrimSpace(rec.Content) != ""
}

// UnregisterRunSandbox clears a per-run node sandbox record once the runtime
// provider tears down its container. Cleanup intent and archive keys remain
// durable until provider API and archive deletions succeed. Host garbage
// collection belongs to the provider. Implements runtime.SandboxRegistry.
func (s *SandboxService) UnregisterRunSandbox(name string) {
	if name == "" {
		return
	}
	// Archive the raw container logs before the provider removes the container,
	// so the startup/exec output (e.g. a failed git clone) survives for
	// post-mortem troubleshooting even after the sandbox is gone.
	s.archiveLog(context.Background(), name)
	s.mu.Lock()
	delete(s.runActive, name)
	s.mu.Unlock()
	var row models.Sandbox
	if err := s.db.Where("name = ?", name).First(&row).Error; err != nil {
		return
	}
	if err := s.CancelSandbox(context.Background(), row.ID); err != nil {
		log.Warn().Err(err).Str("name", name).Msg("run cleanup retained for retry")
	}
}

// archiveLog captures a container's combined stdout/stderr and upserts it into
// the sandbox_logs table (keyed by container name), copying run/node/profile
// attribution from the live sandbox row when present. Best-effort: a container
// that is already gone (empty output) is skipped so we never clobber a prior
// good snapshot with an empty one. Read failures are logged and skipped — they
// must not block teardown.
func (s *SandboxService) archiveLog(ctx context.Context, name string) {
	if name == "" {
		return
	}
	out, err := s.mgr.Logs(ctx, name, 5000)
	if err != nil {
		log.Warn().Err(err).Str("name", name).Msg("archive sandbox log: read failed")
		return
	}
	if strings.TrimSpace(out) == "" {
		return
	}
	// Cap stored size to keep the DB lean while preserving the tail (where
	// failures usually surface).
	const maxBytes = 256 * 1024
	if len(out) > maxBytes {
		out = textutil.TruncateTailBytes(out, maxBytes, "…(truncated)…\n")
	}
	var row models.Sandbox
	rec := models.SandboxLog{Name: name, Content: out}
	if err := s.db.Where("name = ?", name).First(&row).Error; err == nil {
		rec.RunID = row.RunID
		rec.NodeID = row.NodeID
		rec.Profile = row.Profile
	}
	if err := s.db.Where(models.SandboxLog{Name: name}).
		Assign(map[string]any{
			"run_id":     rec.RunID,
			"node_id":    rec.NodeID,
			"profile":    rec.Profile,
			"content":    rec.Content,
			"updated_at": time.Now(),
		}).FirstOrCreate(&models.SandboxLog{}).Error; err != nil {
		log.Warn().Err(err).Str("name", name).Msg("archive sandbox log failed")
	}
}

// SandboxViewForRunNode returns the sandbox view for a workflow run's node,
// using the same DB lookup as PreviewService.SandboxForRunNode (latest by
// updated_at). ErrSandboxAbsent means there is no record, or the container is
// not running and the row is not creating or pulling. creating and pulling
// are returned as views so the UI can keep waiting.
func (s *SandboxService) SandboxViewForRunNode(ctx context.Context, runID, nodeID string) (*SandboxView, error) {
	row, err := runNodeSandboxRow(s.db, runID, nodeID)
	if err != nil {
		return nil, ErrSandboxAbsent
	}
	// A sandbox still provisioning (pulling image / creating container) is
	// surfaced as-is so the UI can render pull/create loading instead of absence.
	// Running rows require a live container.
	if row.Status != "creating" && row.Status != "pulling" && s.mgr.Status(ctx, row.Name) != "running" {
		return nil, ErrSandboxAbsent
	}
	v := s.view(ctx, &row)
	return &v, nil
}

// NodeSandboxLog returns the container logs for a workflow run's node sandbox.
// If the sandbox is still live, it fetches fresh logs from the container
// (including a successful empty read → live=true); otherwise it falls back to
// the archived snapshot captured at teardown. Live read failures are returned
// to the caller and must not be disguised as "no log source".
func (s *SandboxService) NodeSandboxLog(ctx context.Context, runID, nodeID string) (content string, live bool, err error) {
	var row models.Sandbox
	var e error
	if nodeID != "" {
		row, e = runNodeSandboxRow(s.db, runID, nodeID)
	} else {
		e = s.db.Where("run_id = ? AND purpose = ?", runID, "run").Order("updated_at desc").First(&row).Error
	}
	if e == nil {
		if s.mgr.Status(ctx, row.Name) == "running" {
			out, lerr := s.mgr.Logs(ctx, row.Name, 5000)
			if lerr != nil {
				return "", false, lerr
			}
			return out, true, nil
		}
	}
	var rec models.SandboxLog
	rq := s.db.Where("run_id = ?", runID)
	if nodeID != "" {
		rq = rq.Where("node_id = ?", nodeID)
	}
	if e := rq.Order("updated_at desc").First(&rec).Error; e != nil {
		if row.Shared && row.Name != "" && s.db.Where("name = ?", row.Name).First(&rec).Error == nil {
			return rec.Content, false, nil
		}
		return "", false, gorm.ErrRecordNotFound
	}
	return rec.Content, false, nil
}

// RunSandboxLogs returns all archived sandbox log snapshots for a run, ordered
// by node then capture time. Used by the run log export to bundle the raw
// container stdout/stderr (docker logs) alongside the agent event logs.
func (s *SandboxService) RunSandboxLogs(runID string) []models.SandboxLog {
	var rows []models.SandboxLog
	if err := s.db.Where("run_id = ?", runID).
		Order("node_id asc, updated_at asc").Find(&rows).Error; err != nil {
		return nil
	}
	return rows
}

// SandboxLogByID returns the container logs for a sandbox by its record id,
// preferring live logs (including successful empty reads) and falling back to
// the archived snapshot. Live read failures are propagated.
func (s *SandboxService) SandboxLogByID(ctx context.Context, id uint) (content string, live bool, err error) {
	row, err := s.Get(id)
	if err != nil {
		return "", false, err
	}
	if s.mgr.Status(ctx, row.Name) == "running" {
		out, lerr := s.mgr.Logs(ctx, row.Name, 5000)
		if lerr != nil {
			return "", false, lerr
		}
		return out, true, nil
	}
	var rec models.SandboxLog
	if e := s.db.Where("name = ?", row.Name).First(&rec).Error; e != nil {
		return "", false, gorm.ErrRecordNotFound
	}
	return rec.Content, false, nil
}
