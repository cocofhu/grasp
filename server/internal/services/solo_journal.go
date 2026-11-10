package services

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// appendJournal publishes nothing until both the immutable body and its SQL
// index commit. Native output also holds the sandbox operation fence.
func (c *soloChat) appendJournal(ctx context.Context, sandboxID uint, turnID string, entries []chatsession.JournalEntry, settled bool) (chatsession.JournalRange, error) {
	execution, owned := chatsession.CurrentExecution(ctx)
	var owner *chatsession.QueueLease
	if owned && !settled {
		owner = &execution.Lease
	}
	validate := func(tx *gorm.DB) error {
		var parent models.SoloSession
		if err := tx.Select("id", "sandbox_id").First(&parent, "id = ?", c.id).Error; err != nil {
			return err
		}
		if sandboxID != 0 && parent.SandboxID != sandboxID {
			return errSandboxOperationLost
		}
		if settled && owned {
			var receipt models.ChatQueueTurn
			if err := tx.Select("state", "execution_epoch").First(&receipt, "id = ? AND queue_id = ?", turnID, execution.Lease.Scope.ID()).Error; err != nil {
				return err
			}
			var queue models.ChatQueue
			if err := tx.Select("generation", "owner", "owner_expires_at").First(&queue, "id = ?", execution.Lease.Scope.ID()).Error; err != nil {
				return err
			}
			if receipt.ExecutionEpoch != execution.Lease.Generation || receipt.State == chatsession.TurnExecuting || receipt.State == chatsession.TurnQueued || queue.Generation != execution.Lease.Generation || queue.Owner != execution.Lease.Owner || queue.OwnerExpiresAt == nil || !queue.OwnerExpiresAt.After(time.Now()) {
				return chatsession.ErrQueueOwnerLost
			}
		}
		return nil
	}
	persistence, cancel := publicationContext(ctx, 10*time.Second)
	defer cancel()
	return c.svc.durable.AppendJournalWith(persistence, soloQueueScope(c.id), owner, turnID, entries,
		func(attempt context.Context, prepared chatsession.PreparedJournal) (chatsession.JournalRange, error) {
			var written chatsession.JournalRange
			fence := func(tx *gorm.DB) error {
				if err := attempt.Err(); err != nil {
					return err
				}
				return c.svc.durable.FenceJournalTx(tx.Statement.Context, tx, soloQueueScope(c.id), owner, turnID, validate)
			}
			persist := func(tx *gorm.DB) error {
				var err error
				written, err = c.svc.durable.AppendPreparedJournalTx(tx.Statement.Context, tx, prepared, validate)
				return err
			}
			var err error
			if sandboxID != 0 && c.svc.sbx != nil {
				err = c.svc.sbx.commitSandboxOperationPrepared(attempt, sandboxID, fence, persist)
			} else {
				err = c.svc.executionTransaction(attempt, c.id, persist)
			}
			// The entire Queue -> Sandbox transaction has finished here. Only
			// AppendJournalWith may prepare again after a head CAS conflict.
			return written, err
		})
}

func (c *soloChat) emit(ctx context.Context, event string, payload map[string]any) error {
	turnID, _ := payload["userMsgId"].(string)
	if execution, ok := chatsession.CurrentExecution(ctx); ok {
		turnID = execution.TurnID
	}
	var sandboxID uint
	if c.svc.sbx != nil {
		if lease, ok := c.svc.sbx.operations().Current(ctx); ok {
			sandboxID = lease.ID
		}
	}
	_, err := c.appendJournal(ctx, sandboxID, turnID, []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: event, Payload: payload}}, event == chatsession.EventTurnDone || event == chatsession.EventError)
	return err
}

func (c *soloChat) publishExecution(ctx context.Context, event string, payload map[string]any) error {
	c.enrichFrame(event, payload)
	return c.emit(ctx, event, payload)
}

func (s *SoloService) journalTurn(ctx context.Context, turnID, sessionID string, through int64) ([]json.RawMessage, *models.ChatExecution, error) {
	var first models.ChatEventChunk
	err := s.db.WithContext(ctx).Select("first_sequence").Where("queue_id = ? AND turn_id = ?", soloQueueScope(sessionID).ID(), turnID).Order("first_sequence").First(&first).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	cursor := int64(first.FirstSequence) - 1
	events := []json.RawMessage{}
	var execution *models.ChatExecution
	for cursor < through {
		page, err := s.durable.JournalPage(ctx, soloQueueScope(sessionID), chatsession.JournalQuery{After: cursor, Through: &through})
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range page.Items {
			if entry.TurnID != turnID {
				continue
			}
			if entry.Type == chatsession.FrameAcp {
				events = append(events, entry.Data)
			} else if entry.Event == SoloEventExecution {
				data, err := json.Marshal(entry.Payload["execution"])
				if err != nil {
					return nil, nil, err
				}
				if err := json.Unmarshal(data, &execution); err != nil {
					return nil, nil, err
				}
			}
		}
		if page.Next <= cursor {
			return nil, nil, chatsession.ErrJournalGap
		}
		cursor = page.Next
	}
	return events, execution, nil
}

type soloJournalView struct {
	state           map[string]any
	head            int64
	first           int64
	revision        uint64
	generation      uint64
	activeEpoch     uint64
	controlsThrough int64
	updated         time.Time
}

func (s *SoloService) journalView(ctx context.Context, id, user string, previous ...soloJournalView) (soloJournalView, error) {
	// Legacy previews are backfilled without holding a queue lock. A bounded
	// retry captures one fresh, authoritative SQL snapshot after that backfill.
	for range 3 {
		result, legacy, chunks, err := s.journalViewMetadata(ctx, id, user, previous...)
		if err != nil {
			return soloJournalView{}, err
		}
		if len(legacy) > 0 {
			for _, row := range legacy {
				query := s.db.WithContext(ctx).Table("chat_queue_turns").Where("id = ? AND queue_id = ? AND fingerprint = ?", row.ID, row.QueueID, row.Fingerprint)
				row.Payload, err = s.readJournalViewBody(ctx, query)
				if err != nil {
					return soloJournalView{}, err
				}
				turn, err := decodeSoloTurn(row)
				if err != nil {
					return soloJournalView{}, err
				}
				display, err := soloQueueDisplay(turn)
				if err != nil {
					return soloJournalView{}, err
				}
				if err := s.durable.BackfillDisplay(ctx, soloQueueScope(id), row.ID, row.Fingerprint, display); err != nil {
					return soloJournalView{}, err
				}
			}
			continue
		}
		for _, chunk := range chunks {
			if chunk.Count != 1 || chunk.AcpCount != 0 || chunk.Bytes < 1 || chunk.Bytes > chatsession.JournalChunkBytes || chunk.FirstSequence != chunk.LastSequence {
				return soloJournalView{}, chatsession.ErrJournalGap
			}
			// The exact immutable reference was captured with this header. A
			// new owner or later body edit cannot change the captured view.
			entries, err := s.durable.LoadJournalChunk(ctx, chunk)
			if err != nil {
				return soloJournalView{}, err
			}
			if len(entries) != 1 || entries[0].Type != chatsession.FrameSession || entries[0].Event != chunk.ControlEvent {
				return soloJournalView{}, chatsession.ErrJournalGap
			}
			entry := entries[0]
			if entry.Event == SoloEventPhase {
				result.state["phase"] = entry.Payload["phase"]
			}
			if entry.Event == SoloEventExecution {
				result.state["execution"] = entry.Payload["execution"]
			}
			result.controlsThrough = int64(chunk.LastSequence)
		}
		return result, nil
	}
	return soloJournalView{}, chatsession.ErrQueueChanged
}

// readJournalViewBody loads the raw SQL cell into a type with no serializer.
// The cursor and connection are released before any object GET; a model query
// would otherwise perform that GET from inside its GORM Scan callback.
func (s *SoloService) readJournalViewBody(ctx context.Context, query *gorm.DB) ([]byte, error) {
	if _, inside := query.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, chatsession.ErrPayloadInTransaction
	}
	var cell struct{ Payload string }
	if err := query.Select("payload").Take(&cell).Error; err != nil {
		return nil, err
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	return files.Load(ctx, cell.Payload, filestore.Bytes)
}

func (s *SoloService) journalViewMetadata(ctx context.Context, id, user string, previous ...soloJournalView) (soloJournalView, []models.ChatQueueTurn, []chatsession.JournalChunkRecord, error) {
	result := soloJournalView{head: -1, first: -1, controlsThrough: -1}
	var legacy []models.ChatQueueTurn
	var chunks []chatsession.JournalChunkRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.SoloSession
		if err := tx.First(&row, "id = ? AND user_id = ?", id, user).Error; err != nil {
			return err
		}
		snapshot, err := chatsession.NewDurableStore(tx).SnapshotMetadata(ctx, soloQueueScope(id))
		if err != nil {
			return err
		}
		for _, pending := range snapshot.Pending {
			if pending.Display == "" {
				legacy = append(legacy, pending)
			}
		}
		if snapshot.Active != nil && snapshot.Active.Display == "" {
			legacy = append(legacy, *snapshot.Active)
		}
		if len(legacy) > 0 {
			return nil
		}
		sn := chatsession.Snapshot{SessionGeneration: snapshot.Queue.Generation, Revision: snapshot.Queue.Revision, Busy: snapshot.Active != nil, Waiting: len(snapshot.Pending), Items: []map[string]any{}}
		for _, pending := range snapshot.Pending {
			turn, err := decodeSoloMetadata(pending)
			if err != nil {
				return err
			}
			sn.Items = append(sn.Items, soloTurnView(turn).Frame())
		}
		if snapshot.Queue.NextEventSequence > 1<<53-1 {
			return chatsession.ErrJournalGap
		}
		result.head = int64(snapshot.Queue.NextEventSequence) - 1
		result.first = result.head
		if snapshot.Active != nil {
			if snapshot.Active.ExecutionEpoch != snapshot.Queue.Generation {
				return chatsession.ErrQueueChanged
			}
			turn, err := decodeSoloMetadata(*snapshot.Active)
			if err != nil {
				return err
			}
			sn.ActiveItem = soloTurnView(turn).Frame()
		}
		result.state = sn.QueueState()
		applySoloResumeSnapshot(result.state, &row)
		result.revision, result.updated, result.generation = snapshot.Queue.Revision, row.UpdatedAt, snapshot.Queue.Generation
		if snapshot.Active == nil {
			return nil
		}
		result.state["userMsgId"] = snapshot.Active.ID
		result.state["phase"] = SoloPhasePreparing
		result.activeEpoch = snapshot.Active.ExecutionEpoch
		if len(previous) > 0 && previous[0].state["userMsgId"] == snapshot.Active.ID && previous[0].generation == result.generation && previous[0].activeEpoch == result.activeEpoch && previous[0].head <= result.head {
			result.first = previous[0].first
			result.state["phase"], result.state["execution"] = previous[0].state["phase"], previous[0].state["execution"]
			result.controlsThrough = previous[0].controlsThrough
		} else {
			var first models.ChatEventChunk
			if err := tx.Select("first_sequence").Where("queue_id = ? AND turn_id = ? AND execution_epoch = ?", snapshot.Queue.ID, snapshot.Active.ID, snapshot.Active.ExecutionEpoch).Order("first_sequence").First(&first).Error; err == nil {
				result.first = int64(first.FirstSequence) - 1
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		// Capture indexes only. Native bodies and the two immutable control
		// bodies are loaded after releasing the transaction, at most once per
		// subscription/epoch. Newly committed control metadata is still seen.
		if result.head < 0 {
			return nil
		}
		query := tx.Table("chat_event_chunks").Where("queue_id = ? AND turn_id = ? AND execution_epoch = ? AND control_event IN ? AND last_sequence <= ?", snapshot.Queue.ID, snapshot.Active.ID, snapshot.Active.ExecutionEpoch, []string{SoloEventPhase, SoloEventExecution}, uint64(result.head))
		if result.controlsThrough >= 0 {
			query = query.Where("last_sequence > ?", uint64(result.controlsThrough))
		}
		if err := query.Order("first_sequence").Find(&chunks).Error; err != nil {
			return err
		}
		return nil
	})
	return result, legacy, chunks, err
}

func (s *SoloService) journalPointer(tx *gorm.DB, id, turnID string, epoch uint64) (*models.ChatEventLog, error) {
	var result struct {
		From    int64
		Through int64
		Count   int
	}
	err := tx.Model(&models.ChatEventChunk{}).Select("COALESCE(MIN(first_sequence), 0) AS `from`, COALESCE(MAX(last_sequence), 0) AS through, COALESCE(SUM(acp_count), 0) AS count").
		Where("queue_id = ? AND turn_id = ? AND execution_epoch = ? AND acp_count > 0", soloQueueScope(id).ID(), turnID, epoch).Scan(&result).Error
	if err != nil || result.Count == 0 {
		return nil, err
	}
	return &models.ChatEventLog{Version: chatsession.JournalVersion, From: result.From, Through: result.Through, Count: result.Count, Generation: epoch}, nil
}

// Subscribe reads committed data on any application replica. A slow viewer is
// disconnected with its last delivered cursor; no frame is silently dropped.
func (s *SoloService) Subscribe(id, user string, afterSeq int, legacyCursor ...bool) (<-chan chatsession.Event, func(), error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	view, err := s.journalView(ctx, id, user)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	cursor := int64(afterSeq)
	// Migration release: a pre-journal client cursor describes a process-local
	// stream, even if its numeric value happens to fit this persistent range.
	reset := cursor > view.head || cursor < -1 || len(legacyCursor) > 0 && legacyCursor[0] && cursor >= 0
	if cursor == -1 || reset {
		cursor = view.first
	}
	ch := make(chan chatsession.Event, 64)
	send := func(event chatsession.Event) bool {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case ch <- event:
			return true
		case <-ctx.Done():
		case <-timer.C:
		}
		return false
	}
	snapshot := func(view soloJournalView, reset bool) chatsession.Event {
		view.state["journalVersion"] = chatsession.JournalVersion
		view.state["stateSnapshot"] = true
		view.state["cursorReset"] = reset
		view.state["highWatermark"] = view.head
		return chatsession.Event{Seq: int(cursor), Type: chatsession.FrameSession, Event: chatsession.EventQueueState, Payload: view.state}
	}
	ch <- snapshot(view, reset)
	go func() {
		defer close(ch)
		defer cancel()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			for cursor < view.head {
				page, err := s.durable.JournalPage(ctx, soloQueueScope(id), chatsession.JournalQuery{After: cursor})
				if err != nil {
					if ctx.Err() == nil {
						log.Warn().Err(err).Str("session", id).Msg("conversation journal replay stopped without advancing cursor")
					}
					return
				}
				for _, entry := range page.Items {
					if !send(entry.StreamEvent()) {
						return
					}
					cursor = int64(entry.Seq)
				}
				if !page.HasMore {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			next, err := s.journalView(ctx, id, user, view)
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					log.Warn().Err(err).Str("session", id).Msg("conversation journal head unavailable")
				}
				return // Ownership/deletion is rechecked without waking a sandbox.
			}
			if next.head < cursor {
				return // A restored/changed dataset requires a fresh subscription.
			}
			if next.revision != view.revision || !next.updated.Equal(view.updated) {
				if !send(snapshot(next, false)) {
					return
				}
			}
			view = next
		}
	}()
	return ch, cancel, nil
}
