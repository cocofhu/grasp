package handlers

import "github.com/gin-gonic/gin"

// isHumanGateNode reports whether this run node is an approval gate.
// Clarify, review, and takeover conversations are not.
func (h *Handlers) isHumanGateNode(runID, nodeID string) bool {
	if h == nil || h.Runs == nil {
		return false
	}
	run, ok := h.Runs.Get(runID)
	if !ok {
		return false
	}
	node := run.Graph.FindNode(nodeID)
	return node != nil && node.Type == "human_gate"
}

func (h *Handlers) NodeCreateShareLink(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.CreateGateShareLink(c)
		return
	}
	h.CreateReviewShareLink(c)
}

func (h *Handlers) NodeGetShareLink(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.GetGateShareLink(c)
		return
	}
	h.GetReviewShareLink(c)
}

func (h *Handlers) NodeRegenShareLink(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.RegenGateShareLink(c)
		return
	}
	h.RegenReviewShareLink(c)
}

func (h *Handlers) NodeRevokeShareLink(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.RevokeGateShareLink(c)
		return
	}
	h.RevokeReviewShareLink(c)
}

// NodeReply is the clarify/review reply, or a human gate's upstream hot revision.
func (h *Handlers) NodeReply(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.GateReactRevise(c)
		return
	}
	h.ReactReply(c)
}

func (h *Handlers) NodeCancel(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.GateReactCancel(c)
		return
	}
	h.ReactCancel(c)
}

func (h *Handlers) NodeQueueRemove(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.GateReactQueueRemove(c)
		return
	}
	h.ReactQueueRemove(c)
}

func (h *Handlers) NodeQueueReorder(c *gin.Context) {
	if h.isHumanGateNode(c.Param("id"), c.Param("nodeId")) {
		h.GateReactQueueReorder(c)
		return
	}
	h.ReactQueueReorder(c)
}
