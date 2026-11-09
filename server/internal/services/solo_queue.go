package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func soloQueueScope(id string) chatsession.QueueScope {
	return chatsession.QueueScope{Surface: soloPendingSurface, Key: id}
}

func soloTurnView(turn *soloTurn) chatsession.ItemView {
	if turn == nil {
		return chatsession.ItemView{}
	}
	return chatsession.ItemView{ID: turn.ID, ClientMessageID: turn.ClientMessageID, Text: turn.Text,
		Images: turn.Images, Annotations: turn.Annotations, References: turn.References}
}

func decodeSoloTurn(row models.ChatQueueTurn) (*soloTurn, error) {
	var turn soloTurn
	if err := json.Unmarshal(row.Payload, &turn); err != nil {
		return nil, err
	}
	if turn.ID == "" || turn.ID != row.ID {
		return nil, errors.New("queued message identity disagrees with stored intent")
	}
	turn.ActorID, turn.ClientMessageID, turn.Fingerprint = row.ActorID, row.ClientMessageID, row.Fingerprint
	return &turn, nil
}

func (s *SoloService) durableSoloIntent(turn *soloTurn) (chatsession.QueueIntent, error) {
	if turn == nil {
		return chatsession.QueueIntent{}, errors.New("message intent required")
	}
	body, err := json.Marshal(turn)
	if err != nil {
		return chatsession.QueueIntent{}, err
	}
	return chatsession.QueueIntent{ID: turn.ID, ActorID: turn.ActorID, ClientMessageID: turn.ClientMessageID,
		Fingerprint: turn.Fingerprint, Payload: body}, nil
}

func soloIntentFingerprint(turn *soloTurn, refs []ReferenceInput) (string, error) {
	body, err := json.Marshal(struct {
		Text          string
		Images        []models.PromptImage
		Annotations   []models.ReactAnnotation
		References    []ReferenceInput
		Pinned        []models.MessageReference
		PreviewShared bool
	}{turn.Text, turn.Images, turn.Annotations, refs, turn.References, turn.PreviewShared})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func (s *SoloService) soloQueueAdmission(tx *gorm.DB, id string) error {
	var row models.SoloSession
	if err := tx.Select("id", "user_id", "status", "sandbox_id").First(&row, "id = ?", id).Error; err != nil {
		return err
	}
	if row.Status == models.SoloStatusClosed {
		return soloErr(409, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return soloErr(409, "session_error", "沙箱不可用，请重新启动会话")
	}
	if row.Status == models.SoloStatusStopped {
		return soloErr(409, "environment_stopped", "环境已停止，请先恢复环境")
	}
	if row.SandboxID != 0 && s.sbx != nil {
		var sb models.Sandbox
		if err := tx.Select("id", "desired_state").First(&sb, "id = ?", row.SandboxID).Error; err != nil {
			return err
		}
		if sb.DesiredState == "stopped" {
			return soloErr(409, "environment_stopped", "环境已请求停止，请先恢复环境")
		}
	}
	return nil
}

func (s *SoloService) soloQueueReady(id string) (bool, error) {
	if s.chat == nil {
		return false, nil
	}
	var notices []models.ChatQueueTurn
	if err := s.db.Where("queue_id = ? AND (state = ? OR (state = ? AND started_at IS NOT NULL)) AND result_id = ?", soloQueueScope(id).ID(), chatsession.TurnUnknown, chatsession.TurnCancelled, "").Find(&notices).Error; err != nil {
		return false, err
	}
	for _, notice := range notices {
		if err := s.recoverSoloReceipt(id, notice); err != nil {
			return false, err
		}
	}
	row, err := s.load(id)
	if err != nil {
		return false, err
	}
	if row.Status != models.SoloStatusRunning && row.Status != models.SoloStatusStarting {
		return false, nil
	}
	if row.SandboxID != 0 && s.sbx != nil {
		sb, err := s.sbx.Get(row.SandboxID)
		if err != nil {
			return false, err
		}
		creating := row.Status == models.SoloStatusStarting && (sb.Status == "creating" || sb.Status == "pulling")
		if sb.DesiredState == "stopped" || (sb.Status != "running" && !creating) {
			return false, nil
		}
	}
	return true, nil
}

// RestoreQueuedTurns upgrades the old pending set once, then schedules durable
// commands. It never copies a stale process's whole FIFO back over SQL.
func (s *SoloService) RestoreQueuedTurns() {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	s.restoreEnabled = true
	if err := s.importSoloQueuesLocked(); err != nil {
		log.Error().Err(err).Msg("Solo queue migration deferred; legacy requests retained")
	}
	if err := s.durable.ReconcileRetired(context.Background(), soloPendingSurface); err != nil {
		log.Warn().Err(err).Msg("closed Solo queue recovery deferred")
	}
	s.recoverSoloNoticesLocked()
	s.wakeSoloQueuesLocked()
}

// Notice publication can fail after takeover has durably classified a turn as
// unknown. Discover those receipts independently of the remaining FIFO, so even
// an otherwise empty conversation receives its interruption notice on retry.
func (s *SoloService) recoverSoloNoticesLocked() {
	var turns []models.ChatQueueTurn
	err := s.db.Joins("JOIN chat_queues q ON q.id = chat_queue_turns.queue_id").
		Where("q.surface = ? AND (chat_queue_turns.state = ? OR (chat_queue_turns.state = ? AND chat_queue_turns.started_at IS NOT NULL)) AND chat_queue_turns.result_id = ?", soloPendingSurface, chatsession.TurnUnknown, chatsession.TurnCancelled, "").
		Order("chat_queue_turns.created_at, chat_queue_turns.id").Limit(100).Find(&turns).Error
	if err != nil {
		log.Warn().Err(err).Msg("Solo interruption notices deferred")
		return
	}
	for _, turn := range turns {
		var queue models.ChatQueue
		if err := s.db.First(&queue, "id = ?", turn.QueueID).Error; err != nil {
			continue
		}
		if err := s.recoverSoloReceipt(queue.SessionKey, turn); err != nil {
			log.Warn().Err(err).Str("turn", turn.ID).Msg("Solo interruption notice remains pending")
		}
	}
}

func (s *SoloService) importSoloQueuesLocked() error {
	var keys []string
	if err := s.db.Model(&models.ChatPendingTurn{}).Where("surface = ?", soloPendingSurface).Distinct().Pluck("session_key", &keys).Error; err != nil {
		return err
	}
	var failures []error
	for _, id := range keys {
		var row models.SoloSession
		if err := s.db.Select("id", "user_id", "status").First(&row, "id = ?", id).Error; err != nil {
			// Preserve orphaned requests for repair instead of dropping them.
			failures = append(failures, err)
			continue
		}
		imported, err := s.durable.ImportLegacy(context.Background(), soloQueueScope(id), row.UserID, func(tx *gorm.DB, old models.ChatPendingTurn) (string, string, error) {
			return s.classifyLegacySoloTurn(tx, id, row.Status, old)
		})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		delete(s.deferredRestores, id)
		for _, turn := range imported {
			if turn.State == chatsession.TurnUnknown {
				if err := s.recoverSoloReceipt(id, turn); err != nil {
					failures = append(failures, err)
				}
			}
		}
	}
	return errors.Join(failures...)
}

func (s *SoloService) classifyLegacySoloTurn(tx *gorm.DB, id, status string, old models.ChatPendingTurn) (string, string, error) {
	if status == models.SoloStatusClosed {
		return chatsession.TurnCancelled, "session_closed", nil
	}
	var q soloTurn
	if err := json.Unmarshal(old.Payload, &q); err != nil || q.ID != old.ID {
		return chatsession.TurnUnknown, "legacy_payload_invalid", nil
	}
	if !old.Started {
		return chatsession.TurnQueued, "", nil
	}
	var human int64
	if err := tx.Model(&models.SoloMessage{}).Where("id = ? AND session_id = ? AND role = ?", old.ID, id, "user").Count(&human).Error; err != nil {
		return "", "", err
	}
	// The old Solo executor committed this exact human ID before any native
	// call. Its absence proves this accepted turn did not reach dispatch.
	if human == 0 {
		return chatsession.TurnQueued, "legacy_preparing", nil
	}
	var reply models.SoloMessage
	err := tx.Omit("content", "events", "images").Where("session_id = ? AND role = ? AND (id = ? OR turn_id = ?)", id, "assistant", "reply:"+old.ID, old.ID).First(&reply).Error
	if err == nil {
		state := chatsession.TurnSucceeded
		if reply.Status == "stopped" {
			state = chatsession.TurnCancelled
		} else if reply.Status == "failed" {
			state = chatsession.TurnFailed
		} else if reply.Status == "unknown" {
			state = chatsession.TurnUnknown
		}
		return state, "legacy_completed", nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", err
	}
	return chatsession.TurnUnknown, "legacy_interrupted", nil
}

func (s *SoloService) wakeSoloQueuesLocked() {
	scopes, err := s.durable.ReadyScopes(context.Background(), soloPendingSurface)
	if err != nil {
		log.Warn().Err(err).Msg("Solo durable queue discovery deferred")
		return
	}
	for _, scope := range scopes {
		ready, err := s.soloQueueReady(scope.Key)
		if err == nil && ready {
			s.acquire(scope.Key).sess.Wake()
		}
	}
}

// RunQueuedTurns discovers work accepted by another replica, including work
// whose accepting process exited before starting a worker. Reads never resume
// a manually stopped environment.
func (s *SoloService) RunQueuedTurns(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RestoreQueuedTurns()
		}
	}
}

func (s *SoloService) retryDeferredRestores(_ uint) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.restoreEnabled {
		if err := s.importSoloQueuesLocked(); err != nil {
			return
		}
		s.wakeSoloQueuesLocked()
	}
}

func (s *SoloService) recoverSoloReceipt(id string, turn models.ChatQueueTurn) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// The owning conversation's close/delete uses this same row lock.
		if err := tx.Model(&models.ChatQueue{}).Where("id = ?", soloQueueScope(id).ID()).UpdateColumn("revision", gorm.Expr("revision + 1")).Error; err != nil {
			return err
		}
		var current models.ChatQueueTurn
		if err := tx.Omit("payload").First(&current, "id = ? AND queue_id = ? AND state IN ?", turn.ID, soloQueueScope(id).ID(), []string{chatsession.TurnUnknown, chatsession.TurnCancelled}).Error; err != nil {
			return err
		}
		if current.ResultID != "" {
			return nil
		}
		var session models.SoloSession
		if err := tx.Select("id").First(&session, "id = ?", id).Error; err != nil {
			return err
		}
		var replyCount int64
		if err := tx.Model(&models.SoloMessage{}).Where("session_id = ? AND (id = ? OR turn_id = ?)", id, "reply:"+turn.ID, turn.ID).Where("role = ?", "assistant").Count(&replyCount).Error; err != nil {
			return err
		}
		if replyCount > 0 {
			var reply models.SoloMessage
			if err := tx.Select("id").Where("session_id = ? AND (id = ? OR turn_id = ?) AND role = ?", id, "reply:"+turn.ID, turn.ID, "assistant").First(&reply).Error; err != nil {
				return err
			}
			return tx.Model(&current).Update("result_id", reply.ID).Error
		}
		if q, err := decodeSoloTurn(turn); err == nil {
			message := models.SoloMessage{ID: q.ID, ClientMessageID: turn.ClientMessageID, SessionID: id, Role: "user", Content: q.Text,
				Images: q.Images, Annotations: q.Annotations, References: q.References, PreviewShared: q.PreviewShared}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&message).Error; err != nil {
				return err
			}
		}
		notice := models.SoloMessage{ID: "reply:" + turn.ID, TurnID: turn.ID, SessionID: id, Role: "assistant",
			Status: "unknown", Error: "执行结果待核对：" + soloRestartStopped + "，请核对工作区和已执行操作后继续"}
		if current.State == chatsession.TurnCancelled {
			notice.Status = "stopped"
			notice.Error = "已取消本轮，未派发到 Agent"
		}
		if err := tx.Model(&models.SoloMessage{}).Where("id = ? AND session_id = ?", turn.ID, id).Update("status", "stopped").Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&notice).Error; err != nil {
			return err
		}
		return tx.Model(&current).Update("result_id", notice.ID).Error
	})
}

func (s *SoloService) settleSoloReceipt(ctx context.Context, id string, row models.ChatQueueTurn, turn *soloTurn, state string, failure error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := chatsession.FenceExecution(ctx, tx); err != nil {
			return err
		}
		if turn != nil {
			user := models.SoloMessage{ID: row.ID, ClientMessageID: row.ClientMessageID, SessionID: id, Role: "user", Content: turn.Text,
				Images: turn.Images, Annotations: turn.Annotations, References: turn.References, PreviewShared: turn.PreviewShared}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&user).Error; err != nil {
				return err
			}
		}
		reason := "execution_failed"
		if state == chatsession.TurnUnknown {
			reason = "delivery_uncertain"
		}
		message := "本轮执行未完成，请核对工作区后继续"
		if failure != nil && state != chatsession.TurnUnknown {
			message = truncErr(failure)
		}
		reply := models.SoloMessage{ID: "reply:" + row.ID, TurnID: row.ID, SessionID: id, Role: "assistant", Status: "failed", Error: message}
		if state == chatsession.TurnUnknown {
			reply.Status = "unknown"
			reply.Error = "执行结果待核对，请核对工作区和已执行操作后继续"
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&reply).Error; err != nil {
			return err
		}
		return chatsession.FinishExecution(ctx, tx, state, reason, reply.ID)
	})
}

func (s *SoloService) queueFingerprint(turn *soloTurn, refs []ReferenceInput) error {
	if turn.ClientMessageID == "" {
		turn.ClientMessageID = uuid.NewString()
	}
	if len(turn.ClientMessageID) > 128 || strings.ContainsAny(turn.ClientMessageID, "\x00\r\n") {
		return soloErr(400, "invalid_client_identity", "消息身份无效")
	}
	if turn.Fingerprint != "" {
		return nil
	}
	fingerprint, err := soloIntentFingerprint(turn, refs)
	if err != nil {
		return fmt.Errorf("encode message intent: %w", err)
	}
	turn.Fingerprint = fingerprint
	return nil
}

type SoloTurnReceipt struct {
	Waiting         int    `json:"waiting"`
	TurnID          string `json:"turnId"`
	ClientMessageID string `json:"clientMessageId"`
	State           string `json:"state"`
}

func (s *SoloService) SendWithClientMessageID(id, user, clientID, text string, images []models.PromptImage, refs []ReferenceInput, picks ...models.ReactAnnotation) (SoloTurnReceipt, error) {
	var result SoloTurnReceipt
	row, err := s.Require(id, user)
	if err != nil {
		return result, err
	}
	turn := &soloTurn{ClientMessageID: clientID, Text: text, Images: images, Annotations: picks}
	waiting, err := s.sendSoloTurn(row, turn, refs)
	if err != nil {
		if errors.Is(err, chatsession.ErrClientConflict) {
			return result, soloErr(409, "client_identity_conflict", "同一消息身份不能用于不同内容")
		}
		if errors.Is(err, chatsession.ErrQueueFull) {
			return result, soloErr(429, "queue_full", "消息队列已满，请稍候")
		}
		return result, err
	}
	receipt, err := s.durable.Receipt(context.Background(), soloQueueScope(id), turn.ID)
	if err != nil {
		return result, err
	}
	return SoloTurnReceipt{Waiting: waiting, TurnID: receipt.ID, ClientMessageID: receipt.ClientMessageID, State: receipt.State}, nil
}

func (c *soloChat) onAcpForTurn(ctx context.Context, turnID string, raw json.RawMessage) {
	c.mu.Lock()
	current := c.turnID == turnID
	c.mu.Unlock()
	if !current {
		return
	}
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := chatsession.CheckExecution(check); err != nil {
		return
	}
	c.onAcp(raw)
}
