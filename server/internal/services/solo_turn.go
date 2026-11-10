package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func (c *soloChat) publish(event string, payload map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.publishExecution(ctx, event, payload); err != nil {
		log.Warn().Err(err).Str("session", c.id).Msg("conversation notification deferred; accepted queue remains durable")
	}
}

func (c *soloChat) enrichFrame(event string, payload map[string]any) {
	switch event {
	case chatsession.EventQueueState:
		c.mu.Lock()
		active, _ := payload["activeItem"].(map[string]any)
		if busy, _ := payload["busy"].(bool); busy && c.phase != "" && active["id"] == c.turnID {
			payload["phase"] = c.phase
			payload["userMsgId"] = c.turnID
			if c.execution != nil {
				payload["execution"] = c.execution
			}
		}
		c.mu.Unlock()
	case chatsession.EventTurnDone, chatsession.EventError:
		c.mu.Lock()
		payload["userMsgId"] = c.turnID
		if c.execution != nil {
			payload["execution"] = c.execution
		}
		c.phase = ""
		c.sandboxID = 0
		c.mu.Unlock()
	}
}

func (c *soloChat) setPhase(ctx context.Context, phase string) error {
	c.mu.Lock()
	if phase == "" || c.phase == phase {
		c.mu.Unlock()
		return nil
	}
	c.phase = phase
	c.mu.Unlock()
	return c.emit(ctx, SoloEventPhase, map[string]any{"phase": phase})
}

// waitSandbox blocks until the session's sandbox is running.
func (c *soloChat) waitSandbox(ctx context.Context) (uint, *models.SoloSession, error) {
	s := c.svc
	for {
		row, err := s.load(c.id)
		if err != nil {
			return 0, nil, err
		}
		switch row.Status {
		case models.SoloStatusStopped:
			return 0, nil, soloErr(http.StatusConflict, "environment_stopped", "环境已停止，请先恢复环境")
		case models.SoloStatusClosed:
			return 0, nil, errors.New("会话已关闭")
		case models.SoloStatusError:
			if row.Error != "" {
				return 0, nil, errors.New(row.Error)
			}
			return 0, nil, errors.New("沙箱启动失败")
		}
		if row.SandboxID != 0 {
			if row.Status == models.SoloStatusRunning && s.modelAlign == nil {
				agent, err := s.sessionAgent(row)
				if err != nil {
					return 0, nil, err
				}
				if err := s.sbx.EnsureSoloTurnRunning(ctx, row, agent); err != nil {
					return 0, nil, err
				}
			}
			if err := s.sbx.WaitReady(ctx, row.SandboxID, nil); err != nil {
				return 0, nil, err
			}
			if row.Status == models.SoloStatusRunning {
				return row.SandboxID, row, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// recordPreparationFailure leaves a reply even when the CLI never starts. The
// transcript renders failures on assistant rows, including after a reload.
func (c *soloChat) recordPreparationFailure(ctx context.Context, turnID string, started time.Time, err error) {
	status := "failed"
	if ctx.Err() != nil {
		status = "stopped"
	}
	errText := truncErr(err)
	reply := models.SoloMessage{
		ID: "reply:" + turnID, TurnID: turnID, SessionID: c.id, Role: "assistant",
		Status: status, Error: errText, DurationMs: time.Since(started).Milliseconds(),
	}
	pctx, cancel := publicationContext(ctx, 10*time.Second)
	defer cancel()
	prepared, prepareErr := prepareSoloMessage(pctx, c.svc.db, reply)
	if prepareErr != nil {
		log.Warn().Err(prepareErr).Str("session", c.id).Msg("solo preparation failure payload persist failed")
		return
	}
	if perr := c.svc.executionTransaction(pctx, c.id, func(tx *gorm.DB) error {
		if err := chatsession.FenceExecution(pctx, tx); err != nil {
			return err
		}
		if err := tx.Model(&models.SoloMessage{}).Where("id = ?", turnID).Updates(map[string]any{"status": status, "error": errText}).Error; err != nil {
			return err
		}
		if _, err := insertPreparedSoloMessageTx(tx, prepared, "status", "error", "duration_ms", "turn_id"); err != nil {
			return err
		}
		state := chatsession.TurnFailed
		if status == "stopped" {
			state = chatsession.TurnCancelled
		}
		return chatsession.FinishExecution(pctx, tx, state, "preparation_failed", reply.ID)
	}); perr != nil {
		log.Warn().Err(perr).Str("session", c.id).Msg("solo preparation failure persist failed")
	}
}

func (c *soloChat) execute(ctx context.Context, q *soloTurn) (bool, error) {
	s := c.svc
	turnCtx := ctx
	started := time.Now()
	userMsg := models.SoloMessage{ID: q.ID, ClientMessageID: q.ClientMessageID, SessionID: c.id, Role: "user", Content: q.Text, Images: q.Images, Annotations: q.Annotations, PreviewShared: q.PreviewShared}
	if len(q.References) > 0 {
		if row, err := s.load(c.id); err == nil {
			userMsg.References = s.pinReferences(row, q.References)
		} else {
			userMsg.References = q.References
		}
	}
	userPrepareCtx, userPrepareCancel := publicationContext(ctx, 10*time.Second)
	preparedUser, err := prepareSoloMessage(userPrepareCtx, s.db, userMsg)
	if err != nil {
		userPrepareCancel()
		return false, err
	}
	userErr := s.executionTransaction(userPrepareCtx, c.id, func(tx *gorm.DB) error {
		if err := chatsession.FenceExecution(ctx, tx); err != nil {
			return err
		}
		_, err := insertPreparedSoloMessageTx(tx, preparedUser)
		return err
	})
	userPrepareCancel()
	if userErr != nil {
		return false, userErr
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", &started)

	pctx, pcancel := context.WithTimeout(ctx, soloPrepareTimeout)
	sandboxID, _, err := c.waitSandbox(pctx)
	pcancel()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		if _, durable := chatsession.CurrentExecution(ctx); durable && ctx.Err() == nil {
			return false, fmt.Errorf("%w: %v", chatsession.ErrPreparationDeferred, err)
		}
		c.recordPreparationFailure(ctx, q.ID, started, err)
		return false, err
	}
	if s.sbx != nil {
		operationCtx, release, leaseErr := s.sbx.acquireSandboxOperation(ctx, sandboxID, "turn")
		if leaseErr != nil {
			c.recordPreparationFailure(ctx, q.ID, started, leaseErr)
			return false, leaseErr
		}
		defer release()
		ctx = operationCtx
		environment, leaseErr := s.sbx.Get(sandboxID)
		if leaseErr == nil && environment.DesiredState == "stopped" {
			leaseErr = soloErr(409, "environment_stopped", "环境已手动停止，请先恢复环境")
		}
		if leaseErr != nil {
			c.recordPreparationFailure(ctx, q.ID, started, leaseErr)
			return false, leaseErr
		}
	}
	// Read again so a switch made while the sandbox was booting, or while an
	// earlier turn was still generating, is the one this turn applies.
	row, err := s.load(c.id)
	if err != nil {
		c.recordPreparationFailure(ctx, q.ID, started, err)
		return false, err
	}
	if row.SandboxID != sandboxID {
		err := soloErr(409, "environment_changed", "工作区已切换，请在当前环境重试本轮")
		c.recordPreparationFailure(ctx, q.ID, started, err)
		return false, err
	}
	c.mu.Lock()
	c.sandboxID = sandboxID
	c.mu.Unlock()
	if err := c.setPhase(ctx, SoloPhaseRunning); err != nil {
		return false, err
	}

	noticeBefore := strings.TrimSpace(row.ModelNotice)
	switched, alignErr := s.alignSessionModel(ctx, c, sandboxID, row)
	if alignErr != nil {
		c.recordPreparationFailure(ctx, q.ID, started, alignErr)
		return turnCtx.Err() != nil, alignErr
	}
	var conversation soloConversationIdentity
	contextChanged := false
	if preparer, ok := s.chat.(soloConversationPreparer); ok {
		conversation, err = preparer.prepareSoloConversation(ctx, sandboxID)
		if err != nil {
			c.recordPreparationFailure(ctx, q.ID, started, err)
			return turnCtx.Err() != nil, err
		}
		c.mu.Lock()
		contextChanged = c.conversation != conversation
		c.mu.Unlock()
	}
	execution := s.turnExecutionSnapshot(row, sandboxID, started)
	c.mu.Lock()
	c.execution = execution
	c.mu.Unlock()
	if execution != nil {
		if err := c.emit(ctx, SoloEventExecution, map[string]any{"userMsgId": q.ID, "execution": execution}); err != nil {
			return false, err
		}
	}

	prompt := q.Text
	if len(q.Annotations) > 0 {
		prompt = models.RenderAnnotatedPrompt(q.Text, q.Annotations)
	} else if prompt == "" && len(q.Images) > 0 {
		prompt = "（见附件）"
	} else if prompt == "" {
		prompt = "（见引用）"
	}
	if block := s.renderReferenceBlock(userMsg.References); block != "" {
		prompt = block + "\n\n" + prompt
	}
	usedPreamble := false
	// A recreated sandbox still uses the restart summary. A model switch only
	// restarts the coding agent, so it must not claim the sandbox or the
	// repository was rebuilt.
	var pre string
	if row.NeedsPreamble || switched || row.ModelContextPending || row.ConversationContextPending || row.ResumeContextPending || contextChanged {
		header := soloModelPreambleHeader
		if (row.ConversationContextPending || contextChanged) && !switched && !row.ModelContextPending {
			header = soloContextPreambleHeader
		}
		if row.NeedsPreamble {
			header = soloRestartPreambleHeader
		} else if row.ResumeContextPending {
			header = soloResumePreambleHeader
		} else if switched && !row.ModelContextPending && row.CurrentReasoningEffort != row.AppliedReasoningEffort && chatModelID(row) == appliedChatModelID(row) && chatConfigurationID(row) == appliedChatConfigurationID(row) {
			header = soloReasoningPreambleHeader
		}
		// A failure skips the model call; the fenced reply publication below
		// records the failed status on this turn.
		pre, err = s.contextPreambleWithContext(ctx, c.id, q.ID, header)
		usedPreamble = true
	}
	if pre != "" {
		prompt = pre + "\n\n用户本轮消息：\n" + prompt
	}

	nativeCtx, stopNative := context.WithCancelCause(ctx)
	defer stopNative(context.Canceled)
	writer := chatsession.NewJournalWriter(ctx, func(writeCtx context.Context, entries []chatsession.JournalEntry) (chatsession.JournalRange, error) {
		written, err := c.appendJournal(writeCtx, sandboxID, q.ID, entries, false)
		if err == nil {
			c.mu.Lock()
			if c.turnID == q.ID {
				for _, entry := range entries {
					appendSoloReplyText(&c.text, &c.replyTextState, entry.Data)
				}
			}
			c.mu.Unlock()
		}
		return written, err
	}, func(err error) { stopNative(err) })
	c.mu.Lock()
	c.writer = writer
	c.mu.Unlock()
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_ = writer.Close(flushCtx)
	}()
	var usage *models.TokenUsage
	var byModel models.TokenUsageByModel
	if err == nil && s.sbx != nil {
		// Preparation may change the model/runtime/root and stays exclusive.
		// Once stable, console admission can share this exact owner/generation
		// while the turn retains its mutation and final-publication fence.
		err = s.sbx.commitSandboxOperation(ctx, sandboxID, func(tx *gorm.DB) error {
			return s.sbx.updateSandboxOperation(ctx, tx, sandboxID, map[string]any{"operation_kind": sandboxOperationInference})
		})
	}
	if err == nil {
		err = chatsession.DispatchExecution(ctx)
	}
	if err == nil {
		usage, byModel, err = s.chat.ChatWithTimeout(nativeCtx, sandboxID, prompt, q.Images, 0, func(raw json.RawMessage) {
			c.onAcpForTurn(nativeCtx, q.ID, raw)
		})
	}

	flushCtx, flushCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	journalErr := writer.Close(flushCtx)
	flushCancel()
	written := writer.Written()
	var eventLog *models.ChatEventLog
	if written.Count > 0 {
		eventLog = &models.ChatEventLog{Version: chatsession.JournalVersion, From: int64(written.First), Through: int64(written.Last), Count: written.Count}
		if owner, ok := chatsession.CurrentExecution(ctx); ok {
			eventLog.Generation = owner.Lease.Generation
		}
	}
	c.mu.Lock()
	text := strings.TrimSpace(c.text.String())
	c.writer = nil
	c.mu.Unlock()

	status, ledger, errText := "ok", models.TokenLedgerStatusOK, ""
	switch {
	case journalErr != nil:
		status, ledger, errText = "unknown", models.TokenLedgerStatusFailed, "执行记录保存中断，请核对工作区和已执行操作后继续"
		err = journalErr
	case err != nil && errors.Is(context.Cause(ctx), errSandboxOperationLost):
		status, ledger, errText = "failed", models.TokenLedgerStatusFailed, errSandboxOperationLost.Error()
	case err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled)):
		status, ledger, errText = "stopped", models.TokenLedgerStatusCancelled, soloTurnStoppedText
	case err != nil:
		status, ledger, errText = "failed", models.TokenLedgerStatusFailed, truncErr(err)
	}
	reply := models.SoloMessage{
		ID: "reply:" + q.ID, TurnID: q.ID, SessionID: c.id, Role: "assistant", Content: text, EventLog: eventLog,
		Status: status, Error: errText, Usage: usage, UsageByModel: byModel,
		Execution:  execution,
		DurationMs: time.Since(started).Milliseconds(),
	}
	prepareCtx, prepareCancel := publicationContext(ctx, 10*time.Second)
	preparedReply, prepareErr := prepareSoloMessage(prepareCtx, s.db, reply)
	prepareCancel()
	if prepareErr != nil {
		return turnCtx.Err() != nil, fmt.Errorf("保存本轮回复正文失败：%w", prepareErr)
	}
	publish := func(tx *gorm.DB) error {
		if err := chatsession.FenceExecution(ctx, tx); err != nil {
			return err
		}
		cancelled, cancelErr := chatsession.ExecutionCancellation(ctx, tx)
		if cancelErr != nil {
			return cancelErr
		}
		if cancelled && status == "ok" {
			status, ledger, errText = "stopped", models.TokenLedgerStatusCancelled, soloTurnStoppedText
			reply.Status, reply.Error = status, errText
		}
		var deliveries []models.ArtifactDelivery
		if perr := tx.Where("session_id = ? AND turn_id = ?", c.id, q.ID).Order("created_at,id").Find(&deliveries).Error; perr != nil {
			return perr
		}
		for _, file := range deliveries {
			reply.Files = append(reply.Files, file.ArtifactAttachment)
		}
		preparedReply.record.Status, preparedReply.record.Error = reply.Status, reply.Error
		preparedReply.record.Files, cancelErr = messageJSON(reply.Files)
		if cancelErr != nil {
			return cancelErr
		}
		{
			// Delivery owns the placeholder's files. Never overwrite a file
			// attachment concurrently delivered by a tool.
			if _, perr := insertPreparedSoloMessageTx(tx, preparedReply, "content", "events", "event_log", "status", "error", "usage", "usage_by_model", "execution", "duration_ms", "turn_id"); perr != nil {
				return perr
			}
		}
		if status != "ok" {
			if perr := tx.Model(&models.SoloMessage{}).Where("id = ?", q.ID).Update("status", status).Error; perr != nil {
				return perr
			}
		}
		if usedPreamble && status == "ok" {
			if perr := soloLifecycleQuery(tx, row).Where("lifecycle_action = '' AND status <> ? AND workflow_context_version = ?", models.SoloStatusClosed, row.WorkflowContextVersion).Updates(map[string]any{"needs_preamble": false, "model_context_pending": false, "conversation_context_pending": false, "resume_context_pending": false}).Error; perr != nil {
				return perr
			}
		}
		if status == "ok" && alignErr == nil && noticeBefore != "" {
			if perr := tx.Model(&models.SoloSession{}).Where("id = ? AND model_notice = ?", c.id, noticeBefore).Update("model_notice", "").Error; perr != nil {
				return perr
			}
		}
		if err := tx.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", time.Now()).Error; err != nil {
			return err
		}
		if status == "unknown" {
			if err := tx.Model(&models.SoloSession{}).Where("id = ?", c.id).Updates(map[string]any{"conversation_context_pending": true, "workflow_context_version": gorm.Expr("workflow_context_version + 1")}).Error; err != nil {
				return err
			}
		}
		state := chatsession.TurnSucceeded
		if status == "unknown" {
			state = chatsession.TurnUnknown
		} else if status == "failed" {
			state = chatsession.TurnFailed
		} else if status == "stopped" {
			state = chatsession.TurnCancelled
		}
		return chatsession.FinishExecution(ctx, tx, state, "", reply.ID)
	}
	var publicationErr error
	if s.sbx != nil {
		publicationErr = s.sbx.commitSandboxOperationPrepared(ctx, sandboxID, func(tx *gorm.DB) error {
			return chatsession.FenceExecution(ctx, tx)
		}, publish)
	} else {
		publicationErr = s.executionTransaction(ctx, c.id, publish)
	}
	recordSoloUsage(s.db, c.id, sandboxID, usage, byModel, ledger)
	if publicationErr != nil {
		return turnCtx.Err() != nil, fmt.Errorf("保存本轮回复失败：%w", publicationErr)
	}
	if err != nil {
		return turnCtx.Err() != nil, err
	}
	if status == "ok" {
		c.mu.Lock()
		c.conversation = conversation
		c.mu.Unlock()
	}
	return false, nil
}

// recordSoloUsage ledgers one Solo turn; failed and stopped turns still
// consumed tokens.
func recordSoloUsage(db *gorm.DB, sessionID string, sandboxID uint, usage *models.TokenUsage, byModel models.TokenUsageByModel, status string) {
	tokenledger.Record(db, tokenledger.Entry{
		Source: models.TokenLedgerSourceSolo, Phase: models.TokenLedgerPhaseChat, Status: status,
		ThreadID: sessionID, SandboxID: sandboxID, Usage: usage, ByModel: byModel,
	})
}
