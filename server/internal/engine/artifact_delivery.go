package engine

import (
	"errors"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

// CurrentArtifactTurn supplies platform-owned delivery scope. A run-bound MCP
// credential cannot choose a conversation, visit, or another user's turn.
func (e *Engine) CurrentArtifactTurn(runID, nodeID string) (services.ArtifactTurn, error) {
	// Visitor chats share the node's MCP run credential. Until delivery has a
	// per-chat credential, reject ambiguous callers rather than publishing a
	// visitor's file to the owner's simultaneously active turn.
	var visitors []*reviewSession
	e.reviewSess.Range(func(_ string, s *reviewSession) bool {
		if s != nil && s.runID == runID && s.producerID == nodeID && s.lane != "" {
			visitors = append(visitors, s)
		}
		return true
	})
	for _, visitor := range visitors {
		if !visitor.Ready() {
			return services.ArtifactTurn{}, errors.New("file delivery is unavailable while a shared visitor chat is active")
		}
	}
	var sr models.StateRun
	if err := e.db.Select("id", "iteration", "status").Where("run_id = ? AND node_id = ?", runID, nodeID).Order("id desc").First(&sr).Error; err != nil {
		return services.ArtifactTurn{}, err
	}
	turn := services.ArtifactTurn{StateRunID: sr.ID, Iteration: sr.Iteration}
	if s := e.laneSession(runID, nodeID, ""); s != nil {
		v := s.View()
		if v.HasActive && v.Active != nil {
			turn.TurnID = v.Active.ID
			return turn, nil
		}
	}
	if sr.Status == "running" {
		turn.TurnID = fmt.Sprintf("visit:%d", sr.ID)
		return turn, nil
	}
	return turn, errors.New("there is no active conversation turn")
}

func (e *Engine) ArtifactDelivered(runID, nodeID string, turn services.ArtifactTurn, _ models.ArtifactAttachment) {
	e.publishDialogue(e.laneDialogueEntry(runID, nodeID, turn.Lane, ""))
}

func (e *Engine) attachDialogueFiles(st *DialogueState) {
	var files []models.ArtifactDelivery
	if e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", st.RunID, st.NodeID, st.Iteration).Order("created_at,id").Find(&files).Error != nil {
		return
	}
	last := map[string]int{}
	lastAgent := -1
	for i, row := range st.Rows {
		if row.Role == "agent" {
			lastAgent = i
			if row.TurnID != "" {
				last[row.TurnID] = i
			}
		}
	}
	for _, f := range files {
		i, ok := last[f.TurnID]
		if !ok && f.StateRunID != 0 && f.TurnID == fmt.Sprintf("visit:%d", f.StateRunID) {
			i = lastAgent
			ok = i >= 0
		}
		if !ok {
			continue
		}
		duplicate := false
		for _, old := range st.Rows[i].Files {
			if old.ArtifactID == f.ArtifactID && old.Revision == f.Revision {
				duplicate = true
				break
			}
		}
		if !duplicate {
			st.Rows[i].Files = append(st.Rows[i].Files, f.ArtifactAttachment)
		}
		st.Rows[i].Outcome = models.ReplyOutcome(st.Rows[i].ReactMessage)
		for j := range st.Turns {
			if st.Turns[j].TurnID == st.Rows[i].TurnID && st.Turns[j].Phase == turnEnded {
				st.Turns[j].Outcome = st.Rows[i].Outcome
			}
		}
	}
}
