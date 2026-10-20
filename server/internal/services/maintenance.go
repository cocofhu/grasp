package services

import (
	"context"
	"fmt"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"os"
	"path/filepath"
)

// Pause stops cron admission while executions already claimed continue draining.
func (s *CronScheduler) Pause() {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	s.paused.Store(true)
}
func (s *CronScheduler) ActiveExecutions() int64 { return s.active.Load() }
func (r *PmTurnRunner) ActiveThreads() int {
	r.mu.Lock()
	threads := make([]*pmThread, 0, len(r.threads))
	for _, t := range r.threads {
		threads = append(threads, t)
	}
	r.mu.Unlock()
	n := 0
	for _, t := range threads {
		if !t.sess.Ready() {
			n++
		}
	}
	return n
}
func (s *GateAutoInvokeService) BeginMaintenance() { s.mu.Lock(); s.maintenance = true; s.mu.Unlock() }
func (s *GateAutoInvokeService) PendingMaintenance() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.queue.tasks)
	if s.queue.busy {
		n++
	}
	return n
}

// PrepareWorkspaceMaintenance archives configuration homes before retiring idle
// interactive sandboxes. It runs in the old process before the offline backup.
func (s *SandboxService) PrepareWorkspaceMaintenance(ctx context.Context) error {
	var rows []models.Sandbox
	if err := s.db.Where("purpose <> ?", "run").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if s.isBusyRow(&row) {
			return fmt.Errorf("sandbox %d still busy", row.ID)
		}
		s.mu.Lock()
		ls := s.live[row.ID]
		s.mu.Unlock()
		if ls != nil && ls.sb != nil && ls.codexAuthPath != "" {
			body, err := ls.sb.ReadFile(ctx, ls.codexAuthPath)
			if err != nil {
				return fmt.Errorf("read Codex login before retiring sandbox %d: %w", row.ID, err)
			}
			if next, ok := runtime.ShouldWriteBackCodexLogin(nil, string(body), ls.codexAuthInjected, ls.codexAuthRejected); ok {
				if ls.codexAuthCredentialID == "" {
					return fmt.Errorf("cannot identify Codex credential for sandbox %d", row.ID)
				}
				if s.codexLoginWriteBack == nil {
					return fmt.Errorf("Codex login writeback unavailable for sandbox %d", row.ID)
				}
				if err := s.codexLoginWriteBack(ls.codexAuthCredentialID, next); err != nil {
					return fmt.Errorf("persist Codex login before retiring sandbox %d: %w", row.ID, err)
				}
			}
		}
		if row.HomeDir != "" {
			if _, err := os.Stat(row.HomeDir); err == nil {
				dst := filepath.Join(filepath.Dir(s.skills.root), "maintenance-archives", fmt.Sprint(row.ID), "config-home")
				if err := copyPrivateTree(row.HomeDir, dst); err != nil {
					return err
				}
			}
		}
		if err := s.Destroy(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}
func copyPrivateTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot archive sandbox symlink %q", rel)
		}
		if d.IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			return os.Chmod(target, 0700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot archive sandbox special file %q", rel)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0600); err != nil {
			return err
		}
		return os.Chmod(target, 0600)
	})
}
