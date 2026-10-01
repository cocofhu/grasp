package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/rs/zerolog/log"
)

const (
	pmTurnBufferMax = 2048
	// pmDefaultTurnDeadline is the fallback per-turn ctx deadline when the
	// runner has no explicit deadline configured (see SetTurnDeadline).
	pmDefaultTurnDeadline = 10 * time.Minute
)

// PmTurnEvent is one buffered event for WS catch-up / live fan-out.
type PmTurnEvent struct {
	Seq      int             `json:"seq"`
	Type     string          `json:"type"` // acp | turn_done | error
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
	FailKind string          `json:"failKind,omitempty"`
}

type turnSub struct {
	ch   chan PmTurnEvent
	once sync.Once
}

func (s *turnSub) Close() {
	s.once.Do(func() { close(s.ch) })
}

type pmActiveTurn struct {
	threadID  string
	userMsgID string
	sandboxID uint
	cancel    context.CancelFunc
	// chatTimeout overrides the sandbox chat idle/overall cap for this turn
	// (0 → sandbox default). Set for channel turns with a longer deadline.
	chatTimeout time.Duration

	mu         sync.Mutex
	events     []PmTurnEvent
	nextSeq    int
	partial    string
	chunkIndex int
	done       bool
	errMsg     string
	subs       map[*turnSub]struct{}
}

// PmTurnRunner runs PM consult turns decoupled from a single WS request ctx.
// Events are buffered and fanned out so reconnecting clients can catch up.
type PmTurnRunner struct {
	pm  *PmService
	sbx *SandboxService
	// Optional deps for progress-citation existence checks (fail-closed when nil).
	runs *RunService
	arts *ArtifactService
	wf   *WorkflowService

	mu           sync.Mutex
	turns        map[string]*pmActiveTurn // keyed by threadID
	turnDeadline time.Duration            // default per-turn ctx deadline
}

// NewPmTurnRunner builds a runner. The default per-turn deadline is
// pmDefaultTurnDeadline; override at boot with SetTurnDeadline so long
// channel/cron turns are not truncated.
func NewPmTurnRunner(pm *PmService, sbx *SandboxService) *PmTurnRunner {
	return &PmTurnRunner{
		pm:           pm,
		sbx:          sbx,
		turns:        make(map[string]*pmActiveTurn),
		turnDeadline: pmDefaultTurnDeadline,
	}
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

// Active reports whether a live in-process turn exists for the thread.
// Done turns that have not been GC'd yet return false (not live for resume).
func (r *PmTurnRunner) Active(threadID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turns[threadID]
	return t != nil && !t.isDone()
}

// ForceActiveForTest inserts a non-done in-memory turn for handler tests.
func (r *PmTurnRunner) ForceActiveForTest(threadID, userMsgID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns[threadID] = &pmActiveTurn{
		threadID:  threadID,
		userMsgID: userMsgID,
		cancel:    func() {},
		subs:      make(map[*turnSub]struct{}),
	}
}

func (t *pmActiveTurn) isDone() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done
}

// Start launches a background Chat turn with the runner's default deadline.
// Returns error if a turn is already running.
func (r *PmTurnRunner) Start(threadID, userMsgID string, sandboxID uint, prompt string, images []models.PromptImage) error {
	return r.StartWithTimeout(threadID, userMsgID, sandboxID, prompt, images, 0)
}

// StartWithTimeout is Start with a per-call turn deadline override. timeout<=0
// uses the runner default (SetTurnDeadline). A positive timeout caps both the
// turn ctx (timeout + buffer) and the sandbox chat turn (timeout).
func (r *PmTurnRunner) StartWithTimeout(threadID, userMsgID string, sandboxID uint, prompt string, images []models.PromptImage, timeout time.Duration) error {
	if r.sbx == nil || r.pm == nil {
		return fmt.Errorf("pm turn runner unavailable")
	}
	ctxTimeout := r.defaultDeadline()
	var chatTimeout time.Duration
	if timeout > 0 {
		chatTimeout = timeout
		ctxTimeout = timeout + 30*time.Second
	}
	r.mu.Lock()
	if existing := r.turns[threadID]; existing != nil && !existing.isDone() {
		r.mu.Unlock()
		return fmt.Errorf("turn already running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	t := &pmActiveTurn{
		threadID:    threadID,
		userMsgID:   userMsgID,
		sandboxID:   sandboxID,
		cancel:      cancel,
		chatTimeout: chatTimeout,
		subs:        make(map[*turnSub]struct{}),
	}
	r.turns[threadID] = t
	r.mu.Unlock()

	if _, err := r.pm.UpsertDraft(threadID, userMsgID, "", PmDraftStreaming, 0, 0, sandboxID); err != nil {
		log.Warn().Err(err).Str("thread", threadID).Str("op", "upsert_draft").
			Msg("pm turn persist failed")
	}

	go r.run(ctx, t, prompt, images)
	return nil
}

// recordUsage ledgers one PM turn's usage regardless of outcome: failed and
// stopped turns still consumed tokens even though no assistant message is kept.
func (r *PmTurnRunner) recordUsage(t *pmActiveTurn, usage *models.TokenUsage, byModel models.TokenUsageByModel, status string) {
	if r.pm == nil {
		return
	}
	tokenledger.Record(r.pm.db, tokenledger.Entry{
		Source: models.TokenLedgerSourcePM, Phase: models.TokenLedgerPhaseChat, Status: status,
		ThreadID: t.threadID, SandboxID: t.sandboxID, Usage: usage, ByModel: byModel,
	})
}

func (r *PmTurnRunner) run(ctx context.Context, t *pmActiveTurn, prompt string, images []models.PromptImage) {
	defer t.cancel()

	usage, usageByModel, err := r.sbx.ChatWithTimeout(ctx, t.sandboxID, prompt, images, t.chatTimeout, func(raw json.RawMessage) {
		delta := extractPmAgentText(raw)
		t.mu.Lock()
		if delta != "" {
			t.partial += delta
			t.chunkIndex++
		}
		seq := t.nextSeq
		t.nextSeq++
		ev := PmTurnEvent{Seq: seq, Type: "acp", Data: append(json.RawMessage(nil), raw...)}
		t.events = append(t.events, ev)
		if len(t.events) > pmTurnBufferMax {
			t.events = t.events[len(t.events)-pmTurnBufferMax:]
		}
		partial := t.partial
		chunkIndex := t.chunkIndex
		subs := t.snapshotSubsLocked()
		t.mu.Unlock()

		if err := r.pm.PatchDraftPartial(t.threadID, partial, chunkIndex, seq); err != nil {
			log.Warn().Err(err).Str("thread", t.threadID).Str("op", "patch_draft").
				Msg("pm turn persist failed")
		}
		for _, sub := range subs {
			fanoutEvent(sub, ev)
		}
	})

	t.mu.Lock()
	partial := t.partial
	userMsgID := t.userMsgID
	t.mu.Unlock()

	if err != nil {
		failKind := PmFailUnknown
		switch {
		case ctx.Err() == context.DeadlineExceeded:
			failKind = PmFailSandbox
		case ctx.Err() == context.Canceled:
			failKind = PmFailStopped
		}
		ledgerStatus := models.TokenLedgerStatusFailed
		if failKind == PmFailStopped {
			ledgerStatus = models.TokenLedgerStatusCancelled
		}
		r.recordUsage(t, usage, usageByModel, ledgerStatus)
		msg := err.Error()
		r.finishError(t, msg, failKind)
		return
	}

	text := strings.TrimSpace(partial)
	if text == "" {
		r.recordUsage(t, usage, usageByModel, models.TokenLedgerStatusFailed)
		r.persistTurnFailure(t.threadID, userMsgID, PmFailEmpty)
		r.emitTerminal(t, "error", "empty reply", PmFailEmpty)
		return
	}

	citations := r.filterAndEnrichCitations(t.threadID, extractPmCitations(text))
	// Persist Usage only on successful finalize. Append failure must not silently
	// count toward project totals (usage stays off the message).
	if _, aerr := r.pm.AppendMessageSource(t.threadID, "assistant", text, "", citations, nil, nil, usage, usageByModel); aerr != nil {
		log.Warn().Err(aerr).Str("thread", t.threadID).Msg("pm turn finalize append failed")
		r.recordUsage(t, usage, usageByModel, models.TokenLedgerStatusFailed)
		r.persistTurnFailure(t.threadID, userMsgID, PmFailUnknown)
		r.emitTerminal(t, "error", aerr.Error(), PmFailUnknown)
		return
	}
	r.recordUsage(t, usage, usageByModel, models.TokenLedgerStatusOK)
	if _, err := r.pm.UpdateMessageFailure(t.threadID, userMsgID, "ok", ""); err != nil {
		log.Warn().Err(err).Str("thread", t.threadID).Str("op", "clear_msg_failure").
			Msg("pm turn persist failed")
	}
	if err := r.pm.ClearDraft(t.threadID); err != nil {
		log.Warn().Err(err).Str("thread", t.threadID).Str("op", "clear_draft").
			Msg("pm turn persist failed")
	}
	r.emitTerminal(t, "turn_done", "", "")
}

func (r *PmTurnRunner) persistTurnFailure(threadID, userMsgID, failKind string) {
	if err := r.pm.FailDraft(threadID, failKind); err != nil {
		log.Warn().Err(err).Str("thread", threadID).Str("op", "fail_draft").
			Msg("pm turn persist failed")
	}
	if _, err := r.pm.UpdateMessageFailure(threadID, userMsgID, "failed", failKind); err != nil {
		log.Warn().Err(err).Str("thread", threadID).Str("op", "msg_failure").
			Msg("pm turn persist failed")
	}
}

func (r *PmTurnRunner) finishError(t *pmActiveTurn, msg, failKind string) {
	r.persistTurnFailure(t.threadID, t.userMsgID, failKind)
	r.emitTerminal(t, "error", msg, failKind)
}

func (r *PmTurnRunner) emitTerminal(t *pmActiveTurn, typ, errMsg, failKind string) {
	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return
	}
	t.done = true
	t.errMsg = errMsg
	seq := t.nextSeq
	t.nextSeq++
	ev := PmTurnEvent{Seq: seq, Type: typ, Error: errMsg, FailKind: failKind}
	t.events = append(t.events, ev)
	subs := t.snapshotSubsLocked()
	t.subs = make(map[*turnSub]struct{})
	t.mu.Unlock()

	for _, sub := range subs {
		fanoutEvent(sub, ev)
		sub.Close()
	}

	go func() {
		time.Sleep(2 * time.Minute)
		r.mu.Lock()
		if cur := r.turns[t.threadID]; cur == t {
			delete(r.turns, t.threadID)
		}
		r.mu.Unlock()
	}()
}

func (t *pmActiveTurn) snapshotSubsLocked() []*turnSub {
	out := make([]*turnSub, 0, len(t.subs))
	for sub := range t.subs {
		out = append(out, sub)
	}
	return out
}

// Cancel aborts the in-flight turn for a thread.
func (r *PmTurnRunner) Cancel(threadID string) {
	r.mu.Lock()
	t := r.turns[threadID]
	r.mu.Unlock()
	if t == nil {
		return
	}
	t.cancel()
	if r.sbx != nil && t.sandboxID != 0 {
		r.sbx.Cancel(t.sandboxID)
	}
}

// fanoutEvent delivers one event to a subscriber without silently dropping.
// Catch-up channels are sized to hold the full ring buffer; live path blocks
// briefly so a slow WS write does not skip stream chunks.
func fanoutEvent(sub *turnSub, ev PmTurnEvent) {
	select {
	case sub.ch <- ev:
		return
	default:
	}
	select {
	case sub.ch <- ev:
	case <-time.After(3 * time.Second):
		log.Warn().Int("seq", ev.Seq).Str("type", ev.Type).Msg("pm turn fanout timed out; subscriber may skip a frame")
	}
}

// Subscribe returns a channel of events starting after afterSeq (exclusive).
// Caller must call the returned unsubscribe func.
func (r *PmTurnRunner) Subscribe(threadID string, afterSeq int) (<-chan PmTurnEvent, func(), bool) {
	r.mu.Lock()
	t := r.turns[threadID]
	r.mu.Unlock()
	if t == nil {
		return nil, func() {}, false
	}

	// Buffer covers the full in-memory ring so catch-up never drops under lock.
	ch := make(chan PmTurnEvent, pmTurnBufferMax+8)
	sub := &turnSub{ch: ch}
	t.mu.Lock()
	for _, ev := range t.events {
		if ev.Seq > afterSeq {
			// Channel capacity covers the ring; send cannot block under lock.
			ch <- ev
		}
	}
	done := t.done
	if done {
		t.mu.Unlock()
		close(ch)
		return ch, func() {}, true
	}
	t.subs[sub] = struct{}{}
	t.mu.Unlock()

	unsub := func() {
		t.mu.Lock()
		delete(t.subs, sub)
		t.mu.Unlock()
		sub.Close()
	}
	return ch, unsub, true
}

// Status returns in-memory turn progress when live.
func (r *PmTurnRunner) Status(threadID string) (active bool, userMsgID string, partial string, chunkIndex, eventSeq int) {
	r.mu.Lock()
	t := r.turns[threadID]
	r.mu.Unlock()
	if t == nil {
		return false, "", "", 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return false, t.userMsgID, t.partial, t.chunkIndex, t.nextSeq - 1
	}
	seq := t.nextSeq - 1
	if t.nextSeq == 0 {
		seq = -1
	}
	return true, t.userMsgID, t.partial, t.chunkIndex, seq
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
