package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// ErrLiveOpen blocks 确认并流转 while Live variant previews are still in
// source (a session not yet accepted or discarded, or leftover markers).
var ErrLiveOpen = errors.New("还有未采用或放弃的 Live 变体,请先在预览页采用或放弃(或选择「全部放弃」)后再确认")

// ErrLiveScanFailed blocks confirm when Live is on but the worktree cannot
// be scanned. Fail closed so leftover data-grasp-live markers cannot ship.
var ErrLiveScanFailed = errors.New("无法确认预览页 Live 标记已清除,请稍后重试确认")

// ErrLiveDisabled means the node is not an IP-direct app_preview with Live on.
var ErrLiveDisabled = errors.New("该节点未开启 Live 变体")

const liveScanTimeout = 40 * time.Second

// LiveEnabled reports whether nodeID on runID runs Live variants: an
// app_preview with direct_preview on and live_variants not switched off
// (default on, matching runtime.liveVariantsEnabled).
func (e *Engine) LiveEnabled(runID, nodeID string) bool {
	c, err := e.loadCtx(runID)
	if err != nil {
		return false
	}
	n := c.graph.FindNode(nodeID)
	if n == nil || n.Type != "app_preview" || n.Config == nil {
		return false
	}
	if !configTruthyAny(n.Config["direct_preview"]) {
		return false
	}
	v := n.Config["live_variants"]
	if s, ok := v.(string); v == nil || (ok && strings.TrimSpace(s) == "") {
		return true
	}
	return configTruthyAny(v)
}

// LiveSessions lists a node's Live sessions, newest first. openOnly keeps
// sessions that are not accepted, discarded or done.
func (e *Engine) LiveSessions(runID, nodeID string, openOnly bool) []models.LiveSession {
	var out []models.LiveSession
	q := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID)
	if openOnly {
		q = q.Where("state NOT IN ?", []string{models.LiveStateAccepted, models.LiveStateDiscarded, models.LiveStateDone})
	}
	if err := q.Order("created_at desc").Limit(50).Find(&out).Error; err != nil {
		log.Warn().Err(err).Str("run", runID).Str("node", nodeID).Msg("list live sessions")
		return nil
	}
	return out
}

func (e *Engine) liveSession(runID, nodeID, sid string) (*models.LiveSession, error) {
	var s models.LiveSession
	err := e.db.Where("id = ? AND run_id = ? AND node_id = ?", sid, runID, nodeID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// publishLive tells every run subscriber (and drawers, via the public filter)
// where a session stands.
func (e *Engine) publishLive(sess *models.LiveSession) {
	if sess == nil {
		return
	}
	b, err := json.Marshal(map[string]any{
		"type": "live", "runId": sess.RunID, "nodeId": sess.NodeID, "session": sess,
	})
	if err != nil {
		return
	}
	e.broker.Publish(sess.RunID, b)
}

// reviewConvOpen checks the node has a live (not finished) review dialogue.
func (e *Engine) reviewConvOpen(runID, nodeID string) error {
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return errors.New("no react conversation")
	}
	if conv.Done {
		return errors.New("react already done")
	}
	return nil
}

// ReactLiveAs handles one Live request from owner: it moves the session's
// state, then queues the request as a review turn for the parked agent. A
// repeated request (double click, refresh) returns the session unchanged.
func (e *Engine) ReactLiveAs(owner, runID, nodeID string, ev models.LiveEvent) (*models.LiveSession, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	if err := ev.Normalize(); err != nil {
		return nil, err
	}
	if !e.LiveEnabled(runID, nodeID) {
		return nil, ErrLiveDisabled
	}
	if err := e.reviewConvOpen(runID, nodeID); err != nil {
		return nil, err
	}

	e.liveMu.Lock()
	defer e.liveMu.Unlock()

	cur, err := e.liveSession(runID, nodeID, ev.SID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		var other models.LiveSession
		if e.db.Where("id = ?", ev.SID).First(&other).Error == nil {
			return nil, errors.New("Live 会话 id 已被使用")
		}
	}
	state := ""
	if cur != nil {
		state = cur.State
	}
	next, err := models.NextLiveState(state, ev.Op)
	if errors.Is(err, models.ErrLiveDuplicate) {
		return cur, nil
	}
	if err != nil {
		return nil, err
	}
	if cur == nil && ev.Op != models.LiveOpSteer {
		for _, o := range e.LiveSessions(runID, nodeID, true) {
			if o.Mode != "steer" {
				return nil, fmt.Errorf("还有一个未完成的 Live 变体(%s),请先采用或放弃", o.Summary)
			}
		}
	}

	var prev *models.LiveSession
	sess := cur
	if sess == nil {
		sess = &models.LiveSession{
			ID: ev.SID, RunID: runID, NodeID: nodeID, Owner: owner,
			Action: ev.Action, Prompt: ev.Prompt, Count: ev.Count, URL: ev.URL,
		}
		switch ev.Op {
		case models.LiveOpInsert:
			sess.Mode = "insert"
		case models.LiveOpSteer:
			sess.Mode = "steer"
		default:
			sess.Mode = "replace"
		}
		if ev.Element != nil {
			sess.Selector = ev.Element.Selector
			sess.Summary = models.LiveSummary(ev.Element)
		}
	} else {
		cp := *cur
		prev = &cp
	}
	sess.State = next
	sess.Error = ""
	switch ev.Op {
	case models.LiveOpAccept:
		sess.Selected = ev.Variant
	case models.LiveOpMountFailed:
		sess.Error = ev.Error
	}
	if err := e.db.Save(sess).Error; err != nil {
		return nil, err
	}

	item := &reviewQueueItem{
		ID:        uuid.NewString(),
		Text:      models.LiveEventText(ev, sess),
		Effective: models.RenderLiveEvent(ev, sess),
		Source:    "node",
		Owner:     owner,
		Live:      &models.LiveRef{SID: sess.ID, Op: ev.Op, Variant: ev.Variant},
	}
	if _, err := e.enqueueReviewItem(runID, nodeID, sessionKindReview, item); err != nil {
		if prev != nil {
			e.db.Save(prev)
		} else {
			e.db.Delete(&models.LiveSession{}, "id = ?", sess.ID)
		}
		return nil, err
	}
	e.installLiveGuard(runID, nodeID)
	e.publishLive(sess)
	return sess, nil
}

// ReactReplyLiveCtxAs sends a plain chat message while a Live session is
// open: the prompt tells the agent which variant "this" means.
func (e *Engine) ReactReplyLiveCtxAs(owner, runID, nodeID, text string, images []models.PromptImage, annotations []models.ReactAnnotation, ctx *models.LiveCtx) error {
	if ctx == nil || !models.ValidLiveSID(ctx.SID) {
		return e.ReactReplyAs(owner, runID, nodeID, text, images, annotations, false)
	}
	sess, err := e.liveSession(runID, nodeID, ctx.SID)
	if err != nil || sess == nil || !sess.Open() {
		return e.ReactReplyAs(owner, runID, nodeID, text, images, annotations, false)
	}
	if strings.TrimSpace(text) == "" && len(images) == 0 && len(annotations) == 0 {
		return errors.New("text, images, or annotations required")
	}
	if err := e.reviewConvOpen(runID, nodeID); err != nil {
		return err
	}
	images, err = blob.IngestPromptImages(context.Background(), e.blobs, images)
	if err != nil {
		return fmt.Errorf("ingest attachments: %w", err)
	}
	effective := renderReviewHuman(text, annotations)
	if hint := models.RenderLiveCtx(*ctx, sess); hint != "" {
		effective = hint + "\n" + effective
	}
	_, err = e.enqueueReviewItem(runID, nodeID, sessionKindReview, &reviewQueueItem{
		ID: uuid.NewString(), Text: text, Effective: effective,
		Images: images, Annotations: annotations, Source: "node", Owner: owner,
	})
	return err
}

// DiscardAllLiveAs queues a discard for every open Live session on the node.
func (e *Engine) DiscardAllLiveAs(owner, runID, nodeID string) (int, error) {
	n := 0
	for _, s := range e.LiveSessions(runID, nodeID, true) {
		if s.State == models.LiveStateDiscarding || s.Mode == "steer" {
			continue
		}
		if _, err := e.ReactLiveAs(owner, runID, nodeID, models.LiveEvent{Op: models.LiveOpDiscard, SID: s.ID}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ApplyLiveReport applies a live_update call from the node's agent
// (implements mcp.LiveUpdater).
func (e *Engine) ApplyLiveReport(runID, nodeID string, u mcp.LiveReport) (*models.LiveSession, error) {
	u.SID = strings.TrimSpace(u.SID)
	u.State = strings.TrimSpace(u.State)
	if !models.ValidLiveSID(u.SID) {
		return nil, errors.New("session_id 无效")
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	sess, err := e.liveSession(runID, nodeID, u.SID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("没有 Live 会话 %s", u.SID)
	}
	if err := models.CheckLiveReport(sess.State, u.State); err != nil {
		return nil, err
	}
	if u.Variants != nil {
		vs, err := models.NormalizeLiveVariants(u.Variants)
		if err != nil {
			return nil, err
		}
		sess.Variants = vs
	}
	if f := strings.TrimSpace(u.File); f != "" {
		sess.File = clipString(f, 512)
	}
	sess.State = u.State
	sess.Error = ""
	if u.State == models.LiveStateFailed {
		sess.Error = clipString(strings.TrimSpace(u.Error), 2000)
		if sess.Error == "" {
			sess.Error = "Agent 未能完成"
		}
	}
	if u.State == models.LiveStateReady && len(sess.Variants) == 0 && sess.Mode != "steer" {
		return nil, errors.New("state=ready 时需要 variants(每个变体的编号与标签)")
	}
	if err := e.db.Save(sess).Error; err != nil {
		return nil, err
	}
	e.publishLive(sess)
	return sess, nil
}

// settleLiveAfterTurn resolves a session the agent left mid-transition (it
// never called live_update) by looking at the markers in source.
func (e *Engine) settleLiveAfterTurn(runID, nodeID, sid string, interrupted bool) {
	pendingState := func() *models.LiveSession {
		sess, err := e.liveSession(runID, nodeID, sid)
		if err != nil || sess == nil {
			return nil
		}
		switch sess.State {
		case models.LiveStateGenerating, models.LiveStateRefining, models.LiveStateAccepting, models.LiveStateDiscarding:
			return sess
		}
		return nil
	}
	e.liveMu.Lock()
	before := pendingState()
	e.liveMu.Unlock()
	if before == nil {
		return
	}
	// Scan without the lock: it is a sandbox round trip.
	present, scanned := e.liveMarkerPresent(runID, nodeID, sid)

	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	sess := pendingState()
	if sess == nil || sess.State != before.State || !sess.UpdatedAt.Equal(before.UpdatedAt) {
		return // moved on (a live_update or a new request) while we scanned
	}
	next, msg := settleLiveState(sess, present, scanned, interrupted)
	sess.State = next
	sess.Error = msg
	logDB(e.db.Save(sess), runID, "settle live session")
	e.publishLive(sess)
}

// settleLiveState is the pure decision behind settleLiveAfterTurn.
func settleLiveState(sess *models.LiveSession, present, scanned, interrupted bool) (string, string) {
	if !scanned {
		return models.LiveStateFailed, "Agent 没有报告结果,且无法检查源码中的标记"
	}
	switch sess.State {
	case models.LiveStateGenerating, models.LiveStateRefining:
		if sess.Mode == "steer" {
			return models.LiveStateDone, ""
		}
		if present && !interrupted && len(sess.Variants) > 0 {
			return models.LiveStateReady, ""
		}
		if present {
			return models.LiveStateFailed, "Agent 没有报告变体列表;可以放弃后重试"
		}
		if interrupted {
			return models.LiveStateFailed, "本轮已中断,没有生成变体"
		}
		return models.LiveStateFailed, "Agent 没有写入变体"
	case models.LiveStateAccepting:
		if present {
			return models.LiveStateFailed, "采用后源码里仍有预览标记"
		}
		return models.LiveStateAccepted, ""
	default: // discarding
		if present {
			return models.LiveStateFailed, "放弃后源码里仍有预览标记"
		}
		return models.LiveStateDiscarded, ""
	}
}

// liveMarkerPresent scans the parked worktree; scanned=false when it could not.
func (e *Engine) liveMarkerPresent(runID, nodeID, sid string) (present, scanned bool) {
	sc, ok := e.provider.(runtime.LiveMarkerScanner)
	if !ok {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveScanTimeout)
	defer cancel()
	sids, parked, err := sc.LiveMarkerSIDs(ctx, runID, nodeID)
	if err != nil || !parked {
		return false, false
	}
	for _, s := range sids {
		if s == sid {
			return true, true
		}
	}
	return false, true
}

func (e *Engine) installLiveGuard(runID, nodeID string) {
	sc, ok := e.provider.(runtime.LiveMarkerScanner)
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), liveScanTimeout)
		defer cancel()
		sc.InstallLiveGuard(ctx, runID, nodeID)
	}()
}

// checkLiveClosed is the confirm gate: no open session, no marker in source.
// Scan errors and a missing scanner fail closed while Live is enabled.
func (e *Engine) checkLiveClosed(runID, nodeID string) error {
	if !e.LiveEnabled(runID, nodeID) {
		return nil
	}
	if len(e.LiveSessions(runID, nodeID, true)) > 0 {
		return ErrLiveOpen
	}
	sc, ok := e.provider.(runtime.LiveMarkerScanner)
	if !ok {
		return ErrLiveScanFailed
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveScanTimeout)
	defer cancel()
	sids, parked, err := sc.LiveMarkerSIDs(ctx, runID, nodeID)
	if err != nil || !parked {
		return ErrLiveScanFailed
	}
	if len(sids) > 0 {
		return ErrLiveOpen
	}
	return nil
}

func clipString(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
