package services

import (
	"context"
	"errors"

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
	v, err := s.durable.Snapshot(context.Background(), soloQueueScope(sb.ThreadID))
	if err != nil {
		return ArtifactTurn{}, err
	}
	if v.Active == nil {
		return ArtifactTurn{}, errors.New("there is no active Solo turn")
	}
	return ArtifactTurn{SessionID: sb.ThreadID, TurnID: v.Active.ID, SandboxID: sb.ID}, nil
}

func (s *SoloService) ArtifactDelivered(_, _ string, turn ArtifactTurn, file models.ArtifactAttachment) {
	s.acquire(turn.SessionID).publish("artifact", map[string]any{"userMsgId": turn.TurnID, "file": file})
}
