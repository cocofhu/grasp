package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// BeginRunSandbox records a per-run node sandbox as "creating" BEFORE the (slow)
// gateway provisioning completes, so it shows up in the sandbox list and the
// node's live log as "starting" instead of a 404 during the cold-start window.
// The row uses a placeholder name; RegisterRunSandbox later adopts the real
// gateway id and flips it to "running", or UnregisterRunSandbox removes it on
// failure. Implements runtime.RunSandboxBeginner. Keyed by run+node so a retry
// replaces the previous placeholder row.
func (s *SandboxService) BeginRunSandbox(info runtime.RunSandboxInfo) {
	if info.Name == "" {
		return
	}
	fields := map[string]any{
		"name":          info.Name,
		"profile":       info.Profile,
		"purpose":       "run",
		"status":        "creating",
		"repo_url":      info.RepoURL,
		"run_id":        info.RunID,
		"workflow_id":   info.WorkflowID,
		"workflow_name": info.WorkflowName,
		"node_id":       info.NodeID,
		"home_dir":      info.HomeDir,
		"error":         "",
		"destroy_at":    nil,
		"updated_at":    time.Now(),
	}
	if info.Token != "" {
		fields["token"] = info.Token
	}
	if err := s.db.Where(models.Sandbox{RunID: info.RunID, NodeID: info.NodeID, Purpose: "run"}).
		Assign(fields).FirstOrCreate(&models.Sandbox{}).Error; err != nil {
		log.Warn().Err(err).Str("name", info.Name).Msg("begin run sandbox failed")
	}
	s.mu.Lock()
	s.runActive[info.Name] = true
	s.mu.Unlock()
}

// RegisterRunSandbox records a per-run workflow node sandbox so it appears in
// the sandbox list while the node runs. Implements runtime.SandboxRegistry.
// The row carries no DestroyAt (its lifecycle is owned by the runtime
// provider, not the idle-TTL sweeper) and is marked active so it cannot be
// stopped/destroyed from the UI mid-run. When a "creating" placeholder row was
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
		"destroy_at":       nil,
		"updated_at":       time.Now(),
	}
	if info.Token != "" {
		fields["token"] = info.Token
	}
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

// RetireRunSandbox keeps a finished run's node sandbox alive for a debug TTL
// instead of destroying it immediately: it clears the busy/active flag and sets
// an idle deadline so the sweeper reclaims it later, while it remains browsable
// (terminal / IDE / ACP / container logs) in the sandbox UI. Implements
// runtime.RunSandboxRetirer.
//
// Always best-effort archives container logs before idle retention so a later
// CAPA / AggregateRunFailure path still has sandbox_logs even when the engine
// mis-fires after retire (eliminates noSandboxLog:true with no explanation).
func (s *SandboxService) RetireRunSandbox(name string) {
	if name == "" {
		return
	}
	s.archiveLog(context.Background(), name)
	s.mu.Lock()
	delete(s.runActive, name)
	s.mu.Unlock()
	at := time.Now().Add(s.RunTTL())
	if err := s.db.Model(&models.Sandbox{}).Where("name = ?", name).
		Updates(map[string]any{"status": "running", "destroy_at": &at, "updated_at": time.Now()}).Error; err != nil {
		log.Warn().Err(err).Str("name", name).Msg("retire run sandbox failed")
		return
	}
	log.Info().Str("name", name).Dur("ttl", s.RunTTL()).Msg("run sandbox retired for debugging (idle ttl)")
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
// provider tears down its container. Implements runtime.SandboxRegistry.
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
	if err := s.db.Where("name = ?", name).Delete(&models.Sandbox{}).Error; err != nil {
		log.Warn().Err(err).Str("name", name).Msg("unregister run sandbox failed")
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
// updated_at). Returns an error when no record exists or the container is not
// running.
func (s *SandboxService) SandboxViewForRunNode(ctx context.Context, runID, nodeID string) (*SandboxView, error) {
	var row models.Sandbox
	if err := s.db.Where("run_id = ? AND node_id = ? AND purpose = ?", runID, nodeID, "run").
		Order("updated_at desc").First(&row).Error; err != nil {
		return nil, fmt.Errorf("not found")
	}
	// A sandbox still provisioning (pulling image / creating container) is
	// surfaced as-is so the UI can render pull/create loading instead of 404.
	// Running rows require a live container.
	if row.Status != "creating" && row.Status != "pulling" && s.mgr.Status(ctx, row.Name) != "running" {
		return nil, fmt.Errorf("not found")
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
	q := s.db.Where("run_id = ? AND purpose = ?", runID, "run")
	if nodeID != "" {
		q = q.Where("node_id = ?", nodeID)
	}
	if e := q.Order("updated_at desc").First(&row).Error; e == nil {
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
