package services

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/cocofhu/grasp/internal/chatcmd"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// soloSkills lists the managed Agent and shared skills staged by BuildConfigHome.
func soloSkills(files []AgentFile) []string {
	paths := make([]string, len(files))
	for i := range files {
		paths[i] = files[i].Path
	}
	return chatcmd.SkillNames(paths)
}

// Parse before persisting so queued turns keep their intent across restart.
func (s *SoloService) prepareSoloCommand(agentName string, turn *soloTurn, hasReferences bool) error {
	command := chatcmd.Parse(turn.Text)
	if command.ChangesContext() && turn.PreviewShared {
		return soloErr(http.StatusForbidden, "command_context_shared", "%s", chatcmd.ErrSharedContext)
	}
	err := command.Validate(hasReferences || len(turn.Images) != 0 || len(turn.Annotations) != 0 || len(turn.References) != 0)
	switch {
	case errors.Is(err, chatcmd.ErrAttachments):
		return soloErr(http.StatusBadRequest, "command_has_attachments", "%s", err)
	case errors.Is(err, chatcmd.ErrSkillRequired):
		return soloErr(http.StatusBadRequest, "skill_required", "%s", err)
	}
	if command.Name == chatcmd.Skill {
		if err != nil || !slices.Contains(s.configuredSkillNames(agentName), command.Skill) {
			return soloErrParams(http.StatusBadRequest, "skill_not_found", "%s",
				map[string]any{"skill": command.Skill}, chatcmd.UnknownSkill(command.Skill))
		}
	}
	turn.Command = command
	return nil
}

func (s *SoloService) configuredSkillNames(agentName string) []string {
	if s.agents != nil {
		if agent, ok := s.agents.Get(agentName); ok {
			return soloSkills(s.sbx.effectiveAgent(agent).Files)
		}
	}
	return nil
}

// Schedule the native restart at the next prompt boundary and commit the
// transcript marker with it. No sandbox or model is needed to record intent.
func (c *soloChat) executeContextCommand(ctx context.Context, turn *soloTurn, started time.Time) error {
	return c.svc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.SoloSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("context_id").
			Where("id = ? AND status IN ?", c.id, []string{models.SoloStatusStarting, models.SoloStatusRunning}).Take(&row).Error; err != nil {
			return err
		}
		updates := map[string]any{"context_reset_pending": turn.Command.Name, "last_active_at": &started}
		if turn.Command.Name == chatcmd.New {
			row.ContextID = turn.ID
			updates["context_id"] = row.ContextID
		}
		if err := tx.Model(&models.SoloSession{}).Where("id = ?", c.id).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Create(&models.SoloMessage{
			ID: turn.ID, TurnID: turn.ID, SessionID: c.id, ContextID: row.ContextID, Role: "system",
			Content: turn.Text, Status: "context_" + turn.Command.Name,
		}).Error
	})
}

func (s *SoloService) skillConfigRoot(prep soloTurnPrep) (string, error) {
	var configRoot string
	if s.sbx != nil {
		s.sbx.mu.Lock()
		if live := s.sbx.live[prep.sandboxID]; live != nil && live.sb != nil {
			configRoot = live.sb.ConfigRoot
		}
		s.sbx.mu.Unlock()
	}
	if configRoot == "" {
		agent, err := s.sessionAgent(prep.row)
		if err != nil {
			return "", err
		}
		configRoot = agent.Layout.ConfigRoot
	}
	return configRoot, nil
}
