package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/solobackup"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const sandboxSaveTimeout = 60 * time.Minute

func (s *SandboxService) SetBackupStore(store solobackup.Store) { s.backups = store }
func (s *SandboxService) SetBackupKeep(keep int) {
	if keep < 1 {
		keep = 1
	}
	s.backupKeep.Store(int64(keep))
}
func (s *SandboxService) keepRecoveryPoints() int {
	if keep := int(s.backupKeep.Load()); keep > 0 {
		return keep
	}
	return 1
}

// Reclaim saves one successful recovery point before releasing any runtime
// resource. All purposes/backends share this state transition and retry path.
func (s *SandboxService) Reclaim(ctx context.Context, id uint) error {
	unlock, err := s.lockSoloRecovery(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	return s.reclaimLocked(ctx, id, false)
}

func (s *SandboxService) reclaimLocked(ctx context.Context, id uint, automatic bool) error {
	row, err := s.Get(id)
	if err != nil {
		return err
	}
	if row.CleanupPending {
		return fmt.Errorf("sandbox is not available for idle recovery")
	}
	if row.RecoveryPhase == "releasing" {
		return s.finishSavedRelease(ctx, row)
	}
	if row.Status == "stopped" {
		return nil
	}
	if row.Status != "running" {
		return fmt.Errorf("sandbox is not available for idle recovery")
	}
	if s.modelBusy(row) {
		s.MarkModelActivity(id)
		return fmt.Errorf("sandbox has an active task or queued request")
	}
	if automatic && s.keepAlive(row) {
		return errors.New("automatic reclaim is disabled for this sandbox")
	}
	if automatic && s.externalBusy(id) {
		return errors.New("sandbox has an open external connection")
	}
	if automatic && !s.idleDue(row, time.Now()) {
		return errors.New("sandbox has recent model activity")
	}
	idleSince := s.modelIdleSince(row)
	sb, err := s.soloHandle(ctx, id)
	if err != nil {
		return err
	}
	// Turns can also start inside the guest (shared or reattached sessions).
	// A failed probe keeps the original guest intact.
	busy, err := sb.RuntimeActivity(ctx)
	if err != nil {
		return fmt.Errorf("check guest activity: %w", err)
	}
	if busy {
		s.MarkModelActivity(id)
		return errors.New("sandbox still has a running model turn")
	}
	kind, err := s.mgr.RecoveryKind(ctx, row.Name)
	if err != nil {
		return err
	}
	if automatic && kind != "snapshot" {
		return errors.New("automatic reclaim only releases snapshot environments")
	}
	if kind == "files" && s.backups == nil {
		return errors.New("workspace backup storage is unavailable")
	}
	res := s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ? AND cleanup_pending = ?", id, "running", false).
		Updates(map[string]any{"status": "saving", "recovery_kind": kind, "recovery_phase": "archiving", "error": ""})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errors.New("sandbox changed before it could be saved")
	}
	row.RecoveryKind, row.RecoveryPhase = kind, "archiving"
	ctx, cancel := context.WithTimeout(ctx, sandboxSaveTimeout)
	defer cancel()
	if kind == "files" {
		err = s.saveWorkspace(ctx, row, sb)
	}
	if err == nil {
		// Archiving can take minutes. A turn or keep-alive choice made during
		// the save wins over releasing the workload.
		err = s.recheckBeforeRelease(ctx, row, sb, idleSince, automatic)
	}
	if err == nil {
		// A successful archive from an older attempt is not permission to stop
		// today's guest. Record this attempt's verified release boundary first.
		err = s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ?", id, "saving").Update("recovery_phase", "releasing").Error
		if err == nil {
			row.RecoveryPhase = "releasing"
		}
	}
	if err == nil {
		// Stop commits the gateway's backend-specific recovery operation. E2B
		// uses its provider API contract, not a physical storage-reclaim receipt.
		// The gateway retains the specification and provider restore-point IDs.
		err = s.mgr.Stop(ctx, row.Name, s.keepRecoveryPoints())
	}
	if err != nil {
		s.recordReclaimFailure(ctx, row, err)
		return err
	}
	s.teardownLive(id)
	now := time.Now()
	if err = s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ?", id, "saving").Updates(map[string]any{
		"status": "stopped", "recovery_kind": kind, "recovery_phase": "", "error": "", "updated_at": now,
	}).Error; err != nil {
		return err
	}
	row.Status, row.RecoveryKind = "stopped", kind
	if row.Purpose == SandboxPurposeSolo {
		// Idle reclamation keeps the conversation open: its next message wakes
		// the saved environment. A manual stop changes the session explicitly.
		if !automatic {
			s.db.Model(&models.SoloSession{}).Where("sandbox_id = ? AND status IN ?", id,
				[]string{models.SoloStatusRunning, models.SoloStatusStopping}).Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""})
		}
		s.invokeSoloPausedHook(id)
	}
	return s.pruneRecoveryPoints(ctx, row, s.keepRecoveryPoints())
}

func (s *SandboxService) saveWorkspace(ctx context.Context, row *models.Sandbox, sb *sandbox.Sandbox) error {
	key := fmt.Sprintf("sandboxes/%d/%s.tar.gz", row.ID, uuid.NewString())
	// Write the cleanup key BEFORE creating the object. A failed database
	// commit must never leave an untracked archive in remote storage.
	point := models.SandboxRecoveryPoint{Store: s.backups.Kind(), Key: key, CreatedAt: time.Now(), Deleting: true}
	row.RecoveryPoints = append([]models.SandboxRecoveryPoint{point}, row.RecoveryPoints...)
	if err := s.persistRecoveryPoints(row); err != nil {
		return err
	}
	rc, err := sb.ArchiveWorkspace(ctx)
	if err != nil {
		return err
	}
	obj, err := s.backups.Put(ctx, key, rc)
	_ = rc.Close()
	if err != nil {
		return err
	}
	point.SHA256, point.Size = obj.SHA256, obj.Size
	row.RecoveryPoints[0] = point
	if err := s.persistRecoveryPoints(row); err != nil {
		return err
	}
	if err := s.verifyRecoveryPoint(ctx, point); err != nil {
		return err
	}
	row.RecoveryPoints[0].Deleting = false
	if err := s.persistRecoveryPoints(row); err != nil {
		return err
	}
	if row.Purpose == SandboxPurposeSolo {
		if err := s.db.Model(&models.SoloSession{}).Where("sandbox_id = ?", row.ID).Updates(map[string]any{"backup_size": point.Size, "backup_at": point.CreatedAt}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *SandboxService) verifyRecoveryPoint(ctx context.Context, p models.SandboxRecoveryPoint) error {
	if s.backups == nil || p.Store != s.backups.Kind() {
		return errors.New("recovery point storage is unavailable")
	}
	rc, err := s.backups.Open(ctx, p.Key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return err
	}
	if n != p.Size || hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return errors.New("stored workspace archive failed checksum verification")
	}
	return nil
}

func (s *SandboxService) persistRecoveryPoints(row *models.Sandbox) error {
	return s.db.Model(&models.Sandbox{ID: row.ID}).Select("recovery_points").Updates(&models.Sandbox{RecoveryPoints: row.RecoveryPoints}).Error
}

func (s *SandboxService) pruneRecoveryPoints(ctx context.Context, row *models.Sandbox, keep int) error {
	successful := 0
	for i := range row.RecoveryPoints {
		p := &row.RecoveryPoints[i]
		if !p.Deleting {
			successful++
			if successful > keep {
				p.Deleting = true
			}
		}
	}
	if err := s.persistRecoveryPoints(row); err != nil {
		return err
	}
	remaining := make([]models.SandboxRecoveryPoint, 0, len(row.RecoveryPoints))
	var failures []error
	for _, point := range row.RecoveryPoints {
		if !point.Deleting {
			remaining = append(remaining, point)
			continue
		}
		if s.backups == nil || point.Store != s.backups.Kind() {
			remaining = append(remaining, point)
			failures = append(failures, errors.New("recovery point store is not configured"))
			continue
		}
		if err := s.backups.Delete(ctx, point.Key); err != nil {
			remaining = append(remaining, point)
			failures = append(failures, err)
		}
	}
	row.RecoveryPoints = remaining
	if err := s.persistRecoveryPoints(row); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func latestRecoveryPoint(row *models.Sandbox) (models.SandboxRecoveryPoint, bool) {
	for _, p := range row.RecoveryPoints {
		if !p.Deleting {
			return p, true
		}
	}
	return models.SandboxRecoveryPoint{}, false
}

func (s *SandboxService) recordReclaimFailure(ctx context.Context, row *models.Sandbox, cause error) {
	status, probeErr := s.mgr.ProbeStatus(ctx, row.Name)
	next := "saving"
	if probeErr == nil && status == "running" && row.RecoveryPhase != "releasing" {
		next = "running"
	}
	if probeErr == nil && row.RecoveryPhase == "releasing" && workloadReleasedStatus(status) {
		next = "stopped"
		s.teardownLive(row.ID)
	}
	// Preserve the incomplete operation even if the provider confirms compute
	// has stopped. Releasing quota is not a claim that its backup cleanup or
	// physical storage reclamation has completed; wake must finish this intent.
	_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND recovery_phase = ? AND cleanup_pending = ?", row.ID, row.RecoveryPhase, false).Updates(map[string]any{
		"status": next, "error": "保存或释放未完成，可重试：" + truncErr(cause),
	}).Error
}

// EnsureRunning restores the newest successful point before publishing a
// running environment. It never falls back to an older point on corruption.
func (s *SandboxService) EnsureRunning(ctx context.Context, id uint) (*sandbox.Sandbox, bool, error) {
	unlock, err := s.lockSoloRecovery(ctx, id)
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	return s.ensureRunningLocked(ctx, id)
}

func (s *SandboxService) ensureRunningLocked(ctx context.Context, id uint) (*sandbox.Sandbox, bool, error) {
	row, err := s.Get(id)
	if err != nil {
		return nil, false, err
	}
	if row.CleanupPending || row.Status == "deleting" {
		return nil, false, errors.New("sandbox cancellation cleanup is in progress")
	}
	if row.RecoveryPhase == "releasing" {
		if err := s.finishSavedRelease(ctx, row); err != nil {
			return nil, false, fmt.Errorf("complete saved sandbox release: %w", err)
		}
		row.Status, row.RecoveryPhase = "stopped", ""
	}
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	if err != nil {
		return nil, false, err
	}
	if row.Status == "saving" && status == "running" {
		// A failed/interrupted save never replaced this original workload. Its
		// files may have changed since the archive; do not extract that archive
		// over the still-running guest when the status endpoint recovers.
		if err := s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ?", id, "saving").Updates(map[string]any{"status": "running", "error": ""}).Error; err != nil {
			return nil, false, err
		}
		row.Status = "running"
	}
	if row.Status == "running" && status == "running" {
		sb, err := s.mgr.AttachNoEnsure(ctx, row.Name)
		if err == nil {
			sb.Password = row.Token
			s.TouchActivity(id)
		}
		return sb, true, err
	}
	if status == "not_found" {
		if _, saved := latestRecoveryPoint(row); row.RecoveryKind != "files" || !saved {
			return nil, false, ErrSoloSandboxGone
		}
		// A failed cold Create can leave the durable gateway record/spec but no
		// workload. The latest file archive can safely retry StartPrepared; a
		// truly missing gateway record still fails its required Get below.
	}
	if row.Purpose == SandboxPurposeSolo && (status == "stopped" || status == "exited") {
		s.invokeSoloPausedHook(row.ID)
	}
	kind := row.RecoveryKind
	if kind == "" {
		kind, err = s.mgr.RecoveryKind(ctx, row.Name)
		if err != nil {
			return nil, false, err
		}
	}
	point, hasPoint := latestRecoveryPoint(row)
	if row.RecoveryKind == "files" && !hasPoint {
		return nil, false, errors.New("saved workspace recovery point is missing")
	}
	// A provider-paused snapshot resumes through Start's native recovery
	// contract. Repeating Stop here can create another snapshot or prune history;
	// only the persisted releasing intent above authorizes a Stop retry.
	if kind == "files" && hasPoint {
		if err := s.verifyRecoveryPoint(ctx, point); err != nil {
			return nil, false, err
		}
	}
	var prepared *sandbox.Spec
	if kind == "files" && hasPoint && status != "running" {
		spec, err := s.prepareRecoveryInputs(row)
		if err != nil {
			return nil, false, fmt.Errorf("prepare saved sandbox configuration: %w", err)
		}
		prepared = &spec
		defer func() { _ = os.RemoveAll(spec.ConfigHome) }()
	}
	release, err := s.reserveSandbox(id)
	if err != nil {
		return nil, false, err
	}
	keepReservation := false
	defer func() { release(keepReservation) }()
	s.teardownLive(id)
	// From this point an external start may allocate a guest even if its reply
	// or our final DB commit fails. Only a confirmed stopped observation below
	// may release the slot; reservation rollback is no longer safe.
	keepReservation = true
	var sb *sandbox.Sandbox
	if status == "running" {
		sb, err = s.mgr.AttachNoEnsure(ctx, row.Name)
	} else if prepared != nil {
		sb, err = s.mgr.ResumePrepared(ctx, row.Name, *prepared)
	} else {
		sb, err = s.mgr.Resume(ctx, row.Name)
	}
	if err == nil && kind == "files" && hasPoint {
		err = s.restoreWorkspace(ctx, sb, point)
	}
	if err != nil {
		observed, probeErr := s.mgr.ProbeStatus(ctx, row.Name)
		failedStatus := "error"
		if probeErr == nil && (observed == "stopped" || observed == "exited") {
			failedStatus = "stopped"
		}
		_ = s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"status": failedStatus, "error": "恢复未完成，恢复点仍保留：" + truncErr(err)}).Error
		return nil, false, err
	}
	sb.Password = row.Token
	now := time.Now()
	if err = s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"status": "running", "error": "", "last_active_at": now, "model_active_at": now,
		"host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort,
		"model_binding_fingerprint": row.ModelBindingFingerprint, "git_bindings_fingerprint": row.GitBindingsFingerprint,
	}).Error; err != nil {
		return nil, false, err
	}
	return sb, kind == "snapshot", nil
}

func (s *SandboxService) restoreWorkspace(ctx context.Context, sb *sandbox.Sandbox, point models.SandboxRecoveryPoint) error {
	rc, err := s.backups.Open(ctx, point.Key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	if err := sb.RestoreWorkspace(ctx, io.TeeReader(rc, h)); err != nil {
		return err
	}
	if _, err := io.Copy(h, rc); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != point.SHA256 {
		return errors.New("workspace archive checksum mismatch")
	}
	return nil
}

func (s *SandboxService) idleDue(row *models.Sandbox, now time.Time) bool {
	return !s.reclaimIdleSince(row).Add(s.IdleTTL()).After(now)
}

// modelBusy reports control-plane turns and queued requests.
func (s *SandboxService) modelBusy(row *models.Sandbox) bool {
	return s.isBusyRow(row) || s.soloBusy != nil && s.soloBusy(row.ID)
}

func (s *SandboxService) recheckBeforeRelease(ctx context.Context, row *models.Sandbox, sb *sandbox.Sandbox, idleSince time.Time, automatic bool) error {
	fresh, err := s.Get(row.ID)
	if err != nil {
		return err
	}
	if s.modelBusy(fresh) || s.modelIdleSince(fresh).After(idleSince) {
		return errors.New("a model turn started while saving")
	}
	if automatic && s.keepAlive(fresh) {
		return errors.New("automatic reclaim was disabled while saving")
	}
	if automatic && s.externalBusy(fresh.ID) {
		return errors.New("an external connection opened while saving")
	}
	busy, err := sb.RuntimeActivity(ctx)
	if err == nil && busy {
		s.MarkModelActivity(row.ID)
		err = errors.New("a model turn started while saving")
	}
	return err
}

// RetryRecoveryCleanup handles only cleanup already authorized by cancellation
// or successful retention pruning; it never creates periodic backups.
func (s *SandboxService) RetryRecoveryCleanup(ctx context.Context) {
	s.retryLegacyBackupCleanup(ctx)
	var rows []models.Sandbox
	if s.db.Find(&rows).Error != nil {
		return
	}
	cancelledRuns := make(map[string]bool)
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if row.Purpose == "run" && !row.CleanupPending {
			if cancelledRuns[row.RunID] {
				continue
			}
			var run models.Run
			if s.db.Select("status").First(&run, "id = ?", row.RunID).Error == nil && run.Status == "cancelled" {
				_ = s.CancelRunSandboxes(ctx, row.RunID)
				cancelledRuns[row.RunID] = true
				continue
			}
		}
		if row.CleanupPending {
			_ = s.Destroy(ctx, row.ID)
			continue
		}
		if row.Purpose == SandboxPurposeSolo && row.Status == "stopped" && row.RecoveryPhase == "" {
			// A crash or failed archive prune can happen after the completed
			// recovery operation but before the manual-stop session update.
			res := s.db.Model(&models.SoloSession{}).Where("sandbox_id = ? AND status = ?", row.ID, models.SoloStatusStopping).
				Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""})
			if res.Error == nil && res.RowsAffected > 0 {
				s.invokeSoloPausedHook(row.ID)
			}
		}
		if row.Purpose == SandboxPurposeSolo && row.Status == "running" {
			var session models.SoloSession
			if s.db.Where("sandbox_id = ? AND status = ?", row.ID, models.SoloStatusStopping).First(&session).Error == nil {
				_ = s.Reclaim(ctx, row.ID)
				continue
			}
		}
		if row.Status == "saving" || row.Status == "resuming" || row.RecoveryPhase == "releasing" {
			s.reconcileRecoveryState(ctx, &row)
			continue
		}
		if len(row.RecoveryPoints) == 0 {
			continue
		}
		unlock, err := s.lockSoloRecovery(ctx, row.ID)
		if err != nil {
			return
		}
		if fresh, err := s.Get(row.ID); err == nil {
			_ = s.pruneRecoveryPoints(ctx, fresh, s.keepRecoveryPoints())
		}
		unlock()
	}
}

// CancelSandbox records deletion intent before attempting external cleanup.
// Its retained row is the durable retry queue if the gateway/store is down.
func (s *SandboxService) CancelSandbox(ctx context.Context, id uint) error {
	if id == 0 {
		return nil
	}
	res := s.db.Model(&models.Sandbox{}).Where("id = ?", id).Update("cleanup_pending", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}
	s.Cancel(id)
	err := s.Destroy(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// reconcileRecoveryState handles a process exit between the gateway operation
// and its local commit. It only observes; it never replays a destructive save.
func (s *SandboxService) reconcileRecoveryState(ctx context.Context, row *models.Sandbox) {
	unlock, err := s.lockSoloRecovery(ctx, row.ID)
	if err != nil {
		return
	}
	defer unlock()
	fresh, err := s.Get(row.ID)
	if err != nil || fresh.Status != row.Status {
		return
	}
	if fresh.RecoveryPhase == "releasing" {
		if err := s.finishSavedRelease(ctx, fresh); err != nil {
			return
		}
		if fresh.Purpose == SandboxPurposeSolo {
			s.db.Model(&models.SoloSession{}).Where("sandbox_id = ? AND status = ?", row.ID, models.SoloStatusStopping).
				Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""})
			s.invokeSoloPausedHook(row.ID)
		}
		return
	}
	observed, err := s.mgr.ProbeStatus(ctx, row.Name)
	if err != nil {
		return
	}
	kind, err := s.mgr.RecoveryKind(ctx, row.Name)
	if err != nil {
		return
	}
	status := "error"
	if observed == "stopped" || observed == "exited" {
		if fresh.Status == "saving" && fresh.RecoveryKind == "files" {
			return // no durable release intent: an older archive cannot authorize deletion
		}
		if kind == "files" {
			if _, ok := latestRecoveryPoint(fresh); !ok {
				return
			}
			if err := s.mgr.Stop(ctx, row.Name, s.keepRecoveryPoints()); err != nil {
				return
			}
		}
		// Standard E2B can pause independently of Grasp. Observe that state
		// without manufacturing a new recovery point; Start owns readiness.
		status = "stopped"
	}
	if row.Status == "saving" && observed == "running" {
		status = "running"
	}
	if row.Status == "saving" && status == "error" {
		return // uncertain archive state stays retryable and continues to occupy capacity
	}
	if row.Status == "resuming" && kind == "snapshot" && observed == "running" {
		status = "running"
	}
	if err := s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ?", row.ID, row.Status).Updates(map[string]any{"status": status, "recovery_kind": kind, "recovery_phase": "", "error": "服务重启中断操作，已核对环境状态"}).Error; err != nil {
		return
	}
	if row.Purpose == SandboxPurposeSolo {
		sessionStatus := models.SoloStatusStopped
		if status == "running" {
			sessionStatus = models.SoloStatusRunning
		}
		s.db.Model(&models.SoloSession{}).Where("sandbox_id = ? AND status IN ?", row.ID, []string{models.SoloStatusStarting, models.SoloStatusStopping}).Update("status", sessionStatus)
		s.invokeSoloPausedHook(row.ID)
	}
}

// finishSavedRelease retries only a durable release intent. In particular a
// gateway reporting pending may be halfway through deleting a Deployment/PVC;
// it must finish that same Stop before either freeing quota or starting again.
func (s *SandboxService) finishSavedRelease(ctx context.Context, row *models.Sandbox) error {
	if row.RecoveryKind == "files" {
		point, ok := latestRecoveryPoint(row)
		if !ok {
			return errors.New("release recovery point is missing")
		}
		if err := s.verifyRecoveryPoint(ctx, point); err != nil {
			return err
		}
	} else if row.RecoveryKind != "snapshot" {
		return errors.New("release recovery kind is missing")
	}
	if err := s.mgr.Stop(ctx, row.Name, s.keepRecoveryPoints()); err != nil {
		s.recordReclaimFailure(ctx, row, err)
		return err
	}
	if err := s.db.Model(&models.Sandbox{}).Where("id = ? AND recovery_phase = ?", row.ID, "releasing").Updates(map[string]any{"status": "stopped", "recovery_phase": "", "error": ""}).Error; err != nil {
		return err
	}
	s.teardownLive(row.ID)
	return s.pruneRecoveryPoints(ctx, row, s.keepRecoveryPoints())
}
