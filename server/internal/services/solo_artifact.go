package services

import (
	"errors"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

func (s *SoloService) CurrentArtifactTurn(runID, nodeID string) (ArtifactTurn, error) {
	if nodeID != "solo" {
		return ArtifactTurn{}, errors.New("invalid Solo node")
	}
	var sb models.Sandbox
	if err := s.db.Where("run_id = ? AND purpose = ? AND status = ?", runID, SandboxPurposeSolo, "running").First(&sb).Error; err != nil {
		return ArtifactTurn{}, err
	}
	var session models.SoloSession
	if err := s.db.Where("id = ? AND sandbox_id = ? AND status = ?", sb.ThreadID, sb.ID, models.SoloStatusRunning).First(&session).Error; err != nil {
		return ArtifactTurn{}, err
	}
	c := s.lookup(sb.ThreadID)
	if c == nil {
		return ArtifactTurn{}, errors.New("there is no active Solo turn")
	}
	v := c.sess.View()
	if !v.HasActive || v.Active == nil {
		return ArtifactTurn{}, errors.New("there is no active Solo turn")
	}
	return ArtifactTurn{SessionID: sb.ThreadID, TurnID: v.Active.ID, SandboxID: sb.ID}, nil
}

// soloAskRunID is the MCP run namespace of a Solo sandbox ("" when unknown).
func (s *SoloService) soloAskRunID(sandboxID uint) string {
	if s.sbx == nil || s.sbx.host == nil || sandboxID == 0 {
		return ""
	}
	sb, err := s.sbx.Get(sandboxID)
	if err != nil {
		return ""
	}
	return sb.RunID
}

// takeSoloAsks returns and clears what the Agent raised through ask_question
// and ask_form on the Solo node.
func (s *SoloService) takeSoloAsks(runID string) ([]models.ReactQuestion, []models.ReactForm) {
	if runID == "" {
		return nil, nil
	}
	return s.sbx.host.TakePendingQuestions(runID, "solo"), s.sbx.host.TakePendingForms(runID, "solo")
}

func (s *SoloService) ArtifactDelivered(_, _ string, turn ArtifactTurn, file models.ArtifactAttachment) {
	if c := s.lookup(turn.SessionID); c != nil {
		c.stream.Session("artifact", map[string]any{"userMsgId": turn.TurnID, "file": file}, chatsession.KeepAppend)
	}
}
