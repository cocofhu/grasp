package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// --- memory -----------------------------------------------------------------

// ListMemories returns memory items (newest first).
// When agentName is non-empty, results are scoped to that Agent.
func (s *PmService) ListMemories(agentName string) ([]models.MemoryItem, error) {
	q := s.db
	if agentName != "" {
		q = q.Where("agent_name = ?", agentName)
	}
	var items []models.MemoryItem
	if err := q.Order("updated_at desc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// UpsertMemory creates or updates a memory item by title for an agent.
func (s *PmService) UpsertMemory(agentName, title, content, source, updatedBy string) (models.MemoryItem, error) {
	title = strings.TrimSpace(title)
	agentName = strings.TrimSpace(agentName)
	if title == "" {
		return models.MemoryItem{}, fmt.Errorf("记忆标题不能为空")
	}
	if source == "" {
		source = "user"
	}
	now := time.Now()
	var existing models.MemoryItem
	err := s.db.Where("agent_name = ? AND title = ?", agentName, title).First(&existing).Error
	if err == nil {
		existing.Content = content
		existing.Source = source
		existing.UpdatedBy = updatedBy
		existing.UpdatedAt = now
		if err := s.db.Save(&existing).Error; err != nil {
			return models.MemoryItem{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MemoryItem{}, err
	}
	item := models.MemoryItem{
		ID: "mem-" + uuid.NewString()[:12], AgentName: agentName,
		Title: title, Content: content, Source: source, UpdatedBy: updatedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.Create(&item).Error; err != nil {
		return models.MemoryItem{}, err
	}
	return item, nil
}

// UpdateMemoryForAgent patches a memory only when it belongs to the agent.
func (s *PmService) UpdateMemoryForAgent(agentName, id, title, content, updatedBy string) (models.MemoryItem, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return models.MemoryItem{}, ErrPmMemoryNotFound
	}
	return s.patchMemory(agentName, id, title, content, updatedBy)
}

func (s *PmService) patchMemory(agentName, id, title, content, updatedBy string) (models.MemoryItem, error) {
	var item models.MemoryItem
	if err := s.db.Where("id = ? AND agent_name = ?", id, agentName).
		First(&item).Error; err != nil {
		return models.MemoryItem{}, ErrPmMemoryNotFound
	}
	if t := strings.TrimSpace(title); t != "" {
		item.Title = t
	}
	item.Content = content
	item.Source = "user"
	item.UpdatedBy = updatedBy
	item.UpdatedAt = time.Now()
	if err := s.db.Save(&item).Error; err != nil {
		return models.MemoryItem{}, err
	}
	return item, nil
}

// DeleteMemoryForAgent removes one memory only when it belongs to the agent.
func (s *PmService) DeleteMemoryForAgent(agentName, id string) error {
	agentName = strings.TrimSpace(agentName)
	id = strings.TrimSpace(id)
	if agentName == "" || id == "" {
		return ErrPmMemoryNotFound
	}
	res := s.db.Where("id = ? AND agent_name = ?", id, agentName).
		Delete(&models.MemoryItem{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPmMemoryNotFound
	}
	return nil
}

// ClearMemoriesForAgent deletes memories for one agent.
func (s *PmService) ClearMemoriesForAgent(agentName string) (int64, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return 0, fmt.Errorf("agentName required")
	}
	res := s.db.Where("agent_name = ?", agentName).
		Delete(&models.MemoryItem{})
	return res.RowsAffected, res.Error
}

// PurgeAgentEverywhere removes all agent-scoped rows and
// clears any PM Leader binding that points at the agent. Used on Agent delete.
func (s *PmService) PurgeAgentEverywhere(agentName string) error {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_name = ?", agentName).Delete(&models.MemoryItem{}).Error; err != nil {
			return err
		}
		var threadIDs []string
		if err := tx.Model(&models.ChatThread{}).Where("agent_name = ?", agentName).
			Pluck("id", &threadIDs).Error; err != nil {
			return err
		}
		if len(threadIDs) > 0 {
			if err := tx.Where("thread_id IN ?", threadIDs).Delete(&models.ChatTurnDraft{}).Error; err != nil {
				return err
			}
			if err := tx.Where("thread_id IN ?", threadIDs).Delete(&models.ChatMessage{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", threadIDs).Delete(&models.ChatThread{}).Error; err != nil {
				return err
			}
		}
		var jobIDs []string
		if err := tx.Model(&models.AgentCronJob{}).Where("agent_name = ?", agentName).
			Pluck("id", &jobIDs).Error; err != nil {
			return err
		}
		if len(jobIDs) > 0 {
			if err := tx.Where("job_id IN ?", jobIDs).Delete(&models.AgentCronRun{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", jobIDs).Delete(&models.AgentCronJob{}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.Workspace{}).
			Where("pm_leader_agent = ?", agentName).
			Updates(map[string]any{
				"pm_leader_agent":   "",
				"pm_leader_enabled": false,
				"updated_at":        time.Now(),
			}).Error
	})
}

// RenameAgentScopedData rewrites agent_name on memories/threads/cron and PM
// Leader refs when an Agent is renamed. Agent names are global.
func (s *PmService) RenameAgentScopedData(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}
	now := time.Now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.MemoryItem{}).Where("agent_name = ?", oldName).
			Update("agent_name", newName).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ChatThread{}).Where("agent_name = ?", oldName).
			Updates(map[string]any{"agent_name": newName, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AgentCronJob{}).Where("agent_name = ?", oldName).
			Updates(map[string]any{"agent_name": newName, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Workspace{}).Where("pm_leader_agent = ?", oldName).
			Updates(map[string]any{"pm_leader_agent": newName, "updated_at": now}).Error
	})
}

// GetMemory returns one memory by id for the agent.
func (s *PmService) GetMemory(agentName, id string) (models.MemoryItem, error) {
	var item models.MemoryItem
	if err := s.db.Where("id = ? AND agent_name = ?", id, agentName).
		First(&item).Error; err != nil {
		return models.MemoryItem{}, ErrPmMemoryNotFound
	}
	return item, nil
}

// SearchMemories finds memories by title/content keyword for an agent.
func (s *PmService) SearchMemories(agentName, q string, limit int) ([]map[string]any, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query required")
	}
	if limit <= 0 {
		limit = 20
	}
	like := "%" + EscapeLike(q) + "%"
	var items []models.MemoryItem
	if err := s.db.Where("agent_name = ? AND (title LIKE ? ESCAPE ? OR content LIKE ? ESCAPE ?)",
		agentName, like, `\`, like, `\`).Order("updated_at desc").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		summary := it.Content
		if len([]rune(summary)) > 120 {
			summary = string([]rune(summary)[:120]) + "…"
		}
		out = append(out, map[string]any{
			"id": it.ID, "title": it.Title, "summary": summary, "updatedAt": it.UpdatedAt,
		})
	}
	return out, nil
}
