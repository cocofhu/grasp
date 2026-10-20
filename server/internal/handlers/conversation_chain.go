package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// ConversationChain is a light, execution-ordered view of a run's node
// executions for the chat UI: GET /api/runs/:id/conversation-chain
// [?until=<nodeId>[&iteration=<n>]]. until cuts the chain after the matching
// execution (inclusive); without iteration it is that node's LAST execution.
// 404 unknown run; 400 bad iteration / iteration without until / until not in
// the chain.
func (h *Handlers) ConversationChain(c *gin.Context) {
	run, ok := h.Runs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	until := strings.TrimSpace(c.Query("until"))
	iter := 0
	if raw := strings.TrimSpace(c.Query("iteration")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_iteration", "message": "iteration must be a positive integer"})
			return
		}
		if until == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_iteration", "message": "iteration requires until"})
			return
		}
		iter = n
	}
	segs, states, err := h.Runs.ConversationChain(run, until, iter)
	if err != nil {
		if errors.Is(err, services.ErrChainUntilNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "until_not_found", "message": "no such execution in the chain"})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.Eng != nil {
		wantKind := map[uint]bool{}
		for _, s := range segs {
			if segmentHasTask(s) {
				wantKind[s.StateRunID] = true
			}
		}
		if len(wantKind) > 0 {
			tasks := h.Eng.SegmentTasks(run, states, func(sr models.StateRun) bool { return wantKind[sr.ID] })
			for i := range segs {
				segs[i].Task = tasks[segs[i].StateRunID]
			}
		}
	}
	out := gin.H{"runId": run.ID, "status": run.Status, "segments": segs}
	if cn, ok := h.runCurrentNodes([]models.Run{run})[run.ID]; ok {
		out["currentNode"] = cn
	}
	c.JSON(http.StatusOK, out)
}

// segmentHasTask: task previews are for Agent executions only (agent /
// clarify / review / takeover); gates and non-Agent nodes never carry one.
func segmentHasTask(s services.ChainSegment) bool {
	if s.NodeType != "agent" {
		return false
	}
	switch s.Kind {
	case services.CurrentKindAgent, services.CurrentKindClarify, services.CurrentKindReview, services.CurrentKindTakeover:
		return true
	}
	return false
}
