package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/rs/zerolog/log"
)

const (
	pmTurnBufferMax = 2048
	// pmDefaultTurnDeadline is the fallback per-turn ctx deadline when the
	// runner has no explicit deadline configured (see SetTurnDeadline).
	pmDefaultTurnDeadline = 10 * time.Minute
	// pmPrepareTimeout bounds sandbox boot for web turns; a cold image pull
	// takes minutes and must not count against the chat deadline.
	pmPrepareTimeout = 20 * time.Minute
	pmQueueCapacity  = 8
)

// PM turn phases, reported in queue_state and phase frames while busy.
const (
	PmPhasePreparing = "preparing"
	PmPhaseRunning   = "running"
)

// PmEventPhase is the session event announcing a phase change.
const PmEventPhase = "phase"

var errPmTurnDuplicate = errors.New("该消息已在处理中")

// PmTurnEvent is one frame of a thread's chat stream.
type PmTurnEvent = chatsession.Event

// PmTurnRequest is one queued PM turn.
type PmTurnRequest struct {
	UserMsgID string
	// Text is shown for the item in queue_state; defaults to Prompt.
	Text   string
	Images []models.PromptImage
	// SandboxID and Prompt are used as-is when Prepare is nil.
	SandboxID uint
	Prompt    string
	// Timeout overrides the chat turn cap (0 → runner default).
	Timeout time.Duration
	// Prepare readies the sandbox and builds the prompt once the turn starts;
	// setPhase reports boot progress (e.g. "pulling").
	Prepare func(ctx context.Context, setPhase func(string)) (sandboxID uint, prompt string, err error)
}

type pmChatter interface {
	ChatWithTimeout(ctx context.Context, id uint, text string, images []models.PromptImage, timeout time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error)
	Cancel(id uint)
}

// PmTurnRunner runs PM consult turns: one chatsession FIFO per thread, shared
// by the web UI, IM channels, cron and gate-auto. Turns run on a background
// context; viewers subscribe and get a snapshot plus the active turn's frames.
type PmTurnRunner struct {
	pm   *PmService
	chat pmChatter
	// Optional deps for progress-citation existence checks (fail-closed when nil).
	runs *RunService
	arts *ArtifactService
	wf   *WorkflowService

	mu           sync.Mutex
	threads      map[string]*pmThread
	turnDeadline time.Duration
}

type pmThread struct {
	r    *PmTurnRunner
	id   string
	sess *chatsession.Session[*PmTurnRequest]
	// refs counts callers and subscribers using the thread; guarded by r.mu.
	refs int

	stream *chatsession.Stream

	mu        sync.Mutex
	userMsgID string
	partial   string
	phase     string
	sandboxID uint
	failKind  string
}

// NewPmTurnRunner builds a runner. The default per-turn deadline is
// pmDefaultTurnDeadline; override at boot with SetTurnDeadline so long
// channel/cron turns are not truncated.
func NewPmTurnRunner(pm *PmService, sbx *SandboxService) *PmTurnRunner {
	r := &PmTurnRunner{
		pm:           pm,
		threads:      make(map[string]*pmThread),
		turnDeadline: pmDefaultTurnDeadline,
	}
	if sbx != nil {
		r.chat = sbx
	}
	return r
}

// SetChatterForTest replaces the sandbox chat backend.
func (r *PmTurnRunner) SetChatterForTest(c pmChatter) {
	r.chat = c
}

// SetTurnDeadline configures the default per-turn ctx deadline (values <= 0 are
// ignored). Typically set to AgentChatTimeout()+buffer at boot.
func (r *PmTurnRunner) SetTurnDeadline(d time.Duration) {
	if d <= 0 {
		return
	}
	r.mu.Lock()
	r.turnDeadline = d
	r.mu.Unlock()
}

func (r *PmTurnRunner) defaultDeadline() time.Duration {
	r.mu.Lock()
	d := r.turnDeadline
	r.mu.Unlock()
	if d <= 0 {
		return pmDefaultTurnDeadline
	}
	return d
}

// DefaultDeadline reports the runner's default per-turn ctx deadline. Callers
// that poll for turn completion (e.g. the channel bridge) should derive their
// wait budget from this so they never cancel a still-healthy turn.
func (r *PmTurnRunner) DefaultDeadline() time.Duration {
	return r.defaultDeadline()
}

func (r *PmTurnRunner) acquire(threadID string) *pmThread {
	r.mu.Lock()
	defer r.mu.Unlock()
	th := r.threads[threadID]
	if th == nil {
		th = r.newThread(threadID)
		r.threads[threadID] = th
	}
	th.refs++
	return th
}

func (r *PmTurnRunner) release(th *pmThread) {
	r.mu.Lock()
	defer r.mu.Unlock()
	th.refs--
	r.dropIfIdleLocked(th)
}

func (r *PmTurnRunner) dropIfIdleLocked(th *pmThread) {
	if th.refs <= 0 && th.sess.Idle() && r.threads[th.id] == th {
		delete(r.threads, th.id)
	}
}

func (r *PmTurnRunner) lookup(threadID string) *pmThread {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.threads[threadID]
}

func (r *PmTurnRunner) newThread(threadID string) *pmThread {
	th := &pmThread{r: r, id: threadID, stream: chatsession.NewStream(pmTurnBufferMax)}
	th.sess = chatsession.New(chatsession.Config[*PmTurnRequest]{
		Capacity:  pmQueueCapacity,
		FullError: "PM 会话排队已满，请稍候",
		View: func(q *PmTurnRequest) chatsession.ItemView {
			return chatsession.ItemView{ID: q.UserMsgID, Text: q.Text, Images: q.Images}
		},
		Execute: th.execute,
		Publish: th.publishSession,
		TurnBeginExtra: func(q *PmTurnRequest) map[string]any {
			return map[string]any{"userMsgId": q.UserMsgID}
		},
		BeforeTurn: func(q *PmTurnRequest, _ <-chan struct{}) {
			th.mu.Lock()
			th.userMsgID = q.UserMsgID
			th.partial = ""
			th.sandboxID = q.SandboxID
			th.failKind = ""
			th.phase = PmPhaseRunning
			if q.Prepare != nil {
				th.phase = PmPhasePreparing
			}
			th.mu.Unlock()
		},
		OnDropped: func(items []*PmTurnRequest) {
			for _, q := range items {
				r.persistTurnFailure(threadID, q.UserMsgID, PmFailStopped)
			}
		},
		OnIdle: func() {
			r.mu.Lock()
			r.dropIfIdleLocked(th)
			r.mu.Unlock()
		},
		CancelTurn: func() {
			th.mu.Lock()
			sid := th.sandboxID
			th.mu.Unlock()
			if r.chat != nil && sid != 0 {
				r.chat.Cancel(sid)
			}
		},
	})
	return th
}

// Active reports whether the thread has a running or queued turn.
func (r *PmTurnRunner) Active(threadID string) bool {
	th := r.lookup(threadID)
	return th != nil && !th.sess.Ready()
}

// Start queues a turn with the runner's default deadline.
func (r *PmTurnRunner) Start(threadID, userMsgID string, sandboxID uint, prompt string, images []models.PromptImage) error {
	return r.StartWithTimeout(threadID, userMsgID, sandboxID, prompt, images, 0)
}

// StartWithTimeout is Start with a per-call turn deadline override. timeout<=0
// uses the runner default (SetTurnDeadline). A positive timeout caps both the
// turn ctx (timeout + buffer) and the sandbox chat turn (timeout).
func (r *PmTurnRunner) StartWithTimeout(threadID, userMsgID string, sandboxID uint, prompt string, images []models.PromptImage, timeout time.Duration) error {
	_, err := r.Enqueue(threadID, PmTurnRequest{
		UserMsgID: userMsgID,
		SandboxID: sandboxID,
		Prompt:    prompt,
		Images:    images,
		Timeout:   timeout,
	})
	return err
}

// Enqueue appends a turn to the thread FIFO and returns the pending count.
func (r *PmTurnRunner) Enqueue(threadID string, req PmTurnRequest) (int, error) {
	if r.chat == nil || r.pm == nil {
		return 0, errors.New("pm turn runner unavailable")
	}
	if req.Text == "" {
		req.Text = req.Prompt
	}
	q := &req
	th := r.acquire(threadID)
	defer r.release(th)
	return th.sess.Enqueue(q, func(active *PmTurnRequest, hasActive bool, pending []*PmTurnRequest) error {
		if hasActive && active.UserMsgID == q.UserMsgID {
			return errPmTurnDuplicate
		}
		for _, p := range pending {
			if p.UserMsgID == q.UserMsgID {
				return errPmTurnDuplicate
			}
		}
		return nil
	})
}

// Cancel stops the active turn and drops queued ones (marked stopped).
func (r *PmTurnRunner) Cancel(threadID string) {
	if th := r.lookup(threadID); th != nil {
		th.sess.Cancel(true)
	}
}

// Subscribe streams the thread: first a queue_state snapshot, then (when busy)
// the active turn's frames after afterSeq, then live frames. The channel stays
// open across turns until the returned unsubscribe func is called.
func (r *PmTurnRunner) Subscribe(threadID string, afterSeq int) (<-chan PmTurnEvent, func(), bool) {
	th := r.acquire(threadID)
	ch, unsubStream := th.stream.Subscribe(func() (map[string]any, bool) {
		sn := th.sess.Snapshot()
		qs := sn.QueueState()
		th.mu.Lock()
		th.decorateQueueLocked(qs)
		th.mu.Unlock()
		return qs, sn.Busy
	}, afterSeq)
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			unsubStream()
			r.release(th)
		})
	}
	return ch, unsub, true
}

// Status returns the active turn's progress.
func (r *PmTurnRunner) Status(threadID string) (active bool, userMsgID string, partial string, chunkIndex, eventSeq int) {
	th := r.lookup(threadID)
	if th == nil {
		return false, "", "", 0, -1
	}
	_, busy := th.sess.Active()
	th.mu.Lock()
	defer th.mu.Unlock()
	return busy, th.userMsgID, th.partial, 0, th.stream.LastSeq()
}

func (th *pmThread) decorateQueueLocked(payload map[string]any) {
	if busy, _ := payload["busy"].(bool); busy && th.phase != "" {
		payload["phase"] = th.phase
		payload["userMsgId"] = th.userMsgID
	}
}

func (th *pmThread) publishSession(event string, payload map[string]any) {
	keep := chatsession.KeepNone
	th.mu.Lock()
	switch event {
	case chatsession.EventQueueState:
		th.decorateQueueLocked(payload)
	case chatsession.EventTurnBegin:
		keep = chatsession.KeepBegin
	case chatsession.EventTurnDone, chatsession.EventError:
		payload["userMsgId"] = th.userMsgID
		if th.failKind != "" {
			payload["failKind"] = th.failKind
		}
		th.sandboxID = 0
		th.phase = ""
		keep = chatsession.KeepEnd
	}
	th.mu.Unlock()
	th.stream.Session(event, payload, keep)
}

func (th *pmThread) setPhase(phase string) {
	th.mu.Lock()
	if phase == "" || th.phase == phase {
		th.mu.Unlock()
		return
	}
	th.phase = phase
	th.mu.Unlock()
	th.stream.Session(PmEventPhase, map[string]any{"phase": phase}, chatsession.KeepNone)
}

func (th *pmThread) onAcp(raw json.RawMessage) {
	delta := extractPmAgentText(raw)
	th.mu.Lock()
	th.partial += delta
	th.mu.Unlock()
	th.stream.Acp(raw)
}

func (th *pmThread) fail(q *PmTurnRequest, failKind string) {
	th.mu.Lock()
	th.failKind = failKind
	th.mu.Unlock()
	th.r.persistTurnFailure(th.id, q.UserMsgID, failKind)
}

func (th *pmThread) execute(ctx context.Context, q *PmTurnRequest) (bool, error) {
	r := th.r
	sandboxID, prompt := q.SandboxID, q.Prompt
	if q.Prepare != nil {
		pctx, pcancel := context.WithTimeout(ctx, pmPrepareTimeout)
		sid, p, err := q.Prepare(pctx, th.setPhase)
		pcancel()
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			kind := PmFailSandbox
			if ctx.Err() != nil {
				kind = PmFailStopped
			}
			th.fail(q, kind)
			return false, err
		}
		sandboxID, prompt = sid, p
		th.mu.Lock()
		th.sandboxID = sid
		th.mu.Unlock()
		th.setPhase(PmPhaseRunning)
	}

	ctxTimeout := r.defaultDeadline()
	var chatTimeout time.Duration
	if q.Timeout > 0 {
		chatTimeout = q.Timeout
		ctxTimeout = q.Timeout + 30*time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, ctxTimeout)
	defer cancel()

	// The streaming draft marks an unfinished turn so a restart can fail it.
	if _, err := r.pm.UpsertDraft(th.id, q.UserMsgID, "", PmDraftStreaming, 0, 0, sandboxID); err != nil {
		log.Warn().Err(err).Str("thread", th.id).Str("op", "upsert_draft").Msg("pm turn persist failed")
	}
	usage, usageByModel, err := r.chat.ChatWithTimeout(tctx, sandboxID, prompt, q.Images, chatTimeout, th.onAcp)

	th.mu.Lock()
	partial := th.partial
	th.mu.Unlock()

	if err != nil {
		failKind := PmFailUnknown
		switch {
		case ctx.Err() != nil:
			failKind = PmFailStopped
		case errors.Is(tctx.Err(), context.DeadlineExceeded):
			failKind = PmFailSandbox
		}
		ledgerStatus := models.TokenLedgerStatusFailed
		if failKind == PmFailStopped {
			ledgerStatus = models.TokenLedgerStatusCancelled
		}
		r.recordUsage(th.id, sandboxID, usage, usageByModel, ledgerStatus)
		th.fail(q, failKind)
		return false, err
	}

	text := strings.TrimSpace(partial)
	if text == "" {
		r.recordUsage(th.id, sandboxID, usage, usageByModel, models.TokenLedgerStatusFailed)
		th.fail(q, PmFailEmpty)
		return false, errors.New("empty reply")
	}

	citations := r.filterAndEnrichCitations(th.id, extractPmCitations(text))
	// Persist Usage only on successful finalize. Append failure must not silently
	// count toward project totals (usage stays off the message).
	if _, aerr := r.pm.AppendMessageSource(th.id, "assistant", text, "", citations, nil, nil, usage, usageByModel); aerr != nil {
		log.Warn().Err(aerr).Str("thread", th.id).Msg("pm turn finalize append failed")
		r.recordUsage(th.id, sandboxID, usage, usageByModel, models.TokenLedgerStatusFailed)
		th.fail(q, PmFailUnknown)
		return false, aerr
	}
	r.recordUsage(th.id, sandboxID, usage, usageByModel, models.TokenLedgerStatusOK)
	if _, err := r.pm.UpdateMessageFailure(th.id, q.UserMsgID, "ok", ""); err != nil {
		log.Warn().Err(err).Str("thread", th.id).Str("op", "clear_msg_failure").Msg("pm turn persist failed")
	}
	if err := r.pm.ClearDraft(th.id); err != nil {
		log.Warn().Err(err).Str("thread", th.id).Str("op", "clear_draft").Msg("pm turn persist failed")
	}
	return false, nil
}

// recordUsage ledgers one PM turn's usage regardless of outcome: failed and
// stopped turns still consumed tokens even though no assistant message is kept.
func (r *PmTurnRunner) recordUsage(threadID string, sandboxID uint, usage *models.TokenUsage, byModel models.TokenUsageByModel, status string) {
	if r.pm == nil {
		return
	}
	tokenledger.Record(r.pm.db, tokenledger.Entry{
		Source: models.TokenLedgerSourcePM, Phase: models.TokenLedgerPhaseChat, Status: status,
		ThreadID: threadID, SandboxID: sandboxID, Usage: usage, ByModel: byModel,
	})
}

func (r *PmTurnRunner) persistTurnFailure(threadID, userMsgID, failKind string) {
	if r.pm == nil || userMsgID == "" {
		return
	}
	if err := r.pm.FailDraft(threadID, failKind); err != nil {
		log.Warn().Err(err).Str("thread", threadID).Str("op", "fail_draft").Msg("pm turn persist failed")
	}
	if _, err := r.pm.UpdateMessageFailure(threadID, userMsgID, "failed", failKind); err != nil {
		log.Warn().Err(err).Str("thread", threadID).Str("op", "msg_failure").Msg("pm turn persist failed")
	}
}

// ExtractAgentMessageText pulls agent_message_chunk text from a raw ACP frame.
// Non-message / tool frames return empty so channel Reply can suppress noise.
func ExtractAgentMessageText(raw json.RawMessage) string {
	return extractPmAgentText(raw)
}

// extractPmAgentText pulls agent_message_chunk text from a raw ACP frame.
func extractPmAgentText(raw json.RawMessage) string {
	var envelope map[string]any
	if json.Unmarshal(raw, &envelope) != nil {
		return ""
	}
	ev := envelope
	if op, _ := envelope["op"].(string); op == "event" {
		if data, ok := envelope["data"].(map[string]any); ok {
			ev = data
		}
	} else if data, ok := envelope["data"].(map[string]any); ok {
		if _, hasType := envelope["type"]; !hasType {
			ev = data
		}
	}
	typ, _ := ev["type"].(string)
	if typ != "session_update" {
		return ""
	}
	update, _ := ev["update"].(map[string]any)
	if update == nil {
		return ""
	}
	kind := stringifyKind(update["sessionUpdate"])
	if kind == "" {
		kind = stringifyKind(update["session_update"])
	}
	if kind == "" {
		kind = stringifyKind(update["type"])
	}
	if normalizePmKind(kind) != "agent_message_chunk" {
		return ""
	}
	return contentTextAny(update["content"])
}

func stringifyKind(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func normalizePmKind(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				prev := s[i-1]
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					b.WriteByte('_')
				}
			}
			b.WriteByte(c - 'A' + 'a')
			continue
		}
		if c == '-' {
			b.WriteByte('_')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func contentTextAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var out string
		for _, e := range x {
			out += contentTextAny(e)
		}
		return out
	case map[string]any:
		if t, ok := x["text"].(string); ok {
			return t
		}
		if parts, ok := x["parts"].([]any); ok {
			return contentTextAny(parts)
		}
	}
	return ""
}
