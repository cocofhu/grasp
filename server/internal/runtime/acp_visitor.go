package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

var _ VisitorLaneProvider = (*acpProvider)(nil)

// visitorLane is one share-link visitor's chat inside a node's parked sandbox.
type visitorLane struct {
	sb     *sandbox.Sandbox
	acp    *sandbox.ACPClient
	chatID string
	primed bool
}

func visitorKey(runID, nodeID, lane string) string {
	return runID + "|" + nodeID + "|" + lane
}

// VisitorTurn runs one visitor turn on its own bridge chat. The chat shares
// the parked session's sandbox (and so its workspace) but not its Agent context.
func (c *acpProvider) VisitorTurn(ctx context.Context, req NodeReq, lane, prelude, human string, images []models.PromptImage, onProgress func([]models.AcpEvent, bool)) ReactTurn {
	vl, err := c.openVisitorLane(ctx, req, lane)
	if err != nil {
		if errors.Is(err, ErrVisitorsFull) {
			return ReactTurn{Msg: "(同时对话的访客已满,请稍后再试)", Err: err}
		}
		if errors.Is(err, ErrNoParkedSession) {
			return ReactTurn{Msg: "(" + err.Error() + ")", Err: err}
		}
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).Msg("visitor chat open failed")
		return ReactTurn{Msg: "(访客会话启动失败:" + err.Error() + ")", Err: err}
	}
	prompt := human
	if !vl.primed {
		prelude = strings.TrimSpace(prelude + previewNodePromptExtras(req))
	}
	if !vl.primed && prelude != "" {
		prompt = prelude + "\n\n## 用户消息\n" + strings.TrimRight(human, "\n")
	}
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	defer cancel()
	res, err := vl.acp.ChatStreamResult(chatCtx, prompt, images, func(r *sandbox.ChatResult) {
		if onProgress == nil {
			return
		}
		busy := true
		if r.BusySet {
			busy = r.Busy
		}
		onProgress(chatResultToEvents(r), busy)
	})
	// Visitor turns never answer the node's own pending ask_question.
	_ = takeClarifyPending(c.host, req.RunID, req.NodeID)
	if err != nil {
		if !vl.acp.IsConnected() {
			c.RetireVisitorLane(req.RunID, req.NodeID, lane)
		}
		return ReactTurn{Msg: "(访客对话失败:" + err.Error() + ")", Err: err, Interrupted: isTurnTimeoutErr(err)}
	}
	vl.primed = true
	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent
	absorbChat(&usage, &usageByModel, &events, res)
	out := ReactTurn{Msg: res.Narration, Events: events, Usage: usage, UsageByModel: usageByModel,
		Handoffs: handoffNarrations(res), OpID: res.OpID}
	if fail := chatFailure(res); fail != "" {
		out.Msg = withFailureBanner(res.Narration, "访客对话失败", fail)
		out.Err = errors.New(fail)
		out.Interrupted = res.Interrupted
	}
	return out
}

func (c *acpProvider) openVisitorLane(ctx context.Context, req NodeReq, lane string) (*visitorLane, error) {
	key := visitorKey(req.RunID, req.NodeID, lane)
	c.mu.Lock()
	if c.visitors == nil {
		c.visitors = map[string]*visitorLane{}
	}
	if vl := c.visitors[key]; vl != nil && vl.acp != nil && vl.acp.IsConnected() {
		c.mu.Unlock()
		return vl, nil
	}
	stale := c.visitors[key]
	delete(c.visitors, key)
	parked := c.sessions[reactKey(req)]
	c.mu.Unlock()
	if stale != nil {
		// Its sandbox may already be gone; do not stall the new turn on it.
		go c.closeVisitorLane(stale)
	}
	if parked == nil || parked.sb == nil || parked.acp == nil || !parked.acp.IsConnected() {
		return nil, ErrNoParkedSession
	}
	sb := parked.sb
	chatID, err := sandbox.CreateChat(ctx, sb.Host, sb.Port, sb.Password, "visitor "+lane)
	if errors.Is(err, sandbox.ErrTooManyChats) {
		return nil, ErrVisitorsFull
	}
	if err != nil {
		return nil, err
	}
	acp := sb.ACP().WithChat(chatID).
		WithSession(sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeout(c.opts.ChatIdleTimeout).
		WithBridgeModel(parked.acp.BridgeModel())
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		_ = sandbox.DeleteChat(context.Background(), sb.Host, sb.Port, sb.Password, chatID)
		return nil, err
	}
	vl := &visitorLane{sb: sb, acp: acp, chatID: chatID}
	c.mu.Lock()
	if prev := c.visitors[key]; prev != nil {
		c.mu.Unlock()
		c.closeVisitorLane(vl)
		return prev, nil
	}
	c.visitors[key] = vl
	c.mu.Unlock()
	return vl, nil
}

// VisitorChatID returns the bridge chat serving a lane ("" when none).
func (c *acpProvider) VisitorChatID(runID, nodeID, lane string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if vl := c.visitors[visitorKey(runID, nodeID, lane)]; vl != nil {
		return vl.chatID
	}
	return ""
}

// CancelVisitorTurn aborts the lane's in-flight turn without closing its chat.
func (c *acpProvider) CancelVisitorTurn(runID, nodeID, lane string) {
	c.mu.Lock()
	vl := c.visitors[visitorKey(runID, nodeID, lane)]
	c.mu.Unlock()
	if vl != nil && vl.acp != nil {
		_ = vl.acp.Cancel()
	}
}

// RetireVisitorLane closes one lane's chat and deletes it from the bridge.
func (c *acpProvider) RetireVisitorLane(runID, nodeID, lane string) {
	key := visitorKey(runID, nodeID, lane)
	c.mu.Lock()
	vl := c.visitors[key]
	delete(c.visitors, key)
	c.mu.Unlock()
	if vl != nil {
		c.closeVisitorLane(vl)
	}
}

// retireVisitorLanes closes every lane whose key starts with prefix.
func (c *acpProvider) retireVisitorLanes(prefix string) {
	c.mu.Lock()
	var lanes []*visitorLane
	for k, vl := range c.visitors {
		if strings.HasPrefix(k, prefix) {
			lanes = append(lanes, vl)
			delete(c.visitors, k)
		}
	}
	c.mu.Unlock()
	for _, vl := range lanes {
		c.closeVisitorLane(vl)
	}
}

func (c *acpProvider) closeVisitorLane(vl *visitorLane) {
	if vl == nil {
		return
	}
	if vl.acp != nil {
		vl.acp.Close()
	}
	if vl.sb != nil && vl.chatID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sandbox.DeleteChat(ctx, vl.sb.Host, vl.sb.Port, vl.sb.Password, vl.chatID); err != nil {
			log.Debug().Err(err).Str("chat", vl.chatID).Msg("delete visitor chat failed")
		}
	}
}
