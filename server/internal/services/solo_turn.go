package services

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (c *soloChat) publish(event string, payload map[string]any) {
	keep := chatsession.KeepNone
	switch event {
	case chatsession.EventQueueState:
		c.mu.Lock()
		if busy, _ := payload["busy"].(bool); busy && c.phase != "" {
			payload["phase"] = c.phase
			payload["userMsgId"] = c.turnID
			if c.execution != nil {
				payload["execution"] = c.execution
			}
		}
		c.mu.Unlock()
	case chatsession.EventTurnBegin:
		keep = chatsession.KeepBegin
	case chatsession.EventTurnDone, chatsession.EventError:
		c.mu.Lock()
		payload["userMsgId"] = c.turnID
		if c.execution != nil {
			payload["execution"] = c.execution
		}
		c.phase = ""
		c.sandboxID = 0
		c.mu.Unlock()
		keep = chatsession.KeepEnd
	}
	c.stream.Session(event, payload, keep)
}

func (c *soloChat) setPhase(phase string) {
	c.mu.Lock()
	if phase == "" || c.phase == phase {
		c.mu.Unlock()
		return
	}
	c.phase = phase
	c.mu.Unlock()
	c.stream.Session(SoloEventPhase, map[string]any{"phase": phase}, chatsession.KeepAppend)
}

func (c *soloChat) onAcp(raw json.RawMessage) {
	delta := extractAgentText(raw)
	c.mu.Lock()
	c.text.WriteString(delta)
	if len(c.events) < soloMaxEvents {
		c.events = append(c.events, append(json.RawMessage(nil), raw...))
	}
	c.mu.Unlock()
	c.stream.Acp(raw)
}

// waitSandbox blocks until the session's sandbox is running. A failed wake is
// retried: the provider may finish resuming after the gateway call timed out.
func (c *soloChat) waitSandbox(ctx context.Context) (uint, *models.SoloSession, error) {
	s := c.svc
	wakeFailures := 0
	for {
		row, err := s.load(c.id)
		if err != nil {
			return 0, nil, err
		}
		switch row.Status {
		case models.SoloStatusClosed:
			return 0, nil, errors.New("会话已关闭")
		case models.SoloStatusStopped, models.SoloStatusStopping:
			return 0, nil, errors.New("会话已停止")
		case models.SoloStatusError:
			if row.Error != "" {
				return 0, nil, errors.New(row.Error)
			}
			return 0, nil, errors.New("沙箱启动失败")
		}
		if row.SandboxID != 0 {
			if row.Status == models.SoloStatusRunning && s.modelAlign == nil {
				if err := s.sbx.EnsureSoloRunning(ctx, row.SandboxID); err != nil {
					wakeFailures++
					if wakeFailures >= soloWakeAttempts || ctx.Err() != nil {
						return 0, nil, err
					}
					log.Warn().Err(err).Str("session", c.id).Int("attempt", wakeFailures).Msg("solo sandbox wake failed; retrying")
					select {
					case <-ctx.Done():
						return 0, nil, err
					case <-time.After(soloWakeRetryDelay):
					}
					continue
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
	c.svc.db.Model(&models.SoloMessage{}).Where("id = ?", turnID).
		Updates(map[string]any{"status": status, "error": errText})
	reply := models.SoloMessage{
		ID: "reply:" + turnID, TurnID: turnID, SessionID: c.id, Role: "assistant",
		Status: status, Error: errText, DurationMs: time.Since(started).Milliseconds(),
	}
	if perr := c.svc.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "error", "duration_ms", "turn_id"})}).Create(&reply).Error; perr != nil {
		log.Warn().Err(perr).Str("session", c.id).Msg("solo preparation failure persist failed")
	}
}

// execute runs one queued turn on the chatsession worker: save the user
// message, prepare the sandbox and model, build the prompt, call the Agent and
// save the result. The bool reports a turn stopped by its context.
func (c *soloChat) execute(ctx context.Context, q *soloTurn) (bool, error) {
	started := time.Now()
	userMsg, err := c.saveUserMessage(q, started)
	if err != nil {
		return false, err
	}
	prep, stopped, err := c.prepareTurn(ctx, q.ID, started)
	if err != nil {
		c.recordPreparationFailure(ctx, q.ID, started, err)
		return stopped, err
	}
	in := c.svc.buildTurnInput(c.id, q, userMsg.References, prep)
	out := c.runAgent(ctx, prep.sandboxID, in)
	c.saveTurnResult(q.ID, started, prep, in, out)
	if out.err != nil {
		return ctx.Err() != nil, out.err
	}
	return false, nil
}

// soloTurnPrep is what preparing a turn settled: the running sandbox, the
// session row read after any concurrent switch, and the applied model.
type soloTurnPrep struct {
	sandboxID uint
	row       *models.SoloSession
	// switched reports that alignment restarted the coding agent.
	switched bool
	// noticeBefore is the model notice an earlier turn left in place.
	noticeBefore string
	execution    *models.ChatExecution
}

// soloTurnInput is the prompt for the Agent. err is a context preamble
// failure; the Agent is then not called and the turn fails.
type soloTurnInput struct {
	prompt       string
	images       []models.PromptImage
	usedPreamble bool
	err          error
}

// soloTurnOutput is what the Agent call produced and how the turn ended.
type soloTurnOutput struct {
	text      string
	events    []json.RawMessage
	usage     *models.TokenUsage
	byModel   models.TokenUsageByModel
	questions []models.ReactQuestion
	forms     []models.ReactForm
	status    string
	ledger    string
	errText   string
	err       error
}

// saveUserMessage persists the user row, pinning its references first.
func (c *soloChat) saveUserMessage(q *soloTurn, started time.Time) (models.SoloMessage, error) {
	s := c.svc
	userMsg := models.SoloMessage{ID: q.ID, SessionID: c.id, Role: "user", Content: q.Text, Images: q.Images, Annotations: q.Annotations, PreviewShared: q.PreviewShared}
	if len(q.References) > 0 {
		if row, err := s.load(c.id); err == nil {
			userMsg.References = s.pinReferences(row, q.References)
		} else {
			userMsg.References = q.References
		}
	}
	if err := s.db.Create(&userMsg).Error; err != nil {
		return userMsg, err
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", &started)
	return userMsg, nil
}

// prepareTurn waits for the sandbox, applies the session's model and publishes
// the execution snapshot. stopped is meaningful only with an error.
func (c *soloChat) prepareTurn(ctx context.Context, turnID string, started time.Time) (prep soloTurnPrep, stopped bool, err error) {
	s := c.svc
	pctx, pcancel := context.WithTimeout(ctx, soloPrepareTimeout)
	sandboxID, _, err := c.waitSandbox(pctx)
	pcancel()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return prep, false, err
	}
	// Read again so a switch made while the sandbox was booting, or while an
	// earlier turn was still generating, is the one this turn applies.
	row, err := s.load(c.id)
	if err != nil {
		return prep, false, err
	}
	c.mu.Lock()
	c.sandboxID = sandboxID
	c.mu.Unlock()
	c.setPhase(SoloPhaseRunning)

	prep = soloTurnPrep{sandboxID: sandboxID, row: row, noticeBefore: strings.TrimSpace(row.ModelNotice)}
	if prep.switched, err = s.alignSessionModel(ctx, c, sandboxID, row); err != nil {
		return prep, ctx.Err() != nil, err
	}
	prep.execution = s.turnExecutionSnapshot(row, sandboxID, started)
	c.mu.Lock()
	c.execution = prep.execution
	c.mu.Unlock()
	if prep.execution != nil {
		c.stream.Session(SoloEventExecution, map[string]any{"userMsgId": turnID, "execution": prep.execution}, chatsession.KeepAppend)
	}
	return prep, false, nil
}

// buildTurnInput renders the prompt: annotations, pinned references and, when
// the coding agent lost its own history, a context preamble of the dialogue.
func (s *SoloService) buildTurnInput(sessionID string, q *soloTurn, refs []models.MessageReference, prep soloTurnPrep) soloTurnInput {
	row := prep.row
	prompt := q.Text
	if len(q.Annotations) > 0 {
		prompt = models.RenderAnnotatedPrompt(q.Text, q.Annotations)
	} else if prompt == "" && len(q.Images) > 0 {
		prompt = "（见附件）"
	} else if prompt == "" {
		prompt = "（见引用）"
	}
	if block := s.renderReferenceBlock(refs); block != "" {
		prompt = block + "\n\n" + prompt
	}
	in := soloTurnInput{images: q.Images}
	// A recreated sandbox still uses the restart summary. A model switch only
	// restarts the coding agent, so it must not claim the sandbox or the
	// repository was rebuilt.
	var pre string
	if row.NeedsPreamble || prep.switched || row.ModelContextPending || row.ResumeContextPending {
		header := soloModelPreambleHeader
		if row.NeedsPreamble {
			header = soloRestartPreambleHeader
		} else if row.ResumeContextPending {
			header = soloResumePreambleHeader
		} else if prep.switched && !row.ModelContextPending && row.CurrentReasoningEffort != row.AppliedReasoningEffort && chatModelID(row) == appliedChatModelID(row) && chatConfigurationID(row) == appliedChatConfigurationID(row) {
			header = soloReasoningPreambleHeader
		}
		var history []models.PromptImage
		pre, history, in.err = s.contextPreambleChecked(sessionID, q.ID, header)
		if len(history) > 0 {
			in.images = append(slices.Clone(q.Images), history...)
		}
		if in.err != nil {
			s.db.Model(&models.SoloMessage{}).Where("id = ?", q.ID).Updates(map[string]any{"status": "failed", "error": truncErr(in.err)})
		}
		in.usedPreamble = true
	}
	if pre != "" {
		prompt = pre + "\n\n用户本轮消息：\n" + prompt
	}
	in.prompt = prompt
	return in
}

// runAgent sends the prompt (unless building it failed), collects the asks and
// streamed reply, and classifies how the turn ended.
func (c *soloChat) runAgent(ctx context.Context, sandboxID uint, in soloTurnInput) soloTurnOutput {
	s := c.svc
	out := soloTurnOutput{err: in.err}
	askRunID := s.soloAskRunID(sandboxID)
	s.takeSoloAsks(askRunID)
	if out.err == nil {
		out.usage, out.byModel, out.err = s.chat.ChatWithTimeout(ctx, sandboxID, in.prompt, in.images, 0, c.onAcp)
	}
	out.questions, out.forms = s.takeSoloAsks(askRunID)

	c.mu.Lock()
	out.text = strings.TrimSpace(c.text.String())
	out.events = c.events
	c.events = nil
	c.mu.Unlock()

	out.status, out.ledger = "ok", models.TokenLedgerStatusOK
	switch {
	case out.err != nil && ctx.Err() != nil:
		out.status, out.ledger, out.errText = "stopped", models.TokenLedgerStatusCancelled, "已停止"
	case out.err != nil:
		out.status, out.ledger, out.errText = "failed", models.TokenLedgerStatusFailed, truncErr(out.err)
	}
	return out
}

// saveTurnResult persists the reply with its delivered files, settles the
// user row and session flags, and ledgers the usage.
func (c *soloChat) saveTurnResult(turnID string, started time.Time, prep soloTurnPrep, in soloTurnInput, out soloTurnOutput) {
	s := c.svc
	reply := models.SoloMessage{
		ID: "reply:" + turnID, TurnID: turnID, SessionID: c.id, Role: "assistant", Content: out.text, Events: out.events,
		Status: out.status, Error: out.errText, Usage: out.usage, UsageByModel: out.byModel,
		Questions: out.questions, Forms: out.forms,
		Execution:  prep.execution,
		DurationMs: time.Since(started).Milliseconds(),
	}
	var deliveries []models.ArtifactDelivery
	if perr := s.db.Where("session_id = ? AND turn_id = ?", c.id, turnID).Order("created_at,id").Find(&deliveries).Error; perr != nil {
		log.Warn().Err(perr).Msg("read delivered files")
	}
	for _, file := range deliveries {
		reply.Files = append(reply.Files, file.ArtifactAttachment)
	}
	if out.text != "" || len(out.events) > 0 || out.err != nil || len(reply.Files) > 0 || len(out.questions) > 0 || len(out.forms) > 0 {
		// Delivery owns the placeholder's files. A tool may finish between the
		// delivery query above and this upsert; never overwrite its attachments.
		if perr := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"content", "events", "questions", "forms", "status", "error", "usage", "usage_by_model", "execution", "duration_ms", "turn_id"})}).Create(&reply).Error; perr != nil {
			log.Warn().Err(perr).Str("session", c.id).Msg("solo reply persist failed")
		}
	}
	if out.status != "ok" {
		s.db.Model(&models.SoloMessage{}).Where("id = ?", turnID).Update("status", out.status)
	}
	if in.usedPreamble && out.status == "ok" {
		s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Updates(map[string]any{"needs_preamble": false, "model_context_pending": false, "resume_context_pending": false})
	}
	// A switch that failed on an earlier turn leaves model_notice set. Once a
	// later turn succeeds on the model that is already applied, the banner is
	// stale. A failure from this turn's own alignment never reaches here.
	if out.status == "ok" && prep.noticeBefore != "" {
		s.db.Model(&models.SoloSession{}).Where("id = ? AND model_notice = ?", c.id, prep.noticeBefore).Update("model_notice", "")
	}
	now := time.Now()
	s.db.Model(&models.SoloSession{}).Where("id = ?", c.id).Update("last_active_at", &now)
	recordSoloUsage(s.db, c.id, prep.sandboxID, out.usage, out.byModel, out.ledger)
}

// recordSoloUsage ledgers one Solo turn; failed and stopped turns still
// consumed tokens.
func recordSoloUsage(db *gorm.DB, sessionID string, sandboxID uint, usage *models.TokenUsage, byModel models.TokenUsageByModel, status string) {
	tokenledger.Record(db, tokenledger.Entry{
		Source: models.TokenLedgerSourceSolo, Phase: models.TokenLedgerPhaseChat, Status: status,
		ThreadID: sessionID, SandboxID: sandboxID, Usage: usage, ByModel: byModel,
	})
}

const (
	soloResumePreambleHeader  = "（系统提示）沙箱已从停止状态恢复，仓库、工作区和未提交的改动都已保留。请结合下面的对话记录继续：\n\n"
	soloRestartPreambleHeader = "（系统提示）此会话的沙箱已重新创建，之前的 CLI 会话与未提交的改动已丢失，仓库已重新克隆。以下是此前的对话记录，供你恢复上下文：\n\n"
	// A model switch restarts only the coding agent. This header must not say
	// the sandbox was recreated, the repository was cloned again, or uncommitted
	// work was lost.
	soloModelPreambleHeader     = "（系统提示）编码模型已更换。仓库和工作区没有重建，已有改动都还在。请接着下面的对话继续：\n\n"
	soloReasoningPreambleHeader = "（系统提示）推理强度已调整。仓库和工作区没有重建，已有改动都还在。请接着下面的对话继续：\n\n"
)
