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
	"github.com/cocofhu/grasp/internal/nodereg"
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

// ErrLiveDisabled means the node is not a supported IP-direct node with Live on.
var ErrLiveDisabled = errors.New("该节点未开启 Live 变体")

const liveScanTimeout = 40 * time.Second

// LiveEnabled reports whether this node supports direct-preview Live editing.
func (e *Engine) LiveEnabled(runID, nodeID string) bool {
	c, err := e.loadCtx(runID)
	if err != nil {
		return false
	}
	n := c.graph.FindNode(nodeID)
	return n != nil && models.LiveVariantsEnabled(n.Type, n.Config)
}

// liveQueueKind preserves the node's normal execution contract. Grasp uses
// ReactReply (clarification/force-confirm), while app_preview uses review.
func (e *Engine) liveQueueKind(runID, nodeID string) sessionKind {
	if c, err := e.loadCtx(runID); err == nil {
		if n := c.graph.FindNode(nodeID); n != nil && nodereg.ClarifyInteractive(n.Type) {
			return sessionKindClarify
		}
	}
	return sessionKindReview
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
	return e.ReactLiveWithAttachmentsAs(owner, runID, nodeID, ev, nil, nil)
}

// ReactLiveWithAttachmentsAs preserves attachments and annotations when the
// chat composer sends a Live request instead of an ordinary reply.
func (e *Engine) ReactLiveWithAttachmentsAs(owner, runID, nodeID string, ev models.LiveEvent, images []models.PromptImage, annotations []models.ReactAnnotation) (*models.LiveSession, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	if strings.TrimSpace(ev.Op) == models.LiveOpGenerate && strings.TrimSpace(ev.Scope) == "page" && strings.TrimSpace(ev.Prompt) == "" && (len(images) > 0 || len(annotations) > 0) {
		ev.Prompt = "根据所附图片和标注，在页面中生成可比较的设计候选"
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
		if ev.Op == models.LiveOpSteer && cur.Mode != "steer" {
			return nil, errors.New("整页调整不能复用元素变体会话")
		}
		if ev.Op == models.LiveOpAccept && cur.State == models.LiveStateFailed {
			if !cur.RetryAccept || ev.Variant != cur.Selected {
				return nil, errors.New("只能重试之前未完成的采用目标")
			}
			// The wrapper (and page LiveCtx) may already be gone. Retry the
			// durable adoption request, never the page's later knob values.
			ev.Params = cur.FinalParams
			ev.Retry = true
		}
		if ev.Op == models.LiveOpAccept || (ev.Op == models.LiveOpRefine && ev.Count == 0) {
			if !liveVariantExists(cur, ev.Variant) {
				return nil, errors.New("指定的变体不存在")
			}
		}
		if ev.Op == models.LiveOpDiscard && cur.Mode == "steer" && cur.State == models.LiveStateFailed {
			// Steer has no original-version wrapper to restore. Dismiss only the
			// failed request; keep its partial edits for the human to review.
			cur.State = models.LiveStateDiscarded
			cur.Error = ""
			if err := e.db.Save(cur).Error; err != nil {
				return nil, err
			}
			e.publishLive(cur)
			return cur, nil
		}
	}
	next, err := models.NextLiveState(state, ev.Op)
	if errors.Is(err, models.ErrLiveDuplicate) {
		return cur, nil
	}
	if err != nil {
		return nil, err
	}
	images, err = blob.IngestPromptImages(context.Background(), e.blobs, images)
	if err != nil {
		return nil, fmt.Errorf("ingest attachments: %w", err)
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
		} else if ev.Scope == "page" {
			sess.Summary = "页面候选"
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
		sess.FinalParams = ev.Params
		sess.RetryAccept = true
	case models.LiveOpSteer:
		sess.Prompt = ev.Prompt
		if ev.URL != "" {
			sess.URL = ev.URL
		}
	case models.LiveOpMountFailed:
		sess.Error = ev.Error
		sess.RetryAccept = false
	case models.LiveOpRefine, models.LiveOpDiscard:
		sess.RetryAccept = false
	}
	if err := e.db.Save(sess).Error; err != nil {
		return nil, err
	}

	effective := models.RenderLiveEvent(ev, sess)
	if block := models.RenderAnnotations(annotations); block != "" {
		effective += "\n" + block
	}
	item := &reviewQueueItem{
		ID:          uuid.NewString(),
		Text:        models.LiveEventText(ev, sess),
		Effective:   effective,
		Images:      images,
		Annotations: annotations,
		Source:      "node",
		Owner:       owner,
		Live:        &models.LiveRef{SID: sess.ID, Op: ev.Op, Variant: ev.Variant},
	}
	if _, err := e.enqueueReviewItem(runID, nodeID, e.liveQueueKind(runID, nodeID), item); err != nil {
		if prev != nil {
			e.db.Save(prev)
		} else {
			e.db.Delete(&models.LiveSession{}, "id = ?", sess.ID)
		}
		return nil, err
	}
	e.publishLive(sess)
	return sess, nil
}

// ReactReplyLiveCtxAs sends a plain chat message while a Live session is
// open: the prompt tells the agent which variant "this" means.
func (e *Engine) ReactReplyLiveCtxAs(owner, runID, nodeID, text string, images []models.PromptImage, annotations []models.ReactAnnotation, ctx *models.LiveCtx) error {
	return e.ReactReplyLiveCtxWithPermissionAs(owner, runID, nodeID, text, images, annotations, ctx, true)
}

// ReactReplyLiveCtxWithPermissionAs carries the server-derived Live permission
// through the queue. A react_only comment cannot authorize Live tool writes,
// even when its body includes a forged liveCtx.
func (e *Engine) ReactReplyLiveCtxWithPermissionAs(owner, runID, nodeID, text string, images []models.PromptImage, annotations []models.ReactAnnotation, ctx *models.LiveCtx, allowLive bool) error {
	if e.IsHalted() {
		return errors.New("server is shutting down")
	}
	var sess *models.LiveSession
	var err error
	if ctx != nil && models.ValidLiveSID(ctx.SID) && e.LiveEnabled(runID, nodeID) {
		sess, err = e.liveSession(runID, nodeID, ctx.SID)
		if err != nil {
			return err
		}
		if sess != nil && (!sess.Open() || sess.Mode == "steer") {
			sess = nil
		}
	}
	if sess == nil && allowLive {
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
	item := &reviewQueueItem{
		ID: uuid.NewString(), Text: text, Images: images, Annotations: annotations,
		Source: "node", Owner: owner, LiveWritesDenied: !allowLive,
	}
	if sess != nil {
		if !liveVariantExists(sess, ctx.Current) {
			return errors.New("当前变体不存在")
		}
		params, err := models.NormalizeLiveParams(ctx.Params)
		if err != nil {
			return err
		}
		snapshot := &models.LiveCtx{SID: ctx.SID, Current: ctx.Current, Params: params}
		if allowLive {
			item.LiveChat = snapshot
			item.Live = &models.LiveRef{SID: snapshot.SID, Op: models.LiveOpRefine, Variant: snapshot.Current}
			effective = models.RenderLiveCtx(*snapshot, sess) + "\n" + effective
		} else {
			effective = fmt.Sprintf("## Live 上下文(仅评论)\n当前查看会话 `%s` 的变体 %d。本链接不能操作 Live:不要调用 live_update、采用或放弃变体。\n", sess.ID, snapshot.Current) + effective
		}
	}
	item.Effective = effective
	_, err = e.enqueueReviewItem(runID, nodeID, e.liveQueueKind(runID, nodeID), item)
	return err
}

func liveVariantExists(sess *models.LiveSession, n int) bool {
	for _, v := range sess.Variants {
		if v.N == n {
			return true
		}
	}
	return false
}

// activeLiveChat snapshots authorization from the turn currently running, not
// from a later queued message or mutable page state.
func (e *Engine) activeLiveChat(runID, nodeID string) (*models.LiveCtx, bool) {
	e.reviewMu.Lock()
	s := e.reviewSess[e.reviewSessionKey(runID, nodeID)]
	e.reviewMu.Unlock()
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil, false
	}
	return s.active.LiveChat, s.active.LiveWritesDenied
}

func (e *Engine) failCancelledQueuedLive(runID, nodeID, sid string) {
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	sess, err := e.liveSession(runID, nodeID, sid)
	if err != nil || sess == nil {
		return
	}
	switch sess.State {
	case models.LiveStateGenerating, models.LiveStateRefining, models.LiveStateAccepting, models.LiveStateDiscarding:
		sess.State = models.LiveStateFailed
		sess.Error = "请求尚未执行时已取消;可重试或关闭失败请求"
		if err := e.db.Save(sess).Error; err != nil {
			log.Warn().Err(err).Str("sid", sid).Msg("cancel queued live session")
			return
		}
		e.publishLive(sess)
	}
}

// DiscardAllLiveAs queues a discard for every open Live session on the node.
func (e *Engine) DiscardAllLiveAs(owner, runID, nodeID string) (int, error) {
	n := 0
	for _, s := range e.LiveSessions(runID, nodeID, true) {
		if s.State == models.LiveStateDiscarding || (s.Mode == "steer" && s.State != models.LiveStateFailed) {
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
	chat, denied := e.activeLiveChat(runID, nodeID)
	if denied {
		return nil, errors.New("当前链接权限不允许操作 Live 变体")
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
	if u.State == models.LiveStateAccepting || u.State == models.LiveStateRefining {
		if chat == nil || chat.SID != sess.ID || !liveVariantExists(sess, chat.Current) {
			return nil, errors.New("开始采用或修改需要当前 Chat 消息的 Live 上下文")
		}
		target := chat.Current
		if u.Variant != 0 {
			target = u.Variant
		}
		if !liveVariantExists(sess, target) {
			return nil, errors.New("指定的变体不存在")
		}
		if u.State == models.LiveStateAccepting && target != chat.Current {
			return nil, errors.New("请先切换到要采用的变体并重新发送采用消息,以保留该变体的最终参数")
		}
		if u.State == models.LiveStateAccepting && sess.State == models.LiveStateAccepting {
			if sess.Selected != chat.Current {
				return nil, errors.New("正在采用另一个变体")
			}
			return sess, nil // repeated begin must not replace the frozen knobs
		}
	}
	if err := models.CheckLiveReport(sess.State, u.State); err != nil {
		return nil, err
	}
	if u.State == models.LiveStateDone && sess.Mode != "steer" {
		return nil, errors.New("候选生成必须报告 ready 并等待用户采用或放弃")
	}
	if u.State == models.LiveStateRefining {
		sess.RetryAccept = false
		sess.Selected = chat.Current
		if u.Variant > 0 {
			sess.Selected = u.Variant
		}
	}
	if u.State == models.LiveStateAccepting {
		sess.Selected = chat.Current
		sess.FinalParams = chat.Params
		sess.RetryAccept = true
	}
	fromState := sess.State
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
	if u.State == models.LiveStateAccepted || u.State == models.LiveStateDiscarded || u.State == models.LiveStateReady {
		sess.RetryAccept = false
	}
	sess.Error = ""
	if u.State == models.LiveStateFailed {
		sess.Error = clipString(strings.TrimSpace(u.Error), 2000)
		if sess.Error == "" {
			sess.Error = "Agent 未能完成"
		}
	}
	if u.State == models.LiveStateReady {
		if err := liveReadyVariantsError(sess, fromState); err != nil {
			return nil, err
		}
	}
	if err := e.db.Save(sess).Error; err != nil {
		return nil, err
	}
	e.publishLive(sess)
	return sess, nil
}

// liveReadyVariantsError also guards recovery after a turn without live_update.
// Count is the initial generation request; refine(count) means additional
// variants, so an existing session's refined list may exceed that initial count.
func liveReadyVariantsError(sess *models.LiveSession, fromState string) error {
	if sess.Mode == "steer" {
		return nil
	}
	if len(sess.Variants) == 0 {
		return errors.New("state=ready 时需要 variants(每个变体的编号与标签)")
	}
	if sess.Mode != "replace" || sess.Selector != "" {
		return nil
	}
	if fromState == models.LiveStateFailed {
		return errors.New("页面候选已失败,请先重试修改,或放弃后重新生成")
	}
	if fromState == models.LiveStateGenerating {
		count := sess.Count
		if count == 0 {
			count = 3 // same default as LiveEvent.Normalize
		}
		if count < models.LiveMinVariants || count > models.LiveMaxVariants {
			return errors.New("页面候选请求的变体数量无效")
		}
		if len(sess.Variants) != count {
			return fmt.Errorf("页面候选需要 %d 个变体,实际报告 %d 个;请补齐后重新报告", count, len(sess.Variants))
		}
	} else if len(sess.Variants) < models.LiveMinVariants {
		return errors.New("页面候选至少需要两个变体供用户选择")
	}
	return nil
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
	if !models.LiveStateOpen(next) {
		sess.RetryAccept = false
	}
	logDB(e.db.Save(sess), runID, "settle live session")
	e.publishLive(sess)
}

// settleLiveState is the pure decision behind settleLiveAfterTurn.
func settleLiveState(sess *models.LiveSession, present, scanned, interrupted bool) (string, string) {
	if sess.Mode == "steer" && interrupted {
		return models.LiveStateFailed, "整页调整已中断;可重试或关闭失败请求(保留已有改动)"
	}
	if !scanned {
		return models.LiveStateFailed, "Agent 没有报告结果,且无法检查源码中的标记"
	}
	switch sess.State {
	case models.LiveStateGenerating, models.LiveStateRefining:
		if sess.Mode == "steer" {
			return models.LiveStateDone, ""
		}
		if present && !interrupted && len(sess.Variants) > 0 {
			if err := liveReadyVariantsError(sess, sess.State); err != nil {
				return models.LiveStateFailed, err.Error()
			}
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
		if interrupted {
			return models.LiveStateFailed, "采用已中断;请检查页面和源码后再决定采用或放弃"
		}
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

// Prepare only when this queued generation actually starts: preceding ordinary
// turns may still be cloning the repository. Never initialize during accept or
// confirm, when a leftover marker must not become a new baseline.
func (e *Engine) prepareLiveTurn(ctx context.Context, runID, nodeID string, item *reviewQueueItem) error {
	if item.Live == nil || (item.Live.Op != models.LiveOpGenerate && item.Live.Op != models.LiveOpInsert && item.Live.Op != models.LiveOpSteer) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, liveScanTimeout)
	defer cancel()
	if prep, ok := e.provider.(runtime.LiveBaselinePreparer); ok {
		if err := prep.PrepareLiveBaseline(ctx, runID, nodeID); err != nil {
			return err
		}
	}
	if sc, ok := e.provider.(runtime.LiveMarkerScanner); ok {
		sc.InstallLiveGuard(ctx, runID, nodeID)
	}
	return nil
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
	// A Grasp dialogue may register a preview without ever editing source.
	// Do not make ordinary clarification depend on a live sandbox merely
	// because direct_preview was enabled. Once Live has been used, retain the
	// same fail-closed scan as app_preview, including terminal sessions.
	if e.liveQueueKind(runID, nodeID) == sessionKindClarify {
		var count int64
		if err := e.db.Model(&models.LiveSession{}).Where("run_id = ? AND node_id = ?", runID, nodeID).Count(&count).Error; err != nil {
			return ErrLiveScanFailed
		}
		if count == 0 {
			return nil
		}
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
