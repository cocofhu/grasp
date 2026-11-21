package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func (s *SoloService) lookup(id string) *soloChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chats[id]
}

func (s *SoloService) acquire(id string) *soloChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[id]
	if c == nil {
		c = s.newChat(id)
		s.chats[id] = c
	}
	return c
}

func (s *SoloService) newChat(id string) *soloChat {
	c := &soloChat{svc: s, id: id, stream: chatsession.NewStream(soloStreamBuffer)}
	c.sess = chatsession.New(chatsession.Config[*soloTurn]{
		Capacity:  soloQueueCapacity,
		FullError: "Solo 会话排队已满，请稍候",
		View: func(q *soloTurn) chatsession.ItemView {
			return chatsession.ItemView{ID: q.ID, Text: q.Text, Images: q.Images, Annotations: q.Annotations, References: q.References}
		},
		Execute: c.execute,
		Publish: c.publish,
		Persist: chatsession.Persister(s.pending, soloPendingSurface, id, func(q *soloTurn) string { return q.ID }),
		TurnBeginExtra: func(q *soloTurn) map[string]any {
			return map[string]any{"userMsgId": q.ID}
		},
		BeforeTurn: func(q *soloTurn, _ <-chan struct{}) {
			c.mu.Lock()
			c.turnID = q.ID
			c.phase = SoloPhasePreparing
			c.reply = sandbox.Reply{}
			c.events = nil
			c.execution = nil
			c.mu.Unlock()
		},
		CancelTurn: func() {
			c.mu.Lock()
			sid := c.sandboxID
			c.mu.Unlock()
			if s.chat != nil && sid != 0 {
				s.chat.Cancel(sid)
			}
		},
		SteerTurn: func(ctx context.Context, _, q *soloTurn) chatsession.SteerResult {
			c.mu.Lock()
			sid, running := c.sandboxID, c.phase == SoloPhaseRunning
			c.mu.Unlock()
			// A turn still preparing has not reached the agent yet.
			if !running || !q.joinsRunningTurn() {
				return chatsession.SteerRejected
			}
			return steerSandboxTurn(ctx, s.chat, sid, q.Text, q.Images)
		},
	})
	return c
}

// SendWithReferences queues a user turn with explicit @ picks and returns the
// number of turns ahead of it. References only come from refs; the text is
// never parsed for @.
func (s *SoloService) SendWithReferences(id, user, text string, images []models.PromptImage, refs []ReferenceInput, picks ...models.ReactAnnotation) (int, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return 0, err
	}
	return s.sendSoloTurn(row, &soloTurn{Text: text, Images: images, Annotations: picks}, refs)
}

// SendPreview queues a share turn with references already validated without the owner's credentials.
func (s *SoloService) SendPreview(id, user, text string, images []models.PromptImage, refs []models.MessageReference, picks []models.ReactAnnotation) error {
	row, err := s.Require(id, user)
	if err != nil {
		return err
	}
	_, err = s.sendSoloTurn(row, &soloTurn{Text: text, Images: images, References: refs, Annotations: picks, PreviewShared: true}, nil)
	return err
}

func (s *SoloService) sendSoloTurn(row *models.SoloSession, turn *soloTurn, inputs []ReferenceInput) (int, error) {
	if s.sbx != nil && row.SandboxID != 0 {
		unlock, err := s.sbx.lockSoloRecovery(context.Background(), row.SandboxID)
		if err != nil {
			return 0, err
		}
		defer unlock()
		if err := s.db.First(row, "id = ?", row.ID).Error; err != nil {
			return 0, err
		}
	}
	if row.Status == models.SoloStatusClosed {
		return 0, soloErr(http.StatusConflict, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return 0, soloErr(http.StatusConflict, "session_error", "沙箱不可用，请重新启动会话")
	}
	if row.Status == models.SoloStatusStopped || row.Status == models.SoloStatusStopping {
		return 0, soloErr(http.StatusConflict, "session_stopped", "会话已停止，请先恢复")
	}
	turn.Text = strings.TrimSpace(turn.Text)
	turn.Annotations = models.NormalizePreviewPicks(turn.Annotations, 8)
	if turn.Text == "" && len(turn.Images) == 0 && len(turn.References) == 0 && len(turn.Annotations) == 0 && len(inputs) == 0 {
		return 0, soloErr(http.StatusBadRequest, "content_required", "消息不能为空")
	}
	if err := s.prepareSoloCommand(row.AgentName, turn, len(inputs) > 0); err != nil {
		return 0, err
	}
	if len(inputs) > 0 {
		references, err := s.prepareReferences(context.Background(), row, inputs)
		if err != nil {
			return 0, err
		}
		turn.References = references
	}
	return s.enqueueTurn(row.ID, turn)
}

// enqueueTurn queues a turn whose references are already validated.
func (s *SoloService) enqueueTurn(id string, turn *soloTurn) (int, error) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.deferredRestores[id] {
		return 0, soloErr(http.StatusServiceUnavailable, "session_recovering", "工作区和消息队列正在恢复，请稍后重试")
	}
	images, err := blob.IngestPromptImages(context.Background(), s.blobs, turn.Images)
	if err != nil {
		return 0, fmt.Errorf("ingest attachments: %w", err)
	}
	if s.chat == nil {
		return 0, errors.New("solo chat unavailable")
	}
	turn.ID = uuid.NewString()
	turn.Text = strings.TrimSpace(turn.Text)
	turn.Images = images
	return s.acquire(id).sess.Enqueue(turn, nil)
}

const (
	soloPendingSurface = "solo"
	soloRestartStopped = "服务重启，本轮回复已中断"
)

// RestoreQueuedTurns puts the turns that were still unfinished when the
// previous process exited back into their sessions, in order. Call it once at
// startup, after the sandbox hooks are wired.
func (s *SoloService) RestoreQueuedTurns() {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.chat == nil {
		return
	}
	sessions, keys, err := s.pending.Load(soloPendingSurface)
	if err != nil {
		log.Error().Err(err).Msg("solo: load queued turns failed")
		return
	}
	for _, id := range keys {
		if !s.deferredRestores[id] {
			continue // accepted after this process started; already in its live FIFO
		}
		s.restoreSessionLocked(id, sessions[id])
	}
	s.restoreEnabled = true
}

func (s *SoloService) restoreSessionLocked(id string, stored []chatsession.PendingTurn) {
	row, err := s.load(id)
	if err != nil || row.Status == models.SoloStatusClosed || row.Status == models.SoloStatusError {
		delete(s.deferredRestores, id)
		// Closing a session or losing its sandbox drops its queue as well.
		log.Info().Str("session", id).Int("turns", len(stored)).Msg("solo: queued turns dropped with an ended session")
		if err := s.pending.Clear(soloPendingSurface, id); err != nil {
			log.Warn().Err(err).Str("session", id).Msg("solo: clear queued turns failed")
		}
		return
	}
	if s.sbx != nil && row.SandboxID != 0 {
		if sb, getErr := s.sbx.Get(row.SandboxID); getErr == nil && sb.Status != "running" {
			if s.deferredRestores == nil {
				s.deferredRestores = map[string]bool{}
			}
			s.deferredRestores[id] = true
			return
		}
	}
	delete(s.deferredRestores, id)
	var turns []*soloTurn
	for _, t := range stored {
		var q soloTurn
		if err := json.Unmarshal(t.Payload, &q); err != nil || q.ID == "" {
			log.Warn().Err(err).Str("session", id).Str("turn", t.ID).Msg("solo: unreadable queued turn skipped")
			continue
		}
		if t.Started && s.settleInterruptedTurn(id, q.ID) {
			continue
		}
		turns = append(turns, &q)
	}
	if len(turns) == 0 {
		if err := s.pending.Clear(soloPendingSurface, id); err != nil {
			log.Warn().Err(err).Str("session", id).Msg("solo: clear queued turns failed")
		}
		return
	}
	s.acquire(id).sess.Restore(turns)
	log.Info().Str("session", id).Int("turns", len(turns)).Msg("solo: queued turns restored")
}

func (s *SoloService) retryDeferredRestores(sandboxID uint) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if !s.restoreEnabled || len(s.deferredRestores) == 0 {
		return
	}
	var sessions []models.SoloSession
	s.db.Where("sandbox_id = ?", sandboxID).Find(&sessions)
	for _, session := range sessions {
		if !s.deferredRestores[session.ID] {
			continue
		}
		stored, _, err := s.pending.Load(soloPendingSurface)
		if err == nil {
			s.restoreSessionLocked(session.ID, stored[session.ID])
		}
	}
}

func (s *SoloService) clearDeferredQueue(id string) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.deferredRestores[id] {
		delete(s.deferredRestores, id)
		if err := s.pending.Clear(soloPendingSurface, id); err != nil {
			log.Warn().Err(err).Str("session", id).Msg("solo: clear deferred queue failed")
		}
	}
}

// settleInterruptedTurn closes a turn the restart cut off. It reports false
// when the turn never reached the transcript, so it can simply run again.
func (s *SoloService) settleInterruptedTurn(sessionID, turnID string) bool {
	var user models.SoloMessage
	if err := s.db.First(&user, "id = ? AND session_id = ?", turnID, sessionID).Error; err != nil {
		return false
	}
	if user.Role == "system" {
		return true
	}
	var replies int64
	s.db.Model(&models.SoloMessage{}).
		Where("session_id = ? AND role = ? AND created_at >= ?", sessionID, "assistant", user.CreatedAt).Count(&replies)
	if replies > 0 || user.Status != "" {
		return true
	}
	reply := models.SoloMessage{ID: uuid.NewString(), SessionID: sessionID, TurnID: turnID, ContextID: user.ContextID, Role: "assistant",
		Status: "stopped", Error: soloRestartStopped}
	if err := s.db.Create(&reply).Error; err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("solo: record interrupted turn failed")
	}
	s.db.Model(&models.SoloMessage{}).Where("id = ?", turnID).Update("status", "stopped")
	return true
}

// Cancel stops the active turn and drops queued ones.
func (s *SoloService) Cancel(id, user string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	if c := s.lookup(id); c != nil {
		c.sess.Cancel(true)
	}
	s.clearDeferredQueue(id)
	return nil
}

// CancelPreviewTurn preserves Solo's clear-queue policy, but refuses stale
// public requests before either the queue or the runtime can be affected.
func (s *SoloService) CancelPreviewTurn(id, user, turnID string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	if c := s.lookup(id); c != nil && c.sess.CancelActive(turnID, true) {
		return nil
	}
	return soloErr(http.StatusConflict, "turn_changed", "当前回复已变化，请重试")
}

// SendAndSteer queues a user turn and handles it at once (SteerQueued).
func (s *SoloService) SendAndSteer(id, user, text string, images []models.PromptImage, refs []ReferenceInput, picks ...models.ReactAnnotation) (chatsession.SteerOutcome, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return "", err
	}
	turn := &soloTurn{Text: text, Images: images, Annotations: picks}
	if _, err := s.sendSoloTurn(row, turn, refs); err != nil {
		return "", err
	}
	return s.steer(id, turn.ID)
}

// SteerQueued handles a pending turn now: the running turn takes it when the
// agent supports that, otherwise the running turn is interrupted and this one
// runs next.
func (s *SoloService) SteerQueued(id, user, turnID string) (chatsession.SteerOutcome, error) {
	if _, err := s.Require(id, user); err != nil {
		return "", err
	}
	return s.steer(id, turnID)
}

func (s *SoloService) steer(id, turnID string) (chatsession.SteerOutcome, error) {
	c := s.lookup(id)
	if c == nil {
		return "", soloErr(http.StatusNotFound, "turn_not_found", "排队消息不存在")
	}
	out, err := c.sess.Steer(context.Background(), turnID)
	switch {
	case errors.Is(err, chatsession.ErrItemActive):
		// It started on its own meanwhile; that is what steering asked for.
		return chatsession.SteerOutcomeNext, nil
	case errors.Is(err, chatsession.ErrNotFound):
		return "", soloErr(http.StatusNotFound, "turn_not_found", "排队消息不存在")
	}
	return out, err
}

// RemoveQueued drops one pending turn.
func (s *SoloService) RemoveQueued(id, user, turnID string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	c := s.lookup(id)
	if c == nil {
		return soloErr(http.StatusNotFound, "turn_not_found", "排队消息不存在")
	}
	return c.sess.Remove(turnID)
}

// ReorderQueued reorders pending turns.
func (s *SoloService) ReorderQueued(id, user string, ids []string) error {
	if _, err := s.Require(id, user); err != nil {
		return err
	}
	c := s.lookup(id)
	if c == nil {
		return nil
	}
	return c.sess.Reorder(ids)
}

// Subscribe streams a session: queue_state snapshot, the active turn, then
// live frames. The returned func unsubscribes.
func (s *SoloService) Subscribe(id, user string, afterSeq int) (<-chan chatsession.Event, func(), error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, nil, err
	}
	c := s.acquire(id)
	// KeepNone status/model frames are not replayed. Capture the row at subscribe
	// time so the first queue_state is an authoritative session snapshot.
	row, rowErr := s.load(id)
	ch, unsub := c.stream.Subscribe(func() (map[string]any, bool) {
		sn := c.sess.Snapshot()
		qs := sn.QueueState()
		c.mu.Lock()
		if sn.Busy && c.phase != "" {
			qs["phase"] = c.phase
			qs["userMsgId"] = c.turnID
			if c.execution != nil {
				qs["execution"] = c.execution
			}
		}
		c.mu.Unlock()
		if rowErr == nil {
			applySoloResumeSnapshot(qs, row)
		}
		return qs, sn.Busy
	}, afterSeq)
	return ch, unsub, nil
}
