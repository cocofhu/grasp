package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"
)

func (s *SandboxService) lockLifecycle(id string) func() {
	lock, _ := s.lifecycleLocks.LoadOrStore(id, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// A non-zero-size token identifies one async attempt across the lock handoff.
// Superseded workers must neither run nor clear a newer attempt's marker.
type provisioningAttempt byte

func (s *SandboxService) beginProvisioning(id string) *provisioningAttempt {
	attempt := new(provisioningAttempt)
	s.provisioning.Store(id, attempt)
	return attempt
}

func (s *SandboxService) ownsProvisioning(id string, attempt *provisioningAttempt) bool {
	current, ok := s.provisioning.Load(id)
	return ok && current == attempt
}

func (s *SandboxService) finishProvisioning(id string, attempt *provisioningAttempt) {
	s.provisioning.CompareAndDelete(id, attempt)
}

func saveSpec(sb *models.Sandbox, spec driver.Spec) error {
	spec.Env = nil // credentials already have an authoritative column
	data, err := json.Marshal(spec)
	if err == nil {
		sb.SpecJSON = string(data)
	}
	return err
}

// WorkspaceDir returns the saved workload path, independent of later defaults
// or tool configuration changes. Older records can still carry it in Env.
func (s *SandboxService) WorkspaceDir(sb *models.Sandbox) string {
	var spec driver.Spec
	if sb.SpecJSON != "" && json.Unmarshal([]byte(sb.SpecJSON), &spec) == nil && spec.WorkspaceDir != "" {
		return spec.WorkspaceDir
	}
	if path := sb.Env()["WORKSPACE_DIR"]; path != "" {
		return path
	}
	return s.cfg.WorkspaceDir
}

func (s *SandboxService) restoreSpec(ctx context.Context, sb *models.Sandbox) (driver.Spec, error) {
	var spec driver.Spec
	if sb.SpecJSON != "" {
		if err := json.Unmarshal([]byte(sb.SpecJSON), &spec); err != nil {
			return spec, err
		}
	} else {
		// Adopt the old record while its workload still exists, before deleting
		// anything. Docker must inspect its binds; a failed read is not permission
		// to recreate the sandbox with a guessed mount layout.
		spec = driver.Spec{ID: sb.ID, Image: sb.Image, WorkspaceDir: s.cfg.WorkspaceDir,
			Ports: append([]int(nil), s.cfg.Ports...), InternalPorts: append([]int(nil), s.cfg.InternalPorts...),
			Resources: driver.Resources{CPUCores: sb.CPUCores, MemoryMB: sb.MemoryMB, DiskGi: sb.DiskGi},
			Env:       sb.Env(), Labels: sb.Labels()}
		if path := spec.Env["WORKSPACE_DIR"]; path != "" {
			spec.WorkspaceDir = path
		}
		for p := range sb.Endpoints() {
			spec.Ports = mergePorts(spec.Ports, []int{p})
		}
		spec.Ports = subtractPorts(spec.Ports, spec.InternalPorts)
		if reader, ok := s.drv.(driver.SpecReader); ok {
			var err error
			spec, err = reader.CaptureSpec(ctx, spec)
			if err != nil {
				return spec, fmt.Errorf("adopt sandbox recreation inputs: %w", err)
			}
			sb.SetEnv(spec.Env)
		}
		if err := saveSpec(sb, spec); err != nil {
			return spec, err
		}
		if err := s.store.Save(ctx, sb); err != nil {
			return spec, err
		}
	}
	spec.Env, spec.Labels = sb.Env(), sb.Labels()
	return spec, nil
}

func applyColdRestoreOptions(spec *driver.Spec, opts driver.StartOptions) error {
	if workspace, exists := opts.RestoreEnv["WORKSPACE_DIR"]; exists && workspace != spec.WorkspaceDir {
		return fmt.Errorf("cold restore cannot change the workspace directory")
	}
	if opts.RuntimeURL != "" {
		spec.Env = mergeEnv(spec.Env, map[string]string{
			"GRASP_RUNTIME_URL": opts.RuntimeURL, "SANDBOX_INJECT_HEADERS": opts.RuntimeHeaders,
			"SANDBOX_INJECT": "",
		})
	}
	if opts.RestoreEnv != nil || len(opts.ReplaceEnvKeys) > 0 {
		spec.Env = mergeEnv(spec.Env, nil)
		for _, key := range opts.ReplaceEnvKeys {
			delete(spec.Env, key)
		}
		for key, value := range opts.RestoreEnv {
			spec.Env[key] = value
		}
	}
	if opts.RuntimeURL != "" || opts.RestoreEnv != nil || len(opts.ReplaceEnvKeys) > 0 {
		// URL config injection is regenerated above. Keeping its old Config
		// would let drivers overwrite fresh URLs with expired grants. Native
		// host bindings remain part of the original workload specification.
		if spec.Config != nil && spec.Config.HostPath == "" {
			spec.Config = nil
		}
	}
	return nil
}

// StartPrepared resumes a snapshot or recreates a released workload from the
// persisted specification. Grasp restores files before admitting new work.
func (s *SandboxService) StartPrepared(ctx context.Context, id string, opts driver.StartOptions) error {
	unlock := s.lockLifecycle(id)
	defer unlock()
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if sb.Status == models.StatusDeleting || sb.Status == models.StatusStopping {
		return fmt.Errorf("sandbox cleanup is in progress")
	}
	if sb.Status == models.StatusCreating || sb.Status == models.StatusPulling {
		return nil
	}
	if sb.WorkloadReleased {
		spec, err := s.restoreSpec(ctx, sb)
		if err != nil {
			return err
		}
		if err := applyColdRestoreOptions(&spec, opts); err != nil {
			return err
		}
		sb.SetEnv(spec.Env)
		if err := saveSpec(sb, spec); err != nil {
			return err
		}
		sb.Status, sb.Error = models.StatusCreating, ""
		attempt := s.beginProvisioning(id)
		if err := s.store.Save(ctx, sb); err != nil {
			s.finishProvisioning(id, attempt)
			return err
		}
		go s.recreate(id, attempt)
		return nil
	}
	// Existing retained workloads can still contain the sole copy of their
	// files. Repair their bootstrap in place instead of discarding that data.
	if opts.RuntimeURL != "" || opts.BootstrapScript != "" {
		if sb.Env()["GRASP_RUNTIME_URL"] == "" {
			return fmt.Errorf("sandbox does not use the runtime bootstrap")
		}
		if p, ok := s.drv.(driver.StartPreparer); ok {
			if err := p.PrepareStart(ctx, id, opts); err != nil {
				return err
			}
			env := sb.Env()
			env["GRASP_RUNTIME_URL"], env["SANDBOX_INJECT_HEADERS"], env["SANDBOX_INJECT"] = opts.RuntimeURL, opts.RuntimeHeaders, ""
			sb.SetEnv(env)
			if err := s.store.Save(ctx, sb); err != nil {
				return err
			}
		}
	}
	if err := s.drv.Start(ctx, id); err != nil {
		return err
	}
	sb.Status = models.StatusCreating
	attempt := s.beginProvisioning(id)
	if err := s.store.Save(ctx, sb); err != nil {
		s.finishProvisioning(id, attempt)
		return err
	}
	go func() {
		unlock := s.lockLifecycle(id)
		defer unlock()
		defer s.finishProvisioning(id, attempt)
		if !s.ownsProvisioning(id, attempt) {
			return
		}
		current, err := s.store.Get(context.Background(), id)
		if err != nil || current.Status != models.StatusCreating {
			return
		}
		s.finalize(id)
	}()
	return nil
}

func (s *SandboxService) recreate(id string, attempt *provisioningAttempt) {
	unlock := s.lockLifecycle(id)
	defer unlock()
	defer s.finishProvisioning(id, attempt)
	if !s.ownsProvisioning(id, attempt) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.FinalizeTimeout)
	defer cancel()
	sb, err := s.store.Get(ctx, id)
	if err != nil || sb.Status != models.StatusCreating {
		return
	}
	spec, err := s.restoreSpec(ctx, sb)
	// A failed previous recreation may have left partial exclusive resources.
	// The durable recovery point remains authoritative until restoration succeeds.
	if err == nil {
		err = s.drv.Destroy(ctx, id)
	}
	if err == nil {
		err = s.ensureImageBeforeCreate(ctx, id, spec.Image)
	}
	if err == nil {
		var h *driver.Handle
		h, err = s.drv.Create(ctx, spec)
		if err == nil {
			sb.Name, sb.Namespace, sb.WorkloadReleased = h.Name, h.Namespace, false
			sb.SetEndpoints(h.Endpoints)
			err = s.store.Save(ctx, sb)
		}
	}
	if err != nil {
		s.fail(id, fmt.Sprintf("restore workload: %v", err))
		return
	}
	s.finalize(id)
}

// Stop releases runtime resources after the caller has committed its file
// archive, or waits for an E2B snapshot to become durable before reporting success.
func (s *SandboxService) Stop(ctx context.Context, id string, keep int) error {
	if keep < 1 {
		return fmt.Errorf("backupKeep must be positive")
	}
	unlock := s.lockLifecycle(id)
	defer unlock()
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if sb.Status == models.StatusDeleting {
		return fmt.Errorf("sandbox deletion is in progress")
	}
	if sb.Status == models.StatusStopped && sb.WorkloadReleased {
		return nil
	}
	if _, ok := s.drv.(driver.SnapshotPauser); !ok {
		if _, err := s.restoreSpec(ctx, sb); err != nil {
			return err
		}
	}
	sb.Status = models.StatusStopping
	if err := s.store.Save(ctx, sb); err != nil {
		return err
	}
	if pauser, ok := s.drv.(driver.SnapshotPauser); ok {
		err = pauser.PauseSnapshot(ctx, id, keep)
	} else {
		err = s.drv.Destroy(ctx, id)
	}
	if err != nil {
		sb.Error = err.Error()
		if saveErr := s.store.Save(context.Background(), sb); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	sb.Status, sb.Error = models.StatusStopped, ""
	_, snapshot := s.drv.(driver.SnapshotPauser)
	sb.WorkloadReleased = !snapshot
	return s.store.Save(context.Background(), sb)
}

// Destroy retains an explicit cleanup intent until the driver confirms success.
// A retry therefore cannot lose the only identifier needed to reclaim resources.
func (s *SandboxService) Destroy(ctx context.Context, idOrName string) error {
	sb, err := s.lookup(ctx, idOrName)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	unlock := s.lockLifecycle(sb.ID)
	defer unlock()
	sb, err = s.store.Get(ctx, sb.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// A failed cold recreation may have allocated resources even though its
	// release flag still preserves eligibility to retry from the saved spec.
	// Only a completed stop or an earlier confirmed deletion is exit proof.
	if sb.Status != models.StatusStopped && sb.Status != models.StatusDeleting {
		sb.WorkloadReleased = false
	}
	sb.Status = models.StatusDeleting
	if err := s.store.Save(ctx, sb); err != nil {
		return err
	}
	bg, cancel := context.WithTimeout(context.Background(), destroyDriverTimeout)
	defer cancel()
	if err := s.drv.Destroy(bg, sb.ID); err != nil {
		sb.Error = err.Error()
		var pending *driver.CleanupPendingError
		if errors.As(err, &pending) && pending.WorkloadReleased {
			sb.WorkloadReleased = true
		}
		if saveErr := s.store.Save(bg, sb); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	return s.store.Delete(bg, sb.ID)
}

func (s *SandboxService) retryDeletes(ctx context.Context) {
	rows, err := s.store.List(ctx, ListFilter{})
	if err != nil {
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if row.Status == models.StatusDeleting {
			if err := s.Destroy(ctx, row.ID); err != nil {
				log.Warn().Err(err).Str("sandbox_id", row.ID).Msg("sandbox cleanup retry failed")
			}
		}
	}
}
