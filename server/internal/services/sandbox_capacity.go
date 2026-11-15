package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrSandboxLimit rejects admission without changing any existing workload.
type ErrSandboxLimit struct{ Max int }

func (e ErrSandboxLimit) Error() string {
	return fmt.Sprintf("已达到沙箱总上限（%d），请等待空闲沙箱回收或停止不用的环境", e.Max)
}

type SandboxCapacity struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// Only provider-confirmed stopped workloads release their slot. Backup deletion
// can remain pending after compute is released; an observation error proves
// neither. This counts API-level allocations, not physical host reclamation.
func occupiedSandboxes(db *gorm.DB) *gorm.DB {
	return db.Model(&models.Sandbox{}).Where("status <> ?", "stopped")
}

func workloadReleasedStatus(status string) bool {
	return status == "stopped" || status == "exited" || status == "not_found"
}

func (s *SandboxService) Capacity() (SandboxCapacity, error) {
	var used int64
	err := occupiedSandboxes(s.db).Count(&used).Error
	return SandboxCapacity{Used: int(used), Limit: s.MaxSandboxes()}, err
}

// Lock a single database row before counting so two server processes cannot
// both admit against the same free slot. SQLite serializes the write; MySQL
// holds the conflicting row lock until the admission transaction commits.
func lockCapacity(tx *gorm.DB) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"updated_at": time.Now()}),
	}).Create(&models.Setting{Key: "internal.sandbox_capacity_lock", UpdatedAt: time.Now()}).Error
}

func (s *SandboxService) checkCapacity(tx *gorm.DB) error {
	var used int64
	if err := occupiedSandboxes(tx).Count(&used).Error; err != nil {
		return err
	}
	if used >= int64(s.MaxSandboxes()) {
		return ErrSandboxLimit{Max: s.MaxSandboxes()}
	}
	return nil
}

// createSandboxRow reserves capacity before any asynchronous provisioning. The
// same transaction handles admission for every purpose, including workflow nodes.
func (s *SandboxService) createSandboxRow(row *models.Sandbox) error {
	s.capacityMu.Lock()
	defer s.capacityMu.Unlock()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockCapacity(tx); err != nil {
			return err
		}
		if err := s.checkCapacity(tx); err != nil {
			return err
		}
		return tx.Create(row).Error
	})
}

// reserveSandbox includes recovery in the same quota as creation. Its caller
// serializes lifecycle work for this id. release(true) hands the reservation to
// that lifecycle; it does not publish a ready workload. release(false) rolls
// back only an untouched reservation, before external provisioning has begun.
func (s *SandboxService) reserveSandbox(id uint) (release func(bool), err error) {
	s.capacityMu.Lock()
	defer s.capacityMu.Unlock()
	var row models.Sandbox
	reserved := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockCapacity(tx); err != nil {
			return err
		}
		if err := tx.First(&row, id).Error; err != nil {
			return err
		}
		if row.Status != "stopped" {
			return nil // the existing resource already owns a slot
		}
		if err := s.checkCapacity(tx); err != nil {
			return err
		}
		res := tx.Model(&models.Sandbox{}).Where("id = ? AND status = ?", id, "stopped").
			Updates(map[string]any{"status": "resuming", "updated_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("sandbox changed while reserving recovery capacity")
		}
		reserved = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func(keepReservation bool) {
		once.Do(func() {
			if !reserved || keepReservation {
				return
			}
			if err := s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ?", id, "resuming").
				Update("status", "stopped").Error; err != nil {
				log.Error().Err(err).Uint("sandbox", id).Msg("persist sandbox capacity reservation result")
			}
		})
	}, nil
}
