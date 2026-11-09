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

func decodeSoloMetadata(row models.ChatQueueTurn) (*soloTurn, error) {
	var item chatsession.ItemView
	if err := json.Unmarshal([]byte(row.Display), &item); err != nil {
		return nil, err
	}
	if item.ID != row.ID || item.ClientMessageID != row.ClientMessageID {
		return nil, errors.New("queued message preview identity disagrees with accepted intent")
	}
	return &soloTurn{ID: row.ID, ActorID: row.ActorID, ClientMessageID: row.ClientMessageID, Fingerprint: row.Fingerprint,
		Text: item.Text, Images: item.Images, Annotations: item.Annotations, References: item.References}, nil
}

func soloQueueDisplay(turn *soloTurn) ([]byte, error) {
	view := soloTurnView(turn)
	view.Text = truncRunes(view.Text, 1024)
	view.Images = append([]models.PromptImage(nil), view.Images...)
	if len(view.Images) > 16 {
		view.Images = view.Images[:16]
	}
	for i := range view.Images {
		view.Images[i].Data = ""
		view.Images[i].Name = truncRunes(view.Images[i].Name, 128)
		view.Images[i].MimeType = truncRunes(view.Images[i].MimeType, 128)
		if len(view.Images[i].Ref) > 256 {
			view.Images[i].Ref = "" // The full accepted attachment remains in Payload.
		}
	}
	view.Annotations = append([]models.ReactAnnotation(nil), view.Annotations...)
	if len(view.Annotations) > 16 {
		view.Annotations = view.Annotations[:16]
	}
	for i := range view.Annotations {
		pick := view.Annotations[i]
		view.Annotations[i] = models.ReactAnnotation{Selector: truncRunes(pick.Selector, 128), URL: truncRunes(pick.URL, 256), Label: truncRunes(pick.Label, 120), Note: truncRunes(pick.Note, 120)}
	}
	view.References = append([]models.MessageReference(nil), view.References...)
	if len(view.References) > 16 {
		view.References = view.References[:16]
	}
	for i := range view.References {
		ref := view.References[i]
		view.References[i] = models.MessageReference{ID: ref.ID, Kind: ref.Kind, Target: truncRunes(ref.Target, 256), Title: truncRunes(ref.Title, 120), URL: truncRunes(ref.URL, 256), State: ref.State}
	}
	for {
		body, err := json.Marshal(view)
		if err != nil || len(body) <= 65535 {
			return body, err
		}
		// Escaped Unicode/HTML and legacy metadata can exceed the byte budget
		// even with per-field rune limits. This affects previews only.
		switch {
		case len(view.References) > 0:
			view.References = view.References[:len(view.References)-1]
		case len(view.Annotations) > 0:
			view.Annotations = view.Annotations[:len(view.Annotations)-1]
		case len(view.Images) > 0:
			view.Images = view.Images[:len(view.Images)-1]
		default:
			return nil, errors.New("queued message preview identity is too large")
		}
	}
}

func (s *SoloService) durableSoloIntent(turn *soloTurn) (chatsession.QueueIntent, error) {
	if turn == nil {
		return chatsession.QueueIntent{}, errors.New("message intent required")
	}
	body, err := json.Marshal(turn)
	if err != nil {
		return chatsession.QueueIntent{}, err
	}
	display, err := soloQueueDisplay(turn)
	if err != nil {
		return chatsession.QueueIntent{}, err
	}
	return chatsession.QueueIntent{ID: turn.ID, ActorID: turn.ActorID, ClientMessageID: turn.ClientMessageID,
		Fingerprint: turn.Fingerprint, Payload: body, Display: display}, nil
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
	if s.sbx != nil && row.SandboxID == 0 {
		// First-message admission commits before provisioning. A different
		// replica may observe that intent while no environment is bound yet.
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
	// An explicit startup/recovery request also retries environments whose
	// passive discovery was backed off. It never resumes stopped compute.
	s.queueProbes = nil
	s.maintainSoloQueuesLocked()
	s.wakeSoloQueuesLocked()
}

func (s *SoloService) maintainSoloQueuesLocked() {
	s.restoreUnboundSoloCreations()
	if err := s.importSoloQueuesLocked(); err != nil {
		log.Error().Err(err).Msg("Solo queue migration deferred; legacy requests retained")
	}
	if err := s.durable.ReconcileRetired(context.Background(), soloPendingSurface); err != nil {
		log.Warn().Err(err).Msg("closed Solo queue recovery deferred")
	}
	s.recoverSoloNoticesLocked()
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
	// Legacy started flags lack a durable dispatch fence. An absent transcript
	// row is not permission to repeat an external effect after a migration.
	if human == 0 {
		return chatsession.TurnUnknown, "legacy_interrupted", nil
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
	s.wakeSoloQueuesAtLocked(time.Now())
}

type soloQueueProbe struct {
	Next  time.Time
	Delay time.Duration
}

const (
	soloQueueDiscoveryInterval   = time.Second
	soloQueueMaintenanceInterval = 30 * time.Second
	soloQueueMaxProbeDelay       = 30 * time.Second
)

func (s *SoloService) wakeSoloQueuesAtLocked(now time.Time) {
	scopes, err := s.durable.ReadyScopes(context.Background(), soloPendingSurface)
	if err != nil {
		log.Warn().Err(err).Msg("Solo durable queue discovery deferred")
		return
	}
	if s.queueProbes == nil {
		s.queueProbes = map[string]soloQueueProbe{}
	}
	candidates := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		candidates[scope.Key] = true
		probe := s.queueProbes[scope.Key]
		if probe.Next.After(now) {
			continue
		}
		ready, err := s.soloQueueReady(scope.Key)
		if err == nil && ready {
			delete(s.queueProbes, scope.Key)
			s.acquire(scope.Key).sess.Wake()
			continue
		}
		probe.Delay *= 2
		if probe.Delay < 2*soloQueueDiscoveryInterval {
			probe.Delay = 2 * soloQueueDiscoveryInterval
		}
		if probe.Delay > soloQueueMaxProbeDelay {
			probe.Delay = soloQueueMaxProbeDelay
		}
		probe.Next = now.Add(probe.Delay)
		s.queueProbes[scope.Key] = probe
	}
	for id := range s.queueProbes {
		if !candidates[id] {
			delete(s.queueProbes, id)
		}
	}
}

// RunQueuedTurns discovers work accepted by another replica, including work
// whose accepting process exited before starting a worker. Reads never resume
// a manually stopped environment.
func (s *SoloService) RunQueuedTurns(ctx context.Context) {
	discovery := time.NewTicker(soloQueueDiscoveryInterval)
	maintenance := time.NewTicker(soloQueueMaintenanceInterval)
	defer discovery.Stop()
	defer maintenance.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-discovery.C:
			s.pollSoloQueues(now, false)
		case now := <-maintenance.C:
			s.pollSoloQueues(now, true)
		}
	}
}

func (s *SoloService) pollSoloQueues(now time.Time, maintenance bool) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if maintenance || !s.restoreEnabled {
		s.restoreEnabled = true
		s.maintainSoloQueuesLocked()
	}
	s.wakeSoloQueuesAtLocked(now)
}

func (s *SoloService) retryDeferredRestores(sandboxID uint) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.restoreEnabled {
		if len(s.deferredRestores) > 0 {
			if err := s.importSoloQueuesLocked(); err != nil {
				return
			}
		}
		var ids []string
		if err := s.db.Model(&models.SoloSession{}).Where("sandbox_id = ?", sandboxID).Pluck("id", &ids).Error; err != nil {
			return
		}
		for _, id := range ids {
			delete(s.queueProbes, id)
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
		if q, err := decodeSoloTurn(turn); err == nil {
			message := models.SoloMessage{ID: q.ID, ClientMessageID: turn.ClientMessageID, SessionID: id, Role: "user", Content: q.Text,
				Images: q.Images, Annotations: q.Annotations, References: q.References, PreviewShared: q.PreviewShared}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&message).Error; err != nil {
				return err
			}
		}
		notice := models.SoloMessage{ID: "reply:" + turn.ID, TurnID: turn.ID, SessionID: id, Role: "assistant",
			Status: "unknown", Error: "执行结果待核对：" + soloRestartStopped + "，请核对工作区和已执行操作后继续"}
		if replyCount > 0 {
			// Artifact delivery may have created a placeholder before the
			// executor disappeared. Preserve its body/files and mark its result
			// honestly instead of treating that placeholder as completion.
			var existing models.SoloMessage
			if err := tx.Select("id").Where("session_id = ? AND (id = ? OR turn_id = ?) AND role = ?", id, "reply:"+turn.ID, turn.ID, "assistant").First(&existing).Error; err != nil {
				return err
			}
			notice.ID = existing.ID
		}
		var err error
		notice.EventLog, err = s.journalPointer(tx, id, turn.ID, current.ExecutionEpoch)
		if err != nil {
			return err
		}
		if current.State == chatsession.TurnCancelled {
			notice.Status = "stopped"
			notice.Error = "已取消本轮，未派发到 Agent"
		}
		if err := tx.Model(&models.SoloMessage{}).Where("id = ? AND session_id = ?", turn.ID, id).Update("status", "stopped").Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "error", "event_log", "turn_id"})}).Create(&notice).Error; err != nil {
			return err
		}
		if current.State == chatsession.TurnUnknown {
			if err := tx.Model(&models.SoloSession{}).Where("id = ?", id).Updates(map[string]any{"conversation_context_pending": true, "workflow_context_version": gorm.Expr("workflow_context_version + 1")}).Error; err != nil {
				return err
			}
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
		var err error
		reply.EventLog, err = s.journalPointer(tx, id, row.ID, row.ExecutionEpoch)
		if err != nil {
			return err
		}
		if state == chatsession.TurnUnknown {
			reply.Status = "unknown"
			reply.Error = "执行结果待核对，请核对工作区和已执行操作后继续"
			if err := tx.Model(&models.SoloSession{}).Where("id = ?", id).Updates(map[string]any{"conversation_context_pending": true, "workflow_context_version": gorm.Expr("workflow_context_version + 1")}).Error; err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "error", "event_log", "turn_id"})}).Create(&reply).Error; err != nil {
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
	writer, current := c.writer, c.turnID == turnID
	c.mu.Unlock()
	if !current || writer == nil {
		return
	}
	_ = writer.Add(ctx, chatsession.JournalEntry{Type: chatsession.FrameAcp, Data: raw})
}
