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

// ListMemories returns memory items for a project (newest first).
// When agentName is non-empty, results are scoped to that Agent.
func (s *PmService) ListMemories(projectID, agentName string) ([]models.ProjectMemoryItem, error) {
	if _, ok := s.project(projectID); !ok {
		return nil, ErrProjectNotFound
	}
	q := s.db.Where("project_id = ?", projectID)
	if agentName != "" {
		q = q.Where("agent_name = ?", agentName)
	}
	var items []models.ProjectMemoryItem
	if err := q.Order("updated_at desc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// UpsertMemory creates or updates a memory item by title within a project+agent.
func (s *PmService) UpsertMemory(projectID, agentName, title, content, source, updatedBy string) (models.ProjectMemoryItem, error) {
	if _, ok := s.project(projectID); !ok {
		return models.ProjectMemoryItem{}, ErrProjectNotFound
	}
	title = strings.TrimSpace(title)
	agentName = strings.TrimSpace(agentName)
	if title == "" {
		return models.ProjectMemoryItem{}, fmt.Errorf("记忆标题不能为空")
	}
	if source == "" {
		source = "user"
	}
	now := time.Now()
	var existing models.ProjectMemoryItem
	err := s.db.Where("project_id = ? AND agent_name = ? AND title = ?", projectID, agentName, title).First(&existing).Error
	if err == nil {
		existing.Content = content
		existing.Source = source
		existing.UpdatedBy = updatedBy
		existing.UpdatedAt = now
		if err := s.db.Save(&existing).Error; err != nil {
			return models.ProjectMemoryItem{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ProjectMemoryItem{}, err
	}
	item := models.ProjectMemoryItem{
		ID: "mem-" + uuid.NewString()[:12], ProjectID: projectID, AgentName: agentName,
		Title: title, Content: content, Source: source, UpdatedBy: updatedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.Create(&item).Error; err != nil {
		return models.ProjectMemoryItem{}, err
	}
	return item, nil
}

// UpdateMemoryForAgent patches a memory only when it belongs to project+agent.
func (s *PmService) UpdateMemoryForAgent(projectID, agentName, id, title, content, updatedBy string) (models.ProjectMemoryItem, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return models.ProjectMemoryItem{}, ErrPmMemoryNotFound
	}
	return s.patchMemory(projectID, agentName, id, title, content, updatedBy)
}

func (s *PmService) patchMemory(projectID, agentName, id, title, content, updatedBy string) (models.ProjectMemoryItem, error) {
	var item models.ProjectMemoryItem
	if err := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", id, projectID, agentName).
		First(&item).Error; err != nil {
		return models.ProjectMemoryItem{}, ErrPmMemoryNotFound
	}
	if t := strings.TrimSpace(title); t != "" {
		item.Title = t
	}
	item.Content = content
	item.Source = "user"
	item.UpdatedBy = updatedBy
	item.UpdatedAt = time.Now()
	if err := s.db.Save(&item).Error; err != nil {
		return models.ProjectMemoryItem{}, err
	}
	return item, nil
}

// DeleteMemoryForAgent removes one memory only when it belongs to project+agent.
func (s *PmService) DeleteMemoryForAgent(projectID, agentName, id string) error {
	agentName = strings.TrimSpace(agentName)
	id = strings.TrimSpace(id)
	if agentName == "" || id == "" {
		return ErrPmMemoryNotFound
	}
	res := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", id, projectID, agentName).
		Delete(&models.ProjectMemoryItem{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPmMemoryNotFound
	}
	return nil
}

// ClearMemoriesForAgent deletes memories for one agent in a project.
func (s *PmService) ClearMemoriesForAgent(projectID, agentName string) (int64, error) {
	if _, ok := s.project(projectID); !ok {
		return 0, ErrProjectNotFound
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return 0, fmt.Errorf("agentName required")
	}
	res := s.db.Where("project_id = ? AND agent_name = ?", projectID, agentName).
		Delete(&models.ProjectMemoryItem{})
	return res.RowsAffected, res.Error
}

// PurgeAgentProjectData removes every piece of an Agent's data scoped to one
// project: memories, chat threads (+ messages + drafts), and cron jobs (+ runs).
// It also disables the project's PM Leader when it points at this Agent.
// Idempotent; missing project is tolerated (rows matched by ids only).
func (s *PmService) PurgeAgentProjectData(projectID, agentName string) error {
	projectID = strings.TrimSpace(projectID)
	agentName = strings.TrimSpace(agentName)
	if projectID == "" || agentName == "" {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ? AND agent_name = ?", projectID, agentName).
			Delete(&models.ProjectMemoryItem{}).Error; err != nil {
			return err
		}
		var threadIDs []string
		if err := tx.Model(&models.ChatThread{}).
			Where("project_id = ? AND agent_name = ?", projectID, agentName).
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
		if err := tx.Model(&models.AgentCronJob{}).
			Where("project_id = ? AND agent_name = ?", projectID, agentName).
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
		var p models.Project
		if err := tx.First(&p, "id = ?", projectID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if strings.TrimSpace(p.PmLeaderAgent) == agentName {
			if err := tx.Model(&p).Select("PmLeaderAgent", "PmLeaderEnabled", "UpdatedAt").
				Updates(map[string]any{
					"pm_leader_agent":   "",
					"pm_leader_enabled": false,
					"updated_at":        time.Now(),
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// PurgeAgentEverywhere removes all agent-scoped rows across every project and
// clears any PM Leader binding that points at the agent. Used on Agent delete.
func (s *PmService) PurgeAgentEverywhere(agentName string) error {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_name = ?", agentName).Delete(&models.ProjectMemoryItem{}).Error; err != nil {
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
		return tx.Model(&models.Project{}).
			Where("pm_leader_agent = ?", agentName).
			Updates(map[string]any{
				"pm_leader_agent":   "",
				"pm_leader_enabled": false,
				"updated_at":        time.Now(),
			}).Error
	})
}

// RenameAgentScopedData rewrites agent_name on memories/threads/cron and PM
// Leader refs when an Agent is renamed. Scoped to all projects (agent names are global).
func (s *PmService) RenameAgentScopedData(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}
	now := time.Now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ProjectMemoryItem{}).Where("agent_name = ?", oldName).
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
		return tx.Model(&models.Project{}).Where("pm_leader_agent = ?", oldName).
			Updates(map[string]any{"pm_leader_agent": newName, "updated_at": now}).Error
	})
}

// GetMemory returns one memory by id for project+agent.
func (s *PmService) GetMemory(projectID, agentName, id string) (models.ProjectMemoryItem, error) {
	var item models.ProjectMemoryItem
	if err := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", id, projectID, agentName).
		First(&item).Error; err != nil {
		return models.ProjectMemoryItem{}, ErrPmMemoryNotFound
	}
	return item, nil
}

// SearchMemories finds memories by title/content keyword for an agent.
func (s *PmService) SearchMemories(projectID, agentName, q string, limit int) ([]map[string]any, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query required")
	}
	if limit <= 0 {
		limit = 20
	}
	like := "%" + EscapeLike(q) + "%"
	var items []models.ProjectMemoryItem
	if err := s.db.Where("project_id = ? AND agent_name = ? AND (title LIKE ? ESCAPE ? OR content LIKE ? ESCAPE ?)",
		projectID, agentName, like, `\`, like, `\`).Order("updated_at desc").Limit(limit).Find(&items).Error; err != nil {
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
