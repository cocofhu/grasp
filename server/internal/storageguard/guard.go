// Package storageguard fences offline storage administration against serving
// processes. Presence never expires: a paused or disconnected server must not
// silently become permission to switch the storage underneath it.
package storageguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Control struct {
	Name     string `gorm:"primaryKey" json:"-"`
	Owner    string `json:"maintenanceId,omitempty"`
	Revision uint64 `json:"-"`
}

func (Control) TableName() string { return "payload_storage_control" }

type Runtime struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Instance  string    `json:"instance"`
	ProcessID int       `json:"processId"`
	StartedAt time.Time `json:"startedAt"`
}

func (Runtime) TableName() string { return "payload_storage_runtimes" }

type Status struct {
	MaintenanceID string    `json:"maintenanceId,omitempty"`
	Runtimes      []Runtime `json:"runtimes"`
}

func initialize(db *gorm.DB) error {
	if err := db.AutoMigrate(&Control{}, &Runtime{}); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&Control{Name: "primary"}).Error
}

func Inspect(ctx context.Context, db *gorm.DB) (Status, error) {
	var result Status
	if err := initialize(db.WithContext(ctx)); err != nil {
		return result, err
	}
	var control Control
	if err := db.WithContext(ctx).First(&control, "name = ?", "primary").Error; err != nil {
		return result, err
	}
	result.MaintenanceID = control.Owner
	result.Runtimes = []Runtime{}
	err := db.WithContext(ctx).Order("started_at, id").Find(&result.Runtimes).Error
	return result, err
}

// Register must run before application schema migrations or workers start.
// Unregister is allowed only after the application's SQL pool has been closed.
func Register(ctx context.Context, db *gorm.DB) (string, error) {
	if err := initialize(db.WithContext(ctx)); err != nil {
		return "", err
	}
	id := uuid.NewString()
	instance, err := os.Hostname()
	if err != nil {
		return "", err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked := tx.Model(&Control{}).Where("name = ? AND owner = ?", "primary", "").
			UpdateColumn("revision", gorm.Expr("revision + 1"))
		if locked.Error != nil {
			return locked.Error
		}
		if locked.RowsAffected != 1 {
			return errors.New("payload storage maintenance is active; server startup refused")
		}
		return tx.Create(&Runtime{ID: id, Instance: instance, ProcessID: os.Getpid(), StartedAt: time.Now().UTC()}).Error
	})
	return id, err
}

func Unregister(ctx context.Context, db *gorm.DB, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return errors.New("invalid runtime identity")
	}
	return db.WithContext(ctx).Delete(&Runtime{}, "id = ?", id).Error
}

// Enter atomically excludes new runtimes and checks all existing presences.
// A crashed server's presence requires its exact ID and an operator's explicit
// attestation that the process was stopped. There is deliberately no TTL,
// wildcard, or automatic stale-presence cleanup.
func Enter(ctx context.Context, db *gorm.DB, owner string, stopped []string) error {
	if _, err := uuid.Parse(owner); err != nil {
		return errors.New("invalid maintenance identity")
	}
	if err := initialize(db.WithContext(ctx)); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked := tx.Model(&Control{}).Where("name = ? AND (owner = ? OR owner = ?)", "primary", "", owner).
			Updates(map[string]any{"owner": owner, "revision": gorm.Expr("revision + 1")})
		if locked.Error != nil {
			return locked.Error
		}
		if locked.RowsAffected != 1 {
			return errors.New("another payload storage maintenance operation is active")
		}
		var runtimes []Runtime
		if err := tx.Find(&runtimes).Error; err != nil {
			return err
		}
		acknowledged := map[string]bool{}
		for _, id := range stopped {
			if _, err := uuid.Parse(id); err != nil {
				return errors.New("invalid stopped runtime identity")
			}
			acknowledged[id] = true
		}
		for _, runtime := range runtimes {
			if !acknowledged[runtime.ID] {
				return fmt.Errorf("server %s is still registered; stop every replica before maintenance", runtime.ID)
			}
		}
		if len(runtimes) > 0 {
			return tx.Where("id IN ?", stopped).Delete(&Runtime{}).Error
		}
		return nil
	})
}

func Check(ctx context.Context, db *gorm.DB, owner string) error {
	var control Control
	if err := db.WithContext(ctx).First(&control, "name = ?", "primary").Error; err != nil {
		return err
	}
	if owner == "" || control.Owner != owner {
		return errors.New("payload storage maintenance ownership lost")
	}
	return nil
}

// Leave is explicit; an interrupted operation keeps the startup fence in SQL.
func Leave(ctx context.Context, db *gorm.DB, owner string) error {
	if owner == "" {
		return errors.New("maintenance identity required")
	}
	result := db.WithContext(ctx).Model(&Control{}).Where("name = ? AND owner = ?", "primary", owner).
		Updates(map[string]any{"owner": "", "revision": gorm.Expr("revision + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("payload storage maintenance ownership lost")
	}
	return nil
}
