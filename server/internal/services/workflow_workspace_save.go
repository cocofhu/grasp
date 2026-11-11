package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

const workspaceSavePausePrefix = "workspace_checkpoint:"

// WorkspaceSave is what a run paused between nodes is really doing: saving the
// node's workspace files, or stuck because that save failed. The run status
// stays "paused" so the sandbox and the sealed node outcome survive a retry.
type WorkspaceSave struct {
	CheckpointID string `json:"checkpointId"`
	NodeID       string `json:"nodeId"`
	// State: saving | failed.
	State string `json:"state"`
	// Reason is the checkpoint reason (snapshot_failed, binding_unavailable…);
	// Detail is the capture job's stable error code (git_unsupported…).
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Action is how a user moves a failed save on: "retry" captures again via
	// run resume, "reconcile" re-reads an ambiguous receipt; empty means neither.
	Action string `json:"action,omitempty"`
	// OutcomeSealed is false when the node result is not sealed yet. Failure
	// retry must stay disabled; delete is still allowed for a failed save.
	OutcomeSealed bool `json:"outcomeSealed"`
}

// WorkspaceSaveCheckpointID reports the checkpoint a run is paused on, if any.
func WorkspaceSaveCheckpointID(run models.Run) string {
	if run.Status != "paused" || !strings.HasPrefix(run.PauseFromStatus, workspaceSavePausePrefix) {
		return ""
	}
	return strings.TrimPrefix(run.PauseFromStatus, workspaceSavePausePrefix)
}

// WorkspaceSaves loads the save state of every run paused on a checkpoint.
// Read errors degrade to "no save info" so list pages never fail on it.
func WorkspaceSaves(db *gorm.DB, runs []models.Run) map[string]*WorkspaceSave {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		if id := WorkspaceSaveCheckpointID(run); id != "" {
			ids = append(ids, id)
		}
	}
	out := map[string]*WorkspaceSave{}
	if db == nil || len(ids) == 0 {
		return out
	}
	var rows []models.WorkflowVisitCheckpoint
	if db.Where("id IN ?", ids).Find(&rows).Error != nil {
		return out
	}
	snapshots := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.SnapshotID != "" {
			snapshots = append(snapshots, row.SnapshotID)
		}
	}
	details := map[string]string{}
	if len(snapshots) > 0 {
		var jobs []struct{ SnapshotID, Error string }
		if db.Table("workspace_snapshots").Select("workspace_snapshots.id AS snapshot_id, workspace_snapshot_jobs.error AS error").Joins("JOIN workspace_snapshot_jobs ON workspace_snapshot_jobs.id = workspace_snapshots.job_id").Where("workspace_snapshots.id IN ?", snapshots).Scan(&jobs).Error == nil {
			for _, job := range jobs {
				details[job.SnapshotID] = job.Error
			}
		}
	}
	for _, row := range rows {
		save := &WorkspaceSave{CheckpointID: row.ID, NodeID: row.NodeID, State: "saving", OutcomeSealed: row.RouteState == "sealed"}
		if CheckpointSaveFailed(row) {
			save.State, save.Reason = "failed", row.Reason
			switch row.FileState {
			case "blocked", "unknown", "unavailable":
				save.Action = saveAction(row)
				save.Detail = details[row.SnapshotID]
			}
		}
		out[row.RunID] = save
	}
	return out
}

// CheckpointSaveFailed reports the save state the run page shows as 保存失败.
// A paused run on any other checkpoint state is still saving.
func CheckpointSaveFailed(row models.WorkflowVisitCheckpoint) bool {
	switch row.FileState {
	case "blocked", "unknown", "unavailable":
		return true
	default:
		return row.RouteState == "unknown"
	}
}

// WorkspaceSaveFailed reports a paused run whose workspace save has already failed.
func WorkspaceSaveFailed(db *gorm.DB, run models.Run) bool {
	row, ok := loadWorkspaceSaveCheckpoint(db, run)
	return ok && CheckpointSaveFailed(row)
}

// WorkspaceSaveInProgress reports a paused run still saving its workspace.
func WorkspaceSaveInProgress(db *gorm.DB, run models.Run) bool {
	row, ok := loadWorkspaceSaveCheckpoint(db, run)
	return ok && !CheckpointSaveFailed(row)
}

func loadWorkspaceSaveCheckpoint(db *gorm.DB, run models.Run) (models.WorkflowVisitCheckpoint, bool) {
	id := WorkspaceSaveCheckpointID(run)
	if db == nil || id == "" {
		return models.WorkflowVisitCheckpoint{}, false
	}
	var row models.WorkflowVisitCheckpoint
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		return models.WorkflowVisitCheckpoint{}, false
	}
	return row, true
}

// WorkflowCheckpointOpenSQL matches a checkpoint that still blocks admission.
// Cancelled means the save was ended without counting as a successful apply,
// so it must not keep the run from being deleted or continued.
func WorkflowCheckpointOpenSQL(column string) string {
	if column == "" {
		column = "route_state"
	}
	return column + " NOT IN ('applied', 'cancelled')"
}

// EndOpenWorkspaceSavesTx stops every unfinished workspace save for a run.
// The checkpoint stays cancelled rather than applied, so the save is not
// recorded as successful and the capture loop will not schedule it again.
func EndOpenWorkspaceSavesTx(tx *gorm.DB, runID string) error {
	return tx.Model(&models.WorkflowVisitCheckpoint{}).
		Where("run_id = ? AND "+WorkflowCheckpointOpenSQL("route_state"), runID).
		Update("route_state", "cancelled").Error
}

func saveAction(row models.WorkflowVisitCheckpoint) string {
	switch {
	case retryableCapture(row):
		return "retry"
	case row.RouteState == "sealed" && row.FileState == "unknown" && row.SnapshotID != "" && row.CaptureStartedAt != nil:
		return "reconcile"
	}
	return ""
}
