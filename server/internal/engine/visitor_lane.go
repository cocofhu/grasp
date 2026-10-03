package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	// VisitorLaneIdleTTL is how long an idle visitor keeps its sandbox chat.
	VisitorLaneIdleTTL = 30 * time.Minute
	visitorSweepEvery  = 5 * time.Minute

	visitorPreludeHistoryTurns = 8
	visitorPreludeOwnTurns     = 20
	visitorPreludeTurnChars    = 1200
	visitorPreludeBlockChars   = 4000
)

// ErrVisitorsFull is returned when a link already has its maximum number of
// concurrent visitor lanes.
var ErrVisitorsFull = runtime.ErrVisitorsFull

// VisitorTarget names one share-link visitor lane on a producer session.
type VisitorTarget struct {
	LinkID     string
	RunID      string
	ProducerID string
	Lane       string
	Owner      string
	Source     string // "node" | "gate"
	GateNodeID string
}

type visitorLaneState struct {
	linkID, runID, producerID, lane string
	lastActive                      time.Time
}

type visitorChatIDs interface {
	VisitorChatID(runID, nodeID, lane string) string
}

// VisitorLanesSupported reports whether the backend can host per-visitor
// chats; without it every visitor shares the node's own dialogue.
func (e *Engine) VisitorLanesSupported() bool {
	_, ok := e.provider.(runtime.VisitorLaneProvider)
	return ok
}

// EnqueueVisitorTurn queues one visitor message on its own lane. The lane
// talks to a separate sandbox chat that shares the node's workspace.
func (e *Engine) EnqueueVisitorTurn(t VisitorTarget, text string, images []models.PromptImage, annotations []models.ReactAnnotation) (waiting int, err error) {
	if t.Lane == "" || t.LinkID == "" || t.RunID == "" || t.ProducerID == "" {
		return 0, errors.New("visitor lane required")
	}
	if e.IsHalted() {
		return 0, errors.New("server is shutting down")
	}
	if _, ok := e.provider.(runtime.VisitorLaneProvider); !ok {
		return 0, errors.New("当前执行后端不支持访客对话")
	}
	if strings.TrimSpace(text) == "" && len(images) == 0 && len(annotations) == 0 {
		return 0, errors.New("text, images, or annotations required")
	}
	if !e.HasLiveReviewSession(t.RunID, t.ProducerID) {
		return 0, runtime.ErrNoParkedSession
	}
	if err := e.admitVisitorLane(t); err != nil {
		return 0, err
	}
	images, err = blob.IngestPromptImages(context.Background(), e.blobs, images)
	if err != nil {
		return 0, fmt.Errorf("ingest attachments: %w", err)
	}
	item := &reviewQueueItem{
		ID:          uuid.NewString(),
		Text:        text,
		Effective:   renderReviewHuman(text, annotations),
		Images:      images,
		Annotations: annotations,
		Source:      firstNonEmptyStr(t.Source, "node"),
		GateNodeID:  t.GateNodeID,
		Owner:       t.Owner,
	}
	return e.enqueueLaneItem(t.RunID, t.ProducerID, t.Lane, t.LinkID, sessionKindReview, item)
}

// admitVisitorLane registers the lane, refusing a new one past the link cap.
func (e *Engine) admitVisitorLane(t VisitorTarget) error {
	e.visitorSweep.Do(func() { go e.sweepVisitorLanesLoop() })
	key := e.laneSessionKey(t.RunID, t.ProducerID, t.Lane)
	now := time.Now()
	e.visitorMu.Lock()
	defer e.visitorMu.Unlock()
	if e.visitorLanes == nil {
		e.visitorLanes = map[string]*visitorLaneState{}
	}
	if st := e.visitorLanes[key]; st != nil {
		st.lastActive = now
		return nil
	}
	n := 0
	for _, st := range e.visitorLanes {
		if st.linkID == t.LinkID {
			n++
		}
	}
	if n >= gateshare.MaxVisitorLanesPerLink {
		return ErrVisitorsFull
	}
	e.visitorLanes[key] = &visitorLaneState{
		linkID: t.LinkID, runID: t.RunID, producerID: t.ProducerID, lane: t.Lane, lastActive: now,
	}
	return nil
}

func (e *Engine) touchVisitorLane(runID, producerID, lane string) {
	key := e.laneSessionKey(runID, producerID, lane)
	e.visitorMu.Lock()
	if st := e.visitorLanes[key]; st != nil {
		st.lastActive = time.Now()
	}
	e.visitorMu.Unlock()
}

// VisitorSessionSnapshot is ReviewSessionSnapshotFor for a visitor lane.
func (e *Engine) VisitorSessionSnapshot(runID, producerID, lane string) (ReviewSessionSnapshot, bool) {
	return e.laneSnapshot(e.laneSession(runID, producerID, lane))
}

// VisitorLiveEvents returns the lane's in-flight stream while a turn runs.
func (e *Engine) VisitorLiveEvents(runID, producerID, lane string) []models.AcpEvent {
	s := e.laneSession(runID, producerID, lane)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil
	}
	return append([]models.AcpEvent(nil), s.liveEvents...)
}

// CancelVisitorTurn stops the lane's active turn and drops its queue.
func (e *Engine) CancelVisitorTurn(runID, producerID, lane string) error {
	return e.cancelLaneSession(runID, producerID, lane, true)
}

func (e *Engine) RemoveVisitorQueuedItem(runID, producerID, lane, itemID string) error {
	return e.removeLaneQueuedItem(runID, producerID, lane, itemID)
}

func (e *Engine) ReorderVisitorQueuedItems(runID, producerID, lane string, itemIDs []string) error {
	return e.reorderLaneQueuedItems(runID, producerID, lane, itemIDs)
}

// VisitorTurns is what a visitor sees: its own transcript once it has spoken,
// otherwise the node's dialogue so far.
func (e *Engine) VisitorTurns(linkID, lane, runID, producerID string) []models.ReactMessage {
	var row models.GateShareVisitorConversation
	if err := e.db.Where("link_id = ? AND lane = ?", linkID, lane).First(&row).Error; err == nil {
		return row.Turns()
	}
	return e.defaultLaneTurns(runID, producerID)
}

func (e *Engine) defaultLaneTurns(runID, producerID string) []models.ReactMessage {
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, producerID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return []models.ReactMessage{}
	}
	return conv.Turns()
}

// RetireVisitorLanesForLink stops and closes every visitor lane of a link
// (decided, revoked, re-issued, or expired).
func (e *Engine) RetireVisitorLanesForLink(linkIDs ...string) {
	if len(linkIDs) == 0 {
		return
	}
	want := map[string]bool{}
	for _, id := range linkIDs {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	e.visitorMu.Lock()
	var lanes []*visitorLaneState
	for k, st := range e.visitorLanes {
		if want[st.linkID] {
			lanes = append(lanes, st)
			delete(e.visitorLanes, k)
		}
	}
	e.visitorMu.Unlock()
	for _, st := range lanes {
		e.retireVisitorLane(st)
	}
}

// RetireVisitorLanesForTokenHashes is RetireVisitorLanesForLink keyed by the
// share-link token hashes an invalidation reports.
func (e *Engine) RetireVisitorLanesForTokenHashes(tokenHashes []string) {
	if len(tokenHashes) == 0 {
		return
	}
	e.visitorMu.Lock()
	none := len(e.visitorLanes) == 0
	e.visitorMu.Unlock()
	if none {
		return
	}
	var ids []string
	if err := e.db.Model(&models.GateShareLink{}).Where("token_hash IN ?", tokenHashes).Pluck("id", &ids).Error; err != nil {
		return
	}
	e.RetireVisitorLanesForLink(ids...)
}

func (e *Engine) retireVisitorLane(st *visitorLaneState) {
	_ = e.cancelLaneSession(st.runID, st.producerID, st.lane, true)
	e.revokeLanePageSessions(st.runID, st.producerID, st.lane)
	if vp, ok := e.provider.(runtime.VisitorLaneProvider); ok {
		vp.RetireVisitorLane(st.runID, st.producerID, st.lane)
	}
	logDB(e.db.Model(&models.GateShareVisitorConversation{}).
		Where("link_id = ? AND lane = ?", st.linkID, st.lane).Update("chat_id", ""),
		st.runID, "clear retired visitor chat id")
}

func (e *Engine) sweepVisitorLanesLoop() {
	t := time.NewTicker(visitorSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-t.C:
			e.sweepVisitorLanes(time.Now())
		}
	}
}

// sweepVisitorLanes retires lanes whose link no longer accepts replies, whose
// node session is gone, or which sat idle past VisitorLaneIdleTTL.
func (e *Engine) sweepVisitorLanes(now time.Time) {
	e.visitorMu.Lock()
	states := make([]*visitorLaneState, 0, len(e.visitorLanes))
	linkIDs := map[string]bool{}
	for _, st := range e.visitorLanes {
		cp := *st
		states = append(states, &cp)
		linkIDs[st.linkID] = true
	}
	e.visitorMu.Unlock()
	if len(states) == 0 {
		return
	}
	ids := make([]string, 0, len(linkIDs))
	for id := range linkIDs {
		ids = append(ids, id)
	}
	var links []models.GateShareLink
	if err := e.db.Where("id IN ?", ids).Find(&links).Error; err != nil {
		return
	}
	open := map[string]bool{}
	for _, l := range links {
		open[l.ID] = l.RevokedAt == nil && l.UsedAt == nil && now.Before(l.ExpiresAt)
	}
	for _, st := range states {
		busy := false
		if snap, ok := e.VisitorSessionSnapshot(st.runID, st.producerID, st.lane); ok {
			busy = snap.Busy || snap.Waiting > 0
		}
		retire := !open[st.linkID] || !e.HasLiveReviewSession(st.runID, st.producerID) ||
			(!busy && now.Sub(st.lastActive) > VisitorLaneIdleTTL)
		if !retire {
			continue
		}
		key := e.laneSessionKey(st.runID, st.producerID, st.lane)
		e.visitorMu.Lock()
		cur := e.visitorLanes[key]
		if cur == nil || (open[st.linkID] && !cur.lastActive.Equal(st.lastActive)) {
			e.visitorMu.Unlock()
			continue
		}
		delete(e.visitorLanes, key)
		e.visitorMu.Unlock()
		e.retireVisitorLane(st)
	}
}

func (e *Engine) loadOrSeedVisitorConversation(s *reviewSession) (*models.GateShareVisitorConversation, error) {
	var row models.GateShareVisitorConversation
	err := e.db.Where("link_id = ? AND lane = ?", s.linkID, s.lane).First(&row).Error
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	history := e.defaultLaneTurns(s.runID, s.producerID)
	row = models.GateShareVisitorConversation{
		LinkID: s.linkID, Lane: s.lane, RunID: s.runID, NodeID: s.producerID,
		Messages: append([]models.ReactMessage(nil), history...), HistoryLen: len(history),
		LastActiveAt: time.Now(),
	}
	if err := e.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// executeVisitorTurn runs one visitor message on the lane's own chat and
// records it in the visitor's transcript. The node's own dialogue, clarify
// state and Live sessions are never touched; product edits land in the shared
// workspace and refresh outputs like a review turn.
func (e *Engine) executeVisitorTurn(ctx context.Context, s *reviewSession, item *reviewQueueItem) (interrupted bool, err error) {
	vp, ok := e.provider.(runtime.VisitorLaneProvider)
	if !ok {
		return false, errors.New("当前执行后端不支持访客对话")
	}
	c, err := e.loadCtx(s.runID)
	if err != nil {
		return false, err
	}
	producer := c.graph.FindNode(s.producerID)
	if producer == nil {
		return false, errors.New("上游生产节点不存在")
	}
	conv, err := e.loadOrSeedVisitorConversation(s)
	if err != nil {
		return false, err
	}
	prelude := e.visitorPrelude(c, producer, conv.Messages, conv.HistoryLen)
	conv.Messages = append(conv.Messages, models.ReactMessage{
		Role: "human", Text: item.Text, At: time.Now().Format(time.RFC3339),
		Images: item.Images, Annotations: item.Annotations,
	})
	conv.LastActiveAt = time.Now()
	logDB(e.db.Save(conv), s.runID, "save visitor human turn")
	e.touchVisitorLane(s.runID, s.producerID, s.lane)

	s.mu.Lock()
	s.liveEvents = nil
	s.mu.Unlock()
	req := e.nodeReq(c, producer)
	t := vp.VisitorTurn(ctx, req, s.lane, prelude, withPageSession(item.PageSession, item.Effective), item.Images, func(events []models.AcpEvent, busy bool) {
		s.mu.Lock()
		s.liveEvents = events
		s.mu.Unlock()
		e.publishVisitorAcp(s, events, busy)
	})

	s.mu.Lock()
	cancelled := s.cancelRequested || ctx.Err() != nil
	s.liveEvents = nil
	s.mu.Unlock()
	if cancelled || t.Err != nil {
		interrupted = true
	}
	for _, text := range t.Handoffs {
		conv.Messages = append(conv.Messages, models.ReactMessage{
			Role: "agent", Text: text, At: time.Now().Format(time.RFC3339), Handoff: true,
		})
	}
	agentMsg := models.ReactMessage{
		Role: "agent", Text: t.Msg, At: time.Now().Format(time.RFC3339),
		Interrupted: interrupted, OpID: t.OpID, Tools: models.ToolsFromEvents(t.Events),
	}
	if interrupted && strings.TrimSpace(agentMsg.Text) == "" {
		agentMsg.Text = "(已中断)"
	}
	conv.Messages = append(conv.Messages, agentMsg)
	conv.LastActiveAt = time.Now()
	if ci, ok := e.provider.(visitorChatIDs); ok {
		conv.ChatID = ci.VisitorChatID(s.runID, s.producerID, s.lane)
	}
	logDB(e.db.Save(conv), s.runID, "save visitor agent turn")
	e.touchVisitorLane(s.runID, s.producerID, s.lane)

	e.flushMcpCalls(s.runID, s.producerID)
	e.flushTokenUsage(s.runID, s.producerID, t.Usage, t.UsageByModel)

	if interrupted {
		if !cancelled {
			log.Warn().Err(t.Err).Str("run_id", s.runID).Str("producer", s.producerID).
				Str("lane", s.lane).Msg("visitor turn failed")
		}
		return true, nil
	}
	if nodereg.ClarifyInteractive(producer.Type) {
		return false, nil
	}
	unlock := e.lockResume(s.runID + ":" + s.producerID)
	defer unlock()
	c2, err := e.loadCtx(s.runID)
	if err != nil {
		return false, nil
	}
	if p := c2.graph.FindNode(s.producerID); p != nil {
		e.refreshProducerOutputs(c2, p)
	}
	if item.Source == "gate" && item.GateNodeID != "" {
		e.refreshGateBodyAfterRevise(c2, item.GateNodeID)
		e.broker.Publish(s.runID, jsonMsg("artifact_edit", s.runID, item.GateNodeID))
	} else {
		e.refreshPendingGatesForProducer(c2, s.producerID)
		e.broker.Publish(s.runID, jsonMsg("artifact_edit", s.runID, s.producerID))
	}
	return false, nil
}

func (e *Engine) publishVisitorAcp(s *reviewSession, events []models.AcpEvent, busy bool) {
	msg, err := json.Marshal(map[string]any{
		"type": "visitor", "kind": "acp", "lane": s.lane,
		"runId": s.runID, "nodeId": s.producerID, "events": events, "busy": busy,
	})
	if err != nil {
		return
	}
	e.broker.Publish(s.runID, msg)
}

// visitorPrelude is sent once per fresh visitor chat so the new Agent context
// knows the task, the current product, and the conversation so far.
func (e *Engine) visitorPrelude(c *execCtx, node *models.Node, msgs []models.ReactMessage, historyLen int) string {
	if historyLen > len(msgs) {
		historyLen = len(msgs)
	}
	var b strings.Builder
	b.WriteString("# 访客对话说明\n")
	b.WriteString("你正在与一位通过临时审批链接访问的访客单独对话。其他访客各有独立会话,彼此不可见;")
	b.WriteString("工作区与产物和本节点共享,可按访客意见就地修改。确认或驳回由访客在页面上操作,不要自行结束会话。\n")
	if task := strings.TrimSpace(str(node.Config["prompt"])); task != "" {
		b.WriteString("\n## 节点任务\n")
		b.WriteString(clipRunes(e.interpolate(c, task), visitorPreludeBlockChars))
		b.WriteString("\n")
	}
	b.WriteString("\n## 当前产物\n")
	b.WriteString(clipRunes(e.reviewSummaryMarkdown(c, node), visitorPreludeBlockChars))
	b.WriteString("\n")
	history := tailTurns(msgs[:historyLen], visitorPreludeHistoryTurns)
	own := tailTurns(msgs[historyLen:], visitorPreludeOwnTurns)
	if len(history) > 0 {
		b.WriteString("\n## 节点此前的对话(节选)\n")
		writePreludeTurns(&b, history)
	}
	if len(own) > 0 {
		b.WriteString("\n## 你与该访客此前的对话\n")
		writePreludeTurns(&b, own)
	}
	return b.String()
}

func tailTurns(msgs []models.ReactMessage, n int) []models.ReactMessage {
	var out []models.ReactMessage
	for _, m := range msgs {
		if m.Handoff || strings.TrimSpace(m.Text) == "" {
			continue
		}
		out = append(out, m)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func writePreludeTurns(b *strings.Builder, msgs []models.ReactMessage) {
	for _, m := range msgs {
		who := "Agent"
		if m.Role == "human" {
			who = "用户"
		}
		fmt.Fprintf(b, "**%s**: %s\n\n", who, clipRunes(strings.TrimSpace(m.Text), visitorPreludeTurnChars))
	}
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
