package services

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TouchActivity records real user or task activity for every sandbox. It never
// grants a separate lease: the single configured idle interval applies to all.
func (s *SandboxService) TouchActivity(id uint) {
	if s == nil || id == 0 {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.lastTouch[id] = now
	s.mu.Unlock()
	_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND status NOT IN ?", id, []string{"saving", "stopped", "deleting"}).
		Update("last_active_at", now).Error
}

func (s *SandboxService) TouchActivityByName(name string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	var row models.Sandbox
	if s.db.Select("id").Where("name = ?", name).First(&row).Error == nil {
		s.TouchActivity(row.ID)
	}
}

func (s *SandboxService) forgetTouch(id uint) {
	s.mu.Lock()
	delete(s.lastTouch, id)
	s.mu.Unlock()
}
