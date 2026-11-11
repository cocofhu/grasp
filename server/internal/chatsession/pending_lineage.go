package chatsession

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (p *PendingStore) SetExecutionController(c *executionlineage.Controller) { p.execution = c }

func (p *PendingStore) pendingEntities(db *gorm.DB, surface, key string, turns []PendingTurn) ([]executionlineage.Entity, error) {
	entities := []executionlineage.Entity{}
	if surface == "solo" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: key})
	} else if surface == "workflow.clarify" || surface == "workflow.review" {
		parts := strings.SplitN(key, "|", 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return nil, executionlineage.ErrConflict
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: parts[0]})
		var run models.Run
		if err := db.Select("id", "origin_solo_id").Take(&run, "id = ?", parts[0]).Error; err != nil {
			return nil, err
		}
		if run.OriginSoloID != "" {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
		}
		var visit models.StateRun
		err := db.Select("id").Where("run_id = ? AND node_id = ?", parts[0], parts[1]).Order("iteration desc, id desc").Take(&visit).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if visit.ID != 0 {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(visit.ID), 10)})
		}
	} else {
		return nil, executionlineage.ErrConflict
	}
	for _, turn := range turns {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityLegacyPending, ID: turn.ID})
	}
	return entities, nil
}

func (p *PendingStore) preparePending(ctx context.Context, turns []PendingTurn) ([]filestore.Prepared, error) {
	if _, inside := p.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, ErrPayloadInTransaction
	}
	files, err := filestore.FromDB(p.db)
	if err != nil {
		return nil, err
	}
	prepared := make([]filestore.Prepared, 0, len(turns))
	for _, turn := range turns {
		body, err := files.Prepare(ctx, bytes.NewReader(turn.Payload), filestore.Bytes)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, body)
	}
	return prepared, nil
}

func (p *PendingStore) insertPreparedPending(tx *gorm.DB, row models.ChatPendingTurn, body filestore.Prepared) error {
	files, err := filestore.FromDB(tx)
	if err != nil {
		return err
	}
	ref, err := files.PreparedRef(body)
	if err != nil {
		return err
	}
	return tx.Table("chat_pending_turns").Create(map[string]any{"id": row.ID, "surface": row.Surface, "session_key": row.SessionKey, "position": row.Position, "started": row.Started, "payload": ref, "created_at": row.CreatedAt}).Error
}

func (p *PendingStore) pendingTransaction(ctx context.Context, surface, key string, turns []PendingTurn, body func(*gorm.DB) error) error {
	if p.execution == nil || p.execution.LocalOnly() {
		return p.db.WithContext(ctx).Transaction(body)
	}
	entities, err := p.pendingEntities(p.db.WithContext(ctx), surface, key, turns)
	if err != nil {
		return err
	}
	return p.execution.Transaction(ctx, p.db, entities, body)
}

func (p *PendingStore) loadPending(ctx context.Context, surface string) ([]models.ChatPendingTurn, error) {
	if _, inside := p.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, ErrPayloadInTransaction
	}
	if p.execution != nil && !p.execution.LocalOnly() {
		var err error
		ctx, err = p.execution.PrepareContext(ctx)
		if err != nil {
			return nil, err
		}
	}
	// Raw cells release the SQL cursor before any object reader. Pending
	// restored receipt IDs are excluded before even attempting their body GET.
	query := p.db.WithContext(ctx).Table("chat_pending_turns").Where("surface = ?", surface).Order("session_key asc, position asc")
	if p.execution != nil && !p.execution.LocalOnly() {
		query = query.Where("NOT EXISTS (SELECT 1 FROM execution_quarantines q WHERE q.entity_kind = 'legacy_pending' AND q.entity_id = chat_pending_turns.id AND q.review_state = 'pending')")
	}
	var sources []pendingLineageSource
	if err := query.Find(&sources).Error; err != nil {
		return nil, err
	}
	files, err := filestore.FromDB(p.db)
	if err != nil {
		return nil, err
	}
	rows := make([]models.ChatPendingTurn, 0, len(sources))
	checked := map[string]bool{}
	for _, source := range sources {
		if p.execution != nil && !p.execution.LocalOnly() && !checked[source.SessionKey] {
			entities, err := p.pendingEntities(p.db.WithContext(ctx), surface, source.SessionKey, nil)
			if err != nil {
				return nil, err
			}
			err = p.execution.CheckPrepared(ctx, p.db, entities...)
			if errors.Is(err, executionlineage.ErrQuarantined) {
				continue
			}
			if err != nil {
				return nil, err
			}
			checked[source.SessionKey] = true
		}
		payload, err := files.Load(ctx, source.Payload, filestore.Bytes)
		if err != nil {
			return nil, err
		}
		rows = append(rows, models.ChatPendingTurn{ID: source.ID, Surface: source.Surface, SessionKey: source.SessionKey, Position: source.Position, Started: source.Started, Payload: payload, CreatedAt: source.CreatedAt})
	}
	return rows, nil
}

// Raw SQL projection for current generic pending-store authorization.
type pendingLineageSource struct {
	ID         string
	Surface    string
	SessionKey string
	Position   int
	Started    bool
	Payload    string
	CreatedAt  time.Time
}
